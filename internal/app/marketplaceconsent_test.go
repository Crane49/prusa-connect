package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// ADR-20261005-64j rules 6 and 9 to 18: what an approved install may grant, what an
// update keeps, which name a release may take, where a download may connect,
// and the marketplace's documented answer decoded as it is written.

// asking is the both-halves manifest asking for the permissions and the
// records room given.
func asking(version string, records int, permissions ...string) string {
	lines := []string{
		"name: spools",
		"kind: sink",
		"license: MIT",
		"version: " + version,
		"contract_major: 1",
		"platforms:",
		"  - " + thisPlatform(),
		"permissions:",
	}

	for _, permission := range permissions {
		lines = append(lines, "  - "+permission)
	}

	if records > 0 {
		lines = append(lines, "records_mib: "+strconv.Itoa(records))
	}

	return strings.Join(lines, "\n") + "\n"
}

// declarationOf reads the one declaration of the spools module back.
func declarationOf(t *testing.T, installer *moduleInstaller, configPath string) resource.Declaration {
	t.Helper()

	rows, err := resource.Installed(resource.Paths{File: configPath, Overlay: installer.core.Overlay})
	require.NoError(t, err)

	found, ok := declarationOfNamespace(rows, theNamespace)
	require.True(t, ok)

	return found
}

// operatorChanges rewrites the spools service block in the overlay the way an
// operator does through the resources API: one permission taken away and
// another records room set.
func operatorChanges(t *testing.T, overlay string, permissions []string, records int) {
	t.Helper()

	body, err := os.ReadFile(overlay)
	require.NoError(t, err)

	var document map[string]any

	require.NoError(t, json.Unmarshal(body, &document))

	changed := false

	instances, _ := document["instances"].([]any)
	for _, raw := range instances {
		entry, _ := raw.(map[string]any)
		spec := entry

		if inner, ok := entry["spec"].(map[string]any); ok {
			spec = inner
		}

		service, ok := spec["service"].(map[string]any)
		if !ok {
			continue
		}

		service["permissions"] = permissions
		service["records_mib"] = records
		changed = true
	}

	require.True(t, changed, "the overlay holds no service block to change")

	body, err = json.Marshal(document)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(overlay, body, 0o600))
}

func TestAReleaseOfAnotherModuleCannotTakeAnInstalledName(t *testing.T) {
	t.Parallel()

	installer, configPath := theInstaller(t, true)
	release := aRelease(t, bothHalvesManifest(), true)

	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", From: release, Approve: true,
	})
	require.NoError(t, err)

	// The same manifest name under another module path is another module.
	_, err = installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: "github.com/mallory/spools", Version: "1.0.0", From: release, Approve: true,
	})
	requireCode(t, err, httpfault.CodeModuleNameTaken())

	held := declarationOf(t, installer, configPath)
	require.Equal(t, theNamespace+"/spools", held.Spec.Type, "the installed module was replaced")
}

func TestAModuleOfAnotherKindUnderTheSameNameIsRefused(t *testing.T) {
	t.Parallel()

	named := func(kind ref.Kind, name string) ref.Ref {
		instance, err := ref.New(kind, name)
		require.NoError(t, err)

		return instance
	}

	doc := resource.Document{Instances: []resource.Declaration{{
		Ref:  named(ref.KindSink, "spools"),
		Spec: resource.Spec{Type: theNamespace + "/spools", Runtime: install.RuntimeService},
	}}}

	err := nameFree(doc, named(ref.KindAsset, "spools"), theNamespace+"/spools")
	require.ErrorAs(t, err, new(nameTakenError))

	require.NoError(t, nameFree(doc, named(ref.KindSink, "spools"), theNamespace+"/spools"))
	require.NoError(t, nameFree(doc, named(ref.KindSink, "nozzle"), theNamespace+"/nozzle"))
}

func TestAMarketplaceReleaseAskingMoreThanListedIsRefused(t *testing.T) {
	t.Parallel()

	market := newFakeMarketplace(t, archiveBytes(t, asking("1.0.0", 0, "observe", "operate")), "1.0.0")
	installer, configPath := marketInstaller(t, market.server.URL)

	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", Approve: true,
	})
	requireCode(t, err, httpfault.CodeModuleRequestsChanged())
	require.Contains(t, err.Error(), "permission operate")

	rows, err := resource.Installed(resource.Paths{File: configPath, Overlay: installer.core.Overlay})
	require.NoError(t, err)
	require.Empty(t, rows, "a refused install declared something")

	// Without approve nothing is granted, and the answer lists every request.
	held, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0",
	})
	require.NoError(t, err)
	require.Contains(t, held.Requests, "permission operate")
}

func TestAMarketplaceReleaseWithinItsListingIsInstalled(t *testing.T) {
	t.Parallel()

	market := newFakeMarketplace(t, archiveBytes(t, asking("1.0.0", 64, "observe")), "1.0.0")
	market.asks = &marketplaceRelease{Permissions: []string{"observe", "operate"}, RecordsMiB: 64}
	installer, _ := marketInstaller(t, market.server.URL)

	held, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", Approve: true,
	})
	require.NoError(t, err)
	require.Contains(t, held.Approved, "records 64 MiB")
}

func TestAnExpectTheManifestExceedsIsRefused(t *testing.T) {
	t.Parallel()

	installer, _ := theInstaller(t, true)
	release := aRelease(t, asking("1.0.0", 64, "observe"), true)

	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", From: release, Approve: true,
		Expect: &httpapi.ModuleRequests{Permissions: []string{"observe"}, RecordsMiB: 16},
	})
	requireCode(t, err, httpfault.CodeModuleRequestsChanged())
	require.Contains(t, err.Error(), "records 64 MiB")

	_, err = installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", From: release, Approve: true,
		Expect: &httpapi.ModuleRequests{Permissions: []string{"observe"}, RecordsMiB: 64},
	})
	require.NoError(t, err)
}

func TestAnUpdateKeepsWhatTheOperatorDecided(t *testing.T) {
	t.Parallel()

	installer, configPath := theInstaller(t, true)

	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", Approve: true,
		From: aRelease(t, asking("1.0.0", 64, "observe"), true),
	})
	require.NoError(t, err)

	first := declarationOf(t, installer, configPath)
	require.NotNil(t, first.Spec.Module.Asked)
	require.Equal(t, []string{"observe"}, first.Spec.Module.Asked.Permissions)

	// The operator takes observe away and gives the records less room.
	operatorChanges(t, installer.core.Overlay, []string{}, 16)

	held, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "2.0.0", Approve: true,
		From: aRelease(t, asking("2.0.0", 128, "observe", "operate"), true),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"permission operate"}, held.Added)

	second := declarationOf(t, installer, configPath)
	require.Equal(t, "2.0.0", second.Spec.Module.Version)
	require.Equal(t, []string{"operate"}, declaredPermissions(second), "a permission the operator took away came back")
	require.Equal(t, 16, second.Spec.Service.RecordsMiB, "the room the operator set was replaced")
	require.Equal(t, []string{"observe", "operate"}, second.Spec.Module.Asked.Permissions)

	// A room the request names wins over the one the operator set.
	_, err = installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "2.0.0", Approve: true, RecordsMiB: 32,
		From: aRelease(t, asking("2.0.0", 128, "observe", "operate"), true),
	})
	require.NoError(t, err)
	require.Equal(t, 32, declarationOf(t, installer, configPath).Spec.Service.RecordsMiB)
}

func TestAnUpdateWithNoRecordedAskGrantsWhatTheManifestAsks(t *testing.T) {
	t.Parallel()

	installer, configPath := theInstaller(t, true)

	// Installed without approve, so nothing was decided and no ask is kept.
	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", From: aRelease(t, asking("1.0.0", 0, "observe"), true),
	})
	require.NoError(t, err)
	require.Nil(t, declarationOf(t, installer, configPath).Spec.Module.Asked)

	held, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", Approve: true,
		From: aRelease(t, asking("1.0.0", 0, "observe"), true),
	})
	require.NoError(t, err)
	require.Empty(t, held.Added)
	require.Equal(t, []string{"observe"}, declaredPermissions(declarationOf(t, installer, configPath)))
}

func TestABetaIsNotOfferedAsTheUpdateOfAStableInstall(t *testing.T) {
	t.Parallel()

	market := newFakeMarketplace(t, archiveBytes(t, bothHalvesManifest()), "1.0.0")
	market.channel = channelBeta
	installer, _ := marketInstaller(t, market.server.URL)

	index, err := installer.Index(t.Context(), "user:operator")
	require.NoError(t, err)
	require.Len(t, index.Items, 2)
	require.Equal(t, channelBeta, index.Items[0].Channel, "a module nobody installed lost its beta row")

	// Installed from a path, which is the stable channel.
	_, err = installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", From: aRelease(t, bothHalvesManifest(), true), Approve: true,
	})
	require.NoError(t, err)

	index, err = installer.Index(t.Context(), "user:operator")
	require.NoError(t, err)

	for _, offer := range index.Items {
		require.NotEqual(t, theModulePath, offer.Module, "a beta was offered as the update of a stable install")
	}

	// Installed from the beta channel, the beta row is its update.
	_, err = installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", Approve: true,
	})
	require.NoError(t, err)

	index, err = installer.Index(t.Context(), "user:operator")
	require.NoError(t, err)
	require.Equal(t, theModulePath, index.Items[0].Module)
	require.True(t, index.Items[0].Installed)
}

func TestAPageLinkIsKeptOnlyWhenABrowserMayOpenIt(t *testing.T) {
	t.Parallel()

	public := &marketplaceCatalogue{base: "https://plugins.example"}
	local := &marketplaceCatalogue{base: "http://localhost:4455"}

	cases := []struct {
		catalogue *marketplaceCatalogue
		page      string
		kept      bool
	}{
		{public, "https://plugins.example/connect/extensions/x", true},
		{public, "http://plugins.example/connect/extensions/x", false},
		{public, "javascript:alert(1)", false},
		{public, "https://user:secret@plugins.example/x", false},
		{local, "http://localhost:4455/connect/extensions/x", true},
		{local, "http://localhost:9999/connect/extensions/x", false},
	}

	for _, row := range cases {
		got := row.catalogue.pageURL(row.page)
		if row.kept {
			require.Equal(t, row.page, got)
		} else {
			require.Empty(t, got, row.page)
		}
	}
}

func TestAFailedMarketplaceDownloadIsTheMarketplacesFault(t *testing.T) {
	t.Parallel()

	market := newFakeMarketplace(t, archiveBytes(t, bothHalvesManifest()), "1.0.0")
	market.missing = true
	installer, _ := marketInstaller(t, market.server.URL)

	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", Approve: true,
	})
	requireCode(t, err, httpfault.CodeModuleDownloadFailed())
	// The answer names the download address, and a random port such as 40439
	// holds "404" too.
	withoutSource := strings.ReplaceAll(err.Error(), market.server.URL, "")
	require.NotContains(t, withoutSource, "404", "the answer told the caller what the address answered")
}

func TestAURLWithNoDigestIsRefused(t *testing.T) {
	t.Parallel()

	installer, _ := theInstaller(t, true)

	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", From: "https://plugins.example/spools-1.0.0.tar.gz",
	})
	requireCode(t, err, httpfault.CodeRequestInvalid())
	require.Contains(t, err.Error(), "SHA-256")
}

func TestADownloadReachesNoLocalAddress(t *testing.T) {
	t.Parallel()

	guard := dialGuard(false)

	cases := []struct {
		address string
		allowed bool
	}{
		{"93.184.216.34:443", true},
		{"[2606:4700::1111]:443", true},
		{"127.0.0.1:4455", false},
		{"[::1]:4455", false},
		{"127.0.0.1:22", false},
		{"[::ffff:127.0.0.1]:22", false},
		{"10.0.0.1:443", false},
		{"192.168.1.20:443", false},
		{"172.16.0.1:443", false},
		{"169.254.169.254:80", false},
		{"[fe80::1]:443", false},
		{"[fc00::1]:443", false},
		{"0.0.0.0:443", false},
		{"224.0.0.1:443", false},
	}

	for _, row := range cases {
		err := guard("tcp", row.address, nil)
		if row.allowed {
			require.NoError(t, err, row.address)
		} else {
			require.ErrorIs(t, err, errAddressRefused, row.address)
		}
	}

	// A marketplace on this machine is local development: loopback opens, on
	// any port, because its storage runs beside it. The local network does not.
	local := dialGuard(true)
	require.NoError(t, local("tcp", "127.0.0.1:4545", nil))
	require.NoError(t, local("tcp", "[::1]:4455", nil))
	require.ErrorIs(t, local("tcp", "192.168.1.20:443", nil), errAddressRefused)
	require.ErrorIs(t, local("tcp", "169.254.169.254:80", nil), errAddressRefused)
	require.True(t, publicAddress(netip.MustParseAddr("1.1.1.1")))
}

func TestLoopbackIsReachedOnlyBesideALocalMarketplace(t *testing.T) {
	t.Parallel()

	body := archiveBytes(t, bothHalvesManifest())
	sum := sha256.Sum256(body)

	// The storage a local marketplace runs beside it, on another port.
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(other.Close)

	market := newFakeMarketplace(t, body, "1.0.0")

	release := httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", Approve: true,
		From: other.URL + "/spools.tar.gz", Sha256: hex.EncodeToString(sum[:]),
	}

	for name, installer := range map[string]*moduleInstaller{
		"no marketplace":    first(theInstaller(t, true)),
		"a public one only": first(installerReading(t, "https://plugins.example")),
	} {
		_, err := installer.Install(t.Context(), "user:operator", release)
		requireCode(t, err, httpfault.CodeModuleDownloadFailed())
		require.NotContains(t, err.Error(), "local network", name)
	}

	_, err := first(marketInstaller(t, market.server.URL)).Install(t.Context(), "user:operator", release)
	require.NoError(t, err, "a local marketplace's own storage on another loopback port was refused")
}

func first(installer *moduleInstaller, _ string) *moduleInstaller { return installer }

func installerReading(t *testing.T, market string) (*moduleInstaller, string) {
	t.Helper()

	configPath, core := installConfig(t)
	core.Marketplace = config.Marketplace{URL: market, Timeout: 1}

	return newModuleInstaller(
		core, configPath, decidingGraph{allowed: true}, nil, nil, nil, discard()), configPath
}

func TestALocalIndexRowSaysWhatItsReleaseAsks(t *testing.T) {
	t.Parallel()

	installer, _ := theInstaller(t, true)
	release := aRelease(t, asking("1.0.0", 64, "observe"), true)
	stated := []string{"operate"}

	document, err := json.Marshal(moduleIndexFile{Modules: []moduleIndexRow{
		{Module: "github.com/acme/stated", Version: "1.0.0", Source: "/srv/stated.tar.gz", Permissions: &stated},
		{Module: theModulePath, Version: "1.0.0", Source: release},
		{Module: "github.com/acme/archived", Version: "1.0.0", Source: "/srv/archived.tar.gz"},
	}})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(installer.core.Modules, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(installer.core.Modules, moduleIndexName), document, 0o600))

	index, err := installer.Index(t.Context(), "user:operator")
	require.NoError(t, err)
	require.Len(t, index.Items, 3)

	require.True(t, index.Items[0].RequestsKnown)
	require.Equal(t, []string{"operate"}, index.Items[0].Permissions)

	require.True(t, index.Items[1].RequestsKnown, "the manifest of a release directory was not read")
	require.Equal(t, []string{"observe"}, index.Items[1].Permissions)
	require.Equal(t, 64, index.Items[1].RecordsMiB)

	require.False(t, index.Items[2].RequestsKnown)
}

// TestTheMarketplacesDocumentedAnswerIsDecoded reads the answer the
// marketplace documents for its Connect API, written by hand from its schema,
// so a field this server reads under another name fails here.
func TestTheMarketplacesDocumentedAnswerIsDecoded(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile(filepath.Join("testdata", "marketplace", "connect-list.json"))
	require.NoError(t, err)

	var page marketplacePage

	require.NoError(t, json.Unmarshal(body, &page))
	require.Len(t, page.Extensions, 1)
	require.NotNil(t, page.Next)

	row := page.Extensions[0]
	require.Equal(t, "github.com/prusa3d/parts", row.Module)
	require.Equal(t, "official", row.Author.Trust)
	require.NotNil(t, row.Latest)
	require.Equal(t, channelStable, row.Latest.channel())
	require.Equal(t, 64, row.Latest.RecordsMiB)
	require.Equal(t, map[string]string{"counted": "parts.counted"}, row.Latest.Events)
	require.True(t, row.Latest.CheckAbsentPeople)
	require.Equal(t, "official", row.Latest.Trust)

	catalogue := &marketplaceCatalogue{base: "https://plugin-marketplace.prusa3d.com"}

	offer, installable := catalogue.offerOf(row)
	require.True(t, installable)
	require.True(t, offer.RequestsKnown)
	require.Equal(t, row.PageURL, offer.PageURL)
	require.Equal(t, channelStable, offer.Channel)

	next, err := catalogue.nextPage(catalogue.base+marketplaceExtensionsPath+"?limit=1", *page.Next)
	require.NoError(t, err)
	require.Equal(t, catalogue.base+*page.Next, next)

	body, err = os.ReadFile(filepath.Join("testdata", "marketplace", "connect-detail.json"))
	require.NoError(t, err)

	var detail struct {
		Extension marketplaceModule `json:"extension"`
	}

	require.NoError(t, json.Unmarshal(body, &detail))
	require.Nil(t, detail.Extension.Latest, "a detail whose only release is a beta has no latest")
	require.Len(t, detail.Extension.Versions, 1)
	require.Equal(t, channelBeta, detail.Extension.Versions[0].channel())
	require.Empty(t, detail.Extension.Versions[0].Events)
}

func TestALocalModuleIsOfferedWithItsManifestsFacts(t *testing.T) {
	t.Parallel()

	installer, _ := theInstaller(t, true)
	release := aRelease(t, asking("1.2.0", 64, "observe"), true)
	installer.core.ModulesLocal = map[string]string{theModulePath: release}

	index, err := installer.Index(t.Context(), "user:operator")
	require.NoError(t, err)
	require.Len(t, index.Items, 1)

	offer := index.Items[0]
	require.Equal(t, theModulePath, offer.Module)
	require.Equal(t, "spools", offer.Name)
	require.Equal(t, "1.2.0", offer.Version)
	require.Equal(t, release, offer.Source)
	require.Equal(t, "local", offer.Origin)
	require.True(t, offer.RequestsKnown)
	require.Equal(t, []string{"observe"}, offer.Permissions)
	require.Equal(t, 64, offer.RecordsMiB)

	held, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: offer.Module, Version: offer.Version, From: offer.Source, Approve: true,
	})
	require.NoError(t, err)
	require.Equal(t, "1.2.0", held.Version)
}

func TestAnOfferSaysWhetherTheReleaseRunsAProcess(t *testing.T) {
	t.Parallel()

	installer, _ := theInstaller(t, true)
	uiOnly := strings.Join([]string{"name: shelf", "kind: sink", "license: MIT", "version: 1.0.0", ""}, "\n")
	installer.core.ModulesLocal = map[string]string{
		theModulePath:           aRelease(t, asking("1.0.0", 0, "observe"), true),
		"github.com/acme/shelf": aRelease(t, uiOnly, false),
	}

	index, err := installer.Index(t.Context(), "user:operator")
	require.NoError(t, err)

	process := map[string]bool{}
	for _, offer := range index.Items {
		process[offer.Module] = offer.Process
	}

	require.Equal(t, map[string]bool{theModulePath: true, "github.com/acme/shelf": false}, process)

	catalogue := &marketplaceCatalogue{}
	row := marketplaceModule{Module: theModulePath, Latest: &marketplaceRelease{
		Version: "1.0.0", DownloadURL: "https://example.com/r.tar.gz", Sha256: "00",
	}}

	bundle, ok := catalogue.offerOf(row)
	require.True(t, ok)
	require.False(t, bundle.Process, "a release with no platform ships no binary")

	row.Latest.Platforms = []string{thisPlatform()}
	binary, ok := catalogue.offerOf(row)
	require.True(t, ok)
	require.True(t, binary.Process)
}

func TestALocalModuleThatCannotBeReadIsSkipped(t *testing.T) {
	t.Parallel()

	installer, _ := theInstaller(t, true)
	broken := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(broken, manifestName), []byte("name: [\n"), 0o600))
	installer.core.ModulesLocal = map[string]string{
		"github.com/acme/missing": filepath.Join(t.TempDir(), "absent"),
		"github.com/acme/broken":  broken,
		theModulePath:             aRelease(t, asking("1.0.0", 0, "observe"), true),
	}

	index, err := installer.Index(t.Context(), "user:operator")
	require.NoError(t, err)
	require.Len(t, index.Items, 1)
	require.Equal(t, theModulePath, index.Items[0].Module)
}

func TestAnIndexRowWinsOverALocalModule(t *testing.T) {
	t.Parallel()

	installer, _ := theInstaller(t, true)
	installer.core.ModulesLocal = map[string]string{
		theModulePath: aRelease(t, asking("2.0.0", 0, "observe"), true),
	}
	document, err := json.Marshal(moduleIndexFile{Modules: []moduleIndexRow{
		{Module: theModulePath, Version: "1.0.0", Source: "/srv/spools.tar.gz"},
	}})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(installer.core.Modules, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(installer.core.Modules, moduleIndexName), document, 0o600))

	index, err := installer.Index(t.Context(), "user:operator")
	require.NoError(t, err)
	require.Len(t, index.Items, 1)
	require.Equal(t, "1.0.0", index.Items[0].Version)
}
