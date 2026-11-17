package oidc

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// RFC 7519 section 4.1.3 allows either shape, and issuers send both.
func TestAnAudienceIsAStringOrAnArray(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw  string
		want []string
	}{
		{raw: `"prusa-connect"`, want: []string{"prusa-connect"}},
		{raw: `["prusa-connect"]`, want: []string{"prusa-connect"}},
		{raw: `["other","prusa-connect"]`, want: []string{"other", "prusa-connect"}},
	}

	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()

			var aud audience
			require.NoError(t, json.Unmarshal([]byte(tc.raw), &aud))
			require.Equal(t, tc.want, []string(aud))
		})
	}

	var aud audience
	require.Error(t, json.Unmarshal([]byte(`42`), &aud))
}

// Each case is a token the issuer signed correctly that still does not belong
// to this login.
func TestClaimsCheckRefusesEveryUnboundToken(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)

	good := Claims{
		Subject:   "operator",
		Issuer:    "http://localhost:58092/",
		Audience:  audience{"prusa-connect"},
		Nonce:     "the-nonce",
		ExpiresAt: now.Add(time.Minute).Unix(),
		NotBefore: now.Add(-time.Minute).Unix(),
	}

	require.NoError(t, good.check("http://localhost:58092", "prusa-connect", "the-nonce", now))

	cases := []struct {
		name    string
		mutate  func(*Claims)
		checkAt time.Time
	}{
		{name: "no subject", mutate: func(c *Claims) { c.Subject = "" }},
		{name: "another issuer", mutate: func(c *Claims) { c.Issuer = "https://accounts.example.com" }},
		{name: "another audience", mutate: func(c *Claims) { c.Audience = audience{"someone-else"} }},
		{name: "no audience", mutate: func(c *Claims) { c.Audience = nil }},
		{name: "another login's nonce", mutate: func(c *Claims) { c.Nonce = "a different nonce" }},
		{name: "no expiry", mutate: func(c *Claims) { c.ExpiresAt = 0 }},
		{name: "expired", mutate: func(*Claims) {}, checkAt: now.Add(time.Hour)},
		{name: "not yet valid", mutate: func(*Claims) {}, checkAt: now.Add(-time.Hour)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			claims := good
			tc.mutate(&claims)

			at := tc.checkAt
			if at.IsZero() {
				at = now
			}

			require.ErrorIs(t,
				claims.check("http://localhost:58092", "prusa-connect", "the-nonce", at),
				ErrClaimRejected)
		})
	}
}
