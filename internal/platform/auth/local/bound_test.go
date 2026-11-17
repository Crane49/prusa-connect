package local_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/buildprofile"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/local"
)

// holdingStore is an account store whose ByID blocks once a test arms it. A
// login reads the row by id after it has taken a hashing slot, so a blocked
// read is a slot held for as long as the test wants.
type holdingStore struct {
	*local.MemoryStore

	armed   atomic.Bool
	reads   atomic.Int32
	arrived chan struct{}
	release chan struct{}
}

func newHoldingStore() *holdingStore {
	return &holdingStore{
		MemoryStore: local.NewMemoryStore(),
		arrived:     make(chan struct{}, 1),
		release:     make(chan struct{}),
	}
}

func (h *holdingStore) ByID(ctx context.Context, id string) (local.Account, error) {
	if h.armed.Load() {
		h.reads.Add(1)

		if h.reads.Load() == 1 {
			h.arrived <- struct{}{}
			<-h.release
		}
	}

	return h.MemoryStore.ByID(ctx, id)
}

// secondHashWaits holds one login inside its hashing slot and starts a
// second. The second waits for a slot until its own deadline ends, and never
// reaches the account row. Once the first login leaves, a login goes through.
func secondHashWaits(t *testing.T, store *holdingStore, accounts *local.Accounts) {
	t.Helper()

	setUp(t, accounts)
	store.armed.Store(true)

	first := make(chan error, 1)

	go func() {
		_, err := accounts.VerifyCredentials(context.WithoutCancel(t.Context()), "owner@example.com", goodPassword)
		first <- err
	}()

	select {
	case <-store.arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("the first login never took a hashing slot")
	}

	waiting, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	_, err := accounts.VerifyCredentials(waiting, "owner@example.com", goodPassword)
	require.ErrorIs(t, err, context.DeadlineExceeded, "a second login ran while the first held the only slot")
	require.Equal(t, int32(1), store.reads.Load(), "the second login read the account row without a slot")

	close(store.release)
	require.NoError(t, <-first)

	_, err = accounts.VerifyCredentials(t.Context(), "owner@example.com", goodPassword)
	require.NoError(t, err, "a login after the first one left was refused")
}

// TestASecondHashWaitsWhileTheBoundIsOne is the bound the printer runs with.
// One argon2id pass at a time, and a second login waits within its request
// deadline instead of taking a second memory block.
func TestASecondHashWaitsWhileTheBoundIsOne(t *testing.T) {
	t.Parallel()

	store := newHoldingStore()
	secondHashWaits(t, store, local.New(store, nil, local.WithHashesAtOnce(1)))
}

// TestTheHashBoundTakesItsSetting holds a larger bound to its value. With two
// slots and one held, a second login goes through at once.
func TestTheHashBoundTakesItsSetting(t *testing.T) {
	t.Parallel()

	store := newHoldingStore()
	accounts := local.New(store, nil, local.WithHashesAtOnce(2))
	setUp(t, accounts)
	store.armed.Store(true)

	first := make(chan error, 1)

	go func() {
		_, err := accounts.VerifyCredentials(context.WithoutCancel(t.Context()), "owner@example.com", goodPassword)
		first <- err
	}()

	<-store.arrived

	waiting, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	_, err := accounts.VerifyCredentials(waiting, "owner@example.com", goodPassword)
	require.NoError(t, err, "a second login waited although a second slot was free")

	close(store.release)
	require.NoError(t, <-first)
}

// TestABoundBelowOneKeepsTheDefault covers a value that would leave no slot
// at all, which would stop every login.
func TestABoundBelowOneKeepsTheDefault(t *testing.T) {
	t.Parallel()

	accounts := local.New(local.NewMemoryStore(), nil, local.WithHashesAtOnce(0))
	setUp(t, accounts)

	waiting, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	_, err := accounts.VerifyCredentials(waiting, "owner@example.com", goodPassword)
	require.NoError(t, err)
}

// A build bounds the hashes by its own profile when the configuration names
// no bound. The printer build runs one at a time, so a second login waits.
// make test-printer runs this under the printer tag.
func TestTheBuildBoundsHashesByItsProfile(t *testing.T) {
	t.Parallel()

	if buildprofile.Name != "printer" {
		require.Equal(t, 4, local.DefaultHashesAtOnce)

		return
	}

	require.Equal(t, 1, local.DefaultHashesAtOnce)

	store := newHoldingStore()
	secondHashWaits(t, store, local.New(store, nil))
}
