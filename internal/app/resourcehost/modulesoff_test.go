package resourcehost_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	configinstall "github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// TestTheHostStartsNoInstalledModuleWhileModulesAreOff is the second check of
// ADR-20261019-h76 rule 9. The store never plans the start, and a plan that asks for
// one anyway still starts nothing. The same plan with modules on supervises
// the instance, so the key is what held it.
func TestTheHostStartsNoInstalledModuleWhileModulesAreOff(t *testing.T) {
	t.Parallel()

	instance, err := ref.New(ref.KindAsset, "spools")
	require.NoError(t, err)

	installed := resource.Declaration{
		Ref: instance,
		Spec: resource.Spec{
			Type: "com.github.aux.acme.spools/spools", Enabled: true, Runtime: configinstall.RuntimeAsset,
			Asset: &configinstall.Asset{Path: t.TempDir(), Checksum: "sha256:" + strings.Repeat("0", 64)},
		},
		Source:     resource.SourceOverlay,
		Generation: 1,
	}
	start := resource.Plan{Steps: []resource.Step{{Outcome: resource.OutcomeStart, Ref: instance, Desired: installed}}}

	off := newWiredWith(t, nil, func(cfg *resourcehost.Config) { cfg.ModulesOff = true })
	require.NoError(t, off.host.Reconcile(t.Context(), start))

	_, supervised := off.host.Status(instance)
	require.False(t, supervised, "an installed module was started while modules are off")

	on := newWired(t)
	require.NoError(t, on.host.Reconcile(t.Context(), start))

	_, supervised = on.host.Status(instance)
	require.True(t, supervised, "the same plan with modules on started nothing, so the case proves nothing")
}

// TestTheHostStillStartsWhatTheOperatorDeclaresWhileModulesAreOff holds the
// scope of the same check, ADR-20261019-h76 rule 3: an asset module the operator's file
// declares and a builtin instance are started with modules off.
func TestTheHostStillStartsWhatTheOperatorDeclaresWhileModulesAreOff(t *testing.T) {
	t.Parallel()

	stats, err := ref.New(ref.KindAsset, "stats")
	require.NoError(t, err)

	declared := resource.Declaration{
		Ref: stats,
		Spec: resource.Spec{
			Type: "com.github.aux.acme.stats/stats", Enabled: true, Runtime: configinstall.RuntimeAsset,
			Asset: &configinstall.Asset{Path: t.TempDir(), Checksum: "sha256:" + strings.Repeat("0", 64)},
		},
		Source:     resource.SourceFile,
		Generation: 1,
	}
	builtin := resource.Declaration{
		Ref:        theSink(t),
		Spec:       resource.Spec{Type: "counted", Enabled: true},
		Source:     resource.SourceOverlay,
		Generation: 1,
	}

	off := newWiredWith(t, nil, func(cfg *resourcehost.Config) { cfg.ModulesOff = true })
	require.NoError(t, off.host.Reconcile(t.Context(), resource.Plan{Steps: []resource.Step{
		{Outcome: resource.OutcomeStart, Ref: stats, Desired: declared},
		{Outcome: resource.OutcomeStart, Ref: builtin.Ref, Desired: builtin},
	}}))

	for _, instance := range []ref.Ref{stats, builtin.Ref} {
		_, supervised := off.host.Status(instance)
		require.True(t, supervised, "%s was held back although the operator declared it", instance)
	}
}

// TestSafeModeHoldsTheModuleTheOperatorDeclared is ADR-20261030-7e2 in the
// host: the same plan that starts the operator's asset module with modules
// off starts nothing in safe mode, while a builtin instance still starts.
func TestSafeModeHoldsTheModuleTheOperatorDeclared(t *testing.T) {
	t.Parallel()

	stats, err := ref.New(ref.KindAsset, "stats")
	require.NoError(t, err)

	declared := resource.Declaration{
		Ref: stats,
		Spec: resource.Spec{
			Type: "com.github.aux.acme.stats/stats", Enabled: true, Runtime: configinstall.RuntimeAsset,
			Asset: &configinstall.Asset{Path: t.TempDir(), Checksum: "sha256:" + strings.Repeat("0", 64)},
		},
		Source:     resource.SourceFile,
		Generation: 1,
	}
	builtin := resource.Declaration{
		Ref:        theSink(t),
		Spec:       resource.Spec{Type: "counted", Enabled: true},
		Source:     resource.SourceOverlay,
		Generation: 1,
	}

	safe := newWiredWith(t, nil, func(cfg *resourcehost.Config) {
		cfg.ModulesOff = true
		cfg.SafeMode = true
	})
	require.NoError(t, safe.host.Reconcile(t.Context(), resource.Plan{Steps: []resource.Step{
		{Outcome: resource.OutcomeStart, Ref: stats, Desired: declared},
		{Outcome: resource.OutcomeStart, Ref: builtin.Ref, Desired: builtin},
	}}))

	_, supervised := safe.host.Status(stats)
	require.False(t, supervised, "the operator's module was started in safe mode")

	_, supervised = safe.host.Status(builtin.Ref)
	require.True(t, supervised, "safe mode held a builtin instance")
}
