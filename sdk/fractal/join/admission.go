package join

import (
	"context"
	"slices"
	"sync"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// Admission is the record a Parent keeps for one node it has admitted. A
// Parent keeps the record of a revoked node too.
type Admission struct {
	// NodeID is the Node ID the admitted node uses under this Parent.
	NodeID fractal.NodeID `json:"nodeId"`
	// Identity is the authentication identity that owns that Node ID. The
	// Parent denies any other identity that asks for the same Node ID.
	Identity fractal.Identity `json:"identity"`
	// Revoked is true when an operator has revoked the admission. The Parent
	// then denies every request for this Node ID, from any identity, and
	// issues no credentials for it.
	Revoked bool `json:"revoked"`
	// EverConnected is true when the node has completed a connection at least
	// once. A transport binding sends no loss message for a node that has
	// never connected.
	EverConnected bool `json:"everConnected"`
}

// AdmissionStore keeps the admission records, so a Parent has them again after
// a restart.
type AdmissionStore interface {
	// LoadAdmissions returns every saved record. A store that cannot read
	// its data must return an error, never an empty set. With an empty set
	// the Parent would admit a revoked node again.
	LoadAdmissions(ctx context.Context) ([]Admission, error)
	// SaveAdmission writes one record and replaces the record with the same
	// Node ID. When it returns nil, the record survives a restart.
	SaveAdmission(ctx context.Context, a Admission) error
}

// MemoryAdmissionStore keeps the records in memory and loses them on a
// restart. It is for tests, and for a deployment that accepts that loss. Its
// zero value is ready.
type MemoryAdmissionStore struct {
	mu   sync.Mutex
	byID map[fractal.NodeID]Admission
	// SaveErr, when set, is returned by every call to SaveAdmission.
	SaveErr error
	// LoadErr is returned by every call to LoadAdmissions when it is set.
	LoadErr error
}

// LoadAdmissions returns every record, ordered by Node ID.
func (s *MemoryAdmissionStore) LoadAdmissions(context.Context) ([]Admission, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.LoadErr != nil {
		return nil, s.LoadErr
	}
	out := make([]Admission, 0, len(s.byID))
	for _, a := range s.byID {
		out = append(out, a)
	}
	slices.SortFunc(out, func(x, y Admission) int { return compareNodeID(x.NodeID, y.NodeID) })
	return out, nil
}

// SaveAdmission writes one record.
func (s *MemoryAdmissionStore) SaveAdmission(_ context.Context, a Admission) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.SaveErr != nil {
		return s.SaveErr
	}
	if s.byID == nil {
		s.byID = map[fractal.NodeID]Admission{}
	}
	s.byID[a.NodeID] = a
	return nil
}

// compareNodeID orders two Node IDs as text, so a listing is stable.
func compareNodeID(x, y fractal.NodeID) int {
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}
