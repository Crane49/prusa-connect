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

// clockSkew is how far a token's timestamps may be off before it is refused.
const clockSkew = 2 * time.Minute

var (
	// ErrToken reports a token endpoint that refused the exchange.
	ErrToken = errors.New("oidc: token exchange failed")
	// ErrIDToken reports an ID token that does not verify.
	ErrIDToken = errors.New("oidc: id token rejected")
)

// claims is the part of an ID token this adapter reads. registered keeps every
// claim, so a role claim under a name the operator chose is reachable.
type claims struct {
	Issuer     string   `json:"iss"`
	Subject    string   `json:"sub"`
	Audience   audience `json:"aud"`
	Expiry     int64    `json:"exp"`
	IssuedAt   int64    `json:"iat"`
	Nonce      string   `json:"nonce"`
	Email      string   `json:"email"`
	Name       string   `json:"name"`
	Username   string   `json:"preferred_username"`
	registered map[string]json.RawMessage
}

// audience is one string or a list of them. OpenID Connect allows both.
type audience = oidctoken.Audience

// exchange turns an authorization code into the ID token the issuer signed.
func (p *Provider) exchange(ctx context.Context, meta metadata, code, verifier, redirectURI string) (string, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {p.config.ClientID},
		"code_verifier": {verifier},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, meta.Token,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrToken, err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	// The secret goes in the Authorization header, not the form, so a log of
	// request bodies does not record it.
	if p.config.ClientSecret != "" {
		req.SetBasicAuth(url.QueryEscape(p.config.ClientID), url.QueryEscape(p.config.ClientSecret))
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrToken, err)
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDocumentBytes))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrToken, err)
	}

	if resp.StatusCode != http.StatusOK {
		// The issuer's body is not repeated to the caller. It can contain the
		// code and the client id.
		return "", fmt.Errorf("%w: the token endpoint answered %s", ErrToken, resp.Status)
	}

	var answer struct {
		IDToken string `json:"id_token"`
	}

	if err = json.Unmarshal(body, &answer); err != nil {
		return "", fmt.Errorf("%w: %w", ErrToken, err)
	}

	if answer.IDToken == "" {
		return "", fmt.Errorf("%w: the token endpoint returned no id_token", ErrToken)
	}

	return answer.IDToken, nil
}

// verify checks an ID token's signature against the issuer's published keys,
// then every claim that binds it to this login.
func (p *Provider) verify(ctx context.Context, meta metadata, raw, nonce string, now time.Time) (claims, error) {
	payload, err := oidctoken.Verify(ctx, raw, oidctoken.Policy{
		Algorithms: oidctoken.Accepted(meta.Algorithms, p.config.ClientSecret != ""),
		Keys:       p.keys(meta.JWKS),
		Secret:     []byte(p.config.ClientSecret),
	})
	if err != nil {
		return claims{}, fmt.Errorf("%w: %w", ErrIDToken, err)
	}

	var found claims
	if err = json.Unmarshal(payload, &found); err != nil {
		return claims{}, fmt.Errorf("%w: payload: %w", ErrIDToken, err)
	}

	_ = json.Unmarshal(payload, &found.registered)

	return found, found.check(p.config, nonce, now)
}

// check binds the claims to this login. A token with a valid signature can
// still be replayed from another client or another login, or be expired.
func (c claims) check(cfg Config, nonce string, now time.Time) error {
	switch {
	case strings.TrimSuffix(c.Issuer, "/") != cfg.Issuer:
		return fmt.Errorf("%w: issued by %q", ErrIDToken, c.Issuer)
	case c.Subject == "":
		return fmt.Errorf("%w: it names no subject", ErrIDToken)
	case !slices.Contains(c.Audience, cfg.ClientID):
		return fmt.Errorf("%w: it was issued for another client", ErrIDToken)
	case c.Expiry == 0 || now.After(time.Unix(c.Expiry, 0).Add(clockSkew)):
		return fmt.Errorf("%w: it expired", ErrIDToken)
	case c.IssuedAt != 0 && now.Add(clockSkew).Before(time.Unix(c.IssuedAt, 0)):
		return fmt.Errorf("%w: it was issued in the future", ErrIDToken)
	case c.Nonce != nonce:
		// The comparison is unconditional. Skipping it for an empty nonce
		// would accept a login state with the nonce removed.
		return fmt.Errorf("%w: it answers a different login", ErrIDToken)
	}

	return nil
}

// roles reads the role claim the operator named, as a list of strings or one
// space-separated string. Any other shape gives no roles, not an error, so an
// unexpected claim does not lock everybody out.
func (c claims) roles(claim string) []string {
	raw, ok := c.registered[claim]
	if !ok {
		return nil
	}

	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list
	}

	var single string
	if err := json.Unmarshal(raw, &single); err == nil && single != "" {
		return strings.Fields(single)
	}

	return nil
}

// keys returns the issuer's signing keys. The set is rebuilt when discovery
// names a different URI.
func (p *Provider) keys(uri string) *oidctoken.KeySet {
	p.keyMu.Lock()
	defer p.keyMu.Unlock()

	if p.keySet == nil || p.keySet.URI() != uri {
		p.keySet = oidctoken.NewKeySet(uri, p.client, nil)
	}

	return p.keySet
}
