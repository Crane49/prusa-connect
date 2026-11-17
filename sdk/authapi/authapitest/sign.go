package authapitest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
)

// SignRS256 renders header and claims as a compact JWS under key, for a test
// issuer. The header is the caller's, which is how a token claiming an
// algorithm nobody should accept is produced: the signature is always RS256
// whatever the header says, and a verifier that reads the header first refuses
// it before checking.
func SignRS256(key *rsa.PrivateKey, header, claims map[string]any) string {
	signing := Segment(header) + "." + Segment(claims)

	digest := sha256.Sum256([]byte(signing))

	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		panic(err)
	}

	return signing + "." + base64.RawURLEncoding.EncodeToString(signature)
}

// Segment renders a value as one unpadded base64url JWS segment.
func Segment(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}

	return base64.RawURLEncoding.EncodeToString(raw)
}
