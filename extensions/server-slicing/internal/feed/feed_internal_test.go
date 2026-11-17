package feed

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/server-slicing/internal/catalog"
)

// github serves the recorded releases as two pages: the recorded ones in the
// supported range, then the older ones. It answers 304 to an ETag it gave.
type github struct {
	mu        sync.Mutex
	pages     [][]byte
	requests  []string
	notMods   int
	failWith  int
	tokenSeen string
}

func newGitHub(t *testing.T) *github {
	t.Helper()

	data, err := os.ReadFile("../../testdata/github-releases.json")
	if err != nil {
		t.Fatal(err)
	}

	var all []json.RawMessage
	if err = json.Unmarshal(data, &all); err != nil {
		t.Fatal(err)
	}

	first, _ := json.Marshal(all[:len(all)-1])
	second, _ := json.Marshal(all[len(all)-1:])

	return &github{pages: [][]byte{first, second, []byte("[]")}}
}

func (g *github) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.requests = append(g.requests, r.URL.RawQuery)
	g.tokenSeen = r.Header.Get("Authorization")

	if g.failWith != 0 {
		w.Header().Set("X-Ratelimit-Reset", "1790000000")
		w.WriteHeader(g.failWith)

		return
	}

	number := 1
	if strings.Contains(r.URL.RawQuery, "page=2") {
		number = 2
	} else if strings.Contains(r.URL.RawQuery, "page=3") {
		number = 3
	}

	etag := `"page-` + string(rune('0'+number)) + `"`
	if r.Header.Get("If-None-Match") == etag {
		g.notMods++
		w.WriteHeader(http.StatusNotModified)

		return
	}

	if number < len(g.pages) {
		w.Header().Set("Link", `<https://api.github.com/x?page=`+string(rune('1'+number))+`>; rel="next"`)
	}

	w.Header().Set("ETag", etag)
	_, _ = w.Write(g.pages[number-1])
}

func TestTheListIsReadAndKeptWithItsETags(t *testing.T) {
	t.Parallel()

	server := &github{}
	*server = *newGitHub(t)
	host := httptest.NewServer(server)
	t.Cleanup(host.Close)

	dir := t.TempDir()
	clock := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)

	f := Open(dir, host.URL, host.Client(), "secret-token")
	f.now = func() time.Time { return clock }

	if !f.Stale() {
		t.Fatal("a feed never read is not stale")
	}

	if err := f.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}

	releases, fetched, err := f.Releases()
	if err != nil || len(releases) != 9 || !fetched.Equal(clock) {
		t.Fatalf("read %d releases at %s, %v", len(releases), fetched, err)
	}

	// The second page holds 2.6.1 alone, below the range, so the reading
	// stops there and asks for no third page.
	if len(server.requests) != 2 || server.tokenSeen != "Bearer secret-token" {
		t.Fatalf("asked %v with %q", server.requests, server.tokenSeen)
	}

	if err = f.Refresh(t.Context()); !errors.Is(err, ErrTooSoon) {
		t.Fatalf("a refresh at once answered %v", err)
	}

	clock = clock.Add(RefreshEvery)
	if !f.Stale() {
		t.Fatal("a copy six hours old is not stale")
	}

	if err = f.Refresh(t.Context()); err != nil || server.notMods != 2 {
		t.Fatalf("the second refresh answered %v with %d not-modified pages", err, server.notMods)
	}

	// A new module process reads the copy from disk.
	again := Open(dir, host.URL, host.Client(), "")
	if releases, _, _ = again.Releases(); len(releases) != 9 {
		t.Fatalf("the copy on disk holds %d releases", len(releases))
	}

	offered, _ := catalog.ReadGitHub(releases, "darwin", "arm64")
	if len(offered) == 0 {
		t.Fatal("the copy offers nothing")
	}
}

// When GitHub refuses, the copy stays and the reason says what to do.
func TestARefusedRefreshKeepsTheCopy(t *testing.T) {
	t.Parallel()

	server := newGitHub(t)
	host := httptest.NewServer(server)
	t.Cleanup(host.Close)

	clock := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)

	f := Open(t.TempDir(), host.URL, host.Client(), "")
	f.now = func() time.Time { return clock }

	if err := f.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}

	server.mu.Lock()
	server.failWith = http.StatusForbidden
	server.mu.Unlock()

	clock = clock.Add(time.Hour)

	err := f.Refresh(t.Context())
	if err == nil || !strings.Contains(err.Error(), "github_token") {
		t.Fatalf("a refused refresh answered %v", err)
	}

	releases, _, last := f.Releases()
	if len(releases) != 9 || last == nil {
		t.Fatalf("the copy holds %d releases after a refusal, and the reason is %v", len(releases), last)
	}
}
