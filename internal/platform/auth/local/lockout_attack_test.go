package local_test

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/local"
	"github.com/prusa3d/prusa-connect/sdk/authapi"
)

// argon2Derivation is the function in golang.org/x/crypto/argon2 that holds a
// pass's memory block. It allocates the block when it starts and drops it when
// it returns, so a goroutine inside it is a pass that holds its memory.
const argon2Derivation = "golang.org/x/crypto/argon2.deriveKey"

// peakDerivationsDuring runs one call on each of that many callers at once and
// reports the most argon2id passes it saw running at the same moment.
//
// Each pass holds one memory block of auth.MemoryKiB for as long as it runs, so
// this count times the block size is the memory the passes hold. The count is
// read from the stacks of the running goroutines. A heap reading would also
// count blocks of passes that have finished and that the collector has not
// freed yet. On a busy machine the collector gets little CPU time, and the heap
// then holds many of those blocks. So a heap reading says more about the
// collector than about the bound.
//
// The count covers the whole process. A test that uses this does not call
// t.Parallel, so no other test runs a pass beside it.
//
// The count can miss passes when the sampler gets little CPU time, but it never
// counts a pass that is not running. So a reading above the bound always means
// the bound was broken.
func peakDerivationsDuring(t *testing.T, callers int, run func()) int {
	t.Helper()

	stop := make(chan struct{})
	peak := make(chan int, 1)

	go func() {
		high := 0

		for {
			select {
			case <-stop:
				peak <- high

				return
			default:
			}

			high = max(high, runningDerivations())

			time.Sleep(time.Millisecond)
		}
	}()

	var settled sync.WaitGroup

	for range callers {
		settled.Go(run)
	}

	settled.Wait()
	close(stop)

	high := <-peak

	t.Logf("observed: at most %d argon2id passes ran at once while %d callers ran", high, callers)

	return high
}

// runningDerivations counts the goroutines that are inside argon2Derivation now.
func runningDerivations() int {
	records := make([]runtime.StackRecord, runtime.NumGoroutine()+16)

	n, ok := runtime.GoroutineProfile(records)
	for !ok {
		records = make([]runtime.StackRecord, n+16)
		n, ok = runtime.GoroutineProfile(records)
	}

	running := 0

	for _, record := range records[:n] {
		frames := runtime.CallersFrames(record.Stack())

		for {
			frame, more := frames.Next()
			if frame.Function == argon2Derivation {
				running++

				break
			}

			if !more {
				break
			}
		}
	}

	return running
}

// peakDerivationsDuring must see passes that run outside any slot. Otherwise a
// reading of zero would pass every bound, and a broken bound would pass too.
//
// These callers derive through the auth package directly, with no slot around
// them, and more of them run at once than the bound allows. On a busy machine
// the sampler may get too little CPU time to see them together, so the test
// tries again until a reading shows more passes than the bound allows.
func TestTheDerivationCountSeesPassesOutsideTheBound(t *testing.T) {
	const callers = 3 * local.DefaultHashesAtOnce

	deadline := time.Now().Add(time.Minute)
	hasher := auth.NewDefaultHasher()

	for {
		high := peakDerivationsDuring(t, callers, func() {
			_ = hasher.VerifyUnknownUser([]byte("x"))
		})
		if high > local.DefaultHashesAtOnce {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("%d callers derived with no slot for a minute, and no reading saw more than %d passes at once",
				callers, local.DefaultHashesAtOnce)
		}
	}
}

// fillTheFailureBudget spends an account's whole failure budget from outside,
// the way somebody guessing at a named account does.
//
// The password is one byte, below MinPasswordBytes. auth.VerifyPassword derives
// before it checks that bound, so each of these costs a full argon2id pass
// rather than being the one input a caller can spend a named account's budget
// with for nothing. loginBody's own minLength is 1, so it is a body the HTTP
// surface accepts.
func fillTheFailureBudget(t *testing.T, accounts *local.Accounts, username string) {
	t.Helper()

	for range local.MaxFailures {
		_, err := accounts.VerifyCredentials(t.Context(), username, "x")
		require.ErrorIs(t, err, authapi.ErrInvalidCredentials)
	}
}

// The failure budget must never keep an account's owner out while somebody
// guesses at them.
//
// A rolling window is not enough on its own. The window rolls and record
// refuses to extend a spent budget, so a budget filled once drains after
// FailureWindow. But the budget is keyed by account id and nothing else, so
// whoever refills it first owns the next window, and refilling it takes
// MaxFailures requests. A budget consulted before the password is checked would
// give that attacker the owner's account for as long as they keep sending.
//
// So the password is checked first and the budget never refuses a correct one.
// The owner here holds their own password throughout, and asks with it at the
// two moments an order that checked the budget first would refuse: with the
// budget freshly spent, and across a window that drained and was refilled
// before they could use it.
func TestGuessingAtANamedAccountCannotHoldItsOwnerOut(t *testing.T) {
	t.Parallel()

	const days = 4

	accounts, clock := throttled(t)

	// The owner's own password works before anybody is guessing at it.
	_, err := accounts.VerifyCredentials(t.Context(), "owner@example.com", goodPassword)
	require.NoError(t, err, "the fixture's password is wrong")

	admitted := 0

	for cycle := range days {
		fillTheFailureBudget(t, accounts, "owner@example.com")

		// The owner tries, with the right password, immediately.
		_, ownErr := accounts.VerifyCredentials(t.Context(), "owner@example.com", goodPassword)
		require.NoError(t, ownErr,
			"the owner was refused with the budget freshly spent by somebody else; cycle %d", cycle)

		admitted++

		// The window drains. The attacker is still there, and refills it before
		// the owner's next attempt: a second is generous against ten requests
		// on a LAN.
		clock.advance(local.FailureWindow + time.Second)
		fillTheFailureBudget(t, accounts, "owner@example.com")

		_, ownErr = accounts.VerifyCredentials(t.Context(), "owner@example.com", goodPassword)
		require.NoError(t, ownErr,
			"the owner was refused after the window drained and was refilled; cycle %d", cycle)

		admitted++
	}

	require.Equal(t, days*2, admitted,
		"the owner got in %d times out of %d across %s of somebody guessing at their account",
		admitted, days*2, time.Duration(days)*2*local.FailureWindow)

	// The guessing itself still gets nowhere, which is the half of the budget
	// that was never in question.
	_, err = accounts.VerifyCredentials(t.Context(), "owner@example.com", "x")
	require.ErrorIs(t, err, authapi.ErrInvalidCredentials,
		"a guess was admitted, so the password check went with the lockout")
}

// The price of a guess is what bounds guessing, since the record refuses
// nothing.
//
// Every refused login is expensive. No branch of VerifyCredentials answers
// faster than a wrong password. Length is the one thing a caller controls
// completely, so a refusal that skipped the pass on a password below
// MinPasswordBytes would be the one answer this server gives for free, and the
// one an attacker would send.
func TestAPasswordBelowTheHashersFloorStillCostsAnArgon2Pass(t *testing.T) {
	const rounds = local.MaxFailures

	accounts, _ := newAccounts(t)
	setUp(t, accounts)

	started := time.Now()

	for range rounds {
		_, err := accounts.VerifyCredentials(t.Context(), "owner@example.com", "x")
		require.ErrorIs(t, err, authapi.ErrInvalidCredentials)
	}

	belowFloor := time.Since(started)

	// A second account, so the budget just spent is not what makes the
	// comparison run.
	full, _ := newAccounts(t)
	setUp(t, full)

	started = time.Now()

	for range rounds {
		_, err := full.VerifyCredentials(t.Context(), "owner@example.com", "not the password")
		require.ErrorIs(t, err, authapi.ErrInvalidCredentials)
	}

	wrongPassword := time.Since(started)

	// The budget is gone either way, and neither one refuses the owner: the
	// price is the only thing this test is about.
	_, err := accounts.VerifyCredentials(t.Context(), "owner@example.com", goodPassword)
	require.NoError(t, err)

	require.Greater(t, belowFloor*4, wrongPassword,
		"%d refusals on a password below the hasher's floor cost %s, against %s for %d real ones: "+
			"the budget is spendable for nothing",
		rounds, belowFloor, wrongPassword, rounds)
}

// Admitting a correct password whatever the record holds has a price, and
// DefaultHashesAtOnce is what keeps that price affordable.
//
// The record refuses nothing, so a caller with no credential can force one
// argon2id pass per request on a named account for as long as they keep
// sending. The bound is the only thing between that and the memory of a small
// board. Every derivation on this path has to be inside a slot, including the
// one a password below the hasher's floor runs, which is what this test sends.
func TestAGuessingFloodStaysInsideTheVerificationBound(t *testing.T) {
	const (
		askers = 16
		each   = 4
	)

	accounts, _ := newAccounts(t)
	setUp(t, accounts)

	high := peakDerivationsDuring(t, askers, func() {
		for range each {
			// One byte, the cheapest request a caller can send and still a
			// full pass, on an account whose record fills within the first
			// few of these.
			_, _ = accounts.VerifyCredentials(
				context.WithoutCancel(t.Context()), "owner@example.com", "x")
		}
	})

	require.LessOrEqual(t, high, local.DefaultHashesAtOnce,
		"%d callers guessing at one account ran %d argon2id passes at once; the bound allows %d",
		askers, high, local.DefaultHashesAtOnce)
}
