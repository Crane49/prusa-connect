package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// pathState is this node's Node Path with the values derived from it. SetPath
// replaces the whole struct, so one message is routed with consistent values.
type pathState struct {
	path fractal.NodePath
	// hasParent is false on a root node, which has no node above it.
	hasParent bool
}

// newPathState derives from a Node Path whether the node has a node above it.
func newPathState(path fractal.NodePath) *pathState {
	_, hasParent := path.Parent()
	return &pathState{path: path, hasParent: hasParent}
}

// Node is one node of the Fractal tree. Build it with New, give it interfaces
// with Register, and call Start. A transport then calls Deliver,
// ChildConnected, ChildLost, ParentConnected and ParentLost. It is safe for
// concurrent use.
type Node struct {
	binding    Binding
	admissions Admissions
	shadow     *shadow.Store
	ancestors  *shadow.Store
	// The upkeep goroutine reads the first two channels, the answerer
	// goroutine the third and the rbrAnswerer goroutine the fourth. Each has
	// room for one signal, so a send never blocks.
	arbrAsked    chan struct{}
	upkeepDue    chan struct{}
	windowClosed chan struct{}
	rbrAsked     chan struct{}
	// rbrPayload is the bytes of the last RBR that arrived, to forward down.
	rbrPayload atomic.Pointer[[]byte]
	// arbrPayload is the bytes of the last ARBR that arrived, to forward up.
	arbrPayload atomic.Pointer[[]byte]
	// parentLinks counts the ParentConnected calls, so the upkeep goroutine
	// knows whether its last ARBR came after the latest one.
	parentLinks     atomic.Uint64
	parentLinkUp    atomic.Bool
	arbrWindow      time.Duration
	arbrMinInterval time.Duration
	executed        ExecutedStore
	processed       ProcessedStore
	clock           Clock
	log             *slog.Logger
	metrics         Metrics
	state           atomic.Pointer[pathState]
	work            chan func(context.Context)
	rpc             chan func(context.Context)
	stop            context.CancelFunc
	wg              sync.WaitGroup
	mu              sync.Mutex
	owned           map[instanceKey]*Interface
	ownedOrder      []*Interface
	pending         map[fractal.RPCID]chan fractal.RES
	maxPending      int
	workers         int
	callGrace       time.Duration
	running         bool
	startupDone     bool
}

// New builds a Node from cfg. It returns ErrNoBinding when cfg.Binding is nil,
// ErrSharedShadow when cfg.Ancestors is cfg.Shadow, and an error when cfg.Path
// is set and is not a valid Node Path. New starts no goroutine.
func New(cfg Config) (*Node, error) {
	if cfg.Binding == nil {
		return nil, ErrNoBinding
	}
	if cfg.Ancestors != nil && cfg.Ancestors == cfg.Shadow {
		return nil, ErrSharedShadow
	}
	store, err := storeOrEmpty(cfg.Shadow)
	if err != nil {
		return nil, err
	}
	ancestors, err := storeOrEmpty(cfg.Ancestors)
	if err != nil {
		return nil, err
	}
	clock := cfg.Clock
	if clock == nil {
		clock = SystemClock{}
	}
	n := &Node{
		binding:         cfg.Binding,
		admissions:      orDefault[Admissions](cfg.Admissions, NoChildren{}),
		shadow:          store,
		ancestors:       ancestors,
		arbrAsked:       make(chan struct{}, 1),
		upkeepDue:       make(chan struct{}, 1),
		windowClosed:    make(chan struct{}, 1),
		rbrAsked:        make(chan struct{}, 1),
		arbrWindow:      positive(cfg.ARBRWindow, DefaultARBRWindow),
		arbrMinInterval: positive(cfg.ARBRMinInterval, DefaultARBRMinInterval),
		clock:           clock,
		log:             orDefault(cfg.Logger, slog.New(slog.DiscardHandler)),
		metrics:         orDefault[Metrics](cfg.Metrics, nopMetrics{}),
		work:            make(chan func(context.Context), positive(cfg.WorkQueue, DefaultWorkQueue)),
		rpc:             make(chan func(context.Context), positive(cfg.RPCQueue, DefaultRPCQueue)),
		owned:           make(map[instanceKey]*Interface),
		pending:         make(map[fractal.RPCID]chan fractal.RES),
		maxPending:      positive(cfg.MaxPendingCalls, DefaultMaxPendingCalls),
		callGrace:       cfg.CallGrace,
	}
	if n.callGrace <= 0 {
		n.callGrace = DefaultCallGrace
	}
	memory := NewMemoryDedup(clock, 0, 0)
	n.executed = orDefault[ExecutedStore](cfg.Executed, memory)
	n.processed = orDefault[ProcessedStore](cfg.Processed, memory)
	n.workers = positive(cfg.Workers, DefaultWorkers)
	if cfg.Path != "" {
		if pathErr := cfg.Path.Validate(); pathErr != nil {
			return nil, pathErr
		}
		n.state.Store(newPathState(cfg.Path))
	}
	n.ancestors.Rebase(cfg.Path)
	return n, nil
}

// Path returns this node's Node Path. It is empty until Config.Path or SetPath
// sets one.
func (n *Node) Path() fractal.NodePath {
	s := n.state.Load()
	if s == nil {
		return ""
	}
	return s.path
}

// Shadow returns the shadow store: the local copy of the property values the
// nodes below this one reported.
func (n *Node) Shadow() *shadow.Store { return n.shadow }

// Ancestors returns the ancestor shadow: the local copy of the property values
// the nodes above this one reported. It has only nodes above the current Node
// Path.
func (n *Node) Ancestors() *shadow.Store { return n.ancestors }

// Start starts the node's goroutines and returns. They run until ctx ends or
// Close is called. A node that has a Node Path also asks every node directly
// below it for a full state report. Start returns ErrAlreadyStarted when the
// node is running.
func (n *Node) Start(ctx context.Context) error {
	n.mu.Lock()
	if n.running {
		n.mu.Unlock()
		return ErrAlreadyStarted
	}
	runCtx, cancel := context.WithCancel(ctx)
	n.stop = cancel
	n.running = true
	n.mu.Unlock()

	for range n.workers {
		n.wg.Add(1)
		go n.pump(runCtx, n.work)
	}
	n.wg.Add(1)
	go n.pump(runCtx, n.rpc)
	n.wg.Add(1)
	go n.upkeep(runCtx)
	n.wg.Add(1)
	go n.answerer(runCtx)
	n.wg.Add(1)
	go n.rbrAnswerer(runCtx)
	n.startup(runCtx)
	return nil
}

// Close stops the node's goroutines and waits until they have returned. A
// second call does nothing. It always returns nil.
func (n *Node) Close() error {
	n.mu.Lock()
	stop, running := n.stop, n.running
	n.running = false
	n.mu.Unlock()
	if !running {
		return nil
	}
	stop()
	n.wg.Wait()
	return nil
}

// SetPath sets this node's Node Path. A node that is not the root calls it
// each time a join grants it a path. Setting the same path does nothing.
//
// A new path empties the ancestor shadow. A changed path also drops every
// pending Call, because its answer is addressed to the old path, and resets
// the dedup stores that implement Resetter.
func (n *Node) SetPath(ctx context.Context, path fractal.NodePath) error {
	if err := path.Validate(); err != nil {
		return err
	}
	old := n.Path()
	if old == path {
		return nil
	}
	n.state.Store(newPathState(path))
	// The path is stored before Rebase. An AREP checked against the old path
	// is then refused by the store or deleted by Rebase.
	n.ancestors.Rebase(path)
	signal(n.upkeepDue)
	if old != "" {
		n.discardPending(ctx, old, path)
	}
	n.mu.Lock()
	n.startupDone = false
	running := n.running
	n.mu.Unlock()
	if running {
		n.startup(ctx)
	}
	return nil
}

// ChildConnected tells the node that the link to the node with this Node ID,
// directly below this one, is up. The transport calls it. The node sends that
// node an RBR, the request for a full state report.
func (n *Node) ChildConnected(ctx context.Context, id fractal.NodeID) {
	if _, ok := n.pathIfSet(); !ok {
		n.log.WarnContext(ctx, "a node below connected before this node had a node path", "child", id)
		return
	}
	n.sendRBR(ctx, id)
}

// ChildLost tells the node that the link to the node with this Node ID,
// directly below this one, is down. The node marks that node and every node
// under it as stale in the shadow and sends a LOS to the node above. The
// transport must not call it for a node that has never connected.
func (n *Node) ChildLost(ctx context.Context, id fractal.NodeID) {
	state, ok := n.pathIfSet()
	if !ok {
		n.log.WarnContext(ctx, "the link to a node below went down before this node had a node path", "child", id)
		return
	}
	affected, err := state.Child(id)
	if err != nil {
		n.log.ErrorContext(ctx, "the node id of the lost link makes no valid node path", "child", id, "error", err)
		return
	}
	changed := n.shadow.MarkStale(affected)
	n.log.InfoContext(ctx, "the link to a node below is down", "child", id, "stale", len(changed))
	payload, err := json.Marshal(fractal.LOS{Version: fractal.Version, Sender: state, Child: id})
	if err != nil {
		n.log.ErrorContext(ctx, "encode los", "child", id, "error", err)
		return
	}
	n.sendOrLog(ctx, Dispatch{Kind: fractal.KindLOS, Payload: payload})
}

// ChildRevoked deletes the shadow of a node below that this node revoked, and
// of every node under it. Call it after ChildLost.
func (n *Node) ChildRevoked(ctx context.Context, id fractal.NodeID) {
	state, ok := n.pathIfSet()
	if !ok {
		return
	}
	gone, err := state.Child(id)
	if err != nil {
		n.log.ErrorContext(ctx, "the node id of the revoked node makes no valid node path", "child", id, "error", err)
		return
	}
	removed := n.shadow.RemoveNode(gone)
	n.log.InfoContext(ctx, "a node below was revoked, so the node forgot its state", "child", id,
		"removed", len(removed))
}

// startup sends an RBR to every node directly below this one, once for each
// Node Path the node gets. It does nothing while the node has no path.
func (n *Node) startup(ctx context.Context) {
	if _, ok := n.pathIfSet(); !ok {
		return
	}
	n.mu.Lock()
	done := n.startupDone
	n.startupDone = true
	n.mu.Unlock()
	if done {
		return
	}
	n.broadcastRBR(ctx)
}

// pump runs the jobs of one queue, one after another, until ctx ends.
func (n *Node) pump(ctx context.Context, queue <-chan func(context.Context)) {
	defer n.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-queue:
			job(ctx)
		}
	}
}

// submit puts a job on a queue and never blocks. It returns false when the
// queue is full.
func (n *Node) submit(queue chan func(context.Context), job func(context.Context)) bool {
	select {
	case queue <- job:
		return true
	default:
		return false
	}
}

// pathIfSet returns this node's Node Path. ok is false while the node has none.
func (n *Node) pathIfSet() (fractal.NodePath, bool) {
	s := n.state.Load()
	if s == nil {
		return "", false
	}
	return s.path, true
}

// send passes a message to the transport, durably or best-effort as
// Kind.Delivery says.
func (n *Node) send(ctx context.Context, d Dispatch) error {
	if d.Kind.Delivery() == fractal.Durable {
		return n.binding.SendDurable(ctx, d)
	}
	return n.binding.SendBestEffort(ctx, d)
}

// sendOrLog sends a message. When the send fails it logs the error and counts
// the message as dropped.
func (n *Node) sendOrLog(ctx context.Context, d Dispatch) {
	if err := n.send(ctx, d); err != nil {
		n.log.ErrorContext(ctx, "the transport refused a message", "kind", d.Kind, "child", d.Child, "error", err)
		n.metrics.Drop(d.Kind, sendDropReason(err))
	}
}

// sendDropReason picks the DropReason for a failed send: DropNotRoutable for
// ErrNoSuchLink, DropSendFailed for any other error.
func sendDropReason(err error) DropReason {
	if errors.Is(err, ErrNoSuchLink) {
		return DropNotRoutable
	}
	return DropSendFailed
}

// orDefault returns value, or fallback when value is the zero value.
func orDefault[T comparable](value, fallback T) T {
	var zero T
	if value == zero {
		return fallback
	}
	return value
}

// storeOrEmpty returns store, or a new store with an empty selection when
// store is nil.
func storeOrEmpty(store *shadow.Store) (*shadow.Store, error) {
	if store != nil {
		return store, nil
	}
	built, err := shadow.New(shadow.Config{})
	if err != nil {
		return nil, fmt.Errorf("build an empty shadow: %w", err)
	}
	return built, nil
}

// positive returns value when it is above zero, and fallback otherwise.
func positive[T int | time.Duration](value, fallback T) T {
	if value <= 0 {
		return fallback
	}
	return value
}
