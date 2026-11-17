package oidc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// userinfoIssuer publishes a userinfo endpoint that answers one access token
// with what Authelia's default claims policy keeps out of the ID token.
func userinfoIssuer(t *testing.T, subject string) *httptest.Server {
	t.Helper()

	var server *httptest.Server

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case discoveryPath:
			_ = json.NewEncoder(w).Encode(document(server.URL, map[string]any{
				"userinfo_endpoint": server.URL + "/userinfo",
			}))
		case "/userinfo":
			if r.Header.Get("Authorization") != "Bearer the-access-token" {
				w.WriteHeader(http.StatusUnauthorized)

				return
			}

			_ = json.NewEncoder(w).Encode(map[string]string{
				"sub": subject, "name": "Ada Admin", "email": "ada@example.com", "preferred_username": "ada",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func userinfoClient(t *testing.T, issuer *httptest.Server) *Client {
	t.Helper()

	client, err := New(t.Context(), Config{
		Issuer: issuer.URL, ClientID: "prusa-connect", RedirectURI: "http://127.0.0.1/cb",
	})
	require.NoError(t, err)

	return client
}

// An ID token with no name or address is completed from userinfo, which is
// where Authelia puts them.
func TestTheProfileIsCompletedFromUserinfo(t *testing.T) {
	t.Parallel()

	client := userinfoClient(t, userinfoIssuer(t, "ada-sub"))

	profile, err := client.Profile(t.Context(), Claims{Subject: "ada-sub"}, "the-access-token")
	require.NoError(t, err)
	require.Equal(t, Profile{Name: "Ada Admin", PreferredUsername: "ada", Email: "ada@example.com"}, profile)
	require.Equal(t, "Ada Admin", profile.DisplayName())
}

// What the ID token says wins, and an ID token that says it all costs the
// issuer no request.
func TestTheIDTokenIsReadFirst(t *testing.T) {
	t.Parallel()

	client := userinfoClient(t, userinfoIssuer(t, "ada-sub"))

	profile, err := client.Profile(t.Context(),
		Claims{Subject: "ada-sub", Name: "Ada L.", Email: "ada@corp.example"}, "a-token-userinfo-refuses")
	require.NoError(t, err)
	require.Equal(t, "Ada L.", profile.Name)
	require.Equal(t, "ada@corp.example", profile.Email)
}

// A userinfo answer about another subject is ignored, and the error says so.
func TestUserinfoForAnotherSubjectIsIgnored(t *testing.T) {
	t.Parallel()

	client := userinfoClient(t, userinfoIssuer(t, "someone-else"))

	profile, err := client.Profile(
		t.Context(),
		Claims{Subject: "ada-sub", PreferredUsername: "ada"},
		"the-access-token",
	)
	require.ErrorIs(t, err, ErrUserInfo)
	require.Equal(t, Profile{PreferredUsername: "ada"}, profile)
	require.Equal(t, "ada", profile.DisplayName())
}

func TestDisplayNameTakesTheFirstLabelTheIssuerSent(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		profile Profile
		want    string
	}{
		"the name first":                       {Profile{Name: "Ada Admin", PreferredUsername: "ada", Username: "a", Email: "ada@example.com"}, "Ada Admin"},
		"the preferred username over username": {Profile{PreferredUsername: "ada", Username: "a", Email: "ada@example.com"}, "ada"},
		"the username over the address":        {Profile{Username: "ada", Email: "ada@example.com"}, "ada"},
		"the address last":                     {Profile{Email: "ada@example.com"}, "ada@example.com"},
		"nothing when nothing was sent":        {Profile{}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, test.profile.DisplayName())
		})
	}
}

// Synology SSO sends `username` and no `preferred_username`.
func TestTheUsernameClaimIsRead(t *testing.T) {
	t.Parallel()

	var claims Claims
	require.NoError(t, json.Unmarshal([]byte(`{"sub":"s","username":"ada","email":"ada@example.com"}`), &claims))
	require.Equal(t, "ada", ProfileOf(claims).DisplayName())
}
