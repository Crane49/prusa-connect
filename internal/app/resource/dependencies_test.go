package resource_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The resolver of ADR-20261027-e6k. A, B and C are modules: A and C require B
// at the ranges each case names.

// module is one installed module as the resolver reads it.
func module(t *testing.T, name, version string, enabled bool, requires ...string) resource.Declaration {
	t.Helper()

	entry := install.ModuleEntry{Version: version}

	for index := 0; index+1 < len(requires); index += 2 {
		entry.Requires = append(entry.Requires, install.Requirement{
			Module: "com.github.aux.acme." + requires[index], Version: requires[index+1],
		})
	}

	return resource.Declaration{
		Ref:    mustRef(t, ref.KindSink, name),
		Source: resource.SourceOverlay,
		Spec: resource.Spec{
			Type:    "com.github.aux.acme." + name + "/" + name,
			Enabled: enabled,
			Runtime: install.RuntimeService,
			Module:  &entry,
		},
	}
}

func TestTwoRangesOneReleaseMeetsAreBothMet(t *testing.T) {
	t.Parallel()

	b := module(t, "b", "1.6.0", true)
	a := module(t, "a", "1.0.0", true, "b", "^1.2")
	c := module(t, "c", "1.0.0", true, "b", "^1.4")

	resolved := resource.Resolve([]resource.Declaration{a, b, c})
	require.Empty(t, resolved.Unmet())

	held, found := resolved.Module(a.Ref)
	require.True(t, found)
	require.Equal(t, "1.6.0", held.Requires[0].Installed)
	require.Equal(t, b.Ref, held.Requires[0].Instance)
}

func TestARemovalADependantNeedsIsRefused(t *testing.T) {
	t.Parallel()

	b := module(t, "b", "1.0.0", true)
	a := module(t, "a", "1.0.0", false, "b", "^1")

	err := resource.RefuseRemoval([]resource.Declaration{a, b}, b)
	require.True(t, fault.HasCode(err, httpfault.CodeModuleRequiredBy()), "want MODULE_REQUIRED_BY, got %v", err)
	require.Contains(t, err.Error(), "com.github.aux.acme.a requires com.github.aux.acme.b ^1",
		"a switched-off dependant still blocks a removal")

	// A suggestion never blocks a removal.
	suggesting := module(t, "a", "1.0.0", true)
	suggesting.Spec.Module.Suggests = []install.Suggestion{{Module: "com.github.aux.acme.b", Reason: "plans"}}
	require.NoError(t, resource.RefuseRemoval([]resource.Declaration{suggesting, b}, b))
}

func TestSwitchingOffADependencyIsRefused(t *testing.T) {
	t.Parallel()

	b := module(t, "b", "1.0.0", true)
	a := module(t, "a", "1.0.0", true, "b", "^1")
	before := []resource.Declaration{a, b}

	off := b
	off.Spec.Enabled = false

	err := resource.CheckChange(before, resource.Replaced(before, off), b.Ref)
	require.True(t, fault.HasCode(err, httpfault.CodeModuleRequiredBy()), "want MODULE_REQUIRED_BY, got %v", err)
	require.Contains(t, err.Error(), "com.github.aux.acme.b is switched off")
}

func TestAnUpdateOutOfADependantsRangeIsRefused(t *testing.T) {
	t.Parallel()

	b := module(t, "b", "1.6.0", true)
	c := module(t, "c", "1.0.0", true, "b", "^1.4")
	before := []resource.Declaration{b, c}

	err := resource.CheckChange(before, resource.Replaced(before, module(t, "b", "2.0.0", true)), b.Ref)
	require.True(t, fault.HasCode(err, httpfault.CodeModuleRequiredBy()), "want MODULE_REQUIRED_BY, got %v", err)
	require.Contains(
		t,
		err.Error(),
		"com.github.aux.acme.c needs com.github.aux.acme.b ^1.4, and com.github.aux.acme.b is 2.0.0",
	)

	require.NoError(t, resource.CheckChange(before, resource.Replaced(before, module(t, "b", "1.7.0", true)), b.Ref))
}

func TestSwitchingOnAModuleWhoseDependencyIsMissingIsRefused(t *testing.T) {
	t.Parallel()

	a := module(t, "a", "1.0.0", false, "b", "^1")
	on := module(t, "a", "1.0.0", true, "b", "^1")

	err := resource.CheckChange([]resource.Declaration{a}, []resource.Declaration{on}, a.Ref)
	require.True(t, fault.HasCode(err, httpfault.CodeModuleDependencyMissing()),
		"want MODULE_DEPENDENCY_MISSING, got %v", err)
	require.Contains(t, err.Error(), "com.github.aux.acme.b is not installed")
}

func TestARemovalNamesTheModulesItLeavesUnused(t *testing.T) {
	t.Parallel()

	planner := module(t, "planner", "1.0.0", true, "slicer", "^1")
	slicer := module(t, "slicer", "1.0.0", true, "threemf", "^1")
	slicer.Spec.Module.Pulled = true
	threemf := module(t, "threemf", "1.0.0", true)
	threemf.Spec.Module.Pulled = true
	explicit := module(t, "parts", "1.0.0", true)
	other := module(t, "other", "1.0.0", true, "threemf", "^1")

	unused := resource.Unused([]resource.Declaration{planner, slicer, threemf, explicit}, planner.Ref)
	require.Len(t, unused, 2)
	require.Equal(t, slicer.Ref, unused[0].Ref, "the slicer goes before the module it requires")
	require.Equal(t, threemf.Ref, unused[1].Ref)

	kept := resource.Unused([]resource.Declaration{planner, slicer, threemf, other}, planner.Ref)
	require.Len(t, kept, 1, "a module another module still requires is not unused")
	require.Equal(t, slicer.Ref, kept[0].Ref)
}

// TestSwitchingOffThroughTheResourceAPIIsRefused is the same rule through
// put-resource and delete-resource, which is how the Extensions page switches
// a module on and off.
func TestSwitchingOffThroughTheResourceAPIIsRefused(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.writeOverlayByHand(dependencyOverlay(true, true))
	require.NoError(t, h.store.Reload(t.Context()))

	consumer := mustRef(t, ref.KindAsset, "consumer")

	listed, err := h.get(consumer)
	require.NoError(t, err)
	require.NotNil(t, listed.Module)
	require.True(t, listed.Module.Requires[0].Met)
	require.Equal(t, "1.2.0", listed.Module.Requires[0].Installed)

	provider, err := h.get(mustRef(t, ref.KindAsset, "provider"))
	require.NoError(t, err)
	require.Len(t, provider.Module.RequiredBy, 1)
	require.Equal(t, "asset:consumer", provider.Module.RequiredBy[0].Instance)

	_, err = h.update(mustRef(t, ref.KindAsset, "provider"),
		httpapi.ResourceSpec{Type: providerType, Enabled: false}, h.version())
	require.True(t, fault.HasCode(err, httpfault.CodeModuleRequiredBy()), "want MODULE_REQUIRED_BY, got %v", err)

	_, err = h.update(consumer, httpapi.ResourceSpec{Type: consumerType, Enabled: false}, h.version())
	require.NoError(t, err)

	// Switched off, the consumer still requires the provider, so it cannot go.
	err = h.store.Delete(t.Context(), owner, mustRef(t, ref.KindAsset, "provider"),
		httpapi.Precondition{IfMatch: []string{h.version()}})
	require.True(t, fault.HasCode(err, httpfault.CodeModuleRequiredBy()), "want MODULE_REQUIRED_BY, got %v", err)

	_, err = h.update(mustRef(t, ref.KindAsset, "provider"),
		httpapi.ResourceSpec{Type: providerType, Enabled: false}, h.version())
	require.NoError(t, err)

	_, err = h.update(consumer, httpapi.ResourceSpec{Type: consumerType, Enabled: true}, h.version())
	require.True(t, fault.HasCode(err, httpfault.CodeModuleDependencyMissing()),
		"want MODULE_DEPENDENCY_MISSING, got %v", err)
}

const (
	providerType = "com.github.aux.acme.b/provider"
	consumerType = "com.github.aux.acme.a/consumer"
)

// dependencyOverlay is two asset modules an install wrote: the consumer
// requires the provider, which the install added for it.
func dependencyOverlay(consumerOn, providerOn bool) string {
	return fmt.Sprintf(`{
  "version": 1,
  "instances": [
    {"kind": "asset", "name": "provider", "type": "%s", "runtime": "asset", "enabled": %t, "required": false,
     "asset": {"path": "/opt/modules/provider/ui", "checksum": "sha256:%s"},
     "module": {"version": "1.2.0", "pulled": true}},
    {"kind": "asset", "name": "consumer", "type": "%s", "runtime": "asset", "enabled": %t, "required": false,
     "asset": {"path": "/opt/modules/consumer/ui", "checksum": "sha256:%s"},
     "module": {"version": "1.0.0", "source": "https://example.test/a-1.0.0.tar.gz",
                "requires": [{"module": "com.github.aux.acme.b", "version": "^1.0"}]}}
  ]
}
`, providerType, providerOn, installed, consumerType, consumerOn, installed)
}

func TestAConflictRefusesTheTwoTogether(t *testing.T) {
	t.Parallel()

	b := module(t, "b", "1.6.0", true)
	a := module(t, "a", "1.0.0", false)
	a.Spec.Module.Conflicts = []install.Dependency{{Module: "com.github.aux.acme.b", Version: "<2.0"}}

	err := resource.CheckChange([]resource.Declaration{b}, []resource.Declaration{a, b}, a.Ref)
	require.True(t, fault.HasCode(err, httpfault.CodeModuleDependencyConflict()), "got %v", err)
	require.Contains(t, err.Error(), "com.github.aux.acme.a conflicts with com.github.aux.acme.b <2.0")

	newer := module(t, "b", "2.0.0", true)
	require.NoError(t, resource.CheckChange([]resource.Declaration{newer}, []resource.Declaration{a, newer}, a.Ref))
}
