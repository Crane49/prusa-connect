// Package feed reads the release list of prusa3d/PrusaSlicer from the GitHub
// Releases API and keeps it on disk.
//
// GitHub answers 60 requests an hour to a caller with no token, and a server
// behind one address shares them. So the list is read at most every few hours,
// each page is asked for with the ETag of the copy on disk, and an answer of
// 304 Not Modified, which costs nothing to read, keeps the copy. When GitHub
// cannot be reached the module offers what the copy on disk holds.
package feed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prusa3d/server-slicing/internal/catalog"
	"github.com/prusa3d/server-slicing/internal/install"
)

// ReleasesURL is the release list of PrusaSlicer.
const ReleasesURL = "https://api.github.com/repos/prusa3d/PrusaSlicer/releases"

// The bounds on one refresh. A page holds a hundred releases, newest first,
// and the reading stops at the first page with no release in the supported
// range, which is the second page today.
const (
	perPage       = 100
	maxPages      = 5
	maxPageBytes  = 16 << 20
	cacheFileName = "github-releases.json"
	// RefreshEvery is how old the copy may grow before it is read again.
	RefreshEvery = 6 * time.Hour
	// minGap is how soon after one request a person may ask for another.
	minGap = time.Minute
	// requestTimeout bounds one page.
	requestTimeout = 30 * time.Second
)

// ErrTooSoon reports a refresh asked for within a minute of the last one.
var ErrTooSoon = errors.New("the release list was read less than a minute ago")

type page struct {
	ETag string          `json:"etag"`
	Body json.RawMessage `json:"body"`
	// Next says GitHub named a page after this one. A 304 may carry no Link
	// header, so it is kept with the page.
	Next bool `json:"next,omitempty"`
}

type copyOnDisk struct {
	FetchedAt time.Time `json:"fetched_at"`
	Pages     []page    `json:"pages"`
}

// Feed is the release list and the copy of it on disk.
type Feed struct {
	url    string
	client *http.Client
	token  string
	path   string
	now    func() time.Time

	mu      sync.Mutex
	held    copyOnDisk
	lastErr error
	lastTry time.Time
}

// Open reads the copy kept in dir. url is ReleasesURL, or a test's server.
// token, when set, is sent as a bearer token, which raises the limit to 5000
// requests an hour.
func Open(dir, url string, client *http.Client, token string) *Feed {
	f := &Feed{url: url, client: client, token: token, path: filepath.Join(dir, cacheFileName), now: time.Now}

	if data, err := os.ReadFile(f.path); err == nil {
		var held copyOnDisk
		if json.Unmarshal(data, &held) == nil {
			f.held = held
		}
	}

	return f
}

// Releases answers the releases the copy holds, when it was read, and why the
// last refresh failed, if it did.
func (f *Feed) Releases() ([]catalog.GitHubRelease, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []catalog.GitHubRelease

	for _, held := range f.held.Pages {
		var releases []catalog.GitHubRelease
		if json.Unmarshal(held.Body, &releases) == nil {
			out = append(out, releases...)
		}
	}

	return out, f.held.FetchedAt, f.lastErr
}

// Stale reports a copy older than RefreshEvery, or none.
func (f *Feed) Stale() bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.held.FetchedAt.IsZero() || f.now().Sub(f.held.FetchedAt) >= RefreshEvery
}

// Refresh reads the list again. The copy stays as it was when a page fails.
func (f *Feed) Refresh(ctx context.Context) error {
	f.mu.Lock()
	if !f.lastTry.IsZero() && f.now().Sub(f.lastTry) < minGap {
		f.mu.Unlock()

		return ErrTooSoon
	}

	f.lastTry = f.now()
	old := f.held
	f.mu.Unlock()

	pages, err := f.read(ctx, old.Pages)

	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastErr = err
	if err != nil {
		return err
	}

	f.held = copyOnDisk{FetchedAt: f.now().UTC(), Pages: pages}

	data, err := json.Marshal(f.held)
	if err == nil {
		err = install.WriteFileAtomic(f.path, data)
	}

	if err != nil {
		f.lastErr = fmt.Errorf("keep the release list: %w", err)
	}

	return f.lastErr
}

// read asks for each page, with the ETag of the page held, until a page has
// no release in the supported range or GitHub names no next page.
func (f *Feed) read(ctx context.Context, held []page) ([]page, error) {
	var out []page

	for number := 1; number <= maxPages; number++ {
		previous := page{}
		if number <= len(held) {
			previous = held[number-1]
		}

		current, more, err := f.readPage(ctx, number, previous)
		if err != nil {
			return nil, err
		}

		out = append(out, current)

		if !more || !anySupported(current.Body) {
			break
		}
	}

	return out, nil
}

func (f *Feed) readPage(ctx context.Context, number int, previous page) (page, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	address := f.url + "?per_page=" + strconv.Itoa(perPage) + "&page=" + strconv.Itoa(number)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return page{}, false, fmt.Errorf("read the release list: %w", err)
	}

	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-Github-Api-Version", "2022-11-28")

	if previous.ETag != "" {
		request.Header.Set("If-None-Match", previous.ETag)
	}

	if f.token != "" {
		request.Header.Set("Authorization", "Bearer "+f.token)
	}

	response, err := f.client.Do(request)
	if err != nil {
		return page{}, false, fmt.Errorf("read the release list from GitHub: %w", err)
	}

	defer func() { _ = response.Body.Close() }() // the read error below is the one worth reporting

	more := strings.Contains(response.Header.Get("Link"), `rel="next"`)

	switch response.StatusCode {
	case http.StatusNotModified:
		return previous, previous.Next, nil
	case http.StatusOK:
	case http.StatusForbidden, http.StatusTooManyRequests:
		return page{}, false, fmt.Errorf("GitHub refused the release list, %s: its limit of requests is used up "+
			"until %s. A token in the github_token setting raises it", response.Status, resetOf(response))
	default:
		return page{}, false, fmt.Errorf("GitHub answered the release list with %s", response.Status)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxPageBytes+1))
	if err != nil {
		return page{}, false, fmt.Errorf("read the release list: %w", err)
	}

	if len(body) > maxPageBytes {
		return page{}, false, fmt.Errorf("a page of the release list is over %d bytes", maxPageBytes)
	}

	var releases []catalog.GitHubRelease
	if err = json.Unmarshal(body, &releases); err != nil {
		return page{}, false, fmt.Errorf("read the release list: %w", err)
	}

	// Kept as the fields this module reads, so the copy on disk stays small.
	kept, err := json.Marshal(releases)
	if err != nil {
		return page{}, false, fmt.Errorf("keep the release list: %w", err)
	}

	more = more && len(releases) > 0

	return page{ETag: response.Header.Get("ETag"), Body: kept, Next: more}, more, nil
}

// resetOf is when GitHub's limit resets, in the server's own words.
func resetOf(response *http.Response) string {
	seconds, err := strconv.ParseInt(response.Header.Get("X-Ratelimit-Reset"), 10, 64)
	if err != nil {
		return "later"
	}

	return time.Unix(seconds, 0).UTC().Format(time.RFC3339)
}

// anySupported reports a page with at least one release in the supported
// range. The list is newest first, so a page without one ends the reading.
func anySupported(body json.RawMessage) bool {
	var releases []catalog.GitHubRelease
	if json.Unmarshal(body, &releases) != nil {
		return false
	}

	for _, release := range releases {
		if version, err := catalog.ParseVersion(release.TagName); err == nil && catalog.Supported(version) {
			return true
		}
	}

	return false
}
