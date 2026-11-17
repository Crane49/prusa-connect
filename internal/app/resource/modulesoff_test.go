package resource_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

const (
	spoolsType = "com.github.aux.acme.spools/spools"
	statsType  = "com.github.aux.acme.stats/stats"
)

// installedSpools is the overlay an install writes for an asset module.
func installedSpools(enabled string) string {
	return `{
  "version": 1,
  "instances": [
    {"kind": "asset", "name": "spools", "type": "` + spoolsType + `", "runtime": "asset",
     "enabled": ` + enabled + `, "required": false,
     "asset": {"path": "/opt/modules/spools/ui", "checksum": "sha256:` + installed + `"}}
  ]
}
`
}

// operatorsStats is an asset module the operator declares in their own file.
const operatorsStats = `
resources:
  asset:
    stats:
      type: ` + statsType + `
      asset:
        path: /opt/modules/stats/ui
        checksum: sha256:` + installed + `
`

func modulesOff(cfg *resource.Config) { cfg.ModulesOff = true }

func safeMode(cfg *resource.Config) {
	cfg.ModulesOff = true
	cfg.SafeMode = true
}

// started is every instance a plan asked the plane to run.
func (p *recordingPlane) started() []ref.Ref {
	p.mu.Lock()
	defer p.mu.Unlock()

	var out []ref.Ref

	for _, step := range p.steps {
		if step.Outcome != resource.OutcomeStop && step.Desired.Spec.Enabled {
			out = append(out, step.Ref)
		}
	}

	return out
}

// TestAnInstalledModuleNeverStartsWhileModulesAreOff holds ADR-20261019-h76 rules 3,
// 9 and 11: the module the server installed is not started and the listing
// says why, and the module the operator declared runs.
func TestAnInstalledModuleNeverStartsWhileModulesAreOff(t *testing.T) {
	t.Parallel()

	spools := mustRef(t, ref.KindAsset, "spools")
	stats := mustRef(t, ref.KindAsset, "stats")

	h := newHarness(t, modulesOff)
	h.writeFile(operatorsStats)
	h.writeOverlayByHand(installedSpools("true"))
	require.NoError(t, h.store.Reload(t.Context()))

	require.Equal(t, []ref.Ref{stats}, h.plane.started())

	listed, err := h.get(spools)
	require.NoError(t, err)
	require.True(t, listed.Enabled, "the listing must keep the flag the overlay holds")
	require.Equal(t, condition.StateDisabled().String(), listed.Status.State)
	require.False(t, listed.Status.Serving)
	require.Len(t, listed.Status.Conditions, 1)
	require.Equal(t, resource.ReasonModulesDisabled, listed.Status.Conditions[0].Reason)
	require.Equal(t, condition.StatusFalse.String(), listed.Status.Conditions[0].Status)
	require.Equal(t, listed.Generation, listed.Status.Conditions[0].ObservedGeneration)
	require.False(t, listed.Status.Conditions[0].LastTransitionTime.IsZero())

	// The same files with modules on start both, so the key is what held it.
	on := newHarness(t)
	on.writeFile(operatorsStats)
	on.writeOverlayByHand(installedSpools("true"))
	require.NoError(t, on.store.Reload(t.Context()))
	require.ElementsMatch(t, []ref.Ref{spools, stats}, on.plane.started())
}

// TestAModuleThatMovesFromTheFileToTheOverlayIsNotStarted is the way round a
// check made only at install: the operator's file shadows an overlay entry,
// and removing the block from the file puts the installed one in effect while
// the server runs.
func TestAModuleThatMovesFromTheFileToTheOverlayIsNotStarted(t *testing.T) {
	t.Parallel()

	spools := mustRef(t, ref.KindAsset, "spools")

	h := newHarness(t, modulesOff)
	h.writeFile(`
resources:
  asset:
    spools:
      type: ` + spoolsType + `
      asset:
        path: /opt/modules/spools/ui
        checksum: sha256:` + installed + `
`)
	h.writeOverlayByHand(installedSpools("true"))
	require.NoError(t, h.store.Reload(t.Context()))
	require.Equal(t, []ref.Ref{spools}, h.plane.started(), "the operator's own block did not start")

	h.writeFile("resources: {}\n")
	require.NoError(t, h.store.Reload(t.Context()))

	require.Equal(t, []ref.Ref{spools}, h.plane.started(), "the installed module was started")

	_, running := h.plane.Status(spools)
	require.False(t, running, "the operator's block was not stopped when the installed one took its place")
}

// TestAHeldModuleIsNotStoppedAgainOnEveryReload pins that the mask covers
// both sides of the diff. Masking only the new side would read every reload as
// a switch from on to off, and plan a stop each time.
func TestAHeldModuleIsNotStoppedAgainOnEveryReload(t *testing.T) {
	t.Parallel()

	spools := mustRef(t, ref.KindAsset, "spools")

	h := newHarness(t, modulesOff)
	h.writeOverlayByHand(installedSpools("true"))
	require.NoError(t, h.store.Reload(t.Context()))

	h.plane.mu.Lock()
	before := len(h.plane.steps)
	h.plane.mu.Unlock()

	require.NoError(t, h.store.Reload(t.Context()))

	h.plane.mu.Lock()
	defer h.plane.mu.Unlock()

	for _, step := range h.plane.steps[before:] {
		require.NotEqual(t, spools, step.Ref, "a reload with nothing changed planned %s", step.Outcome)
	}
}

// TestOnlySwitchingAnInstalledModuleOnIsRefused holds ADR-20261019-h76 rule 7: the
// write that turns it on is refused, and a write that keeps it as it is or
// turns it off goes through, because the web app sends enabled with every save.
func TestOnlySwitchingAnInstalledModuleOnIsRefused(t *testing.T) {
	t.Parallel()

	spools := mustRef(t, ref.KindAsset, "spools")

	off := newHarness(t, modulesOff)
	off.writeOverlayByHand(installedSpools("false"))
	require.NoError(t, off.store.Reload(t.Context()))

	_, err := off.update(spools, httpapi.ResourceSpec{Type: spoolsType, Enabled: true}, off.version())
	require.True(t, fault.HasCode(err, httpfault.CodeModulesDisabled()), "want MODULES_DISABLED, got %v", err)

	_, err = off.update(spools, httpapi.ResourceSpec{
		Type: spoolsType, HiddenContributions: []string{"pages/spools"},
	}, off.version())
	require.NoError(t, err, "a settings save of a switched-off module was refused")

	on := newHarness(t, modulesOff)
	on.writeOverlayByHand(installedSpools("true"))
	require.NoError(t, on.store.Reload(t.Context()))

	_, err = on.update(spools, httpapi.ResourceSpec{
		Type: spoolsType, Enabled: true, HiddenContributions: []string{"pages/spools"},
	}, on.version())
	require.NoError(t, err, "a settings save that keeps the module enabled was refused")

	_, err = on.update(spools, httpapi.ResourceSpec{Type: spoolsType}, on.version())
	require.NoError(t, err, "switching the module off was refused")
	require.Empty(t, on.plane.started())
}

// TestAnInstalledModuleIsNotRestartedWhileModulesAreOff names the key in
// the refusal, where the plane would have said the module is not running.
func TestAnInstalledModuleIsNotRestartedWhileModulesAreOff(t *testing.T) {
	t.Parallel()

	h := newHarness(t, modulesOff)
	h.writeOverlayByHand(installedSpools("true"))
	require.NoError(t, h.store.Reload(t.Context()))

	err := h.store.Restart(t.Context(), owner, mustRef(t, ref.KindAsset, "spools"))
	require.True(t, fault.HasCode(err, httpfault.CodeModulesDisabled()), "want MODULES_DISABLED, got %v", err)
}

// TestOnlyAModuleTheServerWroteIsInstalled holds ADR-20261019-h76 rules 2 and 3 on the
// predicate itself: a service or an asset in the overlay is installed, and the
// same runtime in the operator's file, a script or a builtin is not.
func TestOnlyAModuleTheServerWroteIsInstalled(t *testing.T) {
	t.Parallel()

	for _, step := range []struct {
		name      string
		runtime   install.Runtime
		source    resource.Source
		installed bool
	}{
		{"asset in the overlay", install.RuntimeAsset, resource.SourceOverlay, true},
		{"service in the overlay", install.RuntimeService, resource.SourceOverlay, true},
		{"asset in the operator's file", install.RuntimeAsset, resource.SourceFile, false},
		{"script in the overlay", install.RuntimeScript, resource.SourceOverlay, false},
		{"builtin in the overlay", install.RuntimeBuiltin, resource.SourceOverlay, false},
	} {
		t.Run(step.name, func(t *testing.T) {
			t.Parallel()

			declared := resource.Declaration{Spec: resource.Spec{Runtime: step.runtime}, Source: step.source}
			require.Equal(t, step.installed, declared.Installed())
		})
	}
}

// TestSafeModeHoldsModulesTheOperatorDeclared holds ADR-20261030-7e2: safe mode
// keeps off every module, from the overlay or from the operator's file, and
// leaves a script instance alone.
func TestSafeModeHoldsModulesTheOperatorDeclared(t *testing.T) {
	t.Parallel()

	stats := mustRef(t, ref.KindAsset, "stats")
	farm := mustRef(t, ref.KindSink, "farm")

	h := newHarness(t, safeMode)
	h.writeFile(operatorsFarm + operatorsStats[len("\nresources:\n"):] + `  storage:
    local:
      type: filesystem
      config:
        root: /var/lib/prusa
`)
	require.NoError(t, h.store.Reload(t.Context()))

	require.Equal(t, []ref.Ref{mustRef(t, ref.KindStorage, "local")}, h.plane.started(),
		"safe mode started a module, or held an instance that is not one")

	for _, declared := range []ref.Ref{stats, farm} {
		listed, err := h.get(declared)
		require.NoError(t, err)
		require.True(t, listed.Enabled, "the listing must keep the operator's flag")
		require.Equal(t, condition.StateDisabled().String(), listed.Status.State)
		require.Len(t, listed.Status.Conditions, 1)
		require.Equal(t, resource.ReasonSafeMode, listed.Status.Conditions[0].Reason)
		require.Equal(t, condition.StatusFalse.String(), listed.Status.Conditions[0].Status)
	}

	script := resource.Declaration{Spec: resource.Spec{Runtime: install.RuntimeScript}, Source: resource.SourceFile}
	require.False(t, h.store.Held(script), "safe mode held a script")
	require.True(
		t,
		h.store.Held(
			resource.Declaration{Spec: resource.Spec{Runtime: install.RuntimeService}, Source: resource.SourceFile},
		),
	)
	require.True(
		t,
		h.store.Held(
			resource.Declaration{Spec: resource.Spec{Runtime: install.RuntimeAsset}, Source: resource.SourceFile},
		),
	)
	require.True(t, h.store.SafeMode())
}

// TestSafeModeRefusesWithItsOwnCode pins that a held module's restart and a
// switch-on answer SAFE_MODE, and not the code of the operator's key.
func TestSafeModeRefusesWithItsOwnCode(t *testing.T) {
	t.Parallel()

	spools := mustRef(t, ref.KindAsset, "spools")
	stats := mustRef(t, ref.KindAsset, "stats")

	h := newHarness(t, safeMode)
	h.writeFile(operatorsStats)
	h.writeOverlayByHand(installedSpools("false"))
	require.NoError(t, h.store.Reload(t.Context()))

	err := h.store.Restart(t.Context(), owner, stats)
	require.True(t, fault.HasCode(err, httpfault.CodeSafeMode()), "restart: want SAFE_MODE, got %v", err)

	_, err = h.update(spools, httpapi.ResourceSpec{Type: spoolsType, Enabled: true}, h.version())
	require.True(t, fault.HasCode(err, httpfault.CodeSafeMode()), "switch on: want SAFE_MODE, got %v", err)
}

// TestModulesOffStillLeavesDeclaredModulesRunning guards ADR-20261019-h76 rule 11:
// without safe mode the operator's own module runs.
func TestModulesOffStillLeavesDeclaredModulesRunning(t *testing.T) {
	t.Parallel()

	h := newHarness(t, modulesOff)
	h.writeFile(operatorsStats)
	require.NoError(t, h.store.Reload(t.Context()))

	require.Equal(t, []ref.Ref{mustRef(t, ref.KindAsset, "stats")}, h.plane.started())
	require.False(t, h.store.SafeMode())
}
