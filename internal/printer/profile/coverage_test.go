package profile_test

import (
	"slices"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

const (
	verbPrint = connectorapi.Capability("core.command.start_print")
	argPath   = connectorapi.Capability("core.command.start_print#path")
	argSpeed  = connectorapi.Capability("core.command.start_print#speed")
)

func registryOf(sets ...connectorapi.Set) profile.Registry {
	models := []string{"MK4", "MINI", "XL", "IX"}
	registry := profile.Registry{Schema: profile.CurrentSchema}

	for i, set := range sets {
		registry.Profiles = append(registry.Profiles, profile.Profile{
			Model:        models[i],
			Firmware:     profile.Range{From: "6.4.0"},
			Capabilities: set,
		})
	}

	return registry
}

// A key one profile carries is in the coverage. The tier answers "what has this
// driver's code been recorded doing", so a capability one model has is evidence
// the driver has it, and the intersection would subtract on no evidence at all.
func TestCoverageKeepsAKeyAnyOneProfileCarries(t *testing.T) {
	t.Parallel()

	coverage := registryOf(
		connectorapi.Set{verbHome: {Flag: true}},
		connectorapi.Set{verbHome: {Flag: true}, verbPrint: {Flag: true}},
	).Coverage()

	for _, key := range []connectorapi.Capability{verbHome, verbPrint} {
		if !coverage.Has(key) {
			t.Errorf("%s is declared by a profile and is not in the coverage", key)
		}
	}

	if len(coverage) != 2 {
		t.Errorf("coverage = %v, want exactly the two keys the profiles declare", coverage.Keys())
	}
}

// A registry with no profiles has no coverage. Nil is an absent tier and leaves
// the driver's baseline whole; an empty set would narrow it away and leave a
// printer that accepts no command at all.
func TestCoverageOfAnEmptyRegistryIsAbsentRatherThanEmpty(t *testing.T) {
	t.Parallel()

	if coverage := (profile.Registry{Schema: profile.CurrentSchema}).Coverage(); coverage != nil {
		t.Errorf("coverage = %v, want nil so that it narrows nothing", coverage)
	}
}

// The looser of two descriptions of one key wins, one field at a time. A tier
// that widened only some of them would be narrower than the profile a printer
// would have matched had one been recorded for it.
func TestCoverageTakesTheLooserOfEveryField(t *testing.T) {
	t.Parallel()

	coverage := registryOf(
		connectorapi.Set{
			verbPrint: {Flag: true},
			argPath: {
				Flag: true, Kind: connectorapi.KindString,
				Supported: []string{"a.gcode"}, Required: true, Limit: 10, HasLimit: true,
			},
			argSpeed: {Flag: true, Kind: connectorapi.KindInteger, Min: 20, HasMin: true},
		},
		connectorapi.Set{
			verbPrint: {Flag: true},
			argPath: {
				Flag: true, Kind: connectorapi.KindString,
				Supported: []string{"b.gcode"}, Limit: 40, HasLimit: true,
			},
			argSpeed: {Flag: true, Kind: connectorapi.KindInteger, Min: 5, HasMin: true},
		},
	).Coverage()

	path := coverage[argPath]

	if want := []string{"a.gcode", "b.gcode"}; !slices.Equal(path.Supported, want) {
		t.Errorf("supported = %v, want the union %v", path.Supported, want)
	}

	if path.Required {
		t.Error("an argument one profile leaves optional is required in the coverage")
	}

	if path.Limit != 40 {
		t.Errorf("limit = %v, want the higher of the two", path.Limit)
	}

	if speed := coverage[argSpeed]; speed.Min != 5 {
		t.Errorf("min = %v, want the lower of the two", speed.Min)
	}
}

// An unconstrained side absorbs a constrained one. An empty Supported means any
// value the kind can read, and an undeclared bound means unbounded, so a
// profile that says nothing about a value is the looser of the two.
func TestCoverageTreatsAnUndeclaredConstraintAsUnconstrained(t *testing.T) {
	t.Parallel()

	coverage := registryOf(
		connectorapi.Set{
			verbPrint: {Flag: true},
			argPath: {
				Flag: true, Kind: connectorapi.KindString,
				Supported: []string{"a.gcode"}, Limit: 10, HasLimit: true, Min: 1, HasMin: true,
			},
		},
		connectorapi.Set{
			verbPrint: {Flag: true},
			argPath:   {Flag: true, Kind: connectorapi.KindString},
		},
	).Coverage()

	path := coverage[argPath]

	switch {
	case len(path.Supported) != 0:
		t.Errorf("supported = %v, want unconstrained", path.Supported)
	case path.HasLimit:
		t.Errorf("limit = %v, want none", path.Limit)
	case path.HasMin:
		t.Errorf("min = %v, want none", path.Min)
	}
}

// Two profiles that disagree about a key's kind describe two different things
// under one name, and there is no looser form of both. Dropping the key holds a
// device nothing recognises to less rather than to whichever profile the file
// happens to list first.
func TestCoverageDropsAKeyTwoProfilesDisagreeAboutTheKindOf(t *testing.T) {
	t.Parallel()

	integer := connectorapi.Set{
		verbPrint: {Flag: true},
		argSpeed:  {Flag: true, Kind: connectorapi.KindInteger},
	}
	text := connectorapi.Set{
		verbPrint: {Flag: true},
		argSpeed:  {Flag: true, Kind: connectorapi.KindString},
	}

	for _, tc := range []struct {
		name     string
		registry profile.Registry
	}{
		{name: "the disagreement is the last word", registry: registryOf(integer, text)},
		{name: "a later profile agrees with the first", registry: registryOf(integer, text, integer)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			coverage := tc.registry.Coverage()

			if coverage.Has(argSpeed) {
				t.Errorf("%s survives a disagreement about its kind as %+v", argSpeed, coverage[argSpeed])
			}

			if !coverage.Has(verbPrint) {
				t.Error("a key both profiles agree about was dropped with it")
			}
		})
	}
}

// The coverage is a value the resolver holds for the life of a host. A slice
// shared with a profile would let a caller edit the file's own descriptor
// through it.
func TestCoverageDoesNotAliasTheProfilesItWasBuiltFrom(t *testing.T) {
	t.Parallel()

	registry := registryOf(connectorapi.Set{
		verbPrint: {Flag: true},
		argPath:   {Flag: true, Kind: connectorapi.KindString, Supported: []string{"a.gcode"}},
	})

	coverage := registry.Coverage()
	coverage[argPath].Supported[0] = "edited.gcode"

	if got := registry.Profiles[0].Capabilities[argPath].Supported[0]; got != "a.gcode" {
		t.Errorf("the profile now supports %q, so the coverage shares its slice", got)
	}
}
