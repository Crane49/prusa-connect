package oidc

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// ChallengeMethodS256 is the only code challenge method this client offers.
// OAuth 2.1 removes `plain`, and this client does not downgrade to it for an
// issuer that still advertises it. Sending `plain` would put the verifier in
// the authorization request, where a leaked redirect exposes it.
const ChallengeMethodS256 = "S256"

// verifierBytes is 32 bytes, which base64url-encodes to 43 characters. RFC 7636
// section 4.1 allows 43 to 128 and recommends 32 bytes of entropy.
const verifierBytes = 32

// PKCE is one login's proof-of-possession pair. Only the challenge is sent to
// the issuer. The verifier stays in this process until the token request.
type PKCE struct {
	Verifier  string
	Challenge string
}

// NewPKCE draws a fresh verifier and derives its S256 challenge.
func NewPKCE() (PKCE, error) {
	raw := make([]byte, verifierBytes)
	if _, err := rand.Read(raw); err != nil {
		return PKCE{}, fmt.Errorf("read pkce verifier: %w", err)
	}

	verifier := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))

	return PKCE{
		Verifier:  verifier,
		Challenge: base64.RawURLEncoding.EncodeToString(sum[:]),
	}, nil
}
