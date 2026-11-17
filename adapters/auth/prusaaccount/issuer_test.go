package prusaaccount_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/authapi/authapitest"
)

// keyID is what the fake issuer publishes its signing key under, so a token
// naming a different one exercises the lookup rather than the single-key
// shortcut.
const keyID = "prusa-account-signing-key"

// audience is what the fake issuer mints tokens for.
const audience = "prusa-connect"

// issuer is Prusa Account as this adapter sees it: a key set endpoint and a
// signer. There is no login flow to fake, because the adapter has none.
type issuer struct {
	*httptest.Server

	key *rsa.PrivateKey

	// jwksHits counts what the key set costs the issuer, which is what the
	// cache and the refetch throttle are measured by.
	jwksHits atomic.Int64

	mu sync.Mutex
	// down makes the key set endpoint fail, which is the issuer's outage.
	down bool

	Subject string
	Email   string
	Name    string
}

func newIssuer(t *testing.T) *issuer {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	iss := &issuer{
		key:     key,
		Subject: "8f1e1b2c-0000-4000-8000-abcdefabcdef",
		Email:   "operator@example.test",
		Name:    "Fleet Operator",
	}

	iss.Server = httptest.NewServer(http.HandlerFunc(iss.route))
	t.Cleanup(iss.Close)

	return iss
}

// JWKSURL is where the issuer publishes its keys.
func (i *issuer) JWKSURL() string { return i.URL + "/jwks" }

// route dispatches by path rather than through a ServeMux, which
// TestNothingOutsideTheSealedPackageMountsAServeMux refuses anywhere in this
// repository.
func (i *issuer) route(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/jwks" {
		http.NotFound(w, r)

		return
	}

	i.jwksHits.Add(1)

	i.mu.Lock()
	down := i.down
	i.mu.Unlock()

	if down {
		http.Error(w, "down for maintenance", http.StatusServiceUnavailable)

		return
	}

	w.Header().Set("Content-Type", "application/json")

	document := map[string]any{"keys": []map[string]string{
		{
			"kty": "RSA",
			"use": "sig",
			"alg": "RS256",
			"kid": keyID,
			"n":   base64.RawURLEncoding.EncodeToString(i.key.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString([]byte{0x01, 0x00, 0x01}),
		},
		// An encryption key beside the signing one. A verifier that took it
		// would check signatures against a key nobody signs with.
		{
			"kty": "RSA",
			"use": "enc",
			"kid": "an-encryption-key",
			"n":   base64.RawURLEncoding.EncodeToString(i.key.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString([]byte{0x01, 0x00, 0x01}),
		},
	}}

	if err := json.NewEncoder(w).Encode(document); err != nil {
		panic(err)
	}
}

func (i *issuer) setDown(down bool) {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.down = down
}

// Claims is the payload of a real token, exposed so a test can bend one field
// and leave the rest as the issuer would send it.
func (i *issuer) Claims() map[string]any {
	now := time.Now()

	return map[string]any{
		"iss":                i.URL,
		"sub":                i.Subject,
		"aud":                audience,
		"exp":                now.Add(time.Hour).Unix(),
		"iat":                now.Unix(),
		"nbf":                now.Add(-time.Minute).Unix(),
		"email":              i.Email,
		"name":               i.Name,
		"preferred_username": "operator",
	}
}

// Sign renders claims as an RS256 JWS under the published key.
func (i *issuer) Sign(claims map[string]any) string {
	return i.SignWith(map[string]any{"alg": "RS256", "typ": "JWT", "kid": keyID}, claims)
}

// SignWith signs with a header the caller chose, which is how a token claiming
// an algorithm nobody should accept is produced.
func (i *issuer) SignWith(header, claims map[string]any) string {
	return authapitest.SignRS256(i.key, header, claims)
}

func segment(value any) string { return authapitest.Segment(value) }
