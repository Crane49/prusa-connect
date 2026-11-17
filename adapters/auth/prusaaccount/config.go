package prusaaccount

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	// defaultClockSkew is how far a token's timestamps may be off before it is
	// refused.
	defaultClockSkew = 2 * time.Minute
	// defaultTimeout bounds one fetch of the key set.
	defaultTimeout = 10 * time.Second
)

// A broken spec fails at build time, not on the first request.
var (
	// ErrMissingIssuer rejects a spec with no issuer.
	ErrMissingIssuer = errors.New("prusa-account: config.issuer is required")
	// ErrMissingJWKSURL rejects a spec with nowhere to read the signing keys.
	ErrMissingJWKSURL = errors.New("prusa-account: config.jwks_url is required")
	// ErrBadDuration rejects a duration that does not parse or is negative.
	ErrBadDuration = errors.New("prusa-account: a duration is not one this adapter can read")
)

// Config is what the operator writes under an identity resource's config.
type Config struct {
	Issuer    string
	JWKSURL   string
	Audience  string
	ClockSkew time.Duration
	Timeout   time.Duration
}

func configFrom(raw map[string]any) (Config, error) {
	cfg := Config{ClockSkew: defaultClockSkew, Timeout: defaultTimeout}

	issuer, err := absoluteURL(raw["issuer"], ErrMissingIssuer)
	if err != nil {
		return Config{}, err
	}

	// A trailing slash would fail the issuer comparison in the token.
	cfg.Issuer = strings.TrimSuffix(issuer, "/")

	if cfg.JWKSURL, err = absoluteURL(raw["jwks_url"], ErrMissingJWKSURL); err != nil {
		return Config{}, err
	}

	cfg.Audience, _ = raw["audience"].(string)

	if cfg.ClockSkew, err = durationFrom(raw["clock_skew"], defaultClockSkew); err != nil {
		return Config{}, fmt.Errorf("config.clock_skew: %w", err)
	}

	if cfg.Timeout, err = durationFrom(raw["timeout"], defaultTimeout); err != nil {
		return Config{}, fmt.Errorf("config.timeout: %w", err)
	}

	return cfg, nil
}

func absoluteURL(raw any, missing error) (string, error) {
	text, _ := raw.(string)
	if strings.TrimSpace(text) == "" {
		return "", missing
	}

	parsed, err := url.Parse(text)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("%w: %q is not an absolute URL", missing, text)
	}

	return text, nil
}

func durationFrom(raw any, fallback time.Duration) (time.Duration, error) {
	text, ok := raw.(string)
	if !ok || text == "" {
		return fallback, nil
	}

	parsed, err := time.ParseDuration(text)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrBadDuration, err)
	}

	if parsed < 0 {
		return 0, fmt.Errorf("%w: %q is negative", ErrBadDuration, text)
	}

	return parsed, nil
}
