package app

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
)

// switchModulesOff writes the operator's key into the file installConfig
// wrote, as the last key of the core block.
func switchModulesOff(t *testing.T, configPath string) {
	t.Helper()

	file, err := os.OpenFile(configPath, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = file.WriteString("  modules_enabled: false\n")
	require.NoError(t, err)
	require.NoError(t, file.Close())
}

func modulesOff(installer *moduleInstaller) {
	off := false
	installer.core.ModulesEnabled = &off
}

// TestNothingIsInstalledWhileModulesAreOff holds ADR-20261019-h76 rule 5 on both
// doors of an install: a release on this server and one the marketplace
// offers. Nothing is fetched and nothing is written.
func TestNothingIsInstalledWhileModulesAreOff(t *testing.T) {
	t.Parallel()

	market := newFakeMarketplace(t, archiveBytes(t, bothHalvesManifest()), "1.0.0")
	installer, configPath := marketInstaller(t, market.server.URL)
	modulesOff(installer)

	for _, release := range []httpapi.ModuleRelease{
		{Module: theModulePath, Version: "1.0.0", From: aRelease(t, bothHalvesManifest(), true), Approve: true},
		{Module: theModulePath, Version: "1.0.0", Approve: true},
	} {
		_, err := installer.Install(t.Context(), "user:operator", release)
		requireCode(t, err, httpfault.CodeModulesDisabled())
	}

	require.Zero(t, market.lists(), "the marketplace was asked while modules are off")

	rows, err := resource.Installed(resource.Paths{File: configPath, Overlay: installer.core.Overlay})
	require.NoError(t, err)
	require.Empty(t, rows)
	require.NoDirExists(t, filepath.Join(installer.core.Modules, "spools"))
}

// TestTheCatalogueIsNotReadWhileModulesAreOff holds ADR-20261019-h76 rule 6: the
// index and both marketplace operations answer MODULES_DISABLED, the
// marketplace switch stores nothing, and the marketplace is never asked.
func TestTheCatalogueIsNotReadWhileModulesAreOff(t *testing.T) {
	t.Parallel()

	market := newFakeMarketplace(t, archiveBytes(t, bothHalvesManifest()), "1.0.0")
	toggle := &memorySwitch{}
	installer := switchedInstaller(t, market.server.URL, toggle)
	modulesOff(installer)

	_, err := installer.Index(t.Context(), "user:operator")
	requireCode(t, err, httpfault.CodeModulesDisabled())

	_, _, err = installer.Marketplace(t.Context())
	requireCode(t, err, httpfault.CodeModulesDisabled())

	_, _, err = installer.SetMarketplace(t.Context(), true, nil)
	requireCode(t, err, httpfault.CodeModulesDisabled())

	_, recorded, err := toggle.MarketplaceEnabled()
	require.NoError(t, err)
	require.False(t, recorded, "the marketplace switch was stored while modules are off")
	require.Zero(t, market.lists())
}

// TestTheMarketplaceAnswersModulesDisabledWhereNoneIsNamed is rule 6 on a
// server that names no marketplace: the key's refusal comes before the 404 of
// a missing marketplace, so a client learns why nothing can be installed.
func TestTheMarketplaceAnswersModulesDisabledWhereNoneIsNamed(t *testing.T) {
	t.Parallel()

	installer, _ := theInstaller(t, true)
	require.Nil(t, installer.market, "the case needs a server that names no marketplace")
	modulesOff(installer)

	_, _, err := installer.Marketplace(t.Context())
	requireCode(t, err, httpfault.CodeModulesDisabled())

	_, _, err = installer.SetMarketplace(t.Context(), true, nil)
	requireCode(t, err, httpfault.CodeModulesDisabled())
}

// TestAnInstalledModuleIsRemovedWhileModulesAreOff holds ADR-20261019-h76 rule 12:
// a module installed before the key was set to false can still be removed.
func TestAnInstalledModuleIsRemovedWhileModulesAreOff(t *testing.T) {
	t.Parallel()

	installer, configPath := theInstaller(t, true)

	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", From: aRelease(t, bothHalvesManifest(), true), Approve: true,
	})
	require.NoError(t, err)

	modulesOff(installer)
	require.NoError(t, installer.Remove(t.Context(), "user:operator", theNamespace, false))

	rows, err := resource.Installed(resource.Paths{File: configPath, Overlay: installer.core.Overlay})
	require.NoError(t, err)
	require.Empty(t, rows)
}

// TestModuleAddIsRefusedAndRemoveWorksWhileModulesAreOff is the command's
// side of ADR-20261019-h76 rules 5 and 12, read from the operator's file.
func TestModuleAddIsRefusedAndRemoveWorksWhileModulesAreOff(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	addModuleFrom(t, configPath, theModulePath+"@1.0.0", aRelease(t, bothHalvesManifest(), true), "-approve")
	switchModulesOff(t, configPath)

	var out bytes.Buffer

	err := Module(t.Context(), &out, []string{
		addVerb, "-config", configPath, "-from", aRelease(t, bundleOnlyManifest(), false),
		"github.com/acme/fleet-stats@1.0.0",
	})
	requireCode(t, err, httpfault.CodeModulesDisabled())

	var notice bytes.Buffer

	require.NoError(t, Module(t.Context(), &notice, []string{listVerb, "-config", configPath}))
	require.True(t, strings.HasPrefix(notice.String(), "core.modules_enabled is false"), notice.String())

	require.NoError(t, Module(t.Context(), &out, []string{removeVerb, "-config", configPath, theModulePath}))
	require.NoDirExists(t, filepath.Join(core.Modules, "spools"))

	// With nothing left, the listing does not send the operator to the
	// command that was just refused.
	var listed bytes.Buffer

	require.NoError(t, Module(t.Context(), &listed, []string{listVerb, "-config", configPath}))
	require.Contains(t, listed.String(), "`module add` is refused")
}

// TestTheWizardLeavesTheModulesKeyOut holds ADR-20261019-h76 rule 4 by the key's
// name: the wizard does not offer it, and an answer that names it is refused,
// so a signed-in person cannot switch modules back on through the wizard.
func TestTheWizardLeavesTheModulesKeyOut(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("core:\n  site: shed\n"), 0o600))

	contract, err := setupContract(path)
	require.NoError(t, err)

	for _, key := range contract.Keys {
		require.NotEqual(t, "core.modules_enabled", key.Path, "the wizard offers the modules key")
	}

	_, err = setupDocument(map[string]any{"core.modules_enabled": true}, contract, nil)
	requireCode(t, err, httpfault.CodeRequestInvalid())
}

// TestAModuleInstalledEarlierIsNotServedOnceModulesAreOff is a deployment
// that installed a module and then restarted with modules off: the module is
// neither supervised nor mounted, the listing and readiness say why, and a
// restart names the key (ADR-20261019-h76 rules 9 to 11).
func TestAModuleInstalledEarlierIsNotServedOnceModulesAreOff(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	instance := theBundleInstance(t)

	before := aLiveDeploymentOver(t, configPath, core)
	require.True(t, before.install(t, true).Running, "the module never ran, so the case proves nothing")

	off := false
	core.ModulesEnabled = &off
	after := aLiveDeploymentOver(t, configPath, core)

	_, supervised := after.host.Status(instance)
	require.False(t, supervised, "the installed module was started")

	_, mounted := modulePlane{host: after.host}.Module(theBundleNamespace)
	require.False(t, mounted, "the installed module's bundle is served")

	listed, found := after.listed(t, instance)
	require.True(t, found)
	require.Nil(t, listed.UI, "the listing hands a page a bundle to import")
	require.Equal(t, "disabled", listed.Status.State)
	require.Len(t, listed.Status.Conditions, 1)
	require.Equal(t, resource.ReasonModulesDisabled, listed.Status.Conditions[0].Reason)

	declared, ok := after.store.Document().Find(instance)
	require.True(t, ok)
	require.Equal(t, "disabled", readiness{store: after.store, host: after.host}.state(declared))

	err := after.store.Restart(t.Context(), after.principal, instance)
	requireCode(t, err, httpfault.CodeModulesDisabled())

	// Rule 13: one warning at start names it as module remove takes it, and
	// says how to start it again. With modules on, nothing is logged.
	var warned bytes.Buffer

	on := true
	logModulesOff(t.Context(), slog.New(slog.NewTextHandler(&warned, nil)),
		config.Core{ModulesEnabled: &on}, before.store)
	require.Empty(t, warned.String())

	logModulesOff(t.Context(), slog.New(slog.NewTextHandler(&warned, nil)), core, after.store)
	require.Contains(t, warned.String(), "level=WARN")
	require.Contains(t, warned.String(), "modules=["+instance.String()+"]")
	require.Contains(t, warned.String(), "module remove")
	require.Contains(t, warned.String(), "set core.modules_enabled to true")

	// The name in the line is one module remove accepts.
	held, err := moduleNamed(after.store.Document().Instances, instance.String())
	require.NoError(t, err)
	require.Equal(t, instance, held.Ref)
}

// TestTheWarningLeavesOutAModuleSwitchedOffInTheOverlay is rule 13's
// exception: a module the overlay switches off would not start with modules
// on either, so the start advice would be wrong for it.
func TestTheWarningLeavesOutAModuleSwitchedOffInTheOverlay(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	off := false
	core.ModulesEnabled = &off

	require.NoError(t, os.MkdirAll(filepath.Dir(core.Overlay), 0o750))
	require.NoError(t, os.WriteFile(core.Overlay, []byte(`{
  "version": 1,
  "instances": [
    {"kind": "asset", "name": "spools", "type": "com.github.aux.acme.spools/spools", "runtime": "asset",
     "enabled": false, "required": false,
     "asset": {"path": "/opt/modules/spools/ui", "checksum": "sha256:`+strings.Repeat("0", 64)+`"}}
  ]
}
`), 0o600))

	held, closeGraph, err := openGraph(t.Context(), core, "0.0.0-test", discard())
	require.NoError(t, err)
	t.Cleanup(closeGraph)

	store, err := resource.New(resource.Config{
		Paths:      resource.Paths{File: configPath, Overlay: core.Overlay},
		Graph:      held.graph,
		Logger:     discard(),
		ModulesOff: true,
	})
	require.NoError(t, err)
	require.NoError(t, store.Reload(t.Context()))

	var logged bytes.Buffer

	logModulesOff(t.Context(), slog.New(slog.NewTextHandler(&logged, nil)), core, store)
	require.Contains(t, logged.String(), "level=INFO")
	require.NotContains(t, logged.String(), "asset:spools")
}

// TestARunningServerStartsNoInstalledModuleWhileModulesAreOff holds ADR-20261019-h76
// rules 9 to 11 over the real binary, so the wiring from the operator's key to
// the store is tested and not only the store. The same files with modules on
// try to start the module, so the key is what held it.
func TestARunningServerStartsNoInstalledModuleWhileModulesAreOff(t *testing.T) {
	t.Parallel()

	for _, on := range []bool{true, false} {
		t.Run(map[bool]string{true: "on", false: "off"}[on], func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			write(t, filepath.Join(dir, "config.d", "resources.json"), `{
  "version": 1,
  "instances": [
    {"kind": "asset", "name": "spools", "type": "com.github.aux.acme.spools/spools", "runtime": "asset",
     "enabled": true, "required": false,
     "asset": {"path": "`+filepath.Join(dir, "spools")+`", "checksum": "sha256:`+strings.Repeat("0", 64)+`"}}
  ]
}
`)

			file := lines("core:", "  log:", "    level: error")
			if !on {
				file = lines("core:", "  modules_enabled: false", "  log:", "    level: error")
			}

			running := start(t, dir, file)

			read := running.get(t, "/api/v1/resources/asset/spools")
			require.Equal(t, http.StatusOK, read.status, read.body)

			var listed httpapi.ResourceInstance
			require.NoError(t, json.Unmarshal([]byte(read.body), &listed), read.body)

			if on {
				require.NotEqual(t, "disabled", listed.Status.State, read.body)
				running.stopAndWait(t)

				return
			}

			require.Equal(t, "disabled", listed.Status.State, read.body)
			require.Len(t, listed.Status.Conditions, 1, read.body)
			// The literal, because the web app matches it and not the constant.
			require.Equal(t, "ModulesDisabled", listed.Status.Conditions[0].Reason)

			bundle := running.get(t, "/api/v1/modules/com.github.aux.acme.spools/ui/index.js")
			require.Equal(t, http.StatusNotFound, bundle.status, bundle.body)

			running.stopAndWait(t)
		})
	}
}

// TestTheServerSaysModulesAreOffWhenNoneIsInstalled is the other half of rule
// 13: an operator who reads the start-up log learns that the key is in force
// although no installed module is held.
func TestTheServerSaysModulesAreOffWhenNoneIsInstalled(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	off := false
	core.ModulesEnabled = &off
	deployment := aLiveDeploymentOver(t, configPath, core)

	var logged bytes.Buffer

	logModulesOff(t.Context(), slog.New(slog.NewTextHandler(&logged, nil)), core, deployment.store)
	require.Contains(t, logged.String(), "level=INFO")
	require.Contains(t, logged.String(), "core.modules_enabled is false")
	require.NotContains(t, logged.String(), "level=WARN")
}

// TestTheFirstRunLeavesAnInstalledModuleOffWhileModulesAreOff holds ADR-20261019-h76
// rule 8. The page is not offered the module, and a decision that names it,
// taken before the key was set, is applied without it rather than refused.
func TestTheFirstRunLeavesAnInstalledModuleOffWhileModulesAreOff(t *testing.T) {
	t.Parallel()

	instance, err := ref.New(ref.KindAsset, "spools")
	require.NoError(t, err)

	for _, on := range []bool{true, false} {
		t.Run(map[bool]string{true: "on", false: "off"}[on], func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "config.d"), 0o750))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "config.d", "resources.json"), []byte(`{
  "version": 1,
  "instances": [
    {"kind": "asset", "name": "spools", "type": "com.github.aux.acme.spools/spools", "runtime": "asset",
     "enabled": false, "required": false,
     "asset": {"path": "/opt/modules/spools/ui", "checksum": "sha256:`+strings.Repeat("0", 64)+`"}}
  ]
}
`), 0o600))

			install := startInstall(t, dir, filepath.Join(dir, "host.db"), func(core *config.Core) {
				core.ModulesEnabled = &on
			})

			state, readErr := install.first.State(t.Context())
			require.NoError(t, readErr)

			if on {
				require.Len(t, state.Modules, 1, "with modules on the page is offered the module")

				return
			}

			require.Empty(t, state.Modules, "the page is offered a module it may not switch on")

			_, recordErr := install.first.Record(t.Context(), httpapi.SetupDecision{
				Code:    testSetupCode,
				Mode:    resource.SetupOpen,
				Modules: []httpapi.SetupModuleChoice{{Instance: instance.String(), Enabled: true}},
			})
			require.NoError(t, recordErr, "a choice naming a held module stopped the decision")

			after, getErr := install.store.Get(t.Context(), "user:operator", instance)
			require.NoError(t, getErr)
			require.False(t, after.Enabled, "the first run switched an installed module on")
		})
	}
}

// TestTheFirstRunStillSwitchesAnInstalledModuleOff is the other half of rule
// 8: a decision to switch a held module off is applied, so the module stays off
// once the operator turns modules on again.
func TestTheFirstRunStillSwitchesAnInstalledModuleOff(t *testing.T) {
	t.Parallel()

	instance, err := ref.New(ref.KindAsset, "spools")
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "config.d"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.d", "resources.json"), []byte(`{
  "version": 1,
  "instances": [
    {"kind": "asset", "name": "spools", "type": "com.github.aux.acme.spools/spools", "runtime": "asset",
     "enabled": true, "required": false,
     "asset": {"path": "/opt/modules/spools/ui", "checksum": "sha256:`+strings.Repeat("0", 64)+`"}}
  ]
}
`), 0o600))

	off := false
	install := startInstall(t, dir, filepath.Join(dir, "host.db"), func(core *config.Core) {
		core.ModulesEnabled = &off
	})

	_, err = install.first.Record(t.Context(), httpapi.SetupDecision{
		Code:    testSetupCode,
		Mode:    resource.SetupOpen,
		Modules: []httpapi.SetupModuleChoice{{Instance: instance.String(), Enabled: false}},
	})
	require.NoError(t, err)

	after, err := install.store.Get(t.Context(), "user:operator", instance)
	require.NoError(t, err)
	require.False(t, after.Enabled, "the decision to switch the module off was dropped")
}

// TestARootMountSaysModulesAreOff holds ADR-20261019-h76 rule 10 at a root prefix: an
// integration that calls the address an installed module claimed is told
// that the operator switched modules off, and not that the module is broken.
func TestARootMountSaysModulesAreOff(t *testing.T) {
	t.Parallel()

	for _, on := range []bool{true, false} {
		t.Run(map[bool]string{true: "on", false: "off"}[on], func(t *testing.T) {
			t.Parallel()

			configPath, core := installConfig(t)
			core.ModulesEnabled = &on

			require.NoError(t, os.MkdirAll(filepath.Dir(core.Overlay), 0o750))
			require.NoError(t, os.WriteFile(core.Overlay, []byte(`{
  "version": 1,
  "instances": [
    {"kind": "sink", "name": "graphql", "type": "com.github.aux.acme.graphql/graphql", "runtime": "service",
     "enabled": true, "required": false,
     "service": {"path": "/opt/modules/graphql", "checksum": "sha256:`+strings.Repeat("0", 64)+`",
                 "contract_major": 1, "mount": "/graphql"}}
  ]
}
`), 0o600))

			held, closeGraph, err := openGraph(t.Context(), core, "0.0.0-test", discard())
			require.NoError(t, err)
			t.Cleanup(closeGraph)

			store, err := resource.New(resource.Config{
				Paths:      resource.Paths{File: configPath, Overlay: core.Overlay},
				Graph:      held.graph,
				Logger:     discard(),
				ModulesOff: !core.ModulesOn(),
			})
			require.NoError(t, err)
			require.NoError(t, store.Reload(t.Context()))

			surface, found := modulePlane{store: store}.declaredRoot("/graphql/query")
			require.True(t, found, "the declared prefix answered as unknown")

			require.Equal(t, !on, surface.ModulesOff, "the prefix names the wrong reason")
		})
	}
}

// TestThePlaneHoldsAnInstalledModuleWhileModulesAreOff pins the plane's half of
// the second check: a plane built with modulesOff skips a start the store would
// never plan (ADR-20261019-h76 rule 9). serve passes it the key, and no test can see
// that line work, because the store's mask already holds every such module.
func TestThePlaneHoldsAnInstalledModuleWhileModulesAreOff(t *testing.T) {
	t.Parallel()

	metrics, err := newObserved()
	require.NoError(t, err)

	// A working authorizer, so a regression that starts the module fails the
	// assertion below rather than panicking where the hub publishes its status.
	host := testHost(t)
	hub, err := stream.New(stream.Options{Site: "local", Authorizer: visibility{graph: host.graph}, Logger: discard()})
	require.NoError(t, err)
	t.Cleanup(hub.Close)

	supervision, err := startPlane(
		t.Context(),
		discard(),
		hub,
		metrics,
		t.TempDir(),
		true,
		false,
		install.SwitchOff{},
		nil,
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = supervision.shutdown(context.WithoutCancel(t.Context()), discard(), time.Second) })

	instance, err := ref.New(ref.KindAsset, "spools")
	require.NoError(t, err)

	installed := resource.Declaration{
		Ref: instance,
		Spec: resource.Spec{
			Type: "com.github.aux.acme.spools/spools", Enabled: true, Runtime: install.RuntimeAsset,
			Asset: &install.Asset{Path: t.TempDir(), Checksum: "sha256:" + strings.Repeat("0", 64)},
		},
		Source:     resource.SourceOverlay,
		Generation: 1,
	}

	require.NoError(t, supervision.host.Reconcile(t.Context(), resource.Plan{Steps: []resource.Step{
		{Outcome: resource.OutcomeStart, Ref: instance, Desired: installed},
	}}))

	_, supervised := supervision.host.Status(instance)
	require.False(t, supervised, "the plane started an installed module while modules are off")
}

// TestTheCommandsSpeakOnlyOfWhatIsHeld covers two texts of the commands while
// modules are off: `module list` has no notice when every row is the
// operator's own, and refusing to remove such a row does not advise an install
// the server refuses.
func TestTheCommandsSpeakOnlyOfWhatIsHeld(t *testing.T) {
	t.Parallel()

	configPath, _ := installConfig(t)
	switchModulesOff(t, configPath)

	file, err := os.OpenFile(configPath, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = file.WriteString(lines("resources:", "  asset:", "    stats:",
		"      type: com.github.aux.acme.stats/stats", "      asset:", "        path: /opt/modules/stats/ui",
		"        checksum: sha256:"+strings.Repeat("0", 64)))
	require.NoError(t, err)
	require.NoError(t, file.Close())

	var listed bytes.Buffer

	require.NoError(t, Module(t.Context(), &listed, []string{listVerb, "-config", configPath}))
	require.Contains(t, listed.String(), "asset:stats")
	require.NotContains(t, listed.String(), "core.modules_enabled is false")

	var out bytes.Buffer

	err = Module(t.Context(), &out, []string{removeVerb, "-config", configPath, "asset:stats"})
	require.ErrorContains(t, err, "Remove the block there")
	require.NotContains(t, err.Error(), "install")
}

// TestTheAppIsToldWhetherModulesAreOn holds ADR-20261019-h76 rule 15 over the real
// binary: GET /api/v1/ui says modules are on by default and off when the
// operator's file switches them off.
func TestTheAppIsToldWhetherModulesAreOn(t *testing.T) {
	t.Parallel()

	for _, step := range []struct {
		name string
		file string
		on   bool
	}{
		{"no key", lines("core:", "  log:", "    level: error"), true},
		{"switched off", lines("core:", "  modules_enabled: false", "  log:", "    level: error"), false},
	} {
		t.Run(step.name, func(t *testing.T) {
			t.Parallel()

			running := start(t, t.TempDir(), step.file)

			read := running.get(t, "/api/v1/ui")
			require.Equal(t, http.StatusOK, read.status, read.body)

			var settings httpapi.AppSettings
			require.NoError(t, json.Unmarshal([]byte(read.body), &settings), read.body)
			require.Equal(t, step.on, settings.Modules, read.body)

			running.stopAndWait(t)
		})
	}
}
