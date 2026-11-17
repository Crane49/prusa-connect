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
