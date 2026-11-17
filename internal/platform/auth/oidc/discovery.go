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

	"github.com/prusa3d/prusa-connect/sdk/authapi/oidctoken"
)

// Errors reported while reading an issuer's metadata.
var (
	// ErrIssuerMismatch reports a discovery document whose issuer is not the
	// one that was asked for. OpenID Connect Discovery section 4.3 requires the
	// comparison, and skipping it opens a mix-up attack.
	ErrIssuerMismatch = errors.New("oidc: discovery document issuer does not match")
	// ErrIncompleteMetadata reports a document missing an endpoint this client
	// needs.
	ErrIncompleteMetadata = errors.New("oidc: discovery document is incomplete")
	// ErrNoS256 reports an issuer that does not offer S256, which OAuth 2.1
	// requires and this client will not downgrade from.
	ErrNoS256 = errors.New("oidc: issuer does not support S256 code challenges")
)

// discoveryPath is fixed by OpenID Connect Discovery 1.0 section 4.
const discoveryPath = "/.well-known/openid-configuration"

// maxMetadataBytes bounds what an issuer can make this process allocate.
const maxMetadataBytes = 1 << 20

// Metadata is the part of an issuer's discovery document this client uses.
type Metadata struct {
	Issuer                        string   `json:"issuer"`
	AuthorizationEndpoint         string   `json:"authorization_endpoint"`
	TokenEndpoint                 string   `json:"token_endpoint"`
	JWKSURI                       string   `json:"jwks_uri"`
	UserinfoEndpoint              string   `json:"userinfo_endpoint"`
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`
	IDTokenSigningAlgValues       []string `json:"id_token_signing_alg_values_supported"`
}

// AcceptedAlgorithms are the ID token signing algorithms to accept from this
// issuer: those it advertises and this server can check, with an HMAC one
// only when there is a client secret to check it with.
func (m Metadata) AcceptedAlgorithms(haveSecret bool) []string {
	return oidctoken.Accepted(m.IDTokenSigningAlgValues, haveSecret)
}

// Discover fetches and checks an issuer's metadata. The issuer it returns is
// the document's own, and an ID token's `iss` is compared against that.
func Discover(ctx context.Context, client *http.Client, issuer string) (Metadata, error) {
	endpoint, err := url.JoinPath(strings.TrimSuffix(issuer, "/"), discoveryPath)
	if err != nil {
		return Metadata{}, fmt.Errorf("build discovery url for %q: %w", issuer, err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Metadata{}, fmt.Errorf("build discovery request: %w", err)
	}

	response, err := client.Do(request)
	if err != nil {
		return Metadata{}, fmt.Errorf("fetch %s: %w", endpoint, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return Metadata{}, fmt.Errorf("fetch %s: status %d", endpoint, response.StatusCode)
	}

	var meta Metadata
	if decodeErr := json.NewDecoder(io.LimitReader(response.Body, maxMetadataBytes)).Decode(&meta); decodeErr != nil {
		return Metadata{}, fmt.Errorf("decode %s: %w", endpoint, decodeErr)
	}

	if checkErr := meta.check(issuer); checkErr != nil {
		return Metadata{}, checkErr
	}

	return meta, nil
}

// check refuses a document missing anything this client needs.
func (m Metadata) check(asked string) error {
	if !sameIssuer(m.Issuer, asked) {
		return fmt.Errorf("%w: asked %q, document says %q", ErrIssuerMismatch, asked, m.Issuer)
	}

	if m.AuthorizationEndpoint == "" || m.TokenEndpoint == "" || m.JWKSURI == "" {
		return fmt.Errorf("%w: %q", ErrIncompleteMetadata, m.Issuer)
	}

	if !slices.Contains(m.CodeChallengeMethodsSupported, ChallengeMethodS256) {
		return fmt.Errorf("%w: offers %v", ErrNoS256, m.CodeChallengeMethodsSupported)
	}

	return nil
}

// sameIssuer compares two issuer identifiers, ignoring a trailing slash.
// Providers disagree about it, and some publish a document whose issuer carries
// one when the configured value does not.
func sameIssuer(a, b string) bool {
	return strings.TrimSuffix(a, "/") == strings.TrimSuffix(b, "/")
}
