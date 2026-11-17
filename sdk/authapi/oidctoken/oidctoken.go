// Package oidctoken reads the pieces of an ID token and a JWKS that every
// OpenID Connect provider has to read the same way.
//
// It is the JOSE half only. Discovery, the authorize redirect, the token
// exchange and the claim checks that bind a token to one login belong to the
// provider.
package oidctoken

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
)

// exponentBytes is the widest public exponent read here.
const exponentBytes = 8

// DecodeSegment reads one unpadded base64url string, the encoding of every
// part of a compact JWS (RFC 7515 section 2) and every number in a JWK (RFC
// 7517). Padding is refused, so a token has one encoding.
func DecodeSegment(segment string) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		return nil, fmt.Errorf("not base64url: %w", err)
	}

	return decoded, nil
}

// Audience is the `aud` claim. RFC 7519 section 4.1.3 allows one string or an
// array of them.
type Audience []string

// UnmarshalJSON accepts both shapes section 4.1.3 allows.
func (a *Audience) UnmarshalJSON(data []byte) error {
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		*a = Audience{one}

		return nil
	}

	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return fmt.Errorf("aud is neither a string nor a list of them: %w", err)
	}

	*a = many

	return nil
}

// RSAPublicKey rebuilds a signing key from the base64url big-endian modulus and
// exponent of a JWK. An exponent too long for an int is refused, never
// truncated, because a truncated one would be a key the issuer did not
// publish.
func RSAPublicKey(modulus, exponent string) (*rsa.PublicKey, error) {
	n, err := DecodeSegment(modulus)
	if err != nil {
		return nil, err
	}

	e, err := DecodeSegment(exponent)
	if err != nil {
		return nil, err
	}

	if len(n) == 0 || len(e) == 0 || len(e) > exponentBytes {
		return nil, errors.New("oidctoken: the jwk carries an unusable modulus or exponent")
	}

	padded := make([]byte, exponentBytes)
	copy(padded[exponentBytes-len(e):], e)

	value := binary.BigEndian.Uint64(padded)
	if value > math.MaxInt32 {
		return nil, errors.New("oidctoken: the jwk carries a public exponent no RSA key uses")
	}

	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(n),
		E: int(value),
	}, nil
}

// DisplayName returns the first candidate that is not empty. End the list
// with the subject, so the result is never empty.
func DisplayName(candidates ...string) string {
	for _, candidate := range candidates {
		if candidate != "" {
			return candidate
		}
	}

	return ""
}
