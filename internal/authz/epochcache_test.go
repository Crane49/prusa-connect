package authz

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// The epoch row lock orders the transactions, so they commit in the order they
// bumped the counter. It does not order what happens after the commit returns.
// Two writers can be descheduled between their commit and their store, and the
// later one can publish the lower value.
//
// A cache left a generation behind stays behind, because nothing reads the row
// again until the next write. The stream hub takes every subscriber's re-read
// decision off this value, so a subscriber keeps a grant that was withdrawn
// until somebody happens to write to the graph again.
//
// The out-of-order pair is applied directly rather than raced, because a race
// reproduces the defect only when the scheduler cooperates and a test that
// depends on that is a test that passes for the wrong reason.
func TestTheCachedEpochNeverMovesBackwards(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		store.publishEpoch(5)
		require.EqualValues(t, 5, store.Epoch())

		store.publishEpoch(3)
		require.EqualValues(t, 5, store.Epoch(),
			"a writer that committed earlier published its generation last and the cache took it")

		store.publishEpoch(6)
		require.EqualValues(t, 6, store.Epoch(), "the cache stopped moving forwards")
	})
}

// The invariant under contention: whatever order the writers publish in, the
// cache ends on the highest generation any of them produced.
func TestConcurrentWritersLeaveTheCacheOnTheHighestGeneration(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		const writers = 64

		var wg sync.WaitGroup

		wg.Add(writers)

		for i := range writers {
			go func() {
				defer wg.Done()

				store.publishEpoch(uint64(writers - i))
			}()
		}

		wg.Wait()

		require.EqualValues(t, writers, store.Epoch())
	})
}

// The cache still holds what the graph actually did. A mutation moves the row
// and the cache together, so a reader that trusts the cache is not reading a
// generation the database never had.
func TestTheCachedEpochMatchesTheRowAfterAWrite(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		require.NoError(t, store.Write(t.Context(), mustTuple(t, "device:p-delta#viewer@user:bob")))

		row, err := store.readEpoch(t.Context(), store.db)
		require.NoError(t, err)
		require.Equal(t, row, store.Epoch())
	})
}
