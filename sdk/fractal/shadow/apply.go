package shadow

import (
	"maps"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// Outcome says what ApplyREP did with a REP message.
type Outcome uint8

// The values of Outcome.
const (
	// OutcomeApplied means the Store copied the property values of the REP
	// into the shadow interface and set its status to OK. The node forwards
	// the REP to the node above.
	OutcomeApplied Outcome = iota + 1
	// OutcomeDiscarded means the timestamp of the REP is older than the
	// timestamp of the shadow interface. The Store changed nothing, and the
	// node must not forward the REP.
	OutcomeDiscarded
	// OutcomeNotSelected means the Selection does not cover the interface of
	// the REP, or that Rebase anchored the Store and the sender is not above
	// the anchor. The Store created nothing. The node still forwards the REP.
	OutcomeNotSelected
)

// outcomeNames has the text String returns for each outcome.
var outcomeNames = [...]string{
	OutcomeApplied:     "applied",
	OutcomeDiscarded:   "discarded",
	OutcomeNotSelected: "not selected",
}

// String returns "applied", "discarded" or "not selected", and "unknown" for
// any other value.
func (o Outcome) String() string {
	if int(o) <= 0 || int(o) >= len(outcomeNames) {
		return "unknown"
	}
	return outcomeNames[o]
}

// Result is what ApplyREP returns for one REP message.
type Result struct {
	// Key identifies the shadow interface the REP was for.
	Key Key
	// UpdatedAt is the timestamp of the shadow interface after the call. It is
	// zero when the Store does not have the interface.
	UpdatedAt fractal.Timestamp
	// Outcome says what ApplyREP did.
	Outcome Outcome
	// Previous is the status of the interface before the call. It is
	// StatusEmpty for an interface this call created, and zero when the Store
	// does not have the interface after the call.
	Previous Status
	// Status is the status of the interface after the call, and zero when the
	// Store does not have the interface.
	Status Status
	// Created is true when this call created the shadow interface.
	Created bool
}

// ApplyREP applies one REP message to the Store. It creates an interface the
// Selection covers on its first REP. It discards a REP whose timestamp is
// lower than the timestamp of the interface, and applies an equal one. An
// applied REP replaces the property values it contains, keeps every other
// value, and sets the status to OK.
func (s *Store) ApplyREP(m fractal.REP) Result {
	key := Key{Node: m.Sender, Interface: m.Interface, Instance: m.Instance}
	ik := ifaceKey{m.Interface, m.Instance}

	s.mu.RLock()
	node := s.nodes[key.Node]
	s.mu.RUnlock()
	if node != nil {
		if res, held := s.applyHeld(node, ik, key, m); held {
			s.publishApplied(res)
			return res
		}
	}
	res := s.applyNew(ik, key, m)
	s.publishApplied(res)
	return res
}

// applyHeld applies the REP to an interface the Store already has. held is
// false when the node has no such interface.
func (s *Store) applyHeld(node *nodeShadow, ik ifaceKey, key Key, m fractal.REP) (res Result, held bool) {
	node.mu.Lock()
	defer node.mu.Unlock()
	iface, ok := node.ifaces[ik]
	if !ok {
		return res, false
	}
	return s.update(iface, key, m, false), true
}

// applyNew creates the shadow interface when the Selection covers it, and
// applies the REP to it. It looks for the interface again under the write
// lock, because another goroutine can create it first.
func (s *Store) applyNew(ik ifaceKey, key Key, m fractal.REP) Result {
	if !s.Selects(key) {
		return Result{Key: key, Outcome: OutcomeNotSelected}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Read under the write lock, so a report that waited here while Rebase ran
	// cannot bring back a node of the old path.
	if s.anchored && !key.Node.IsAncestorOf(s.anchor) {
		return Result{Key: key, Outcome: OutcomeNotSelected}
	}
	node := s.nodes[key.Node]
	if node != nil {
		node.mu.Lock()
		defer node.mu.Unlock()
		if iface, ok := node.ifaces[ik]; ok {
			return s.update(iface, key, m, false)
		}
	}
	if node == nil {
		node = s.node(key.Node)
		node.mu.Lock()
		defer node.mu.Unlock()
	}
	iface := &shadowIface{status: StatusEmpty, props: make(fractal.Props, len(m.Props))}
	node.ifaces[ik] = iface
	s.count++
	return s.update(iface, key, m, true)
}

// update discards an older REP, or copies the property values and sets the
// timestamp and the status OK. The caller must hold the lock of the node.
func (s *Store) update(iface *shadowIface, key Key, m fractal.REP, created bool) Result {
	res := Result{
		Key:       key,
		Previous:  iface.status,
		Status:    iface.status,
		UpdatedAt: iface.updatedAt,
		Created:   created,
	}
	if iface.status != StatusEmpty && m.Timestamp < iface.updatedAt {
		res.Outcome = OutcomeDiscarded
		return res
	}
	if iface.props == nil {
		iface.props = make(fractal.Props, len(m.Props))
	}
	maps.Copy(iface.props, m.Props)
	iface.updatedAt = m.Timestamp
	iface.status = StatusOK
	res.Outcome = OutcomeApplied
	res.Status = StatusOK
	res.UpdatedAt = m.Timestamp
	return res
}

// publishApplied sends an Event for a REP with OutcomeApplied. Call it with no
// lock of the Store or of a node.
func (s *Store) publishApplied(res Result) {
	if res.Outcome != OutcomeApplied || !s.watching() {
		return
	}
	s.publish(Event{Key: res.Key, Status: res.Status, UpdatedAt: res.UpdatedAt})
}
