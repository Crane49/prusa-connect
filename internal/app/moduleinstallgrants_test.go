package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// An install over HTTP has to leave the authorization graph exactly as
// `module add` leaves it. A module approved over the API and granted nothing
// would be switched on and read no printer. These cases hold both routes
// against the real store, and hold the answer to what was written.

// installOverHTTP runs the install the API runs, over this deployment's own
// graph, and closes the graph before the caller reads it back.
func installOverHTTP(
	t *testing.T, core config.Core, configPath, principal string, approve bool,
) (httpapi.ModuleInstallation, error) {
	t.Helper()

	return installManifestOverHTTP(t, core, configPath, principal, approve, bothHalvesManifest())
}

// installManifestOverHTTP is the same install, of a release whose manifest the
// case chooses.
func installManifestOverHTTP(
	t *testing.T, core config.Core, configPath, principal string, approve bool, document string,
) (httpapi.ModuleInstallation, error) {
	t.Helper()

	host, closeHost, err := openGraph(t.Context(), core, "", quiet())
	require.NoError(t, err)

	defer closeHost()

	installer := newModuleInstaller(core, configPath, host.graph, nil, nil, host.graph, discard())

	return installer.Install(t.Context(), principal, httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0",
		From: aRelease(t, document, true), Approve: approve,
	})
}

// aKindAdmin writes the manager role on the sink resource kind for one more
// person, who holds nothing on the organization, and answers that person.
func aKindAdmin(t *testing.T, core config.Core) string {
	t.Helper()

	host, closeHost, err := openGraph(t.Context(), core, "", quiet())
	require.NoError(t, err)

	defer closeHost()

	require.NoError(t, host.graph.Write(t.Context(),
		tuple(t, authz.TypeResourceKind, "sink", authz.RelManager, "user:kind-admin")))

	return "user:kind-admin"
}

// requireInstallDenied holds an error to the refusal the graph answers.
func requireInstallDenied(t *testing.T, err error) {
	t.Helper()

	require.Error(t, err)

	var raised *fault.Fault

	require.ErrorAs(t, err, &raised)
	require.Equal(t, authz.CodeAccessDenied(), raised.Code())
}

// moduleGrantsOf is every grant on the organization written for the test
// module's own principal.
func moduleGrantsOf(t *testing.T, core config.Core) []string {
	t.Helper()

	return slices.DeleteFunc(grantsOnTheOrg(t, core), func(row string) bool {
		return !strings.HasSuffix(row, "@sink:spools")
	})
}

// moduleObserves places one printer in the organization and asks the graph
// whether the test module's instance may observe it.
func moduleObserves(t *testing.T, core config.Core) bool {
	t.Helper()

	host, closeHost, err := openGraph(t.Context(), core, "", quiet())
	require.NoError(t, err)

	defer closeHost()

	require.NoError(t, host.graph.UpsertDevice(t.Context(), "p-alpha", "Alpha"))
	require.NoError(t, host.graph.Write(t.Context(), tuple(t,
		authz.TypeDevice, "p-alpha", authz.RelOrg, "org:"+bootstrapOrg)))

	module, err := authz.ParseSubject("sink:spools")
	require.NoError(t, err)

	printer, err := authz.NewObject(authz.TypeDevice, "p-alpha")
	require.NoError(t, err)

	allowed, err := host.graph.Can(t.Context(), module, authz.ActionObserve, printer)
	require.NoError(t, err)

	return allowed
}

func TestAnApprovedInstallOverHTTPWritesTheGrantsTheCommandWrites(t *testing.T) {
	t.Parallel()

	commandPath, commandCore := installConfig(t)
	addModuleFrom(t, commandPath, theModulePath+"@1.0.0",
		aRelease(t, bothHalvesManifest(), true), "-approve")

	apiPath, apiCore := installConfig(t)
	installed, err := installOverHTTP(t, apiCore, apiPath, "user:"+apiCore.Principal, true)
	require.NoError(t, err)

	byCommand := moduleGrantsOf(t, commandCore)
	require.Equal(t, []string{theSinkGrant}, byCommand)
	require.Equal(t, byCommand, moduleGrantsOf(t, apiCore),
		"the API and the command wrote different grants for the same module")

	require.Contains(t, installed.Approved, "permission observe")
	require.Empty(t, installed.Withheld)

	require.True(t, moduleObserves(t, commandCore), "the module installed by the command reads no printer")
	require.True(t, moduleObserves(t, apiCore), "the module installed over HTTP reads no printer")
}

// The other direction: an install nobody approved writes no grant, and the
// module reads nothing.
func TestAnInstallOverHTTPNobodyApprovedGrantsNothing(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)

	installed, err := installOverHTTP(t, core, configPath, "user:"+core.Principal, false)
	require.NoError(t, err)

	require.Empty(t, installed.Approved)
	require.Contains(t, installed.Requests, "permission observe")
	require.Empty(t, moduleGrantsOf(t, core))
	require.False(t, moduleObserves(t, core), "a module nobody approved reads a printer")
}

// The grant is written as the caller. A caller who may declare a sink and may
// not administer the organization cannot hand their module the fleet, and the
// install is refused before anything is written, the way the command refuses a
// principal that cannot write the grant.
func TestAnInstallOverHTTPWritesTheGrantAsTheCaller(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)

	_, err := installOverHTTP(t, core, configPath, aKindAdmin(t, core), true)
	requireInstallDenied(t, err)

	require.Empty(t, moduleGrantsOf(t, core))
	require.NoDirExists(t, filepath.Join(core.Modules, "spools"))

	rows, err := resource.Installed(resource.Paths{File: configPath, Overlay: core.Overlay})
	require.NoError(t, err)
	require.Empty(t, rows, "the refused install left a declaration behind")
}

// An installer with no graph writes no grant, and its answer says so: observe
// is withheld rather than listed as given.
func TestAnInstallThatWroteNoGrantSaysItWithheldThePermission(t *testing.T) {
	t.Parallel()

	installer, _ := theInstaller(t, true)

	installed, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0",
		From: aRelease(t, bothHalvesManifest(), true), Approve: true,
	})
	require.NoError(t, err)

	require.NotContains(t, installed.Approved, "permission observe")
	require.Equal(t, []string{"permission observe"}, installed.Withheld)
}

// A reinstall takes back a grant the new manifest no longer asks for, and taking
// one back needs the same standing as writing one. A caller without it is
// refused before the files or the declaration change, so no grant is left that
// no declaration explains.
func TestAReinstallThatWouldTakeAGrantBackIsRefusedBeforeItWrites(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)

	_, err := installOverHTTP(t, core, configPath, "user:"+core.Principal, true)
	require.NoError(t, err)
	require.Equal(t, []string{theSinkGrant}, moduleGrantsOf(t, core))

	before, err := os.ReadFile(core.Overlay)
	require.NoError(t, err)

	_, err = installManifestOverHTTP(t, core, configPath, aKindAdmin(t, core), false, noPermissionManifest())
	requireInstallDenied(t, err)

	after, err := os.ReadFile(core.Overlay)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after), "a refused reinstall replaced the declaration")
	require.Equal(t, []string{theSinkGrant}, moduleGrantsOf(t, core))
}

// absentOnlyManifest asks to check people it is not serving and for no
// permission, so an approved install plans no grant at all.
func absentOnlyManifest() string {
	return strings.Replace(noPermissionManifest(), "platforms:\n", "check_absent_people: true\nplatforms:\n", 1)
}

// listOnlyManifest asks to list the people and for no permission.
func listOnlyManifest() string {
	return strings.Replace(noPermissionManifest(), "platforms:\n", "list_people: true\nplatforms:\n", 1)
}

// Approving writes what the module may do as itself even where no grant is
// planned: a permission the organization cannot confer, check_absent_people
// and list_people. Each administers the organization, so a caller who does not
// is refused before anything is written.
func TestApprovingACeilingWithNoGrantStillNeedsTheOrganization(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		document string
	}{
		{name: "a permission above viewer", document: permissionManifest("operate")},
		{name: "check_absent_people", document: absentOnlyManifest()},
		{name: "list_people", document: listOnlyManifest()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			configPath, core := installConfig(t)

			_, err := installManifestOverHTTP(t, core, configPath, aKindAdmin(t, core), true, tc.document)
			requireInstallDenied(t, err)

			rows, err := resource.Installed(resource.Paths{File: configPath, Overlay: core.Overlay})
			require.NoError(t, err)
			require.Empty(t, rows, "the refused install left a declaration behind")

			// The organization's admin may approve the same release.
			_, err = installManifestOverHTTP(t, core, configPath, "user:"+core.Principal, true, tc.document)
			require.NoError(t, err)
		})
	}
}
