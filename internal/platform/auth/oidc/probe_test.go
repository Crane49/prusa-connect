package oidc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	registeredClient   = "prusa-connect"
	registeredSecret   = "GOCSPX-right"
	registeredRedirect = "http://localhost:8080/api/v1/auth/callback"
)

// strictIssuer behaves the way a provider that registered one confidential
// client does: the token endpoint authenticates the client before it reads
// the code, and the authorization endpoint refuses an address it does not
// know with a page of its own.
func strictIssuer(t *testing.T, tokenStatus int) *httptest.Server {
	t.Helper()

	var server *httptest.Server

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case discoveryPath:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(document(server.URL, nil))
		case "/oauth/token":
			w.Header().Set("Content-Type", "application/json")

			if tokenStatus != 0 {
				w.WriteHeader(tokenStatus)

				return
			}

			user, password, ok := r.BasicAuth()

			switch {
			case !ok:
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_request","error_description":"client_secret is missing."}`))
			case user != registeredClient || password != registeredSecret:
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"Invalid client credentials"}`))
			default:
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Code not valid"}`))
			}
		case "/authorize":
			query := r.URL.Query()
			if query.Get("client_id") == "unknown-client" {
				// Google refuses an unknown client by sending the browser to
				// its own error page.
				http.Redirect(w, r, server.URL+"/signin/oauth/error?authError=Cg9pbnZhbGlk", http.StatusFound)

				return
			}

			if query.Get("client_id") != registeredClient || query.Get("redirect_uri") != registeredRedirect {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("<html><head><title>Invalid parameter: redirect_uri</title></head></html>"))

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

func probeClient(t *testing.T, server *httptest.Server, secret, redirect string) *Client {
	t.Helper()

	client, err := New(t.Context(), Config{
		Issuer:       server.URL,
		ClientID:     registeredClient,
		ClientSecret: secret,
		RedirectURI:  redirect,
		HTTPClient:   server.Client(),
	})
	require.NoError(t, err)

	return client
}

// The token endpoint authenticates the client before it reads the code, so a
// code nobody issued tells a taken secret from a refused one.
func TestProbeClientTellsATakenSecretFromARefusedOne(t *testing.T) {
	t.Parallel()

	server := strictIssuer(t, 0)

	cases := map[string]struct {
		secret  string
		verdict Verdict
		detail  string
	}{
		"the registered secret": {registeredSecret, VerdictPassed, "invalid_grant"},
		"a wrong secret":        {"GOCSPX-wrong", VerdictFailed, "invalid_client"},
		"no secret at all":      {"", VerdictFailed, "client_secret is missing"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			found := probeClient(t, server, tc.secret, registeredRedirect).ProbeClient(t.Context())
			require.Equal(t, tc.verdict, found.Verdict, found.Detail)
			require.Contains(t, found.Detail, tc.detail)
		})
	}
}

// An answer that is neither a refused code nor a refused client is reported
// as such, not passed and not failed.
func TestProbeClientCannotReadAnIssuerThatFails(t *testing.T) {
	t.Parallel()

	found := probeClient(t, strictIssuer(t, http.StatusInternalServerError), registeredSecret, registeredRedirect).
		ProbeClient(t.Context())
	require.Equal(t, VerdictUnknown, found.Verdict)
	require.Contains(t, found.Detail, "status 500")
}

// An issuer sends a browser on to sign in only for an address it registered,
// and refuses any other with a page that names the problem.
func TestProbeRedirectTellsARegisteredAddressFromAnother(t *testing.T) {
	t.Parallel()

	server := strictIssuer(t, 0)

	found := probeClient(t, server, registeredSecret, registeredRedirect).ProbeRedirect(t.Context())
	require.Equal(t, VerdictPassed, found.Verdict, found.Detail)

	found = probeClient(t, server, registeredSecret, "http://elsewhere:8080/api/v1/auth/callback").
		ProbeRedirect(t.Context())
	require.Equal(t, VerdictFailed, found.Verdict)
	require.Equal(t, "status 400: Invalid parameter: redirect_uri", found.Detail)
}

// An issuer that refuses by sending the browser to its own error page fails
// the probe, though it answered with a redirect.
func TestProbeRedirectReadsARedirectToAnErrorPage(t *testing.T) {
	t.Parallel()

	server := strictIssuer(t, 0)

	client, err := New(t.Context(), Config{
		Issuer:      server.URL,
		ClientID:    "unknown-client",
		RedirectURI: registeredRedirect,
		HTTPClient:  server.Client(),
	})
	require.NoError(t, err)

	found := client.ProbeRedirect(t.Context())
	require.Equal(t, VerdictFailed, found.Verdict)
	require.Contains(t, found.Detail, "/signin/oauth/error")
}
