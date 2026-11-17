package upgrade

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"strings"
)

// officialPublicKeys is a comma-separated set of base64 Ed25519 public keys
// stamped into release builds. A source build carries none unless its build
// pipeline supplies them.
var officialPublicKeys string

type TrustedKey struct {
	ID  string
	Key string
}

func BuildVerifier(keys []TrustedKey, allowUnsigned bool) (Verifier, error) {
	declared := make([]string, 0, len(keys)+1)
	if strings.TrimSpace(officialPublicKeys) != "" {
		declared = append(declared, strings.Split(officialPublicKeys, ",")...)
	}
	for _, key := range keys {
		declared = append(declared, key.Key)
	}

	trusted := make([]ed25519.PublicKey, 0, len(declared))
	for index, encoded := range declared {
		raw, err := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(encoded))
		if err != nil {
			return Verifier{}, fmt.Errorf("%w: decode trusted key %d: %w", ErrSignature, index+1, err)
		}
		if len(raw) != ed25519.PublicKeySize {
			return Verifier{}, fmt.Errorf("%w: trusted key %d holds %d bytes", ErrSignature, index+1, len(raw))
		}

		trusted = append(trusted, ed25519.PublicKey(raw))
	}

	verifier, err := NewVerifier(trusted, allowUnsigned)
	if err != nil {
		return Verifier{}, err
	}

	for _, key := range keys {
		raw, _ := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(key.Key))
		if key.ID != "" && key.ID != KeyID(ed25519.PublicKey(raw)) {
			return Verifier{}, fmt.Errorf("%w: configured key id %q does not match its bytes",
				ErrSignature, key.ID)
		}
	}

	return verifier, nil
}
