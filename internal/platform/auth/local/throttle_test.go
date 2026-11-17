package local_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/auth/local"
	"github.com/prusa3d/prusa-connect/sdk/authapi"
)

// clock is a hand-driven clock, so a window that takes a quarter of an hour to
// drain is tested in no time at all.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *clock { return &clock{now: time.Date(2026, time.August, 27, 9, 0, 0, 0, time.UTC)} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
}

func throttled(t *testing.T) (*local.Accounts, *clock) {
	t.Helper()

	tick := newClock()
	accounts := local.New(local.NewMemoryStore(), tick.Now)

	_, err := accounts.CreateFirst(t.Context(), firstID, local.NewAccount{
		Username: "owner@example.com",
		Password: []byte(goodPassword),
	})
	require.NoError(t, err)

	return accounts, tick
}

func spendBudget(t *testing.T, accounts *local.Accounts) {
	t.Helper()

	for range local.MaxFailures {
		_, err := accounts.VerifyCredentials(t.Context(), "owner@example.com", "not the password")
		require.ErrorIs(t, err, authapi.ErrInvalidCredentials)
	}
}

// TestASpentFailureBudgetStillAdmitsTheCorrectPassword is the limit of what a
// budget keyed by the account id is allowed to decide.
//
// It cannot tell the owner from whoever is guessing at them, so a budget that
// refused before the password was checked would be refusing the owner on the
// strength of ten requests anybody can send. What it may do is count failures
// and forget them on a success. What stops a guess is that the guess is wrong.
func TestASpentFailureBudgetStillAdmitsTheCorrectPassword(t *testing.T) {
	t.Parallel()

	accounts, _ := throttled(t)

	// One under the budget admits, which is what stops the guard being a
	// lockout on the first typo.
	for range local.MaxFailures - 1 {
		_, err := accounts.VerifyCredentials(t.Context(), "owner@example.com", "not the password")
		require.ErrorIs(t, err, authapi.ErrInvalidCredentials)
	}

	_, err := accounts.VerifyCredentials(t.Context(), "owner@example.com", goodPassword)
	require.NoError(t, err, "the budget refused a login %d failures in", local.MaxFailures-1)

	spendBudget(t, accounts)

	_, err = accounts.VerifyCredentials(t.Context(), "owner@example.com", goodPassword)
	require.NoError(t, err,
		"the correct password was refused with the failure budget spent, which is a lockout "+
			"anybody can aim at a named account for %d requests", local.MaxFailures)

	// And the wrong password still gets nowhere, budget or no budget.
	spendBudget(t, accounts)

	_, err = accounts.VerifyCredentials(t.Context(), "owner@example.com", "not the password")
	require.ErrorIs(t, err, authapi.ErrInvalidCredentials)
}

// TestACorrectPasswordClearsTheBudget keeps a person who mistyped nine times
// and then got it right from carrying those nine into their next login. NIST SP
// 800-63B section 5.2.2 asks for exactly that: a successful authentication
// disregards the failed attempts before it.
func TestACorrectPasswordClearsTheBudget(t *testing.T) {
	t.Parallel()

	accounts, _ := throttled(t)

	for range local.MaxFailures - 1 {
		_, err := accounts.VerifyCredentials(t.Context(), "owner@example.com", "not the password")
		require.ErrorIs(t, err, authapi.ErrInvalidCredentials)
	}

	_, err := accounts.VerifyCredentials(t.Context(), "owner@example.com", goodPassword)
	require.NoError(t, err)

	for range local.MaxFailures - 1 {
		_, failErr := accounts.VerifyCredentials(t.Context(), "owner@example.com", "not the password")
		require.ErrorIs(t, failErr, authapi.ErrInvalidCredentials)
	}

	_, err = accounts.VerifyCredentials(t.Context(), "owner@example.com", goodPassword)
	require.NoError(t, err, "a successful login did not clear the failures before it")
}

// TestOneAccountsBudgetDoesNotSpendAnothers holds the keying. The record is per
// account, so somebody guessing at one person's password reaches nobody else's,
// and the password check runs before the record, so it does not reach the
// person they are guessing at either.
func TestOneAccountsBudgetDoesNotSpendAnothers(t *testing.T) {
	t.Parallel()

	accounts, _ := throttled(t)

	colleague, err := accounts.Create(t.Context(), local.NewAccount{
		Username: "colleague@example.com",
		Password: []byte("another good password"),
	})
	require.NoError(t, err)
	require.NotEqual(t, firstID, colleague.ID)

	spendBudget(t, accounts)

	_, err = accounts.VerifyCredentials(t.Context(), "colleague@example.com", "another good password")
	require.NoError(t, err, "spending one account's budget shut another account out")

	// Nor the account it was aimed at. The failures were somebody else's, and
	// nothing in this store can tell that from the owner's own typing.
	_, err = accounts.VerifyCredentials(t.Context(), "owner@example.com", goodPassword)
	require.NoError(t, err, "the account the guesses were aimed at was shut out by them")
}

// TestAnUnknownUsernameSpendsNoBudget keeps the record from being a way to grow
// this server's memory. A caller sending names at random leaves nothing behind
// and gets the same refusal either way, and the account they never named is
// untouched.
func TestAnUnknownUsernameSpendsNoBudget(t *testing.T) {
	t.Parallel()

	accounts, _ := throttled(t)

	for range local.MaxFailures * 2 {
		_, err := accounts.VerifyCredentials(t.Context(), "nobody@example.com", "whatever")
		require.ErrorIs(t, err, authapi.ErrInvalidCredentials)
	}

	_, err := accounts.VerifyCredentials(t.Context(), "owner@example.com", goodPassword)
	require.NoError(t, err, "guesses at names nobody holds reached a real account's budget")
}

// TestAnAdministratorResetClearsTheBudget holds the reset end to end, on an
// account somebody has been guessing at. The new password works on the first
// attempt, and the failures aimed at the old one are gone with it.
func TestAnAdministratorResetClearsTheBudget(t *testing.T) {
	t.Parallel()

	accounts, _ := throttled(t)
	spendBudget(t, accounts)

	require.NoError(t, accounts.SetPassword(t.Context(), firstID, []byte("a reset password")))

	_, err := accounts.VerifyCredentials(t.Context(), "owner@example.com", "a reset password")
	require.NoError(t, err, "the account is still refused after an administrator reset it")
}

// TestALoginOnACancelledContextIsRefused holds the verification bound's exit.
// A caller that gave up frees its place rather than holding one.
func TestALoginOnACancelledContextIsRefused(t *testing.T) {
	t.Parallel()

	accounts, _ := throttled(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := accounts.VerifyCredentials(ctx, "owner@example.com", goodPassword)
	require.ErrorIs(t, err, context.Canceled)
}

// TestConcurrentLoginsAreBoundedButNoneIsRefused is the memory bound. One
// argon2id pass holds its whole memory block and the caller has no credential,
// so the number running at once has to be bounded. Refusing the extra callers
// would make the bound a way to stop people logging in, so they wait instead.
func TestConcurrentLoginsAreBoundedButNoneIsRefused(t *testing.T) {
	t.Parallel()

	const callers = local.DefaultHashesAtOnce * 4

	accounts, _ := throttled(t)

	var (
		start sync.WaitGroup
		done  sync.WaitGroup
		mu    sync.Mutex
	)

	admitted := 0

	start.Add(1)
	done.Add(callers)

	for range callers {
		go func() {
			defer done.Done()

			start.Wait()

			_, err := accounts.VerifyCredentials(context.Background(), "owner@example.com", goodPassword)
			if err != nil {
				return
			}

			mu.Lock()
			admitted++
			mu.Unlock()
		}()
	}

	start.Done()
	done.Wait()

	require.Equal(t, callers, admitted,
		"%d of %d concurrent logins were refused, so the bound refuses rather than queues",
		callers-admitted, callers)
}
