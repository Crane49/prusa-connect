package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// providerForProbe is an issuer with one confidential client registered for
// one redirect address.
func providerForProbe(t *testing.T) *httptest.Server {
	t.Helper()

	var server *httptest.Server

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                           server.URL,
				"authorization_endpoint":           server.URL + "/authorize",
				"token_endpoint":                   server.URL + "/token",
				"jwks_uri":                         server.URL + "/jwks",
				"code_challenge_methods_supported": []string{"S256"},
			})
		case "/token":
			w.Header().Set("Content-Type", "application/json")

			if user, password, ok := r.BasicAuth(); !ok || user != "prusa-connect" || password != "right" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"invalid_client"}`))

				return
			}

			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		case "/authorize":
			if r.URL.Query().Get("redirect_uri") != "http://localhost:8080/api/v1/auth/callback" {
				w.WriteHeader(http.StatusBadRequest)

				return
			}

			http.Redirect(w, r, server.URL+"/login", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))

	t.Cleanup(server.Close)

	return server
}

// TestTryingAProviderReportsEveryStep: the wizard's test reads the provider
// the way a sign-in would, and names the step that would fail.
func TestTryingAProviderReportsEveryStep(t *testing.T) {
	t.Parallel()

	server := providerForProbe(t)
	probe := func(secret, redirect string) httpapi.SetupOIDCProbe {
		return httpapi.SetupOIDCProbe{
			Issuer: server.URL, ClientID: "prusa-connect", ClientSecret: secret, RedirectURI: redirect,
		}
	}

	good := checkOIDC(t.Context(), probe("right", "http://localhost:8080/api/v1/auth/callback"))
	require.Equal(t, server.URL, good.Issuer)
	require.Equal(t, "passed", good.Discovery.Status)
	require.Equal(t, "passed", good.Client.Status, good.Client.Detail)
	require.Equal(t, "passed", good.Redirect.Status, good.Redirect.Detail)

	wrong := checkOIDC(t.Context(), probe("wrong", "http://elsewhere/api/v1/auth/callback"))
	require.Equal(t, "passed", wrong.Discovery.Status)
	require.Equal(t, "failed", wrong.Client.Status)
	require.Contains(t, wrong.Client.Detail, "invalid_client")
	require.Equal(t, "failed", wrong.Redirect.Status)
}

// TestAProviderThatCannotBeReadSkipsTheRest: with no discovery document there
// is no token endpoint to ask, so the later steps say why they did not run.
func TestAProviderThatCannotBeReadSkipsTheRest(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)

	check := checkOIDC(t.Context(), httpapi.SetupOIDCProbe{
		Issuer: server.URL, ClientID: "prusa-connect", RedirectURI: "http://localhost:8080/api/v1/auth/callback",
	})
	require.Equal(t, "failed", check.Discovery.Status)
	require.Contains(t, check.Discovery.Detail, "status 404")
	require.Equal(t, "skipped", check.Client.Status)
	require.Equal(t, "skipped", check.Redirect.Status)
}

// TestTryingAProviderTakesTheSetupCode: the check makes this server send
// requests to an address the caller names, so it is behind the code like
// every other first-run call.
func TestTryingAProviderTakesTheSetupCode(t *testing.T) {
	t.Parallel()

	install, _ := wizardInstall(t, t.TempDir())
	server := providerForProbe(t)

	probe := httpapi.SetupOIDCProbe{
		Issuer: server.URL, ClientID: "prusa-connect", ClientSecret: "right",
		RedirectURI: "http://localhost:8080/api/v1/auth/callback",
	}

	_, err := install.first.CheckOIDC(t.Context(), httpapi.SetupClient{}, "WRONG-CODE-0000", probe)
	require.Error(t, err)

	check, err := install.first.CheckOIDC(t.Context(), httpapi.SetupClient{}, testSetupCode, probe)
	require.NoError(t, err)
	require.Equal(t, "passed", check.Client.Status, check.Client.Detail)
}

// TestAProviderThatSignsWithTheSecretNeedsTheSecret: an authentik provider
// with no signing key signs ID tokens with the client secret. A wizard with no
// secret would set up a sign-in that refuses everybody after the provider, so
// the test fails at discovery and says what to set.
func TestAProviderThatSignsWithTheSecretNeedsTheSecret(t *testing.T) {
	t.Parallel()

	var server *httptest.Server

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                server.URL,
			"authorization_endpoint":                server.URL + "/authorize",
			"token_endpoint":                        server.URL + "/token",
			"jwks_uri":                              server.URL + "/jwks",
			"code_challenge_methods_supported":      []string{"S256"},
			"id_token_signing_alg_values_supported": []string{"HS256"},
		})
	}))
	t.Cleanup(server.Close)

	check := checkOIDC(t.Context(), httpapi.SetupOIDCProbe{
		Issuer: server.URL, ClientID: "prusa-connect", RedirectURI: "http://localhost:8080/api/v1/auth/callback",
	})
	require.Equal(t, "failed", check.Discovery.Status)
	require.Contains(t, check.Discovery.Detail, "no client secret is set")
	require.Equal(t, "skipped", check.Client.Status)
}
