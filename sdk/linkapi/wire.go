// Package linkapi is the contract between the host and a link module. It
// defines the calls the host makes on the module, and the call the module makes
// on the host.
//
// A link is the connection from this site to the sites above and below it. A
// site joins one site above it and accepts other sites under it, so the sites
// form a tree.
//
// The host calls link.status, link.approve, link.reject and link.revoke. A
// module installs Handlers on its session and the host uses a Client.
//
// The module calls link.changed with its new Status, and the host publishes
// the link state event from it. The host installs ChangedHandlers and the
// module uses a Reporter.
package linkapi

import (
	"errors"
	"fmt"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// The calls the host makes on a link module.
const (
	// MethodStatus returns the link's Status.
	MethodStatus wirerpc.Method = "link.status"
	// MethodApprove admits one node below this one. The call names the node by
	// the id Status reported. Approving an admitted node changes nothing and
	// returns no error.
	MethodApprove wirerpc.Method = "link.approve"
	// MethodReject removes one node that waits for a decision. The node leaves
	// Status and its id is free again. Rejecting a node that does not wait
	// returns ErrNoNode.
	MethodReject wirerpc.Method = "link.reject"
	// MethodRevoke takes admission away from one node below this one. The node
	// stays in Status with AdmissionRevoked.
	MethodRevoke wirerpc.Method = "link.revoke"
)

// MethodChanged is the call a link module makes on the host when its Status
// changes. The call sends the new Status.
const MethodChanged wirerpc.Method = "link.changed"

// The limits of this contract. The sending side refuses a value over a limit
// before it makes the call, and the receiving side checks again.
const (
	// MaxNodeIDBytes limits the length of a node id.
	MaxNodeIDBytes = 128

	// MaxNodePathBytes limits the length of a node path.
	MaxNodePathBytes = 1024

	// MaxKeyBytes limits the length of a public identity key written as text.
	MaxKeyBytes = 512

	// MaxAddressBytes limits the length of one network address.
	MaxAddressBytes = 512

	// MaxReasonBytes limits the length of Uplink.Reason.
	MaxReasonBytes = 1024

	// MaxNodes limits how many nodes below this one a Status reports.
	MaxNodes = 4096

	// MaxListeners limits how many network listeners a Status reports.
	MaxListeners = 64
)

// The error codes of this contract. A caller branches on the code and not on
// the text of a message. The codes are outside the range JSON-RPC 2.0
// reserves and outside the ranges of the other contracts of this SDK.
const (
	// CodeNoNode is ErrNoNode.
	CodeNoNode = 4000
	// CodeNotReady is ErrNotReady.
	CodeNotReady = 4001
	// CodeBusy is ErrBusy.
	CodeBusy = 4002
	// CodeInvalidReading is ErrInvalidReading.
	CodeInvalidReading = 4003
)

// The errors of this contract. A caller matches them with errors.Is.
var (
	// ErrNoNode reports that the link does not know the node id in the call.
	// The link returns the same error for an id it never saw and for an id it
	// has forgotten, so a caller cannot learn which ids the link has seen.
	ErrNoNode = errors.New("linkapi: no such node")

	// ErrNotReady reports that the link cannot answer yet: it has not joined
	// the node above it, or the module is still starting. The caller can try
	// again later.
	ErrNotReady = errors.New("linkapi: the link is not serving")

	// ErrBusy reports that the link is already answering as many calls at once
	// as it allows. The link refuses one more call and does not queue it.
	ErrBusy = errors.New("linkapi: the link is at its concurrent call limit")

	// ErrInvalidReading reports a Status that is over a limit of this contract
	// or has a value outside a closed set. The host returns it for a Status a
	// module sent. A module returns it for a Status it was about to send.
	ErrInvalidReading = errors.New("linkapi: the reading is outside the published bounds")
)

// NodeID is a node's identity in the network. The link protocol decides how an
// id is made. This contract only passes the id on and limits its length.
type NodeID string

// String returns the id as it is written on the wire.
func (n NodeID) String() string { return string(n) }

// Validate checks the id against MaxNodeIDBytes and refuses an empty id. The
// error matches ErrNoNode.
func (n NodeID) Validate() error {
	switch {
	case n == "":
		return fmt.Errorf("%w: the call names no node", ErrNoNode)
	case len(n) > MaxNodeIDBytes:
		return fmt.Errorf("%w: the id is %d bytes and the limit is %d",
			ErrNoNode, len(n), MaxNodeIDBytes)
	}

	return nil
}

// NodePath is the position of a node in the tree. It names every node from the
// root down to this one. It is empty for a node that has not joined yet, and
// for a protocol that has no paths.
type NodePath string

// String returns the path as it is written on the wire.
func (p NodePath) String() string { return string(p) }

// UplinkState is the state of the connection to the node above this one. It is
// a closed set, and UplinkStates lists every value.
type UplinkState string

const (
	// UplinkNotConfigured means the configuration names no node to join. A
	// site at the top of its tree has this state.
	UplinkNotConfigured UplinkState = "not_configured"
	// UplinkJoining means the link has asked to join and has no answer yet.
	UplinkJoining UplinkState = "joining"
	// UplinkConnected means the node above accepted the join and the
	// connection is open.
	UplinkConnected UplinkState = "connected"
	// UplinkLost means the connection was open and has closed. The module
	// tries to connect again without help, so an operator has nothing to do.
	UplinkLost UplinkState = "lost"
	// UplinkDenied means the node above refused the join. Uplink.Code is the
	// protocol's number for the refusal and Uplink.Reason is its text.
	UplinkDenied UplinkState = "denied"
)

// String returns the state as it is written on the wire.
func (s UplinkState) String() string { return string(s) }

// Valid reports whether s is a member of the closed set.
func (s UplinkState) Valid() bool {
	switch s {
	case UplinkNotConfigured, UplinkJoining, UplinkConnected, UplinkLost, UplinkDenied:
		return true
	}

	return false
}

// UplinkStates returns the closed set, in declaration order.
func UplinkStates() []UplinkState {
	return []UplinkState{
		UplinkNotConfigured, UplinkJoining, UplinkConnected, UplinkLost, UplinkDenied,
	}
}

// Admission says whether this site lets a node below it act here. It is a
// closed set, and Admissions lists every value.
type Admission string

const (
	// AdmissionPending means the node waits for a person to decide. The link
	// may still receive the node's messages, so that person can see the node
	// before deciding.
	AdmissionPending Admission = "pending"
	// AdmissionAdmitted means a person or a rule admitted the node.
	AdmissionAdmitted Admission = "admitted"
	// AdmissionRevoked means a person took the node's admission away. The node
	// stays in Status.
	AdmissionRevoked Admission = "revoked"
)

// String returns the admission as it is written on the wire.
func (a Admission) String() string { return string(a) }

// Valid reports whether a is a member of the closed set.
func (a Admission) Valid() bool {
	switch a {
	case AdmissionPending, AdmissionAdmitted, AdmissionRevoked:
		return true
	}

	return false
}

// Admissions returns the closed set, in declaration order.
func Admissions() []Admission {
	return []Admission{AdmissionPending, AdmissionAdmitted, AdmissionRevoked}
}

// Admits is the policy this site applies to a site that asks to join under it.
// It is a closed set, and AdmitsPolicies lists every value. It is empty for a
// site that accepts no sites under it.
type Admits string

const (
	// AdmitsInherit admits every site that proves it owns its identity key.
	AdmitsInherit Admits = "inherit"
	// AdmitsExplicit keeps every site at AdmissionPending until a person
	// decides.
	AdmitsExplicit Admits = "explicit"
)

// String returns the policy as it is written on the wire.
func (a Admits) String() string { return string(a) }

// Valid reports whether a is a member of the closed set.
func (a Admits) Valid() bool {
	switch a {
	case AdmitsInherit, AdmitsExplicit:
		return true
	}

	return false
}

// AdmitsPolicies returns the closed set, in declaration order.
func AdmitsPolicies() []Admits { return []Admits{AdmitsInherit, AdmitsExplicit} }

// Uplink describes the connection to the node above this one: the address this
// link joins, the state of the connection, and the time it last opened.
type Uplink struct {
	// Address is the address this link joins, in the protocol's format.
	Address string `json:"address,omitempty"`

	// State is the state of the connection.
	State UplinkState `json:"state"`

	// Reason is the text the node above gave for the state. It is empty when
	// the node above gave none. Show it to a person. Do not branch on it.
	Reason string `json:"reason,omitempty"`

	// Code is the protocol's number for a refusal. It is zero when the state
	// is not UplinkDenied. A module decides by this number whether to try
	// again, because Reason is free text.
	Code int `json:"code,omitempty"`

	// FaultCode is the registered code for the refusal, written as
	// domain/REASON. A page translates by it and falls back to Reason. It is
	// empty when the module registered no code for the refusal.
	FaultCode string `json:"fault_code,omitempty"`

	// ConnectedAt is the time the connection last opened, written as RFC 3339
	// text. It is absent until a connection has opened. While the state is
	// UplinkLost it keeps the time of the last opening.
	ConnectedAt *time.Time `json:"connected_at,omitempty"`
}

// Node describes one site below this one, as this link knows it. LastSeen and
// RoundTripMS are pointers, because an absent value is not zero.
type Node struct {
	// ID is the node's identity.
	ID NodeID `json:"id"`

	// Path is the node's position in the tree. It is empty before the node has
	// joined.
	Path NodePath `json:"path,omitempty"`

	// Admission says whether this site lets the node act here.
	Admission Admission `json:"admission"`

	// PublicKey is the node's public identity key written as text. It is empty
	// when this link has not learned the key.
	PublicKey string `json:"public_key,omitempty"`

	// Address is the network address of the node. It is empty when the node
	// connected to this site and does not publish an address.
	Address string `json:"address,omitempty"`

	// Connected says whether the connection to the node is open.
	Connected bool `json:"connected"`

	// LastSeen is the time this link last received a message from the node,
	// written as RFC 3339 text. It is absent until a message has arrived.
	LastSeen *time.Time `json:"last_seen,omitempty"`

	// RoundTripMS is the last round trip this link measured to the node, in
	// milliseconds. It is absent for a protocol that does not measure one, and
	// for a node with no finished measurement.
	RoundTripMS *float64 `json:"round_trip_ms,omitempty"`
}

// Listener describes one network listener the module opened. An operator reads
// it to learn the address other sites connect to.
type Listener struct {
	// Protocol is the module's name for the protocol of the listener, such as
	// nats or tcp. This contract does not define a set of protocol names.
	Protocol string `json:"protocol"`

	// Address is the network interface the listener is bound to.
	Address string `json:"address"`

	// Port is the port the listener accepts connections on. It is zero for a
	// transport that has no ports.
	Port int `json:"port,omitempty"`
}

// Status is the state of one link at one moment, also called a reading.
type Status struct {
	// NodeID is this site's id in the network. It is empty before the site has
	// an id.
	NodeID NodeID `json:"node_id,omitempty"`

	// NodePath is this site's position in the tree. It is empty before the
	// site has joined.
	NodePath NodePath `json:"node_path,omitempty"`

	// PublicKey is this site's public identity key written as text, in the
	// protocol's format. A module never puts the private key in a Status.
	PublicKey string `json:"public_key,omitempty"`

	// Accepting says whether this site accepts other sites under it.
	Accepting bool `json:"accepting"`

	// Admits is the policy this site applies to a site that asks to join under
	// it. It is empty for a site that accepts none.
	Admits Admits `json:"admits,omitempty"`

	// Uplink describes the node above this one. It is nil for a site whose
	// configuration names none.
	Uplink *Uplink `json:"uplink,omitempty"`

	// Nodes lists every site below this one, ordered by id.
	Nodes []Node `json:"nodes,omitempty"`

	// Listeners lists every network listener the module opened.
	Listeners []Listener `json:"listeners,omitempty"`

	// Counters is what this link has handled since it started. It is nil for a
	// link that counts nothing.
	Counters *Counters `json:"counters,omitempty"`
}

// Validate checks a Status against the limits and the closed sets of this
// contract. The error matches ErrInvalidReading. A module calls it before it
// sends a Status, and the host calls it again on the Status it receives.
func (s Status) Validate() error {
	switch {
	case len(s.NodeID) > MaxNodeIDBytes:
		return overBound("node_id", len(s.NodeID), MaxNodeIDBytes)
	case len(s.NodePath) > MaxNodePathBytes:
		return overBound("node_path", len(s.NodePath), MaxNodePathBytes)
	case len(s.PublicKey) > MaxKeyBytes:
		return overBound("public_key", len(s.PublicKey), MaxKeyBytes)
	case len(s.Nodes) > MaxNodes:
		return overBound("nodes", len(s.Nodes), MaxNodes)
	case len(s.Listeners) > MaxListeners:
		return overBound("listeners", len(s.Listeners), MaxListeners)
	case s.Admits != "" && !s.Admits.Valid():
		return fmt.Errorf("%w: admits is %q and the set is %v",
			ErrInvalidReading, s.Admits, AdmitsPolicies())
	}

	if err := s.Uplink.validate(); err != nil {
		return err
	}

	for _, node := range s.Nodes {
		if err := node.validate(); err != nil {
			return err
		}
	}

	for _, listener := range s.Listeners {
		if len(listener.Address) > MaxAddressBytes {
			return overBound("listeners[].address", len(listener.Address), MaxAddressBytes)
		}
	}

	return s.Counters.Validate()
}

// validate checks the Uplink against the limits and the UplinkState set. A nil
// Uplink is valid: the site names no node above it.
func (u *Uplink) validate() error {
	if u == nil {
		return nil
	}

	switch {
	case !u.State.Valid():
		return fmt.Errorf("%w: uplink.state is %q and the set is %v",
			ErrInvalidReading, u.State, UplinkStates())
	case len(u.Address) > MaxAddressBytes:
		return overBound("uplink.address", len(u.Address), MaxAddressBytes)
	case len(u.Reason) > MaxReasonBytes:
		return overBound("uplink.reason", len(u.Reason), MaxReasonBytes)
	}

	return nil
}

// validate checks one Node against the limits and the Admission set.
func (n Node) validate() error {
	if err := n.ID.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidReading, err)
	}

	switch {
	case len(n.Path) > MaxNodePathBytes:
		return overBound("nodes[].path", len(n.Path), MaxNodePathBytes)
	case len(n.PublicKey) > MaxKeyBytes:
		return overBound("nodes[].public_key", len(n.PublicKey), MaxKeyBytes)
	case len(n.Address) > MaxAddressBytes:
		return overBound("nodes[].address", len(n.Address), MaxAddressBytes)
	case !n.Admission.Valid():
		return fmt.Errorf("%w: nodes[%s].admission is %q and the set is %v",
			ErrInvalidReading, n.ID, n.Admission, Admissions())
	}

	return nil
}

// overBound builds the error for a field over its limit. The message names the
// field, its size and the limit.
func overBound(field string, got, limit int) error {
	return fmt.Errorf("%w: %s is %d and the limit is %d", ErrInvalidReading, field, got, limit)
}

// StatusParams is the params object of MethodStatus. It has no members.
type StatusParams struct{}

// Validate always returns nil.
func (StatusParams) Validate() error { return nil }

// NodeParams is the params object of MethodApprove, MethodReject and
// MethodRevoke. It names one node below this one.
type NodeParams struct {
	// ID is the id of the node to approve, reject or revoke.
	ID NodeID `json:"id"`
}

// Validate checks the node id. The error matches ErrNoNode.
func (p NodeParams) Validate() error { return p.ID.Validate() }

// ChangedParams is the params object of MethodChanged. It has the whole new
// Status, so a host that missed one call is correct again after the next.
type ChangedParams struct {
	// Status is the link's Status after the change.
	Status Status `json:"status"`
}

// Validate checks the Status. The error matches ErrInvalidReading.
func (p ChangedParams) Validate() error { return p.Status.Validate() }

// sentinelFor returns the error variable of this package for an error code the
// other side sent, or nil for a code of another contract.
func sentinelFor(code int) error {
	switch code {
	case CodeNoNode:
		return ErrNoNode
	case CodeNotReady:
		return ErrNotReady
	case CodeBusy:
		return ErrBusy
	case CodeInvalidReading:
		return ErrInvalidReading
	}

	return nil
}

// refusal turns an error into the wirerpc error a handler returns. An error
// of this package keeps its code and message, and a registered fault goes in
// the data member. Any other error becomes CodeInternal with the fixed text in
// failed, because its own text could leak a path or an address.
func refusal(err error, failed string) error {
	var carried *faulted

	held := errors.As(err, &carried)

	// The order matters. A reading whose node has no id matches ErrNoNode and
	// ErrInvalidReading, and ErrInvalidReading must win.
	coded := []struct {
		code     int
		sentinel error
	}{
		{CodeInvalidReading, ErrInvalidReading},
		{CodeNoNode, ErrNoNode},
		{CodeNotReady, ErrNotReady},
		{CodeBusy, ErrBusy},
	}

	for _, known := range coded {
		if !errors.Is(err, known.sentinel) {
			continue
		}

		if held {
			// The code of this contract says what the other side has to do.
			// The fault beside it says what to show a person.
			return &wirerpc.Error{Code: known.code, Message: carried.Error(), Data: carried.data()}
		}

		return &wirerpc.Error{Code: known.code, Message: err.Error()}
	}

	if held {
		return carried.wireError()
	}

	return &wirerpc.Error{Code: wirerpc.CodeInternal, Message: failed}
}

// ModuleFailed is the error message the host receives when a link module fails
// with an error this contract has no code for.
const ModuleFailed = "linkapi: the link could not answer"

// HostFailed is the error message a module receives when the host fails with an
// error this contract has no code for.
const HostFailed = "linkapi: the host could not answer"

// remote is an error the other side sent with a code of this contract. Client
// and Reporter return it. errors.Is matches it to the error variable for the
// code.
type remote struct {
	sentinel error
	message  string
}

func (r *remote) Error() string { return r.message }

// Unwrap returns the error variable for the code, so errors.Is works on an
// error that came from another process.
func (r *remote) Unwrap() error { return r.sentinel }
