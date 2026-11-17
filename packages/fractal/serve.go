package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prusa3d/fractal-link/bridge"
	"github.com/prusa3d/fractal-link/files"
	"github.com/prusa3d/fractal-link/interfaces"
	"github.com/prusa3d/fractal-link/natsbind"

	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// reportBudget is the longest this module waits for the host to answer one
// link.changed report.
const reportBudget = 5 * time.Second

// runners is the most goroutines parts.start starts. It is the size of
// the error channel, so no goroutine blocks when it reports a failure.
const runners = 7

// ErrRelocated means the server above gave this node another Node Path. The
// streams and records are named by the path, so the process stops and the
// host starts it again under the new one.
var ErrRelocated = errors.New("this node was granted another place in the tree")

// engine passes the messages of the transport to the node. The transport and
// the node each need the other to be built, so the engine starts empty and
// drops every message until assemble stores the node in it.
type engine struct {
	node atomic.Pointer[fractalnode.Node]
	// cache is the downward cache, nil when the files extension is off.
	cache atomic.Pointer[files.Cache]
	log   *slog.Logger
}

// Deliver passes one message the transport received to the node.
func (e *engine) Deliver(ctx context.Context, in fractalnode.Inbound) {
	if node := e.node.Load(); node != nil {
		node.Deliver(ctx, in)
	}
}

// ChildConnected tells the node that the connection of one server below this
// one is open.
func (e *engine) ChildConnected(ctx context.Context, id fractal.NodeID) {
	if node := e.node.Load(); node != nil {
		node.ChildConnected(ctx, id)
	}
}

// ChildLost tells the node that the connection of one server below this one is
// closed.
func (e *engine) ChildLost(ctx context.Context, id fractal.NodeID) {
	if node := e.node.Load(); node != nil {
		node.ChildLost(ctx, id)
	}
}

// ChildRevoked tells the node that this server revoked one server below it.
func (e *engine) ChildRevoked(ctx context.Context, id fractal.NodeID) {
	if node := e.node.Load(); node != nil {
		node.ChildRevoked(ctx, id)
	}
}

// ParentConnected tells the node that the connection to the server above this
// one is open.
func (e *engine) ParentConnected(ctx context.Context) {
	if node := e.node.Load(); node != nil {
		node.ParentConnected(ctx)
	}
}

// ParentLost tells the node that the connection to the server above this one
// is closed.
func (e *engine) ParentLost(ctx context.Context) {
	if node := e.node.Load(); node != nil {
		node.ParentLost(ctx)
	}
}

// ForgetAncestors tells the node that the server above revoked this server,
// and drops the copies of files from above.
func (e *engine) ForgetAncestors(ctx context.Context) {
	if cache := e.cache.Load(); cache != nil {
		if err := cache.Drop(); err != nil {
			e.log.ErrorContext(ctx, "the copies of files from above could not be dropped after a revocation",
				"error", err.Error())
		}
	}

	if node := e.node.Load(); node != nil {
		node.ForgetAncestors(ctx)
	}
}

// SetPath gives the node the Node Path the server above granted it, and drops
// the copies of files from above when the path is new. It runs before the
// connection to the server above comes up.
func (e *engine) SetPath(ctx context.Context, path fractal.NodePath) error {
	if cache := e.cache.Load(); cache != nil {
		if err := cache.Follow(path); err != nil {
			return fmt.Errorf("keep the copies of files under the granted node path: %w", err)
		}
	}

	node := e.node.Load()
	if node == nil {
		return nil
	}

	if err := node.SetPath(ctx, path); err != nil {
		return fmt.Errorf("take the granted node path: %w", err)
	}

	return nil
}

// session is the ways this process calls the host.
type session struct {
	fleet   bridge.Fleet
	stream  busapi.Opener
	changes reportedBy
	storage files.Storage
	access  bridge.Access
}

// parts is one running node: the transport, the Fractal node, the bridge, and
// a read-only view of the shadow.
type parts struct {
	binding *natsbind.Binding
	node    *fractalnode.Node
	bridge  *bridge.Bridge
	shadow  *bridge.ShadowView
	stream  busapi.Opener

	// publisher and stores publish the shared folders. Both are nil when the
	// file extension is off.
	publisher *files.Publisher
	stores    *files.ContentStores
	rescan    time.Duration
	// files serves /objects/ and prints fractal URLs, nil when the file
	// extension is off.
	files *filesPart
}

// openPublisher reads the shared folders and builds the publisher of the
// content stores. It needs no node, so the bridge can print through it.
func openPublisher(cfg Config, host session, log *slog.Logger) (*files.Shares, *files.Publisher, error) {
	shares, err := files.OpenShares(cfg.Grants.Data)
	if err != nil {
		return nil, nil, fmt.Errorf("read the shared folders: %w", err)
	}

	storages := func(context.Context) ([]string, error) { return shares.Storages(), nil }

	return shares, files.NewPublisher(host.storage, shares, cfg.Grants.Data, storages, log), nil
}

// publish registers the content stores of publisher on the node.
func (p *parts) publish(publisher *files.Publisher, rescan time.Duration, log *slog.Logger) error {
	stores, err := files.NewContentStores(publisher, storeRegistrar{node: p.node}, log)
	if err != nil {
		return fmt.Errorf("build the content stores: %w", err)
	}

	p.publisher, p.stores, p.rescan = publisher, stores, rescan

	return nil
}

// serve waits for the settings, builds the node and runs it. It returns when
// ctx ends, on a new Node Path from above, or when a part of the node fails.
func serve(
	ctx context.Context, state *link, below *tree, host session,
	log *slog.Logger, settings <-chan Config,
) error {
	var cfg Config

	select {
	case cfg = <-settings:
	case <-ctx.Done():
		return nil
	}

	built, err := build(ctx, cfg, state, below, host, log)
	if err != nil {
		return err
	}

	return run(ctx, built, log)
}

// build builds the transport, the node, the shadow and the bridge. It closes
// the transport when a later step fails.
func build(
	ctx context.Context, cfg Config, state *link, below *tree, host session, log *slog.Logger,
) (*parts, error) {
	registry, err := interfaces.Registry()
	if err != nil {
		return nil, err
	}

	mapping, err := bridge.LoadMapping(registry)
	if err != nil {
		return nil, fmt.Errorf("read the mapping table: %w", err)
	}

	// The counters are built before the transport, because the transport's
	// callback reads them and runs before the node exists.
	count := newCounters()

	bindingCfg, err := bindingConfig(cfg, state, host, log, count)
	if err != nil {
		return nil, err
	}

	holder := &engine{log: log}

	binding, err := natsbind.New(ctx, bindingCfg, holder)
	if err != nil {
		return nil, startFailure(cfg, err)
	}

	var (
		shares    *files.Shares
		publisher *files.Publisher
		part      *filesPart
	)

	if cfg.Files != nil {
		shares, publisher, err = openPublisher(cfg, host, log)
		if err == nil {
			part, err = buildFiles(ctx, cfg, binding, publisher, host.storage, log)
		}

		if err != nil {
			_ = binding.Close() // the start failed, and err says why

			return nil, err
		}

		holder.cache.Store(part.cache)
	}

	built, err := assemble(cfg, bindingCfg.Shadow, binding, holder, mapping, host, log, count, part)
	if err == nil && cfg.Files != nil {
		err = built.publish(publisher, cfg.Files.Rescan, log)
	}

	if err != nil {
		if part != nil {
			_ = part.listener.Close() // the start failed, and err says why
		}

		_ = binding.Close()

		return nil, err
	}

	if shares != nil {
		below.sharing(shares, host.storage, log)
		below.browsing(bridge.NewShadowView(built.node.Ancestors()), part.files)
	}

	state.serve(binding, count)
	below.serving(built.shadow, built.node, binding.Path)

	return built, nil
}

// bindingConfig returns the configuration of the transport. Its OnChange
// callback stores each new status in state and reports it to the host.
func bindingConfig(
	cfg Config, state *link, host session, log *slog.Logger, count *counters,
) (natsbind.Config, error) {
	bindingCfg, err := cfg.Binding()
	if err != nil {
		return natsbind.Config{}, err
	}

	bindingCfg.Logger = log

	if cfg.Identity != "" {
		signer, signerErr := natsbind.NewSigner(cfg.Identity)
		if signerErr != nil {
			return natsbind.Config{}, ConfigValueInvalid.New("identity is not an NKEY seed").
				With(fault.StringParam(paramField, "identity")).Wrap(signerErr)
		}

		bindingCfg.Identity = signer
	}

	bindingCfg.OnChange = func(status natsbind.Status) {
		current := currentReading(status, count)
		state.update(current)
		reportEvery(context.Background(), host.changes, current, reportBudget)
	}

	return bindingCfg, nil
}

// assemble builds the shadow, the node and the bridge on one transport, and
// stores the node in the engine so the transport's messages reach it.
func assemble(
	cfg Config,
	shadowCfg shadow.Config,
	binding *natsbind.Binding,
	holder *engine,
	mapping *bridge.Mapping,
	host session,
	log *slog.Logger,
	count *counters,
	part *filesPart,
) (*parts, error) {
	store, err := shadow.New(shadowCfg)
	if err != nil {
		return nil, fmt.Errorf("build the copy of the state below: %w", err)
	}

	ancestors, err := shadow.New(cfg.ancestorShadow())
	if err != nil {
		return nil, fmt.Errorf("build the copy of the state above: %w", err)
	}

	node, err := fractalnode.New(fractalnode.Config{
		Binding:    binding,
		Admissions: binding,
		Shadow:     store,
		Ancestors:  ancestors,
		Executed:   binding,
		Processed:  binding,
		Logger:     log,
		Metrics:    count,
		Path:       binding.Path(),
	})
	if err != nil {
		return nil, fmt.Errorf("build the node: %w", err)
	}

	holder.node.Store(node)

	flush, err := cfg.Flush()
	if err != nil {
		return nil, err
	}

	resync, err := cfg.Resync()
	if err != nil {
		return nil, err
	}

	made, err := bridge.New(bridge.Config{
		Fleet:     host.fleet,
		Node:      registrar{node: node},
		Mapping:   mapping,
		Principal: principalOf(cfg, binding),
		Flush:     flush,
		Resync:    resync,
		Logger:    log,

		Files:         part.bridgeFiles(),
		StartDeadline: part.startDeadline(),
		Access:        host.access,
	})
	if err != nil {
		return nil, fmt.Errorf("build the bridge: %w", err)
	}

	return &parts{
		binding: binding,
		node:    node,
		bridge:  made,
		shadow:  bridge.NewShadowView(store),
		stream:  host.stream,
		files:   part,
	}, nil
}

// registrar adapts the node to bridge.Registrar. The node's Register returns
// its own handle type, and the bridge needs only the Report method of it.
type registrar struct {
	node *fractalnode.Node
}

// Register registers one interface instance with the node and returns the
// handle the bridge sends its state reports through.
func (r registrar) Register(inst fractalnode.Instance) (bridge.Reporter, error) {
	handle, err := r.node.Register(inst)
	if err != nil {
		return nil, fmt.Errorf("register %s: %w", inst.Name, err)
	}

	return handle, nil
}

// storeRegistrar adapts the node to files.Registrar, as registrar does to
// bridge.Registrar.
type storeRegistrar struct {
	node *fractalnode.Node
}

// Register registers one ContentStore1 instance with the node.
func (r storeRegistrar) Register(inst fractalnode.Instance) (files.Reporter, error) {
	return registrar(r).Register(inst)
}

// The values of the command_principal setting.
const (
	// PrincipalNode is the default. The host checks the permissions of the
	// server above this one, as "node:<its Node ID>".
	PrincipalNode = "node"
	// PrincipalSelf makes the host check the permissions of this module.
	PrincipalSelf = "self"
)

// principalOf returns the function that names who a command from above runs
// as. PrincipalNode gives "node:<Node ID of the server above>", and "" on a
// server with none above. PrincipalSelf gives "", so the host checks the
// permissions of this module.
func principalOf(cfg Config, binding *natsbind.Binding) func() string {
	if cfg.CommandPrincipal == PrincipalSelf {
		return func() string { return "" }
	}

	return func() string {
		above, ok := binding.Path().Parent()
		if !ok {
			return ""
		}

		return "node:" + above.Last().String()
	}
}

// run runs the parts until ctx ends, a new Node Path arrives or one part
// fails. It closes the node before the transport and returns the reason.
func run(ctx context.Context, built *parts, log *slog.Logger) error {
	running, cancel := context.WithCancel(ctx)
	defer cancel()

	var group sync.WaitGroup

	failed := make(chan error, runners)

	built.start(running, &group, failed)

	var reason error

	select {
	case <-ctx.Done():
	case path := <-built.binding.Relocated():
		log.ErrorContext(ctx, "the server above granted this node another place in the tree, "+
			"so this process stops and is started again under it",
			"granted", path.String(), "running_under", built.binding.Path().String())

		// Drop the copies of the former parent and record the new path
		// before the next process connects.
		reason = ErrRelocated

		if built.files != nil {
			if err := built.files.cache.Follow(path); err != nil {
				reason = fmt.Errorf("%w: drop the copies of files from above: %w", ErrRelocated, err)
			}
		}
	case err := <-failed:
		reason = err
	}

	cancel()
	group.Wait()

	_ = built.node.Close()
	_ = built.binding.Close()

	log.InfoContext(ctx, "the node has stopped",
		"shadow_interfaces", built.shadow.Count().Interfaces)

	return reason
}

// start runs each part in its own goroutine until ctx ends. The first one
// that fails earlier sends its error on failed.
func (p *parts) start(ctx context.Context, group *sync.WaitGroup, failed chan<- error) {
	work := map[string]func(context.Context) error{
		"the transport": p.binding.Run,
		"the node":      p.node.Start,
		"the bridge":    p.bridge.Run,
		"the event stream": func(inner context.Context) error {
			return watch(inner, p.stream, p.bridge)
		},
	}

	if p.publisher != nil {
		work["the publisher"] = func(inner context.Context) error { return p.publisher.Run(inner, p.rescan) }
		work["the content stores"] = p.stores.Run
	}

	if p.files != nil {
		work["the file server"] = p.files.serve
	}

	for name, runner := range work {
		group.Go(func() {
			if err := runner(ctx); err != nil && ctx.Err() == nil {
				select {
				case failed <- fmt.Errorf("%s stopped: %w", name, err):
				default:
				}
			}
		})
	}
}

// watch passes every window of the host's event stream to the bridge. It
// subscribes again when the host ends the subscription with no error.
func watch(ctx context.Context, stream busapi.Opener, made *bridge.Bridge) error {
	if stream == nil {
		<-ctx.Done()

		return nil
	}

	for ctx.Err() == nil {
		err := busapi.Subscribe(ctx, stream, busapi.SubscribeParams{},
			func(window busapi.Window) error { return made.Window(ctx, window) })
		if err != nil && ctx.Err() == nil {
			return fmt.Errorf("follow the event stream: %w", err)
		}
	}

	return nil
}
