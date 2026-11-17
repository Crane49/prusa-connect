package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// An install over HTTP takes the module's settings the way `module add
// -settings` takes them from a file, and writes them into the same
// declaration. A module with a required setting installed without it does not
// start, so an install that could not carry the setting would fail every time.

// requiredShelfManifest is examples/spools with its one setting required, so
// the module does not start until somebody writes it.
func requiredShelfManifest() string {
	return strings.Join([]string{
		"name: spools",
		"kind: sink",
		"license: MIT",
		"version: 1.0.0",
		"contract_major: 1",
		"platforms:",
		"  - " + runtime.GOOS + "_" + runtime.GOARCH,
		"options:",
		"  - key: shelf_size",
		"    type: int",
		"    required: true",
		"    summary: How many spools this module keeps.",
		"",
	}, "\n")
}

// declaredConfig is the settings the overlay holds for the test module.
func declaredConfig(t *testing.T, core config.Core, configPath string) string {
	t.Helper()

	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
	require.NoError(t, err)

	declared, found := doc.Find(sinkRef(t, "spools"))
	require.True(t, found)

	return string(declared.Spec.Config)
}

func TestTheAPIAndTheCommandWriteTheSameSettings(t *testing.T) {
	t.Parallel()

	const settings = `{"shelf_size":20}`

	commandPath, commandCore := installConfig(t)
	file := filepath.Join(t.TempDir(), "settings.json")
	require.NoError(t, os.WriteFile(file, []byte(settings), 0o600))
	addModuleFrom(t, commandPath, theModulePath+"@1.0.0",
		aRelease(t, requiredShelfManifest(), true), "-settings", file)

	installer, apiPath := theInstaller(t, true)
	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0",
		From:     aRelease(t, requiredShelfManifest(), true),
		Settings: httpapi.ResourceConfig(settings),
	})
	require.NoError(t, err)

	byCommand := declaredConfig(t, commandCore, commandPath)
	require.JSONEq(t, settings, byCommand)
	require.JSONEq(t, byCommand, declaredConfig(t, installer.core, apiPath))
}

// The whole of it on a running server: the module that would fail for want of
// its setting starts when the install carries it.
func TestAnInstallCarryingTheRequiredSettingStartsTheModule(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		settings string
		state    string
	}{
		{name: "with the setting", settings: `{"shelf_size":20}`, state: "ready"},
		{name: "without it", state: "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			deployment := aLiveDeployment(t)
			deployment.installer.settle = settle

			installed, err := deployment.installer.Install(t.Context(), deployment.principal,
				httpapi.ModuleRelease{
					Module: theModulePath, Version: "1.0.0",
					From:     aReleaseOfSpools(t, requiredShelfManifest()),
					Approve:  true,
					Settings: httpapi.ResourceConfig(tc.settings),
				})
			require.NoError(t, err)

			require.Equal(t, tc.state, installed.State, installed.Reason)

			if tc.state == "failed" {
				require.Contains(t, installed.Reason, "shelf_size")
			}
		})
	}
}

// Settings that are not a JSON object stop the install before anything is
// written, on the path both callers share.
func TestSettingsThatAreNotAnObjectStopTheInstall(t *testing.T) {
	t.Parallel()

	installer, configPath := theInstaller(t, true)

	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0",
		From:     aRelease(t, requiredShelfManifest(), true),
		Settings: httpapi.ResourceConfig(`[1, 2]`),
	})
	require.Error(t, err)

	rows, err := resource.Installed(resource.Paths{File: configPath, Overlay: installer.core.Overlay})
	require.NoError(t, err)
	require.Empty(t, rows)
}

// secretManifest is a module with one credential among its settings.
func secretManifest() string {
	return strings.Replace(requiredShelfManifest(), "options:\n", strings.Join([]string{
		"options:",
		"  - key: token",
		"    type: secret",
		"    summary: The key the module signs its requests with.",
		"",
	}, "\n"), 1)
}

// A reinstall keeps a stored credential the way put-resource does. The marker a
// read answers in place of one means "keep it", and a reinstall that sends no
// settings keeps the whole block. The marker for a credential nothing stores is
// refused.
func TestAReinstallKeepsTheStoredCredential(t *testing.T) {
	t.Parallel()

	installer, configPath := theInstaller(t, true)

	install := func(settings string) error {
		_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
			Module: theModulePath, Version: "1.0.0",
			From:     aRelease(t, secretManifest(), true),
			Settings: httpapi.ResourceConfig(settings),
		})

		return err
	}

	require.Error(t, install(`{"shelf_size":20,"token":"`+resource.RedactedValue+`"}`),
		"the marker was stored as a credential nothing held")

	require.NoError(t, install(`{"shelf_size":20,"token":"s3cret"}`))

	require.NoError(t, install(`{"shelf_size":30,"token":"`+resource.RedactedValue+`"}`))
	require.JSONEq(t, `{"shelf_size":30,"token":"s3cret"}`, declaredConfig(t, installer.core, configPath))

	require.NoError(t, install(""))
	require.JSONEq(t, `{"shelf_size":30,"token":"s3cret"}`, declaredConfig(t, installer.core, configPath),
		"a reinstall with no settings wiped the stored ones")
}

// A reinstall carries a stored value only where the new declaration masks it
// the way the old one did. Each case reinstalls with no settings, or with the
// marker, and the stored credential must not survive where a listing would
// read it in plain text. Another module under the same name is refused before
// anything is written, so the credential stays with the module that holds it.
func TestAReinstallDoesNotCarryACredentialPastItsMask(t *testing.T) {
	t.Parallel()

	const credential = "real-secret"

	for _, tc := range []struct {
		name     string
		module   string
		document string
		settings string
	}{
		{
			name: "the secret retyped as a string", module: theModulePath,
			document: strings.Replace(secretManifest(), "type: secret", "type: string", 1),
		},
		{name: "the option taken out", module: theModulePath, document: requiredShelfManifest()},
		{name: "another module under the same name", module: "github.com/evil/spools", document: secretManifest()},
		{
			name: "the marker sent to another module", module: "github.com/evil/spools",
			document: secretManifest(), settings: `{"shelf_size":20,"token":"` + resource.RedactedValue + `"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			installer, configPath := theInstaller(t, true)

			_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
				Module: theModulePath, Version: "1.0.0",
				From:     aRelease(t, secretManifest(), true),
				Settings: httpapi.ResourceConfig(`{"shelf_size":20,"token":"` + credential + `"}`),
			})
			require.NoError(t, err)

			_, err = installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
				Module: tc.module, Version: "1.0.0",
				From:     aRelease(t, tc.document, true),
				Settings: httpapi.ResourceConfig(tc.settings),
			})
			if tc.module != theModulePath {
				require.Error(t, err, "another module replaced the one holding the credential")
				require.Contains(t, declaredConfig(t, installer.core, configPath), credential)

				return
			}

			require.NoError(t, err)
			require.NotContains(t, declaredConfig(t, installer.core, configPath), credential)
		})
	}
}
