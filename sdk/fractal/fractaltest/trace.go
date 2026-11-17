package fractaltest

import "sync"

// Trace records every message one link delivers, with its encoded payload, in
// each direction. A test reads it to check which messages the nodes sent over
// the link, without access to the inside of a node.
type Trace struct {
	up   []Received
	down []Received
	mu   sync.Mutex
}

// Watch starts a new trace on this link. A trace that an earlier call started
// gets no further messages.
func (l *Link) Watch() *Trace {
	tr := &Trace{}
	l.up.watch(tr.addUp)
	l.down.watch(tr.addDown)
	return tr
}

// Up returns the messages the link delivered to the node above, in the order
// of delivery. The slice is a copy, so the caller may change it.
func (tr *Trace) Up() []Received {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return append([]Received(nil), tr.up...)
}

// Down returns the messages the link delivered to the node below, in the order
// of delivery. The slice is a copy, so the caller may change it.
func (tr *Trace) Down() []Received {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return append([]Received(nil), tr.down...)
}

// Reset removes the recorded messages. A test calls it between two steps, so
// that Up and Down return only the messages of the next step.
func (tr *Trace) Reset() {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.up, tr.down = nil, nil
}

// addUp records one message the link delivered to the node above.
func (tr *Trace) addUp(r Received) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.up = append(tr.up, r)
}

// addDown records one message the link delivered to the node below.
func (tr *Trace) addDown(r Received) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.down = append(tr.down, r)
}
