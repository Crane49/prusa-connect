package node

import "github.com/prusa3d/prusa-connect/sdk/fractal"

// Outcome says what the engine did with one message. The engine passes it to
// Metrics.Message.
type Outcome uint8

// The outcomes the engine reports.
const (
	// OutcomeProcessed means the message was for this node and the node
	// handled it.
	OutcomeProcessed Outcome = iota + 1
	// OutcomeForwarded means the node sent the message on, to the node above
	// or to a node below.
	OutcomeForwarded
	// OutcomeDropped means the node dropped the message. The engine also calls
	// Metrics.Drop with the DropReason.
	OutcomeDropped
)

// outcomeNames gives String the name of each outcome.
var outcomeNames = map[Outcome]string{
	OutcomeProcessed: "processed",
	OutcomeForwarded: "forwarded",
	OutcomeDropped:   "dropped",
}

// Outcomes returns every outcome. The caller owns the returned slice.
func Outcomes() []Outcome {
	return []Outcome{OutcomeProcessed, OutcomeForwarded, OutcomeDropped}
}

// String returns the name of the outcome, or "unknown".
func (o Outcome) String() string {
	if name, ok := outcomeNames[o]; ok {
		return name
	}
	return "unknown"
}

// DropReason says why the engine dropped a message. The engine passes it to
// Metrics.Drop.
type DropReason uint8

// The reasons the engine drops a message.
const (
	// DropUnsupportedVersion means the message has a protocol version this
	// package does not support.
	DropUnsupportedVersion DropReason = iota + 1
	// DropMalformed means the payload does not decode or validate as the
	// message type in Inbound.Kind, or the Kind is unknown.
	DropMalformed
	// DropNoNodePath means the message arrived while this node had no Node
	// Path.
	DropNoNodePath
	// DropWrongDirection means an upward message type arrived from the node
	// above, or a downward type from a node below.
	DropWrongDirection
	// DropSenderInvalid means the sender field names a node the message
	// cannot come from. From a node below, the sender must be that node or a
	// node under it. From the node above, it must be a node above this one.
	DropSenderInvalid
	// DropNotRoutable means the receiver of a DES or an RPC is not below this
	// node, the next node on the way is not admitted, or the transport has no
	// link to it.
	DropNotRoutable
	// DropNoParent means a root node received a RES addressed to another node.
	DropNoParent
	// DropStaleReport means the timestamp of a REP or an AREP is lower than
	// the one the shadow has for that interface. The node neither stores nor
	// forwards it.
	DropStaleReport
	// DropDuplicate means this node already accepted a RES with the same id.
	DropDuplicate
	// DropBusy means a queue of the engine was full when the message arrived.
	// The engine does not acknowledge the message, so the transport delivers a
	// durable message again.
	DropBusy
	// DropSendFailed means the transport returned an error for the message,
	// and the error was not ErrNoSuchLink.
	DropSendFailed
	// DropRejected means this node was the receiver and did not do what the
	// message asked: a DES for an interface it does not own, with invalid
	// props or refused by the owner, or an RPC that had expired.
	DropRejected
)

// dropReasonNames gives String the name of each reason.
var dropReasonNames = map[DropReason]string{
	DropUnsupportedVersion: "unsupported version",
	DropMalformed:          "malformed",
	DropNoNodePath:         "no node path",
	DropWrongDirection:     "wrong direction",
	DropSenderInvalid:      "sender invalid",
	DropNotRoutable:        "not routable",
	DropNoParent:           "no parent",
	DropStaleReport:        "stale report",
	DropDuplicate:          "duplicate",
	DropBusy:               "busy",
	DropSendFailed:         "send failed",
	DropRejected:           "rejected",
}

// DropReasons returns every reason. The caller owns the returned slice.
func DropReasons() []DropReason {
	reasons := make([]DropReason, 0, len(dropReasonNames))
	for r := DropUnsupportedVersion; r <= DropRejected; r++ {
		reasons = append(reasons, r)
	}
	return reasons
}

// String returns the name of the reason, or "unknown".
func (r DropReason) String() string {
	if name, ok := dropReasonNames[r]; ok {
		return name
	}
	return "unknown"
}

// Metrics receives a call for every message the engine handles. The engine
// calls both methods while it handles a message, from several goroutines. An
// implementation must return quickly and be safe for concurrent use.
type Metrics interface {
	// Message counts one message of type k that the engine processed,
	// forwarded or dropped.
	Message(k fractal.Kind, o Outcome)
	// Drop counts one message of type k that the engine dropped for reason r.
	Drop(k fractal.Kind, r DropReason)
}

// nopMetrics counts nothing. New uses it when Config.Metrics is nil.
type nopMetrics struct{}

// Message counts nothing.
func (nopMetrics) Message(fractal.Kind, Outcome) {}

// Drop counts nothing.
func (nopMetrics) Drop(fractal.Kind, DropReason) {}
