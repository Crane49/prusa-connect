package oidc

import (
	"crypto"
	"crypto/hmac"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// hs256Issuer advertises the one algorithm an authentik provider with no
// signing key uses: HS256, keyed with the client secret.
func hs256Issuer(t *testing.T, advertised []string) *httptest.Server {
	t.Helper()

	var server *httptest.Server

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != discoveryPath {
			http.NotFound(w, r)

			return
		}

		override := map[string]any{}
		if advertised != nil {
			override["id_token_signing_alg_values_supported"] = advertised
		}

		_ = json.NewEncoder(w).Encode(document(server.URL, override))
	}))
	t.Cleanup(server.Close)

	return server
}

func hs256Token(t *testing.T, secret string, claims map[string]any) string {
	t.Helper()

	b64 := base64.RawURLEncoding.EncodeToString
	payload, err := json.Marshal(claims)
	require.NoError(t, err)

	signed := b64([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." + b64(payload)

	mac := hmac.New(crypto.SHA256.New, []byte(secret))
	mac.Write([]byte(signed))

	return signed + "." + b64(mac.Sum(nil))
}

// An issuer that signs with the client secret signs in a confidential client.
// A public client has no secret, so the same token is refused with a reason
// that names the secret.
func TestAnHS256IDTokenIsCheckedWithTheClientSecret(t *testing.T) {
	t.Parallel()

	issuer := hs256Issuer(t, []string{"HS256"})
	now := time.Now()
	claims := map[string]any{
		"iss": issuer.URL, "sub": "ada", "aud": "prusa-connect", "nonce": "n",
		"exp": now.Add(time.Minute).Unix(), "name": "Ada Admin",
	}

	confidential, err := New(t.Context(), Config{
		Issuer: issuer.URL, ClientID: "prusa-connect", ClientSecret: "the-secret",
		RedirectURI: "http://127.0.0.1/cb",
	})
	require.NoError(t, err)
	require.Equal(t, []string{"HS256"}, confidential.Algorithms())

	verified, err := confidential.VerifyIDToken(t.Context(), hs256Token(t, "the-secret", claims), "n")
	require.NoError(t, err)
	require.Equal(t, "ada", verified.Subject)
	require.Equal(t, "HS256", verified.Algorithm)

	_, err = confidential.VerifyIDToken(t.Context(), hs256Token(t, "another-secret", claims), "n")
	require.ErrorIs(t, err, ErrBadSignature)

	public, err := New(t.Context(), Config{
		Issuer: issuer.URL, ClientID: "prusa-connect", RedirectURI: "http://127.0.0.1/cb",
	})
	require.NoError(t, err)
	require.Empty(t, public.Algorithms())

	_, err = public.VerifyIDToken(t.Context(), hs256Token(t, "the-secret", claims), "n")
	require.ErrorIs(t, err, ErrUnsupportedAlgorithm)
	require.ErrorContains(t, err, "no client secret is configured")
}

// An algorithm the issuer did not advertise is refused before any key is
// fetched, even one this server can check, so a token cannot pick a weaker
// algorithm than the issuer's own.
func TestAnAlgorithmTheIssuerDidNotAdvertiseIsRefused(t *testing.T) {
	t.Parallel()

	issuer := hs256Issuer(t, []string{"RS256"})

	client, err := New(t.Context(), Config{
		Issuer: issuer.URL, ClientID: "prusa-connect", ClientSecret: "the-secret",
		RedirectURI: "http://127.0.0.1/cb",
	})
	require.NoError(t, err)

	_, err = client.VerifyIDToken(t.Context(), hs256Token(t, "the-secret", map[string]any{}), "n")
	require.ErrorIs(t, err, ErrUnsupportedAlgorithm)
	require.ErrorContains(t, err, "only RS256 is accepted")
}

// An issuer that advertises nothing gets RS256, the default of OpenID Connect
// Discovery section 3.
func TestAnIssuerThatAdvertisesNothingGetsRS256(t *testing.T) {
	t.Parallel()

	issuer := hs256Issuer(t, nil)

	client, err := New(t.Context(), Config{
		Issuer: issuer.URL, ClientID: "prusa-connect", RedirectURI: "http://127.0.0.1/cb",
	})
	require.NoError(t, err)
	require.Equal(t, []string{"RS256"}, client.Algorithms())
}
