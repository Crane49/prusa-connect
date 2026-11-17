package xbuddy

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/admission"
)

// contentionBound is how long a case here waits for work that is not blocked to
// finish. A run that finds no contention spends none of it, and a run that finds
// some spends all of it once and fails with a sentence.
const contentionBound = 5 * time.Second

// gatedStore holds its first write until the case lets it go, which is what a
// busy database or a disk under a fleet powering up looks like from the table.
// Every write after it runs at once.
type gatedStore struct {
	mu     sync.Mutex
	rows   map[string]admission.Record
	writes int

	// entered closes when the first write is inside the store, and release lets
	// it finish.
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newGatedStore() *gatedStore {
	return &gatedStore{
		rows:    make(map[string]admission.Record),
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (g *gatedStore) Load(context.Context, string) ([]admission.Record, error) { return nil, nil }

func (g *gatedStore) Save(ctx context.Context, record admission.Record) error {
	if g.hold() {
		select {
		case <-g.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	g.rows[record.Code] = record

	return nil
}

func (g *gatedStore) Forget(_ context.Context, _, code string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	delete(g.rows, code)

	return nil
}

// hold reports whether this is the write the case is holding, and tells the
// case it has arrived.
func (g *gatedStore) hold() bool {
	g.mu.Lock()
	g.writes++
	first := g.writes == 1
	g.mu.Unlock()

	if first {
		close(g.entered)
	}

	return first
}

// let finishes the write this store is holding. It is safe to call twice, so a
// case that gives up early still leaves the bubble with nothing blocked in
// here: a goroutine inside the store when the case ends is a run that hangs
// rather than one that fails.
func (g *gatedStore) let() {
	g.once.Do(func() { close(g.release) })
}

// awaitEmptyTable waits until no registration is waiting for an operator. It
// polls rather than synchronising, because what it is waiting for is a lock
// taken and released on another goroutine and there is nothing to wait on.
func awaitEmptyTable(t *testing.T, registry *registrar) {
	t.Helper()

	for giveUp := time.Now().Add(contentionBound); time.Now().Before(giveUp); {
		if len(registry.pending()) == 0 {
			return
		}

		time.Sleep(time.Millisecond)
	}

	t.Fatal("the rejected registration is still in the table")
}

// held is how many rows the store holds.
func (g *gatedStore) held() int {
	g.mu.Lock()
	defer g.mu.Unlock()

	return len(g.rows)
}

// A store that is slow about one printer holds up that printer and nobody else.
//
// The table is one lock over four hundred machines, so a write made while it is
// held stops every other printer's registration, every websocket credential
// check and the operator's listing for as long as the store takes. A fleet
// powering up after a power cut is four hundred writes at once.
func TestASlowStoreWriteDoesNotHoldUpAnotherPrintersHandshake(t *testing.T) {
	t.Parallel()

	store := newGatedStore()
	defer store.let()

	registry := newRegistrar(ModeToken, 0)
	require.NoError(t, registry.restore(t.Context(), "xbuddy", "printer", store))

	slow := make(chan error, 1)

	go func() {
		_, _, err := registry.begin(t.Context(), Registration{Fingerprint: "FP-SLOW"})
		slow <- err
	}()

	// From here the first printer's row is inside the store and has not
	// landed.
	<-store.entered

	quick := make(chan error, 1)

	go func() {
		_, _, err := registry.begin(t.Context(), Registration{Fingerprint: "FP-QUICK"})
		quick <- err
	}()

	// Real time, and not a synctest bubble: what a second printer waits on
	// here is a mutex, which the bubble's clock does not count as blocked and
	// so never advances past. A green run takes none of this bound.
	select {
	case err := <-quick:
		require.NoError(t, err)
	case <-time.After(contentionBound):
		t.Fatal("a second printer's registration waited on a store write about another printer, " +
			"so the table is still locked across the store")
	}

	// And the table answers the rest of the handshake while the write is in
	// flight: the credential check a websocket makes and the listing an
	// operator reads take the same lock.
	_, err := registry.authenticate("FP-QUICK", "")
	require.Error(t, err, "an unapproved printer was let through")
	require.Len(t, registry.pending(), 2,
		"a registration whose row has not landed is missing from the table")

	store.let()
	require.NoError(t, <-slow)

	require.Equal(t, 2, store.held(), "the store holds %d rows, want both printers", store.held())

	t.Log("observed: a second printer registered, authenticated and was listed while the " +
		"first printer's row was still being written")
}

// Two decisions about one registration reach the store in the order the table
// took them.
//
// The rejection is taken while the save in front of it is still in the store.
// Letting it overtake would delete a row that had not been written and then
// write it, so a printer an operator turned away comes back at the next
// restart.
func TestARejectionIsNotOvertakenByTheWriteInFrontOfIt(t *testing.T) {
	t.Parallel()

	store := newGatedStore()
	defer store.let()

	registry := newRegistrar(ModeToken, 0)
	require.NoError(t, registry.restore(t.Context(), "xbuddy", "printer", store))

	registered := make(chan error, 1)

	go func() {
		_, _, err := registry.begin(t.Context(), Registration{Fingerprint: "FP-1"})
		registered <- err
	}()

	<-store.entered

	// The code comes off the table rather than out of begin, which has not
	// returned: its row is the one being written.
	waiting := registry.pending()
	require.Len(t, waiting, 1)

	rejected := make(chan error, 1)

	go func() { rejected <- registry.Reject(t.Context(), waiting[0].Code) }()

	// The rejection has taken the entry out of the table and is waiting for
	// the save in front of it.
	awaitEmptyTable(t, registry)

	store.let()
	require.NoError(t, <-registered)
	require.NoError(t, <-rejected)

	require.Zero(t, store.held(),
		"the store holds the rejected registration, so the save it overtook wrote the row back")

	t.Log("observed: the removal waited for the save in front of it and the store holds nothing")
}
