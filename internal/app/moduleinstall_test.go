package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// What the install operation has to do, held against the same files
// `module add` writes: the declaration lands in the overlay and the operator's
// own file is untouched, the graph decides before anything is written, and a
// release named by URL is fetched and unpacked like one named by path.

// theInstaller is the installer under test, over a fresh config and overlay,
// supervising nothing.
func theInstaller(t *testing.T, allowed bool) (*moduleInstaller, string) {
	t.Helper()

	return theInstallerOver(t, allowed, nil)
}

// theInstallerOver is the same installer with a supervision plane behind it,
// and no configured inventory: these cases are about the files an install
// writes, and the reconcile that follows one has its own.
func theInstallerOver(t *testing.T, allowed bool, plane runningModules) (*moduleInstaller, string) {
	t.Helper()

	configPath, core := installConfig(t)

	return newModuleInstaller(
		core, configPath, decidingGraph{allowed: allowed}, plane, nil, nil, discard()), configPath
}

// decidingGraph answers the one question the installer asks, and records the
// kind it was asked about.
type decidingGraph struct{ allowed bool }

func (g decidingGraph) Can(
	_ context.Context, _ authz.Subject, _ authz.Action, _ authz.Object,
) (bool, error) {
	return g.allowed, nil
}

func TestInstallingOverHTTPWritesTheSameDeclarationTheCommandWrites(t *testing.T) {
	t.Parallel()

	installer, configPath := theInstaller(t, true)
	release := aRelease(t, bothHalvesManifest(), true)

	held, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", From: release, Approve: true,
	})
	require.NoError(t, err)

	require.Equal(t, "sink:spools", held.Instance)
	require.Equal(t, theNamespace, held.Namespace)
	require.Equal(t, "sink", held.Kind)
	require.Equal(t, "1.0.0", held.Version)
	require.Equal(t, "service", held.Runtime)
	require.True(t, held.Enabled)
	require.Empty(t, held.Requests, "an approved install has nothing left to ask for")

	// The operator's own file is untouched, the way it is for the command.
	body, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.NotContains(t, string(body), "spools")

	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: installer.core.Overlay})
	require.NoError(t, err)

	instance, err := ref.New(ref.KindSink, "spools")
	require.NoError(t, err)

	declared, found := doc.Find(instance)
	require.True(t, found, "the overlay holds no declaration for the module")
	require.Equal(t, resource.SourceOverlay, declared.Source)
	require.True(t, declared.Spec.Enabled)

	require.FileExists(t,
		filepath.Join(installer.core.Modules, "spools", binaryDirName, thisPlatformBinary()))
}

// Without approve the module is declared, switched off, and the answer says
// what it asked for, which is earlier research over HTTP.
func TestInstallingWithoutApproveReportsWhatTheModuleAsksFor(t *testing.T) {
	t.Parallel()

	installer, _ := theInstaller(t, true)
	release := aRelease(t, bothHalvesManifest(), true)

	held, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "^1.0.0", From: release,
	})
	require.NoError(t, err)

	require.False(t, held.Enabled)
	require.Contains(t, held.Requests, "permission observe")
}

// The graph decides, and a refusal leaves the deployment exactly as it was.
func TestACallerTheGraphRefusesWritesNothing(t *testing.T) {
	t.Parallel()

	installer, configPath := theInstaller(t, false)
	release := aRelease(t, bothHalvesManifest(), true)

	_, err := installer.Install(t.Context(), "user:nobody", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", From: release, Approve: true,
	})
	require.Error(t, err)

	var raised *fault.Fault

	require.ErrorAs(t, err, &raised)
	require.Equal(t, authz.CodeAccessDenied(), raised.Code())

	rows, err := resource.Installed(
		resource.Paths{File: configPath, Overlay: installer.core.Overlay})
	require.NoError(t, err)
	require.Empty(t, rows, "a refused install declared something")

	require.NoDirExists(t, filepath.Join(installer.core.Modules, "spools"))
}

// A release named by URL is fetched into a file of this server's own and then
// installed exactly as a release named by path is.
func TestAReleaseNamedByURLIsFetchedAndInstalled(t *testing.T) {
	t.Parallel()

	archive := anArchiveOf(t, aRelease(t, bothHalvesManifest(), true))

	body, err := os.ReadFile(archive)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)

	// The server is on loopback, which a download reaches only as the
	// configured local marketplace.
	installer, _ := marketInstaller(t, server.URL)
	sum := sha256.Sum256(body)

	held, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0",
		From: server.URL + "/spools-1.0.0.tar.gz", Sha256: hex.EncodeToString(sum[:]), Approve: true,
	})
	require.NoError(t, err)
	require.Equal(t, "sink:spools", held.Instance)
}

// A URL that names something other than an archive is refused before this
// server fetches anything.
func TestAURLThatIsNotAnArchiveIsRefused(t *testing.T) {
	t.Parallel()

	installer, _ := theInstaller(t, true)

	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", From: "https://example.test/spools",
	})
	require.Error(t, err)

	var raised *fault.Fault

	require.ErrorAs(t, err, &raised)
	require.Equal(t, "core.validation/REQUEST_INVALID", raised.Code().String())
}

// Removing by namespace takes the declaration and the files away, and a
// namespace nothing declares is a 404 rather than a silent success.
func TestRemovingByNamespaceTakesTheDeclarationAndTheFiles(t *testing.T) {
	t.Parallel()

	installer, configPath := theInstaller(t, true)
	release := aRelease(t, bothHalvesManifest(), true)

	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", From: release, Approve: true,
	})
	require.NoError(t, err)

	require.NoError(t, installer.Remove(t.Context(), "user:operator", theNamespace, false))

	rows, err := resource.Installed(
		resource.Paths{File: configPath, Overlay: installer.core.Overlay})
	require.NoError(t, err)
	require.Empty(t, rows)
	require.NoDirExists(t, filepath.Join(installer.core.Modules, "spools"))

	err = installer.Remove(t.Context(), "user:operator", theNamespace, false)
	require.Error(t, err, "removing a namespace nothing declares reported success")
}

// The index is read from the modules directory and marks what this deployment
// already declares. A deployment with no index file offers nothing.
func TestTheIndexMarksWhatIsAlreadyInstalled(t *testing.T) {
	t.Parallel()

	installer, _ := theInstaller(t, true)

	index, err := installer.Index(t.Context(), "user:operator")
	offers := index.Items
	require.NoError(t, err)
	require.Empty(t, offers, "a deployment with no index file offered something")

	release := aRelease(t, bothHalvesManifest(), true)

	_, err = installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", From: release, Approve: true,
	})
	require.NoError(t, err)

	document, err := json.Marshal(moduleIndexFile{Modules: []moduleIndexRow{
		{
			Module: theModulePath, Name: "Spools", Description: "Counts what each print used.",
			Version: "1.0.0", Source: "https://example.test/spools-1.0.0.tar.gz", Kind: "sink",
		},
		{
			Module: "github.com/acme/nozzle-watch", Name: "Nozzle watch",
			Version: "2.1.0", Source: "https://example.test/nozzle-watch-2.1.0.tar.gz", Kind: "asset",
		},
	}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(
		filepath.Join(installer.core.Modules, moduleIndexName), document, 0o600))

	index, err = installer.Index(t.Context(), "user:operator")
	require.NoError(t, err)
	require.Nil(t, index.Marketplace, "a deployment with no marketplace reported one")

	offers = index.Items
	require.Len(t, offers, 2)
	require.Equal(t, originLocal, offers[0].Origin)
	require.Equal(t, "1.0.0", offers[0].InstalledVersion)

	require.True(t, offers[0].Installed)
	require.Equal(t, "sink:spools", offers[0].Instance)
	require.Equal(t, "Counts what each print used.", offers[0].Description)

	require.False(t, offers[1].Installed)
	require.Empty(t, offers[1].Instance)
}

// An install approved over HTTP says what it gave, check_absent_people among
// it, the way `module add` prints it. One without approve gives nothing.
func TestAnInstallOverHTTPSaysItGaveCheckAbsentPeople(t *testing.T) {
	t.Parallel()

	for _, approve := range []bool{true, false} {
		configPath, core := installConfig(t)

		host, closeHost, err := openGraph(t.Context(), core, "", quiet())
		require.NoError(t, err)

		installer := newModuleInstaller(core, configPath, host.graph, nil, nil, host.graph, discard())
		release := aRelease(t, askingManifest(), true)

		held, err := installer.Install(t.Context(), "user:"+core.Principal, httpapi.ModuleRelease{
			Module: theModulePath, Version: "1.0.0", From: release, Approve: approve,
		})
		closeHost()
		require.NoError(t, err)

		if approve {
			require.ElementsMatch(t, []string{"permission observe", "permission check_absent_people"}, held.Approved)
			require.Empty(t, held.Requests)

			continue
		}

		require.Empty(t, held.Approved)
		require.Contains(t, held.Requests, "permission check_absent_people")
	}
}

// An approved install says exactly what it gave: the permissions a grant was
// written for, check_absent_people and each event. A permission no grant was
// written for is withheld and said apart, because the module cannot use it
// (ADR-20260929-f4n rule 3).
func TestAnApprovedInstallSaysExactlyWhatItGave(t *testing.T) {
	t.Parallel()

	asking := manifest.Manifest{
		Permissions:       []string{"observe", "operate"},
		CheckAbsentPeople: true,
		ListPeople:        true,
		Events:            map[string]string{"loaded": "spool.loaded"},
	}

	given, withheld := approvedOf(asking, theNamespace, []authz.Action{authz.ActionObserve})

	require.Equal(t, []string{
		"event loaded as " + theNamespace + ".spool.loaded",
		"permission check_absent_people",
		"permission list_people",
		"permission observe",
	}, given)
	require.Equal(t, []string{"permission operate"}, withheld)

	// Nothing written: the declaration still gives the event and
	// check_absent_people and list_people, and no permission is listed as
	// given.
	given, withheld = approvedOf(asking, theNamespace, nil)

	require.Equal(t, []string{
		"event loaded as " + theNamespace + ".spool.loaded",
		"permission check_absent_people",
		"permission list_people",
	}, given)
	require.Equal(t, []string{"permission observe", "permission operate"}, withheld)
}

// A sound release this server cannot write into its modules directory is the
// server's failure, and says which directory to fix. It is not a request the
// caller got wrong: a read-only core.modules answered REQUEST_INVALID before.
func TestAModulesDirectoryThatCannotBeWrittenIsTheServersFailure(t *testing.T) {
	t.Parallel()

	installer, _ := theInstaller(t, true)

	// A file where the directory should be refuses every write, as root too.
	blocked := filepath.Join(t.TempDir(), "modules")
	require.NoError(t, os.WriteFile(blocked, []byte("not a directory"), 0o600))
	installer.core.Modules = blocked

	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", From: aRelease(t, bothHalvesManifest(), true),
	})
	requireCode(t, err, httpfault.CodeModuleInstallFailed())
	require.Contains(t, err.Error(), blocked)
}

// theInstallerWithPool is the installer under test with core.modules_memory
// set, so the pool does not depend on the machine the tests run on.
func theInstallerWithPool(t *testing.T, poolMiB int64) (*moduleInstaller, string) {
	t.Helper()

	installer, configPath := theInstaller(t, true)
	installer.core.ModulesMemory = poolMiB << 20

	return installer, configPath
}

// The pool is a warning, not a limit: a module whose budget passes it still
// installs, switches on, and upgrades.
func TestInstallOverThePoolSucceeds(t *testing.T) {
	t.Parallel()

	installer, configPath := theInstallerWithPool(t, 256)

	installed, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", From: aRelease(t, recommendingManifest("1.0.0", 384), true),
		Approve: true,
	})
	require.NoError(t, err)
	require.True(t, installed.Enabled)

	_, err = installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.1.0", From: aRelease(t, recommendingManifest("1.1.0", 512), true),
		Approve: true,
	})
	require.NoError(t, err)

	budget, _ := resource.BudgetOf(installedSpools(t, installer, configPath).Spec)
	require.Equal(t, int64(512<<20), budget.Bytes)
}

// The operator's budget is set with approve and kept by an update that names
// none.
func TestAnUpdateKeepsTheOperatorsMemoryBudget(t *testing.T) {
	t.Parallel()

	installer, configPath := theInstaller(t, true)

	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", From: aRelease(t, recommendingManifest("1.0.0", 128), true),
		Approve: true, MemoryMiB: 64,
	})
	require.NoError(t, err)

	_, err = installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.1.0", From: aRelease(t, recommendingManifest("1.1.0", 96), true),
		Approve: true,
	})
	require.NoError(t, err)

	budget, _ := resource.BudgetOf(installedSpools(t, installer, configPath).Spec)
	require.Equal(t, resource.Budget{Bytes: 64 << 20, Source: resource.BudgetOperator}, budget)
}

// A budget is the operator's decision, so it is refused where nothing is
// approved.
func TestAMemoryBudgetWithoutApproveIsRefused(t *testing.T) {
	t.Parallel()

	installer, _ := theInstaller(t, true)

	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", From: aRelease(t, recommendingManifest("1.0.0", 128), true),
		MemoryMiB: 64,
	})

	var raised *fault.Fault

	require.ErrorAs(t, err, &raised)
	require.Equal(t, httpfault.CodeRequestInvalid(), raised.Code())
	require.NoDirExists(t, filepath.Join(installer.core.Modules, "spools"))
}

// An update that lowers the budget passes on a pool that is already
// over-committed, so the operator can bring it back within the pool.
func TestALoweringUpgradeOnAnOverCommittedPoolIsAccepted(t *testing.T) {
	t.Parallel()

	installer, configPath := theInstaller(t, true)

	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", From: aRelease(t, recommendingManifest("1.0.0", 384), true),
		Approve: true,
	})
	require.NoError(t, err)

	installer.core.ModulesMemory = 256 << 20

	_, err = installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.1.0", From: aRelease(t, recommendingManifest("1.1.0", 320), true),
		Approve: true,
	})
	require.NoError(t, err)

	budget, _ := resource.BudgetOf(installedSpools(t, installer, configPath).Spec)
	require.Equal(t, int64(320<<20), budget.Bytes)
}
