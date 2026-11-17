package authz

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// mayStandDown reads the graph the mutation produces and refuses one that
// leaves the caller unable to administer the object. It reads it inside the
// writing transaction, which is what makes the answer the caller acts on the
// same one the write lands against.
//
// That holds only while the transaction is the unit two writers serialise on.
// The epoch row lock is what serialises them, and it was taken after the rule
// had already decided. On Postgres a transaction opens at READ COMMITTED, so
// two callers each read a snapshot with the other's write absent from it, each
// rule passes, and both writes land. The object is then administered by
// nobody, and nothing can put an administrator back: writing any grant on it
// takes an action the projection no longer confers to anybody.
//
// The device carries no org, because an org tuple is a second path to the
// object and would hide the case behind it.
func TestTwoConcurrentWithdrawalsCannotStrandTheObject(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		const rounds = 25

		stranded := 0

		// Each round is a subtest of its own so the database it opens is torn
		// down when the round ends. Registered on this t, every round's
		// cleanup would wait for the last one, and on Postgres that is three
		// sessions a round held for the whole run: the admin handle's idle
		// connection and the pool of two. The CI fixture allows a hundred
		// sessions for the whole parallel suite, and this one test held
		// seventy-three of them.
		for i := range rounds {
			t.Run(fmt.Sprintf("round-%02d", i), func(t *testing.T) {
				if withdrawalsStrand(t, e) {
					stranded++
				}
			})
		}

		require.Zerof(t, stranded,
			"%d of %d rounds left the device with no administrator, and nothing can put one back",
			stranded, rounds)
	})
}

// withdrawalsStrand runs one round on a fresh store and reports whether the
// two withdrawals left the device with no administrator.
func withdrawalsStrand(t *testing.T, e engine) bool {
	t.Helper()

	store := newStore(t, e)
	require.NoError(t, store.UpsertDevices(t.Context(), DeviceRow{ID: "p-x", Name: "X"}))
	require.NoError(t, store.Write(t.Context(), mustTuples(t,
		"device:p-x#manager@user:m1",
		"device:p-x#manager@user:m2",
	)...))

	m1, m2 := subjectOf(t, "user:m1"), subjectOf(t, "user:m2")
	device := deviceOf(t, "p-x")

	// Both tuples are built on the test's own goroutine: mustTuple asserts,
	// and an assertion inside a goroutine is not the test's to make.
	dropM2 := mustTuple(t, "device:p-x#manager@user:m2")
	dropM1 := mustTuple(t, "device:p-x#manager@user:m1")

	var wg sync.WaitGroup

	wg.Add(2)

	go func() {
		defer wg.Done()

		_ = store.WithdrawGrant(t.Context(), m1, dropM2)
	}()

	go func() {
		defer wg.Done()

		_ = store.WithdrawGrant(t.Context(), m2, dropM1)
	}()

	wg.Wait()

	first, err := store.Can(t.Context(), m1, ActionAdminister, device)
	require.NoError(t, err)

	second, err := store.Can(t.Context(), m2, ActionAdminister, device)
	require.NoError(t, err)

	return !first && !second
}
