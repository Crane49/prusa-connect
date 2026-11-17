package local_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/local"
	"github.com/prusa3d/prusa-connect/sdk/authapi"
)

// TestEveryRefusedLoginCostsTheSame is the timing defence over the whole login
// path, and it is the reason VerifyCredentials runs the dummy hash on branches
// that never touch a stored one.
//
// Three refusals are timed against each other. A wrong password on an account
// that exists is the baseline. A username nobody holds must cost the same, or
// the response time enumerates accounts. An account whose failure budget is
// spent must cost the same as well: a refusal that skipped the derivation once
// the record was full would make the record itself the oracle, because a fast
// refusal says the account exists and has been guessed at. That branch now
// derives the account's own hash rather than the dummy, because the password is
// checked before the record is consulted, so it is the same work by the same
// route rather than the same work by a parallel one.
//
// It does not call t.Parallel, and it compares the branches round by round.
// See pairedMedian for why, and for what the fastest-of-each shape it replaces
// did on a two-core runner. It runs at the default cost, at the printer's,
// which makes more passes over less memory, and at a much larger cost.
func TestEveryRefusedLoginCostsTheSame(t *testing.T) {
	// 64 MiB with 3 passes is far from either default. An unknown username
	// checked against a default dummy hash instead of the configured one
	// answers in a fraction of the time there, which the ratio catches.
	costs := []auth.Cost{auth.DefaultCost(), {MemoryKiB: 7 * 1024, Passes: 5}, {MemoryKiB: 64 * 1024, Passes: 3}}

	for _, cost := range costs {
		t.Run(fmt.Sprintf("%d KiB with %d passes", cost.MemoryKiB, cost.Passes), func(t *testing.T) {
			hasher, err := auth.NewHasher(cost)
			require.NoError(t, err)

			refusedLoginsCostTheSame(t, hasher)
		})
	}
}

// refusedLoginsCostTheSame times the three refusals against each other, with
// every account store hashing through one hasher.
func refusedLoginsCostTheSame(t *testing.T, hasher *auth.Hasher) {
	t.Helper()

	const (
		rounds = 25
		// A ratio this far from 1 is a machine under load. Dropping the dummy
		// hash from either branch puts the ratio near 0, so the bound catches
		// that by a wide margin while leaving no room for a flake to be the
		// finding.
		tolerance = 0.5
	)

	known, _ := newAccounts(t, local.WithHasher(hasher))
	setUp(t, known)

	// A second store, so spending one account's budget does not disturb the
	// baseline this measurement is taken against.
	spent, _ := newAccounts(t, local.WithHasher(hasher))
	setUp(t, spent)
	spendBudget(t, spent)

	wrongPassword := func() {
		_, err := known.VerifyCredentials(t.Context(), "owner@example.com", "not the password")
		require.ErrorIs(t, err, authapi.ErrInvalidCredentials)
	}

	unknownUser := func() {
		_, err := known.VerifyCredentials(t.Context(), "nobody@example.com", "not the password")
		require.ErrorIs(t, err, authapi.ErrInvalidCredentials)
	}

	overBudget := func() {
		_, err := spent.VerifyCredentials(t.Context(), "owner@example.com", "not the password")
		require.ErrorIs(t, err, authapi.ErrInvalidCredentials)
	}

	// One of each first, so no timing below includes a cold page fault the
	// others do not.
	wrongPassword()
	unknownUser()
	overBudget()

	wrongs := make([]time.Duration, 0, rounds)
	unknowns := make([]time.Duration, 0, rounds)
	refused := make([]time.Duration, 0, rounds)

	for range rounds {
		wrongs = append(wrongs, timeOf(wrongPassword))
		unknowns = append(unknowns, timeOf(unknownUser))
		refused = append(refused, timeOf(overBudget))
	}

	unknownRatio := pairedMedian(unknowns, wrongs)
	refusedRatio := pairedMedian(refused, wrongs)

	fastest := slices.Min(wrongs)
	fastestUnknown := slices.Min(unknowns)
	fastestRefused := slices.Min(refused)

	// The deterministic half of the proof, and the one a removed dummy hash
	// trips: an argon2id pass cannot finish in under a millisecond, so a
	// branch that answers faster than that did no work at all.
	require.Greater(t, fastest, time.Millisecond,
		"a wrong password answered in %s, which means no argon2id pass ran", fastest)
	require.Greater(t, fastestUnknown, time.Millisecond,
		"an unknown username answered in %s, which enumerates accounts", fastestUnknown)
	require.Greater(t, fastestRefused, time.Millisecond,
		"a login over the failure budget answered in %s, which says the account exists", fastestRefused)

	t.Logf("wrong password %s; unknown username %s (ratio %.3f); over budget %s (ratio %.3f); %d rounds",
		fastest, fastestUnknown, unknownRatio, fastestRefused, refusedRatio, rounds)

	require.InDelta(t, 1.0, unknownRatio, tolerance,
		"an unknown username costs %.3f times a wrong password, which enumerates accounts", unknownRatio)

	require.InDelta(t, 1.0, refusedRatio, tolerance,
		"a login over the budget costs %.3f times a wrong password, which says which accounts are being guessed at",
		refusedRatio)
}

// pairedMedian is the middle of the per-round ratios, and it is what makes this
// measurement survive a runner with two cores.
//
// Each round times the three refusals back to back, so a machine busy for
// longer than one round slows every member of that round and the ratio between
// them does not move. Taking the fastest of each series instead threw the
// pairing away: the fastest of one series and the fastest of another come from
// different rounds, and on a loaded machine they are two draws from a
// long-tailed distribution rather than one comparison.
//
// Measured in golang:1.26 at --cpus 2, under a second copy of the same test, 48
// samples of each estimator:
//
//	fastest of 9      worst |ratio-1| 0.509
//	fastest of 25     worst |ratio-1| 0.535
//	paired median 9   worst |ratio-1| 0.276
//	paired median 15  worst |ratio-1| 0.233
//	paired median 25  worst |ratio-1| 0.110
//
// The fastest-of-each shape is outside the tolerance it is checked against, and
// more rounds do not rescue it. It is what failed on CI at a ratio of 1.632.
// Both series were centred on 1.00 across those samples, with medians of 0.981
// and 0.997, so nothing was leaking: the estimator was the noise.
func pairedMedian(series, baseline []time.Duration) float64 {
	ratios := make([]float64, len(series))
	for i := range series {
		ratios[i] = float64(series[i]) / float64(baseline[i])
	}

	slices.Sort(ratios)

	return ratios[len(ratios)/2]
}

// TestTheFailureBudgetIsUnderTheStandardsCeiling holds the numbers against the
// source they were chosen from. NIST SP 800-63B section 5.2.2 puts its ceiling
// at 100 consecutive failed attempts per account, so a record that held more
// than that could not be the basis of a limit anybody enforced over it. The
// record refuses nothing here, because a budget keyed by the account id cannot
// tell the owner from whoever is guessing at them. What bounds guessing is the
// last line: the price of a guess, a bounded number at a time.
func TestTheFailureBudgetIsUnderTheStandardsCeiling(t *testing.T) {
	t.Parallel()

	require.Positive(t, local.MaxFailures, "a budget of nothing refuses the first attempt")
	require.LessOrEqual(t, local.MaxFailures, 100,
		"the failure budget is over NIST SP 800-63B section 5.2.2's ceiling of 100 per account")
	require.Positive(t, local.FailureWindow, "a window of zero drains before it is read, so nothing is bounded")
	require.Positive(t, local.DefaultHashesAtOnce,
		"an unbounded number of concurrent argon2id passes is one memory block each to anything holding a socket")
}

// timeOf is one measurement of one call.
func timeOf(run func()) time.Duration {
	start := time.Now()

	run()

	return time.Since(start)
}
