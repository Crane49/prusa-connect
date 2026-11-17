package join

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// Endpoint is the address a node sends its join request to. The transport
// binding chooses its format. This package only compares two endpoints for
// equality: Decide refuses a grant from an unknown identity that comes
// through the endpoint the node joined through before.
type Endpoint string

// String returns the endpoint unchanged.
func (e Endpoint) String() string { return string(e) }

// Membership is the record a node saves after it accepts a grant.
type Membership struct {
	// NodePath is the Node Path the grant gave this node.
	NodePath fractal.NodePath `json:"nodePath"`
	// ParentIdentity is the authentication identity of the node above. Decide
	// compares it with the identity in the next grant, before the node
	// connects.
	ParentIdentity fractal.Identity `json:"parentIdentity"`
	// Credentials are the connection credentials this node uses to connect to
	// the node above.
	Credentials fractal.ConnectionCredentials `json:"credentials"`
	// ExpiresAt is the time those credentials expire. The node runs the join
	// exchange again before then. The protocol has no other way to renew
	// them.
	ExpiresAt fractal.Timestamp `json:"expiresAt"`
	// Endpoint is the address this node sent its join request to.
	Endpoint Endpoint `json:"endpoint"`
	// Binding is extra data from the answer that the transport binding needs
	// later. This package stores the bytes and never reads them.
	Binding json.RawMessage `json:"binding,omitempty"`
	// Extensions is the join data of the protocol extensions this node runs,
	// from the last grant, keyed by extension name. This package stores the
	// bytes and never reads them.
	Extensions map[fractal.Identifier]json.RawMessage `json:"extensions,omitempty"`
}

// MembershipStore keeps the Membership, so a node has it again after a
// restart.
type MembershipStore interface {
	// LoadMembership returns the saved Membership. ok is false when none was
	// ever saved. A store that cannot read its data must return an error.
	// Without a Membership the node would accept a grant from any node.
	LoadMembership(ctx context.Context) (m Membership, ok bool, err error)
	// SaveMembership writes the Membership and replaces the saved one. When
	// it returns nil, the record survives a restart.
	SaveMembership(ctx context.Context, m Membership) error
}

// MemoryMembershipStore keeps the Membership in memory and loses it on a
// restart. Its zero value is ready and has no Membership.
type MemoryMembershipStore struct {
	mu     sync.Mutex
	held   Membership
	filled bool
	// SaveErr, when set, is returned by every call to SaveMembership.
	SaveErr error
	// LoadErr, when set, is returned by every call to LoadMembership.
	LoadErr error
}

// LoadMembership returns the Membership the last SaveMembership wrote.
func (s *MemoryMembershipStore) LoadMembership(context.Context) (Membership, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.LoadErr != nil {
		return Membership{}, false, s.LoadErr
	}
	return s.held, s.filled, nil
}

// SaveMembership writes the membership.
func (s *MemoryMembershipStore) SaveMembership(_ context.Context, m Membership) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.SaveErr != nil {
		return s.SaveErr
	}
	s.held, s.filled = m, true
	return nil
}
