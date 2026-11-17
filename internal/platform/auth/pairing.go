package auth

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"
)

// Errors a pairing store reports. A caller inspects them with errors.Is. Any
// other error a store returns means it could not answer.
var (
	// ErrNoPairing reports a subject no administrator has paired.
	ErrNoPairing = errors.New("auth: no pairing for that subject")
	// ErrPairingExists reports a subject that is already paired. A subject
	// speaks for one principal, and changing which one means unpairing and
	// pairing again, so a second write is refused and never replaces the row.
	ErrPairingExists = errors.New("auth: the subject is already paired")
	// ErrInvalidPairing reports a pairing with a field missing.
	ErrInvalidPairing = errors.New("auth: the pairing is incomplete")
)

// Pairing maps one subject an identity provider names onto one local
// principal. A verified token whose subject has no row is refused, and an
// administrator writes the row. Nothing is created on first sight.
//
// Provider is the plugin type that vouches for the subject, so two providers
// that number their subjects alike cannot collide. Principal is the full graph
// subject, user:<id> or service_account:<id>, so a token can act for a service
// account. No device is named here: a person and a device are never linked.
type Pairing struct {
	Provider  string
	Subject   string
	Principal string
	// Label is what an administrator wrote to recognise the row in a
	// listing, and nothing selects on it.
	Label     string
	CreatedAt time.Time
	// Generation is the principal's generation when the pairing was checked
	// for, before it was saved. A password change raises the principal's
	// generation, and a pairing carrying an older one is refused.
	Generation int64
	// CurrentGeneration is the principal's generation now, read beside the
	// row. A lookup fills it and a save ignores it.
	CurrentGeneration int64
	// CreatedBy is the graph subject of the person who wrote the pairing. It
	// is empty for a pairing an older build wrote. A password change of that
	// person ends the pairing, whatever principal it names.
	CreatedBy string
	// CreatorGeneration is the creator's generation when the pairing was
	// checked for, and CreatorCurrentGeneration the one now, read beside the
	// row.
	CreatorGeneration        int64
	CreatorCurrentGeneration int64
	// RootCreator is the person at the top of the chain that wrote the
	// pairing. A pairing written with a paired token names the root of that
	// pairing, so the root's password change ends the whole chain. It is
	// empty for a pairing an older build wrote.
	RootCreator string
	// RootGeneration is the root's generation when the pairing was checked
	// for, and RootCurrentGeneration the one now, read beside the row.
	RootGeneration        int64
	RootCurrentGeneration int64
}

// Root is the person a token resolved through this pairing acts under, for a
// mint or a pairing the token then writes. It falls back to the creator and
// then to the principal for a pairing an older build wrote.
func (p Pairing) Root() string {
	switch {
	case p.RootCreator != "":
		return p.RootCreator
	case p.CreatedBy != "":
		return p.CreatedBy
	default:
		return p.Principal
	}
}

// Validate refuses a pairing with a field missing. Whether a principal is
// allowed is the caller's question, asked against the authorization vocabulary.
// This only says whether a store can key on the row.
func (p Pairing) Validate() error {
	switch {
	case strings.TrimSpace(p.Provider) == "":
		return errors.Join(ErrInvalidPairing, errors.New("no provider"))
	case strings.TrimSpace(p.Subject) == "":
		return errors.Join(ErrInvalidPairing, errors.New("no subject"))
	case strings.TrimSpace(p.Principal) == "":
		return errors.Join(ErrInvalidPairing, errors.New("no principal"))
	}

	return nil
}

// Pairings holds the pairing rows. It is a port, like Store: the in-memory one
// here serves the tests, and a persistent one plugs in without this package
// changing.
//
// Lookup reports a miss as ErrNoPairing and Save a duplicate as
// ErrPairingExists. Every other error means the store could not answer, and a
// verify treats that as a refusal.
type Pairings interface {
	Lookup(ctx context.Context, provider, subject string) (Pairing, error)
	Save(ctx context.Context, pairing Pairing) error
	// List returns every pairing, or those of one provider when provider is
	// not empty, ordered by provider then subject.
	List(ctx context.Context, provider string) ([]Pairing, error)
	// Delete removes one pairing. Removing one that is not there is
	// ErrNoPairing, so a caller can say whether anything changed.
	Delete(ctx context.Context, provider, subject string) error
	// DeletePrincipal removes every pairing to one principal whose generation
	// is below the principal's current one, and reports how many. A password
	// change calls it after raising the generation, so an outside account
	// somebody paired to the person stops acting as them. A pairing written
	// after the change stays.
	DeletePrincipal(ctx context.Context, principal string) (int, error)
	// DeleteCreatedBy removes every pairing one person wrote, or whose chain
	// that person is the root of, below that person's current generation, and
	// reports how many. A password change calls it, so a pairing written with
	// a stolen session for somebody else ends too, however long the chain.
	DeleteCreatedBy(ctx context.Context, creator string) (int, error)
}

// MemoryPairings keeps pairings for the life of the process, for tests and a
// single-process install.
type MemoryPairings struct {
	mu   sync.RWMutex
	rows map[pairingKey]Pairing
	// generations is each principal's current generation, which Bump raises.
	generations map[string]int64
	// fail makes every method report a store that cannot answer. Only Fail
	// sets it, and no configuration can.
	fail error
}

type pairingKey struct{ provider, subject string }

// NewMemoryPairings returns an empty store.
func NewMemoryPairings() *MemoryPairings {
	return &MemoryPairings{rows: make(map[pairingKey]Pairing), generations: make(map[string]int64)}
}

// Fail makes every later call report err, so a test can run the fail-closed
// path.
func (m *MemoryPairings) Fail(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.fail = err
}

// Lookup returns the pairing for a subject, or ErrNoPairing.
func (m *MemoryPairings) Lookup(_ context.Context, provider, subject string) (Pairing, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.fail != nil {
		return Pairing{}, m.fail
	}

	row, ok := m.rows[pairingKey{provider, subject}]
	if !ok {
		return Pairing{}, ErrNoPairing
	}

	row.CurrentGeneration = m.generations[row.Principal]
	if row.CreatedBy != "" {
		row.CreatorCurrentGeneration = m.generations[row.CreatedBy]
	}

	if row.RootCreator != "" {
		row.RootCurrentGeneration = m.generations[row.RootCreator]
	}

	return row, nil
}

// Bump raises a subject's generation by one, which is what a password write
// does in the database. The tests call it where a database would.
func (m *MemoryPairings) Bump(subject string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.generations[subject]++
}

// DeleteCreatedBy removes every pairing one person wrote below their current
// generation and reports how many.
func (m *MemoryPairings) DeleteCreatedBy(_ context.Context, creator string) (int, error) {
	if err := ValidSubject(creator); err != nil {
		return 0, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.fail != nil {
		return 0, m.fail
	}

	removed := 0

	for key, row := range m.rows {
		byCreator := row.CreatedBy == creator && row.CreatorGeneration < m.generations[creator]
		byRoot := row.RootCreator == creator && row.RootGeneration < m.generations[creator]

		if byCreator || byRoot {
			delete(m.rows, key)

			removed++
		}
	}

	return removed, nil
}

// Save writes a pairing, refusing a subject already paired.
func (m *MemoryPairings) Save(_ context.Context, pairing Pairing) error {
	if err := pairing.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.fail != nil {
		return m.fail
	}

	key := pairingKey{pairing.Provider, pairing.Subject}
	if _, taken := m.rows[key]; taken {
		return ErrPairingExists
	}

	m.rows[key] = pairing

	return nil
}

// List returns the pairings, ordered by provider then subject.
func (m *MemoryPairings) List(_ context.Context, provider string) ([]Pairing, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.fail != nil {
		return nil, m.fail
	}

	out := make([]Pairing, 0, len(m.rows))

	for key, row := range m.rows {
		if provider == "" || key.provider == provider {
			out = append(out, row)
		}
	}

	slices.SortFunc(out, comparePairings)

	return out, nil
}

// Delete removes one pairing, or reports ErrNoPairing.
func (m *MemoryPairings) Delete(_ context.Context, provider, subject string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.fail != nil {
		return m.fail
	}

	key := pairingKey{provider, subject}
	if _, ok := m.rows[key]; !ok {
		return ErrNoPairing
	}

	delete(m.rows, key)

	return nil
}

// DeletePrincipal removes every pairing to one principal below its current
// generation and reports how many.
func (m *MemoryPairings) DeletePrincipal(_ context.Context, principal string) (int, error) {
	if err := ValidSubject(principal); err != nil {
		return 0, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.fail != nil {
		return 0, m.fail
	}

	removed := 0

	for key, row := range m.rows {
		if row.Principal == principal && row.Generation < m.generations[principal] {
			delete(m.rows, key)

			removed++
		}
	}

	return removed, nil
}

// comparePairings orders a listing by provider then subject, so two reads
// render in one order.
func comparePairings(a, b Pairing) int {
	if by := strings.Compare(a.Provider, b.Provider); by != 0 {
		return by
	}

	return strings.Compare(a.Subject, b.Subject)
}
