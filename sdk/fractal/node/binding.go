package node

import (
	"context"
	"errors"
	"iter"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// Dispatch is one message the engine gives the transport to send.
type Dispatch struct {
	// Payload is the encoded message. The transport must send the bytes
	// unchanged.
	Payload []byte
	// Child is the Node ID of the node directly below this one that the
	// message goes to. It is empty for a message that goes to the node above.
	Child fractal.NodeID
	// ID is the id of an RPC or a RES. A durable send delivers a message with
	// the same id only once. It is empty for every other message type.
	ID fractal.RPCID
	// Kind is the message type.
	Kind fractal.Kind
}

// Down reports whether the message goes to a node below this one.
func (d Dispatch) Down() bool { return d.Child != "" }

// Binding is the transport a Node sends messages through. A transport package
// implements it. The same package calls Node.Deliver for every message it
// receives, and the Connected and Lost methods when a link goes up or down.
// Both methods return an error that wraps ErrNoSuchLink when the transport
// has no link to the target.
type Binding interface {
	// SendBestEffort tries once to send d. It serves every message except
	// RPC and RES. When the link is down the transport drops the message and
	// returns nil. It must not queue or retry.
	SendBestEffort(ctx context.Context, d Dispatch) error
	// SendDurable sends an RPC or a RES. It must return only after the
	// message is stored so that a restart does not lose it. The transport
	// keeps the message while the link is down, delivers the messages for one
	// target in the order sent, and delivers one Dispatch.ID once within its
	// deduplication window.
	SendDurable(ctx context.Context, d Dispatch) error
}

// ErrNoSuchLink is the error a Binding returns, wrapped, when it has no link
// to the node the message goes to. A link that exists and is down is not an
// error. The engine answers an RPC that fails this way with
// fractal.CodeNodeNotFound.
var ErrNoSuchLink = errors.New("this node holds no link to that node")

// Inbound is one message the transport gives to Node.Deliver.
type Inbound struct {
	// Payload is the encoded message as it arrived.
	Payload []byte
	// Ack tells the transport that the engine has finished with the message
	// and it must not be delivered again. Set it for an RPC and a RES, and
	// leave it nil otherwise.
	//
	// The engine calls Ack at most once, after it has forwarded the message
	// durably, recorded and answered the method it ran, or dropped the
	// message. When the engine does not call Ack, deliver the message again.
	Ack func(ctx context.Context) error
	// From is the Node ID of the node directly below this one whose link the
	// message arrived on. It is empty for a message from the node above. The
	// engine requires the sender field to be that node or a node under it.
	From fractal.NodeID
	// Kind is the message type. The transport knows it from where the message
	// arrived, and the engine decodes Payload as that type.
	Kind fractal.Kind
}

// FromChild reports whether the message arrived from a node below this one.
func (in Inbound) FromChild() bool { return in.From != "" }

// Admissions tells the engine which nodes are directly below this one. The
// program that builds the Node supplies it, from the join exchange. The engine
// sends nothing toward a node that is not admitted: it answers an RPC with
// fractal.CodeNodeNotFound and drops a DES.
type Admissions interface {
	// Admitted reports whether id is the Node ID of a node directly below this
	// one.
	Admitted(id fractal.NodeID) bool
	// Children iterates over the Node ID of every node directly below this
	// one.
	Children() iter.Seq[fractal.NodeID]
}

// NoChildren is the Admissions of a node that admits no node below it. A leaf
// of the tree uses it.
type NoChildren struct{}

// Admitted always returns false.
func (NoChildren) Admitted(fractal.NodeID) bool { return false }

// Children iterates over nothing.
func (NoChildren) Children() iter.Seq[fractal.NodeID] {
	return func(func(fractal.NodeID) bool) {}
}
