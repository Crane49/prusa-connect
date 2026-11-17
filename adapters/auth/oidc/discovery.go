package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// discoveryPath is fixed by OpenID Connect Discovery 1.0. A configurable path
// could point discovery at metadata the issuer did not publish.
const discoveryPath = "/.well-known/openid-configuration"

// maxDocumentBytes bounds every response read from the issuer.
const maxDocumentBytes = 1 << 20

// ErrDiscovery reports metadata that could not be fetched or that describes a
// different issuer than the one asked for.
var ErrDiscovery = errors.New("oidc: issuer discovery failed")

// metadata is the part of the discovery document a login needs.
type metadata struct {
	Issuer        string `json:"issuer"`
	Authorization string `json:"authorization_endpoint"`
	Token         string `json:"token_endpoint"`
	JWKS          string `json:"jwks_uri"`
	// Algorithms are the ID token signing algorithms the issuer advertises.
	Algorithms []string `json:"id_token_signing_alg_values_supported"`
}

// check validates a document before use. The issuer must be the one asked for,
// or a login could be redirected to an issuer nobody configured.
func (m metadata) check(asked string) error {
	switch {
	case m.Issuer == "":
		return fmt.Errorf("%w: the document names no issuer", ErrDiscovery)
	case strings.TrimSuffix(m.Issuer, "/") != strings.TrimSuffix(asked, "/"):
		return fmt.Errorf("%w: %s publishes metadata for %s", ErrDiscovery, asked, m.Issuer)
	case m.Authorization == "":
		return fmt.Errorf("%w: %s publishes no authorization_endpoint", ErrDiscovery, asked)
	case m.Token == "":
		return fmt.Errorf("%w: %s publishes no token_endpoint", ErrDiscovery, asked)
	case m.JWKS == "":
		return fmt.Errorf("%w: %s publishes no jwks_uri", ErrDiscovery, asked)
	}

	return nil
}

// discovery fetches the issuer's metadata on the first login and keeps it. A
// failed fetch is not cached, so the next login tries again.
type discovery struct {
	issuer string
	client *http.Client

	mu     sync.Mutex
	loaded *metadata
}

func (d *discovery) get(ctx context.Context) (metadata, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.loaded != nil {
		return *d.loaded, nil
	}

	fetched, err := fetchMetadata(ctx, d.client, d.issuer)
	if err != nil {
		return metadata{}, err
	}

	d.loaded = &fetched

	return fetched, nil
}

func fetchMetadata(ctx context.Context, client *http.Client, issuer string) (metadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, issuer+discoveryPath, nil)
	if err != nil {
		return metadata{}, fmt.Errorf("%w: %w", ErrDiscovery, err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return metadata{}, fmt.Errorf("%w: %w", ErrDiscovery, err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return metadata{}, fmt.Errorf("%w: %s answered %s", ErrDiscovery, issuer, resp.Status)
	}

	var found metadata
	if err = json.NewDecoder(io.LimitReader(resp.Body, maxDocumentBytes)).Decode(&found); err != nil {
		return metadata{}, fmt.Errorf("%w: %w", ErrDiscovery, err)
	}

	if err = found.check(issuer); err != nil {
		return metadata{}, err
	}

	return found, nil
}
