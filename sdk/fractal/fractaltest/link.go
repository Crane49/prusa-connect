package fractaltest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// DefaultDedupWindow is how long a link remembers the id of a durable message
// when LinkConfig leaves the window at zero.
const DefaultDedupWindow = 5 * time.Minute

// pipesPerLink is the number of goroutines one link runs: one for each
// direction.
const pipesPerLink = 2

// The errors of the in-memory transport.
var (
	// ErrNoLink is the error a Transport returns from a send when it has no
	// link to the node the message is for. It wraps node.ErrNoSuchLink. The
	// engine then counts the message as one it could not route, and it answers
	// a method invocation with fractal.CodeNodeNotFound.
	ErrNoLink = fmt.Errorf("no link to that node: %w", node.ErrNoSuchLink)
	// ErrFabricStarted is the error Fabric.Link returns for a new link after
	// Fabric.Start. Start starts the goroutines of the links that exist, so a
	// link built later would have none.
	ErrFabricStarted = errors.New("the fabric is already running")
)

// LinkConfig configures one link between two nodes: the node above and the
// node below it.
type LinkConfig struct {
	// Above is the engine of the node above. The link delivers to it every
	// message the node below sends, and the connect and loss signals. It may
	// be set later with Attach.
	Above Engine
	// Below is the engine of the node below. The link delivers to it every
	// message the node above sends, and the signals of its link to the node
	// above. It may be set later with Attach.
	Below Engine
	// Clock gives the time that the link measures its deduplication window
	// with. It defaults to node.SystemClock.
	Clock node.Clock
	// Parent is the Node Path of the node above.
	Parent fractal.NodePath
	// Child is the Node ID of the node below.
	Child fractal.NodeID
	// DedupWindow is how long the link remembers the id of a durable message.
	// Zero or less means DefaultDedupWindow.
	DedupWindow time.Duration
}

// Link is the in-memory link between a node and one node below it. It has a
// message queue for each direction. When it comes up and goes down, it calls
// ChildConnected and ChildLost on the engine of the node above, and
// ParentConnected and ParentLost on the engine of the node below.
type Link struct {
	above     Engine
	below     Engine
	down      *pipe
	up        *pipe
	parent    fractal.NodePath
	childPath fractal.NodePath
	child     fractal.NodeID
	mu        sync.Mutex
	connected bool
	ever      bool
}

// NewLink builds one link. The link delivers nothing until Start and Connect
// are called. NewLink returns an error when cfg.Parent and cfg.Child do not
// make a valid Node Path.
func NewLink(cfg LinkConfig) (*Link, error) {
	childPath, err := cfg.Parent.Child(cfg.Child)
	if err != nil {
		return nil, fmt.Errorf("build the Node Path of the node below: %w", err)
	}
	clock := cfg.Clock
	if clock == nil {
		clock = node.SystemClock{}
	}
	window := cfg.DedupWindow
	if window <= 0 {
		window = DefaultDedupWindow
	}
	l := &Link{
		above:     cfg.Above,
		below:     cfg.Below,
		down:      newPipe("", clock, window),
		up:        newPipe(cfg.Child, clock, window),
		parent:    cfg.Parent,
		childPath: childPath,
		child:     cfg.Child,
	}
	l.down.attach(cfg.Below)
	l.up.attach(cfg.Above)
	return l, nil
}

// Parent returns the Node Path of the node above.
func (l *Link) Parent() fractal.NodePath { return l.parent }

// Child returns the Node ID of the node below.
func (l *Link) Child() fractal.NodeID { return l.child }

// ChildPath returns the Node Path of the node below.
func (l *Link) ChildPath() fractal.NodePath { return l.childPath }

// Attach sets the engine of the node above and the engine of the node below.
// A nil argument keeps the engine that end already has.
func (l *Link) Attach(above, below Engine) {
	l.mu.Lock()
	if above != nil {
		l.above = above
		l.up.attach(above)
	}
	if below != nil {
		l.below = below
	}
	l.mu.Unlock()
	if below != nil {
		l.down.attach(below)
	}
}

// Start starts one goroutine for each direction of the link. They deliver the
// queued messages until ctx ends. Start adds both goroutines to wg, so the
// caller can wait for them.
func (l *Link) Start(ctx context.Context, wg *sync.WaitGroup) {
	wg.Add(pipesPerLink)
	go func() {
		defer wg.Done()
		l.down.pump(ctx)
	}()
	go func() {
		defer wg.Done()
		l.up.pump(ctx)
	}()
}

// Connect brings the link up, calls ChildConnected on the engine of the node
// above and ParentConnected on the engine of the node below. A node.Node above
// then asks the node below for a full state report, and a node.Node below asks
// the nodes above for theirs (fractal.md, Synchronization of the Shadows -
// Reaction to Connection Loss and Reconnection).
func (l *Link) Connect(ctx context.Context) {
	l.mu.Lock()
	l.connected = true
	l.ever = true
	above, below := l.above, l.below
	l.mu.Unlock()
	l.down.setUp(true)
	l.up.setUp(true)
	if above != nil {
		above.ChildConnected(ctx, l.child)
	}
	if below != nil {
		below.ParentConnected(ctx)
	}
}

// Cut takes the link down, calls ChildLost on the engine of the node above and
// ParentLost on the engine of the node below. A node.Node above then marks as
// stale the state it keeps for the node below and for every node under that
// one, and sends a loss message upward. A node.Node below marks its ancestor
// shadow stale and sends a loss message downward. Stale means the values may
// be out of date.
//
// Cut calls neither for a link that is already down, or for one that never
// connected. Only the transport knows whether a link has been up, so the
// transport applies this rule and not the engine (fractal-nats.md, Where
// liveness monitoring starts).
func (l *Link) Cut(ctx context.Context) {
	l.mu.Lock()
	was, ever, above, below := l.connected, l.ever, l.above, l.below
	l.connected = false
	l.mu.Unlock()
	l.down.setUp(false)
	l.up.setUp(false)
	if !was || !ever {
		return
	}
	if above != nil {
		above.ChildLost(ctx, l.child)
	}
	if below != nil {
		below.ParentLost(ctx)
	}
}

// Restore brings the link up again, as Connect does. The link then delivers
// the durable messages that waited, in the order they were sent. It delivers
// again a durable message that the receiver did not acknowledge before the
// cut.
func (l *Link) Restore(ctx context.Context) { l.Connect(ctx) }

// Connected reports whether the link is up.
func (l *Link) Connected() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.connected
}

// ParentSide returns a new Transport for the node above. This link is its only
// link.
func (l *Link) ParentSide() *Transport {
	return &Transport{path: l.parent, down: map[fractal.NodeID]*Link{l.child: l}}
}

// ChildSide returns a new Transport for the node below. This link is its only
// link.
func (l *Link) ChildSide() *Transport {
	return &Transport{path: l.childPath, up: l}
}

// Upward returns the counters for messages that go to the node above.
func (l *Link) Upward() Stats { return l.up.snapshot() }

// Downward returns the counters for messages that go to the node below.
func (l *Link) Downward() Stats { return l.down.snapshot() }

// Waiting returns the number of messages in the queue of each direction. A
// durable message counts until the receiver acknowledges it.
func (l *Link) Waiting() (upward, downward int) {
	return l.up.waiting(), l.down.waiting()
}

// Transport is the node.Binding of one node. It has the link to the node above
// and the links to the nodes below, and it puts each message in the queue of
// the right link.
type Transport struct {
	up   *Link
	down map[fractal.NodeID]*Link
	path fractal.NodePath
	mu   sync.RWMutex
}

// SendBestEffort sends a message that the link drops when it is down. It
// returns ErrNoLink when the transport has no link for the message.
func (t *Transport) SendBestEffort(_ context.Context, d node.Dispatch) error {
	return t.hand(d, false)
}

// SendDurable sends a message that the link keeps until the receiver
// acknowledges it. It returns when the message is in the queue of the link. A
// message sent while the link is down waits in that queue. It returns
// ErrNoLink when the transport has no link for the message.
func (t *Transport) SendDurable(_ context.Context, d node.Dispatch) error {
	return t.hand(d, true)
}

// Path returns the Node Path of the node this transport belongs to.
func (t *Transport) Path() fractal.NodePath { return t.path }

// hand puts one message in the queue of the link it goes through: the link to
// the node d.Child names for a message that goes down, and the link to the
// node above for every other message.
func (t *Transport) hand(d node.Dispatch, durable bool) error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if d.Down() {
		link, ok := t.down[d.Child]
		if !ok {
			return fmt.Errorf("%s below %s: %w", d.Child, t.path, ErrNoLink)
		}
		link.down.send(d, durable)
		return nil
	}
	if t.up == nil {
		return fmt.Errorf("above %s: %w", t.path, ErrNoLink)
	}
	t.up.up.send(d, durable)
	return nil
}

// addChild adds the link to one node below this node.
func (t *Transport) addChild(id fractal.NodeID, l *Link) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.down == nil {
		t.down = make(map[fractal.NodeID]*Link)
	}
	t.down[id] = l
}

// setParent sets the link to the node above this node.
func (t *Transport) setParent(l *Link) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.up = l
}
