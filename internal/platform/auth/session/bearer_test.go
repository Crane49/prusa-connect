package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// bearerToken is shaped like a JWS and is the one the fake resolver admits.
const bearerToken = "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJ4In0.c2ln"

// recordingBearer is a bearer resolver that records the transport it was asked
// on and admits one token as one principal.
type recordingBearer struct {
	asked []auth.Transport
}

func (b *recordingBearer) Resolve(_ context.Context, token auth.Token, on auth.Transport) (auth.Credential, error) {
	b.asked = append(b.asked, on)

	if token != bearerToken {
		return auth.Credential{}, fault.AuthBearerSignature.New("not the token the fake knows")
	}

	return auth.Credential{Subject: "service_account:dashboard", Kind: auth.KindPrusaAccount,
		ExpiresAt: time.Now().Add(time.Hour)}, nil
}

// TestTheShapeOfTheTokenPicksThePath: a JWS goes to the bearer resolver on both
// transports and a minted token still goes to the verifier, so the session
// path is as it was and a caller cannot steer a minted token into the bearer
// check or a JWS into the store.
func TestTheShapeOfTheTokenPicksThePath(t *testing.T) {
	t.Parallel()

	store := auth.NewMemoryStore()
	bearer := &recordingBearer{}
	principals := NewPrincipals(auth.NewVerifier(store, nil), nil, streamPath).WithBearer(bearer)

	session, _, err := auth.Issue(t.Context(), store, "operator", auth.KindSession, time.Now())
	require.NoError(t, err)

	overHTTP, err := principals.Resolve(requestWith(t, bearerToken, false))
	require.NoError(t, err)
	require.Equal(t, "service_account:dashboard", overHTTP.Subject)
	require.Equal(t, auth.KindPrusaAccount, overHTTP.Kind)

	onStream, err := principals.Resolve(requestWith(t, bearerToken, true))
	require.NoError(t, err)
	require.Equal(t, overHTTP.Subject, onStream.Subject)

	require.Equal(t, []auth.Transport{auth.TransportHTTP, auth.TransportSSE}, bearer.asked,
		"the bearer resolver has to be told the transport the route decided")

	asSession, err := principals.Principal(requestWith(t, session, true))
	require.NoError(t, err)
	require.Equal(t, "operator", asSession)
	require.Len(t, bearer.asked, 2, "a minted token reached the bearer resolver")

	_, err = principals.Principal(requestWith(t, "eyJhbGciOiJub25lIn0.eyJzdWIiOiJ4In0.", false))
	require.True(t, fault.HasCode(err, fault.AuthBearerSignature.Code()), "got %v", err)
}

// TestAServerWithNoBearerPathRefusesAJWSAsRubbish is the deployment that
// verifies no bearer token: the JWS falls through to the verifier and is
// refused in the one wording every unknown token gets.
func TestAServerWithNoBearerPathRefusesAJWSAsRubbish(t *testing.T) {
	t.Parallel()

	principals := NewPrincipals(auth.NewVerifier(auth.NewMemoryStore(), nil), nil, streamPath)

	_, err := principals.Principal(requestWith(t, bearerToken, false))
	require.True(t, auth.Refused(err), "got %v", err)
	require.True(t, fault.HasCode(err, fault.AuthTokenInvalid.Code()))
}

// TestTheBearerCookieCarriesOnlyABearerToken reads the cookie the old web
// client keeps its Prusa Account token in, after the header and the session
// cookie, and never a minted token: this server did not set that cookie.
func TestTheBearerCookieCarriesOnlyABearerToken(t *testing.T) {
	t.Parallel()

	withCookie := func(value string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
		r.AddCookie(&http.Cookie{Name: BearerCookieName, Value: value})

		return r
	}

	require.Equal(t, auth.Token(bearerToken), TokenFrom(withCookie(bearerToken)))
	require.Empty(t, TokenFrom(withCookie("pcl_minted_in_the_wrong_cookie")))
	require.Empty(t, TokenFrom(withCookie("not-a-token")))

	both := withCookie(bearerToken)
	both.AddCookie(&http.Cookie{Name: CookieName, Value: "pcl_session"})
	require.Equal(t, auth.Token("pcl_session"), TokenFrom(both), "the session cookie wins over the bearer one")

	header := withCookie(bearerToken)
	header.Header.Set("Authorization", "Bearer pcl_header")
	require.Equal(t, auth.Token("pcl_header"), TokenFrom(header), "the header wins over every cookie")
}

// An app pairing credential in its JWT shape is checked by the verifier in the
// Authorization header on both transports, and never by the bearer resolver.
// The auth.access_token cookie never carries it, bare or shaped: a page script
// wrote that cookie, so another script could read it (ADR-20261112-nay). A
// revoked or an expired one is refused.
func TestAnAppPairingCredentialInItsJWTShapeIsCheckedByTheVerifier(t *testing.T) {
	t.Parallel()

	minted := time.Now()
	store := auth.NewMemoryStore()
	bearer := &recordingBearer{}
	principals := NewPrincipals(auth.NewVerifier(store, nil), nil, streamPath).WithBearer(bearer)

	issue := func() (auth.Token, auth.Credential) {
		token, cred, err := auth.IssueAPI(t.Context(), store, auth.APISpec{
			Subject: "user:ada", Name: "PrusaSlicer", MintedBy: "user:ada",
			Scopes: []string{"operate"}, Origin: auth.OriginAppPairing,
		}, minted)
		require.NoError(t, err)

		return token, cred
	}

	inCookie := func(value auth.Token) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
		r.AddCookie(&http.Cookie{Name: BearerCookieName, Value: string(value)})

		return r
	}

	token, cred := issue()
	shaped := auth.WrapJWT(token, cred)

	for name, r := range map[string]*http.Request{
		"over HTTP": requestWith(t, shaped, false),
		"on SSE":    requestWith(t, shaped, true),
	} {
		resolved, err := principals.Resolve(r)
		require.NoError(t, err, name)
		require.Equal(t, cred.PublicID, resolved.PublicID, name)
		require.Equal(t, "user:ada", resolved.Subject, name)
	}

	require.Empty(t, bearer.asked, "an app pairing credential reached the bearer resolver")

	for name, value := range map[string]auth.Token{"bare": token, "in its JWT shape": shaped} {
		require.Empty(t, TokenFrom(inCookie(value)), "the credential %s was read from the cookie", name)

		_, err := principals.Resolve(inCookie(value))
		require.Error(t, err, "the credential %s in the cookie was admitted", name)
	}

	expired := NewPrincipals(auth.NewVerifier(store, func() time.Time { return cred.ExpiresAt }), nil, streamPath)

	_, err := expired.Resolve(requestWith(t, shaped, false))
	require.True(t, auth.Refused(err), "an expired credential was admitted: %v", err)

	revokedToken, revoked := issue()
	require.NoError(t, auth.NewVerifier(store, nil).Revoke(t.Context(), revoked.PublicID))

	_, err = principals.Resolve(requestWith(t, auth.WrapJWT(revokedToken, revoked), false))
	require.True(t, auth.Refused(err), "a revoked credential was admitted: %v", err)
}
