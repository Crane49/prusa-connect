package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"
)

const (
	defaultReleaseRepository  = "prusa3d/prusa-connect"
	releaseResponseMaxBytes   = 4 << 20
	defaultReleaseHTTPTimeout = 30 * time.Second
)

var ErrRelease = errors.New("upgrade: release discovery failed")

type ReleaseClient struct {
	Repository string
	BaseURL    string
	Client     *http.Client
	MaxBytes   int64
	AssetName  string
}

type Release struct {
	Version     string    `json:"version"      doc:"The semantic release version."`
	Name        string    `json:"name"         doc:"The human-readable release name."`
	Notes       string    `json:"notes"        doc:"The release notes published with this version."`
	PublishedAt time.Time `json:"published_at" doc:"The instant GitHub published the release."`
	Prerelease  bool      `json:"prerelease"   doc:"Whether this release belongs to a prerelease channel."`
	Binary      Asset     `json:"binary"       doc:"The executable asset compatible with this server."`
	Sums        Asset     `json:"sums"         doc:"The checksum manifest asset."`
	Signature   Asset     `json:"signature"    doc:"The detached checksum signature asset."`
}

type Asset struct {
	Name string `json:"name" doc:"The release asset filename."`
	URL  string `json:"url"  doc:"The authenticated download URL for this asset."`
	Size int64  `json:"size" doc:"The asset size in bytes."`
}

func (c ReleaseClient) List(ctx context.Context, current string) ([]Release, error) {
	repository := c.Repository
	if repository == "" {
		repository = defaultReleaseRepository
	}
	if strings.Count(repository, "/") != 1 || strings.Contains(repository, "..") {
		return nil, fmt.Errorf("%w: repository %q is not owner/name", ErrRelease, repository)
	}

	base := c.BaseURL
	if base == "" {
		base = "https://api.github.com"
	}

	endpoint := strings.TrimRight(base, "/") + "/repos/" + repository + "/releases?per_page=30"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %w", ErrRelease, err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-Github-Api-Version", "2022-11-28")

	response, err := c.client().Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRelease, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: GitHub answered %s", ErrRelease, response.Status)
	}

	var payload []githubRelease
	decoder := json.NewDecoder(io.LimitReader(response.Body, releaseResponseMaxBytes))
	if err = decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: decode response: %w", ErrRelease, err)
	}

	binaryName := c.AssetName
	if binaryName == "" {
		binaryName = ReleaseBinaryName("normal", "full")
	}

	return compatibleReleases(payload, current, binaryName), nil
}

func compatibleReleases(
	payload []githubRelease,
	current string,
	binaryName string,
) []Release {
	var releases []Release

	for _, item := range payload {
		if item.Draft || !strings.HasPrefix(item.TagName, "v") {
			continue
		}

		version := strings.TrimPrefix(item.TagName, "v")
		if PermitVersion(current, version) != nil {
			continue
		}

		release := Release{
			Version: version, Name: item.Name, Notes: item.Body,
			PublishedAt: item.PublishedAt, Prerelease: item.Prerelease,
		}
		for _, asset := range item.Assets {
			converted := Asset(asset)
			switch asset.Name {
			case binaryName:
				release.Binary = converted
			case "SHA256SUMS":
				release.Sums = converted
			case "SHA256SUMS.sig":
				release.Signature = converted
			}
		}

		if release.Binary.Name != "" && release.Sums.Name != "" && release.Signature.Name != "" {
			releases = append(releases, release)
		}
	}

	return releases
}

func (c ReleaseClient) Download(ctx context.Context, asset Asset, destination io.Writer) error {
	parsed, err := url.Parse(asset.URL)
	if err != nil || parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("%w: asset %s has an unusable URL", ErrRelease, asset.Name)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return fmt.Errorf("%w: build download: %w", ErrRelease, err)
	}

	response, err := c.client().Do(request)
	if err != nil {
		return fmt.Errorf("%w: download %s: %w", ErrRelease, asset.Name, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: download %s answered %s", ErrRelease, asset.Name, response.Status)
	}

	limit := c.MaxBytes
	if limit <= 0 {
		limit = DefaultMaxBinaryBytes
	}
	read, err := io.Copy(destination, io.LimitReader(response.Body, limit+1))
	if err != nil {
		return fmt.Errorf("%w: download %s: %w", ErrRelease, asset.Name, err)
	}
	if read > limit {
		return fmt.Errorf("%w: asset %s exceeds %d bytes", ErrRelease, asset.Name, limit)
	}

	return nil
}

func (c ReleaseClient) client() *http.Client {
	if c.Client != nil {
		return c.Client
	}

	return &http.Client{Timeout: defaultReleaseHTTPTimeout}
}

type githubRelease struct {
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	Body        string        `json:"body"`
	Draft       bool          `json:"draft"`
	Prerelease  bool          `json:"prerelease"`
	PublishedAt time.Time     `json:"published_at"`
	Assets      []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// ReleaseBinaryName returns the exact release asset compatible with this
// process profile and variant.
func ReleaseBinaryName(profile, variant string) string {
	if profile == "printer" {
		if runtime.GOOS == "linux" && runtime.GOARCH == "arm64" {
			return "prusa_connect_printer_aarch64"
		}

		return ""
	}

	if variant == "minimal" {
		switch runtime.GOOS + "/" + runtime.GOARCH {
		case "linux/amd64":
			return "prusa_connect_minimal_amd64"
		case "linux/arm64":
			return "prusa_connect_minimal_aarch64"
		case "linux/arm":
			return "prusa_connect_minimal_" + armName()
		default:
			return ""
		}
	}

	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64":
		return "prusa_connect_amd64"
	case "linux/arm64":
		return "prusa_connect_aarch64"
	case "linux/arm":
		return "prusa_connect_" + armName()
	case "darwin/amd64":
		return "prusa_connect_macos_amd64"
	case "darwin/arm64":
		return "prusa_connect_macos_arm64"
	case "windows/amd64":
		return "prusa_connect_windows_amd64.exe"
	case "windows/arm64":
		return "prusa_connect_windows_arm64.exe"
	default:
		return ""
	}
}

func releaseBinaryName() string {
	return ReleaseBinaryName("normal", "full")
}
