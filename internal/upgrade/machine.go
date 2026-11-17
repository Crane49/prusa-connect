package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/internal/kernel/httporigin"
)

const (
	// MachineCatalogPath is the versioned catalog endpoint below the configured
	// core.upgrade.server base URL.
	MachineCatalogPath = "/api/connect-local/v1/core/releases"

	defaultMachineCatalogTimeout  = 30 * time.Second
	defaultMachineDownloadTimeout = 10 * time.Minute
	defaultMachineCatalogMaxBytes = 4 << 20
	maxMachineRedirects           = 5
	machineCatalogSchema          = 1
)

var ErrMachineRelease = errors.New("upgrade: machine release request failed")

// ErrMachineRefused reports an update server that refused this server's
// credential with 401 or 403: it is not paired, or the pairing was revoked.
var ErrMachineRefused = errors.New("upgrade: the update server refused this server's pairing")

// ReleaseSource discovers and downloads releases. MachineClient and the
// GitHub ReleaseClient both satisfy it.
type ReleaseSource interface {
	List(context.Context, string) ([]Release, error)
	Download(context.Context, Asset, io.Writer) error
}

// MachineClient reads the generic machine update API served below BaseURL.
// Authorize signs every request, including each same-origin redirect.
// Rejected sees each 401 answer to a signed request and says whether to sign
// it again and send it once more. An error it returns is the request's.
type MachineClient struct {
	BaseURL         string
	Client          *http.Client
	Authorize       func(*http.Request) error
	Rejected        func(*http.Response) (bool, error)
	MaxBytes        int64
	CatalogMaxBytes int64
	AssetName       string
}

// portalChannel is what the portal calls the channel of every build, alpha,
// beta and stable alike. The beta offers no stable channel of its own.
const portalChannel = "beta"

type machineCatalog struct {
	Schema   int       `json:"schema"`
	Releases []Release `json:"releases"`
}

func (c MachineClient) List(ctx context.Context, current string) ([]Release, error) {
	base, endpoint, err := c.address()
	if err != nil {
		return nil, err
	}

	assetName := c.AssetName
	if assetName == "" {
		assetName = ReleaseBinaryName("normal", "full")
	}

	query := endpoint.Query()
	query.Set("current", current)
	query.Set("channel", portalChannel)
	query.Set("platform", Platform())
	query.Set("asset", assetName)
	endpoint.RawQuery = query.Encode()

	catalog, err := c.fetchCatalog(ctx, base, endpoint)
	if err != nil {
		return nil, err
	}

	return compatibleMachineReleases(base, catalog.Releases, current, assetName), nil
}

func (c MachineClient) fetchCatalog(
	ctx context.Context, base, endpoint *url.URL,
) (machineCatalog, error) {
	requestCtx, cancel := context.WithTimeout(ctx, defaultMachineCatalogTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return machineCatalog{}, fmt.Errorf("%w: build catalog request: %w", ErrMachineRelease, err)
	}
	request.Header.Set("Accept", "application/json")
	if err = c.authorize(request, base); err != nil {
		return machineCatalog{}, fmt.Errorf("%w: authorize catalog request: %w", ErrMachineRelease, err)
	}

	response, err := c.send(base, request)
	if err != nil {
		return machineCatalog{}, fmt.Errorf("%w: read catalog: %w", ErrMachineRelease, err)
	}
	defer func() { _ = response.Body.Close() }()

	if err = limitedResponse(response, time.Now()); err != nil {
		return machineCatalog{}, fmt.Errorf("%w: catalog: %w", ErrMachineRelease, err)
	}
	if refused(response) {
		return machineCatalog{}, fmt.Errorf("%w: %w: catalog answered %s", ErrMachineRelease, ErrMachineRefused,
			response.Status)
	}
	if response.StatusCode != http.StatusOK {
		return machineCatalog{}, fmt.Errorf("%w: catalog answered %s", ErrMachineRelease, response.Status)
	}

	limit := c.CatalogMaxBytes
	if limit <= 0 {
		limit = defaultMachineCatalogMaxBytes
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return machineCatalog{}, fmt.Errorf("%w: read catalog body: %w", ErrMachineRelease, err)
	}
	if int64(len(content)) > limit {
		return machineCatalog{}, fmt.Errorf("%w: catalog exceeds %d bytes", ErrMachineRelease, limit)
	}

	var catalog machineCatalog
	if err = json.Unmarshal(content, &catalog); err != nil {
		return machineCatalog{}, fmt.Errorf("%w: decode catalog: %w", ErrMachineRelease, err)
	}
	if catalog.Schema != machineCatalogSchema {
		return machineCatalog{}, fmt.Errorf("%w: catalog schema is %d, expected %d",
			ErrMachineRelease, catalog.Schema, machineCatalogSchema)
	}

	return catalog, nil
}

func compatibleMachineReleases(
	base *url.URL, catalog []Release, current, assetName string,
) []Release {
	releases := make([]Release, 0, len(catalog))
	for _, release := range catalog {
		if PermitVersion(current, release.Version) != nil {
			continue
		}
		if release.Binary.Name != assetName || release.Sums.Name == "" || release.Signature.Name == "" {
			continue
		}

		assets := []*Asset{&release.Binary, &release.Sums, &release.Signature}
		valid := true
		for _, asset := range assets {
			if err := normalizeMachineAsset(base, asset); err != nil {
				valid = false

				break
			}
		}
		if valid {
			releases = append(releases, release)
		}
	}

	return releases
}

func (c MachineClient) Download(ctx context.Context, asset Asset, destination io.Writer) error {
	base, _, err := c.address()
	if err != nil {
		return err
	}
	if err = normalizeMachineAsset(base, &asset); err != nil {
		return err
	}

	limit := c.MaxBytes
	if limit <= 0 {
		limit = DefaultMaxBinaryBytes
	}
	if asset.Size > limit {
		return fmt.Errorf("%w: asset %s exceeds %d bytes", ErrMachineRelease, asset.Name, limit)
	}

	requestCtx, cancel := context.WithTimeout(ctx, defaultMachineDownloadTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return fmt.Errorf("%w: build artifact request: %w", ErrMachineRelease, err)
	}
	request.Header.Set("Accept", "application/octet-stream")
	if err = c.authorize(request, base); err != nil {
		return fmt.Errorf("%w: authorize artifact request: %w", ErrMachineRelease, err)
	}

	response, err := c.send(base, request)
	if err != nil {
		return fmt.Errorf("%w: download %s: %w", ErrMachineRelease, asset.Name, err)
	}
	defer func() { _ = response.Body.Close() }()

	if err = limitedResponse(response, time.Now()); err != nil {
		return fmt.Errorf("%w: download %s: %w", ErrMachineRelease, asset.Name, err)
	}
	if refused(response) {
		return fmt.Errorf("%w: %w: download %s answered %s", ErrMachineRelease, ErrMachineRefused, asset.Name,
			response.Status)
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: download %s answered %s", ErrMachineRelease, asset.Name, response.Status)
	}

	read, err := io.Copy(destination, io.LimitReader(response.Body, limit+1))
	if err != nil {
		return fmt.Errorf("%w: download %s: %w", ErrMachineRelease, asset.Name, err)
	}
	if read > limit {
		return fmt.Errorf("%w: asset %s exceeds %d bytes", ErrMachineRelease, asset.Name, limit)
	}

	return nil
}

func (c MachineClient) address() (*url.URL, *url.URL, error) {
	base, err := url.Parse(strings.TrimSpace(c.BaseURL))
	if err != nil || base.Hostname() == "" || base.User != nil ||
		base.RawQuery != "" || base.Fragment != "" ||
		base.Scheme != "https" && (base.Scheme != "http" || !loopbackHost(base.Hostname())) {
		return nil, nil, fmt.Errorf("%w: core upgrade server is not an HTTPS or loopback HTTP base URL",
			ErrMachineRelease)
	}

	endpoint := *base
	endpoint.Path = strings.TrimRight(base.Path, "/") + MachineCatalogPath
	endpoint.RawPath = ""

	return base, &endpoint, nil
}

func (c MachineClient) authorize(request *http.Request, base *url.URL) error {
	request.Header.Del("Authorization")
	request.Header.Del("DPoP")
	if c.Authorize == nil || !httporigin.Same(base, request.URL) {
		return nil
	}

	return c.Authorize(request)
}

// send sends a signed request, and once more when Rejected asks for it.
func (c MachineClient) send(base *url.URL, request *http.Request) (*http.Response, error) {
	client := c.client(base)
	response, err := client.Do(request) //nolint:gosec // G704: address checked the request against the base origin
	if err != nil || response.StatusCode != http.StatusUnauthorized || c.Rejected == nil ||
		request.Header.Get("Authorization") == "" {
		return response, err
	}

	retry, err := c.Rejected(response)
	if err != nil {
		_ = response.Body.Close()

		return nil, err
	}
	if !retry {
		return response, nil
	}

	_ = response.Body.Close()
	if err = c.authorize(request, base); err != nil {
		return nil, err
	}

	return client.Do(request) //nolint:gosec // G704: the same request, signed again
}

func (c MachineClient) client(base *url.URL) *http.Client {
	var client http.Client
	if c.Client != nil {
		client = *c.Client
	}
	if client.Timeout == 0 {
		client.Timeout = defaultMachineDownloadTimeout
	}

	previous := client.CheckRedirect
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		next.Header.Del("Authorization")
		next.Header.Del("DPoP")
		if len(via) >= maxMachineRedirects {
			return fmt.Errorf("%w: more than %d redirects", ErrMachineRelease, maxMachineRedirects)
		}
		if !httporigin.Same(base, next.URL) {
			return fmt.Errorf("%w: redirect left the configured origin", ErrMachineRelease)
		}
		if previous != nil {
			if err := previous(next, via); err != nil {
				return err
			}
		}

		if err := c.authorize(next, base); err != nil {
			return fmt.Errorf("%w: authorize redirect: %w", ErrMachineRelease, err)
		}

		return nil
	}

	return &client
}

func normalizeMachineAsset(base *url.URL, asset *Asset) error {
	if strings.TrimSpace(asset.Name) == "" || asset.Size < 0 {
		return fmt.Errorf("%w: catalog contains an invalid artifact", ErrMachineRelease)
	}

	address, err := url.Parse(strings.TrimSpace(asset.URL))
	if err != nil {
		return fmt.Errorf("%w: artifact %s has an invalid URL", ErrMachineRelease, asset.Name)
	}
	address = base.ResolveReference(address)
	if address.User != nil || !httporigin.Same(base, address) {
		return fmt.Errorf("%w: artifact %s is outside the configured origin", ErrMachineRelease, asset.Name)
	}
	asset.URL = address.String()

	return nil
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}

func refused(response *http.Response) bool {
	return response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden
}
