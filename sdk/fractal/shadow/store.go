package shadow

import (
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// ErrBadSelection means New got a Selection with a Key or a pattern that is
// not valid.
var ErrBadSelection = errors.New("selection is not valid")

// Config is the input of New.
type Config struct {
	// Selection says which interfaces the Store keeps. An empty Selection is
	// valid, and the Store then keeps nothing.
	Selection Selection
}

// ifaceKey identifies an interface on one node.
type ifaceKey struct {
	name     fractal.InterfaceName
	instance fractal.InstanceID
}

// shadowIface is one shadow interface. props has the raw property values.
// updatedAt is the timestamp of the last REP applied.
type shadowIface struct {
	props     fractal.Props
	updatedAt fractal.Timestamp
	status    Status
}

// nodeShadow has the shadow interfaces of one node. Each node has its own
// lock, so a REP for one node does not wait for a REP for another node.
type nodeShadow struct {
	ifaces map[ifaceKey]*shadowIface
	mu     sync.Mutex
}

// Store is the shadow of one node: a copy of the property values of the
// interfaces the node selected, on the nodes below it, or on the nodes above
// it for the ancestor shadow. It is safe for concurrent use. REP messages
// from different nodes do not wait for each other.
type Store struct {
	enumerated map[Key]struct{}
	nodes      map[fractal.NodePath]*nodeShadow
	subs       []*Subscription
	order      []fractal.NodePath
	// anchor is the Node Path the last Rebase got. While anchored is true,
	// applyNew creates only a node above it.
	anchor    fractal.NodePath
	anchored  bool
	selection Selection
	count     int
	mu        sync.RWMutex
	subsMu    sync.RWMutex
	subCount  atomic.Int64
}

// New builds a Store from cfg. It creates, with StatusEmpty, every interface
// in Selection.Interfaces that names an instance. It returns an error that
// wraps ErrBadSelection when a Key or a pattern in the Selection is not
// valid.
func New(cfg Config) (*Store, error) {
	if err := cfg.Selection.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadSelection, err)
	}
	s := &Store{
		enumerated: make(map[Key]struct{}, len(cfg.Selection.Interfaces)),
		nodes:      make(map[fractal.NodePath]*nodeShadow),
		selection:  cfg.Selection.clone(),
	}
	for _, k := range s.selection.Interfaces {
		s.enumerated[k] = struct{}{}
	}
	s.seed(func(fractal.NodePath) bool { return true })
	return s, nil
}

// seed creates, with StatusEmpty, every interface in s.enumerated that names
// an instance and whose node keep accepts. The caller must hold the write lock.
func (s *Store) seed(keep func(fractal.NodePath) bool) {
	for k := range s.enumerated {
		if k.Instance == "" || !keep(k.Node) {
			continue
		}
		node := s.node(k.Node)
		node.ifaces[ifaceKey{k.Interface, k.Instance}] = &shadowIface{status: StatusEmpty}
		s.count++
	}
}

// Selection returns a copy of the Selection the Store was built with.
func (s *Store) Selection() Selection { return s.selection.clone() }

// Selects reports whether the Selection of the Store covers k. It does when k
// is in Selection.Interfaces, when Selection.Interfaces names k.Node and
// k.Interface with no instance id, or when a pattern matches k.Interface.
func (s *Store) Selects(k Key) bool {
	if _, ok := s.enumerated[k]; ok {
		return true
	}
	if _, ok := s.enumerated[Key{Node: k.Node, Interface: k.Interface}]; ok {
		return true
	}
	for _, p := range s.selection.Patterns {
		if p.Match(k.Interface) {
			return true
		}
	}
	return false
}

// Len returns the number of shadow interfaces in the Store.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.count
}

// Status returns the status of one shadow interface, and the timestamp of the
// last REP applied to it. updatedAt is zero for an interface that is EMPTY. ok
// is false when the Store does not have the interface.
func (s *Store) Status(k Key) (status Status, updatedAt fractal.Timestamp, ok bool) {
	s.mu.RLock()
	node := s.nodes[k.Node]
	s.mu.RUnlock()
	if node == nil {
		return 0, 0, false
	}
	node.mu.Lock()
	defer node.mu.Unlock()
	iface, found := node.ifaces[ifaceKey{k.Interface, k.Instance}]
	if !found {
		return 0, 0, false
	}
	return iface.status, iface.updatedAt, true
}

// Snapshot is a copy of one shadow interface, as Get returns it. Props is a
// new map that the caller may change. The byte slices in it are shared with
// the Store and every other reader, so the caller must not change their
// bytes.
type Snapshot struct {
	Key       Key
	Props     fractal.Props
	UpdatedAt fractal.Timestamp
	Status    Status
}

// Get returns a Snapshot of one shadow interface. ok is false when the Store
// does not have it.
func (s *Store) Get(k Key) (snap Snapshot, ok bool) {
	s.mu.RLock()
	node := s.nodes[k.Node]
	s.mu.RUnlock()
	if node == nil {
		return snap, false
	}
	node.mu.Lock()
	defer node.mu.Unlock()
	iface, found := node.ifaces[ifaceKey{k.Interface, k.Instance}]
	if !found {
		return snap, false
	}
	snap = Snapshot{Key: k, UpdatedAt: iface.updatedAt, Status: iface.status}
	if len(iface.props) > 0 {
		snap.Props = make(fractal.Props, len(iface.props))
		maps.Copy(snap.Props, iface.props)
	}
	return snap, true
}

// Prop returns the raw value of one property of one shadow interface. The
// slice is shared with the Store, so the caller must not change its bytes. ok
// is false when the Store does not have the interface or the property.
func (s *Store) Prop(k Key, name fractal.PropertyName) (value json.RawMessage, ok bool) {
	s.mu.RLock()
	node := s.nodes[k.Node]
	s.mu.RUnlock()
	if node == nil {
		return nil, false
	}
	node.mu.Lock()
	defer node.mu.Unlock()
	iface, found := node.ifaces[ifaceKey{k.Interface, k.Instance}]
	if !found {
		return nil, false
	}
	value, ok = iface.props[name]
	return value, ok
}

// Interfaces returns the keys of the shadow interfaces of one node, sorted by
// interface name and then by instance id. The keys are collected before it
// returns, so the loop body may call the Store.
func (s *Store) Interfaces(path fractal.NodePath) iter.Seq[Key] {
	s.mu.RLock()
	node := s.nodes[path]
	s.mu.RUnlock()
	keys := nodeKeys(path, node)
	return slices.Values(keys)
}

// Subtree returns the keys of the shadow interfaces of the node at path and of
// every node below it, sorted by node path, then by interface name, then by
// instance id. The loop body may call the Store.
func (s *Store) Subtree(path fractal.NodePath) iter.Seq[Key] {
	s.mu.RLock()
	var keys []Key
	for _, node := range s.subtreeNodes(path) {
		keys = append(keys, nodeKeys(node.path, node.shadow)...)
	}
	s.mu.RUnlock()
	return slices.Values(keys)
}

// Nodes returns the Node Path of every node the Store has an interface of,
// sorted.
func (s *Store) Nodes() iter.Seq[fractal.NodePath] {
	s.mu.RLock()
	paths := slices.Clone(s.order)
	s.mu.RUnlock()
	return slices.Values(paths)
}

// nodeKeys returns the sorted keys of the interfaces of one node.
func nodeKeys(path fractal.NodePath, node *nodeShadow) []Key {
	if node == nil {
		return nil
	}
	node.mu.Lock()
	keys := make([]Key, 0, len(node.ifaces))
	for ik := range node.ifaces {
		keys = append(keys, Key{Node: path, Interface: ik.name, Instance: ik.instance})
	}
	node.mu.Unlock()
	sortKeys(keys)
	return keys
}

// node returns the nodeShadow of path and creates it when the Store has none.
// It keeps s.order sorted. The caller must hold the write lock.
func (s *Store) node(path fractal.NodePath) *nodeShadow {
	if node, ok := s.nodes[path]; ok {
		return node
	}
	node := &nodeShadow{ifaces: make(map[ifaceKey]*shadowIface)}
	s.nodes[path] = node
	at, _ := slices.BinarySearch(s.order, path)
	s.order = slices.Insert(s.order, at, path)
	return node
}
