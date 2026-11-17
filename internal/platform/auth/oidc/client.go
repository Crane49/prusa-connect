package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/authapi/oidctoken"
)

// Errors reported by a token exchange.
var (
	// ErrExchangeRefused reports an issuer that would not trade the code for a
	// token. A wrong PKCE verifier lands here.
	ErrExchangeRefused = errors.New("oidc: authorization code exchange refused")
	// ErrNoIDToken reports a token response with no id_token, which means the
	// issuer did not authenticate anybody.
	ErrNoIDToken = errors.New("oidc: token response carries no id_token")
	// ErrIncompleteConfig reports a Config missing something with no default.
	ErrIncompleteConfig = errors.New("oidc: incomplete configuration")
)

// maxTokenBytes bounds what a token response can make this process allocate.
const maxTokenBytes = 1 << 20

// defaultTimeout bounds a call to the issuer. session.Require is fail-closed,
// so a login that cannot reach the issuer must fail. An unbounded wait would
// hang instead of refusing.
const defaultTimeout = 10 * time.Second

// Config is what a client needs to run a login.
type Config struct {
	// Issuer is the identifier the discovery document must agree with.
	Issuer string
	// ClientID is this server's registration at the issuer.
	ClientID string
	// ClientSecret is optional. A public client running PKCE has none, which is
	// the shape a browser login uses.
	ClientSecret string
	// RedirectURI is compared exactly by the issuer and sent again at exchange.
	RedirectURI string
	// Scopes defaults to openid, profile, email.
	Scopes []string
	// HTTPClient defaults to one with a bounded timeout.
	HTTPClient *http.Client
}

// Client runs the authorization code flow against one issuer.
type Client struct {
	meta   Metadata
	config Config
	policy oidctoken.Policy
	http   *http.Client
	scopes string
	nowFn  func() time.Time
}

// Tokens is a successful exchange. The access and refresh tokens belong to the
// issuer and this server presents them nowhere. A caller carries a credential
// this server minted instead.
type Tokens struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

// New discovers the issuer and returns a client. An unreachable issuer fails
// here, so a misconfigured server refuses at start-up and not at the first
// login.
func New(ctx context.Context, config Config) (*Client, error) {
	if config.Issuer == "" || config.ClientID == "" || config.RedirectURI == "" {
		return nil, fmt.Errorf("%w: issuer, client id and redirect uri are required", ErrIncompleteConfig)
	}

	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}

	scopes := config.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile", "email"}
	}

	meta, err := Discover(ctx, httpClient, config.Issuer)
	if err != nil {
		return nil, err
	}

	return &Client{
		meta:   meta,
		config: config,
		policy: oidctoken.Policy{
			Algorithms: meta.AcceptedAlgorithms(config.ClientSecret != ""),
			Keys:       oidctoken.NewKeySet(meta.JWKSURI, httpClient, nil),
			Secret:     []byte(config.ClientSecret),
		},
		http:   httpClient,
		scopes: strings.Join(scopes, " "),
		nowFn:  time.Now,
	}, nil
}

// Algorithms are the ID token signing algorithms a login accepts: those the
// issuer advertises and this server can check. Empty means no login can pass,
// which the caller says at start-up.
func (c *Client) Algorithms() []string { return slices.Clone(c.policy.Algorithms) }

// CheckAlgorithms reports why no ID token from this issuer can pass, or nil
// when one can. An authentik provider with no signing key signs with the
// client secret, and a public client has none, so every sign-in would be
// refused after the person signed in at the provider.
func (c *Client) CheckAlgorithms() error {
	if len(c.policy.Algorithms) > 0 {
		return nil
	}

	advertised := c.meta.IDTokenSigningAlgValues
	if c.config.ClientSecret == "" && slices.ContainsFunc(advertised, isHMAC) {
		return fmt.Errorf("%w: the provider signs ID tokens with %s, which is checked with the client "+
			"secret, and no client secret is set. Set the client secret, or give the provider a signing key",
			ErrUnsupportedAlgorithm, strings.Join(advertised, ", "))
	}

	return fmt.Errorf("%w: the provider signs ID tokens with %s, and this server can check none of them",
		ErrUnsupportedAlgorithm, strings.Join(advertised, ", "))
}

func isHMAC(alg string) bool { return strings.HasPrefix(alg, "HS") }

// Issuer is the identifier the discovery document published. An ID token's
// `iss` is checked against it.
func (c *Client) Issuer() string { return c.meta.Issuer }

// ConfiguredIssuer is the issuer this client was configured with, without a
// trailing slash. It is the one core.principal is mapped against, so a login
// and the first administrator grant derive the same id.
func (c *Client) ConfiguredIssuer() string { return normalIssuer(c.config.Issuer) }

// PrincipalID is the user id a subject this issuer named maps to.
func (c *Client) PrincipalID(subject string) string {
	return PrincipalID(c.config.Issuer, subject)
}

// AuthorizationURL is where the browser is sent. The verifier stays in this
// process; only its S256 challenge goes over the wire.
func (c *Client) AuthorizationURL(state, nonce string, pkce PKCE) string {
	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {c.config.ClientID},
		"redirect_uri":          {c.config.RedirectURI},
		"scope":                 {c.scopes},
		"state":                 {state},
		"nonce":                 {nonce},
		"code_challenge":        {pkce.Challenge},
		"code_challenge_method": {ChallengeMethodS256},
	}

	separator := "?"
	if strings.Contains(c.meta.AuthorizationEndpoint, "?") {
		separator = "&"
	}

	return c.meta.AuthorizationEndpoint + separator + query.Encode()
}

// Exchange trades an authorization code for tokens, proving possession of the
// verifier the challenge was derived from. An issuer that does not check the
// verifier cannot be caught here.
func (c *Client) Exchange(ctx context.Context, code string, pkce PKCE) (Tokens, error) {
	response, err := c.postCode(ctx, code, pkce)
	if err != nil {
		return Tokens{}, err
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxTokenBytes))
	if err != nil {
		return Tokens{}, fmt.Errorf("read token response: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		return Tokens{}, fmt.Errorf("%w: status %d: %s", ErrExchangeRefused, response.StatusCode, summarise(body))
	}

	var tokens Tokens
	if decodeErr := json.Unmarshal(body, &tokens); decodeErr != nil {
		return Tokens{}, fmt.Errorf("decode token response: %w", decodeErr)
	}

	if tokens.IDToken == "" {
		return Tokens{}, ErrNoIDToken
	}

	return tokens, nil
}

// postCode sends one authorization code to the token endpoint, with the
// client's secret in HTTP Basic when it has one (RFC 6749 section 2.3.1).
func (c *Client) postCode(ctx context.Context, code string, pkce PKCE) (*http.Response, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {c.config.RedirectURI},
		"client_id":     {c.config.ClientID},
		"code_verifier": {pkce.Verifier},
	}

	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.meta.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build token request: %w", err)
	}

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")

	if c.config.ClientSecret != "" {
		request.SetBasicAuth(url.QueryEscape(c.config.ClientID), url.QueryEscape(c.config.ClientSecret))
	}

	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("post %s: %w", c.meta.TokenEndpoint, err)
	}

	return response, nil
}

// VerifyIDToken checks an ID token's signature and every claim that binds it to
// this login, and returns what it says about the user.
func (c *Client) VerifyIDToken(ctx context.Context, raw, nonce string) (Claims, error) {
	return verifyIDToken(ctx, c.policy, raw, c.meta.Issuer, c.config.ClientID, nonce, c.nowFn())
}

// summarise keeps an issuer's error body short enough for a log line. It is
// never rendered to a client.
func summarise(body []byte) string {
	const limit = 256

	text := strings.TrimSpace(string(body))
	if len(text) > limit {
		return text[:limit]
	}

	return text
}
