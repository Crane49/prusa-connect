package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// roomyManifest is the two-halves module asking for 128 MiB for its records
// (ADR-20261007-br4).
func roomyManifest() string {
	return strings.Replace(bothHalvesManifest(), "permissions:\n", "records_mib: 128\npermissions:\n", 1)
}

// declaredRoomy installs the module asking for room and reads back what the
// overlay holds.
func declaredRoomy(t *testing.T, extra ...string) (resource.Declaration, string) {
	t.Helper()

	configPath, core := installConfig(t)
	release := aRelease(t, roomyManifest(), true)

	said := addModuleFrom(t, configPath, theModulePath+"@1.0.0", release, extra...)

	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
	require.NoError(t, err)
	require.Empty(t, doc.Failures)

	instance, err := ref.New(ref.KindSink, "spools")
	require.NoError(t, err)

	declared, found := doc.Find(instance)
	require.True(t, found)

	return declared, said
}

// An approved install writes the room into the declaration, and the command
// says how much it gave against the default.
func TestAnApprovedInstallGrantsTheRoomTheModuleAsked(t *testing.T) {
	t.Parallel()

	declared, said := declaredRoomy(t, "-approve")

	require.Equal(t, 128, declared.Spec.Service.RecordsMiB)
	require.Equal(t, int64(128<<20), declared.Spec.Service.RecordLimit())
	require.Contains(t, said, "records     128 MiB, where the default is 8 MiB (records_mib)")
}

// An install without approve writes nothing and lists the room among the
// requests, so the namespace keeps the default until the operator agrees.
func TestAnInstallWithoutApproveOnlyListsTheRoom(t *testing.T) {
	t.Parallel()

	declared, said := declaredRoomy(t)

	require.Zero(t, declared.Spec.Service.RecordsMiB)
	require.Contains(t, said, "asks for    records 128 MiB")
	require.NotContains(t, said, "records     128 MiB")
}

// The HTTP install says the same: the room is a request until approve, and
// then it is among what was given.
func TestAnInstallOverHTTPSaysItGaveTheRoom(t *testing.T) {
	t.Parallel()

	for _, approve := range []bool{true, false} {
		installer, _ := theInstaller(t, true)
		release := aRelease(t, roomyManifest(), true)

		held, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
			Module: theModulePath, Version: "1.0.0", From: release, Approve: approve,
		})
		require.NoError(t, err)

		if approve {
			require.Contains(t, held.Approved, "records 128 MiB")
			require.Empty(t, held.Requests)

			continue
		}

		require.Empty(t, held.Approved)
		require.Contains(t, held.Requests, "records 128 MiB")
	}
}

// A manifest content with the default asks for nothing and is given nothing.
func TestAModuleContentWithTheDefaultAsksForNoRoom(t *testing.T) {
	t.Parallel()

	given, _ := approvedOf(manifest.Manifest{RecordsMiB: 0}, theNamespace, nil)
	require.Empty(t, given)

	given, _ = approvedOf(manifest.Manifest{RecordsMiB: 64}, theNamespace, nil)
	require.Equal(t, []string{"records 64 MiB"}, given)
}

// The operator may grant other room than the module asks for, more or less,
// and installing again with another number changes the grant. The new limit
// holds from the module's next start (ADR-20261007-br4 rule 5).
func TestTheOperatorGrantsTheRoomTheyChoose(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	release := aRelease(t, roomyManifest(), true)

	instance, err := ref.New(ref.KindSink, "spools")
	require.NoError(t, err)

	grantOf := func() int {
		doc, loaded := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
		require.NoError(t, loaded)

		declared, found := doc.Find(instance)
		require.True(t, found)

		return declared.Spec.Service.RecordsMiB
	}

	said := addModuleFrom(t, configPath, theModulePath+"@1.0.0", release, "-approve", "-records-mib", "256")
	require.Equal(t, 256, grantOf())
	require.Contains(t, said, "records     256 MiB, where the default is 8 MiB (records_mib)")

	// The same module installed again over the same declaration, with less.
	addModuleFrom(t, configPath, theModulePath+"@1.0.0", release, "-approve", "-records-mib", "16")
	require.Equal(t, 16, grantOf())
}

// A room named without approve would be read as given while nothing is, and a
// room past the ceiling is one the store would refuse. Both stop the install
// before anything is written.
func TestARoomTheInstallCannotGrantIsRefused(t *testing.T) {
	t.Parallel()

	for name, extra := range map[string][]string{
		"without approve":  {"-records-mib", "64"},
		"past the ceiling": {"-approve", "-records-mib", "2048"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			configPath, _ := installConfig(t)
			release := aRelease(t, roomyManifest(), true)

			var out bytes.Buffer

			args := append([]string{addVerb, "-config", configPath, "-from", release}, extra...)
			err := Module(t.Context(), &out, append(args, theModulePath+"@1.0.0"))
			require.ErrorIs(t, err, ErrUsage)
			require.Contains(t, err.Error(), "records_mib")
		})
	}
}

// Over HTTP the operator names the room in the body, as records_mib.
func TestAnInstallOverHTTPGrantsTheRoomTheOperatorNamed(t *testing.T) {
	t.Parallel()

	installer, _ := theInstaller(t, true)
	release := aRelease(t, roomyManifest(), true)

	held, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", From: release, Approve: true, RecordsMiB: 512,
	})
	require.NoError(t, err)
	require.Contains(t, held.Approved, "records 512 MiB")
}
