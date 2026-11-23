package auth

import (
	"context"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
)

// MemoryStore keeps credentials for the life of the process. A single-node
// install and the tests run on it. A credential does not survive a restart: a
// session then needs a new login, and an api token needs a persistent store in
// place of this one.
type MemoryStore struct {
	mu   sync.RWMutex
	rows map[string]Credential
	// generations is each subject's current generation. A subject that is not
	// here is at zero.
	generations map[string]int64
	// fail makes every method report a store that cannot answer, so a test can
	// run the fail-closed path. Only Break sets it, and no configuration can.
	fail error
}

// NewMemoryStore returns an empty store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{rows: make(map[string]Credential), generations: make(map[string]int64)}
}

// Lookup returns the row for a public id, or ErrNoCredential.
func (s *MemoryStore) Lookup(_ context.Context, publicID string) (Credential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.fail != nil {
		return Credential{}, s.fail
	}

	cred, ok := s.rows[publicID]
	if !ok {
		return Credential{}, ErrNoCredential
	}

	cred.CurrentGeneration = s.generations[cred.Subject]
	if cred.MintedBy != "" {
		cred.MinterCurrentGeneration = s.generations[cred.MintedBy]
	}

	if cred.RootCreator != "" {
		cred.RootCurrentGeneration = s.generations[cred.RootCreator]
	}

	return cred, nil
}

// Save writes a row.
func (s *MemoryStore) Save(_ context.Context, cred Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fail != nil {
		return s.fail
	}

	cred.Scopes = slices.Clone(cred.Scopes)
	s.rows[cred.PublicID] = cred

	return nil
}

// Touch slides a row's idle window.
func (s *MemoryStore) Touch(_ context.Context, publicID string, seen time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fail != nil {
		return s.fail
	}

	cred, ok := s.rows[publicID]
	if !ok {
		return ErrNoCredential
	}

	cred.LastSeen = seen
	s.rows[publicID] = cred

	return nil
}

// Revoke deletes a row. Deleting one that is not there is not an error: the
// caller wanted it gone and it is gone.
func (s *MemoryStore) Revoke(_ context.Context, publicID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fail != nil {
		return s.fail
	}

	delete(s.rows, publicID)

	for id, row := range s.rows {
		// A row with no parent is no page session of an empty id.
		if row.Parent != "" && row.Parent == publicID {
			delete(s.rows, id)
		}
	}

	return nil
}

// Children returns the rows that name parent, oldest first.
func (s *MemoryStore) Children(_ context.Context, parent string) ([]Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fail != nil {
		return nil, s.fail
	}

	out := make([]Credential, 0)

	for _, row := range s.rows {
		if parent != "" && row.Parent == parent {
			row.Scopes = slices.Clone(row.Scopes)
			out = append(out, row)
		}
	}

	slices.SortFunc(out, func(a, b Credential) int {
		if c := a.IssuedAt.Compare(b.IssuedAt); c != 0 {
			return c
		}

		return strings.Compare(a.PublicID, b.PublicID)
	})

	return out, nil
}

// RevokeSubject deletes every row held for one subject and reports how many it
// removed. Removing none is not an error, for the reason Revoke gives.
func (s *MemoryStore) RevokeSubject(_ context.Context, subject string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fail != nil {
		return 0, s.fail
	}

	removed := 0

	for publicID, cred := range s.rows {
		if cred.Subject == subject {
			delete(s.rows, publicID)

			removed++
		}
	}

	return removed, nil
}

// Generation returns a subject's current generation.
func (s *MemoryStore) Generation(_ context.Context, subject string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.fail != nil {
		return 0, s.fail
	}

	return s.generations[subject], nil
}

// Bump raises each subject's generation by one, which is what a password write
// does in the database. The tests call it where a database would.
func (s *MemoryStore) Bump(subjects ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, subject := range subjects {
		s.generations[subject]++
	}
}

// EndSubjects deletes every row of each subject below its current generation,
// and every row a subject minted below its current generation.
func (s *MemoryStore) EndSubjects(_ context.Context, subjects []string) (Ended, error) {
	for _, subject := range subjects {
		if err := ValidSubject(subject); err != nil {
			return Ended{}, err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fail != nil {
		return Ended{}, s.fail
	}

	removed := Ended{Held: make([]int, len(subjects))}

	for i, subject := range subjects {
		for publicID, cred := range s.rows {
			if cred.Subject == subject && cred.Generation < s.generations[subject] {
				delete(s.rows, publicID)

				removed.Held[i]++
			}
		}
	}

	for publicID, cred := range s.rows {
		byMinter := cred.MintedBy != "" && slices.Contains(subjects, cred.MintedBy) &&
			cred.MinterGeneration < s.generations[cred.MintedBy]
		byRoot := cred.RootCreator != "" && slices.Contains(subjects, cred.RootCreator) &&
			cred.RootGeneration < s.generations[cred.RootCreator]

		if byMinter || byRoot {
			delete(s.rows, publicID)

			removed.MintedForOthers++
		}
	}

	return removed, nil
}

// ListSubject returns every row held for one subject, oldest first.
func (s *MemoryStore) ListSubject(_ context.Context, subject string) ([]Credential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.fail != nil {
		return nil, s.fail
	}

	held := make([]Credential, 0)

	for _, cred := range s.rows {
		if cred.Subject == subject {
			held = append(held, cred)
		}
	}

	// A map has no order, and a listing that changed order between two reads
	// would look like a change to the reader.
	slices.SortFunc(held, func(a, b Credential) int {
		if by := a.IssuedAt.Compare(b.IssuedAt); by != 0 {
			return by
		}

		return strings.Compare(a.PublicID, b.PublicID)
	})

	return held, nil
}

// Break makes every method report err, standing in for a store or an issuer
// this node cannot reach. Passing nil restores it.
func (s *MemoryStore) Break(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.fail = err
}

// Rows returns a copy of what is held, for a test that asserts on the store
// itself.
func (s *MemoryStore) Rows() map[string]Credential {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return maps.Clone(s.rows)
}
