package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// issuedPairing is what an app reads back when it asks.
type issuedPairing struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	Interval        int    `json:"interval"`
}

// collectedPairing is the credential an app collects.
type collectedPairing struct {
	ID     string   `json:"id"`
	Token  string   `json:"token"`
	Scopes []string `json:"scopes"`
	Origin string   `json:"origin"`
}

// An app asks for a credential with no credential of its own, a person
// approves it with fewer scopes than it asked for, and the app collects a
// token once. Over the shipping composition the token observes a printer and
// is refused everything its scope leaves out: a command, another person's
// credentials, the grants. The person sees it among their credentials as a
// paired app and revokes it there.
func TestAnAppPairedByAPersonGetsATokenWithOnlyTheApprovedScopes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, bearerStreamConfig(dir))

	owner := signUpTheOwner(t, running)

	dialInUnwatched(t, running, "SIM-PAIRED")

	asked := send(t, running, http.MethodPost, "/api/v1/auth/app-pairings",
		`{"app_name":"kitchen display","scopes":["read","operate"]}`, nil)
	require.Equal(t, http.StatusCreated, asked.status, asked.body)

	var issued issuedPairing

	require.NoError(t, json.Unmarshal([]byte(asked.body), &issued))
	require.NotEmpty(t, issued.DeviceCode)
	require.Equal(t, 5, issued.Interval)
	require.True(t, strings.HasSuffix(issued.VerificationURI, "/pair"), issued.VerificationURI)

	poll := func() headed {
		return send(t, running, http.MethodPost, "/api/v1/auth/app-pairings/token",
			`{"device_code":"`+issued.DeviceCode+`"}`, nil)
	}

	pending := poll()
	require.Equal(t, http.StatusBadRequest, pending.status, pending.body)
	require.Contains(t, pending.body, "APP_PAIRING_PENDING")

	polled := time.Now()

	shown := send(t, running, http.MethodGet, "/api/v1/auth/app-pairings/"+issued.UserCode, "", owner)
	require.Equal(t, http.StatusOK, shown.status, shown.body)
	require.Contains(t, shown.body, `"app_name":"kitchen display"`)

	// An approval cannot hand out more than the app asked for.
	widened := send(t, running, http.MethodPost, "/api/v1/auth/app-pairings/"+issued.UserCode+"/approve",
		`{"scopes":["administer"]}`, owner)
	require.Equal(t, http.StatusUnprocessableEntity, widened.status, widened.body)

	approved := send(t, running, http.MethodPost, "/api/v1/auth/app-pairings/"+issued.UserCode+"/approve",
		`{"scopes":["read"]}`, owner)
	require.Equal(t, http.StatusNoContent, approved.status, approved.body)

	// The app keeps to the interval, as RFC 8628 asks of it.
	time.Sleep(time.Until(polled.Add(time.Duration(issued.Interval)*time.Second + 100*time.Millisecond)))

	got := poll()
	require.Equal(t, http.StatusOK, got.status, got.body)

	var collected collectedPairing

	require.NoError(t, json.Unmarshal([]byte(got.body), &collected))

	require.Equal(t, []string{"read"}, collected.Scopes)
	require.Equal(t, "app_pairing", collected.Origin)

	time.Sleep(time.Duration(issued.Interval) * time.Second)

	again := poll()
	require.Contains(t, again.body, "APP_PAIRING_UNKNOWN", "a code worked twice")

	app := map[string]string{"Authorization": "Bearer " + collected.Token}

	var id string

	require.Eventually(t, func() bool {
		listed := send(t, running, http.MethodGet, "/api/v1/devices", "", app)
		if listed.status != http.StatusOK || !strings.Contains(listed.body, `"id":"`) {
			return false
		}

		var page struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}

		require.NoError(t, json.Unmarshal([]byte(listed.body), &page))
		id = page.Items[0].ID

		return true
	}, settle, 25*time.Millisecond, "a read token does not list the printer its person sees")

	read := send(t, running, http.MethodGet, "/api/v1/devices/"+id, "", app)
	require.Equal(t, http.StatusOK, read.status, read.body)

	for _, c := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/devices/" + id + "/commands", `{"verb":"core.command.pause_print"}`},
		{http.MethodGet, "/api/v1/auth/credentials?subject=service_account:ci", ""},
		{http.MethodGet, "/api/v1/grants/org/default", ""},
	} {
		refused := send(t, running, c.method, c.path, c.body, app)
		require.Equalf(t, http.StatusForbidden, refused.status, "%s %s answered: %s", c.method, c.path, refused.body)
	}

	// The person reads it as a paired app, and revokes it.
	paired := send(t, running, http.MethodGet, "/api/v1/auth/credentials?origin=app_pairing", "", owner)
	require.Equal(t, http.StatusOK, paired.status, paired.body)
	require.Contains(t, paired.body, `"name":"kitchen display"`)

	revoked := send(t, running, http.MethodDelete, "/api/v1/auth/credentials/"+collected.ID, "", owner)
	require.Equal(t, http.StatusNoContent, revoked.status, revoked.body)

	requireRefusedOver(t, running, "a revoked app token", app)
}

// A person who denies a pairing is heard by the app once, and an api
// credential cannot decide a pairing at all: deciding hands out access.
func TestADeniedPairingIsToldOnceAndATokenCannotDecideOne(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, bearerStreamConfig(dir))

	owner := signUpTheOwner(t, running)
	script := bearerOf(mintOver(t, running, owner, `{"name":"script","scopes":["administer"]}`))

	asked := send(t, running, http.MethodPost, "/api/v1/auth/app-pairings",
		`{"app_name":"unknown thing","scopes":["administer"]}`, nil)
	require.Equal(t, http.StatusCreated, asked.status, asked.body)

	var issued issuedPairing

	require.NoError(t, json.Unmarshal([]byte(asked.body), &issued))

	byToken := send(t, running, http.MethodPost, "/api/v1/auth/app-pairings/"+issued.UserCode+"/approve", "", script)
	require.Equal(t, http.StatusForbidden, byToken.status, byToken.body)
	require.Contains(t, byToken.body, "SESSION_REQUIRED")

	denied := send(t, running, http.MethodPost, "/api/v1/auth/app-pairings/"+issued.UserCode+"/deny", "", owner)
	require.Equal(t, http.StatusNoContent, denied.status, denied.body)

	late := send(t, running, http.MethodPost, "/api/v1/auth/app-pairings/"+issued.UserCode+"/approve", "", owner)
	require.Equal(t, http.StatusConflict, late.status, late.body)
	require.Contains(t, late.body, "APP_PAIRING_CLOSED")

	told := send(t, running, http.MethodPost, "/api/v1/auth/app-pairings/token",
		`{"device_code":"`+issued.DeviceCode+`"}`, nil)
	require.Contains(t, told.body, "APP_PAIRING_DENIED")

	gone := send(t, running, http.MethodPost, "/api/v1/auth/app-pairings/token",
		`{"device_code":"`+issued.DeviceCode+`"}`, nil)
	require.Contains(t, gone.body, "APP_PAIRING_UNKNOWN")
}

// One address cannot ask without a bound: asking takes no credential, and the
// waiting pairings are a table somebody on the network could otherwise fill.
func TestOneAddressAsksForAFewPairingsAndIsThenRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, bearerStreamConfig(dir))

	signUpTheOwner(t, running)

	for range 5 {
		asked := send(t, running, http.MethodPost, "/api/v1/auth/app-pairings",
			`{"app_name":"eager","scopes":["read"]}`, nil)
		require.Equal(t, http.StatusCreated, asked.status, asked.body)
	}

	refused := send(t, running, http.MethodPost, "/api/v1/auth/app-pairings",
		`{"app_name":"eager","scopes":["read"]}`, nil)
	require.Equal(t, http.StatusTooManyRequests, refused.status, refused.body)
	require.Contains(t, refused.body, "APP_PAIRING_LIMITED")
}
