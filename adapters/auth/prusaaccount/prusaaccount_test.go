package prusaaccount_test

import (
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/adapters/auth/prusaaccount"
	"github.com/prusa3d/prusa-connect/sdk/authapi"
	"github.com/prusa3d/prusa-connect/sdk/authapi/authapitest"
)

func provider(t *testing.T, iss *issuer, extra map[string]any) authapi.TokenVerifier {
	t.Helper()

	config := map[string]any{"issuer": iss.URL, "jwks_url": iss.JWKSURL(), "audience": audience}
	maps.Copy(config, extra)

	built, err := prusaaccount.New(authapi.ProviderSpec{Type: prusaaccount.Type, Config: config})
	require.NoError(t, err)

	verifier, ok := built.(authapi.TokenVerifier)
	require.True(t, ok, "the provider advertises Bearer and must verify tokens")

	return verifier
}

// The published suite every identity provider is held to, ours included.
func TestIdentityProviderContract(t *testing.T) {
	authapitest.RunIdentityProviderContract(t, func(t *testing.T) authapitest.Harness {
		iss := newIssuer(t)

		built, err := prusaaccount.New(authapi.ProviderSpec{Type: prusaaccount.Type, Config: map[string]any{
			"issuer": iss.URL, "jwks_url": iss.JWKSURL(), "audience": audience,
		}})
		require.NoError(t, err)

		return authapitest.Harness{
			Provider:   built,
			ValidToken: func(*testing.T) string { return iss.Sign(iss.Claims()) },
		}
	})
}

func TestTheFactoryIsRegisteredUnderItsType(t *testing.T) {
	t.Parallel()

	iss := newIssuer(t)

	built, err := prusaaccount.New(authapi.ProviderSpec{
		Type:   prusaaccount.Type,
		Config: map[string]any{"issuer": iss.URL, "jwks_url": iss.JWKSURL()},
	})
	require.NoError(t, err)
	assert.Equal(t, prusaaccount.Type, built.Name())
	assert.Equal(t, authapi.Capabilities{Bearer: true}, built.Capabilities())
}

func TestASpecMissingAURLOrCarryingABadDurationIsRejected(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		config map[string]any
		want   error
	}{
		"no issuer":           {map[string]any{"jwks_url": "https://account.example/jwks"}, prusaaccount.ErrMissingIssuer},
		"no jwks url":         {map[string]any{"issuer": "https://account.example"}, prusaaccount.ErrMissingJWKSURL},
		"issuer is not a URL": {map[string]any{"issuer": "account.example", "jwks_url": "https://a.example/j"}, prusaaccount.ErrMissingIssuer},
		"skew is not a duration": {
			map[string]any{"issuer": "https://a.example", "jwks_url": "https://a.example/j", "clock_skew": "soon"},
			prusaaccount.ErrBadDuration,
		},
		"timeout is negative": {
			map[string]any{"issuer": "https://a.example", "jwks_url": "https://a.example/j", "timeout": "-1s"},
			prusaaccount.ErrBadDuration,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := prusaaccount.New(authapi.ProviderSpec{Type: prusaaccount.Type, Config: tc.config})
			require.ErrorIs(t, err, tc.want)
		})
	}
}

// A good token yields the subject and the claims the pairing and a listing
// need, and its expiry, so the host can bound the credential it admits.
func TestAGoodTokenYieldsTheSubjectAndItsExpiry(t *testing.T) {
	t.Parallel()

	iss := newIssuer(t)
	claims := iss.Claims()

	identity, err := provider(t, iss, nil).VerifyToken(t.Context(), iss.Sign(claims))
	require.NoError(t, err)

	assert.Equal(t, iss.Subject, identity.Subject)
	assert.Equal(t, iss.Email, identity.Email)
	assert.Equal(t, iss.Name, identity.DisplayName)
	assert.Equal(t, claims["exp"], identity.ExpiresAt)
	assert.Empty(t, identity.Roles, "the token carries no local roles; grants are the pairing's")
}

// Every refusal a token can earn, each with the error the host maps to a
// fault code, so an operator debugging a client reads which one it was.
func TestEachBadTokenIsRefusedWithItsOwnError(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		token func(iss *issuer) string
		want  error
	}{
		"expired": {func(iss *issuer) string {
			claims := iss.Claims()
			claims["exp"] = 1

			return iss.Sign(claims)
		}, authapi.ErrTokenExpired},
		"not yet valid": {func(iss *issuer) string {
			claims := iss.Claims()
			claims["nbf"] = claims["exp"]

			return iss.Sign(claims)
		}, authapi.ErrTokenExpired},
		"no expiry at all": {func(iss *issuer) string {
			claims := iss.Claims()
			delete(claims, "exp")

			return iss.Sign(claims)
		}, authapi.ErrTokenExpired},
		"wrong audience": {func(iss *issuer) string {
			claims := iss.Claims()
			claims["aud"] = []string{"somebody-else"}

			return iss.Sign(claims)
		}, authapi.ErrTokenAudience},
		"wrong issuer": {func(iss *issuer) string {
			claims := iss.Claims()
			claims["iss"] = "https://issuer.elsewhere"

			return iss.Sign(claims)
		}, authapi.ErrTokenIssuer},
		"no subject": {func(iss *issuer) string {
			claims := iss.Claims()
			delete(claims, "sub")

			return iss.Sign(claims)
		}, authapi.ErrTokenMalformed},
		"unknown key id": {func(iss *issuer) string {
			return iss.SignWith(
				map[string]any{"alg": "RS256", "typ": "JWT", "kid": "a-kid-nobody-publishes"},
				iss.Claims(),
			)
		}, authapi.ErrTokenSignature},
		"alg none": {func(iss *issuer) string {
			return segment(map[string]any{"alg": "none", "typ": "JWT"}) + "." + segment(iss.Claims()) + "."
		}, authapi.ErrTokenSignature},
		"alg none with a signature attached": {func(iss *issuer) string {
			return iss.SignWith(map[string]any{"alg": "none", "typ": "JWT", "kid": keyID}, iss.Claims())
		}, authapi.ErrTokenSignature},
		"HS256": {func(iss *issuer) string {
			return iss.SignWith(map[string]any{"alg": "HS256", "typ": "JWT", "kid": keyID}, iss.Claims())
		}, authapi.ErrTokenSignature},
		"RS512": {func(iss *issuer) string {
			return iss.SignWith(map[string]any{"alg": "RS512", "typ": "JWT", "kid": keyID}, iss.Claims())
		}, authapi.ErrTokenSignature},
		"signed by another key": {func(iss *issuer) string {
			other := newIssuer(t)

			return other.SignWith(map[string]any{"alg": "RS256", "typ": "JWT", "kid": keyID}, iss.Claims())
		}, authapi.ErrTokenSignature},
		"tampered payload": {func(iss *issuer) string {
			parts := strings.Split(iss.Sign(iss.Claims()), ".")
			claims := iss.Claims()
			claims["sub"] = "somebody-else"
			parts[1] = segment(claims)

			return strings.Join(parts, ".")
		}, authapi.ErrTokenSignature},
		"tampered signature": {func(iss *issuer) string {
			// The first character of the signature holds six bits of its
			// first byte and no padding bits. Changing that character always
			// changes the decoded signature.
			parts := strings.Split(iss.Sign(iss.Claims()), ".")
			first := "A"
			if parts[2][0] == 'A' {
				first = "B"
			}
			parts[2] = first + parts[2][1:]

			return strings.Join(parts, ".")
		}, authapi.ErrTokenSignature},
		"not a JWS": {func(*issuer) string { return "pcl_not_a_jwt" }, authapi.ErrTokenMalformed},
		"header is not JSON": {func(iss *issuer) string {
			return "bm90LWpzb24." + segment(iss.Claims()) + ".c2ln"
		}, authapi.ErrTokenMalformed},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			iss := newIssuer(t)

			_, err := provider(t, iss, nil).VerifyToken(t.Context(), tc.token(iss))
			require.ErrorIs(t, err, tc.want)
		})
	}
}

// A token Prusa Account mints for another service carries that service's
// audience. An operator who leaves the key empty accepts it; one who sets it
// gets the check.
func TestAnEmptyAudienceSkipsTheCheck(t *testing.T) {
	t.Parallel()

	iss := newIssuer(t)
	claims := iss.Claims()
	claims["aud"] = "another-service"

	_, err := provider(t, iss, map[string]any{"audience": ""}).VerifyToken(t.Context(), iss.Sign(claims))
	require.NoError(t, err)

	_, err = provider(t, iss, nil).VerifyToken(t.Context(), iss.Sign(claims))
	require.ErrorIs(t, err, authapi.ErrTokenAudience)
}

// The key set is fetched once and every token after that is verified against
// the cached copy. A verifier that fetched per request would put the issuer in
// the path of every call.
func TestTheKeySetIsFetchedOnceForManyTokens(t *testing.T) {
	t.Parallel()

	iss := newIssuer(t)
	p := provider(t, iss, nil)

	const tokens = 25

	for range tokens {
		_, err := p.VerifyToken(t.Context(), iss.Sign(iss.Claims()))
		require.NoError(t, err)
	}

	assert.EqualValues(t, 1, iss.jwksHits.Load(), "%d tokens cost the issuer %d fetches", tokens, iss.jwksHits.Load())
}

// A token naming a key the issuer does not publish costs the issuer one
// refetch, not one per token. The key name comes out of the token, so it is
// input somebody else chose.
func TestAnUnknownKeyIsRefetchedOnceAndThenThrottled(t *testing.T) {
	t.Parallel()

	iss := newIssuer(t)
	p := provider(t, iss, nil)

	_, err := p.VerifyToken(t.Context(), iss.Sign(iss.Claims()))
	require.NoError(t, err)
	require.EqualValues(t, 1, iss.jwksHits.Load())

	const attempts = 10

	for range attempts {
		_, err = p.VerifyToken(t.Context(),
			iss.SignWith(map[string]any{"alg": "RS256", "typ": "JWT", "kid": "a-kid-nobody-publishes"}, iss.Claims()))
		require.ErrorIs(t, err, authapi.ErrTokenSignature)
	}

	assert.EqualValues(t, 1, iss.jwksHits.Load(),
		"the first fetch was a moment ago, so %d unknown keys cost no second fetch", attempts)
}

// An issuer that cannot be reached is its own refusal, so an operator reads an
// outage and not a forged token, and a caller knows a retry can pass.
func TestAnUnreachableKeySetIsItsOwnRefusal(t *testing.T) {
	t.Parallel()

	iss := newIssuer(t)
	iss.setDown(true)

	_, err := provider(t, iss, nil).VerifyToken(t.Context(), iss.Sign(iss.Claims()))
	require.ErrorIs(t, err, authapi.ErrTokenKeys)
}

// Text that is not a token never reaches the issuer.
func TestRubbishCostsTheIssuerNothing(t *testing.T) {
	t.Parallel()

	iss := newIssuer(t)
	p := provider(t, iss, nil)

	for _, token := range []string{"", "x", "a.b", "a.b.c.d", "!!!.!!!.!!!"} {
		_, err := p.VerifyToken(t.Context(), token)
		require.ErrorIs(t, err, authapi.ErrTokenMalformed)
	}

	assert.Zero(t, iss.jwksHits.Load())
}

// An issuer that is down is not a reason to refuse to start. It is a reason
// for a request to fail, which is what the operator sees and can act on.
func TestADeadIssuerIsNotABootDependency(t *testing.T) {
	t.Parallel()

	iss := newIssuer(t)
	iss.setDown(true)

	_, err := prusaaccount.New(authapi.ProviderSpec{
		Type:   prusaaccount.Type,
		Config: map[string]any{"issuer": iss.URL, "jwks_url": iss.JWKSURL()},
	})
	require.NoError(t, err)
	assert.Zero(t, iss.jwksHits.Load(), "building the provider reached the issuer")
}
