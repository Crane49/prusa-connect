package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The edges of reading a marketplace: one that is down, one that answers rows
// this server cannot install, and many requests at once.

// A marketplace that never answered is tried once, and then left alone for a
// while, so the settings screen does not wait the whole timeout on each visit.
func TestAMarketplaceThatIsDownIsNotAskedAgainUntilTheRetry(t *testing.T) {
	t.Parallel()

	market := newFakeMarketplace(t, nil, "1.0.0")
	market.setDown(true)

	installer, _ := marketInstaller(t, market.server.URL)

	clock := time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	installer.market.now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()

		return clock
	}

	for range 3 {
		_, err := installer.Index(t.Context(), "user:operator")
		require.NoError(t, err)
	}

	require.Equal(t, 1, market.lists(), "a marketplace that is down was asked on every visit")

	mu.Lock()
	clock = clock.Add(marketplaceRetry + time.Second)
	mu.Unlock()

	market.setDown(false)

	index, err := installer.Index(t.Context(), "user:operator")
	require.NoError(t, err)
	require.Equal(t, 2, market.lists())
	require.NotEmpty(t, index.Items, "the marketplace answered after the retry and its rows were not listed")
	require.True(t, index.Marketplace.Reachable)
}

// A row is offered only when an install can take it: a module path a
// namespace derives from, a release for this platform, an address and a digest.
func TestRowsThisServerCannotInstallAreNotOffered(t *testing.T) {
	t.Parallel()

	release := func(platforms []string, sha, address string) *marketplaceRelease {
		return &marketplaceRelease{
			Version: "1.0.0", Kind: "sink", Platforms: platforms, Sha256: sha, DownloadURL: address,
		}
	}

	here := []string{thisPlatform()}
	rows := []marketplaceModule{
		{Module: "github.com/acme/good", Latest: release(here, "ab", "https://plugins.example/good.tar.gz")},
		{Module: "github.com/acme/bundle", Latest: release(nil, "ab", "https://plugins.example/bundle.tar.gz")},
		{Module: "github.com/acme/nodigest", Latest: release(here, "", "https://plugins.example/n.tar.gz")},
		{Module: "github.com/acme/noaddress", Latest: release(here, "ab", "")},
		{Module: "spools", Latest: release(here, "ab", "https://plugins.example/s.tar.gz")},
		{
			Module: "github.com/acme/elsewhere",
			Latest: release([]string{"plan9_mips"}, "ab", "https://plugins.example/e.tar.gz"),
		},
		{Module: "github.com/acme/unreleased"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"extensions": rows})
	}))
	t.Cleanup(server.Close)

	installer, _ := marketInstaller(t, server.URL)

	index, err := installer.Index(t.Context(), "user:operator")
	require.NoError(t, err)

	offered := make([]string, 0, len(index.Items))
	for _, item := range index.Items {
		offered = append(offered, item.Module)
	}

	require.Equal(t, []string{"github.com/acme/good", "github.com/acme/bundle"}, offered)
}

// Many requests at once on a slow marketplace make one first read, and the
// ones that did not wait for it answer the local rows without a second one.
func TestManyRequestsAtOnceMakeOneFirstRead(t *testing.T) {
	t.Parallel()

	var listed atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		listed.Add(1)
		time.Sleep(100 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"extensions": []marketplaceModule{}})
	}))
	t.Cleanup(server.Close)

	installer, _ := marketInstaller(t, server.URL)

	var group sync.WaitGroup
	for range 16 {
		group.Go(func() {
			_, err := installer.Index(t.Context(), "user:operator")
			require.NoError(t, err)
		})
	}

	group.Wait()

	require.Equal(t, int32(1), listed.Load(), "concurrent first reads each asked the marketplace")
}
