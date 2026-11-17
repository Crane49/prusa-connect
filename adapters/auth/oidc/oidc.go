// Package oidc authenticates people against an OpenID Connect issuer.
//
// It is a reference implementation of sdk/authapi, meant to be copied. An
// author writing a SAML or LDAP provider replaces BeginLogin and CompleteLogin.
//
// It follows OAuth 2.1: the authorization code flow with PKCE, S256 only.
//
// The provider is stateless. What one login needs between the redirect and the
// callback travels in the providerState blob the host persists, so a restart
// or a second instance still completes the login.
package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/authapi"
	"github.com/prusa3d/prusa-connect/sdk/authapi/oidctoken"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// Type is the `type` of an identity resource that selects this adapter.
const Type = "oidc"

// challengeMethod is S256. The plain method puts the verifier in the authorize
// request, where anything that sees the redirect can replay it.
const challengeMethod = "S256"

// The random bytes behind one login. RFC 7636 recommends a 32-byte verifier.
const (
	verifierBytes = 32
	nonceBytes    = 16
)

// ErrCallback reports a callback this adapter cannot complete a login from.
var ErrCallback = errors.New("oidc: the callback carries no usable authorization code")

//nolint:gochecknoinits // registration has to happen before main; the host reaches it through a blank import.
func init() { authapi.Register(Type, doc(), New) }

// doc is this provider's entry in the published extension reference.
func doc() extension.Doc {
	return extension.Doc{
		Summary: "Authenticates people against an OpenID Connect issuer with the authorization code flow.",
		Description: "OAuth 2.1: authorization code with PKCE, S256 only, and an ID token whose signature " +
			"is checked before any claim in it is read: with an algorithm the issuer advertises, against " +
			"its published keys, or against the client secret for HS256. " +
			"The provider holds no state between the redirect and the callback, so a restart or a second " +
			"instance still completes a login. It re-checks nothing after login: an ID token is a statement " +
			"about one moment, and re-checking a subject needs a token this provider is not given.",
		Version:   "1.0.0",
		Contract:  authapi.Contract,
		Stability: extension.StabilityStable,
		Options: []extension.Option{
			{
				Key:      "issuer",
				Type:     extension.OptionURL,
				Required: true,
				Summary:  "The issuer, whose metadata is read from its .well-known/openid-configuration.",
			},
			{
				Key:      "client_id",
				Type:     extension.OptionString,
				Required: true,
				Summary:  "The client id this server is registered under at the issuer.",
			},
			{
				Key:     "client_secret",
				Type:    extension.OptionSecret,
				Summary: "The client secret, for a confidential client. A public client with PKCE needs none.",
			},
			{
				Key:     "scopes",
				Type:    extension.OptionList,
				Default: "openid profile email",
				Summary: "The scopes asked for at the authorize endpoint.",
			},
			{
				Key:     "role_claim",
				Type:    extension.OptionString,
				Default: "roles",
				Summary: "The ID token claim the granted roles are read from.",
			},
			{
				Key:     "timeout",
				Type:    extension.OptionDuration,
				Default: "10s",
				Summary: "How long one call to the issuer waits.",
			},
		},
	}
}

// Provider implements authapi.IdentityProvider.
type Provider struct {
	config    Config
	client    *http.Client
	discovery *discovery

	keyMu  sync.Mutex
	keySet *oidctoken.KeySet
}

// New builds a Provider from its spec. It does not contact the issuer, so an
// issuer that is down does not stop this server from starting.
func New(spec authapi.ProviderSpec) (authapi.IdentityProvider, error) {
	cfg, err := configFrom(spec.Config)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: cfg.Timeout}

	return &Provider{
		config:    cfg,
		client:    client,
		discovery: &discovery{issuer: cfg.Issuer, client: client},
	}, nil
}

// Name is the provider type an operator writes in configuration.
func (p *Provider) Name() string { return Type }

// Capabilities reports the redirect flow only. Revalidation is not offered:
// RefreshIdentity gets a subject and no token, and an issuer answers a
// question about a subject only to a caller with a credential for it.
func (p *Provider) Capabilities() authapi.Capabilities {
	return authapi.Capabilities{Redirect: true}
}

// VerifyCredentials is unsupported. OAuth 2.1 removes the password grant.
func (p *Provider) VerifyCredentials(context.Context, string, string) (authapi.Identity, error) {
	return authapi.Identity{}, authapi.ErrUnsupportedFlow
}

// RefreshIdentity is unsupported, for the reason Capabilities gives.
func (p *Provider) RefreshIdentity(context.Context, string) (authapi.Identity, error) {
	return authapi.Identity{}, authapi.ErrUnsupportedFlow
}

// login is what one authorize request needs at its callback. The host persists
// it as the providerState blob. It must not reach the browser: the verifier
// proves the callback belongs to the client that started the login.
type login struct {
	Verifier    string `json:"v"`
	Nonce       string `json:"n"`
	RedirectURI string `json:"r"`
}

// BeginLogin builds the authorize URL to send the browser to. The host
// generates the state, not this provider.
func (p *Provider) BeginLogin(ctx context.Context, redirectURI, state string) (string, string, error) {
	meta, err := p.discovery.get(ctx)
	if err != nil {
		return "", "", err
	}

	verifier, err := randomText(verifierBytes)
	if err != nil {
		return "", "", err
	}

	nonce, err := randomText(nonceBytes)
	if err != nil {
		return "", "", err
	}

	challenge := sha256.Sum256([]byte(verifier))

	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {p.config.ClientID},
		"redirect_uri":          {redirectURI},
		"scope":                 {strings.Join(p.config.Scopes, " ")},
		"state":                 {state},
		"nonce":                 {nonce},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {challengeMethod},
	}

	authorize := meta.Authorization
	if strings.Contains(authorize, "?") {
		authorize += "&" + query.Encode()
	} else {
		authorize += "?" + query.Encode()
	}

	blob, err := json.Marshal(login{Verifier: verifier, Nonce: nonce, RedirectURI: redirectURI})
	if err != nil {
		return "", "", fmt.Errorf("oidc: encode the login state: %w", err)
	}

	return authorize, base64.RawURLEncoding.EncodeToString(blob), nil
}

// CompleteLogin finishes a login from the issuer's callback. The redirect URI
// sent to the token endpoint is the one from BeginLogin. Deriving it from the
// callback would let whatever served the callback choose it.
func (p *Provider) CompleteLogin(
	ctx context.Context, params url.Values, providerState string,
) (authapi.Identity, error) {
	if refusal := params.Get("error"); refusal != "" {
		return authapi.Identity{}, fmt.Errorf("%w: the issuer refused the login with %q", ErrCallback, refusal)
	}

	code := params.Get("code")
	if code == "" {
		return authapi.Identity{}, ErrCallback
	}

	started, err := decodeLogin(providerState)
	if err != nil {
		return authapi.Identity{}, err
	}

	meta, err := p.discovery.get(ctx)
	if err != nil {
		return authapi.Identity{}, err
	}

	raw, err := p.exchange(ctx, meta, code, started.Verifier, started.RedirectURI)
	if err != nil {
		return authapi.Identity{}, err
	}

	verified, err := p.verify(ctx, meta, raw, started.Nonce, time.Now())
	if err != nil {
		return authapi.Identity{}, err
	}

	return authapi.Identity{
		Subject:     verified.Subject,
		Email:       verified.Email,
		DisplayName: displayName(verified),
		Roles:       verified.roles(p.config.RoleClaim),
	}, nil
}

func decodeLogin(providerState string) (login, error) {
	if providerState == "" {
		return login{}, fmt.Errorf("%w: no login was started for it", ErrCallback)
	}

	blob, err := base64.RawURLEncoding.DecodeString(providerState)
	if err != nil {
		return login{}, fmt.Errorf("%w: the login state is unreadable", ErrCallback)
	}

	var started login
	if err = json.Unmarshal(blob, &started); err != nil {
		return login{}, fmt.Errorf("%w: the login state is unreadable", ErrCallback)
	}

	// BeginLogin always writes a nonce. A state without one was rewritten to
	// remove the token's replay check.
	if started.Verifier == "" || started.RedirectURI == "" || started.Nonce == "" {
		return login{}, fmt.Errorf("%w: the login state is missing what one login needs", ErrCallback)
	}

	return started, nil
}

func displayName(c claims) string {
	return oidctoken.DisplayName(c.Name, c.Username, c.Email, c.Subject)
}

func randomText(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("oidc: read random bytes: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}
