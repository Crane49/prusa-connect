package oidctoken

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"

	// The hashes the algorithms below name, linked in so crypto.Hash.New
	// finds them.
	_ "crypto/sha256"
	_ "crypto/sha512"
)

// SigningAlgorithm is what VerifyRS256 accepts, and what Accepted falls back
// to for an issuer whose discovery document lists no algorithm. RS256 is the
// default OpenID Connect Discovery 1.0 section 3 gives.
const SigningAlgorithm = "RS256"

// jwsParts is how many dot-separated segments a compact JWS has: the header,
// the payload and the signature.
const jwsParts = 3

// The refusals a signature check reports. A caller inspects them with
// errors.Is and wraps the one that applies in its own refusal.
var (
	// ErrMalformed reports text that is not a compact JWS.
	ErrMalformed = errors.New("oidctoken: the token is not a compact JWS")
	// ErrAlgorithm reports a token signed with an algorithm the caller did
	// not accept, alg none included, or an HMAC token with no secret to check
	// it against.
	ErrAlgorithm = errors.New("oidctoken: the token's algorithm is not accepted")
	// ErrSignature reports a signature that does not verify against the key
	// the token names.
	ErrSignature = errors.New("oidctoken: the signature does not verify")
)

// Policy is what a signature is checked against.
type Policy struct {
	// Algorithms are the JWS algorithms a token may use. Nil accepts
	// SigningAlgorithm alone, and an empty list accepts nothing. An algorithm
	// this package does not implement is never accepted.
	Algorithms []string
	// Keys are the issuer's published keys, for every algorithm but HMAC.
	Keys *KeySet
	// Secret is the client secret, which OpenID Connect Core section 10.1
	// makes the key of an HS256, HS384 or HS512 token. Empty refuses those.
	Secret []byte
}

// method is how one JWS algorithm is checked.
type method struct {
	// hash is the HMAC's hash, and zero for an algorithm checked against a
	// published key.
	hash crypto.Hash
	// keyed reports whether a published key can check this algorithm. It is
	// nil for HMAC, whose key is the client secret.
	keyed func(crypto.PublicKey) bool
	// check verifies a signature over the signed bytes with a key keyed
	// accepted.
	check func(key crypto.PublicKey, signed, signature []byte) bool
}

// methods are the algorithms RFC 7518 section 3.1 and RFC 8037 define for a
// signature, less `none`, which is never accepted, and Ed448, which the
// standard library cannot check.
var methods = map[string]method{
	"RS256": rsaPKCS1(crypto.SHA256),
	"RS384": rsaPKCS1(crypto.SHA384),
	"RS512": rsaPKCS1(crypto.SHA512),
	"PS256": rsaPSS(crypto.SHA256),
	"PS384": rsaPSS(crypto.SHA384),
	"PS512": rsaPSS(crypto.SHA512),
	"ES256": ecdsaOn("P-256", crypto.SHA256),
	"ES384": ecdsaOn("P-384", crypto.SHA384),
	"ES512": ecdsaOn("P-521", crypto.SHA512),
	"EdDSA": ed25519Method(),
	// RFC 9864 names the same algorithm by its curve.
	"Ed25519": ed25519Method(),
	"HS256":   {hash: crypto.SHA256},
	"HS384":   {hash: crypto.SHA384},
	"HS512":   {hash: crypto.SHA512},
}

// Supported is every algorithm Verify can check, sorted.
func Supported() []string {
	names := make([]string, 0, len(methods))
	for name := range methods {
		names = append(names, name)
	}

	slices.Sort(names)

	return names
}

// Accepted is the algorithms to accept from an issuer: those its discovery
// document lists under id_token_signing_alg_values_supported and this package
// can check, in the issuer's order. An HMAC algorithm is left out when there
// is no client secret. An issuer that lists nothing gets SigningAlgorithm.
func Accepted(advertised []string, haveSecret bool) []string {
	if len(advertised) == 0 {
		return []string{SigningAlgorithm}
	}

	accepted := make([]string, 0, len(advertised))

	for _, alg := range advertised {
		known, ok := methods[alg]
		if !ok || (known.keyed == nil && !haveSecret) || slices.Contains(accepted, alg) {
			continue
		}

		accepted = append(accepted, alg)
	}

	return accepted
}

// VerifyRS256 checks a compact JWS signed with RS256 against the issuer's
// published keys and returns its payload, still unread.
func VerifyRS256(ctx context.Context, raw string, keys *KeySet) ([]byte, error) {
	return Verify(ctx, raw, Policy{Algorithms: []string{SigningAlgorithm}, Keys: keys})
}

// Verify checks a compact JWS against a policy and returns its payload, still
// unread. The algorithm is refused before a key is fetched, and the payload
// is returned only after the signature verifies. An HMAC token is checked
// against the client secret, never against a published key.
func Verify(ctx context.Context, raw string, policy Policy) ([]byte, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != jwsParts || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("%w: it is not a three-part JWS", ErrMalformed)
	}

	var header struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
	}

	headerBytes, err := DecodeSegment(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: header: %w", ErrMalformed, err)
	}

	if err = json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("%w: header: %w", ErrMalformed, err)
	}

	alg, err := policy.method(header.Algorithm)
	if err != nil {
		return nil, err
	}

	signature, err := DecodeSegment(parts[2])
	if err != nil {
		return nil, fmt.Errorf("%w: signature: %w", ErrMalformed, err)
	}

	if len(signature) == 0 {
		return nil, fmt.Errorf("%w: the signature segment is empty", ErrMalformed)
	}

	signed := []byte(parts[0] + "." + parts[1])

	if err = policy.check(ctx, alg, header.Algorithm, header.KeyID, signed, signature); err != nil {
		return nil, err
	}

	payload, err := DecodeSegment(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: payload: %w", ErrMalformed, err)
	}

	return payload, nil
}

// method returns how to check alg, or the refusal that names why it is not
// accepted.
func (p Policy) method(alg string) (method, error) {
	accepted := p.Algorithms
	if accepted == nil {
		accepted = []string{SigningAlgorithm}
	}

	known, ok := methods[alg]

	switch {
	case !ok:
		return method{}, fmt.Errorf("%w: signed with %q, which this server cannot check", ErrAlgorithm, alg)
	case known.keyed == nil && len(p.Secret) == 0:
		return method{}, fmt.Errorf("%w: signed with %s, which is checked with the client secret, "+
			"and no client secret is configured", ErrAlgorithm, alg)
	case len(accepted) == 0:
		return method{}, fmt.Errorf("%w: signed with %s, and the issuer advertises no algorithm "+
			"this server can check", ErrAlgorithm, alg)
	case !slices.Contains(accepted, alg):
		return method{}, fmt.Errorf("%w: signed with %s, and only %s is accepted",
			ErrAlgorithm, alg, strings.Join(accepted, ", "))
	}

	return known, nil
}

// check verifies one signature with the secret or with the published key the
// token names.
func (p Policy) check(ctx context.Context, alg method, name, kid string, signed, signature []byte) error {
	if alg.keyed == nil {
		mac := hmac.New(alg.hash.New, p.Secret)
		mac.Write(signed)

		if !hmac.Equal(mac.Sum(nil), signature) {
			return fmt.Errorf("%w: the %s signature does not match the client secret", ErrSignature, name)
		}

		return nil
	}

	if p.Keys == nil {
		return fmt.Errorf("%w: there is no key set to check %s against", ErrKeysUnavailable, name)
	}

	key, err := p.Keys.Key(ctx, kid, name)
	if err != nil {
		return err
	}

	if !alg.check(key, signed, signature) {
		return fmt.Errorf("%w: the %s signature does not check out against key %q", ErrSignature, name, kid)
	}

	return nil
}

func digestOf(hash crypto.Hash, signed []byte) []byte {
	digest := hash.New()
	digest.Write(signed)

	return digest.Sum(nil)
}

func rsaPKCS1(hash crypto.Hash) method {
	return method{
		keyed: isRSA,
		check: func(key crypto.PublicKey, signed, signature []byte) bool {
			rsaKey, ok := key.(*rsa.PublicKey)

			return ok && rsa.VerifyPKCS1v15(rsaKey, hash, digestOf(hash, signed), signature) == nil
		},
	}
}

// rsaPSS takes a salt as long as the hash, which RFC 7518 section 3.5
// requires.
func rsaPSS(hash crypto.Hash) method {
	return method{
		keyed: isRSA,
		check: func(key crypto.PublicKey, signed, signature []byte) bool {
			rsaKey, ok := key.(*rsa.PublicKey)
			options := &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash, Hash: hash}

			return ok && rsa.VerifyPSS(rsaKey, hash, digestOf(hash, signed), signature, options) == nil
		},
	}
}

// ecdsaOn checks a signature that is R and S side by side, as RFC 7518 section
// 3.4 requires. The algorithm fixes the curve.
func ecdsaOn(curve string, hash crypto.Hash) method {
	fits := func(key crypto.PublicKey) bool {
		ecKey, ok := key.(*ecdsa.PublicKey)

		return ok && ecKey.Curve.Params().Name == curve
	}

	return method{
		keyed: fits,
		check: func(key crypto.PublicKey, signed, signature []byte) bool {
			ecKey, ok := key.(*ecdsa.PublicKey)
			if !ok || !fits(key) {
				return false
			}

			size := (ecKey.Curve.Params().BitSize + bitsPerByte - 1) / bitsPerByte
			if len(signature) != 2*size {
				return false
			}

			r := new(big.Int).SetBytes(signature[:size])
			s := new(big.Int).SetBytes(signature[size:])

			return ecdsa.Verify(ecKey, digestOf(hash, signed), r, s)
		},
	}
}

// ed25519Method signs the bytes themselves and no digest of them.
func ed25519Method() method {
	return method{
		keyed: func(key crypto.PublicKey) bool {
			_, ok := key.(ed25519.PublicKey)

			return ok
		},
		check: func(key crypto.PublicKey, signed, signature []byte) bool {
			edKey, ok := key.(ed25519.PublicKey)

			return ok && ed25519.Verify(edKey, signed, signature)
		},
	}
}

func isRSA(key crypto.PublicKey) bool {
	_, ok := key.(*rsa.PublicKey)

	return ok
}
