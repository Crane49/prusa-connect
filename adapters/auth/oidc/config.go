package oidc

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	defaultTimeout   = 10 * time.Second
	defaultRoleClaim = "roles"
)

// defaultScopes applies when the operator names no scopes.
var defaultScopes = []string{"openid", "profile", "email"}

// A broken spec fails at build time, not on the first login.
var (
	// ErrMissingIssuer rejects a spec with no issuer to discover.
	ErrMissingIssuer = errors.New("oidc: config.issuer is required")
	// ErrMissingClientID rejects a spec the issuer could not identify.
	ErrMissingClientID = errors.New("oidc: config.client_id is required")
)

// Config is what the operator writes under an identity resource's config.
type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	Scopes       []string
	RoleClaim    string
	Timeout      time.Duration
}

func configFrom(raw map[string]any) (Config, error) {
	cfg := Config{
		Scopes:    defaultScopes,
		RoleClaim: defaultRoleClaim,
		Timeout:   defaultTimeout,
	}

	issuer, _ := raw["issuer"].(string)
	if strings.TrimSpace(issuer) == "" {
		return Config{}, ErrMissingIssuer
	}

	parsed, err := url.Parse(issuer)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return Config{}, fmt.Errorf("%w: %q is not an absolute URL", ErrMissingIssuer, issuer)
	}

	// A trailing slash would double the slash in the discovery URL and fail
	// the issuer comparison in the token.
	cfg.Issuer = strings.TrimSuffix(issuer, "/")

	clientID, _ := raw["client_id"].(string)
	if clientID == "" {
		return Config{}, ErrMissingClientID
	}

	cfg.ClientID = clientID
	cfg.ClientSecret, _ = raw["client_secret"].(string)

	if scopes := stringsFrom(raw["scopes"]); len(scopes) > 0 {
		cfg.Scopes = scopes
	}

	if claim, ok := raw["role_claim"].(string); ok && claim != "" {
		cfg.RoleClaim = claim
	}

	if timeout, ok := raw["timeout"].(string); ok {
		parsedTimeout, parseErr := time.ParseDuration(timeout)
		if parseErr != nil {
			return Config{}, fmt.Errorf("oidc: config.timeout: %w", parseErr)
		}

		cfg.Timeout = parsedTimeout
	}

	return cfg, nil
}

// stringsFrom reads a list written as a YAML sequence or as one
// space-separated string.
func stringsFrom(raw any) []string {
	switch value := raw.(type) {
	case []string:
		return value
	case string:
		return strings.Fields(value)
	case []any:
		out := make([]string, 0, len(value))

		for _, item := range value {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}

		return out
	default:
		return nil
	}
}
