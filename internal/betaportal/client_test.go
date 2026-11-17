package betaportal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

// fakePortal answers the device flow with the shapes the deployed portal sends.
type fakePortal struct {
	mu sync.Mutex
	// clock is the portal's own time. It writes the Date header and judges iat.
	clock func() time.Time
	// manual makes the product team approve a confirmed device.
	manual    bool
	confirmed bool
	approved  bool
	declined  bool
	// identity is the account the token response names, or nil for the
	// portal answering without one.
	identity map[string]string
	// refuseProofs refuses every proof, whatever its date.
	refuseProofs bool
	// retryAfter makes the device authorization endpoint answer 429.
	retryAfter string
	// complete overrides verification_uri_complete.
	complete *string
	// page overrides verification_uri.
	page string
	// broken makes the token endpoint answer 503.
	broken bool
	// deviceRefusal makes the device endpoint refuse with this error.
	deviceRefusal string
	// hold blocks every request until it is closed.
	hold     chan struct{}
	requests map[string]int
	form     map[string][]string
}

func newFakePortal(t *testing.T, portal *fakePortal) (*httptest.Server, *fakePortal) {
	t.Helper()
	if portal.clock == nil {
		portal.clock = time.Now
	}
	portal.requests = map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(portal.serve))
	t.Cleanup(server.Close)

	return server, portal
}

func (p *fakePortal) serve(response http.ResponseWriter, request *http.Request) {
	if p.hold != nil {
		<-p.hold
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	p.requests[request.URL.Path]++
	now := p.clock()
	response.Header().Set("Date", now.UTC().Format(http.TimeFormat))
	if err := request.ParseForm(); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid_request"})

		return
	}
	if request.URL.Path == DeviceAuthorizationPath && p.retryAfter != "" {
		response.Header().Set("Retry-After", p.retryAfter)
		writeJSON(response, http.StatusTooManyRequests, map[string]string{
			"error": "slow_down", "error_description": "Too many pairings from this address",
		})

		return
	}
	if request.URL.Path == devicePath {
		p.device(response, request, now)

		return
	}
	if refused := p.refusedProof(request.Header.Get("DPoP"), now); refused != "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "invalid_dpop_proof", "error_description": refused,
		})

		return
	}

	switch request.URL.Path {
	case DeviceAuthorizationPath:
		p.form = request.PostForm
		page := serverURL(request) + "/device"
		if p.page != "" {
			page = p.page
		}
		complete := page + "?user_code=WDJB-MJHT"
		if p.complete != nil {
			complete = *p.complete
		}
		writeJSON(response, http.StatusOK, map[string]any{
			"device_code": "device-code-secret", "user_code": "WDJB-MJHT",
			"verification_uri": page, "verification_uri_complete": complete,
			"expires_in": 900, "interval": defaultInterval,
		})
	case TokenPath:
		if p.broken {
			response.WriteHeader(http.StatusServiceUnavailable)

			return
		}
		p.token(response)
	case RevocationPath:
		response.WriteHeader(http.StatusOK)
	default:
		response.WriteHeader(http.StatusNotFound)
	}
}

// device answers GET /api/v1/device the way the portal does: 401 with the
// reason in WWW-Authenticate.
func (p *fakePortal) device(response http.ResponseWriter, request *http.Request, now time.Time) {
	refusal := p.deviceRefusal
	if p.refusedProof(request.Header.Get("DPoP"), now) != "" {
		refusal = "invalid_dpop_proof"
	}
	if refusal != "" {
		response.Header().Set("WWW-Authenticate", `DPoP error="`+refusal+`", error_description="refused"`)
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})

		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"device": map[string]string{"status": "approved"}})
}

func (p *fakePortal) refusedProof(proof string, now time.Time) string {
	if p.refuseProofs {
		return "the signature does not hold"
	}
	parts := strings.Split(proof, ".")
	if len(parts) != 3 {
		return "the proof is missing"
	}
	var claims proofClaims
	content, err := jsonPart(parts[1])
	if err != nil || json.Unmarshal(content, &claims) != nil {
		return "the proof does not decode"
	}
	seconds := now.Unix()
	if claims.IssuedAt < seconds-300 || claims.IssuedAt > seconds+60 {
		return "iat is too old or in the future"
	}

	return ""
}

func (p *fakePortal) token(response http.ResponseWriter) {
	pending := func(description string) {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "authorization_pending", "error_description": description,
		})
	}
	switch {
	case p.declined:
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "access_denied", "error_description": "The pairing was declined",
		})
	case !p.confirmed:
		pending("Nobody confirmed the code yet")
	case p.manual && !p.approved:
		pending(approvalPending)
	default:
		var identity any
		if p.identity != nil {
			identity = p.identity
		}
		writeJSON(response, http.StatusOK, map[string]any{
			"access_token": "bp_device_token", "token_type": "DPoP", "scope": "device",
			"product": map[string]string{"slug": "connect-local", "name": "Prusa Connect"},
			"device": map[string]any{
				"identifier": "instance-1", "fields": map[string]string{"instance_id": "instance-1"},
				"status": "approved", "verified": false, "lastVersion": nil, "lastSeenAt": nil,
			},
			"identity": identity,
		})
	}
}

func (p *fakePortal) set(change func(*fakePortal)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	change(p)
}

func (p *fakePortal) count(path string) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.requests[path]
}

func jsonPart(encoded string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(encoded)
}

func portalClient(t *testing.T, server *httptest.Server) (*Client, *time.Time) {
	t.Helper()
	client, now := testClient(t, server)
	*now = time.Now().UTC().Truncate(time.Second)

	return client, now
}

func TestAutomaticApprovalPairsWithIdentityAndKeepsTheCompleteAddress(t *testing.T) {
	t.Parallel()

	server, portal := newFakePortal(t, &fakePortal{
		identity: map[string]string{"subject": "tester-1", "name": "Ada Lovelace"},
	})
	client, now := portalClient(t, server)

	status, err := client.Start(t.Context())
	require.NoError(t, err)
	require.Equal(t, StatePending, status.State)
	require.Equal(t, StepConfirmation, status.Step)
	require.Equal(t, "WDJB-MJHT", status.UserCode)
	require.Equal(t, server.URL+"/device", status.VerificationURI)
	require.Equal(t, server.URL+"/device?user_code=WDJB-MJHT", status.VerificationURIComplete)

	*now = now.Add(defaultInterval * time.Second)
	status, err = client.Refresh(t.Context())
	require.NoError(t, err)
	require.Equal(t, StepConfirmation, status.Step)

	portal.set(func(p *fakePortal) { p.confirmed = true })
	*now = now.Add(defaultInterval * time.Second)
	status, err = client.Refresh(t.Context())
	require.NoError(t, err)
	require.Equal(t, StatePaired, status.State)
	require.Equal(t, &Identity{Subject: "tester-1", Name: "Ada Lovelace"}, status.Identity)
	require.Equal(t, &Product{Slug: "connect-local", Name: "Prusa Connect"}, status.Product)
	require.Equal(t, &Device{Identifier: "instance-1"}, status.Device)
	require.Empty(t, status.UserCode)
	token, _ := client.store.access()
	require.Equal(t, "bp_device_token", token)
}

func TestManualApprovalWaitsForTheTeamAndFallsBackToProductAndDevice(t *testing.T) {
	t.Parallel()

	server, portal := newFakePortal(t, &fakePortal{manual: true})
	client, now := portalClient(t, server)
	_, err := client.Start(t.Context())
	require.NoError(t, err)

	portal.set(func(p *fakePortal) { p.confirmed = true })
	*now = now.Add(defaultInterval * time.Second)
	status, err := client.Refresh(t.Context())
	require.NoError(t, err)
	require.Equal(t, StatePending, status.State)
	require.Equal(t, StepApproval, status.Step)

	reopened, err := Open(client.store.path)
	require.NoError(t, err)
	require.Equal(t, StepApproval, reopened.Status().Step, "the step survives a restart")

	*now = now.Add(defaultInterval * time.Second)
	status, err = client.Refresh(t.Context())
	require.NoError(t, err)
	require.Equal(t, StepApproval, status.Step)

	portal.set(func(p *fakePortal) { p.approved = true })
	*now = now.Add(defaultInterval * time.Second)
	status, err = client.Refresh(t.Context())
	require.NoError(t, err)
	require.Equal(t, StatePaired, status.State)
	require.Nil(t, status.Identity, "the portal sent identity: null")
	require.Equal(t, "Prusa Connect", status.Product.Name)
	require.Equal(t, "instance-1", status.Device.Identifier)
}

func TestDeclinedPairingIsDenied(t *testing.T) {
	t.Parallel()

	server, portal := newFakePortal(t, &fakePortal{})
	client, now := portalClient(t, server)
	_, err := client.Start(t.Context())
	require.NoError(t, err)
	portal.set(func(p *fakePortal) { p.declined = true })
	*now = now.Add(defaultInterval * time.Second)
	status, err := client.Refresh(t.Context())
	require.NoError(t, err)
	require.Equal(t, StateDenied, status.State)
}

func TestCompleteAddressOnAnotherOriginIsDropped(t *testing.T) {
	t.Parallel()

	for name, complete := range map[string]string{
		"other origin": "https://attacker.example/device?user_code=WDJB-MJHT",
		"script":       "javascript:alert(1)",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server, _ := newFakePortal(t, &fakePortal{complete: &complete})
			client, _ := portalClient(t, server)
			status, err := client.Start(t.Context())
			require.NoError(t, err)
			require.Empty(t, status.VerificationURIComplete)
			require.Equal(t, server.URL+"/device", status.VerificationURI)
		})
	}
}

func TestStartOnAPairedServerIsRefusedWithoutContactingThePortal(t *testing.T) {
	t.Parallel()

	server, portal := newFakePortal(t, &fakePortal{})
	client, _ := portalClient(t, server)
	markPaired(t, client.store, "kept-token")

	_, err := client.Start(t.Context())
	require.ErrorIs(t, err, ErrAlreadyPaired)
	require.Equal(t, 0, portal.count(DeviceAuthorizationPath))
	token, _ := client.store.access()
	require.Equal(t, "kept-token", token)
	require.Equal(t, StatePaired, client.store.Status().State)
}

func TestStartAfterARevokedDeniedOrExpiredPairingAsksForANewCode(t *testing.T) {
	t.Parallel()

	server, portal := newFakePortal(t, &fakePortal{})
	client, _ := portalClient(t, server)
	markPaired(t, client.store, "refused-token")
	require.NoError(t, client.store.revoke("refused-token"))
	require.Equal(t, StateRevoked, client.store.Status().State)

	status, err := client.Start(t.Context())
	require.NoError(t, err)
	require.Equal(t, StatePending, status.State)
	require.Equal(t, 1, portal.count(DeviceAuthorizationPath))
}

func TestStatusAnswersWhileThePortalIsSlow(t *testing.T) {
	t.Parallel()

	hold := make(chan struct{})
	server, _ := newFakePortal(t, &fakePortal{hold: hold})
	client, _ := portalClient(t, server)

	started := make(chan error, 1)
	go func() {
		_, err := client.Start(t.Context())
		started <- err
	}()

	answered := make(chan Status, 1)
	go func() {
		status, _ := client.Status(t.Context())
		answered <- status
	}()
	select {
	case status := <-answered:
		require.Equal(t, StateUnpaired, status.State)
	case <-time.After(5 * time.Second):
		t.Fatal("Status waited for the portal call of Start")
	}
	close(hold)
	require.NoError(t, <-started)
}

func TestAPortalThatNeverAnswersTimesOut(t *testing.T) {
	t.Parallel()

	hold := make(chan struct{})
	server, _ := newFakePortal(t, &fakePortal{hold: hold})
	t.Cleanup(func() { close(hold) })
	client, _ := portalClient(t, server)
	client.timeout = 50 * time.Millisecond

	began := time.Now()
	_, err := client.Start(t.Context())
	require.ErrorIs(t, err, ErrUpstream)
	require.Less(t, time.Since(began), 5*time.Second)
	require.Equal(t, StateUnpaired, client.store.Status().State)
}

func TestConcurrentRefreshesPollOnce(t *testing.T) {
	t.Parallel()

	hold := make(chan struct{})
	server, portal := newFakePortal(t, &fakePortal{})
	client, now := portalClient(t, server)
	_, err := client.Start(t.Context())
	require.NoError(t, err)
	portal.set(func(p *fakePortal) { p.hold = hold })
	*now = now.Add(defaultInterval * time.Second)

	const callers = 6
	var group sync.WaitGroup
	group.Add(callers)
	for range callers {
		go func() {
			defer group.Done()
			_, _ = client.Refresh(t.Context())
		}()
	}
	require.Eventually(t, func() bool {
		client.mu.Lock()
		defer client.mu.Unlock()

		return client.polling
	}, 5*time.Second, time.Millisecond)
	close(hold)
	group.Wait()
	require.Equal(t, 1, portal.count(TokenPath))
}

func TestAPollAnsweredAfterDisconnectDoesNotPairTheServer(t *testing.T) {
	t.Parallel()

	hold := make(chan struct{})
	server, portal := newFakePortal(t, &fakePortal{confirmed: true})
	client, now := portalClient(t, server)
	_, err := client.Start(t.Context())
	require.NoError(t, err)
	portal.set(func(p *fakePortal) { p.hold = hold })
	*now = now.Add(defaultInterval * time.Second)

	polled := make(chan error, 1)
	go func() {
		_, refreshErr := client.Refresh(t.Context())
		polled <- refreshErr
	}()
	require.Eventually(t, func() bool {
		client.mu.Lock()
		defer client.mu.Unlock()

		return client.polling
	}, 5*time.Second, time.Millisecond)
	require.NoError(t, client.Disconnect(t.Context()))
	close(hold)
	require.NoError(t, <-polled)

	require.Equal(t, StateUnpaired, client.store.Status().State)
	token, _ := client.store.access()
	require.Empty(t, token)
}

func TestRateLimitedPairingWaitsForRetryAfter(t *testing.T) {
	t.Parallel()

	server, portal := newFakePortal(t, &fakePortal{retryAfter: "120"})
	client, now := portalClient(t, server)

	_, err := client.Start(t.Context())
	var limited *RateLimitError
	require.ErrorAs(t, err, &limited)
	require.ErrorIs(t, err, ErrRateLimited)
	require.Equal(t, 2*time.Minute, limited.RetryAfter)

	*now = now.Add(time.Minute)
	_, err = client.Start(t.Context())
	require.ErrorAs(t, err, &limited)
	require.Equal(t, time.Minute, limited.RetryAfter)
	require.Equal(t, 1, portal.count(DeviceAuthorizationPath), "the wait is kept without asking again")

	portal.set(func(p *fakePortal) { p.retryAfter = "" })
	*now = now.Add(time.Minute)
	status, err := client.Start(t.Context())
	require.NoError(t, err)
	require.Equal(t, StatePending, status.State)
}

func TestRetryAfterReadsSecondsAndDates(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_800_000_000, 0).UTC()
	for name, test := range map[string]struct {
		header string
		want   time.Duration
	}{
		"seconds":  {"30", 30 * time.Second},
		"date":     {now.Add(90 * time.Second).Format(http.TimeFormat), 90 * time.Second},
		"missing":  {"", defaultRetryAfter},
		"garbage":  {"soon", defaultRetryAfter},
		"past":     {now.Add(-time.Hour).Format(http.TimeFormat), defaultRetryAfter},
		"huge":     {"10000000000", time.Hour},
		"far date": {now.Add(400 * 24 * time.Hour).Format(http.TimeFormat), time.Hour},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, retryAfter(test.header, now))
		})
	}
}

func TestAWrongClockIsCorrectedFromThePortalDate(t *testing.T) {
	t.Parallel()

	for name, offset := range map[string]time.Duration{
		"server behind": 2 * time.Hour,
		"server ahead":  -10 * time.Minute,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server, portal := newFakePortal(t, &fakePortal{})
			client, now := portalClient(t, server)
			portal.set(func(p *fakePortal) { p.clock = func() time.Time { return now.Add(offset) } })

			status, err := client.Start(t.Context())
			require.NoError(t, err)
			require.Equal(t, StatePending, status.State)
			require.Equal(t, 2, portal.count(DeviceAuthorizationPath), "one refused proof, one corrected")

			*now = now.Add(defaultInterval * time.Second)
			_, err = client.Refresh(t.Context())
			require.NoError(t, err)
			require.Equal(t, 1, portal.count(TokenPath), "later proofs carry the correction")
		})
	}
}

func TestAProofRefusedAfterTheCorrectionAsksToCheckTheClock(t *testing.T) {
	t.Parallel()

	server, portal := newFakePortal(t, &fakePortal{refuseProofs: true})
	client, now := portalClient(t, server)
	portal.set(func(p *fakePortal) { p.clock = func() time.Time { return now.Add(3 * time.Hour) } })

	_, err := client.Start(t.Context())
	var skew *ClockSkewError
	require.ErrorAs(t, err, &skew)
	require.ErrorIs(t, err, ErrClockSkew)
	require.Equal(t, 3*time.Hour, skew.Offset)
	require.Equal(t, 2, portal.count(DeviceAuthorizationPath))
}

func TestAProofRefusedWithTheClockRightIsAnUpstreamFault(t *testing.T) {
	t.Parallel()

	server, portal := newFakePortal(t, &fakePortal{refuseProofs: true})
	client, now := portalClient(t, server)
	portal.set(func(p *fakePortal) { p.clock = func() time.Time { return *now } })

	_, err := client.Start(t.Context())
	require.ErrorIs(t, err, ErrUpstream)
	require.NotErrorIs(t, err, ErrClockSkew)
	require.Equal(t, 1, portal.count(DeviceAuthorizationPath), "no retry when the clocks agree")
}

func TestRejectedAccessTokenMovesThePairingToRevoked(t *testing.T) {
	t.Parallel()

	store, err := Open(filepath.Join(t.TempDir(), pairingFile))
	require.NoError(t, err)
	_, err = store.start(testPairing("rejected"), nil)
	require.NoError(t, err)
	require.NoError(t, store.approve("rejected", approval{
		token: "refused-token", identity: &Identity{Subject: "tester-1", Name: "Ada"},
	}))

	request := httptest.NewRequest(http.MethodGet, "https://portal.example/api/connect-local/v1/core/releases", nil)
	require.NoError(t, store.Authorize(request))
	response := &http.Response{
		StatusCode: http.StatusUnauthorized, Request: request,
		Header: http.Header{"Date": {time.Now().UTC().Format(http.TimeFormat)}},
	}
	retry, err := store.Rejected(response)
	require.NoError(t, err)
	require.False(t, retry)

	status := store.Status()
	require.Equal(t, StateRevoked, status.State)
	require.Equal(t, "Ada", status.Identity.Name, "the page can name the pairing that stopped working")
	token, _ := store.access()
	require.Empty(t, token)

	reopened, err := Open(store.path)
	require.NoError(t, err)
	require.Equal(t, StateRevoked, reopened.Status().State)
}

func TestRejectedWithAWrongClockRetriesOnceWithACorrectedProof(t *testing.T) {
	t.Parallel()

	store, err := Open(filepath.Join(t.TempDir(), pairingFile))
	require.NoError(t, err)
	markPaired(t, store, "kept-token")

	request := httptest.NewRequest(http.MethodGet, "https://portal.example/api/connect-local/v1/core/releases", nil)
	require.NoError(t, store.Authorize(request))
	portalNow := time.Now().Add(2 * time.Hour)
	response := &http.Response{
		StatusCode: http.StatusUnauthorized, Request: request,
		Header: http.Header{"Date": {portalNow.UTC().Format(http.TimeFormat)}},
	}
	retry, err := store.Rejected(response)
	require.NoError(t, err)
	require.True(t, retry)
	require.Equal(t, StatePaired, store.Status().State)

	again := httptest.NewRequest(http.MethodGet, "https://portal.example/api/connect-local/v1/core/releases", nil)
	require.NoError(t, store.Authorize(again))
	var claims proofClaims
	decodeJWTPart(t, strings.Split(again.Header.Get("DPoP"), ".")[1], &claims)
	require.InDelta(t, portalNow.Unix(), claims.IssuedAt, 5)

	// The corrected proof was refused too: the token itself is gone.
	response.Request = again
	retry, err = store.Rejected(response)
	require.NoError(t, err)
	require.False(t, retry)
	require.Equal(t, StateRevoked, store.Status().State)
}

func TestRejectedIgnoresOtherAnswersAndOtherTokens(t *testing.T) {
	t.Parallel()

	store, err := Open(filepath.Join(t.TempDir(), pairingFile))
	require.NoError(t, err)
	markPaired(t, store, "current-token")

	stale := httptest.NewRequest(http.MethodGet, "https://portal.example/x", nil)
	stale.Header.Set("Authorization", "DPoP an-older-token")
	unsigned := httptest.NewRequest(http.MethodGet, "https://storage.example/x", nil)
	skewed := http.Header{"Date": {time.Now().Add(-25 * 365 * 24 * time.Hour).UTC().Format(http.TimeFormat)}}
	for _, response := range []*http.Response{
		{StatusCode: http.StatusForbidden, Request: stale},
		{StatusCode: http.StatusUnauthorized, Request: stale},
		{StatusCode: http.StatusUnauthorized},
		{StatusCode: http.StatusUnauthorized, Request: unsigned, Header: skewed},
		{StatusCode: http.StatusUnauthorized, Request: stale, Header: skewed},
	} {
		retry, rejectErr := store.Rejected(response)
		require.NoError(t, rejectErr)
		require.False(t, retry)
	}
	require.Equal(t, StatePaired, store.Status().State)
	require.Zero(t, store.skew.Load(), "only a refusal of the token in use may move the clock")
}

func TestAClockDifferenceOverADayIsNotCorrected(t *testing.T) {
	t.Parallel()

	store, err := Open(filepath.Join(t.TempDir(), pairingFile))
	require.NoError(t, err)
	markPaired(t, store, "kept-token")

	request := httptest.NewRequest(http.MethodGet, "https://portal.example/x", nil)
	require.NoError(t, store.Authorize(request))
	retry, err := store.Rejected(&http.Response{
		StatusCode: http.StatusUnauthorized, Request: request,
		Header: http.Header{"Date": {time.Now().Add(-48 * time.Hour).UTC().Format(http.TimeFormat)}},
	})
	require.NoError(t, err)
	require.False(t, retry)
	require.Zero(t, store.skew.Load())
}

func TestADamagedPairingFileIsMovedAsideAndTheServerStartsUnpaired(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]string{
		"not json":        "{this is not json",
		"unknown version": `{"version":99,"private_key":"x","state":"paired"}`,
		"bad key":         `{"version":2,"private_key":"not-a-key","state":"unpaired"}`,
		"bad state":       `{"version":2,"private_key":"","state":"dancing"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.d", pairingFile)
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
			require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
			require.NoError(t, os.Chmod(path, 0o644))

			store, err := Open(path)
			require.NoError(t, err)
			require.Equal(t, StateUnpaired, store.Status().State)
			require.NotEmpty(t, store.Recovered())
			require.True(t, strings.HasPrefix(store.Recovered(), path+".broken-"))
			kept, err := os.ReadFile(store.Recovered())
			require.NoError(t, err)
			require.Equal(t, content, string(kept), "the damaged file is kept for inspection")
			if runtime.GOOS != "windows" {
				info, statErr := os.Stat(store.Recovered())
				require.NoError(t, statErr)
				require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the damaged file may still hold the key")
			}

			reopened, err := Open(path)
			require.NoError(t, err)
			require.Empty(t, reopened.Recovered())
		})
	}
}

func TestAnUnreadablePairingFileStillFailsTheStart(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), pairingFile)
	require.NoError(t, os.Mkdir(path, 0o700))
	_, err := Open(path)
	require.Error(t, err)
}

func TestTheServerNameIsCutTo200Characters(t *testing.T) {
	t.Parallel()

	server, portal := newFakePortal(t, &fakePortal{})
	store, err := Open(filepath.Join(t.TempDir(), pairingFile))
	require.NoError(t, err)
	client, err := NewClient(server.URL, server.Client(), store, Metadata{Name: strings.Repeat("Dílna ", 60)})
	require.NoError(t, err)
	_, err = client.Start(t.Context())
	require.NoError(t, err)

	portal.mu.Lock()
	sent := portal.form["name"][0]
	portal.mu.Unlock()
	require.Equal(t, 200, utf8.RuneCountInString(sent))
	require.True(t, utf8.ValidString(sent))
	require.True(t, strings.HasPrefix(strings.Repeat("Dílna ", 60), sent))
}

func TestClockSkewErrorNamesTheDifference(t *testing.T) {
	t.Parallel()

	err := error(&ClockSkewError{Offset: -(2*time.Hour + 400*time.Millisecond)})
	require.ErrorIs(t, err, ErrClockSkew)
	require.Contains(t, err.Error(), "2h0m0s")
}

func TestAFailedPollKeepsTheInterval(t *testing.T) {
	t.Parallel()

	server, portal := newFakePortal(t, &fakePortal{broken: true})
	client, now := portalClient(t, server)
	_, err := client.Start(t.Context())
	require.NoError(t, err)
	*now = now.Add(defaultInterval * time.Second)

	for range 10 {
		_, _ = client.Refresh(t.Context())
	}
	require.Equal(t, 1, portal.count(TokenPath), "only the first Refresh was due")

	*now = now.Add(defaultInterval * time.Second)
	_, err = client.Refresh(t.Context())
	require.ErrorIs(t, err, ErrUpstream)
	require.Equal(t, 2, portal.count(TokenPath))
}

func TestADisconnectDuringStartDropsTheCode(t *testing.T) {
	t.Parallel()

	hold := make(chan struct{})
	server, portal := newFakePortal(t, &fakePortal{hold: hold})
	client, _ := portalClient(t, server)

	started := make(chan error, 1)
	go func() {
		_, err := client.Start(t.Context())
		started <- err
	}()
	require.Eventually(t, func() bool {
		client.mu.Lock()
		defer client.mu.Unlock()

		return client.starting != nil
	}, 5*time.Second, time.Millisecond)
	require.NoError(t, client.Disconnect(t.Context()))
	close(hold)
	require.NoError(t, <-started)

	require.Equal(t, StateUnpaired, client.store.Status().State,
		"the code is bound to the key the disconnect rotated")
	require.Equal(t, 1, portal.count(DeviceAuthorizationPath))
}

func TestAStartOutlivesTheCallerThatBeganIt(t *testing.T) {
	t.Parallel()

	hold := make(chan struct{})
	server, _ := newFakePortal(t, &fakePortal{hold: hold})
	client, _ := portalClient(t, server)

	first, cancel := context.WithCancel(t.Context())
	firstDone := make(chan error, 1)
	go func() {
		_, err := client.Start(first)
		firstDone <- err
	}()
	require.Eventually(t, func() bool {
		client.mu.Lock()
		defer client.mu.Unlock()

		return client.starting != nil
	}, 5*time.Second, time.Millisecond)

	secondDone := make(chan Status, 1)
	go func() {
		status, _ := client.Start(t.Context())
		secondDone <- status
	}()
	cancel()
	close(hold)

	require.Equal(t, StatePending, (<-secondDone).State)
	<-firstDone
}

func TestAVerificationPageOffThePortalIsRefused(t *testing.T) {
	t.Parallel()

	server, _ := newFakePortal(t, &fakePortal{page: "https://attacker.example/device"})
	client, _ := portalClient(t, server)

	_, err := client.Start(t.Context())
	require.ErrorIs(t, err, ErrUpstream)
	require.Equal(t, StateUnpaired, client.store.Status().State)
}

func TestAProofRefusedWithTheCorrectionInUseIsNotTheClock(t *testing.T) {
	t.Parallel()

	server, portal := newFakePortal(t, &fakePortal{})
	client, now := portalClient(t, server)
	portal.set(func(p *fakePortal) { p.clock = func() time.Time { return now.Add(2 * time.Hour) } })
	_, err := client.Start(t.Context())
	require.NoError(t, err)

	portal.set(func(p *fakePortal) { p.refuseProofs = true })
	*now = now.Add(defaultInterval * time.Second)
	_, err = client.Refresh(t.Context())
	require.ErrorIs(t, err, ErrUpstream)
	require.NotErrorIs(t, err, ErrClockSkew)
}

func TestRefreshAsksThePortalWhetherAPairingStillWorks(t *testing.T) {
	t.Parallel()

	// One clock for both sides, moved only under the portal's lock, so a
	// check a minute later is not refused as a proof from the future.
	now := time.Now()
	server, portal := newFakePortal(t, &fakePortal{clock: func() time.Time { return now }})
	advance := func() {
		portal.mu.Lock()
		now = now.Add(verifyInterval)
		portal.mu.Unlock()
	}
	store, err := Open(filepath.Join(t.TempDir(), pairingFile))
	require.NoError(t, err)
	client, err := NewClient(server.URL, server.Client(), store, Metadata{})
	require.NoError(t, err)
	client.now = func() time.Time { return now }
	markPaired(t, store, "kept-token")
	checks := func() int {
		portal.mu.Lock()
		defer portal.mu.Unlock()

		return portal.requests[devicePath]
	}

	status, err := client.Refresh(t.Context())
	require.NoError(t, err)
	require.Equal(t, StatePaired, status.State)
	require.False(t, status.AccessSuspended)
	require.Equal(t, 1, checks())

	// A second refresh within the minute does not ask again.
	portal.mu.Lock()
	portal.deviceRefusal = "access_suspended"
	portal.mu.Unlock()
	_, err = client.Refresh(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, checks())

	advance()
	status, err = client.Refresh(t.Context())
	require.NoError(t, err)
	require.Equal(t, StatePaired, status.State, "a suspension keeps the pairing")
	require.True(t, status.AccessSuspended)

	portal.mu.Lock()
	portal.deviceRefusal = ""
	portal.mu.Unlock()
	advance()
	status, err = client.Refresh(t.Context())
	require.NoError(t, err)
	require.False(t, status.AccessSuspended, "restored access shows at the next check")

	portal.mu.Lock()
	portal.deviceRefusal = "invalid_token"
	portal.mu.Unlock()
	advance()
	status, err = client.Refresh(t.Context())
	require.NoError(t, err)
	require.Equal(t, StateRevoked, status.State)
}

func TestRejectedFollowsTheReasonThePortalNames(t *testing.T) {
	t.Parallel()

	refused := func(t *testing.T, store *Store, reason string, date time.Time) (bool, error) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "https://portal.example/x", nil)
		require.NoError(t, store.Authorize(request))
		header := http.Header{"Date": {date.UTC().Format(http.TimeFormat)}}
		if reason != "" {
			header.Set("WWW-Authenticate", `DPoP error="`+reason+`", error_description="a, b"`)
		}

		return store.Rejected(&http.Response{StatusCode: http.StatusUnauthorized, Request: request, Header: header})
	}
	open := func(t *testing.T) *Store {
		t.Helper()
		store, err := Open(filepath.Join(t.TempDir(), pairingFile))
		require.NoError(t, err)
		markPaired(t, store, "kept-token")

		return store
	}

	t.Run("suspended", func(t *testing.T) {
		t.Parallel()
		store := open(t)
		retry, err := refused(t, store, "access_suspended", time.Now())
		require.ErrorIs(t, err, ErrAccessSuspended)
		require.False(t, retry)
		require.Equal(t, StatePaired, store.Status().State)
		require.True(t, store.Status().AccessSuspended)
	})
	t.Run("proof refused with a right clock keeps the token", func(t *testing.T) {
		t.Parallel()
		store := open(t)
		retry, err := refused(t, store, "invalid_dpop_proof", time.Now())
		require.NoError(t, err)
		require.False(t, retry)
		require.Equal(t, StatePaired, store.Status().State)
	})
	t.Run("proof refused with a wrong clock retries", func(t *testing.T) {
		t.Parallel()
		store := open(t)
		retry, err := refused(t, store, "invalid_dpop_proof", time.Now().Add(2*time.Hour))
		require.NoError(t, err)
		require.True(t, retry)
	})
	t.Run("invalid token revokes without touching the clock", func(t *testing.T) {
		t.Parallel()
		store := open(t)
		retry, err := refused(t, store, "invalid_token", time.Now().Add(2*time.Hour))
		require.NoError(t, err)
		require.False(t, retry)
		require.Equal(t, StateRevoked, store.Status().State)
		require.Zero(t, store.skew.Load())
	})
}
