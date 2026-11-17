package app

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/authz"
)

// What `module add -approve` has to do beside the declaration: the module's own
// principal holds a grant afterwards, so the module reads the printers of the
// server it was installed on. Without -approve nothing is granted, a removal
// takes the grant back, and a permission nothing can confer stops the install
// before a file is written.

// grantsOnTheOrg is every grant this deployment's graph holds on the
// organization, as the notation a refusal spells them in.
func grantsOnTheOrg(t *testing.T, core config.Core) []string {
	t.Helper()

	host, closeHost, err := openGraph(t.Context(), core, "", quiet())
	require.NoError(t, err)

	defer closeHost()

	who, err := authz.NewSubject(humanSubjectType, core.Principal)
	require.NoError(t, err)

	org, err := authz.NewObject(authz.TypeOrg, bootstrapOrg)
	require.NoError(t, err)

	rows, err := host.graph.Grants(t.Context(), who, org)
	require.NoError(t, err)

	held := make([]string, 0, len(rows))
	for _, row := range rows {
		held = append(held, row.String())
	}

	return held
}

// theSinkGrant is the tuple an approved install of the test module writes.
const theSinkGrant = "org:local#viewer@sink:spools"

func TestApprovingAnInstallGrantsTheModuleTheFleet(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	release := aRelease(t, bothHalvesManifest(), true)

	said := addModuleFrom(t, configPath, theModulePath+"@1.0.0", release, "-approve")
	require.Contains(t, said, "granted     "+theSinkGrant+" for observe")

	require.Contains(t, grantsOnTheOrg(t, core), theSinkGrant,
		"the module holds no grant, so it would read no printer")
}

func TestAnInstallNobodyApprovedGrantsTheModuleNothing(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	release := aRelease(t, bothHalvesManifest(), true)

	said := addModuleFrom(t, configPath, theModulePath+"@1.0.0", release)
	require.NotContains(t, said, "granted     ")
	require.Contains(t, said, "asks for    permission observe")

	require.NotContains(t, grantsOnTheOrg(t, core), theSinkGrant)
}

func TestInstallingTheSameModuleTwiceLeavesOneGrant(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	release := aRelease(t, bothHalvesManifest(), true)

	addModuleFrom(t, configPath, theModulePath+"@1.0.0", release, "-approve")
	addModuleFrom(t, configPath, theModulePath+"@1.0.0", release, "-approve")

	held := grantsOnTheOrg(t, core)

	written := 0

	for _, row := range held {
		if row == theSinkGrant {
			written++
		}
	}

	require.Equal(t, 1, written, "the graph holds the same grant twice")
}

func TestRemovingAModuleTakesItsGrantAway(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	release := aRelease(t, bothHalvesManifest(), true)

	addModuleFrom(t, configPath, theModulePath+"@1.0.0", release, "-approve")
	require.Contains(t, grantsOnTheOrg(t, core), theSinkGrant)

	var out bytes.Buffer

	require.NoError(t, Module(t.Context(), &out,
		[]string{removeVerb, "-config", configPath, theModulePath}))
	require.Contains(t, out.String(), "withdrawn   "+theSinkGrant)

	require.NotContains(t, grantsOnTheOrg(t, core), theSinkGrant,
		"the module is gone and its grant stayed behind")
}

// permissionManifest is the test module asking for one named permission.
func permissionManifest(permission string) string {
	return strings.Join([]string{
		"name: spools",
		"kind: sink",
		"license: MIT",
		"version: 1.0.0",
		"contract_major: 1",
		"platforms:",
		"  - " + runtime.GOOS + "_" + runtime.GOARCH,
		"permissions:",
		"  - " + permission,
		"",
	}, "\n")
}

func TestAPermissionTheGraphDoesNotHoldStopsTheInstall(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	release := aRelease(t, permissionManifest("read_everything"), true)

	var out bytes.Buffer

	err := Module(t.Context(), &out, []string{
		addVerb, "-config", configPath, "-from", release, "-approve", theModulePath + "@1.0.0",
	})
	require.ErrorContains(t, err, "read_everything")

	// Nothing was written: no files, no declaration, no grant.
	require.NoDirExists(t, filepath.Join(core.Modules, "spools"))
	require.NoFileExists(t, core.Overlay)
	require.NotContains(t, grantsOnTheOrg(t, core), theSinkGrant)
}

func TestAPermissionNoRoleCarriesStopsTheInstall(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	release := aRelease(t, permissionManifest("report_telemetry"), true)

	var out bytes.Buffer

	err := Module(t.Context(), &out, []string{
		addVerb, "-config", configPath, "-from", release, "-approve", theModulePath + "@1.0.0",
	})
	require.ErrorContains(t, err, "report_telemetry")

	require.NoDirExists(t, filepath.Join(core.Modules, "spools"))
	require.NoFileExists(t, core.Overlay)
}

func TestAPermissionAboveViewerIsSaidAndNotGranted(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	release := aRelease(t, permissionManifest("operate"), true)

	said := addModuleFrom(t, configPath, theModulePath+"@1.0.0", release, "-approve")
	require.Contains(t, said, "not granted operate. It needs the operator role on the organization")

	// The install went through and the module is declared with the permission
	// recorded, because the operator still grants that one on each printer.
	body, err := os.ReadFile(core.Overlay)
	require.NoError(t, err)
	require.Contains(t, string(body), "operate")

	// And no tuple at all, not merely no operator one. A module asking for
	// operate alone must not be handed the viewer role as a consolation.
	for _, row := range grantsOnTheOrg(t, core) {
		require.NotContains(t, row, "sink:spools",
			"the module asked for operate alone and something was granted anyway")
	}
}

// TestAGrantedLineSaysWhatTheRoleAlsoCarries is what an operator has to be able
// to read off the install. The viewer role answers a request for observe and
// hands over two resource-kind actions with it, and an install that printed the
// action alone would have said less than it did.
func TestAGrantedLineSaysWhatTheRoleAlsoCarries(t *testing.T) {
	t.Parallel()

	configPath, _ := installConfig(t)
	release := aRelease(t, bothHalvesManifest(), true)

	said := addModuleFrom(t, configPath, theModulePath+"@1.0.0", release, "-approve")
	require.Contains(t, said, "The viewer role also carries view_resource, read_storage")
}

// TestTheTupleAnInstallWritesConfersTheseActions pins what the written grant is
// worth, so a change to the viewer mask is noticed here rather than in a
// deployment.
func TestTheTupleAnInstallWritesConfersTheseActions(t *testing.T) {
	t.Parallel()

	instance := sinkRef(t, "spools")

	plan, err := planModuleGrants(instance, []string{"observe"})
	require.NoError(t, err)
	require.Len(t, plan.write, 1)

	written := plan.write[0].tuple
	require.Equal(t, "org:local#viewer@sink:spools", written.String())

	require.Equal(t,
		[]authz.Action{authz.ActionObserve, authz.ActionViewResource, authz.ActionReadStorage},
		authz.OrgRoleConfers(written.Relation()),
		"the grant an install writes for observe is worth exactly these actions")
}

// TestAPrincipalThatCannotWriteTheGrantInstallsNothing is the state this whole
// change exists to prevent, reached from the other side: a deployment whose
// core.principal the graph holds nothing for.
func TestAPrincipalThatCannotWriteTheGrantInstallsNothing(t *testing.T) {
	t.Parallel()

	path, core := installConfig(t)
	release := aRelease(t, bothHalvesManifest(), true)

	// One install and one removal, so the graph exists with its first grant
	// written for the principal the file names, and nothing is declared.
	addModuleFrom(t, path, theModulePath+"@1.0.0", release, "-approve")

	var removed bytes.Buffer

	require.NoError(t, Module(t.Context(), &removed,
		[]string{removeVerb, "-config", path, theModulePath}))

	// Every grant withdrawn, so the graph holds nothing for that principal,
	// and the next start hands nothing back.
	withdrawEveryGrant(t, core)

	var out bytes.Buffer

	err := Module(t.Context(), &out, []string{
		addVerb, "-config", path, "-from", release, "-approve", theModulePath + "@1.0.0",
	})
	require.ErrorContains(t, err, core.Principal)
	require.ErrorContains(t, err, "Nothing was installed")

	// Nothing was written, so no module is switched on holding permissions it
	// was never granted.
	require.NoDirExists(t, filepath.Join(core.Modules, "spools"))
	require.Empty(t, withdrawEveryGrant(t, core), "the refused install wrote a grant")

	rows, err := resource.Installed(resource.Paths{File: path, Overlay: core.Overlay})
	require.NoError(t, err)
	require.Empty(t, rows, "the refused install left a declaration behind")
}

// withdrawEveryGrant deletes every tuple in the graph core names and returns
// what it deleted.
func withdrawEveryGrant(t *testing.T, core config.Core) []authz.Tuple {
	t.Helper()

	host, closeHost, err := openGraph(t.Context(), core, "", quiet())
	require.NoError(t, err)

	defer closeHost()

	held, err := host.graph.Tuples(t.Context())
	require.NoError(t, err)
	require.NoError(t, host.graph.Delete(t.Context(), held...))

	return held
}

// TestARecordedPermissionNobodyCanPlanStillRemovesTheModule is the case a
// removal must never be refused for. A declaration edited by hand, or written by
// a build whose vocabulary has since changed, holds a permission this server
// cannot turn into a grant. Refusing would leave the files, the declaration and
// every other grant of that module standing with nothing left to act on them.
func TestARecordedPermissionNobodyCanPlanStillRemovesTheModule(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	release := aRelease(t, bothHalvesManifest(), true)

	addModuleFrom(t, configPath, theModulePath+"@1.0.0", release, "-approve")
	require.Contains(t, grantsOnTheOrg(t, core), theSinkGrant)

	// The overlay as another route left it: one permission a role on the
	// organization reaches, and one only a device's own credential takes, which
	// no grant can ever answer.
	overlay, err := os.ReadFile(core.Overlay)
	require.NoError(t, err)

	edited := strings.Replace(string(overlay), `"observe"`, `"observe","report_telemetry"`, 1)
	require.NotEqual(t, string(overlay), edited, "the overlay records no permission")
	require.NoError(t, os.WriteFile(core.Overlay, []byte(edited), 0o600))

	var out bytes.Buffer

	require.NoError(t, Module(t.Context(), &out,
		[]string{removeVerb, "-config", configPath, theModulePath}))

	said := out.String()
	require.Contains(t, said, "removed sink:spools")
	require.Contains(t, said, "withdrawn   "+theSinkGrant)
	require.Contains(t, said, `left alone  the recorded permission "report_telemetry"`)

	require.NoDirExists(t, filepath.Join(core.Modules, "spools"))
	require.NotContains(t, grantsOnTheOrg(t, core), theSinkGrant)
}

// TestAReinstallTakesBackAGrantTheNewManifestDoesNotAskFor is the other half of
// keeping the graph and the declaration saying one thing. A module that dropped
// a permission keeps the grant it was given for it otherwise, and nothing left
// in the declaration says why that grant is there.
func TestAReinstallTakesBackAGrantTheNewManifestDoesNotAskFor(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)

	addModuleFrom(t, configPath, theModulePath+"@1.0.0",
		aRelease(t, bothHalvesManifest(), true), "-approve")
	require.Contains(t, grantsOnTheOrg(t, core), theSinkGrant)

	// The same module again, from a release whose manifest asks for nothing.
	said := addModuleFrom(t, configPath, theModulePath+"@1.0.0",
		aRelease(t, noPermissionManifest(), true), "-approve")
	require.Contains(t, said, "withdrawn   "+theSinkGrant)

	require.NotContains(t, grantsOnTheOrg(t, core), theSinkGrant,
		"the module no longer asks for anything and keeps the fleet")
}

// noPermissionManifest is the test module with its permissions list taken out.
func noPermissionManifest() string {
	return strings.Join([]string{
		"name: spools",
		"kind: sink",
		"license: MIT",
		"version: 1.0.0",
		"contract_major: 1",
		"platforms:",
		"  - " + runtime.GOOS + "_" + runtime.GOARCH,
		"",
	}, "\n")
}

// TestRemovingAModuleOverHTTPTakesItsGrantAway holds the two removal routes to
// one answer. DELETE /api/v1/modules/<namespace> is what a marketplace page
// removes a module through, and a grant it left standing would be written for a
// principal nothing declares any more.
func TestRemovingAModuleOverHTTPTakesItsGrantAway(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	release := aRelease(t, bothHalvesManifest(), true)

	addModuleFrom(t, configPath, theModulePath+"@1.0.0", release, "-approve")
	require.Contains(t, grantsOnTheOrg(t, core), theSinkGrant)

	removeOverHTTP(t, core, configPath)

	require.NotContains(t, grantsOnTheOrg(t, core), theSinkGrant,
		"the module was removed over the API and kept the fleet")
}

// removeOverHTTP runs the removal the API runs, over this deployment's own
// graph, and closes the graph before the caller reads it back.
func removeOverHTTP(t *testing.T, core config.Core, configPath string) {
	t.Helper()

	host, closeHost, err := openGraph(t.Context(), core, "", quiet())
	require.NoError(t, err)

	defer closeHost()

	installer := newModuleInstaller(core, configPath, decidingGraph{allowed: true}, nil, nil,
		host.graph, discard())

	require.NoError(t, installer.Remove(t.Context(), "user:operator", theNamespace, false))
}

// TestWriteStorageIsGrantedOnTheStorageKind pins where write_storage goes. No
// role on the organization a module may hold reaches it, and the storage
// routes check it on the storage kind, so an approved install writes it
// there and a module that writes files needs no grant by hand.
func TestWriteStorageIsGrantedOnTheStorageKind(t *testing.T) {
	t.Parallel()

	plan, err := planModuleGrants(sinkRef(t, "slicer"), []string{"read_storage", "write_storage"})
	require.NoError(t, err)
	require.Empty(t, plan.apart)

	var written []string
	for _, grant := range plan.write {
		written = append(written, grant.tuple.String())
	}

	require.Contains(t, written, "resource_kind:storage#operator@sink:slicer")
}

// grantsOnTheStorageKind is every grant this deployment's graph holds on the
// storage resource kind.
func grantsOnTheStorageKind(t *testing.T, core config.Core) []string {
	t.Helper()

	host, closeHost, err := openGraph(t.Context(), core, "", quiet())
	require.NoError(t, err)

	defer closeHost()

	who, err := authz.NewSubject(humanSubjectType, core.Principal)
	require.NoError(t, err)

	kind, err := authz.NewObject(authz.TypeResourceKind, "storage")
	require.NoError(t, err)

	rows, err := host.graph.Grants(t.Context(), who, kind)
	require.NoError(t, err)

	held := make([]string, 0, len(rows))
	for _, row := range rows {
		held = append(held, row.String())
	}

	return held
}

// theStorageGrant is the tuple an approved install of a module asking for
// write_storage writes.
const theStorageGrant = "resource_kind:storage#operator@sink:spools"

// A grant on the storage kind is the module's like a grant on the
// organization: an update whose manifest no longer asks for it and a removal
// both take it back (ADR-20261027-pq5 rule 12).
func TestAStorageGrantIsTakenBackOnUpdateAndRemoval(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)

	said := addModuleFrom(t, configPath, theModulePath+"@1.0.0",
		aRelease(t, permissionManifest("write_storage"), true), "-approve")
	require.Contains(t, said, "granted     "+theStorageGrant)
	require.Contains(t, grantsOnTheStorageKind(t, core), theStorageGrant)

	said = addModuleFrom(t, configPath, theModulePath+"@1.0.0",
		aRelease(t, noPermissionManifest(), true), "-approve")
	require.Contains(t, said, "withdrawn   "+theStorageGrant)
	require.NotContains(t, grantsOnTheStorageKind(t, core), theStorageGrant,
		"the module no longer asks for write_storage and keeps it")

	addModuleFrom(t, configPath, theModulePath+"@1.0.0",
		aRelease(t, permissionManifest("write_storage"), true), "-approve")
	require.Contains(t, grantsOnTheStorageKind(t, core), theStorageGrant)

	var out bytes.Buffer

	require.NoError(t, Module(t.Context(), &out,
		[]string{removeVerb, "-config", configPath, theModulePath}))
	require.Contains(t, out.String(), "withdrawn   "+theStorageGrant)
	require.NotContains(t, grantsOnTheStorageKind(t, core), theStorageGrant,
		"the module is gone and its storage grant stayed behind")
}
