package shadow

import (
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// afterSlash is "0", the ASCII character directly after "/". Every path below
// "hq/a" sorts at or after "hq/a/" and before "hq/a0".
const afterSlash = "0"

// nodeEntry is one node of a subtree: its path and its shadow interfaces.
type nodeEntry struct {
	shadow *nodeShadow
	path   fractal.NodePath
}

// subtreeNodes returns the node at path and every node below it, sorted by
// path. The caller must hold a lock of the Store.
func (s *Store) subtreeNodes(path fractal.NodePath) []nodeEntry {
	var out []nodeEntry
	if node, ok := s.nodes[path]; ok {
		out = append(out, nodeEntry{shadow: node, path: path})
	}
	lo, _ := slices.BinarySearch(s.order, path+"/")
	hi, _ := slices.BinarySearch(s.order, path+afterSlash)
	for _, below := range s.order[lo:hi] {
		out = append(out, nodeEntry{shadow: s.nodes[below], path: below})
	}
	return out
}

// MarkStale sets to STALE every OK interface of the node at path and of every
// node below it. An EMPTY interface stays EMPTY. It returns the sorted keys
// of the interfaces it changed. Call it when the link to a node directly
// below is lost. For a LOS message, call ApplyLOS.
func (s *Store) MarkStale(path fractal.NodePath) []Key {
	var changed []Key
	var events []Event
	watching := s.watching()
	s.mu.RLock()
	for _, entry := range s.subtreeNodes(path) {
		changed, events = entry.shadow.markStale(entry.path, changed, events, watching)
	}
	s.mu.RUnlock()
	sortKeys(changed)
	s.publishEvents(events)
	return changed
}

// MarkStaleAbove sets to STALE every OK interface of every node above path. It
// leaves the node at path alone, and an EMPTY interface stays EMPTY. It
// returns the sorted keys of the interfaces it changed. Call it on the
// ancestor shadow, with the node's own path when it loses the node above it,
// and with the sender of an ALOS.
func (s *Store) MarkStaleAbove(path fractal.NodePath) []Key {
	var changed []Key
	var events []Event
	watching := s.watching()
	s.mu.RLock()
	for above, ok := path.Parent(); ok; above, ok = above.Parent() {
		if node, held := s.nodes[above]; held {
			changed, events = node.markStale(above, changed, events, watching)
		}
	}
	s.mu.RUnlock()
	sortKeys(changed)
	s.publishEvents(events)
	return changed
}

// markStale sets every OK interface of the node at path to STALE. It appends
// the key of each one to changed, and its Event to events when watching.
func (n *nodeShadow) markStale(path fractal.NodePath, changed []Key, events []Event, watching bool) ([]Key, []Event) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for ik, iface := range n.ifaces {
		if iface.status == StatusStale || !Allowed(iface.status, StatusStale) {
			continue
		}
		iface.status = StatusStale
		key := Key{Node: path, Interface: ik.name, Instance: ik.instance}
		changed = append(changed, key)
		if watching {
			events = append(events, Event{Key: key, Status: StatusStale, UpdatedAt: iface.updatedAt})
		}
	}
	return changed, events
}

// ApplyLOS calls MarkStale for the node a LOS message reports as lost, which
// is fractal.LOS.Affected. It returns an error, and changes nothing, when
// that is not a valid Node Path.
func (s *Store) ApplyLOS(m fractal.LOS) ([]Key, error) {
	affected, err := m.Affected()
	if err != nil {
		return nil, err
	}
	return s.MarkStale(affected), nil
}

// RemoveNode deletes every shadow interface of the node at path and of every
// node below it. It returns the sorted keys of the interfaces it deleted.
// Call it for a node that has left the tree for good, such as a revoked one.
func (s *Store) RemoveNode(path fractal.NodePath) []Key {
	var removed []Key
	var events []Event
	watching := s.watching()
	s.mu.Lock()
	for _, entry := range s.subtreeNodes(path) {
		removed, events = s.detach(entry, removed, events, watching)
		delete(s.nodes, entry.path)
		if at, found := slices.BinarySearch(s.order, entry.path); found {
			s.order = slices.Delete(s.order, at, at+1)
		}
	}
	s.mu.Unlock()
	sortKeys(removed)
	s.publishEvents(events)
	return removed
}

// Rebase makes the Store the ancestor shadow of a node at path. Call it each
// time the node takes a Node Path. It deletes every interface, then creates,
// with StatusEmpty, the interfaces in Selection.Interfaces whose node is
// above path. From then on ApplyREP creates no interface of a node that is
// not above path. It returns the sorted keys of the interfaces it deleted.
//
// With an empty path it deletes everything and creates nothing.
func (s *Store) Rebase(path fractal.NodePath) []Key {
	var removed []Key
	var events []Event
	watching := s.watching()
	s.mu.Lock()
	for _, at := range s.order {
		removed, events = s.detach(nodeEntry{shadow: s.nodes[at], path: at}, removed, events, watching)
	}
	clear(s.nodes)
	s.order = nil
	s.anchor, s.anchored = path, true
	s.seed(func(node fractal.NodePath) bool { return node.IsAncestorOf(path) })
	s.mu.Unlock()
	sortKeys(removed)
	s.publishEvents(events)
	return removed
}

// detach empties one node and collects the keys and the Removed events. The
// caller must hold the write lock and take the node out of s.nodes and s.order.
func (s *Store) detach(entry nodeEntry, removed []Key, events []Event, watching bool) ([]Key, []Event) {
	entry.shadow.mu.Lock()
	defer entry.shadow.mu.Unlock()
	for ik, iface := range entry.shadow.ifaces {
		key := Key{Node: entry.path, Interface: ik.name, Instance: ik.instance}
		removed = append(removed, key)
		if watching {
			events = append(events, Event{Key: key, Status: iface.status, UpdatedAt: iface.updatedAt, Removed: true})
		}
	}
	s.count -= len(entry.shadow.ifaces)
	entry.shadow.ifaces = nil
	return removed, events
}

// sortKeys sorts keys by node path, then by interface name, then by instance
// id.
func sortKeys(keys []Key) {
	slices.SortFunc(keys, func(a, b Key) int {
		switch {
		case a.Node != b.Node:
			return strings.Compare(string(a.Node), string(b.Node))
		case a.Interface != b.Interface:
			return strings.Compare(string(a.Interface), string(b.Interface))
		default:
			return strings.Compare(string(a.Instance), string(b.Instance))
		}
	})
}
