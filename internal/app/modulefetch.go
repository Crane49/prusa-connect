package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/kernel/httporigin"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// Fetching a release over the network. A fetched release is somebody else's
// bytes. The address is held to https, and every redirect is held to the same
// rule. The body is bounded. The bytes are hashed while they are written.
// When they are not the bytes the request or the marketplace named, they are
// refused before anything unpacks them.

// The bounds a fetch runs under.
const (
	fetchTimeout    = 10 * time.Minute
	releaseTempName = "module-fetch-*.tar.gz"
	// maxRedirects is how many hops a download follows. A release on a code
	// host is one redirect to its storage, and a chain longer than this is not
	// a download.
	maxRedirects = 5
)

// What this build fetches a release over, and the separator that tells an
// address from a path.
const (
	httpsPrefix     = "https://"
	httpPrefix      = "http://"
	schemeSeparator = "://"
)

// errRedirect is a redirect this server does not follow.
var errRedirect = errors.New("the release's address redirected somewhere this server does not fetch from")

// remoteAllowed reports an address this server fetches from: https, or http to
// a loopback host. A signed storage URL carries a query, so unlike the
// marketplace's own origin this does not refuse one.
func remoteAllowed(address *url.URL) bool {
	if address.Hostname() == "" || address.User != nil {
		return false
	}

	switch address.Scheme {
	case "https":
		return true
	case "http":
		return config.IsLoopbackHost(address.Hostname())
	}

	return false
}

// checkRemoteRedirect holds every hop of a download to the rule the first
// address was held to, and to a hop limit.
func checkRemoteRedirect(next *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("%w: more than %d redirects", errRedirect, maxRedirects)
	}

	if !remoteAllowed(next.URL) {
		return fmt.Errorf("%w: %s", errRedirect, next.URL.Redacted())
	}
	if len(via) > 0 && via[0].Header.Get("Authorization") != "" &&
		!httporigin.Same(via[0].URL, next.URL) {
		return fmt.Errorf("%w: an authenticated request left its configured origin", errRedirect)
	}

	return nil
}

// releaseSource is where an install's bytes are, the digest they must have,
// and what the marketplace said about the release when it named them.
type releaseSource struct {
	address string
	digest  string
	// listed is what the marketplace lists the release as asking for, and nil
	// for a release the caller named.
	listed  *install.ModuleAsks
	channel string
	// authorized is true only when the marketplace selected this artifact and
	// its address is on the configured Beta Portal origin.
	authorized bool
}

// addressOf is where an install's bytes are and the digest they must have. A
// request that names no address installs what the marketplace offers for that
// module and version.
func (m *moduleInstaller) addressOf(ctx context.Context, release httpapi.ModuleRelease) (releaseSource, error) {
	from := strings.TrimSpace(release.From)
	if from != "" {
		return releaseSource{address: from, digest: strings.ToLower(strings.TrimSpace(release.Sha256))}, nil
	}

	if m.market == nil {
		return releaseSource{}, httpfault.RequestInvalid("body.from", "the release's address",
			"name where the release is; this server reads no marketplace it could take it from")
	}

	on, err := m.marketplaceOn()
	if err != nil {
		return releaseSource{}, err
	}

	if !on {
		return releaseSource{}, httpfault.MarketplaceDisabled(m.market.base)
	}

	offered, err := m.market.release(ctx, strings.TrimSpace(release.Module), strings.TrimSpace(release.Version))
	if err != nil {
		return releaseSource{}, err
	}

	if offered.DownloadURL == "" || offered.Sha256 == "" {
		return releaseSource{}, httpfault.ModuleNotOffered(release.Module, release.Version)
	}

	listed := offered.asks()

	source := m.marketSource(offered.DownloadURL, offered.Sha256)
	source.listed, source.channel = &listed, offered.channel()

	return source, nil
}

// marketSource is a release the marketplace selected, at its address and
// with its digest. A download is signed only when the marketplace listing it
// is the portal itself, so another marketplace cannot spend the pairing.
func (m *moduleInstaller) marketSource(address, digest string) releaseSource {
	authorized := false
	if m.market != nil {
		at, atErr := url.Parse(address)
		market, marketErr := url.Parse(m.market.base)
		authorized = atErr == nil && marketErr == nil && m.portal.matches(at) && m.portal.matches(market)
	}

	return releaseSource{
		address:    strings.TrimSpace(address),
		digest:     strings.ToLower(strings.TrimSpace(digest)),
		authorized: authorized,
	}
}

// reachable turns what the caller named into a path on this machine, and
// returns what to call when the install is over.
//
// A path is used where it is: an operator who put a release on the server meant
// that release. A URL is fetched into a file of this server's own, because
// stageRelease reads a directory or an archive and nothing about the rest of
// the install should know whether the bytes arrived over a network.
func (m *moduleInstaller) reachable(ctx context.Context, source releaseSource) (string, func(), error) {
	from := strings.TrimSpace(source.address)
	digest := source.digest

	// The two prefixes and not url.Parse's scheme. On Windows a path begins
	// with a drive letter and a colon, which parses as a scheme, so asking the
	// parser what this is answers "the c scheme" for C:\releases\spools.
	lower := strings.ToLower(from)
	if !strings.HasPrefix(lower, httpsPrefix) && !strings.HasPrefix(lower, httpPrefix) {
		if scheme, _, found := strings.Cut(from, schemeSeparator); found && len(scheme) > 1 {
			return "", nil, httpfault.RequestInvalid("body.from", "the release's address",
				"name an https URL of a "+archiveSuffix+", or a path on this server")
		}

		return from, func() {}, nil
	}

	parsed, err := url.Parse(from)
	if err != nil {
		return "", nil, httpfault.RequestInvalid("body.from", "the release's address", err.Error())
	}

	if !remoteAllowed(parsed) {
		return "", nil, httpfault.RequestInvalid("body.from", "the release's address",
			"a release is fetched over https, and only a loopback host may be reached over http")
	}

	// A fetched release has no checksum file this server trusts, so the digest
	// is what pins its bytes.
	if digest == "" {
		return "", nil, httpfault.RequestInvalid("body.sha256", "the release's digest",
			"a release fetched from a URL takes its SHA-256, so this server installs exactly those bytes")
	}

	staged, err := m.fetch(ctx, parsed, digest, source.authorized)
	if err != nil {
		return "", nil, err
	}

	return staged, func() { _ = os.Remove(staged) }, nil
}

// fetch downloads one release archive into a file this server owns, hashing it
// on the way. The body is bounded, because the file is somebody else's and a
// length header is only a claim. A digest that was stated and is not met
// removes the file and refuses the install.
func (m *moduleInstaller) fetch(
	ctx context.Context, from *url.URL, digest string, authorized bool,
) (string, error) {
	source := sourceOf(from)

	// Every failure to download answers the same fault, and only the log says
	// what happened, so the answer tells a caller nothing about the address.
	failed := func(reason string, attrs ...any) (string, error) {
		m.log.WarnContext(ctx, "a module release could not be downloaded",
			append([]any{"source", source, "reason", reason}, attrs...)...)

		return "", httpfault.ModuleDownloadFailed(source)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, from.String(), nil)
	if err != nil {
		return "", httpfault.RequestInvalid("body.from", "the release's address", err.Error())
	}
	if authorized {
		if err = m.portal.authorize(request); err != nil {
			return failed("the authorization proof could not be made")
		}
	}

	send := m.client.Do
	if authorized {
		send = func(request *http.Request) (*http.Response, error) { return m.portal.send(m.client, request) }
	}
	response, err := send(request)
	if err != nil {
		return failed("the request failed", "error", err.Error())
	}

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return failed("the address answered something other than the release", "status", response.StatusCode)
	}

	file, err := os.CreateTemp("", releaseTempName)
	if err != nil {
		return "", httpfault.Unexpected("make a file for the release")
	}

	staged := file.Name()
	sum := sha256.New()

	written, err := io.Copy(io.MultiWriter(file, sum), io.LimitReader(response.Body, maxArchiveBytes+1))

	// The close is checked here and not deferred. On FAT and on a full disk,
	// out of space is reported only by the close, and a truncated archive that
	// looked written is one this server would then unpack.
	closeErr := file.Close()

	refuse := func(raised error) (string, error) {
		_ = os.Remove(staged)

		return "", raised
	}

	switch {
	case err != nil:
		_ = os.Remove(staged)

		return failed("the body could not be read", "error", err.Error())
	case closeErr != nil:
		return refuse(httpfault.Unexpected("write the release to disk"))
	case written > maxArchiveBytes:
		return refuse(httpfault.RequestInvalid("body.from", "the release's size",
			fmt.Sprintf("a release is at most %d bytes", int64(maxArchiveBytes))))
	}

	if actual := hex.EncodeToString(sum.Sum(nil)); digest != "" && actual != digest {
		return refuse(httpfault.ModuleDigestMismatch(source, digest, actual))
	}

	return staged, nil
}

// sourceOf is a download's address as an answer and a log name it: the
// scheme, the host and the path. The query is left out, because a signed
// storage address carries its signature there.
func sourceOf(address *url.URL) string {
	return (&url.URL{Scheme: address.Scheme, Host: address.Host, Path: address.Path}).String()
}
