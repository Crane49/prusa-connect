package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// ADR-20261005-64j, held against a marketplace served from this test: the index merges
// its rows after the local ones, keeps a copy, survives a marketplace that
// stops answering, and an install with no address takes the release and its
// digest from the marketplace and refuses bytes that are not those.

// thisPlatform is the platform the server under test asks the marketplace for.
func thisPlatform() string { return runtime.GOOS + "_" + runtime.GOARCH }

// fakeMarketplace serves a catalogue and the archives it names, and records
// what it was asked.
type fakeMarketplace struct {
	server *httptest.Server

	mu       sync.Mutex
	listed   int
	queries  []string
	down     bool
	archive  []byte
	digest   string
	versions []string
	// channel is the channel every release is listed on, and asks replaces
	// what the listing says the release asks for when it is set.
	channel string
	asks    *marketplaceRelease
	// missing makes the download answer 404.
	missing bool
}

func newFakeMarketplace(t *testing.T, archive []byte, versions ...string) *fakeMarketplace {
	t.Helper()

	sum := sha256.Sum256(archive)
	market := &fakeMarketplace{archive: archive, digest: hex.EncodeToString(sum[:]), versions: versions}

	// One handler and a switch, because nothing outside the sealed router
	// package mounts a ServeMux.
	prefix := "/api/connect/v1/extensions"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, prefix)

		switch {
		case r.URL.Path == prefix:
			market.list(w, r)
		case r.URL.Path == "/storage/spools.tar.gz":
			market.mu.Lock()
			missing := market.missing
			market.mu.Unlock()

			if missing {
				http.NotFound(w, r)

				return
			}

			_, _ = w.Write(market.archive)
		case strings.HasSuffix(rest, "/download"):
			// The marketplace's download is an API path that redirects to storage.
			http.Redirect(w, r, "/storage/spools.tar.gz", http.StatusFound)
		case strings.HasPrefix(r.URL.Path, prefix+"/"):
			market.detail(w, r, strings.TrimPrefix(rest, "/"))
		default:
			http.NotFound(w, r)
		}
	})

	market.server = httptest.NewServer(handler)
	t.Cleanup(market.server.Close)

	return market
}

func (f *fakeMarketplace) release(version string) marketplaceRelease {
	release := marketplaceRelease{
		Channel:     f.channel,
		Version:     version,
		Core:        ">=0.0.0",
		Kind:        "sink",
		Platforms:   []string{thisPlatform()},
		Permissions: []string{"observe"},
		HasUI:       true,
		Sha256:      f.digest,
		DownloadURL: f.server.URL + "/api/connect/v1/extensions/" + theNamespace + "/download?version=" + version,
	}

	if f.asks != nil {
		release.Permissions = f.asks.Permissions
		release.Events = f.asks.Events
		release.CheckAbsentPeople = f.asks.CheckAbsentPeople
		release.ListPeople = f.asks.ListPeople
		release.RecordsMiB = f.asks.RecordsMiB
		release.License = f.asks.License
		release.Egress = f.asks.Egress
		release.RequestHeaders = f.asks.RequestHeaders
	}

	return release
}

func (f *fakeMarketplace) row() marketplaceModule {
	latest := f.release(f.versions[len(f.versions)-1])

	row := marketplaceModule{
		ID: theNamespace, Module: theModulePath, Name: "spools", Title: "Spools",
		Summary: "Counts what each print used.",
		Author:  marketplaceAuthor{ID: "a1", Name: "Acme", Trust: "verified"},
		Trust:   "verified", PageURL: f.server.URL + "/connect/extensions/" + theNamespace,
		Latest: &latest,
	}

	for _, version := range f.versions {
		row.Versions = append(row.Versions, f.release(version))
	}

	return row
}

func (f *fakeMarketplace) list(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.listed++
	f.queries = append(f.queries, r.URL.RawQuery)
	down := f.down
	f.mu.Unlock()

	if down {
		http.Error(w, "down", http.StatusServiceUnavailable)

		return
	}

	other := marketplaceRelease{
		Version: "2.1.0", Kind: "asset", Platforms: []string{thisPlatform()},
		Sha256: "00", DownloadURL: f.server.URL + "/nozzle.tar.gz",
	}

	_ = json.NewEncoder(w).Encode(map[string]any{"extensions": []marketplaceModule{
		f.row(),
		{Module: "github.com/acme/nozzle-watch", Name: "nozzle-watch", Latest: &other},
		{Module: "github.com/acme/unbuilt", Name: "unbuilt"},
	}})
}

func (f *fakeMarketplace) detail(w http.ResponseWriter, r *http.Request, id string) {
	if id != theNamespace {
		http.NotFound(w, r)

		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{"extension": f.row()})
}

func (f *fakeMarketplace) setDown(down bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.down = down
}

func (f *fakeMarketplace) lists() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.listed
}

// marketInstaller is the installer under test, reading the fake marketplace.
func marketInstaller(t *testing.T, market string) (*moduleInstaller, string) {
	t.Helper()

	configPath, core := installConfig(t)
	core.Marketplace = config.Marketplace{URL: market, Timeout: 5 * time.Second}

	return newModuleInstaller(
		core, configPath, decidingGraph{allowed: true}, nil, nil, nil, discard()), configPath
}

func archiveBytes(t *testing.T, manifestDocument string) []byte {
	t.Helper()

	body, err := os.ReadFile(anArchiveOf(t, aRelease(t, manifestDocument, true)))
	require.NoError(t, err)

	return body
}

func TestTheIndexListsTheMarketplaceAfterTheLocalRows(t *testing.T) {
	t.Parallel()

	market := newFakeMarketplace(t, archiveBytes(t, bothHalvesManifest()), "1.0.0")
	installer, _ := marketInstaller(t, market.server.URL)

	// The local index names nozzle-watch, so the marketplace's row for it is
	// left out: the operator's own file wins.
	document, err := json.Marshal(moduleIndexFile{Modules: []moduleIndexRow{{
		Module: "github.com/acme/nozzle-watch", Name: "Nozzle watch", Version: "2.0.0",
		Source: "/srv/nozzle-watch-2.0.0.tar.gz",
	}}})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(installer.core.Modules, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(installer.core.Modules, moduleIndexName), document, 0o600))

	index, err := installer.Index(t.Context(), "user:operator")
	require.NoError(t, err)

	require.Len(t, index.Items, 2, "a row with no release, or a module the local index names, was listed")
	require.Equal(t, originLocal, index.Items[0].Origin)
	require.Equal(t, "2.0.0", index.Items[0].Version)

	offer := index.Items[1]
	require.Equal(t, originMarketplace, offer.Origin)
	require.Equal(t, theModulePath, offer.Module)
	require.Equal(t, "Spools", offer.Title)
	require.Equal(t, "Counts what each print used.", offer.Description)
	require.Equal(t, "Acme", offer.Author)
	require.Equal(t, "verified", offer.Trust)
	require.Equal(t, market.digest, offer.Sha256)
	require.Equal(t, []string{"observe"}, offer.Permissions)
	require.Equal(t, []string{thisPlatform()}, offer.Platforms)
	require.False(t, offer.Installed)
	require.Equal(t, theNamespace, offer.Namespace)

	require.NotNil(t, index.Marketplace)
	require.True(t, index.Marketplace.Reachable)
	require.Equal(t, market.server.URL+"/connect", index.Marketplace.BrowseURL)

	// The server asked for its own platform.
	require.Contains(t, market.queries[0], "platform="+thisPlatform())
}

func TestTheCatalogueIsKeptAndAFailedRefreshServesTheCopy(t *testing.T) {
	t.Parallel()

	market := newFakeMarketplace(t, archiveBytes(t, bothHalvesManifest()), "1.0.0")
	installer, _ := marketInstaller(t, market.server.URL)

	clock := time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	installer.market.now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()

		return clock
	}

	_, err := installer.Index(t.Context(), "user:operator")
	require.NoError(t, err)
	_, err = installer.Index(t.Context(), "user:operator")
	require.NoError(t, err)
	require.Equal(t, 1, market.lists(), "a fresh copy was read again")

	// Past the TTL, with the marketplace down: the request is answered from
	// the copy and a refresh runs behind it, fails, and says why.
	market.setDown(true)
	mu.Lock()
	clock = clock.Add(marketplaceTTL + time.Second)
	mu.Unlock()

	index, err := installer.Index(t.Context(), "user:operator")
	require.NoError(t, err)
	require.Len(t, index.Items, 2, "the copy was not served while the refresh ran")

	require.Eventually(t, func() bool {
		return installer.market.status().Problem != ""
	}, 5*time.Second, 10*time.Millisecond)

	index, err = installer.Index(t.Context(), "user:operator")
	require.NoError(t, err)
	require.Len(t, index.Items, 2, "a failed refresh dropped the copy")
	require.False(t, index.Marketplace.Reachable, "a marketplace whose last refresh failed was reported reachable")
	require.True(t, index.Marketplace.Stale)
	require.NotEmpty(t, index.Marketplace.Problem)
}

func TestAMarketplaceThatNeverAnsweredLeavesTheLocalRows(t *testing.T) {
	t.Parallel()

	market := newFakeMarketplace(t, nil, "1.0.0")
	market.setDown(true)

	installer, _ := marketInstaller(t, market.server.URL)

	index, err := installer.Index(t.Context(), "user:operator")
	require.NoError(t, err, "a marketplace that is down failed the index")
	require.Empty(t, index.Items)
	require.False(t, index.Marketplace.Reachable)
	require.NotEmpty(t, index.Marketplace.Problem)
}

func TestAMarketplaceBehindABrokenResolverNamesTheDNSFailure(t *testing.T) {
	t.Parallel()

	installer, _ := marketInstaller(t, "https://plugin-marketplace.prusa3d.com")
	installer.market.client = &http.Client{Transport: &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, &net.DNSError{
				Err: "server misbehaving", Name: "plugin-marketplace.prusa3d.com",
				Server: "127.0.0.11:53", IsTemporary: true,
			}
		},
	}}

	index, err := installer.Index(t.Context(), "user:operator")
	require.NoError(t, err)
	require.False(t, index.Marketplace.Reachable)
	require.Contains(t, index.Marketplace.Problem, "cannot resolve plugin-marketplace.prusa3d.com")
}

func TestAnInstallWithNoAddressTakesTheMarketplaceRelease(t *testing.T) {
	t.Parallel()

	market := newFakeMarketplace(t, archiveBytes(t, bothHalvesManifest()), "1.0.0")
	installer, _ := marketInstaller(t, market.server.URL)

	held, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", Approve: true,
	})
	require.NoError(t, err)
	require.Equal(t, "sink:spools", held.Instance)

	index, err := installer.Index(t.Context(), "user:operator")
	require.NoError(t, err)
	require.True(t, index.Items[0].Installed)
	require.Equal(t, "1.0.0", index.Items[0].InstalledVersion)
}

func TestAReleaseWithAnotherDigestIsRefusedBeforeItIsUnpacked(t *testing.T) {
	t.Parallel()

	market := newFakeMarketplace(t, archiveBytes(t, bothHalvesManifest()), "1.0.0")
	market.digest = strings.Repeat("ab", sha256.Size)

	installer, configPath := marketInstaller(t, market.server.URL)

	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", Approve: true,
	})
	requireCode(t, err, httpfault.CodeModuleDigestMismatch())

	rows, err := resource.Installed(resource.Paths{File: configPath, Overlay: installer.core.Overlay})
	require.NoError(t, err)
	require.Empty(t, rows)
	require.NoDirExists(t, filepath.Join(installer.core.Modules, "spools"))
}

func TestADigestTheCallerStatesIsHeldToo(t *testing.T) {
	t.Parallel()

	market := newFakeMarketplace(t, archiveBytes(t, bothHalvesManifest()), "1.0.0")
	installer, _ := marketInstaller(t, market.server.URL)

	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", Approve: true,
		From:   market.server.URL + "/storage/spools.tar.gz",
		Sha256: strings.Repeat("CD", sha256.Size),
	})
	requireCode(t, err, httpfault.CodeModuleDigestMismatch())

	_, err = installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", Approve: true,
		From:   market.server.URL + "/storage/spools.tar.gz",
		Sha256: strings.ToUpper(market.digest),
	})
	require.NoError(t, err)
}

func TestAVersionTheMarketplaceDoesNotOfferIsRefused(t *testing.T) {
	t.Parallel()

	market := newFakeMarketplace(t, archiveBytes(t, bothHalvesManifest()), "1.0.0")
	installer, _ := marketInstaller(t, market.server.URL)

	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "9.9.9",
	})
	requireCode(t, err, httpfault.CodeModuleNotOffered())

	_, err = installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: "github.com/acme/elsewhere", Version: "1.0.0",
	})
	requireCode(t, err, httpfault.CodeModuleNotOffered())
}

func TestAnInstallWithNoAddressAndNoMarketplaceIsRefused(t *testing.T) {
	t.Parallel()

	installer, _ := theInstaller(t, true)

	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0",
	})
	requireCode(t, err, httpfault.CodeRequestInvalid())
}

// requireCode holds an error to one registered code.
func requireCode(t *testing.T, err error, code fault.Code) {
	t.Helper()

	require.Error(t, err)

	var raised *fault.Fault

	require.ErrorAs(t, err, &raised)
	require.Equal(t, code, raised.Code(), raised.Error())
}

// TestABundleWithNoPlatformRunsEverywhere is a module that ships only a
// browser bundle: the marketplace lists it with no platform, and it fits this
// server whatever its platform is.
func TestABundleWithNoPlatformRunsEverywhere(t *testing.T) {
	t.Parallel()

	catalogue := &marketplaceCatalogue{platform: "linux_arm64"}

	require.True(t, catalogue.runsHere(marketplaceRelease{}))
	require.True(t, catalogue.runsHere(marketplaceRelease{Platforms: []string{"linux_arm64"}}))
	require.False(t, catalogue.runsHere(marketplaceRelease{Platforms: []string{"darwin_arm64"}}))
}

// TestTheCatalogueIsAskedForAVersionTheMarketplaceReads is the core the
// catalogue read names: the whole version with its prerelease, so the
// marketplace holds a range the way the install does, and nothing for a
// build stamped with a commit, which the marketplace would refuse as 400.
func TestTheCatalogueIsAskedForAVersionTheMarketplaceReads(t *testing.T) {
	t.Parallel()

	for running, want := range map[string]string{
		"0.2.0":                   "0.2.0",
		"0.1.0-beta.1":            "0.1.0-beta.1",
		"0.0.1-alpha.6":           "0.0.1-alpha.6",
		"0.0.1-alpha.6+abc.dirty": "0.0.1-alpha.6",
		"1.4.2+sha.abc":           "1.4.2",
		"3029254f":                "",
		"":                        "",
	} {
		require.Equal(t, want, marketplaceCore(running), "running %q", running)
	}
}

// TestTheIndexOffersOnlyAReleaseTheInstallTakes holds the newest release of
// each row to this core by the install's rule. A marketplace that compares a
// prerelease core by its three numbers alone offers a tester on 0.0.1-alpha.6
// a release that needs >=0.0.1-alpha.7, and the install would refuse it.
func TestTheIndexOffersOnlyAReleaseTheInstallTakes(t *testing.T) {
	t.Parallel()

	row := func(module, core string) marketplaceModule {
		return marketplaceModule{Module: module, Latest: &marketplaceRelease{Version: "0.3.0", Core: core}}
	}

	rows := []marketplaceModule{
		row("github.com/acme/needs-alpha7", ">=0.0.1-alpha.7"),
		row("github.com/acme/any-0.0.1", ">=0.0.1"),
		row("github.com/acme/no-range", ""),
		row("github.com/acme/unreadable", "not a range"),
		row("github.com/acme/later", ">=0.0.2"),
	}

	for core, want := range map[string][]string{
		"0.0.1-alpha.6": {"github.com/acme/any-0.0.1", "github.com/acme/no-range"},
		"0.0.1-alpha.7": {
			"github.com/acme/needs-alpha7", "github.com/acme/any-0.0.1", "github.com/acme/no-range",
		},
		"0.0.2": {
			"github.com/acme/needs-alpha7", "github.com/acme/any-0.0.1", "github.com/acme/no-range",
			"github.com/acme/later",
		},
		"": {
			"github.com/acme/needs-alpha7", "github.com/acme/any-0.0.1", "github.com/acme/no-range",
			"github.com/acme/unreadable", "github.com/acme/later",
		},
	} {
		catalogue := &marketplaceCatalogue{platform: thisPlatform(), core: core}

		offered := []string{}
		for _, kept := range catalogue.runningHere(rows) {
			offered = append(offered, kept.Module)
		}

		require.Equal(t, want, offered, "core %q", core)
	}
}
