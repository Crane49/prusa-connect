package oidc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// tokenRequest is what an issuer's token endpoint received.
type tokenRequest struct {
	authorization string
	user          string
	password      string
	basic         bool
	form          url.Values
}

// serveTokenEndpoint is an issuer that publishes its discovery document and
// records the one code exchange made against it.
func serveTokenEndpoint(t *testing.T) (*httptest.Server, func() tokenRequest) {
	t.Helper()

	var (
		mu       sync.Mutex
		received tokenRequest
		server   *httptest.Server
	)

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case discoveryPath:
			if err := json.NewEncoder(w).Encode(document(server.URL, nil)); err != nil {
				t.Errorf("writing the discovery document: %v", err)
			}
		case "/oauth/token":
			if err := r.ParseForm(); err != nil {
				t.Errorf("reading the token request: %v", err)
			}

			user, password, basic := r.BasicAuth()

			mu.Lock()
			received = tokenRequest{
				authorization: r.Header.Get("Authorization"),
				user:          user,
				password:      password,
				basic:         basic,
				form:          r.PostForm,
			}
			mu.Unlock()

			_, _ = w.Write([]byte(`{"id_token":"header.payload.signature","token_type":"Bearer"}`))
		default:
			t.Errorf("a client asked for %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))

	t.Cleanup(server.Close)

	return server, func() tokenRequest {
		mu.Lock()
		defer mu.Unlock()

		return received
	}
}

// A confidential client proves itself at the code exchange with its secret,
// form-encoded into HTTP Basic as RFC 6749 section 2.3.1 asks, and never puts
// the secret in the form body.
func TestExchangeAuthenticatesAConfidentialClientWithItsSecret(t *testing.T) {
	t.Parallel()

	server, received := serveTokenEndpoint(t)

	client, err := New(t.Context(), Config{
		Issuer:       server.URL,
		ClientID:     "prusa-connect.apps.example",
		ClientSecret: "GOCSPX-a+b/c d",
		RedirectURI:  "http://localhost:8080/api/v1/auth/callback",
		HTTPClient:   server.Client(),
	})
	require.NoError(t, err)

	pkce, err := NewPKCE()
	require.NoError(t, err)

	_, err = client.Exchange(t.Context(), "code-1", pkce)
	require.NoError(t, err)

	got := received()
	require.True(t, got.basic, "the token request carried no client authentication")
	require.Equal(t, url.QueryEscape("prusa-connect.apps.example"), got.user)
	require.Equal(t, url.QueryEscape("GOCSPX-a+b/c d"), got.password)
	require.Empty(t, got.form.Get("client_secret"), "the secret was sent in the form body")
	require.Equal(t, pkce.Verifier, got.form.Get("code_verifier"), "a secret replaced PKCE instead of adding to it")
}

// A public client has no secret, so the exchange carries no Authorization
// header and PKCE alone binds the code to this login.
func TestExchangeSendsNoSecretForAPublicClient(t *testing.T) {
	t.Parallel()

	server, received := serveTokenEndpoint(t)

	client, err := New(t.Context(), Config{
		Issuer:      server.URL,
		ClientID:    "prusa-connect",
		RedirectURI: "http://localhost:8080/api/v1/auth/callback",
		HTTPClient:  server.Client(),
	})
	require.NoError(t, err)

	pkce, err := NewPKCE()
	require.NoError(t, err)

	_, err = client.Exchange(t.Context(), "code-1", pkce)
	require.NoError(t, err)

	got := received()
	require.Empty(t, got.authorization)
	require.Equal(t, "prusa-connect", got.form.Get("client_id"))
	require.Equal(t, pkce.Verifier, got.form.Get("code_verifier"))
}
