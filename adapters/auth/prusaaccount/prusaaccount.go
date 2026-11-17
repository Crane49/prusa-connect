// Package prusaaccount verifies a Prusa Account token a caller presents.
//
// It implements the bearer flow of sdk/authapi for the `prusa-account`
// credential kind. The token is an RS256 JSON Web Token. The adapter checks
// its signature against the issuer's published key set, checks the issuer and
// audience claims, and returns the subject. The host decides what that subject
// may do.
//
// There is no login flow. The token is verified offline. The only network call
// fetches the key set, which is cached.
package prusaaccount

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/authapi"
	"github.com/prusa3d/prusa-connect/sdk/authapi/oidctoken"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// Type is the `type` of an identity resource that selects this adapter. It is
// also the credential kind the host admits a verified token under.
const Type = "prusa-account"

//nolint:gochecknoinits // registration has to happen before main; the host reaches it through a blank import.
func init() { authapi.Register(Type, doc(), New) }

// doc is this provider's entry in the published extension reference.
func doc() extension.Doc {
	return extension.Doc{
		Summary: "Verifies a Prusa Account token a caller presents on every request.",
		Description: "The token is an RS256 JSON Web Token from Prusa Account, sent as Authorization: Bearer " +
			"or in the auth.access_token cookie. Its signature is checked offline against the issuer's " +
			"published key set, cached for two hours or for as long as the issuer's cache headers say, and " +
			"refetched once when a token names a key not held. The issuer, the audience, the expiry and the " +
			"not-before are checked; alg none and every algorithm but RS256 are refused. The subject it " +
			"yields reaches this server only through a pairing an administrator wrote at " +
			"POST /api/v1/auth/pairings. There is no login flow: a person signs in at Prusa Account.",
		Version:   "1.0.0",
		Contract:  authapi.Contract,
		Stability: extension.StabilityExperimental,
		Options: []extension.Option{
			{
				Key:      "issuer",
				Type:     extension.OptionURL,
				Required: true,
				Summary:  "The issuer the token's iss claim must name.",
			},
			{
				Key:      "jwks_url",
				Type:     extension.OptionURL,
				Required: true,
				Summary:  "Where the issuer publishes its signing keys as a JSON Web Key Set.",
			},
			{
				Key:     "audience",
				Type:    extension.OptionString,
				Summary: "The audience the token's aud claim must contain. Empty skips the check.",
			},
			{
				Key:     "clock_skew",
				Type:    extension.OptionDuration,
				Default: "2m",
				Summary: "How far a token's expiry and not-before may be out before it is refused.",
			},
			{
				Key:     "timeout",
				Type:    extension.OptionDuration,
				Default: "10s",
				Summary: "How long one fetch of the key set waits.",
			},
		},
	}
}

// Provider implements authapi.IdentityProvider and authapi.TokenVerifier.
type Provider struct {
	config Config
	keys   *oidctoken.KeySet
	now    func() time.Time
}

var (
	_ authapi.IdentityProvider = (*Provider)(nil)
	_ authapi.TokenVerifier    = (*Provider)(nil)
)

// New builds a Provider from its spec. It does not fetch the key set, so an
// issuer that is down does not stop this server from starting.
func New(spec authapi.ProviderSpec) (authapi.IdentityProvider, error) {
	cfg, err := configFrom(spec.Config)
	if err != nil {
		return nil, err
	}

	return &Provider{
		config: cfg,
		keys:   oidctoken.NewKeySet(cfg.JWKSURL, &http.Client{Timeout: cfg.Timeout}, nil),
		now:    time.Now,
	}, nil
}

// Name is the provider type an operator writes in configuration.
func (p *Provider) Name() string { return Type }

// Capabilities reports the bearer flow only. A person signs in at Prusa
// Account, so there is no redirect and no password.
func (p *Provider) Capabilities() authapi.Capabilities {
	return authapi.Capabilities{Bearer: true}
}

// VerifyCredentials is unsupported: this adapter never sees a password.
func (p *Provider) VerifyCredentials(context.Context, string, string) (authapi.Identity, error) {
	return authapi.Identity{}, authapi.ErrUnsupportedFlow
}

// BeginLogin is unsupported, for the reason Capabilities gives.
func (p *Provider) BeginLogin(context.Context, string, string) (string, string, error) {
	return "", "", authapi.ErrUnsupportedFlow
}

// CompleteLogin is unsupported, for the reason Capabilities gives.
func (p *Provider) CompleteLogin(context.Context, url.Values, string) (authapi.Identity, error) {
	return authapi.Identity{}, authapi.ErrUnsupportedFlow
}

// RefreshIdentity is unsupported. Asking the issuer about a subject needs a
// credential this adapter is not given.
func (p *Provider) RefreshIdentity(context.Context, string) (authapi.Identity, error) {
	return authapi.Identity{}, authapi.ErrUnsupportedFlow
}

// VerifyToken checks one token and returns who it names.
func (p *Provider) VerifyToken(ctx context.Context, token string) (authapi.Identity, error) {
	found, err := p.verify(ctx, token, p.now())
	if err != nil {
		return authapi.Identity{}, err
	}

	return authapi.Identity{
		Subject:     found.Subject,
		Email:       found.Email,
		DisplayName: displayName(found),
		ExpiresAt:   found.Expiry,
	}, nil
}

func displayName(c claims) string {
	return oidctoken.DisplayName(c.Name, c.Username, c.Email, c.Subject)
}
