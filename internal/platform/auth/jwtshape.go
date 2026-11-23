package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// An app pairing credential reaches its client in the shape of a JWT as well
// as bare (ADR-20261112-nay). This server reads it only from an
// Authorization: Bearer header, and never from a cookie, auth.access_token
// included (ADR-20261112-nay rule 14). No client checks a signature. This server reads only the third segment, which
// is the minted token, and holds the payload to the row that token names. The
// header and the payload are a copy of the row for the client, and a holder
// who edits them gains nothing.

// JWTIssuer is the iss claim of every token this server shapes as a JWT.
const JWTIssuer = "prusa-connect"

// jwtHeader is the header every such token carries. The algorithm is none,
// because nothing a client could check signs the token. The third segment is
// a credential this server looks up.
//
//nolint:gochecknoglobals // a fixed encoding computed once, never written.
var jwtHeader = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","kid":"pcl","typ":"JWT"}`))

// maxJWTPayload bounds the payload segment a verify decodes. The claims this
// server writes take about two hundred bytes.
const maxJWTPayload = 1024

// MintedClaims is the payload of a token this server shapes as a JWT.
type MintedClaims struct {
	// Subject is the principal the credential speaks for.
	Subject string `json:"sub"`
	// IssuedAt and ExpiresAt are seconds since the Unix epoch.
	IssuedAt  int64 `json:"iat"`
	ExpiresAt int64 `json:"exp"`
	// Issuer is JWTIssuer.
	Issuer string `json:"iss"`
	// ID is the credential's public id.
	ID string `json:"jti"`
}

// WrapJWT shapes a minted token as a JWT whose payload describes cred.
func WrapJWT(token Token, cred Credential) Token {
	payload, err := json.Marshal(MintedClaims{
		Subject:   cred.Subject,
		IssuedAt:  cred.IssuedAt.Unix(),
		ExpiresAt: cred.ExpiresAt.Unix(),
		Issuer:    JWTIssuer,
		ID:        cred.PublicID,
	})
	if err != nil {
		// A struct of strings and integers always encodes.
		panic(fmt.Sprintf("auth: encode the claims: %v", err))
	}

	return Token(jwtHeader + "." + base64.RawURLEncoding.EncodeToString(payload) + "." + string(token))
}

// IsMintedJWT reports whether presented text is shaped like a JWT whose third
// segment is a token this server minted. Such text is checked by the verifier
// and never by the bearer path.
func IsMintedJWT(token Token) bool {
	_, inner, ok := jwtSegments(token)

	return ok && strings.HasPrefix(inner, Prefix)
}

// Inner is the minted token presented text carries: the third segment of the
// JWT shape, or the text itself.
func Inner(token Token) Token {
	if !IsMintedJWT(token) {
		return token
	}

	_, inner, _ := jwtSegments(token)

	return Token(inner)
}

// unwrapJWT reads a token of the JWT shape. A token of any other shape comes
// back unchanged with no claims. A token of the shape whose payload cannot be
// read is ErrMalformedToken.
func unwrapJWT(token Token) (Token, *MintedClaims, error) {
	if !IsMintedJWT(token) {
		return token, nil, nil
	}

	payload, inner, _ := jwtSegments(token)
	if len(payload) > maxJWTPayload {
		return "", nil, fmt.Errorf("%w: the payload is %d bytes", ErrMalformedToken, len(payload))
	}

	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", nil, fmt.Errorf("%w: the payload is not base64url: %w", ErrMalformedToken, err)
	}

	var claims MintedClaims
	if err = json.Unmarshal(decoded, &claims); err != nil {
		return "", nil, fmt.Errorf("%w: the payload is not the claims: %w", ErrMalformedToken, err)
	}

	return Token(inner), &claims, nil
}

// match holds the claims to the row the third segment named. Only an app
// pairing credential is shaped as a JWT, so a session or a credential a person
// minted, wrapped by hand, is refused. The cookie the
// slicer's page keeps its token in then cannot carry a session past the flags
// the session cookie has.
func (c *MintedClaims) match(cred Credential) error {
	switch {
	case cred.Kind != KindAPI || cred.Origin != OriginAppPairing || cred.Parent != "":
		return fmt.Errorf("%w: %q is a %s credential of origin %q, and only an app pairing credential "+
			"is shaped as a JWT", ErrMalformedToken, cred.PublicID, cred.Kind, cred.Origin)
	case c.ID != cred.PublicID:
		return fmt.Errorf("%w: the jti %q names another credential than %q", ErrMalformedToken, c.ID, cred.PublicID)
	case c.Subject != cred.Subject:
		return fmt.Errorf("%w: the sub %q is not the subject of %q", ErrMalformedToken, c.Subject, cred.PublicID)
	case c.ExpiresAt != cred.ExpiresAt.Unix():
		return fmt.Errorf("%w: the exp %d is not the expiry %d of %q",
			ErrMalformedToken, c.ExpiresAt, cred.ExpiresAt.Unix(), cred.PublicID)
	}

	return nil
}

// jwtSegments splits text into the three segments of a compact JWS and
// returns the two this server reads. The header is never read.
func jwtSegments(token Token) (payload, signature string, ok bool) {
	parts := strings.Split(string(token), ".")
	if len(parts) != jwsSegments {
		return "", "", false
	}

	return parts[1], parts[2], true
}
