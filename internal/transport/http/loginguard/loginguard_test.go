package loginguard_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/loginguard"
)

// clock is a hand-wound time source. Nothing here waits for a real duration:
// the window and the hold are both read, so a test that measures them is a
// test that cannot flake.
type clock struct {
	mu sync.Mutex
	at time.Time
}

func newClock() *clock {
	return &clock{at: time.Date(2026, time.August, 28, 9, 0, 0, 0, time.UTC)}
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.at
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.at = c.at.Add(d)
}

// recorder collects what each attempt was held for, in place of holding it.
// Spending a hold moves its clock on by the hold, as a real wait would, so a
// caller who sends one attempt after another is one who waited for each.
type recorder struct {
	mu    sync.Mutex
	spent []time.Duration
	clock *clock
}

func newRecorder() *recorder {
	return &recorder{clock: newClock()}
}

func (r *recorder) sleep(_ context.Context, held time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.spent = append(r.spent, held)
	r.clock.advance(held)

	return nil
}

func (r *recorder) holds() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]time.Duration(nil), r.spent...)
}

// last is what the most recent attempt was held for, and zero for one that was
// not held at all.
func (r *recorder) last(attempts int) time.Duration {
	spent := r.holds()
	if len(spent) < attempts {
		return 0
	}

	return spent[len(spent)-1]
}

// The client and the account a test charges against, where neither of the two
// is itself what the test is about.
const (
	guesserAddress = "198.51.100.7"
	guessedAccount = "owner@example.com"
)

// attempts charges a run of attempts and reports what the last of them was
// held for.
func attempts(t *testing.T, guard *loginguard.Guard, r *recorder, runs int) time.Duration {
	t.Helper()

	for range runs {
		require.NoError(t, guard.Hold(t.Context(), guesserAddress, guessedAccount))
	}

	return r.last(1)
}

// TestNoNumberOfAttemptsEverRefusesOne is the property d7f6baf established,
// carried forward to the layer that counts. A limit that refuses a caller who
// waits their turn is a lockout whatever it is keyed by, and this one has no
// refusal to give them: the answer to ten thousand guesses sent one after
// another is a wait, and the wait is the ceiling.
func TestNoNumberOfAttemptsEverRefusesOne(t *testing.T) {
	t.Parallel()

	held := newRecorder()
	guard := loginguard.New(loginguard.Options{
		Base:    time.Second,
		Ceiling: 4 * time.Second,
		Now:     held.clock.now,
		Sleep:   held.sleep,
	})

	const guesses = 10_000

	for range guesses {
		require.NoError(t, guard.Hold(t.Context(), "198.51.100.7", "owner@example.com"),
			"an attempt was refused rather than held")
	}

	for _, spent := range held.holds() {
		require.LessOrEqual(t, spent, 4*time.Second, "a hold ran past the ceiling")
	}
}

// TestAttemptsSentTogetherFromOneAddressQueue is the gap a hold per attempt
// left. Each attempt waited on its own, so a hundred connections opened at once
// all waited the same few seconds and all reached the password check. They
// now take turns: each waits for the ones before it, and one that would wait
// past the ceiling is refused with the time its turn comes, so connections buy
// a guesser nothing. The clock does not move here, which is every attempt
// arriving at the same moment.
func TestAttemptsSentTogetherFromOneAddressQueue(t *testing.T) {
	t.Parallel()

	wound := newClock()
	guard := loginguard.New(loginguard.Options{
		AccountFree: 1000,
		AddressFree: 1,
		Base:        time.Second,
		Ceiling:     5 * time.Second,
		Now:         wound.now,
		Sleep:       func(context.Context, time.Duration) error { return nil },
	})

	const together = 50

	var (
		admitted int
		busy     *loginguard.BusyError
	)

	for range together {
		err := guard.Hold(t.Context(), guesserAddress, guessedAccount)
		if err == nil {
			admitted++

			continue
		}

		require.ErrorAs(t, err, &busy)
		require.Positive(t, busy.RetryAfter)
	}

	// The free attempt, then holds of 1s and 2s+1s. The next would wait 4s+3s.
	require.Equal(t, 3, admitted, "attempts arriving together were not queued")

	wound.advance(busy.RetryAfter)
	require.NoError(t, guard.Hold(t.Context(), guesserAddress, guessedAccount),
		"an address was still refused once its turn came")
}

// TestTheHoldGrowsWithEachAttemptAndStopsAtTheCeiling is the shape SP 800-63B
// section 5.2.2 names: a wait that rises as an account approaches its
// allowance. It rises by doubling and it stops, because a wait that keeps
// growing is a lockout with a slower fuse.
func TestTheHoldGrowsWithEachAttemptAndStopsAtTheCeiling(t *testing.T) {
	t.Parallel()

	held := newRecorder()
	guard := loginguard.New(loginguard.Options{
		AccountFree: 1,
		AddressFree: 1,
		Base:        100 * time.Millisecond,
		Ceiling:     time.Second,
		Now:         held.clock.now,
		Sleep:       held.sleep,
	})

	const runs = 8

	for range runs {
		require.NoError(t, guard.Hold(t.Context(), "198.51.100.7", "owner@example.com"))
	}

	require.Equal(t, []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		time.Second,
		time.Second,
		time.Second,
	}, held.holds())
}

// TestACorrectPasswordForgetsTheAccountAndForgivesTheAddress is what a verifier
// does on a successful authentication, and it is what keeps a workshop behind
// one address usable: one person getting in there brings everybody on it back
// to the shortest hold, whatever the guessing had grown it to.
func TestACorrectPasswordForgetsTheAccountAndForgivesTheAddress(t *testing.T) {
	t.Parallel()

	held := newRecorder()
	guard := loginguard.New(loginguard.Options{
		AccountFree: 1,
		AddressFree: 1,
		Base:        time.Second,
		Now:         held.clock.now,
		Sleep:       held.sleep,
	})

	require.Equal(t, 4*time.Second, attempts(t, guard, held, 4))

	guard.Passed("198.51.100.7", "owner@example.com")

	require.Equal(t, time.Second, attempts(t, guard, held, 1),
		"an attempt after a correct password was held for the guessing before it")
}

// TestALoginBetweenGuessesBuysNoFreshAllowance is the reset a guesser with an
// account of their own had. They logged in to it between guesses, the address
// record was forgotten, and every login handed them a full allowance and an
// empty queue to spend on somebody else's account. A correct password now
// keeps the queue and lowers the count no further than the allowance. The
// clock does not move, which is every attempt arriving at the same moment.
func TestALoginBetweenGuessesBuysNoFreshAllowance(t *testing.T) {
	t.Parallel()

	wound := newClock()
	guard := loginguard.New(loginguard.Options{
		AccountFree: 1000,
		AddressFree: 3,
		Base:        time.Second,
		Ceiling:     5 * time.Second,
		Now:         wound.now,
		Sleep:       func(context.Context, time.Duration) error { return nil },
	})

	// Three free attempts, then holds of 1s and 2s+1s: the queue runs 3s out.
	for range 5 {
		require.NoError(t, guard.Hold(t.Context(), guesserAddress, guessedAccount))
	}

	guard.Passed(guesserAddress, "guesser-own@example.com")

	admitted := 0

	for range 10 {
		if guard.Hold(t.Context(), guesserAddress, guessedAccount) == nil {
			admitted++
		}
	}

	// A 1s hold behind the 3s queue fits under the ceiling. The next would
	// wait 2s+4s. A forgotten record let five through here.
	require.Equal(t, 1, admitted, "a correct password handed the address a fresh allowance or an empty queue")
}

// TestAUsernameIsFoldedIntoOneKey holds the guard to the account store's own
// folding. Two spellings of one name that reach the same account must reach the
// same record, or the limit is a limit per spelling.
func TestAUsernameIsFoldedIntoOneKey(t *testing.T) {
	t.Parallel()

	held := newRecorder()
	guard := loginguard.New(loginguard.Options{
		AccountFree: 1,
		AddressFree: 1000,
		Base:        time.Second,
		Now:         held.clock.now,
		Sleep:       held.sleep,
	})

	require.Positive(t, attempts(t, guard, held, 3))

	guard.Passed("198.51.100.7", "  Owner@Example.COM  ")

	before := len(held.holds())
	require.NoError(t, guard.Hold(t.Context(), "198.51.100.7", "owner@example.com"))
	require.Len(t, held.holds(), before, "one account was recorded under two spellings")
}

// TestARecordAgesOutWhenGuessingStops is the window. It rolls from the last
// attempt rather than the first, so it drains when somebody stops and not while
// they carry on.
func TestARecordAgesOutWhenGuessingStops(t *testing.T) {
	t.Parallel()

	held := newRecorder()
	wound := held.clock
	guard := loginguard.New(loginguard.Options{
		AccountFree: 1,
		AddressFree: 1,
		Base:        time.Second,
		Window:      15 * time.Minute,
		Now:         wound.now,
		Sleep:       held.sleep,
	})

	require.Positive(t, attempts(t, guard, held, 4))

	wound.advance(16 * time.Minute)

	before := len(held.holds())
	require.NoError(t, guard.Hold(t.Context(), "198.51.100.7", "owner@example.com"))
	require.Len(t, held.holds(), before, "a record outlived the window with nothing charged to it")
}

// TestTheAddressIsTheRightmostHopThatIsNotDeclared is the part a limiter most
// often gets wrong. RemoteAddr behind a proxy is the proxy, so every client
// shares one key; X-Forwarded-For from anywhere else is a key the caller picks.
func TestTheAddressIsTheRightmostHopThatIsNotDeclared(t *testing.T) {
	t.Parallel()

	trusted := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("192.168.0.0/16"),
	}

	cases := []struct {
		name      string
		trusted   []netip.Prefix
		remote    string
		forwarded []string
		want      string
	}{
		{
			name:   "no proxy is declared, so the peer is the client",
			remote: "198.51.100.7:44001",
			want:   "198.51.100.7",
		},
		{
			name:      "a header from an undeclared peer is not read at all",
			remote:    "198.51.100.7:44001",
			forwarded: []string{"203.0.113.1"},
			want:      "198.51.100.7",
		},
		{
			name:      "a header from an undeclared peer is not read even when a proxy is declared",
			trusted:   trusted,
			remote:    "198.51.100.7:44001",
			forwarded: []string{"10.0.0.9"},
			want:      "198.51.100.7",
		},
		{
			name:      "the declared proxy's own header names the client",
			trusted:   trusted,
			remote:    "10.0.0.9:44001",
			forwarded: []string{"198.51.100.7"},
			want:      "198.51.100.7",
		},
		{
			name:      "the entries a client wrote in front of the real one are skipped",
			trusted:   trusted,
			remote:    "10.0.0.9:44001",
			forwarded: []string{"203.0.113.1, 198.51.100.7"},
			want:      "198.51.100.7",
		},
		{
			name:      "two declared hops are walked past to the client",
			trusted:   trusted,
			remote:    "10.0.0.9:44001",
			forwarded: []string{"198.51.100.7, 192.168.4.4", "10.0.0.9"},
			want:      "198.51.100.7",
		},
		{
			name:      "a header of nothing but declared hops leaves the peer",
			trusted:   trusted,
			remote:    "10.0.0.9:44001",
			forwarded: []string{"192.168.4.4, 10.0.0.9"},
			want:      "10.0.0.9",
		},
		{
			name:      "a header this cannot read leaves the peer",
			trusted:   trusted,
			remote:    "10.0.0.9:44001",
			forwarded: []string{"not-an-address"},
			want:      "10.0.0.9",
		},
		{
			name:   "an address written as IPv4 in IPv6 is one address",
			remote: "[::ffff:198.51.100.7]:44001",
			want:   "198.51.100.7",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			guard := loginguard.New(loginguard.Options{TrustedProxies: tc.trusted})

			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
			req.RemoteAddr = tc.remote

			for _, line := range tc.forwarded {
				req.Header.Add("X-Forwarded-For", line)
			}

			require.Equal(t, tc.want, guard.Address(req))
		})
	}
}

// TestAnIPv6ClientIsCountedByItsRoutingPrefix is what stops IPv6 from making
// the address key free. A single host is routinely given a /64, so a guesser
// keyed by the whole address holds a fresh record per attempt and is never
// held at all.
func TestAnIPv6ClientIsCountedByItsRoutingPrefix(t *testing.T) {
	t.Parallel()

	held := newRecorder()
	guard := loginguard.New(loginguard.Options{
		AccountFree: 1000,
		AddressFree: 1,
		Base:        time.Second,
		Now:         held.clock.now,
		Sleep:       held.sleep,
	})

	addresses := []string{
		"2001:db8:1:1::1",
		"2001:db8:1:1::2",
		"2001:db8:1:1::3",
		"2001:db8:1:1::4",
	}

	for i, address := range addresses {
		require.NoError(t, guard.Hold(t.Context(), address, "account-"+string(rune('a'+i))+"@example.com"))
	}

	require.Positive(t, held.last(1), "a guesser inside one /64 was never held")
}

// TestAClientThatHangsUpStopsHoldingAnything is what keeps a held login from
// costing more than a slow reader on any other route. The wait is on the
// caller's own context, so a client that goes away frees the connection rather
// than holding it to the ceiling.
func TestAClientThatHangsUpStopsHoldingAnything(t *testing.T) {
	t.Parallel()

	guard := loginguard.New(loginguard.Options{
		AccountFree: -1,
		AddressFree: -1,
		Base:        time.Hour,
		Ceiling:     time.Hour,
		Now:         newClock().now,
	})

	ctx, hangUp := context.WithCancel(t.Context())
	hangUp()

	require.ErrorIs(t, guard.Hold(ctx, "198.51.100.7", "owner@example.com"), context.Canceled)
}

// TestAttemptsAndClearancesArrivingTogetherAreSafe is the race lane's case. A
// login and a correct password reach the same two records from different
// connections, and the counting has to survive that and still count afterwards.
func TestAttemptsAndClearancesArrivingTogetherAreSafe(t *testing.T) {
	t.Parallel()

	held := newRecorder()
	guard := loginguard.New(loginguard.Options{
		AccountFree: 1,
		AddressFree: 1,
		Base:        time.Millisecond,
		Ceiling:     2 * time.Millisecond,
		Now:         held.clock.now,
		Sleep:       held.sleep,
	})

	const callers = 64

	refusals := make(chan error, callers)

	var running sync.WaitGroup

	for i := range callers {
		running.Go(func() {
			if i%2 == 0 {
				guard.Passed("198.51.100.7", "owner@example.com")

				return
			}

			if err := guard.Hold(context.Background(), "198.51.100.7", "owner@example.com"); err != nil {
				refusals <- err
			}
		})
	}

	running.Wait()
	close(refusals)

	// Attempts arriving together queue on the address, so some are refused
	// for being too far back in it. Nothing else may refuse one.
	var busy *loginguard.BusyError
	for err := range refusals {
		require.ErrorAs(t, err, &busy, "an attempt was refused for something other than the queue")
	}

	// Whatever order those arrived in, the record still counts: two attempts
	// past an allowance of one earn a hold.
	require.Positive(t, attempts(t, guard, held, 3),
		"the records stopped counting after attempts and clearances crossed")
}

// TestEveryHeldOrRefusedAttemptIsLogged is the third bound of the owner
// decision 20260828-65v: every failure is recorded. A wait nobody can read
// back is a guess nobody knows was made.
func TestEveryHeldOrRefusedAttemptIsLogged(t *testing.T) {
	t.Parallel()

	var logged bytes.Buffer

	wound := newClock()
	guard := loginguard.New(loginguard.Options{
		AccountFree: 1000,
		AddressFree: 1,
		Base:        time.Second,
		Ceiling:     time.Second,
		Now:         wound.now,
		Sleep:       func(context.Context, time.Duration) error { return nil },
		Logger:      slog.New(slog.NewJSONHandler(&logged, nil)),
	})

	require.NoError(t, guard.Hold(t.Context(), guesserAddress, guessedAccount))
	require.Empty(t, logged.String(), "an attempt inside the allowance was logged")

	require.NoError(t, guard.Hold(t.Context(), guesserAddress, guessedAccount))
	require.Contains(t, logged.String(), `"msg":"login attempt held"`)

	var busy *loginguard.BusyError
	require.ErrorAs(t, guard.Hold(t.Context(), guesserAddress, guessedAccount), &busy)
	require.Contains(t, logged.String(), `"msg":"login attempt refused"`)

	require.Contains(t, logged.String(), `"account":"`+guessedAccount+`"`)
	require.Contains(t, logged.String(), `"address":"`+guesserAddress+`"`)
}

// TestARunOfRefusalsIsLoggedAtDoublingCounts is what stops a refusal, which
// costs its sender nothing, from being a way to fill the log. Every refusal is
// still counted, and the count is what the lines carry.
func TestARunOfRefusalsIsLoggedAtDoublingCounts(t *testing.T) {
	t.Parallel()

	var logged bytes.Buffer

	guard := loginguard.New(loginguard.Options{
		AccountFree: 1000,
		AddressFree: -1,
		Base:        time.Second,
		Ceiling:     time.Second,
		Now:         newClock().now,
		Sleep:       func(context.Context, time.Duration) error { return nil },
		Logger:      slog.New(slog.NewJSONHandler(&logged, nil)),
	})

	require.NoError(t, guard.Hold(t.Context(), guesserAddress, guessedAccount))

	const refusals = 1000

	var busy *loginguard.BusyError
	for range refusals {
		require.ErrorAs(t, guard.Hold(t.Context(), guesserAddress, guessedAccount), &busy)
	}

	require.Equal(t, refusals, busy.Refused, "a refusal went uncounted")
	// 1, 2, 4 and so on up to 512.
	require.Equal(t, 10, strings.Count(logged.String(), `"msg":"login attempt refused"`))
	require.Contains(t, logged.String(), "512 refused")
}
