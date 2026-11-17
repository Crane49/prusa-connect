package natsbind

import (
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// UplinkState is the state of this node's connection to the node above it.
type UplinkState string

// The values of UplinkState.
const (
	// UplinkNotConfigured means Config.ParentAddr is empty, so this is a root
	// node.
	UplinkNotConfigured UplinkState = "not_configured"
	// UplinkJoining means the node has a ParentAddr and its leafnode connection
	// to the node above is not open yet. It may still wait for the answer to
	// its join request.
	UplinkJoining UplinkState = "joining"
	// UplinkConnected means the node above granted the join request, the
	// leafnode connection to it is open and the node above listens on it.
	UplinkConnected UplinkState = "connected"
	// UplinkLost means the leafnode connection was open and has closed. The
	// NATS server connects again without any action.
	UplinkLost UplinkState = "lost"
	// UplinkDenied means the node above denied the join request. Uplink.Code
	// has the Fractal denial code and Uplink.Reason has its text. It
	// also means this node refused the grant, with Uplink.Refused set.
	UplinkDenied UplinkState = "denied"
)

// String returns the state as text.
func (s UplinkState) String() string { return string(s) }

// Admission says whether a node below may use its link to this node.
type Admission string

// The values of Admission.
const (
	// AdmissionPending means the node sent a join request and waits for an
	// operator to approve it.
	AdmissionPending Admission = "pending"
	// AdmissionAdmitted means this node granted the node's join request.
	AdmissionAdmitted Admission = "admitted"
	// AdmissionRevoked means an operator revoked the node. Status still lists
	// it, so the operator sees which nodes are revoked.
	AdmissionRevoked Admission = "revoked"
)

// String returns the admission as text.
func (a Admission) String() string { return string(a) }

// Listener is one address the NATS server of this node listens on.
type Listener struct {
	// Protocol is "nats" for the client listener and "nats-leaf" for the
	// leafnode listener.
	Protocol string
	// Address is the host or IP address the listener is bound to.
	Address string
	// Port is the port of the listener.
	Port int
}

// Uplink describes this node's connection to the node above it.
type Uplink struct {
	// Address is the client address of the node above, where this node sends
	// its join request.
	Address string
	// State is the state of the connection.
	State UplinkState
	// Reason is the text the node above sent with a denial. While the node is
	// still joining it is the error of the last join request that did not
	// reach the node above, so an operator reads a wrong address here.
	Reason string
	// Code is the denial code. It is zero when State is not UplinkDenied.
	Code fractal.Code
	// Refused is true when State is UplinkDenied because this node refused
	// the grant of the node above. It refuses a grant for a protocol version
	// or extension data it does not support, one that misses a field, and one
	// from another parent or with another Node Path. Code is zero then.
	Refused bool
	// Unreadable is true when State is UplinkDenied because this node cannot
	// read its saved refusal. It asks the node above nothing until an
	// operator deletes the file. Code is zero then.
	Unreadable bool
	// ConnectedAt is when the leafnode connection last opened. It is nil until
	// the first connection, and it keeps its value while State is UplinkLost.
	ConnectedAt *time.Time
}

// Below describes one node below this one.
type Below struct {
	// ID is the node's Node ID.
	ID fractal.NodeID
	// Path is the node's Node Path: this node's Node Path followed by ID. It
	// is empty for a node whose join request waits for an operator.
	Path fractal.NodePath
	// Identity is the public key that signed the node's join request.
	Identity fractal.Identity
	// Admission says whether the node may use its link to this node.
	Admission Admission
	// Connected is true while the node's leafnode connection is open, the node
	// listens on it and its heartbeats arrive within the liveness timeout.
	Connected bool
	// LastHeartbeat is when the last heartbeat from the node arrived, or when
	// its link connected if no heartbeat has arrived since. It is nil before
	// the node's first connection.
	LastHeartbeat *time.Time
}

// Status describes this node's connection to the node above and its links to
// the nodes below. The links page of the application shows it.
type Status struct {
	// NodeID is this node's Node ID.
	NodeID fractal.NodeID
	// NodePath is this node's Node Path. It is empty until the node above
	// grants the join request.
	NodePath fractal.NodePath
	// Identity is the public key of this node's identity key pair. Status
	// never has the private seed.
	Identity fractal.Identity
	// Accepting is Config.AcceptsChildren: this node answers join requests.
	Accepting bool
	// AsksFirst is Config.AskFirst: a join request from an unknown public key
	// waits for an operator.
	AsksFirst bool
	// Mode is how this node checks a join request.
	Mode Mode
	// Uplink describes the connection to the node above.
	Uplink Uplink
	// Nodes lists every node below this one, sorted by Node ID.
	Nodes []Below
	// Listeners lists every address the NATS server of this node listens on.
	Listeners []Listener
}
