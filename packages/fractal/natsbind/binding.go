package natsbind

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/prusa3d/fractal-link/natsbind/joinwire"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/join"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// The files a node writes in its data directory, beside the JetStream store.
const (
	// admissionFile lists the nodes this node let join, and the ones an
	// operator revoked.
	admissionFile = "admissions.json"
	// membershipFn has the Node Path and the connection credentials this node
	// received when it joined.
	membershipFn = "membership.json"
	// pendingFile lists the join requests that wait for an operator.
	pendingFile = "pending.json"
)

// The errors the binding returns.
var (
	// ErrNoLink means a message names a node below this one and this node has
	// no link to it. A link is the account, the connection and the stream this
	// node creates for one node that joined. The error wraps the engine's
	// node.ErrNoSuchLink, so the engine answers a method invocation with
	// fractal.CodeNodeNotFound.
	ErrNoLink = fmt.Errorf("no link to that node: %w", fractalnode.ErrNoSuchLink)
	// ErrNoUplink means the engine sent a message upward and this node joined
	// no other node.
	ErrNoUplink = errors.New("this node has no node above it")
	// ErrNotStarted means a caller sent a message before Start or after Close.
	ErrNotStarted = errors.New("the binding is not running")
	// ErrAlreadyStarted means a caller called Start twice.
	ErrAlreadyStarted = errors.New("the binding is already running")
	// ErrNoNodePath means a caller called Start before the node had a Node
	// Path. The node names its JetStream domain after the Node Path, so it
	// cannot start the server without one.
	ErrNoNodePath = errors.New("the node holds no node path")
	// ErrUnknownNode means an operator named a Node ID this node has no record
	// of: Approve found no waiting join request for it, or Revoke found no
	// node that joined under it.
	ErrUnknownNode = errors.New("no node below this one under that id")
)

// Engine receives the messages the binding reads from NATS. The Node of
// sdk/fractal/node implements it. The interface is small so that a test can
// use a recorder.
type Engine interface {
	// Deliver receives one message the binding read from NATS.
	Deliver(ctx context.Context, in fractalnode.Inbound)
	// ChildConnected tells the engine that the node below with this Node ID
	// connected and listens on its connection.
	ChildConnected(ctx context.Context, id fractal.NodeID)
	// ChildLost tells the engine that the node below with this Node ID
	// disconnected or stopped sending heartbeats.
	ChildLost(ctx context.Context, id fractal.NodeID)
	// ChildRevoked tells the engine that this node revoked the node below
	// with this Node ID, after ChildLost. The engine then deletes the shadow
	// of that node and of every node under it.
	ChildRevoked(ctx context.Context, id fractal.NodeID)
	// ParentConnected tells the engine that the leafnode connection to the
	// node above opened and the node above listens on it. The engine then
	// asks the nodes above for their state.
	ParentConnected(ctx context.Context)
	// ParentLost tells the engine that the leafnode connection to the node
	// above closed after it was open. The engine then marks the state of the
	// nodes above stale and tells the nodes below.
	ParentLost(ctx context.Context)
	// ForgetAncestors tells the engine that the node above revoked this
	// node's admission. The engine then discards its ancestor shadow in full.
	ForgetAncestors(ctx context.Context)
}

// PathSetter is an optional interface of an Engine. The binding calls SetPath
// with the Node Path the node above assigned, so the engine routes messages
// under that path.
type PathSetter interface {
	SetPath(ctx context.Context, path fractal.NodePath) error
}

// Binding sends and receives Prusa Fractal messages over NATS. The engine of
// sdk/fractal/node uses it as its transport: the engine calls the Send methods,
// and the binding calls Engine.Deliver for every message it reads.
//
// One Binding owns one embedded NATS server, this node's NATS account, one
// account for each node below, and the goroutines that read from them. A NATS
// account is an isolated set of subjects: a connection in one account cannot
// read another account's messages. Every goroutine stops when the context
// given to Start ends, and Close waits for all of them.
type Binding struct {
	cfg      Config
	engine   Engine
	log      *slog.Logger
	identity Signer
	ident    fractal.Identity
	anchors  *x509.CertPool
	accounts *accounts

	joiner     *join.Joiner
	membership join.MembershipStore
	parent     *join.Parent
	askFirst   *joinwire.AskFirst

	// relocate receives a Node Path that the node above assigned while this
	// node was running under a different one. It buffers one path. When the
	// buffer is full the binding drops the new path.
	relocate chan fractal.NodePath

	// refusedJoin receives the error of a grant this node refused in a Join
	// while it runs, so Run stops the link and the renewals. It buffers one.
	refusedJoin chan error

	// provisioning is held while provisionLink builds a link.
	provisioning sync.Mutex

	mu   sync.Mutex
	path fractal.NodePath
	srv  *server.Server
	// srvOpts are the options srv runs with. A reload starts from a copy.
	srvOpts   *server.Options
	local     *nats.Conn
	bootstrap *nats.Conn
	// events is this node's connection in the system account, on which
	// leafEvents receives the connect and disconnect events of the server.
	events     *nats.Conn
	leafEvents chan *nats.Msg
	localJS    nats.JetStreamContext
	dedup      *kvDedup
	links      map[fractal.NodeID]*link
	uplink     Uplink
	// parentUp is whether pollLeaves last saw the parent link open. Uplink
	// also records joins and denials, so its state misses some transitions.
	parentUp  bool
	leafToken string
	leafRemot string
	heartbeat time.Duration
	running   bool
	// closing is set while Close waits for the goroutines, so spawn starts
	// no new one.
	closing bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	// extensions is the join data of the last grant, for the extensions in
	// Config.JoinExtensions.
	extensions map[fractal.Identifier]json.RawMessage

	// pathRefused is the error of the engine for the Node Path of the last
	// grant before Start. Start refuses to bring the node up while it is set.
	pathRefused error

	// permanent is the refusal that ends the joining of this node, kept in
	// refusalFile. While it is set the node sends no join request and opens no
	// leafnode connection to the node above.
	permanent error
}

// New builds a Binding. It reads the node's identity key, its saved Node Path
// and its list of joined nodes from cfg.DataDir. It opens no connection and
// starts no server: Start does that.
func New(ctx context.Context, cfg Config, engine Engine) (*Binding, error) {
	if engine == nil {
		return nil, errors.New("the binding has no engine")
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("fractal over nats: %w", err)
	}
	cfg = cfg.withDefaults()
	if err := os.MkdirAll(cfg.DataDir, dirMode); err != nil {
		return nil, fmt.Errorf("make %s: %w", cfg.DataDir, err)
	}
	identity := cfg.Identity
	if identity == nil {
		loaded, err := loadOrCreateIdentity(cfg.DataDir)
		if err != nil {
			return nil, err
		}
		identity = loaded
	}
	ident, err := identityOf(identity)
	if err != nil {
		return nil, err
	}
	anchors, err := LoadAnchors(anchorFiles(cfg.TLS))
	if err != nil {
		return nil, err
	}
	accounts, err := newAccounts(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	b := &Binding{
		cfg:         cfg,
		engine:      engine,
		log:         cfg.Logger,
		identity:    identity,
		ident:       ident,
		anchors:     anchors,
		accounts:    accounts,
		membership:  join.NewFileMembershipStore(filepath.Join(cfg.DataDir, membershipFn)),
		links:       map[fractal.NodeID]*link{},
		relocate:    make(chan fractal.NodePath, 1),
		refusedJoin: make(chan error, 1),
		heartbeat:   cfg.HeartbeatInterval,
		uplink:      Uplink{State: UplinkNotConfigured, Address: cfg.ParentAddr},
	}
	if setupErr := b.setupRole(ctx); setupErr != nil {
		return nil, setupErr
	}
	// The first reading goes out before the node joins, so the links page of
	// the host shows this node's name, its key and the address it is trying,
	// rather than an empty body for as long as the join takes.
	b.report()
	return b, nil
}

// setupRole builds the joiner when ParentAddr is set, so this node can send a
// join request. It builds the join endpoint when AcceptsChildren is set, so
// this node can answer join requests. It also loads the saved Node Path.
func (b *Binding) setupRole(ctx context.Context) error {
	if b.cfg.ParentAddr != "" {
		joiner, err := b.newJoiner()
		if err != nil {
			return err
		}
		b.joiner = joiner
		b.uplink.State = UplinkJoining
	}
	held, joined, err := b.membership.LoadMembership(ctx)
	if err != nil {
		return fmt.Errorf("read the saved node path and credentials: %w", err)
	}
	if joined {
		if refused := b.checkSaved(held); refused != nil {
			b.log.ErrorContext(ctx, "this node refused its saved grant and waits for a new one",
				"address", b.cfg.ParentAddr, "error", refused)
			b.uplink.State = UplinkDenied
			b.uplink.Reason, b.uplink.Refused = refusedReason(refused), true
		} else {
			b.adopt(held)
		}
	}
	if b.joiner != nil {
		if refusalErr := b.loadRefusal(ctx); refusalErr != nil {
			return refusalErr
		}
	}
	if b.cfg.ParentAddr == "" {
		path, pathErr := fractal.ParseNodePath(string(b.cfg.NodeID))
		if pathErr != nil {
			return fmt.Errorf("a root node uses its node id as its node path: %w", pathErr)
		}
		b.path = path
	}
	if !b.cfg.AcceptsChildren {
		return nil
	}
	return b.newParent(ctx)
}

// checkSaved runs Config.CheckExtensions on the join data of the saved
// membership, as the joiner does on a grant it receives. A grant saved before
// this node ran an extension, or under settings it no longer accepts, would
// otherwise bring up the link when the node above does not answer at start.
func (b *Binding) checkSaved(held join.Membership) error {
	if b.joiner == nil || b.cfg.CheckExtensions == nil {
		return nil
	}
	if err := b.cfg.CheckExtensions(held.Extensions); err != nil {
		return fmt.Errorf("the saved grant: %w: %w", join.ErrExtensionRefused, err)
	}
	return nil
}

// Path returns this node's Node Path. It is empty until the node above grants
// the join request.
func (b *Binding) Path() fractal.NodePath {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.path
}

// Identity returns the public key of this node's identity key pair.
func (b *Binding) Identity() fractal.Identity { return b.ident }

// Admitted reports whether a node with this Node ID joined this node and is
// not revoked. With it the Binding implements the Admissions interface of
// sdk/fractal/node, so the engine can use the Binding for that too.
func (b *Binding) Admitted(id fractal.NodeID) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	l, ok := b.links[id]
	if !ok {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.admission == AdmissionAdmitted
}

// ChildByIdentity returns the Node ID of the node below that joined with this
// public key and is not revoked. A node checks with it that the caller of a
// request it serves is one of its children.
func (b *Binding) ChildByIdentity(id fractal.Identity) (fractal.NodeID, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for nodeID, l := range b.links {
		l.mu.Lock()
		match := l.identity == id && l.admission == AdmissionAdmitted
		l.mu.Unlock()
		if match {
			return nodeID, true
		}
	}
	return "", false
}

// Signer returns what signs with this node's identity key. The seed stays
// inside it.
func (b *Binding) Signer() joinwire.Signer { return b.identity }

// Children iterates the Node ID of every node that joined this node and is
// not revoked, sorted as text.
func (b *Binding) Children() iter.Seq[fractal.NodeID] {
	return func(yield func(fractal.NodeID) bool) {
		for _, id := range b.childIDs() {
			if !yield(id) {
				return
			}
		}
	}
}

// Join sends a join request to the node above and saves the Node Path and the
// connection credentials from the grant. It sends the request again after no
// answer, and after a denial that allows a retry, until the context ends. It
// returns an error for a denial that allows no retry. On a root node, which
// has no ParentAddr, it does nothing.
//
// A denial that allows no retry, or a grant this node refuses, stops the
// renewals of Run while it runs, and Run drops only the link above. This node
// refuses a grant for a protocol version or an extension it does not support,
// one that misses a field, and one from another parent or with another Node
// Path. Such a refusal is
// kept, also for a new start of the module, and every later Join returns it
// without a request.
func (b *Binding) Join(ctx context.Context) error {
	if b.joiner == nil {
		return nil
	}
	_, err := b.joinParent(ctx)
	b.mu.Lock()
	running := b.running
	b.mu.Unlock()
	if running && permanent(err) {
		select {
		case b.refusedJoin <- err:
		default:
		}
	}
	return err
}

// refusedGrant reports whether err is a grant this node refused for a
// protocol version or an extension it does not support, for a missing field,
// or by refusedParent.
func refusedGrant(err error) bool {
	return errors.Is(err, join.ErrUnsupportedGrantVersion) || errors.Is(err, join.ErrExtensionRefused) ||
		errors.Is(err, join.ErrUnusableGrant) || refusedParent(err)
}

// refusedParent reports whether err is a grant this node refused for a Node
// Path or a node above it may not take.
func refusedParent(err error) bool {
	return errors.Is(err, join.ErrHasChildren) || errors.Is(err, join.ErrDifferentParent) ||
		errors.Is(err, join.ErrPathDisagreement)
}

// permanent reports whether err ends the joining of this node permanently: a
// permanent denial, or a grant this node refused.
func permanent(err error) bool {
	return deniedPermanently(err) || refusedGrant(err)
}

// deniedPermanently reports whether err is a denial the joiner does not retry,
// such as ERR_JOIN_REVOKED. The denial stands until an operator acts.
func deniedPermanently(err error) bool {
	var denial *join.DenialError
	return errors.As(err, &denial)
}

// joinParent is Join on a node that has a joiner. It also returns the
// membership of the grant.
func (b *Binding) joinParent(ctx context.Context) (join.Membership, error) {
	if refusal := b.permanentRefusal(); refusal != nil {
		return join.Membership{}, fmt.Errorf("join %s: %w", b.cfg.ParentAddr, refusal)
	}
	held, err := b.joiner.Join(ctx)
	if err != nil {
		b.denied(ctx, err)
		return join.Membership{}, fmt.Errorf("join %s: %w", b.cfg.ParentAddr, err)
	}
	return held, nil
}

// Start starts the embedded NATS server, this node's connections to it, the
// join endpoint and the goroutines that read messages. The node needs a Node
// Path first, so a node that has never joined calls Join before Start.
func (b *Binding) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return ErrAlreadyStarted
	}
	path, refused := b.path, b.pathRefused
	b.closing = false
	b.mu.Unlock()
	if path == "" {
		return ErrNoNodePath
	}
	if refused != nil {
		return refused
	}
	runCtx, cancel := context.WithCancel(ctx)
	if err := b.bringUp(runCtx, path); err != nil {
		cancel()
		b.teardown()
		return err
	}
	b.mu.Lock()
	b.cancel = cancel
	b.running = true
	b.mu.Unlock()
	b.spawn(func() { b.watchLeaves(runCtx) })
	b.spawn(func() { b.watchHeartbeats(runCtx) })
	if b.askFirst != nil {
		b.spawn(func() { b.watchPending(runCtx) })
	}
	if b.joiner != nil {
		b.spawn(func() { b.sendHeartbeats(runCtx) })
		b.spawn(func() { b.pullRPC(runCtx) })
	}
	b.report()
	return nil
}

// Run calls Join and Start. It then sends the join request again before the
// connection credentials expire, until the context ends. The protocol has no
// separate renewal message: the answer to a repeated join request has new
// credentials.
//
// A node joins once before Start, so its leafnode connection opens with the
// credentials of that grant and no grant closes it. A node that restarts with
// a saved Node Path waits for that join at most savedPathJoinWait. Then it
// starts on its saved credentials and joins again, so it runs while the node
// above cannot be reached, and still learns of a revocation or a new Node
// Path.
func (b *Binding) Run(ctx context.Context) error {
	if b.joiner == nil {
		if err := b.Start(ctx); err != nil {
			return err
		}
		defer func() { _ = b.Close() }() // Close always returns nil.
		<-ctx.Done()
		return ctx.Err()
	}
	if refusal := b.permanentRefusal(); refusal != nil {
		return b.serveBelowOnly(ctx, refusal)
	}
	keepCurrent, err := b.joinBeforeStart(ctx)
	if permanent(err) {
		return b.serveBelowOnly(ctx, err)
	}
	if err != nil {
		return err
	}
	if err = b.Start(ctx); err != nil {
		return err
	}
	defer func() { _ = b.Close() }() // Close always returns nil.
	err = b.keepCurrentUnlessRefused(ctx, keepCurrent)
	switch {
	case err == nil || errors.Is(err, context.Canceled):
		return nil
	case permanent(err):
		// The node keeps its links to the nodes below and drops only the one
		// above, which it may not ask again.
		b.stopUplink(ctx)
		return b.waitRefused(ctx, err)
	default:
		return fmt.Errorf("keep the connection credentials current: %w", err)
	}
}

// serveBelowOnly starts a node that the node above denied permanently, or that
// refused a grant, before it started. The NATS server opens no leafnode
// connection to the node above, so the node asks it nothing, and it serves the
// nodes below until ctx ends. A node that never joined has no Node Path, so it
// starts no server and only waits.
func (b *Binding) serveBelowOnly(ctx context.Context, denial error) error {
	if b.Path() == "" {
		return b.waitRefused(ctx, denial)
	}
	if err := b.Start(ctx); err != nil {
		return err
	}
	defer func() { _ = b.Close() }() // Close always returns nil.
	return b.waitRefused(ctx, denial)
}

// keepCurrentUnlessRefused runs keepCurrent until it ends, or until a Join
// from outside Run refuses a grant. Then it stops the renewals and returns the
// refusal. It records the error of keepCurrent as Join records its own.
func (b *Binding) keepCurrentUnlessRefused(ctx context.Context, keepCurrent func(context.Context) error) error {
	keepCtx, stop := context.WithCancel(ctx)
	defer stop()
	renewed := make(chan error, 1)
	go func() { renewed <- keepCurrent(keepCtx) }()
	select {
	case err := <-renewed:
		b.denied(ctx, err)
		return err
	case err := <-b.refusedJoin:
		stop()
		<-renewed
		return err
	}
}

// waitRefused logs that this node asks the node above nothing more, and waits
// until ctx ends. Such a node opens no connection above, logs the error and
// does not retry. A process that ends is started again, so it waits.
func (b *Binding) waitRefused(ctx context.Context, err error) error {
	b.log.ErrorContext(ctx, refusedLog, "address", b.cfg.ParentAddr, "error", err)
	<-ctx.Done()
	return ctx.Err()
}

// savedPathJoinWait is how long a node with a saved Node Path waits for its
// first join before it starts. A node above that is up answers within it,
// unless many nodes restart at once and its join rate makes them wait. Such a
// node starts anyway, and the grant it gets later closes its link once.
const savedPathJoinWait = 5 * time.Second

// joinBeforeStart joins the node above and returns what keeps the credentials
// current after Start. A node with a saved Node Path that gets no answer
// within savedPathJoinWait starts anyway and joins again at once.
func (b *Binding) joinBeforeStart(ctx context.Context) (func(context.Context) error, error) {
	joinCtx := ctx
	if b.Path() != "" {
		var cancel context.CancelFunc
		joinCtx, cancel = context.WithTimeout(ctx, savedPathJoinWait)
		defer cancel()
	}
	held, err := b.joinParent(joinCtx)
	switch {
	case err == nil:
		return func(ctx context.Context) error { return b.joiner.Renew(ctx, held) }, nil
	case ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded):
		return b.joiner.Run, nil
	default:
		return nil, err
	}
}

// Close stops the goroutines, waits for them, closes the connections and shuts
// the NATS server down. A second call does nothing. It always returns nil.
func (b *Binding) Close() error {
	b.mu.Lock()
	cancel, running := b.cancel, b.running
	b.running = false
	b.closing = running
	b.mu.Unlock()
	if !running {
		return nil
	}
	cancel()
	b.wg.Wait()
	b.teardown()
	return nil
}

// Status returns the state of the connection to the node above and of every
// node below. It reads the binding's fields in memory and never waits for the
// network, because its caller in sdk/linkapi has a short deadline.
func (b *Binding) Status() Status {
	b.mu.Lock()
	s := Status{
		NodeID:    b.cfg.NodeID,
		NodePath:  b.path,
		Identity:  b.ident,
		Accepting: b.cfg.AcceptsChildren,
		AsksFirst: b.cfg.AskFirst,
		Mode:      b.cfg.Verification,
		Uplink:    b.uplink,
	}
	srv := b.srv
	links := make([]*link, 0, len(b.links))
	for _, l := range b.links {
		links = append(links, l)
	}
	b.mu.Unlock()
	if srv != nil {
		s.Listeners = listeners(srv)
	}
	for _, l := range links {
		s.Nodes = append(s.Nodes, l.snapshot())
	}
	if b.askFirst != nil {
		for _, r := range b.askFirst.Pending() {
			if !slices.ContainsFunc(s.Nodes, func(n Below) bool { return n.ID == r.NodeID }) {
				s.Nodes = append(s.Nodes, Below{
					ID:        r.NodeID,
					Identity:  r.Identity,
					Admission: AdmissionPending,
				})
			}
		}
	}
	slices.SortFunc(s.Nodes, func(x, y Below) int { return compareNodeID(x.ID, y.ID) })
	return s
}

// Approve records an operator's approval of the waiting join request for this
// Node ID. The node's next join request is then granted. It returns an error
// when Config.AskFirst is not set, and ErrUnknownNode when no join request
// for id is waiting.
//
// The approval is for one public key: the key that asked for this Node ID
// first. The log line names that key, so the operator can read back which
// key he admitted.
func (b *Binding) Approve(ctx context.Context, id fractal.NodeID) error {
	if b.askFirst == nil {
		return fmt.Errorf("approve %s: this node does not wait for an operator before it grants a join", id)
	}
	if err := b.askFirst.Approve(ctx, id); err != nil {
		if errors.Is(err, joinwire.ErrNoPending) {
			return fmt.Errorf("%w: %s", ErrUnknownNode, id)
		}
		return err
	}
	var approved fractal.Identity
	if held, ok := b.askFirst.Record(id); ok {
		approved = held.Identity
	}
	b.log.InfoContext(ctx, "an operator approved a node", "node", id, "identity", approved)
	b.report()
	return nil
}

// Reject removes the waiting join request for this Node ID, whether an
// operator approved it or not. The Node ID is then free, so a machine that
// took a name this node's operator wanted for another machine can be cleared,
// and another machine can ask for that name. It returns an error when
// Config.AskFirst is not set, and ErrUnknownNode when no join request for id
// is waiting.
//
// A node that has already joined is removed with Revoke, not here.
func (b *Binding) Reject(ctx context.Context, id fractal.NodeID) error {
	if b.askFirst == nil {
		return fmt.Errorf("reject %s: this node does not wait for an operator before it grants a join", id)
	}
	var rejected fractal.Identity
	if held, ok := b.askFirst.Record(id); ok {
		rejected = held.Identity
	}
	if err := b.askFirst.Forget(ctx, id); err != nil {
		if errors.Is(err, joinwire.ErrNoPending) {
			return fmt.Errorf("%w: %s", ErrUnknownNode, id)
		}
		return err
	}
	b.log.InfoContext(ctx, "an operator rejected a node", "node", id, "identity", rejected)
	b.report()
	return nil
}

// Revoke removes a node below this one. Its connection credentials stop
// working at once, also on the connection that is already open, and its next
// join request is denied.
func (b *Binding) Revoke(ctx context.Context, id fractal.NodeID) error {
	if b.parent == nil {
		return fmt.Errorf("revoke %s: this node does not accept joins", id)
	}
	if err := b.parent.Revoke(ctx, id); err != nil {
		if errors.Is(err, join.ErrUnknownChild) {
			return fmt.Errorf("%w: %s", ErrUnknownNode, id)
		}
		return err
	}
	b.report()
	return nil
}

// bringUp creates the accounts, starts the NATS server, connects to it,
// creates this node's streams, and on a node that accepts joins restores the
// links and opens the join endpoint.
func (b *Binding) bringUp(ctx context.Context, path fractal.NodePath) error {
	if err := b.accounts.mint(accountSystem, accountSpec{}); err != nil {
		return fmt.Errorf("mint the system account: %w", err)
	}
	if err := b.accounts.mint(accountLocal, accountSpec{jetStream: true}); err != nil {
		return fmt.Errorf("mint this node's own account: %w", err)
	}
	sentinel, err := b.mintBootstrap()
	if err != nil {
		return err
	}
	b.mu.Lock()
	leafRemote, heartbeat := b.leafRemot, b.heartbeat
	if b.permanent != nil {
		leafRemote = ""
	}
	b.mu.Unlock()
	srv, srvOpts, err := startServer(ctx, serverParams{
		cfg:        b.cfg,
		path:       path,
		accounts:   b.accounts,
		sentinel:   sentinel,
		leafRemote: leafRemote,
		signLeaf:   b.signLeafNonce,
		anchors:    b.anchors,
	})
	if err != nil {
		return err
	}
	b.mu.Lock()
	b.srv, b.srvOpts = srv, srvOpts
	b.heartbeat = heartbeat
	b.mu.Unlock()

	if connErr := b.prusaConnect(); connErr != nil {
		return connErr
	}
	if eventsErr := b.subscribeLeafEvents(); eventsErr != nil {
		return eventsErr
	}
	if provErr := b.provisionSelf(ctx, path); provErr != nil {
		return provErr
	}
	if b.parent != nil {
		if pathErr := b.parent.SetPath(path); pathErr != nil {
			return fmt.Errorf("give the join endpoint this node's path: %w", pathErr)
		}
		if restoreErr := b.restoreLinks(ctx); restoreErr != nil {
			return restoreErr
		}
		if endpointErr := b.startJoinEndpoint(ctx); endpointErr != nil {
			return endpointErr
		}
	}
	return nil
}

// teardown closes the links, then the connections, then the NATS server.
func (b *Binding) teardown() {
	b.mu.Lock()
	links := make([]*link, 0, len(b.links))
	for _, l := range b.links {
		links = append(links, l)
	}
	local, bootstrap, events, srv := b.local, b.bootstrap, b.events, b.srv
	b.local, b.bootstrap, b.events, b.localJS, b.srv, b.srvOpts = nil, nil, nil, nil, nil, nil
	b.mu.Unlock()
	for _, l := range links {
		l.close()
	}
	for _, conn := range []*nats.Conn{events, bootstrap, local} {
		if conn != nil {
			conn.Close()
		}
	}
	if srv != nil {
		srv.Shutdown()
		srv.WaitForShutdown()
	}
}

// spawn starts a goroutine that Close waits for. Once Close has begun it
// starts none, so no goroutine starts while Close waits for the others.
func (b *Binding) spawn(f func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closing {
		return
	}
	b.wg.Go(f)
}

// report calls Config.OnChange with the current Status.
func (b *Binding) report() {
	if b.cfg.OnChange == nil {
		return
	}
	b.cfg.OnChange(b.Status())
}

// anchorFiles returns the CA certificate files, or nil when TLS is off.
func anchorFiles(m *TLSMaterial) []string {
	if m == nil {
		return nil
	}
	return m.CAFiles
}

// compareNodeID compares two Node IDs as text, so every listing has the same
// order.
func compareNodeID(x, y fractal.NodeID) int {
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}
