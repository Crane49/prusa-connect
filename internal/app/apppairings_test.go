package app

import (
	"encoding/base64"
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
	ID          string   `json:"id"`
	Token       string   `json:"token"`
	AccessToken string   `json:"access_token"`
	Scopes      []string `json:"scopes"`
	Origin      string   `json:"origin"`
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

// The collection answers the credential in its JWT shape too. The shape works
// in the Authorization header and stops working with the bare token when the
// person revokes it. The auth.access_token cookie carries neither shape: a
// page script writes that cookie (ADR-20261112-nay).
func TestACollectedCredentialAlsoComesInAJWTShape(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, bearerStreamConfig(dir))

	owner := signUpTheOwner(t, running)
	collected := pairApp(t, running, owner, `["operate"]`)

	segments := strings.Split(collected.AccessToken, ".")
	require.Len(t, segments, 3, collected.AccessToken)
	require.Equal(t, collected.Token, segments[2], "the third segment is not the token")

	header, err := base64.RawURLEncoding.DecodeString(segments[0])
	require.NoError(t, err)
	require.JSONEq(t, `{"alg":"none","kid":"pcl","typ":"JWT"}`, string(header))

	payload, err := base64.RawURLEncoding.DecodeString(segments[1])
	require.NoError(t, err)

	var claims struct {
		Sub string `json:"sub"`
		Iss string `json:"iss"`
		Jti string `json:"jti"`
		Iat int64  `json:"iat"`
		Exp int64  `json:"exp"`
	}

	require.NoError(t, json.Unmarshal(payload, &claims))
	require.Equal(t, "prusa-connect", claims.Iss)
	require.Equal(t, collected.ID, claims.Jti)
	require.NotEmpty(t, claims.Sub)
	require.Less(t, claims.Iat, claims.Exp)

	inHeader := map[string]string{"Authorization": "Bearer " + collected.AccessToken}

	got := send(t, running, http.MethodGet, "/api/v1/devices", "", inHeader)
	require.Equal(t, http.StatusOK, got.status, got.body)

	for what, value := range map[string]string{"in its JWT shape": collected.AccessToken, "bare": collected.Token} {
		requireRefusedOver(t, running, "the credential "+what+" in the cookie",
			map[string]string{"Cookie": "auth.access_token=" + value})
	}

	revoked := send(t, running, http.MethodDelete, "/api/v1/auth/credentials/"+collected.ID, "", owner)
	require.Equal(t, http.StatusNoContent, revoked.status, revoked.body)

	requireRefusedOver(t, running, "a revoked credential in the header", inHeader)
}

// A page inside an app trades the app's credential for a page session: an
// HttpOnly, SameSite=Strict session cookie that speaks for the same person
// with the same scopes. Only the Authorization header opens one, never a
// cookie and never a credential of another kind. Revoking the app's
// credential ends the page session, the listing never shows it, and one
// address opens a bounded number (ADR-20261112-nay).
func TestAPageSessionStandsInForTheAppsCredential(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, bearerStreamConfig(dir))

	owner := signUpTheOwner(t, running)
	collected := pairApp(t, running, owner, `["read"]`)

	open := func(headers map[string]string) headed {
		return send(t, running, http.MethodPost, "/api/v1/auth/app-pairings/session", "", headers)
	}

	for what, token := range map[string]string{"bare": collected.Token, "in its JWT shape": collected.AccessToken} {
		opened := open(map[string]string{"Authorization": "Bearer " + token})
		require.Equalf(t, http.StatusOK, opened.status, "%s: %s", what, opened.body)
		require.Contains(t, opened.body, `"expires_at"`)
	}

	opened := open(map[string]string{"Authorization": "Bearer " + collected.AccessToken})
	require.Equal(t, http.StatusOK, opened.status, opened.body)

	cookies := (&http.Response{Header: opened.header}).Cookies()
	require.Len(t, cookies, 1)
	require.Equal(t, "pcl_session", cookies[0].Name)
	require.True(t, cookies[0].HttpOnly, "the page session cookie is readable by script")
	require.Equal(t, http.SameSiteStrictMode, cookies[0].SameSite)
	require.Equal(t, "/", cookies[0].Path)
	require.NotContains(t, cookies[0].Value, collected.Token, "the cookie carries the app's credential")
	require.LessOrEqual(t, time.Until(cookies[0].Expires), time.Hour+time.Minute)

	page := map[string]string{"Cookie": "pcl_session=" + cookies[0].Value}

	listed := send(t, running, http.MethodGet, "/api/v1/devices", "", page)
	require.Equal(t, http.StatusOK, listed.status, listed.body)

	// The scope of the app's credential holds: read cannot list another
	// person's credentials.
	scoped := send(t, running, http.MethodGet, "/api/v1/auth/credentials?subject=service_account:ci", "", page)
	require.Equal(t, http.StatusForbidden, scoped.status, scoped.body)

	// The listing shows the app once, not each page session it opened.
	paired := send(t, running, http.MethodGet, "/api/v1/auth/credentials?origin=app_pairing", "", owner)
	require.Equal(t, http.StatusOK, paired.status, paired.body)
	require.Equal(t, 1, strings.Count(paired.body, `"origin":"app_pairing"`), paired.body)

	for what, headers := range map[string]map[string]string{
		"the page session cookie alone":    page,
		"the app's credential in a cookie": {"Cookie": "auth.access_token=" + collected.AccessToken},
		"a page session as the bearer":     {"Authorization": "Bearer " + cookies[0].Value},
		"a person's session as the bearer": {"Authorization": "Bearer " + strings.TrimPrefix(owner["Cookie"], "pcl_session=")},
	} {
		refused := open(headers)
		require.NotEqualf(t, http.StatusOK, refused.status, "%s opened a page session: %s", what, refused.body)
		require.Emptyf(t, refused.header.Get("Set-Cookie"), "%s set a cookie", what)
	}

	revoked := send(t, running, http.MethodDelete, "/api/v1/auth/credentials/"+collected.ID, "", owner)
	require.Equal(t, http.StatusNoContent, revoked.status, revoked.body)

	requireRefusedOver(t, running, "a page session whose app credential was revoked", page)

	// One credential opens a bounded number from one address. A person's
	// session or rubbish in the header does not spend that count, so it
	// cannot lock the slicer out.
	other := pairApp(t, running, owner, `["read"]`)
	bearer := map[string]string{"Authorization": "Bearer " + other.Token}

	for range 40 {
		open(map[string]string{"Authorization": "Bearer " + strings.TrimPrefix(owner["Cookie"], "pcl_session=")})
		open(map[string]string{"Authorization": "Bearer pcl_AAAAAAAAAAAAAAAAAAAAAA_rubbish"})
	}

	var last headed

	for i := range 31 {
		last = open(bearer)
		if i < 30 {
			require.Equalf(t, http.StatusOK, last.status, "open %d: %s", i+1, last.body)
		}
	}

	require.Equal(t, http.StatusTooManyRequests, last.status, last.body)
	require.Contains(t, last.body, "PAGE_SESSION_LIMITED")
}

// The answer carries the cookie it sets, for an app that stores it into its
// web view itself, with the attributes the Set-Cookie header has.
func TestAPageSessionAnswersTheCookieItSets(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, bearerStreamConfig(dir))

	owner := signUpTheOwner(t, running)
	collected := pairApp(t, running, owner, `["operate"]`)

	opened := send(t, running, http.MethodPost, "/api/v1/auth/app-pairings/session", "",
		map[string]string{"Authorization": "Bearer " + collected.AccessToken})
	require.Equal(t, http.StatusOK, opened.status, opened.body)

	var body struct {
		ExpiresAt time.Time `json:"expires_at"`
		Cookie    struct {
			Name     string    `json:"name"`
			Value    string    `json:"value"`
			Path     string    `json:"path"`
			Expires  time.Time `json:"expires"`
			HTTPOnly bool      `json:"http_only"`
			SameSite string    `json:"same_site"`
			Secure   bool      `json:"secure"`
		} `json:"cookie"`
	}

	require.NoError(t, json.Unmarshal([]byte(opened.body), &body))

	cookies := (&http.Response{Header: opened.header}).Cookies()
	require.Len(t, cookies, 1)
	require.Equal(t, cookies[0].Name, body.Cookie.Name)
	require.Equal(t, cookies[0].Value, body.Cookie.Value)
	require.Equal(t, cookies[0].Path, body.Cookie.Path)
	require.Equal(t, cookies[0].Secure, body.Cookie.Secure)
	require.True(t, body.Cookie.HTTPOnly)
	require.Equal(t, "Strict", body.Cookie.SameSite)
	require.WithinDuration(t, body.ExpiresAt, body.Cookie.Expires, time.Second)
	require.NotContains(t, body.Cookie.Value, collected.Token)

	page := map[string]string{"Cookie": body.Cookie.Name + "=" + body.Cookie.Value}
	listed := send(t, running, http.MethodGet, "/api/v1/devices", "", page)
	require.Equal(t, http.StatusOK, listed.status, listed.body)
}

// pairApp runs one pairing to its collection: an app asks for scopes, the
// owner approves them, and the app collects after the interval.
func pairApp(t *testing.T, running *process, owner map[string]string, scopes string) collectedPairing {
	t.Helper()

	asked := send(t, running, http.MethodPost, "/api/v1/auth/app-pairings",
		`{"app_name":"PrusaSlicer","scopes":`+scopes+`}`, nil)
	require.Equal(t, http.StatusCreated, asked.status, asked.body)

	var issued issuedPairing

	require.NoError(t, json.Unmarshal([]byte(asked.body), &issued))

	approved := send(t, running, http.MethodPost, "/api/v1/auth/app-pairings/"+issued.UserCode+"/approve", "", owner)
	require.Equal(t, http.StatusNoContent, approved.status, approved.body)

	got := send(t, running, http.MethodPost, "/api/v1/auth/app-pairings/token",
		`{"device_code":"`+issued.DeviceCode+`"}`, nil)
	require.Equal(t, http.StatusOK, got.status, got.body)

	var collected collectedPairing

	require.NoError(t, json.Unmarshal([]byte(got.body), &collected))

	return collected
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
