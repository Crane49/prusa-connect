package fractaltest

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// FabricOptions configures a Fabric. Every field has a default.
type FabricOptions struct {
	// Clock gives the time that every link measures its deduplication window
	// with. It defaults to node.SystemClock.
	Clock node.Clock
	// DedupWindow is how long each link remembers the id of a durable message.
	// A link delivers a second durable message with the same id only after
	// that time. Zero or less means DefaultDedupWindow.
	DedupWindow time.Duration
}

// Fabric connects several nodes in one process into a tree. A test uses it in
// this order:
//
//   - Link builds each link of the tree
//   - Transport returns the node.Binding for one Node Path, and the test
//     builds the node at that path with it
//   - Attach gives the fabric the engine of each node, so the links can
//     deliver to it
//   - Start runs the links, and ConnectAll brings them up
type Fabric struct {
	clock      node.Clock
	transports map[fractal.NodePath]*Transport
	engines    map[fractal.NodePath]Engine
	links      map[fractal.NodePath]*Link
	cancel     context.CancelFunc
	window     time.Duration
	wg         sync.WaitGroup
	mu         sync.Mutex
	started    bool
}

// NewFabric builds an empty fabric.
func NewFabric(opts FabricOptions) *Fabric {
	clock := opts.Clock
	if clock == nil {
		clock = node.SystemClock{}
	}
	window := opts.DedupWindow
	if window <= 0 {
		window = DefaultDedupWindow
	}
	return &Fabric{
		clock:      clock,
		transports: make(map[fractal.NodePath]*Transport),
		engines:    make(map[fractal.NodePath]Engine),
		links:      make(map[fractal.NodePath]*Link),
		window:     window,
	}
}

// Transport returns the transport of the node at path. The first call for a
// path builds it. Pass it to the node at that path as its node.Binding.
func (f *Fabric) Transport(path fractal.NodePath) *Transport {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.transportLocked(path)
}

// Attach sets e as the engine of the node at path. The link above that node
// and every link below it then deliver their messages to e. This covers links
// that exist already and links that Link builds later.
func (f *Fabric) Attach(path fractal.NodePath, e Engine) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.engines[path] = e
	if link, ok := f.links[path]; ok {
		link.Attach(nil, e)
	}
	for childPath, link := range f.links {
		if parent, ok := childPath.Parent(); ok && parent == path {
			link.Attach(e, nil)
		}
	}
}

// Link builds the link between the node at the Node Path parent and the node
// with the Node ID child below it. It adds the link to the Transport of each
// of the two nodes. A second call for the same two nodes returns the existing
// link. Link returns ErrFabricStarted for a new link after Start.
func (f *Fabric) Link(parent fractal.NodePath, child fractal.NodeID) (*Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	childPath, err := parent.Child(child)
	if err != nil {
		return nil, err
	}
	if link, ok := f.links[childPath]; ok {
		return link, nil
	}
	if f.started {
		return nil, ErrFabricStarted
	}
	link, err := NewLink(LinkConfig{
		Above:       f.engines[parent],
		Below:       f.engines[childPath],
		Clock:       f.clock,
		Parent:      parent,
		Child:       child,
		DedupWindow: f.window,
	})
	if err != nil {
		return nil, err
	}
	f.links[childPath] = link
	f.transportLocked(parent).addChild(child, link)
	f.transportLocked(childPath).setParent(link)
	return link, nil
}

// LinkTo returns the link between the node at path and the node above it. The
// second result is false when the fabric has no such link, as for a root node.
func (f *Fabric) LinkTo(path fractal.NodePath) (*Link, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	link, ok := f.links[path]
	return link, ok
}

// Links returns every link, sorted by the Node Path of the node below.
func (f *Fabric) Links() []*Link {
	f.mu.Lock()
	defer f.mu.Unlock()
	paths := make([]fractal.NodePath, 0, len(f.links))
	for path := range f.links {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	links := make([]*Link, 0, len(paths))
	for _, path := range paths {
		links = append(links, f.links[path])
	}
	return links
}

// Start starts the goroutines of every link. They run until ctx ends or Close
// is called. A second call does nothing.
func (f *Fabric) Start(ctx context.Context) {
	f.mu.Lock()
	if f.started {
		f.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	f.cancel = cancel
	f.started = true
	links := make([]*Link, 0, len(f.links))
	for _, link := range f.links {
		links = append(links, link)
	}
	f.mu.Unlock()
	for _, link := range links {
		link.Start(runCtx, &f.wg)
	}
}

// ConnectAll brings every link up, from the root down. The link above a node
// is up before any link below that node.
func (f *Fabric) ConnectAll(ctx context.Context) {
	for _, link := range f.Links() {
		link.Connect(ctx)
	}
}

// Close stops every link and waits until their goroutines end.
func (f *Fabric) Close() {
	f.mu.Lock()
	cancel, started := f.cancel, f.started
	f.started = false
	f.mu.Unlock()
	if !started {
		return
	}
	cancel()
	f.wg.Wait()
}

// transportLocked returns the transport of one path and builds it on the first
// call. The caller holds f.mu.
func (f *Fabric) transportLocked(path fractal.NodePath) *Transport {
	if t, ok := f.transports[path]; ok {
		return t
	}
	t := &Transport{path: path, down: make(map[fractal.NodeID]*Link)}
	f.transports[path] = t
	return t
}
