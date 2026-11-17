package joinwire

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/join"
)

// The requirements of JWT mode for a join credential. A JWT is a signed
// token: a header, a set of claims and a signature.
const (
	// AlgRS256 is the only algorithm a join credential may name.
	// TokenAuthorizer refuses any other algorithm before it looks up a key.
	AlgRS256 = "RS256"
	// MinModulusBits is the smallest RSA modulus a verifying key may have, in
	// bits.
	MinModulusBits = 2048
	// DefaultSkew is how far before its not-before time (nbf) a join
	// credential is already accepted. The expiry time (exp) has no such
	// tolerance.
	DefaultSkew = 60 * time.Second
	// DefaultMaxTokenBytes is the largest join credential accepted, in bytes.
	DefaultMaxTokenBytes = 16 << 10
)

// secondsBound is the largest absolute value of a time claim, in seconds.
// parseNumericDate refuses a larger number before it converts the number to a
// time.
const secondsBound = 1 << 40

// The errors TokenAuthorizer returns for a join credential. It wraps each one
// together with join.ErrUnauthorized, so the node denies the join request with
// fractal.CodeJoinUnauthorized.
var (
	// ErrNoToken means the join request has no join credential.
	ErrNoToken = errors.New("the request carries no join credential")
	// ErrTokenAlgorithm means the header's alg is not RS256. TokenAuthorizer
	// checks it before it looks up a key.
	ErrTokenAlgorithm = errors.New("the join credential does not declare RS256")
	// ErrTokenSignature means the signature does not match the verifying key.
	ErrTokenSignature = errors.New("the join credential signature does not verify")
	// ErrWeakKey means the modulus of the verifying key has fewer bits than
	// MinModulusBits. The JWKS key source also returns it as the reason it
	// skipped a key.
	ErrWeakKey = errors.New("the verifying key modulus is too small")
	// ErrTokenExpired means the current time is after the exp claim.
	ErrTokenExpired = errors.New("the join credential has expired")
	// ErrTokenTooEarly means the current time is more than Skew before the nbf
	// claim.
	ErrTokenTooEarly = errors.New("the join credential is not valid yet")
	// ErrTokenMismatch means the nodeId, parentPath or sub claim differs from
	// the join request.
	ErrTokenMismatch = errors.New("the join credential does not match the request")
	// ErrBadNumericDate means a time claim is missing, is not a JSON number,
	// or is over secondsBound.
	ErrBadNumericDate = errors.New("the time claim is not a numeric date")
	// ErrTokenTooLarge means the join credential has more bytes than
	// MaxTokenBytes. TokenAuthorizer checks the size before it parses.
	ErrTokenTooLarge = errors.New("the join credential is over the size bound")
)

// ErrUnknownKey means a key source fetched its keys again and still has no key
// with the kid, the key ID in the token's header. The node then denies the
// join request with fractal.CodeJoinUnauthorized. Every other error of a key
// source means the node could not check the credential, and it denies with
// fractal.CodeJoinUnavailable.
var ErrUnknownKey = errors.New("the key source holds no key under that kid")

// KeySource returns the public key that verifies a join credential. The
// standard source is a JWKS endpoint, a URL that serves a JSON list of public
// keys.
type KeySource interface {
	// Key returns the key with this kid. It returns ErrUnknownKey when the
	// source fetched its keys again and has no such key. It returns any other
	// error when it could not look the key up.
	Key(ctx context.Context, kid string) (*rsa.PublicKey, error)
}

// tokenHeader is the protected header of a join credential.
type tokenHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

// tokenClaims has the claims JWT mode reads. Every other claim must be
// ignored, and encoding/json does that.
type tokenClaims struct {
	NodeID     fractal.NodeID   `json:"nodeId"`
	ParentPath fractal.NodePath `json:"parentPath"`
	Subject    string           `json:"sub"`
	Expires    json.RawMessage  `json:"exp"`
	NotBefore  json.RawMessage  `json:"nbf"`
}

// TokenAuthorizer implements JWT mode. An authority outside the node decides
// which node may join and signs a token for it. This node verifies the token
// with a key from Keys. TokenAuthorizer implements join.Authorizer of the sdk.
type TokenAuthorizer struct {
	// Keys returns the public keys. It is required.
	Keys KeySource
	// Now returns the current time. Nil means time.Now.
	Now func() time.Time
	// Skew is how far before its nbf time a token is already accepted. Zero
	// means DefaultSkew. Skew never applies to the exp claim.
	Skew time.Duration
	// MaxTokenBytes is the largest join credential accepted, in bytes. Zero
	// or less means DefaultMaxTokenBytes.
	MaxTokenBytes int
}

// Authorize verifies the join credential of a join request. It then compares
// the token's claims with the Node ID the request asks for, with the Node Path
// of this node (parent), and with the public key that signed the request.
func (a TokenAuthorizer) Authorize(ctx context.Context, req fractal.JoinRequest, parent fractal.NodePath) error {
	if a.Keys == nil {
		return errors.New("jwt mode: no key source is configured")
	}
	if req.Credential == "" {
		return fmt.Errorf("%w: %w", join.ErrUnauthorized, ErrNoToken)
	}
	limit := a.MaxTokenBytes
	if limit <= 0 {
		limit = DefaultMaxTokenBytes
	}
	if len(req.Credential) > limit {
		return fmt.Errorf("%w: %d bytes over %d: %w",
			join.ErrUnauthorized, len(req.Credential), limit, ErrTokenTooLarge)
	}
	claims, err := a.verify(ctx, string(req.Credential))
	if err != nil {
		return err
	}
	return a.match(claims, req, parent)
}

// verify checks the algorithm, looks up the key, verifies the signature and
// checks the time claims, in that order.
func (a TokenAuthorizer) verify(ctx context.Context, token string) (tokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != jwsParts {
		return tokenClaims{}, fmt.Errorf("%w: %d parts: %w", join.ErrUnauthorized, len(parts), ErrNotCompact)
	}
	enc := base64.RawURLEncoding
	rawHeader, err := enc.DecodeString(parts[0])
	if err != nil {
		return tokenClaims{}, fmt.Errorf("%w: decode the credential header: %w", join.ErrUnauthorized, err)
	}
	var header tokenHeader
	if unmarshalErr := json.Unmarshal(rawHeader, &header); unmarshalErr != nil {
		return tokenClaims{}, fmt.Errorf("%w: parse the credential header: %w", join.ErrUnauthorized, unmarshalErr)
	}
	if header.Alg != AlgRS256 {
		return tokenClaims{}, fmt.Errorf("%w: alg %q: %w", join.ErrUnauthorized, header.Alg, ErrTokenAlgorithm)
	}
	key, err := a.Keys.Key(ctx, header.Kid)
	switch {
	case errors.Is(err, ErrUnknownKey):
		return tokenClaims{}, fmt.Errorf("%w: kid %q: %w", join.ErrUnauthorized, header.Kid, err)
	case err != nil:
		// The key source failed, so the token was not checked. The error does
		// not wrap join.ErrUnauthorized, and the node denies with
		// fractal.CodeJoinUnavailable.
		return tokenClaims{}, fmt.Errorf("resolve the verifying key for kid %q: %w", header.Kid, err)
	}
	if bits := key.N.BitLen(); bits < MinModulusBits {
		return tokenClaims{}, fmt.Errorf("%w: %d bits: %w", join.ErrUnauthorized, bits, ErrWeakKey)
	}
	sig, err := enc.DecodeString(parts[2])
	if err != nil {
		return tokenClaims{}, fmt.Errorf("%w: decode the credential signature: %w", join.ErrUnauthorized, err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if verifyErr := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig); verifyErr != nil {
		return tokenClaims{}, fmt.Errorf("%w: %w", join.ErrUnauthorized, ErrTokenSignature)
	}
	rawClaims, err := enc.DecodeString(parts[1])
	if err != nil {
		return tokenClaims{}, fmt.Errorf("%w: decode the credential claims: %w", join.ErrUnauthorized, err)
	}
	var claims tokenClaims
	if unmarshalErr := json.Unmarshal(rawClaims, &claims); unmarshalErr != nil {
		return tokenClaims{}, fmt.Errorf("%w: parse the credential claims: %w", join.ErrUnauthorized, unmarshalErr)
	}
	if timeErr := a.checkTimes(claims); timeErr != nil {
		return tokenClaims{}, timeErr
	}
	return claims, nil
}

// checkTimes refuses a token at or after its exp time, with no tolerance, and
// before its nbf time minus Skew.
func (a TokenAuthorizer) checkTimes(claims tokenClaims) error {
	now := time.Now
	if a.Now != nil {
		now = a.Now
	}
	at := now()
	expires, err := parseNumericDate(claims.Expires)
	if err != nil {
		return fmt.Errorf("%w: exp: %w", join.ErrUnauthorized, err)
	}
	if !at.Before(expires) {
		return fmt.Errorf("%w: expired at %s: %w", join.ErrUnauthorized, expires.UTC(), ErrTokenExpired)
	}
	if len(claims.NotBefore) == 0 {
		return nil
	}
	notBefore, err := parseNumericDate(claims.NotBefore)
	if err != nil {
		return fmt.Errorf("%w: nbf: %w", join.ErrUnauthorized, err)
	}
	skew := a.Skew
	if skew == 0 {
		skew = DefaultSkew
	}
	if at.Before(notBefore.Add(-skew)) {
		return fmt.Errorf("%w: valid from %s: %w", join.ErrUnauthorized, notBefore.UTC(), ErrTokenTooEarly)
	}
	return nil
}

// match compares the nodeId, parentPath and sub claims with the join request.
func (a TokenAuthorizer) match(claims tokenClaims, req fractal.JoinRequest, parent fractal.NodePath) error {
	switch {
	case claims.NodeID != req.NodeID:
		return fmt.Errorf("%w: nodeId %q asked for %q: %w",
			join.ErrUnauthorized, claims.NodeID, req.NodeID, ErrTokenMismatch)
	case claims.ParentPath != parent:
		return fmt.Errorf("%w: parentPath %q asked at %q: %w",
			join.ErrUnauthorized, claims.ParentPath, parent, ErrTokenMismatch)
	case claims.Subject != "" && fractal.Identity(claims.Subject) != req.Identity:
		// An issuer that knows the node's public key puts it in sub. A token
		// with no sub works for every key, for this Node ID at this node.
		return fmt.Errorf("%w: sub names another identity: %w", join.ErrUnauthorized, ErrTokenMismatch)
	}
	return nil
}

// parseNumericDate parses one time claim. RFC 7519 defines a NumericDate as a
// JSON number, so it accepts an integer, a fraction and an exponent. It
// refuses a value over secondsBound before it converts the value to a time.
func parseNumericDate(raw json.RawMessage) (time.Time, error) {
	if len(raw) == 0 {
		return time.Time{}, fmt.Errorf("%w: the claim is missing", ErrBadNumericDate)
	}
	// A JSON string of digits is not a JSON number. A number starts with a
	// digit or a minus sign, and a string starts with a quote.
	text := strings.TrimSpace(string(raw))
	if text == "" || (text[0] != '-' && (text[0] < '0' || text[0] > '9')) {
		return time.Time{}, fmt.Errorf("%w: %s is not a json number", ErrBadNumericDate, text)
	}
	number := json.Number(text)
	if seconds, err := number.Int64(); err == nil {
		if seconds > secondsBound || seconds < -secondsBound {
			return time.Time{}, fmt.Errorf("%w: %d seconds is out of range", ErrBadNumericDate, seconds)
		}
		return time.Unix(seconds, 0), nil
	}
	seconds, err := number.Float64()
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %w", ErrBadNumericDate, err)
	}
	if math.IsNaN(seconds) || seconds > secondsBound || seconds < -secondsBound {
		return time.Time{}, fmt.Errorf("%w: %v seconds is out of range", ErrBadNumericDate, seconds)
	}
	whole, fraction := int64(seconds), seconds-float64(int64(seconds))
	return time.Unix(whole, int64(fraction*float64(time.Second))), nil
}
