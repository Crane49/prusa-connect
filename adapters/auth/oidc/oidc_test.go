package oidc_test

import (
	"encoding/base64"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/adapters/auth/oidc"
	"github.com/prusa3d/prusa-connect/sdk/authapi"
	"github.com/prusa3d/prusa-connect/sdk/authapi/authapitest"
)

const clientID = "prusa-connect"

func provider(t *testing.T, iss *issuer, extra map[string]any) authapi.IdentityProvider {
	t.Helper()

	config := map[string]any{"issuer": iss.URL, "client_id": clientID}
	maps.Copy(config, extra)

	built, err := oidc.New(authapi.ProviderSpec{Type: oidc.Type, Config: config})
	require.NoError(t, err)

	return built
}

// The published suite every identity provider is held to, ours included. The
// callback is driven through the issuer rather than supplied as a fixed set,
// because a login here is bound to a nonce and a PKCE verifier made inside
// BeginLogin.
func TestIdentityProviderContract(t *testing.T) {
	authapitest.RunIdentityProviderContract(t, func(t *testing.T) authapitest.Harness {
		iss := newIssuer(t)

		return authapitest.Harness{
			Provider: provider(t, iss, nil),
			Callback: iss.Browser,
		}
	})
}

func TestTheFactoryIsRegisteredUnderItsType(t *testing.T) {
	t.Parallel()

	iss := newIssuer(t)

	built, err := oidc.New(authapi.ProviderSpec{
		Type:   oidc.Type,
		Config: map[string]any{"issuer": iss.URL, "client_id": clientID},
	})
	require.NoError(t, err)
	assert.Equal(t, oidc.Type, built.Name())
	assert.Equal(t, authapi.Capabilities{Redirect: true}, built.Capabilities())
}

func TestASpecWithoutAnIssuerOrAClientIsRejected(t *testing.T) {
	t.Parallel()

	for name, config := range map[string]map[string]any{
		"no issuer":    {"client_id": clientID},
		"no client id": {"issuer": "https://issuer.example"},
		"issuer is not a URL": {
			"issuer": "issuer.example", "client_id": clientID,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := oidc.New(authapi.ProviderSpec{Type: oidc.Type, Config: config})
			require.Error(t, err)
		})
	}
}

// A login carries the whole identity the issuer asserted, not just the subject.
// The host writes these onto its user row, so a missing role is a person who
// cannot do their job.
func TestACompletedLoginCarriesTheAssertedIdentity(t *testing.T) {
	t.Parallel()

	iss := newIssuer(t)
	p := provider(t, iss, nil)

	identity := complete(t, p, iss)

	assert.Equal(t, iss.Subject, identity.Subject)
	assert.Equal(t, iss.Email, identity.Email)
	assert.Equal(t, iss.Name, identity.DisplayName)
	assert.Equal(t, []string{"admin", "viewer"}, identity.Roles)
	assert.True(t, identity.HasRole("admin"))
}

// An issuer publishes roles under whatever claim its administrator configured,
// so the claim name is the operator's to name.
func TestRolesComeFromTheConfiguredClaim(t *testing.T) {
	t.Parallel()

	iss := newIssuer(t)
	iss.claim = "https://prusa.example/roles"
	iss.Roles = []string{"operator"}

	p := provider(t, iss, map[string]any{"role_claim": "https://prusa.example/roles"})

	assert.Equal(t, []string{"operator"}, complete(t, p, iss).Roles)
}

// The scopes an operator writes reach the authorize request. An issuer returns
// no email claim for a login that never asked for the email scope.
func TestTheConfiguredScopesReachTheAuthorizeRequest(t *testing.T) {
	t.Parallel()

	iss := newIssuer(t)
	p := provider(t, iss, map[string]any{"scopes": []any{"openid", "groups"}})

	authorizeURL, _, err := p.BeginLogin(t.Context(), "https://host/cb", "state")
	require.NoError(t, err)

	parsed, err := url.Parse(authorizeURL)
	require.NoError(t, err)
	assert.Equal(t, "openid groups", parsed.Query().Get("scope"))
	assert.Equal(t, "S256", parsed.Query().Get("code_challenge_method"))
	assert.NotEmpty(t, parsed.Query().Get("code_challenge"))
	assert.NotEmpty(t, parsed.Query().Get("nonce"))
}

// A token signed with an algorithm this adapter does not accept is refused
// before any claim in it is read. "none" is the attack the check exists for,
// and an HMAC algorithm is the same attack against a key the issuer publishes.
func TestATokenSignedWithAnotherAlgorithmIsRefused(t *testing.T) {
	t.Parallel()

	for _, algorithm := range []string{"none", "HS256", "RS512"} {
		t.Run(algorithm, func(t *testing.T) {
			t.Parallel()

			iss := newIssuer(t)
			iss.mintWith(func(nonce string) string {
				return iss.SignWith(
					map[string]any{"alg": algorithm, "typ": "JWT", "kid": keyID},
					iss.Claims(nonce))
			})

			_, err := attempt(t, provider(t, iss, nil), iss)
			require.ErrorIs(t, err, oidc.ErrIDToken)
			assert.Contains(t, err.Error(), algorithm)
		})
	}
}

// A token whose audience is another client is a token minted for somebody
// else's application at the same issuer, replayed here.
func TestATokenForAnotherClientIsRefused(t *testing.T) {
	t.Parallel()

	iss := newIssuer(t)
	iss.mintWith(func(nonce string) string {
		claims := iss.Claims(nonce)
		claims["aud"] = "some-other-application"

		return iss.Sign(claims)
	})

	_, err := attempt(t, provider(t, iss, nil), iss)
	require.ErrorIs(t, err, oidc.ErrIDToken)
	assert.Contains(t, err.Error(), "another client")
}

// A token from a different issuer at the same endpoint is the confused-deputy
// case: the endpoint answered, the signature checks out against the keys it
// publishes, and the assertion is somebody else's.
func TestATokenFromAnotherIssuerIsRefused(t *testing.T) {
	t.Parallel()

	iss := newIssuer(t)
	iss.mintWith(func(nonce string) string {
		claims := iss.Claims(nonce)
		claims["iss"] = "https://issuer.elsewhere"

		return iss.Sign(claims)
	})

	_, err := attempt(t, provider(t, iss, nil), iss)
	require.ErrorIs(t, err, oidc.ErrIDToken)
}

// A token that answers a login this server did not start is a captured callback
// replayed into a fresh session.
func TestATokenAnsweringAnotherLoginIsRefused(t *testing.T) {
	t.Parallel()

	iss := newIssuer(t)
	iss.mintWith(func(string) string {
		return iss.Sign(iss.Claims("a-nonce-from-another-login"))
	})

	_, err := attempt(t, provider(t, iss, nil), iss)
	require.ErrorIs(t, err, oidc.ErrIDToken)
	assert.Contains(t, err.Error(), "different login")
}

// An expired token is one a browser sat on. It is refused on its own claims,
// whatever the endpoint that handed it over said.
func TestAnExpiredTokenIsRefused(t *testing.T) {
	t.Parallel()

	iss := newIssuer(t)
	iss.mintWith(func(nonce string) string {
		claims := iss.Claims(nonce)
		claims["exp"] = 1

		return iss.Sign(claims)
	})

	_, err := attempt(t, provider(t, iss, nil), iss)
	require.ErrorIs(t, err, oidc.ErrIDToken)
	assert.Contains(t, err.Error(), "expired")
}

// A token signed by a key the issuer does not publish is refused after one
// refetch, which is what separates a rotation from a forgery.
func TestATokenSignedWithAnUnpublishedKeyIsRefused(t *testing.T) {
	t.Parallel()

	iss := newIssuer(t)
	other := newIssuer(t)

	iss.mintWith(func(nonce string) string {
		return other.SignWith(
			map[string]any{"alg": "RS256", "typ": "JWT", "kid": keyID},
			iss.Claims(nonce))
	})

	_, err := attempt(t, provider(t, iss, nil), iss)
	require.ErrorIs(t, err, oidc.ErrIDToken)
}

// A token naming a key the issuer does not publish costs the issuer one
// refetch, not one per token. The key name comes out of the token, so it is
// input somebody else chose: without the window a run of tokens naming keys
// nobody publishes is a run of requests at the issuer with this server as the
// amplifier.
func TestAnUnknownKeyIsRefetchedOnceAndThenThrottled(t *testing.T) {
	t.Parallel()

	iss := newIssuer(t)
	iss.mintWith(func(nonce string) string {
		return iss.SignWith(
			map[string]any{"alg": "RS256", "typ": "JWT", "kid": "a-kid-nobody-publishes"},
			iss.Claims(nonce))
	})

	p := provider(t, iss, nil)

	const attempts = 10

	for range attempts {
		_, err := attempt(t, p, iss)
		require.ErrorIs(t, err, oidc.ErrIDToken)
	}

	assert.EqualValues(t, 1, iss.jwksHits.Load(),
		"%d tokens naming an unpublished key cost the issuer %d fetches",
		attempts, iss.jwksHits.Load())
}

// A callback carrying the issuer's own refusal is not a login. It reaches here
// when a person declines the consent screen.
func TestARefusedConsentIsNotALogin(t *testing.T) {
	t.Parallel()

	iss := newIssuer(t)
	p := provider(t, iss, nil)

	_, providerState, err := p.BeginLogin(t.Context(), "https://host/cb", "state")
	require.NoError(t, err)

	_, err = p.CompleteLogin(t.Context(),
		url.Values{"error": {"access_denied"}}, providerState)
	require.ErrorIs(t, err, oidc.ErrCallback)
}

// A login state that was never issued, or that has been tampered with, cannot
// complete. The host replays this blob verbatim, so a corrupt one is a bug
// somewhere in its own persistence and not something to guess through.
func TestAnUnusableLoginStateIsRefused(t *testing.T) {
	t.Parallel()

	iss := newIssuer(t)
	p := provider(t, iss, nil)

	for name, state := range map[string]string{
		"empty":         "",
		"not base64url": "!!!!",
		"not a login":   "eyJ4IjoxfQ",
		// A state with the nonce taken out and the rest left intact. It is the
		// one rewrite worth making, because the nonce is what stops a token
		// minted for another login being replayed into this one.
		"no nonce": base64.RawURLEncoding.EncodeToString(
			[]byte(`{"v":"a-verifier","n":"","r":"https://host/cb"}`)),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := p.CompleteLogin(t.Context(), url.Values{"code": {"c"}}, state)
			require.ErrorIs(t, err, oidc.ErrCallback)
		})
	}
}

// An issuer that is down is not a reason to refuse to start. It is a reason for
// a login to fail, which is what the operator sees and can act on.
func TestADeadIssuerIsNotABootDependency(t *testing.T) {
	t.Parallel()

	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down for maintenance", http.StatusServiceUnavailable)
	}))
	t.Cleanup(dead.Close)

	built, err := oidc.New(authapi.ProviderSpec{
		Type:   oidc.Type,
		Config: map[string]any{"issuer": dead.URL, "client_id": clientID},
	})
	require.NoError(t, err, "a provider must build against an issuer that is not answering")

	_, _, err = built.BeginLogin(t.Context(), "https://host/cb", "state")
	require.ErrorIs(t, err, oidc.ErrDiscovery)
}

// Metadata naming a different issuer than the one asked for is how a login is
// pointed at an issuer nobody configured.
func TestMetadataForAnotherIssuerIsRefused(t *testing.T) {
	t.Parallel()

	var lying *httptest.Server

	lying = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		write(w, map[string]string{
			"issuer":                 "https://issuer.elsewhere",
			"authorization_endpoint": lying.URL + "/authorize",
			"token_endpoint":         lying.URL + "/token",
			"jwks_uri":               lying.URL + "/jwks",
		})
	}))
	t.Cleanup(lying.Close)

	built, err := oidc.New(authapi.ProviderSpec{
		Type:   oidc.Type,
		Config: map[string]any{"issuer": lying.URL, "client_id": clientID},
	})
	require.NoError(t, err)

	_, _, err = built.BeginLogin(t.Context(), "https://host/cb", "state")
	require.ErrorIs(t, err, oidc.ErrDiscovery)
}

// A trailing slash on the issuer is a typo that would otherwise make every
// token comparison fail against an issuer that does not send one.
func TestATrailingSlashOnTheIssuerIsToleratedEndToEnd(t *testing.T) {
	t.Parallel()

	iss := newIssuer(t)
	p := provider(t, iss, map[string]any{"issuer": iss.URL + "/"})

	assert.Equal(t, iss.Subject, complete(t, p, iss).Subject)
}

// complete drives one whole login and requires it to succeed.
func complete(t *testing.T, p authapi.IdentityProvider, iss *issuer) authapi.Identity {
	t.Helper()

	identity, err := attempt(t, p, iss)
	require.NoError(t, err)

	return identity
}

// attempt drives one whole login and hands back whatever it produced, so a case
// can assert on the refusal.
func attempt(t *testing.T, p authapi.IdentityProvider, iss *issuer) (authapi.Identity, error) {
	t.Helper()

	authorizeURL, providerState, err := p.BeginLogin(t.Context(), "https://host/auth/sso/callback", "state")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(authorizeURL, iss.URL), "the authorize URL must be the issuer's")

	return p.CompleteLogin(t.Context(), iss.Browser(t, authorizeURL), providerState)
}
