package connectorapi_test

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// A descriptor is data, so every rule it breaks has to be caught by something
// that reads it rather than by the code that trusts it.
func TestProblemsCatchesADescriptorNothingCanActOn(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		set  connectorapi.Set
	}{
		{
			name: "a key the vocabulary's syntax refuses",
			set:  connectorapi.Set{"core.command.START_PRINT": {Flag: true}},
		},
		{
			name: "a kind outside the defined set",
			set:  connectorapi.Set{verbPrint: {Flag: true, Kind: connectorapi.Kind(99)}},
		},
		{
			name: "an argument of a verb nothing declares",
			set:  connectorapi.Set{argPath: {Flag: true, Kind: connectorapi.KindString}},
		},
		{
			name: "a flag constraining values nothing will read",
			set:  connectorapi.Set{verbPrint: {Flag: true, Supported: []string{"a"}}},
		},
		{
			name: "a flag carrying a lower bound nothing will read",
			set:  connectorapi.Set{verbPrint: {Flag: true, HasMin: true}},
		},
		{
			name: "a default outside its own supported set",
			set: connectorapi.Set{
				verbPrint: {Flag: true},
				argPath:   {Flag: true, Kind: connectorapi.KindString, Supported: []string{"a"}, Default: "b"},
			},
		},
		{
			name: "a default its own limit refuses",
			set: connectorapi.Set{
				verbPrint: {Flag: true},
				argPath: {
					Flag: true, Kind: connectorapi.KindInteger,
					Default: "99999", Limit: 10, HasLimit: true,
				},
			},
		},
		{
			name: "a default unreadable as its own kind",
			set: connectorapi.Set{
				verbPrint: {Flag: true},
				argPath:   {Flag: true, Kind: connectorapi.KindInteger, Default: "not-a-number"},
			},
		},
		{
			// The requirement can never be enforced against anyone: fill runs
			// before anything can be missing, so ArgumentMissing is unreachable
			// for this key and the descriptor says two things it cannot both do.
			name: "a required argument that also declares a default",
			set: connectorapi.Set{
				verbPrint: {Flag: true},
				argPath: {
					Flag: true, Kind: connectorapi.KindString, Required: true, Default: "a.gcode",
				},
			},
		},
		{
			name: "a lower bound on a string, where it would bound the length",
			set: connectorapi.Set{
				verbPrint: {Flag: true},
				argPath:   {Flag: true, Kind: connectorapi.KindString, Min: 2, HasMin: true},
			},
		},
		{
			name: "a range that permits nothing",
			set: connectorapi.Set{
				verbPrint: {Flag: true},
				argPath: {
					Flag: true, Kind: connectorapi.KindInteger,
					Min: 500, HasMin: true, Limit: 10, HasLimit: true,
				},
			},
		},
		{
			// ArgumentKey joins whatever it is given, so a long verb and a long
			// argument name compose a key past the length the vocabulary
			// permits. Nothing stops it being built; this is what stops it being
			// shipped.
			name: "an argument key composed past the length limit",
			set: connectorapi.Set{
				verbPrint: {Flag: true},
				connectorapi.ArgumentKey(verbPrint, strings.Repeat("a", 300)): {
					Flag: true, Kind: connectorapi.KindString,
				},
			},
		},
		{
			// A profile file cannot carry this: JSON has no NaN. A driver's
			// Baseline is Go and carries whatever it is written with, and the
			// bound is silent in both directions - NaN is false on both sides of
			// every comparison, so the limit accepted 999999999 and Problems
			// reported nothing, which is the conformance suite certifying it.
			name: "an upper bound that is not a number",
			set: connectorapi.Set{
				verbPrint: {Flag: true},
				argPath: {
					Flag: true, Kind: connectorapi.KindNumber, HasLimit: true, Limit: math.NaN(),
				},
			},
		},
		{
			name: "a lower bound that is not a number",
			set: connectorapi.Set{
				verbPrint: {Flag: true},
				argPath: {
					Flag: true, Kind: connectorapi.KindNumber, HasMin: true, Min: math.NaN(),
				},
			},
		},
		{
			name: "an infinite upper bound, which is not a bound",
			set: connectorapi.Set{
				verbPrint: {Flag: true},
				argPath: {
					Flag: true, Kind: connectorapi.KindNumber, HasLimit: true, Limit: math.Inf(1),
				},
			},
		},
		{
			// The opposite failure and just as quiet: Limit bounds the absolute
			// value, which is never below zero, so nothing can ever be supplied
			// for this argument and the verb carrying it cannot be used.
			name: "a negative magnitude bound, which permits nothing",
			set: connectorapi.Set{
				verbPrint: {Flag: true},
				argPath: {
					Flag: true, Kind: connectorapi.KindNumber, HasLimit: true, Limit: -1,
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if problems := tc.set.Problems(); len(problems) == 0 {
				t.Error("the descriptor rules accepted a set nothing can act on")
			}
		})
	}
}

// TestABoundThatIsNotANumberRefusesRatherThanAccepts is the other half. Problems
// is a gate a driver runs; a command that arrives while a descriptor is broken
// has to be answered anyway, and narrowing is the only direction a broken
// descriptor may be read in.
func TestABoundThatIsNotANumberRefusesRatherThanAccepts(t *testing.T) {
	t.Parallel()

	for name, value := range map[string]connectorapi.Value{
		"an upper bound of NaN": {
			Flag: true, Kind: connectorapi.KindNumber, HasLimit: true, Limit: math.NaN(),
		},
		"a lower bound of NaN": {
			Flag: true, Kind: connectorapi.KindNumber, HasMin: true, Min: math.NaN(),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			set := connectorapi.Set{verbPrint: {Flag: true}, argPath: value}

			for _, sent := range []string{"999999999", "-999999999", "0"} {
				if _, err := set.Validate(connectorapi.Command{
					Verb: verbPrint, Args: map[string]string{"path": sent},
				}); err == nil {
					t.Errorf("Validate(%q) was accepted against a bound nothing can be compared to", sent)
				}
			}
		})
	}
}

func TestProblemsAcceptsADescriptorThatCanBeActedOn(t *testing.T) {
	t.Parallel()

	set := connectorapi.Set{
		verbPrint: {Flag: true},
		argPath: {
			Flag: true, Kind: connectorapi.KindString,
			Supported: []string{"a.gcode", "b.gcode"}, Default: "a.gcode",
		},
		"core.command.start_print#temperature": {
			Flag: true, Kind: connectorapi.KindNumber,
			Default: "215", Min: 0, HasMin: true, Limit: 300, HasLimit: true,
		},
		"core.feature.dialogs": {Flag: true},
	}

	if problems := set.Problems(); len(problems) != 0 {
		t.Errorf("the descriptor rules rejected a set that can be acted on: %v", problems)
	}
}

// Prune is what a host does with a merged set. Every case here is a descriptor
// the rules refuse, and the answer is the same one in each: the key goes, what
// depended on it goes with it, and everything else survives.
func TestPruneRemovesWhatTheRulesRefuseAndKeepsTheRest(t *testing.T) {
	t.Parallel()

	const argSpeed = connectorapi.Capability("core.command.start_print#speed")

	for _, tc := range []struct {
		name string
		set  connectorapi.Set
		want []connectorapi.Capability
	}{
		{
			name: "a default the descriptor's own rules refuse",
			set: connectorapi.Set{
				verbPause: {Flag: true},
				verbPrint: {Flag: true},
				argPath: {
					Flag: true, Kind: connectorapi.KindString,
					Supported: []string{"a.gcode"}, Default: "b.gcode",
				},
			},
			want: []connectorapi.Capability{verbPause, verbPrint},
		},
		{
			name: "a required argument that is unusable takes its verb with it",
			set: connectorapi.Set{
				verbPause: {Flag: true},
				verbPrint: {Flag: true},
				argSpeed: {
					Flag: true, Kind: connectorapi.KindInteger, Required: true, Default: "10",
				},
			},
			want: []connectorapi.Capability{verbPause},
		},
		{
			name: "a bound nothing can be compared against",
			set: connectorapi.Set{
				verbPause: {Flag: true},
				verbPrint: {Flag: true},
				argPath: {
					Flag: true, Kind: connectorapi.KindString, HasLimit: true, Limit: math.Inf(1),
				},
			},
			want: []connectorapi.Capability{verbPause, verbPrint},
		},
		{
			name: "a key the vocabulary's syntax refuses",
			set:  connectorapi.Set{verbPause: {Flag: true}, "core.command.HOME": {Flag: true}},
			want: []connectorapi.Capability{verbPause},
		},
		{
			name: "a set with nothing wrong with it",
			set:  connectorapi.Set{verbPause: {Flag: true}, verbPrint: {Flag: true}},
			want: []connectorapi.Capability{verbPause, verbPrint},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sound, removed, problems := tc.set.Prune()

			if got := sound.Keys(); !slices.Equal(got, slices.Sorted(slices.Values(tc.want))) {
				t.Errorf("pruned to %v, want %v", got, tc.want)
			}

			if left := sound.Problems(); len(left) != 0 {
				t.Errorf("the pruned set still breaks the rules: %v", left)
			}

			if len(removed)+len(sound) != len(tc.set) {
				t.Errorf("%d keys went in, %d were kept and %d reported removed",
					len(tc.set), len(sound), len(removed))
			}

			if len(removed) > 0 && len(problems) == 0 {
				t.Error("keys were removed and nothing says why")
			}
		})
	}
}

// Prune must not edit the set it was handed. A host prunes the merge and keeps
// the tiers, and a tier narrowed in place is narrowed for every printer after it.
func TestPruneLeavesTheSetItWasGivenAlone(t *testing.T) {
	t.Parallel()

	set := connectorapi.Set{
		verbPrint: {Flag: true},
		argPath:   {Flag: true, Kind: connectorapi.KindString, Supported: []string{"a"}, Default: "b"},
	}

	if _, removed, _ := set.Prune(); len(removed) == 0 {
		t.Fatal("nothing was pruned, so this measures nothing")
	}

	if len(set) != 2 {
		t.Errorf("the set handed in is now %v", set.Keys())
	}
}
