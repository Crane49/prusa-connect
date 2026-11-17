package oidc

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// Each case is a way an issuer can weaken a login. Each one is caught at
// construction and not at the first login.
func TestDiscoverRefusesADocumentThisClientCannotSafelyUse(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		document func(base string) map[string]any
		want     error
	}{
		{
			name: "issuer does not match",
			document: func(base string) map[string]any {
				return document(base, map[string]any{"issuer": "https://accounts.example.com"})
			},
			want: ErrIssuerMismatch,
		},
		{
			name: "no token endpoint",
			document: func(base string) map[string]any {
				return document(base, map[string]any{"token_endpoint": ""})
			},
			want: ErrIncompleteMetadata,
		},
		{
			name: "no jwks",
			document: func(base string) map[string]any {
				return document(base, map[string]any{"jwks_uri": ""})
			},
			want: ErrIncompleteMetadata,
		},
		{
			name: "plain only",
			document: func(base string) map[string]any {
				return document(base, map[string]any{"code_challenge_methods_supported": []string{"plain"}})
			},
			want: ErrNoS256,
		},
		{
			name: "no pkce at all",
			document: func(base string) map[string]any {
				return document(base, map[string]any{"code_challenge_methods_supported": []string{}})
			},
			want: ErrNoS256,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := serveDocument(t, tc.document)

			_, err := Discover(t.Context(), server.Client(), server.URL)
			require.ErrorIs(t, err, tc.want)

			_, err = New(t.Context(), Config{
				Issuer:      server.URL,
				ClientID:    "prusa-connect",
				RedirectURI: "http://localhost:8080/callback",
				HTTPClient:  server.Client(),
			})
			require.ErrorIs(t, err, tc.want, "New must refuse at construction, not at the first login")
		})
	}
}

// Providers disagree about a trailing slash, so it is not treated as a
// difference.
func TestDiscoverAcceptsATrailingSlashDifference(t *testing.T) {
	t.Parallel()

	server := serveDocument(t, func(base string) map[string]any {
		return document(base, map[string]any{"issuer": base + "/"})
	})

	meta, err := Discover(t.Context(), server.Client(), server.URL)
	require.NoError(t, err)
	require.Equal(t, server.URL+"/", meta.Issuer)
}

func TestNewRefusesAnIncompleteConfig(t *testing.T) {
	t.Parallel()

	cases := []Config{
		{ClientID: "id", RedirectURI: "http://localhost/cb"},
		{Issuer: "http://localhost", RedirectURI: "http://localhost/cb"},
		{Issuer: "http://localhost", ClientID: "id"},
	}

	for _, config := range cases {
		_, err := New(t.Context(), config)
		require.ErrorIs(t, err, ErrIncompleteConfig)
	}
}

// The challenge is derived as RFC 7636 section 4.2 defines it, and the length
// bounds are section 4.1.
func TestNewPKCEDerivesTheChallengeFromTheVerifier(t *testing.T) {
	t.Parallel()

	first, err := NewPKCE()
	require.NoError(t, err)

	second, err := NewPKCE()
	require.NoError(t, err)

	require.NotEqual(t, first.Verifier, second.Verifier)
	require.GreaterOrEqual(t, len(first.Verifier), 43)
	require.LessOrEqual(t, len(first.Verifier), 128)
	require.NotEqual(t, first.Verifier, first.Challenge, "S256 is a hash, not a copy")

	// The same verifier hashed again gives the same challenge, which is what the
	// issuer recomputes.
	require.Equal(t, first.Challenge, challengeOf(first.Verifier))
	require.NotEqual(t, first.Challenge, challengeOf(second.Verifier))
}

// The front channel reaches browser history and server logs, so it must not
// carry the verifier.
func TestAuthorizationURLCarriesTheChallengeAndNotTheVerifier(t *testing.T) {
	t.Parallel()

	server := serveDocument(t, func(base string) map[string]any { return document(base, nil) })

	client, err := New(t.Context(), Config{
		Issuer:      server.URL,
		ClientID:    "prusa-connect",
		RedirectURI: "http://localhost:8080/callback",
		HTTPClient:  server.Client(),
	})
	require.NoError(t, err)

	pkce, err := NewPKCE()
	require.NoError(t, err)

	target := client.AuthorizationURL("the-state", "the-nonce", pkce)

	require.Contains(t, target, "code_challenge="+pkce.Challenge)
	require.Contains(t, target, "code_challenge_method=S256")
	require.NotContains(t, target, pkce.Verifier)
	require.NotContains(t, target, "code_verifier")
}

// challengeOf recomputes the challenge the way an issuer does, from the
// verifier alone. Deriving it here keeps the assertion above from restating
// NewPKCE.
func challengeOf(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))

	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func document(base string, override map[string]any) map[string]any {
	doc := map[string]any{
		"issuer":                           base,
		"authorization_endpoint":           base + "/authorize",
		"token_endpoint":                   base + "/oauth/token",
		"jwks_uri":                         base + "/.well-known/jwks.json",
		"code_challenge_methods_supported": []string{ChallengeMethodS256},
	}

	maps.Copy(doc, override)

	return doc
}

func serveDocument(t *testing.T, build func(base string) map[string]any) *httptest.Server {
	t.Helper()

	var server *httptest.Server

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A handler runs on another goroutine, where a failed require would
		// abort the wrong one. It reports, and the assertion in the test body
		// is the one that fails.
		if r.URL.Path != discoveryPath {
			t.Errorf("a client asked for %q and not the well-known path", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(build(server.URL)); err != nil {
			t.Errorf("writing the discovery document: %v", err)
		}
	}))

	t.Cleanup(server.Close)

	return server
}
