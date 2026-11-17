package fractaltest

import (
	"context"
	"sync"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// Received is one message that a Recorder or a Trace recorded. Its fields are
// the fields of the node.Inbound the transport delivered.
type Received struct {
	// Ack acknowledges the message to the transport. It is nil when the
	// transport delivered the message with no Ack function, and in every
	// message of a Trace.
	Ack func(ctx context.Context) error
	// Payload is the encoded message as it arrived.
	Payload []byte
	// From is the Node ID of the link the message arrived on, for a message
	// from a node below. It is empty for a message from the node above.
	From fractal.NodeID
	// Kind is the message type.
	Kind fractal.Kind
}

// Recorder is an Engine that records every message and every link signal and
// does nothing else with them. The conformance suite puts one at each end of a
// link, so a case tests the transport alone, with no node.Node behind it.
type Recorder struct {
	mu         sync.Mutex
	messages   []Received
	connected  []fractal.NodeID
	lost       []fractal.NodeID
	revoked    []fractal.NodeID
	parentUp   int
	parentLost int
	autoAck    bool
}

// NewRecorder builds a recorder. When autoAck is true, the recorder
// acknowledges every durable message when it arrives, as a node does after it
// has processed the message.
func NewRecorder(autoAck bool) *Recorder {
	return &Recorder{autoAck: autoAck}
}

// Deliver records one message, and acknowledges it when autoAck is on.
func (r *Recorder) Deliver(ctx context.Context, in node.Inbound) {
	r.mu.Lock()
	r.messages = append(r.messages, Received{Ack: in.Ack, Payload: in.Payload, From: in.From, Kind: in.Kind})
	auto := r.autoAck
	r.mu.Unlock()
	if auto && in.Ack != nil {
		_ = in.Ack(ctx) // The in-memory transport never refuses an acknowledgement.
	}
}

// ChildConnected records the Node ID of a node below whose link came up.
func (r *Recorder) ChildConnected(_ context.Context, id fractal.NodeID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.connected = append(r.connected, id)
}

// ChildLost records the Node ID of a node below whose link went down.
func (r *Recorder) ChildLost(_ context.Context, id fractal.NodeID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lost = append(r.lost, id)
}

// ChildRevoked records the Node ID of a node below that this node revoked.
func (r *Recorder) ChildRevoked(_ context.Context, id fractal.NodeID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.revoked = append(r.revoked, id)
}

// ParentConnected counts a call that says the link to the node above came up.
func (r *Recorder) ParentConnected(context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.parentUp++
}

// ParentLost counts a call that says the link to the node above went down.
func (r *Recorder) ParentLost(context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.parentLost++
}

// ForgetAncestors does nothing, because a Recorder keeps no ancestor shadow.
func (r *Recorder) ForgetAncestors(context.Context) {}

// Messages returns the recorded messages in the order they arrived. The slice
// is a copy, so the caller may change it.
func (r *Recorder) Messages() []Received {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Received, len(r.messages))
	copy(out, r.messages)
	return out
}

// Kinds returns the message type of every recorded message, in the order the
// messages arrived.
func (r *Recorder) Kinds() []fractal.Kind {
	r.mu.Lock()
	defer r.mu.Unlock()
	kinds := make([]fractal.Kind, len(r.messages))
	for i, m := range r.messages {
		kinds[i] = m.Kind
	}
	return kinds
}

// Count returns the number of recorded messages. A message delivered twice
// counts twice.
func (r *Recorder) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.messages)
}

// Connected returns the Node ID from every ChildConnected call, in the order
// of the calls.
func (r *Recorder) Connected() []fractal.NodeID {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]fractal.NodeID(nil), r.connected...)
}

// Lost returns the Node ID from every ChildLost call, in the order of the
// calls.
func (r *Recorder) Lost() []fractal.NodeID {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]fractal.NodeID(nil), r.lost...)
}

// Revoked returns the Node IDs of the ChildRevoked calls, in order.
func (r *Recorder) Revoked() []fractal.NodeID {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]fractal.NodeID(nil), r.revoked...)
}

// ParentConnections returns the number of ParentConnected calls.
func (r *Recorder) ParentConnections() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.parentUp
}

// ParentLosses returns the number of ParentLost calls.
func (r *Recorder) ParentLosses() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.parentLost
}

// Reset removes every recorded message and every recorded link signal.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = nil
	r.connected = nil
	r.lost = nil
	r.parentUp = 0
	r.parentLost = 0
}

// SetAutoAck turns on or off the acknowledgement of every durable message when
// it arrives. With it off, the recorder models a node that received a method
// invocation and stopped before it acknowledged the message. The transport
// must then deliver the message again.
func (r *Recorder) SetAutoAck(on bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.autoAck = on
}
