package oidc_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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
const keyID = "test-signing-key"

// issuer is an OpenID Connect issuer that actually performs the exchange:
// discovery, an authorize endpoint that records what one login asked for, a
// token endpoint that enforces PKCE, and a JWKS the signature is checked
// against. A provider verified against a stub that hands back a fixed token
// proves the stub.
type issuer struct {
	*httptest.Server

	key   *rsa.PrivateKey
	claim string // the claim name roles are published under

	// jwksHits counts what the key set costs the issuer, which is what the
	// refetch throttle is measured by.
	jwksHits atomic.Int64

	mu      sync.Mutex
	pending map[string]pendingLogin
	mint    func(nonce string) string

	// Subject, Email and Roles are what the next minted token carries.
	Subject string
	Email   string
	Name    string
	Roles   []string
}

// pendingLogin is what the authorize endpoint recorded for one code.
type pendingLogin struct {
	challenge   string
	nonce       string
	redirectURI string
}

func newIssuer(t *testing.T) *issuer {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	iss := &issuer{
		key:     key,
		claim:   "roles",
		pending: map[string]pendingLogin{},
		Subject: "8f1e1b2c-0000-4000-8000-abcdefabcdef",
		Email:   "operator@example.test",
		Name:    "Fleet Operator",
		Roles:   []string{"admin", "viewer"},
	}

	iss.Server = httptest.NewServer(http.HandlerFunc(iss.route))
	t.Cleanup(iss.Close)

	return iss
}

// route dispatches by path rather than through a ServeMux, which
// TestNothingOutsideTheSealedPackageMountsAServeMux refuses anywhere in this
// repository: a mux registers routes the OpenAPI document cannot enumerate, and
// the gate does not read whether the file is a test.
func (i *issuer) route(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		i.metadata(w, r)
	case "/jwks":
		i.jwks(w, r)
	case "/authorize":
		i.authorize(w, r)
	case "/token":
		i.token(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (i *issuer) metadata(w http.ResponseWriter, _ *http.Request) {
	write(w, map[string]string{
		"issuer":                 i.URL,
		"authorization_endpoint": i.URL + "/authorize",
		"token_endpoint":         i.URL + "/token",
		"jwks_uri":               i.URL + "/jwks",
	})
}

func (i *issuer) jwks(w http.ResponseWriter, _ *http.Request) {
	i.jwksHits.Add(1)

	write(w, map[string]any{"keys": []map[string]string{{
		"kty": "RSA",
		"use": "sig",
		"alg": "RS256",
		"kid": keyID,
		"n":   base64.RawURLEncoding.EncodeToString(i.key.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString([]byte{0x01, 0x00, 0x01}),
	}}})
}

// authorize is the screen a person would see. It records what this login asked
// for and redirects straight back with a code, which is what the browser does
// once they have signed in.
func (i *issuer) authorize(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	redirectURI := query.Get("redirect_uri")
	if redirectURI == "" || query.Get("code_challenge_method") != "S256" {
		http.Error(w, "the request is not an OAuth 2.1 authorization request", http.StatusBadRequest)

		return
	}

	code := "code-" + query.Get("nonce")

	i.mu.Lock()
	i.pending[code] = pendingLogin{
		challenge:   query.Get("code_challenge"),
		nonce:       query.Get("nonce"),
		redirectURI: redirectURI,
	}
	i.mu.Unlock()

	back, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "unparseable redirect_uri", http.StatusBadRequest)

		return
	}

	answer := back.Query()
	answer.Set("code", code)
	answer.Set("state", query.Get("state"))
	back.RawQuery = answer.Encode()

	w.Header().Set("Location", back.String())
	w.WriteHeader(http.StatusFound)
}

// token exchanges a code for an ID token, holding the client to the PKCE
// verifier and the redirect URI its authorize request carried.
func (i *issuer) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unreadable form", http.StatusBadRequest)

		return
	}

	i.mu.Lock()
	started, known := i.pending[r.PostForm.Get("code")]
	i.mu.Unlock()

	verifier := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))

	switch {
	case r.PostForm.Get("grant_type") != "authorization_code":
		http.Error(w, "unsupported_grant_type", http.StatusBadRequest)
	case !known:
		http.Error(w, "invalid_grant", http.StatusBadRequest)
	case base64.RawURLEncoding.EncodeToString(verifier[:]) != started.challenge:
		http.Error(w, "invalid_grant: the code_verifier does not match the challenge", http.StatusBadRequest)
	case r.PostForm.Get("redirect_uri") != started.redirectURI:
		http.Error(w, "invalid_grant: redirect_uri mismatch", http.StatusBadRequest)
	default:
		write(w, map[string]string{
			"token_type": "Bearer",
			"id_token":   i.mintFor(started.nonce),
		})
	}
}

// mintWith replaces what the token endpoint hands back, so a case can send the
// one token a real issuer would never sign.
func (i *issuer) mintWith(mint func(nonce string) string) {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.mint = mint
}

func (i *issuer) mintFor(nonce string) string {
	i.mu.Lock()
	mint := i.mint
	i.mu.Unlock()

	if mint != nil {
		return mint(nonce)
	}

	return i.Sign(i.Claims(nonce))
}

// Claims is the payload of the next token, exposed so a test can bend one field
// and leave the rest a real issuer would send.
func (i *issuer) Claims(nonce string) map[string]any {
	now := time.Now()

	return map[string]any{
		"iss":                i.URL,
		"sub":                i.Subject,
		"aud":                clientID,
		"exp":                now.Add(time.Hour).Unix(),
		"iat":                now.Unix(),
		"nonce":              nonce,
		"email":              i.Email,
		"name":               i.Name,
		"preferred_username": "operator",
		i.claim:              i.Roles,
	}
}

// Sign renders claims as an RS256 JWS, the way the issuer signs a real one.
func (i *issuer) Sign(claims map[string]any) string {
	return i.SignWith(map[string]any{"alg": "RS256", "typ": "JWT", "kid": keyID}, claims)
}

// SignWith signs with a header the caller chose, which is how a token claiming
// an algorithm nobody should accept is produced.
func (i *issuer) SignWith(header, claims map[string]any) string {
	return authapitest.SignRS256(i.key, header, claims)
}

// Browser follows one authorize URL the way a browser would and returns the
// parameters the issuer sends back to the callback.
func (i *issuer) Browser(t *testing.T, authorizeURL string) url.Values {
	t.Helper()

	client := &http.Client{
		// The callback is a URL this test does not serve, so the redirect is
		// read rather than followed.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, authorizeURL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusFound, resp.StatusCode, "the issuer did not redirect back")

	back, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)

	return back.Query()
}

func write(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(body); err != nil {
		panic(err)
	}
}
