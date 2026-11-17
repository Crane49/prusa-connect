package prusaaccount

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/authapi"
	"github.com/prusa3d/prusa-connect/sdk/authapi/oidctoken"
)

// claims is the part of a token this adapter reads.
type claims struct {
	Issuer    string             `json:"iss"`
	Subject   string             `json:"sub"`
	Audience  oidctoken.Audience `json:"aud"`
	Expiry    int64              `json:"exp"`
	NotBefore int64              `json:"nbf"`
	IssuedAt  int64              `json:"iat"`
	Email     string             `json:"email"`
	Name      string             `json:"name"`
	Username  string             `json:"preferred_username"`
}

// verify checks the token's signature against the issuer's published keys,
// then the claims that bind it to this server.
func (p *Provider) verify(ctx context.Context, raw string, now time.Time) (claims, error) {
	payload, err := oidctoken.VerifyRS256(ctx, raw, p.keys)
	if err != nil {
		return claims{}, tokenError(err)
	}

	var found claims
	if err = json.Unmarshal(payload, &found); err != nil {
		return claims{}, fmt.Errorf("%w: payload: %w", authapi.ErrTokenMalformed, err)
	}

	return found, found.check(p.config, now)
}

// tokenError maps an oidctoken error to the authapi token error, so the host
// can tell the caller which check failed.
func tokenError(err error) error {
	switch {
	case errors.Is(err, oidctoken.ErrMalformed):
		return fmt.Errorf("%w: %w", authapi.ErrTokenMalformed, err)
	case errors.Is(err, oidctoken.ErrKeysUnavailable):
		return fmt.Errorf("%w: %w", authapi.ErrTokenKeys, err)
	default:
		return fmt.Errorf("%w: %w", authapi.ErrTokenSignature, err)
	}
}

// check binds the claims to this server. A token with a valid signature can
// still be from another issuer, for another audience, or outside its window.
func (c claims) check(cfg Config, now time.Time) error {
	switch {
	case strings.TrimSuffix(c.Issuer, "/") != cfg.Issuer:
		return fmt.Errorf("%w: issued by %q", authapi.ErrTokenIssuer, c.Issuer)
	case c.Subject == "":
		return fmt.Errorf("%w: it names no subject", authapi.ErrTokenMalformed)
	case cfg.Audience != "" && !slices.Contains(c.Audience, cfg.Audience):
		return fmt.Errorf("%w: it was issued for %v", authapi.ErrTokenAudience, []string(c.Audience))
	case c.Expiry == 0 || now.After(time.Unix(c.Expiry, 0).Add(cfg.ClockSkew)):
		return fmt.Errorf("%w: it expired at %d", authapi.ErrTokenExpired, c.Expiry)
	case c.NotBefore != 0 && now.Add(cfg.ClockSkew).Before(time.Unix(c.NotBefore, 0)):
		return fmt.Errorf("%w: it is not valid before %d", authapi.ErrTokenExpired, c.NotBefore)
	case c.IssuedAt != 0 && now.Add(cfg.ClockSkew).Before(time.Unix(c.IssuedAt, 0)):
		return fmt.Errorf("%w: it was issued in the future", authapi.ErrTokenExpired)
	}

	return nil
}
