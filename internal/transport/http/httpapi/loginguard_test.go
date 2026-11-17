package httpapi

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/loginguard"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// holdRecorder is the guard's wait with the waiting taken out. It records what
// each attempt was held for, so a test reads the limit rather than spending it.
// Its clock moves on by each hold, as a real wait would, for a guard that reads
// the time from it.
type holdRecorder struct {
	mu    sync.Mutex
	spent []time.Duration
	at    time.Time
}

func (h *holdRecorder) sleep(_ context.Context, held time.Duration) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.spent = append(h.spent, held)
	h.at = h.at.Add(held)

	return nil
}

// now is the recorder's clock: the zero time plus every hold spent so far.
func (h *holdRecorder) now() time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.at
}

// holds is what every attempt so far was held for.
func (h *holdRecorder) holds() []time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()

	return append([]time.Duration(nil), h.spent...)
}

// held is what the last attempt was held for, and zero when nothing was.
func (h *holdRecorder) held() time.Duration {
	spent := h.holds()
	if len(spent) == 0 {
		return 0
	}

	return spent[len(spent)-1]
}

// guardedServer is a server whose login guard is the caller's, so a test can
// read what each attempt was held for without waiting for it.
func guardedServer(t *testing.T, accounts Accounts, guard *loginguard.Guard) *Server {
	t.Helper()

	server, err := New(Config{
		Version:    "test",
		Principals: fixedPrincipal(""),
		Resources:  seededStore(),
		Presets:    seededPresets(),
		Endpoints:  seededEndpoints(),
		Grants:     &fakeGrantStore{},
		Sessions:   newFakeSessions(),
		Accounts:   accounts,
		Logins:     guard,
		Readiness:  &fakeReadiness{report: readyReport()},
		Devices:    noPrinters{},
		Stores:     noStorages{},
		Events:     noEvents{},
	})
	require.NoError(t, err)

	return server
}

// theRightPassword is what oneGoodAccount takes, and a guess is anything else.
const theRightPassword = "the password this account has"

// login sends one password login with a wrong password, from a client address,
// with whatever forwarding header the caller wants in front of it.
func login(
	t *testing.T, handler http.Handler, from, username string, forwarded ...string,
) *httptest.ResponseRecorder {
	t.Helper()

	return loginWith(t, handler, from, username, "a guess at somebody", forwarded...)
}

// loginWith sends one password login, whatever the password is.
func loginWith(
	t *testing.T, handler http.Handler, from, username, password string, forwarded ...string,
) *httptest.ResponseRecorder {
	t.Helper()

	body := `{"username":"` + username + `","password":"` + password + `"}`

	req := httptest.NewRequest(http.MethodPost, loginPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = net.JoinHostPort(from, "51000")

	for _, hop := range forwarded {
		req.Header.Add("X-Forwarded-For", hop)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec
}

// refusingAccounts is a credential store that refuses every password, which is
// what somebody guessing sees.
func refusingAccounts() *fakeAccounts {
	return &fakeAccounts{
		info:     AuthInfo{Provider: "local", Credentials: true},
		loginErr: fault.AuthTokenInvalid.New("the credential is not one this server accepts"),
	}
}

// TestARunOfFailedLoginsHoldsTheNextOne is the gap d7f6baf left open, at the
// layer that can close it. The credential store is handed a username and a
// password and nothing else, so it cannot limit attempts without refusing the
// owner too. This surface is handed the request, so it can.
//
// The hold is read rather than waited out: the guard's wait is the recorder's,
// and every attempt is still charged.
func TestARunOfFailedLoginsHoldsTheNextOne(t *testing.T) {
	t.Parallel()

	recorder := &holdRecorder{}
	guard := loginguard.New(loginguard.Options{AccountFree: 2, Sleep: recorder.sleep})
	handler := guardedServer(t, refusingAccounts(), guard).Handler()

	for range 2 {
		require.Equal(t, http.StatusUnauthorized,
			login(t, handler, "203.0.113.9", "owner@example.com").Code)
	}

	require.Empty(t, recorder.holds(), "an attempt inside the allowance was held")

	require.Equal(t, http.StatusUnauthorized,
		login(t, handler, "203.0.113.9", "owner@example.com").Code)
	require.Positive(t, recorder.held(), "the login path charges the guard nothing")

	require.Equal(t, http.StatusUnauthorized,
		login(t, handler, "203.0.113.9", "owner@example.com").Code)
	require.Greater(t, recorder.held(), recorder.holds()[0],
		"a further attempt earned no longer hold than the one before it")
}

// TestACorrectPasswordClearsWhatWasChargedToTheAccount is section 5.2.2's other
// half: a successful authentication forgets the failures before it. Without it
// the person who mistyped their password four times is held on every login for
// the rest of the window.
func TestACorrectPasswordClearsWhatWasChargedToTheAccount(t *testing.T) {
	t.Parallel()

	recorder := &holdRecorder{}
	accounts := refusingAccounts()
	// The address allowance is out of reach, so every hold here is the
	// account's.
	guard := loginguard.New(loginguard.Options{AccountFree: 1, AddressFree: 1000, Sleep: recorder.sleep})
	handler := guardedServer(t, accounts, guard).Handler()

	for range 3 {
		require.Equal(t, http.StatusUnauthorized,
			login(t, handler, "203.0.113.9", "owner@example.com").Code)
	}

	require.Positive(t, recorder.held())

	accounts.mu.Lock()
	accounts.loginErr = nil
	accounts.mu.Unlock()

	require.Equal(t, http.StatusOK, login(t, handler, "203.0.113.9", "owner@example.com").Code)

	before := len(recorder.holds())

	require.Equal(t, http.StatusOK, login(t, handler, "203.0.113.9", "owner@example.com").Code)
	require.Len(t, recorder.holds(), before,
		"a login after a correct password was still held for the failures before it")
}

// oneGoodAccount is a credential store where one username has the password the
// caller sends and every other name does not.
type oneGoodAccount struct {
	fakeAccounts

	username string
}

func (a *oneGoodAccount) Login(ctx context.Context, username, password string) (Session, error) {
	if username != a.username || password != theRightPassword {
		return Session{}, fault.AuthTokenInvalid.New("the credential is not one this server accepts")
	}

	return a.fakeAccounts.Login(ctx, username, password)
}

// TestSomebodyOnTheAddressAGuesserIsUsingStillGetsIn is the property the
// fifteen-minute lockout could not give, and the reason this limit holds rather
// than refuses. The person guessing and the person logging in share one address,
// which is every workshop behind one NAT.
//
// The hold is what they pay, and it is bounded by the ceiling. A refusal here
// would be the lockout d7f6baf removed, moved to an address.
func TestSomebodyOnTheAddressAGuesserIsUsingStillGetsIn(t *testing.T) {
	t.Parallel()

	recorder := &holdRecorder{}
	accounts := &oneGoodAccount{
		fakeAccounts: fakeAccounts{info: AuthInfo{Provider: "local", Credentials: true}},
		username:     "colleague@example.com",
	}
	guard := loginguard.New(loginguard.Options{
		AccountFree: 1,
		AddressFree: 1,
		Base:        time.Second,
		Ceiling:     4 * time.Second,
		Now:         recorder.now,
		Sleep:       recorder.sleep,
	})
	handler := guardedServer(t, accounts, guard).Handler()

	const guesses = 200

	for range guesses {
		require.Equal(t, http.StatusUnauthorized,
			login(t, handler, "198.51.100.7", "owner@example.com").Code)
	}

	rec := loginWith(t, handler, "198.51.100.7", "colleague@example.com", theRightPassword)
	require.Equal(t, http.StatusOK, rec.Code,
		"somebody on the address a guesser is using was refused: body %s", rec.Body.String())

	for _, held := range recorder.holds() {
		require.LessOrEqual(t, held, 4*time.Second, "a hold ran past the ceiling")
	}

	require.Equal(t, []string{"colleague@example.com"}, accounts.loggedIn)
}

// TestTheOwnerOfAGuessedAccountStillGetsIn is the same property on the other
// key. Somebody who knows a username can hold its owner up, and cannot hold
// them out: what they buy is the ceiling, once, not a quarter of an hour.
func TestTheOwnerOfAGuessedAccountStillGetsIn(t *testing.T) {
	t.Parallel()

	recorder := &holdRecorder{}
	accounts := &oneGoodAccount{
		fakeAccounts: fakeAccounts{info: AuthInfo{Provider: "local", Credentials: true}},
		username:     "owner@example.com",
	}
	guard := loginguard.New(loginguard.Options{
		AccountFree: 1,
		AddressFree: 1,
		Base:        time.Second,
		Ceiling:     4 * time.Second,
		Now:         recorder.now,
		Sleep:       recorder.sleep,
	})
	handler := guardedServer(t, accounts, guard).Handler()

	const guesses = 200

	for range guesses {
		require.Equal(t, http.StatusUnauthorized,
			login(t, handler, "198.51.100.7", "owner@example.com").Code)
	}

	// From the owner's own address, which is not the one the guesses came from.
	rec := loginWith(t, handler, "192.0.2.44", "owner@example.com", theRightPassword)
	require.Equal(t, http.StatusOK, rec.Code,
		"the owner of a guessed account was refused: body %s", rec.Body.String())
	require.LessOrEqual(t, recorder.held(), 4*time.Second, "the owner was held past the ceiling")
}

// TestTheGuardCountsTheClientAndNotTheProxy is the part that is worth getting
// wrong quietly. Behind a reverse proxy every request arrives from the proxy,
// so a limiter reading RemoteAddr keys the whole internet under one address and
// holds every user for whatever one of them did.
func TestTheGuardCountsTheClientAndNotTheProxy(t *testing.T) {
	t.Parallel()

	recorder := &holdRecorder{}
	guard := loginguard.New(loginguard.Options{
		AccountFree:    1,
		AddressFree:    1,
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
		Sleep:          recorder.sleep,
	})
	handler := guardedServer(t, refusingAccounts(), guard).Handler()

	for range 4 {
		require.Equal(t, http.StatusUnauthorized,
			login(t, handler, "10.1.2.3", "guessed@example.com", "198.51.100.7").Code)
	}

	require.Positive(t, recorder.held(), "the address behind the proxy was never charged")

	before := len(recorder.holds())

	require.Equal(t, http.StatusUnauthorized,
		login(t, handler, "10.1.2.3", "somebody-else@example.com", "198.51.100.200").Code)
	require.Len(t, recorder.holds(), before,
		"a second client behind the same proxy was held for the first one's attempts")
}

// TestARefusedLoginIsLoggedWithoutThePassword is the third bound of the owner
// decision 20260828-65v at the layer that knows how the check went. The guard
// records the holds; only this surface sees a password refused.
func TestARefusedLoginIsLoggedWithoutThePassword(t *testing.T) {
	t.Parallel()

	var logged bytes.Buffer

	server, err := New(Config{
		Version:    "test",
		Logger:     slog.New(slog.NewJSONHandler(&logged, nil)),
		Principals: fixedPrincipal(""),
		Resources:  seededStore(),
		Presets:    seededPresets(),
		Endpoints:  seededEndpoints(),
		Grants:     &fakeGrantStore{},
		Sessions:   newFakeSessions(),
		Accounts:   refusingAccounts(),
		Readiness:  &fakeReadiness{report: readyReport()},
		Devices:    noPrinters{},
		Stores:     noStorages{},
		Events:     noEvents{},
	})
	require.NoError(t, err)

	require.Equal(t, http.StatusUnauthorized,
		loginWith(t, server.Handler(), "203.0.113.9", "  Owner@Example.COM ", "hunter2-not-logged").Code)

	require.Contains(t, logged.String(), `"msg":"login refused"`)
	require.Contains(t, logged.String(), `"account":"owner@example.com"`)
	require.Contains(t, logged.String(), `"address":"203.0.113.9"`)
	require.NotContains(t, logged.String(), "hunter2-not-logged")
}

// TestAQueuedAddressIsToldWhenToComeBack is the refusal the queue adds. It is a
// 429 with Retry-After rather than the login refusal, because no password was
// checked, and a client that waits that long is let through.
func TestAQueuedAddressIsToldWhenToComeBack(t *testing.T) {
	t.Parallel()

	guard := loginguard.New(loginguard.Options{
		AccountFree: 1000,
		AddressFree: 1,
		Base:        700 * time.Millisecond,
		Ceiling:     time.Second,
		Now:         func() time.Time { return time.Date(2026, time.October, 27, 9, 0, 0, 0, time.UTC) },
		Sleep:       func(context.Context, time.Duration) error { return nil },
	})
	handler := guardedServer(t, refusingAccounts(), guard).Handler()

	for range 2 {
		require.Equal(t, http.StatusUnauthorized, login(t, handler, "203.0.113.9", "owner@example.com").Code)
	}

	// The clock does not move, so the third attempt arrives with the second
	// still in the queue: its 1s hold plus 0.7s queued is 0.7s past the
	// ceiling, which Retry-After rounds up to a whole second.
	rec := login(t, handler, "203.0.113.9", "owner@example.com")
	require.Equal(t, http.StatusTooManyRequests, rec.Code, "body %s", rec.Body.String())
	require.Equal(t, "1", rec.Header().Get("Retry-After"))
	require.Contains(t, rec.Body.String(), "core.auth/LOGIN_LIMITED")
}
