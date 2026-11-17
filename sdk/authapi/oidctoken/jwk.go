package oidctoken

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"errors"
	"fmt"
)

const (
	// bitsPerByte turns a curve's bit size into the bytes of one coordinate.
	bitsPerByte = 8
	// uncompressedPoint starts an uncompressed point, SEC 1 section 2.3.3.
	uncompressedPoint = 0x04
)

// jwk is one entry of a key set, RFC 7517 section 4, with the members of the
// three key types RFC 7518 section 6 and RFC 8037 define for a signature.
type jwk struct {
	Kind  string `json:"kty"`
	ID    string `json:"kid"`
	Use   string `json:"use"`
	Curve string `json:"crv"`
	// N and E are an RSA key's modulus and exponent.
	N string `json:"n"`
	E string `json:"e"`
	// X and Y are an EC key's point. An OKP key has only X.
	X string `json:"x"`
	Y string `json:"y"`
}

// publicKey rebuilds the key a JWK describes.
func (j jwk) publicKey() (crypto.PublicKey, error) {
	switch j.Kind {
	case "RSA":
		return RSAPublicKey(j.N, j.E)
	case "EC":
		return ECPublicKey(j.Curve, j.X, j.Y)
	case "OKP":
		return Ed25519PublicKey(j.Curve, j.X)
	default:
		return nil, fmt.Errorf("oidctoken: the jwk is of type %q, which signs nothing read here", j.Kind)
	}
}

// curves are the EC curves RFC 7518 section 6.2.1.1 names.
var curves = map[string]elliptic.Curve{
	"P-256": elliptic.P256(),
	"P-384": elliptic.P384(),
	"P-521": elliptic.P521(),
}

// ECPublicKey rebuilds an EC signing key from the curve name and the base64url
// coordinates of a JWK. A point that is not on the curve is refused, which
// prevents an invalid-curve attack.
func ECPublicKey(curve, x, y string) (*ecdsa.PublicKey, error) {
	named, ok := curves[curve]
	if !ok {
		return nil, fmt.Errorf("oidctoken: the jwk names the curve %q, which is not read here", curve)
	}

	xBytes, err := DecodeSegment(x)
	if err != nil {
		return nil, err
	}

	yBytes, err := DecodeSegment(y)
	if err != nil {
		return nil, err
	}

	size := (named.Params().BitSize + bitsPerByte - 1) / bitsPerByte
	if len(xBytes) != size || len(yBytes) != size {
		return nil, errors.New("oidctoken: the jwk carries a point of the wrong size for its curve")
	}

	// The uncompressed encoding is 0x04 followed by both coordinates, and the
	// parse refuses a point that is not on the curve.
	encoded := append(append([]byte{uncompressedPoint}, xBytes...), yBytes...)

	key, err := ecdsa.ParseUncompressedPublicKey(named, encoded)
	if err != nil {
		return nil, fmt.Errorf("oidctoken: the jwk carries a point that is not on %s: %w", curve, err)
	}

	return key, nil
}

// Ed25519PublicKey rebuilds an Ed25519 key from an OKP JWK, RFC 8037 section
// 2. Ed448 is not read here, because the standard library cannot check it.
func Ed25519PublicKey(curve, x string) (ed25519.PublicKey, error) {
	if curve != "Ed25519" {
		return nil, fmt.Errorf("oidctoken: the jwk names the curve %q, which is not read here", curve)
	}

	key, err := DecodeSegment(x)
	if err != nil {
		return nil, err
	}

	if len(key) != ed25519.PublicKeySize {
		return nil, errors.New("oidctoken: the jwk carries an Ed25519 key of the wrong size")
	}

	return ed25519.PublicKey(key), nil
}
