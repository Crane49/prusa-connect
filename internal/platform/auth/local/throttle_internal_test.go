package local

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The failure record does not decide who is admitted: a correct password is
// verified before the record is consulted at all, because a record keyed by the
// account id cannot tell the owner from whoever is guessing at them. What is
// left to hold it to is what it costs: one bounded, self-draining entry per
// account that has failed recently, and nothing at all for one that has not.
//
// That is what MaxFailures and FailureWindow now name, and none of it is
// reachable through VerifyCredentials any more, which is why these tests are
// inside the package rather than beside the others.

// recordEpoch is where the hand-driven times below start.
var recordEpoch = time.Date(2026, time.August, 27, 9, 0, 0, 0, time.UTC)

// heldBy reports how many failures the record holds for an account at now. It
// prunes as it reads, which is what the record itself does when it is written.
func heldBy(t *testing.T, tr *throttle, id string, now time.Time) int {
	t.Helper()

	tr.mu.Lock()
	defer tr.mu.Unlock()

	return len(tr.prune(id, now))
}

// TestTheFailureRecordDrainsWithTheWindow keeps a failure from being remembered
// forever. Nothing sweeps this map on a timer, so a window that never drained
// would be one entry per account ever guessed at, held for the life of the
// process.
func TestTheFailureRecordDrainsWithTheWindow(t *testing.T) {
	t.Parallel()

	tr := newThrottle()

	for range MaxFailures {
		tr.record("account", recordEpoch)
	}

	require.Equal(t, MaxFailures, heldBy(t, tr, "account", recordEpoch),
		"a failure was not recorded")
	require.Equal(t, MaxFailures, heldBy(t, tr, "account", recordEpoch.Add(FailureWindow/2)),
		"the window drained early")

	require.Zero(t, heldBy(t, tr, "account", recordEpoch.Add(FailureWindow)),
		"the window never drained, so a failure is remembered for the life of the process")
	require.Empty(t, tr.failed, "a drained window left the account's entry in the map")
}

// TestGuessingOnDoesNotHoldTheRecordOpen is the shape a naive counter gets
// wrong. Appending a failure to a record that is already full would push the
// window's end forward on every attempt, so somebody guessing on would hold an
// entry per account for as long as they kept going rather than for a quarter of
// an hour.
func TestGuessingOnDoesNotHoldTheRecordOpen(t *testing.T) {
	t.Parallel()

	tr := newThrottle()

	for range MaxFailures {
		tr.record("account", recordEpoch)
	}

	// Somebody guessing on, spread across the window rather than at its start.
	for step := range 9 {
		tr.record("account", recordEpoch.Add(FailureWindow/10*time.Duration(step+1)))
	}

	require.Zero(t, heldBy(t, tr, "account", recordEpoch.Add(FailureWindow)),
		"guessing on held the record open past the window's length")
	require.Empty(t, tr.failed, "guessing on left the account's entry in the map")
}

// TestClearingForgetsAnAccountsFailures is the record's half of what a correct
// password and an administrator reset do. NIST SP 800-63B section 5.2.2 asks a
// verifier to disregard earlier failed attempts once the subscriber
// authenticates.
func TestClearingForgetsAnAccountsFailures(t *testing.T) {
	t.Parallel()

	tr := newThrottle()

	for range MaxFailures {
		tr.record("account", recordEpoch)
	}

	tr.clear("account")

	require.Zero(t, heldBy(t, tr, "account", recordEpoch), "a cleared account still holds failures")
	require.Empty(t, tr.failed, "a cleared account left an entry in the map")
}

// TestAnAccountThatHasNotFailedHoldsNothing keeps reading the record from being
// a way to grow it. A caller sending names at random must leave nothing behind.
func TestAnAccountThatHasNotFailedHoldsNothing(t *testing.T) {
	t.Parallel()

	tr := newThrottle()

	require.Zero(t, heldBy(t, tr, "nobody", recordEpoch))
	require.Empty(t, tr.failed, "reading an account's record wrote one")
}
