package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// streamHeartbeat is the keep-alive this lane runs the core on. The credential
// re-check rides it, so the interval is what bounds how long a revoked token
// keeps a stream open, and the case below asserts against exactly that.
const streamHeartbeat = 200 * time.Millisecond

// bearerStreamConfig is the smallest deployment a client SDK meets: the
// password login on, so there is somebody to mint a token, and a keep-alive
// short enough that a revocation is noticed inside the budget.
func bearerStreamConfig(dir string) string {
	return lines(
		"core:",
		"  log:",
		"    level: error",
		"  authz:",
		"    database: '"+slashed(filepath.Join(dir, "authz.db"))+"'",
		"  overlay: '"+slashed(filepath.Join(dir, "resources.json"))+"'",
		"  auth:",
		"    local: true",
		"    setup_code: "+testSetupCode,
		"  events:",
		"    heartbeat: "+streamHeartbeat.String(),
	)
}

// TestABearerTokenSubscribesUntilItIsRevoked is what a client SDK holding a
// token is worth on the stream, over the shipping composition: the token opens
// GET /api/v1/events, the stream carries what the holder may see, and revoking
// the token ends it inside one keep-alive with the frame that tells the reader
// it was logged out rather than dropped. The session that minted the token is
// untouched by any of it.
func TestABearerTokenSubscribesUntilItIsRevoked(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, bearerStreamConfig(dir))

	owner := signUpTheOwner(t, running)
	minted := mintOver(t, running, owner, `{"name":"sdk","scopes":["administer"]}`)
	asBearer := map[string]string{"Authorization": "Bearer " + minted.Token}

	// Open before the printer dials in, so the arrival is the event that proves
	// the stream is carrying anything.
	frames := watchWith(t, running, asBearer)

	dialInUnwatched(t, running, "SIM-BEARER")

	require.Eventually(t, func() bool { return frames.saw(event.TypeDeviceArrived) },
		settle, 25*time.Millisecond, "a printer dialled in and the bearer's stream never carried it")

	// The session still reads its own stream, before and after the revocation
	// below, which is the path that existed first and must not have moved.
	asSession := watchWith(t, running, owner)

	revoked := send(t, running, http.MethodDelete, "/api/v1/auth/credentials/"+minted.ID, "", owner)
	require.Equal(t, http.StatusNoContent, revoked.status, revoked.body)

	revokedAt := time.Now()

	require.Eventually(t, func() bool { return !frames.still() }, settle, 10*time.Millisecond,
		"the token was revoked and the stream it opened stayed open")

	noticed := time.Since(revokedAt)
	require.Less(t, noticed, 5*streamHeartbeat,
		"the revocation reached the stream after %s, with a %s keep-alive", noticed, streamHeartbeat)
	require.True(t, frames.saw(httpapi.CredentialExpired),
		"the stream ended without telling the reader the credential had gone: %v", frames.kinds())

	t.Logf("observed: the revocation ended the bearer's stream after %s", noticed)

	refused := send(t, running, http.MethodGet, "/api/v1/events", "", asBearer)
	require.Equal(t, http.StatusUnauthorized, refused.status,
		"the revoked token opened a new stream: %s", refused.body)

	require.True(t, asSession.still(), "revoking the token ended the session's stream too")

	dialInUnwatched(t, running, "SIM-AFTER")

	require.Eventually(t, func() bool { return asSession.saw(event.TypeDeviceArrived) },
		settle, 25*time.Millisecond, "the session's stream carried nothing after the token was revoked")
}

// TestAnExpiredBearerTokenIsRefusedTheStream is the other way a token stops
// being accepted. The expiry passes under an open stream and the same re-check
// ends it, and the token is then refused at the door in the same words as a
// revoked one.
func TestAnExpiredBearerTokenIsRefusedTheStream(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, bearerStreamConfig(dir))

	owner := signUpTheOwner(t, running)

	const life = 2 * time.Second

	expiresAt := time.Now().Add(life).UTC().Format(time.RFC3339Nano)
	minted := mintOver(t, running, owner, `{"name":"short","scopes":["administer"],"expires_at":"`+expiresAt+`"}`)
	asBearer := map[string]string{"Authorization": "Bearer " + minted.Token}

	frames := watchWith(t, running, asBearer)

	require.Eventually(t, func() bool { return !frames.still() }, life+settle, 25*time.Millisecond,
		"the token expired and the stream it opened stayed open")
	require.True(t, frames.saw(httpapi.CredentialExpired),
		"the stream ended without telling the reader the credential had gone: %v", frames.kinds())

	refused := send(t, running, http.MethodGet, "/api/v1/events", "", asBearer)
	require.Equal(t, http.StatusUnauthorized, refused.status,
		"the expired token opened a new stream: %s", refused.body)
}

// dialInUnwatched starts a simulated printer against the core and leaves it
// running for the rest of the case. Whether it arrived is read off the stream,
// which is the assertion, rather than off a listing the reader here holds no
// cookie for, which is what dialIn waits on.
func dialInUnwatched(t *testing.T, running *process, serial string) {
	t.Helper()

	printer, err := simulator.New(simulator.Config{
		Model:          "MK4",
		Serial:         serial,
		BaseURL:        "http://" + running.addr,
		ReconnectDelay: 50 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan error, 1)

	go func() { done <- printer.RegisterAndRun(ctx) }()

	t.Cleanup(func() {
		cancel()
		<-done
	})
}

// signUpTheOwner runs the first-run path and logs the owner in, and returns
// the headers a request from them carries.
func signUpTheOwner(t *testing.T, running *process) map[string]string {
	t.Helper()

	body := `{"username":"` + testOwner + `","password":"` + testPassword + `"}`

	// The decision is already recorded and no account exists, so the account
	// path is the way in, and it takes the setup code (ADR-20260929-a4j rule 17).
	made := send(t, running, http.MethodPost, "/api/v1/auth/setup",
		`{"username":"`+testOwner+`","password":"`+testPassword+`","code":"`+testSetupCode+`"}`, nil)
	require.Equal(t, http.StatusCreated, made.status, made.body)

	opened := send(t, running, http.MethodPost, "/api/v1/auth/login", body, nil)
	require.Equal(t, http.StatusOK, opened.status, opened.body)

	raw := opened.header.Get("Set-Cookie")
	require.NotEmpty(t, raw, "the login set no cookie")

	return map[string]string{"Cookie": strings.SplitN(raw, ";", 2)[0]}
}

// mintedToken is what the mint answers, as this lane reads it.
type mintedToken struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}

// mintOver mints a token for the person the headers belong to.
func mintOver(t *testing.T, running *process, who map[string]string, body string) mintedToken {
	t.Helper()

	got := send(t, running, http.MethodPost, "/api/v1/auth/credentials", body, who)
	require.Equal(t, http.StatusCreated, got.status, got.body)

	var minted mintedToken

	require.NoError(t, json.Unmarshal([]byte(got.body), &minted))
	require.NotEmpty(t, minted.Token)
	require.NotEmpty(t, minted.ID)

	return minted
}

// headed is an answer with its headers, for the one call that reads a cookie.
type headed struct {
	status int
	body   string
	header http.Header
}

// send is one request carrying the headers given, read whole.
func send(t *testing.T, running *process, method, path, body string, headers map[string]string) headed {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	request, err := http.NewRequestWithContext(t.Context(), method, "http://"+running.addr+path, reader)
	require.NoError(t, err)

	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}

	for key, value := range headers {
		request.Header.Set(key, value)
	}

	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)

	defer func() { _ = response.Body.Close() }()

	read, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	return headed{status: response.StatusCode, body: string(read), header: response.Header}
}
