package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/authapi/oidctoken"
)

// Errors reported while checking an ID token. A caller turns all of them into
// one refusal; they are separate so a log line can say which.
var (
	// ErrMalformedJWT reports text that is not three base64url segments.
	ErrMalformedJWT = errors.New("oidc: malformed id token")
	// ErrUnsupportedAlgorithm reports a token signed with an algorithm the
	// issuer did not advertise, that this server cannot check, or that needs
	// a client secret this server does not have. A token claiming the `none`
	// algorithm is refused with it.
	ErrUnsupportedAlgorithm = errors.New("oidc: unsupported id token algorithm")
	// ErrUnknownKey reports a kid the issuer's JWKS does not publish, or a
	// JWKS that could not be read.
	ErrUnknownKey = errors.New("oidc: id token signed by an unknown key")
	// ErrBadSignature reports a signature that does not verify.
	ErrBadSignature = errors.New("oidc: id token signature does not verify")
	// ErrClaimRejected reports a token whose claims do not match this login.
	ErrClaimRejected = errors.New("oidc: id token claim rejected")
)

// clockSkew is what an `exp` or `nbf` is allowed to be wrong by. A local
// install and its issuer are usually the same host, and an NTP step of a
// second should not log everybody out.
const clockSkew = 30 * time.Second

// Claims is what this server reads out of an ID token. Any other claim the
// issuer sends is ignored.
type Claims struct {
	Subject           string   `json:"sub"`
	Issuer            string   `json:"iss"`
	Audience          audience `json:"aud"`
	Nonce             string   `json:"nonce"`
	ExpiresAt         int64    `json:"exp"`
	NotBefore         int64    `json:"nbf"`
	IssuedAt          int64    `json:"iat"`
	Email             string   `json:"email"`
	Name              string   `json:"name"`
	PreferredUsername string   `json:"preferred_username"`
	// Username is not an OpenID Connect claim. Synology SSO sends it in place
	// of preferred_username.
	Username string `json:"username"`
	// Algorithm is the JWS algorithm the token was signed with, read from its
	// header once the signature held. It is for a log line.
	Algorithm string `json:"-"`
}

// audience is `aud`, which RFC 7519 allows to be one string or an array of
// them. The published contract defines both readings, so this is an alias.
type audience = oidctoken.Audience

// verifyIDToken checks the signature and then every claim that binds the token
// to this login. Order matters: nothing in the payload is read as true before
// the signature over it is verified.
func verifyIDToken(
	ctx context.Context,
	policy oidctoken.Policy,
	raw, issuer, clientID, nonce string,
	now time.Time,
) (Claims, error) {
	payload, err := oidctoken.Verify(ctx, raw, policy)
	if err != nil {
		return Claims{}, signatureError(err)
	}

	var claims Claims
	if decodeErr := json.Unmarshal(payload, &claims); decodeErr != nil {
		return Claims{}, fmt.Errorf("%w: payload: %w", ErrMalformedJWT, decodeErr)
	}

	claims.Algorithm = algorithmOf(raw)

	return claims, claims.check(issuer, clientID, nonce, now)
}

// signatureError keeps what the shared check said and adds this package's
// error, so a log line names the cause and a caller can still tell them apart.
func signatureError(err error) error {
	switch {
	case errors.Is(err, oidctoken.ErrMalformed):
		return fmt.Errorf("%w: %w", ErrMalformedJWT, err)
	case errors.Is(err, oidctoken.ErrAlgorithm):
		return fmt.Errorf("%w: %w", ErrUnsupportedAlgorithm, err)
	case errors.Is(err, oidctoken.ErrSignature):
		return fmt.Errorf("%w: %w", ErrBadSignature, err)
	default:
		return fmt.Errorf("%w: %w", ErrUnknownKey, err)
	}
}

// algorithmOf reads the alg of a token whose signature already held.
func algorithmOf(raw string) string {
	header, _, _ := strings.Cut(raw, ".")

	decoded, err := oidctoken.DecodeSegment(header)
	if err != nil {
		return ""
	}

	var head struct {
		Algorithm string `json:"alg"`
	}

	_ = json.Unmarshal(decoded, &head)

	return head.Algorithm
}

// check ties a token to the login that asked for it.
func (c Claims) check(issuer, clientID, nonce string, now time.Time) error {
	switch {
	case c.Subject == "":
		return fmt.Errorf("%w: no sub", ErrClaimRejected)
	case !sameIssuer(c.Issuer, issuer):
		return fmt.Errorf("%w: iss %q, expected %q", ErrClaimRejected, c.Issuer, issuer)
	case !slices.Contains(c.Audience, clientID):
		return fmt.Errorf("%w: aud %v, expected %q", ErrClaimRejected, []string(c.Audience), clientID)
	case c.Nonce != nonce:
		// The nonce ties the token to the authorization request this process
		// started, so a token minted for another login cannot be replayed here.
		return fmt.Errorf("%w: nonce does not match this login", ErrClaimRejected)
	case c.ExpiresAt == 0 || now.After(time.Unix(c.ExpiresAt, 0).Add(clockSkew)):
		return fmt.Errorf("%w: expired at %d", ErrClaimRejected, c.ExpiresAt)
	case c.NotBefore != 0 && now.Add(clockSkew).Before(time.Unix(c.NotBefore, 0)):
		return fmt.Errorf("%w: not valid before %d", ErrClaimRejected, c.NotBefore)
	default:
		return nil
	}
}
