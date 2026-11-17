package join

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// Reasons Decide refuses a grant. Decide puts one in Decision.Reason. Match
// each with errors.Is.
var (
	// ErrUnsupportedGrantVersion means the grant has a protocol version this
	// node does not support. The node does not connect and does not try
	// again.
	ErrUnsupportedGrantVersion = errors.New("the grant names an unsupported protocol version")
	// ErrPathDisagreement means the node above is the same as before, and the
	// Node Path in its grant differs from the saved Membership.NodePath.
	ErrPathDisagreement = errors.New("the node above granted a different node path")
	// ErrDifferentParent means the grant has another ParentIdentity than the
	// saved Membership, and it came through the same Endpoint.
	ErrDifferentParent = errors.New("a node with a different identity answered on the same endpoint")
	// ErrHasChildren means this node has admitted at least one node below it,
	// and the grant has another ParentIdentity than the saved Membership. Such
	// a node must stay under the node that granted its Node Path.
	ErrHasChildren = errors.New("a node that has nodes below it must not join under a different node")
	// ErrUnusableGrant means the grant is missing a required field.
	ErrUnusableGrant = errors.New("the grant is unusable")
)

// Action is what Decide tells a node to do with a grant.
type Action uint8

// The values of Action.
const (
	// ActionAdopt means the node takes the Node Path in the grant, saves the
	// Membership and connects.
	ActionAdopt Action = iota + 1
	// ActionKeep means the grant has the ParentIdentity and the Node Path the
	// node already saved. The node saves the new credentials and the new
	// expiry time.
	ActionKeep
	// ActionRefuse means the node does not connect and takes no Node Path.
	// Decision.Reason says why.
	ActionRefuse
)

// String returns "adopt", "keep" or "refuse".
func (a Action) String() string {
	switch a {
	case ActionAdopt:
		return "adopt"
	case ActionKeep:
		return "keep"
	case ActionRefuse:
		return "refuse"
	}
	return "unknown"
}

// Situation is the input of Decide: the grant and the state of the node that
// received it.
type Situation struct {
	// Held is the Membership the node saved after an earlier grant. It is
	// used only when Joined is true.
	Held Membership
	// Joined is true when the node has a saved Membership.
	Joined bool
	// Grant is the grant the node above sent.
	Grant fractal.JoinGrant
	// Endpoint is the address the node sent this join request to.
	Endpoint Endpoint
	// HasChildren is true when this node has admitted at least one node below
	// it.
	HasChildren bool
	// Binding is Reply.Extra, the extra bytes the transport binding wants to
	// keep from this answer. Decide copies it to Membership.Binding. On
	// ActionKeep with Binding nil, Decide keeps Held.Binding.
	Binding json.RawMessage
	// Extensions are the names of the protocol extensions this node runs.
	// Decide keeps their entries of Grant.Extensions in
	// Membership.Extensions and ignores the rest.
	Extensions []fractal.Identifier
}

// Decision is the result of Decide.
type Decision struct {
	// Action is ActionAdopt, ActionKeep or ActionRefuse.
	Action Action
	// Membership is the record the node saves. It is set on ActionAdopt and
	// on ActionKeep.
	Membership Membership
	// Reason is the error that says why Decide refused. It is nil for the
	// other actions.
	Reason error
}

// Decide says what a node does with one grant. It reads only s, and it saves
// and sends nothing.
func Decide(s Situation) Decision {
	if !fractal.SupportedVersion(s.Grant.Version) {
		return refuse(fmt.Errorf("version %d: %w", s.Grant.Version, ErrUnsupportedGrantVersion))
	}
	if err := s.Grant.Validate(); err != nil {
		return refuse(fmt.Errorf("%w: %w", ErrUnusableGrant, err))
	}
	fresh := Membership{
		NodePath:       s.Grant.NodePath,
		ParentIdentity: s.Grant.ParentIdentity,
		Credentials:    s.Grant.Credentials,
		ExpiresAt:      s.Grant.ExpiresAt,
		Endpoint:       s.Endpoint,
		Binding:        s.Binding,
		Extensions:     runsOnly(s.Grant.Extensions, s.Extensions),
	}
	if !s.Joined {
		return Decision{Action: ActionAdopt, Membership: fresh}
	}
	if s.Held.ParentIdentity == s.Grant.ParentIdentity {
		if s.Held.NodePath != s.Grant.NodePath {
			return refuse(fmt.Errorf("%w: %s, holding %s",
				ErrPathDisagreement, s.Grant.NodePath, s.Held.NodePath))
		}
		if fresh.Binding == nil {
			fresh.Binding = s.Held.Binding
		}
		return Decision{Action: ActionKeep, Membership: fresh}
	}
	if s.HasChildren {
		return refuse(fmt.Errorf("%w: holding %s", ErrHasChildren, s.Held.NodePath))
	}
	if s.Endpoint == s.Held.Endpoint {
		return refuse(fmt.Errorf("%w: %s", ErrDifferentParent, s.Endpoint))
	}
	return Decision{Action: ActionAdopt, Membership: fresh}
}

// refuse builds a Decision with ActionRefuse.
func refuse(reason error) Decision {
	return Decision{Action: ActionRefuse, Reason: reason}
}

// runsOnly returns the entries of offered whose name is in runs, or nil when
// there is none.
func runsOnly(
	offered map[fractal.Identifier]json.RawMessage, runs []fractal.Identifier,
) map[fractal.Identifier]json.RawMessage {
	var kept map[fractal.Identifier]json.RawMessage
	for _, name := range runs {
		data, ok := offered[name]
		if !ok {
			continue
		}
		if kept == nil {
			kept = map[fractal.Identifier]json.RawMessage{}
		}
		kept[name] = data
	}
	return kept
}
