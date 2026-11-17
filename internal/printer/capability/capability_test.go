package capability_test

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

const (
	verbHome    = connectorapi.Capability("core.command.home")
	verbPrint   = connectorapi.Capability("core.command.start_print")
	verbChamber = connectorapi.Capability("core.command.set_chamber_target_temp")
	argPath     = connectorapi.Capability("core.command.start_print#path")
)

func on() connectorapi.Value { return connectorapi.Value{Flag: true} }

// one is a registry covering MK4 alone, from the firmware floor upwards.
func one(set connectorapi.Set) profile.Registry {
	return profile.Registry{
		Schema: profile.CurrentSchema,
		Profiles: []profile.Profile{
			{Model: "MK4", Firmware: profile.Range{From: "6.4.0"}, Capabilities: set},
		},
	}
}

// The property the three tiers exist for. A profile that names a capability the
// driver's code does not have must not produce it, however confidently the file
// asserts it.
func TestALowerTierSubtractsAndNeverAdds(t *testing.T) {
	t.Parallel()

	baseline := connectorapi.Set{verbHome: on()}

	for _, tc := range []struct {
		name  string
		tiers capability.Tiers
		want  []connectorapi.Capability
	}{
		{
			name: "a profile claiming a capability the baseline lacks",
			tiers: capability.Tiers{
				Baseline: baseline,
				Profile:  connectorapi.Set{verbHome: on(), verbPrint: on()},
			},
			want: []connectorapi.Capability{verbHome},
		},
		{
			name: "a probe claiming a capability the baseline lacks",
			tiers: capability.Tiers{
				Baseline: baseline,
				Probe:    connectorapi.Set{verbHome: on(), verbPrint: on()},
			},
			want: []connectorapi.Capability{verbHome},
		},
		{
			name: "a profile and a probe agreeing on a capability the baseline lacks",
			tiers: capability.Tiers{
				Baseline: baseline,
				Profile:  connectorapi.Set{verbHome: on(), verbPrint: on()},
				Probe:    connectorapi.Set{verbHome: on(), verbPrint: on()},
			},
			want: []connectorapi.Capability{verbHome},
		},
		{
			name: "a probe subtracting what the profile allowed",
			tiers: capability.Tiers{
				Baseline: connectorapi.Set{verbHome: on(), verbPrint: on()},
				Profile:  connectorapi.Set{verbHome: on(), verbPrint: on()},
				Probe:    connectorapi.Set{verbHome: on()},
			},
			want: []connectorapi.Capability{verbHome},
		},
		{
			name:  "no profile and no probe leaves the baseline untouched",
			tiers: capability.Tiers{Baseline: connectorapi.Set{verbHome: on(), verbPrint: on()}},
			want:  []connectorapi.Capability{verbPrint, verbHome},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resolved := capability.Resolve(tc.tiers)

			for _, key := range tc.want {
				if !resolved.Has(key) {
					t.Errorf("%s was subtracted and should not have been", key)
				}
			}

			if len(resolved) != len(tc.want) {
				t.Errorf("resolved %v, want exactly %v", resolved.Keys(), tc.want)
			}
		})
	}
}

// Intersection subtracts. A profile that leaves out an argument the driver's
// code demands must not leave the verb behind in a form nothing declared: the
// descriptor says a verb takes exactly the arguments it carries keys for, so a
// surviving verb with its required argument gone is a command the caller may now
// send empty. That is a phantom command shape from a lower tier, which is the
// direction C1 forbids.
func TestAProfileThatDropsARequiredArgumentSubtractsTheVerb(t *testing.T) {
	t.Parallel()

	baseline := connectorapi.Set{
		verbPrint: on(),
		argPath:   {Flag: true, Kind: connectorapi.KindString, Required: true},
		verbHome:  on(),
	}

	resolved := capability.Resolve(capability.Tiers{
		Baseline: baseline,
		Profile:  connectorapi.Set{verbPrint: on(), verbHome: on()},
	})

	if resolved.Has(verbPrint) {
		t.Error("the verb survived a profile that dropped the argument its code demands")
	}

	if !resolved.Has(verbHome) {
		t.Error("a verb with no arguments was subtracted along with it")
	}

	if _, err := resolved.Validate(connectorapi.Command{Verb: verbPrint}); !fault.HasCode(
		err, connectorapi.CapabilityNotSupported.Code(),
	) {
		t.Errorf("err = %v, want the verb refused rather than accepted with no arguments", err)
	}
}

// Two tiers that disagree about a verb's kind drop the verb, and must not strand
// its arguments: an argument of a verb that is not there is a descriptor nothing
// can act on. Only the merged set shows it, because each tier on its own is
// consistent.
func TestAMergedSetNeverDescribesAnArgumentOfAVerbItDropped(t *testing.T) {
	t.Parallel()

	resolved := capability.Resolve(capability.Tiers{
		Baseline: connectorapi.Set{
			verbPrint: {Flag: true, Kind: connectorapi.KindFlag},
			argPath:   {Flag: true, Kind: connectorapi.KindString},
		},
		Profile: connectorapi.Set{
			verbPrint: {Flag: true, Kind: connectorapi.KindInteger},
			argPath:   {Flag: true, Kind: connectorapi.KindString},
		},
	})

	if len(resolved) != 0 {
		t.Errorf("the merge left %v after dropping the verb they disagreed about", resolved.Keys())
	}
}

// The merged set is the one the host commands against, so it answers to the
// descriptor rules like any other tier. Every model this binary ships is put
// through them.
func TestEveryBuiltinProfileMergesToASetThatSatisfiesTheDescriptorRules(t *testing.T) {
	t.Parallel()

	registry, err := profile.Builtin()
	if err != nil {
		t.Fatalf("loading the built-in profiles: %v", err)
	}

	for _, model := range registry.Models() {
		matched, ok := registry.Match(model, connectorapi.NewFirmwareLabel("6.4.0"))
		if !ok {
			t.Fatalf("%s: the registry does not match its own model at the floor", model)
		}

		merged := capability.Resolve(capability.Tiers{
			Baseline: matched.Capabilities,
			Profile:  matched.Capabilities,
		})

		for _, problem := range merged.Problems() {
			t.Errorf("%s: %v", model, problem)
		}
	}
}

// A nil profile is "nothing covers this firmware" and an empty one is "this
// firmware exposes nothing". Confusing the two turns a missing data file into a
// printer that can do nothing, or the reverse.
func TestAnAbsentTierIsNotAnEmptyTier(t *testing.T) {
	t.Parallel()

	baseline := connectorapi.Set{verbHome: on()}

	absent := capability.Resolve(capability.Tiers{Baseline: baseline, Profile: nil})
	if !absent.Has(verbHome) {
		t.Error("an absent profile narrowed the baseline")
	}

	empty := capability.Resolve(capability.Tiers{Baseline: baseline, Profile: connectorapi.Set{}})
	if len(empty) != 0 {
		t.Errorf("a profile declaring nothing left %v", empty.Keys())
	}
}

// The coverage tier is what a printer no profile covers is held to, and it must
// never widen a printer that one does cover: a matched MK4 is not entitled to
// what an XL's profile declares just because both are in the same file.
func TestTheCoverageTierAppliesOnlyWhenNoProfileMatched(t *testing.T) {
	t.Parallel()

	baseline := connectorapi.Set{verbHome: on(), verbPrint: on(), verbChamber: on()}

	for _, tc := range []struct {
		name  string
		tiers capability.Tiers
		want  []connectorapi.Capability
	}{
		{
			name: "no profile matched, so the coverage bounds the baseline",
			tiers: capability.Tiers{
				Baseline: baseline,
				Coverage: connectorapi.Set{verbHome: on(), verbPrint: on()},
			},
			want: []connectorapi.Capability{verbHome, verbPrint},
		},
		{
			name: "a profile matched, so the coverage is not read",
			tiers: capability.Tiers{
				Baseline: baseline,
				Profile:  connectorapi.Set{verbHome: on()},
				Coverage: connectorapi.Set{verbHome: on(), verbPrint: on()},
			},
			want: []connectorapi.Capability{verbHome},
		},
		{
			name:  "an absent coverage narrows nothing",
			tiers: capability.Tiers{Baseline: baseline},
			want:  []connectorapi.Capability{verbHome, verbPrint, verbChamber},
		},
		{
			name:  "a coverage declaring nothing narrows everything away",
			tiers: capability.Tiers{Baseline: baseline, Coverage: connectorapi.Set{}},
			want:  nil,
		},
		{
			name: "the probe still narrows what the coverage left",
			tiers: capability.Tiers{
				Baseline: baseline,
				Coverage: connectorapi.Set{verbHome: on(), verbPrint: on()},
				Probe:    connectorapi.Set{verbHome: on()},
			},
			want: []connectorapi.Capability{verbHome},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resolved := capability.Resolve(tc.tiers)

			for _, key := range tc.want {
				if !resolved.Has(key) {
					t.Errorf("%s was subtracted and should not have been", key)
				}
			}

			if len(resolved) != len(tc.want) {
				t.Errorf("resolved %v, want exactly %v", resolved.Keys(), tc.want)
			}
		})
	}
}

func TestResolveDoesNotMutateTheTiersItWasGiven(t *testing.T) {
	t.Parallel()

	baseline := connectorapi.Set{verbHome: on(), verbPrint: on()}

	capability.Resolve(capability.Tiers{Baseline: baseline, Profile: connectorapi.Set{verbHome: on()}})

	if len(baseline) != 2 {
		t.Errorf("the baseline was narrowed in place: %v", baseline.Keys())
	}
}

// An identity nothing covers is held to what this driver's profiles describe
// between them, and says so. Keeping the whole baseline offered an unknown
// device more than a recognised one, so the tier that exists to subtract added
// by being absent.
//
// The union is what the unmatched printer is held to and not the intersection:
// a capability one model has is a capability this driver's code is exercised
// for, and withholding it from every unknown device would subtract on no
// evidence at all.
func TestAnUnmatchedIdentityIsHeldToWhatTheProfilesCoverLoudly(t *testing.T) {
	t.Parallel()

	logged := &bytes.Buffer{}
	log := slog.New(slog.NewTextHandler(logged, &slog.HandlerOptions{Level: slog.LevelWarn}))

	registry := profile.Registry{
		Schema: profile.CurrentSchema,
		Profiles: []profile.Profile{
			{
				Model:        "MK4",
				Firmware:     profile.Range{From: "6.4.0"},
				Capabilities: connectorapi.Set{verbHome: on()},
			},
			{
				Model:        "MINI",
				Firmware:     profile.Range{From: "6.4.0"},
				Capabilities: connectorapi.Set{verbHome: on(), verbPrint: on()},
			},
		},
	}

	resolver := capability.NewResolver(registry, log)
	baseline := connectorapi.Set{verbHome: on(), verbPrint: on(), verbChamber: on()}

	resolution := resolver.Resolve(t.Context(), baseline, connectorapi.Identity{
		Model:    "Voron",
		Firmware: connectorapi.NewFirmwareLabel("2.4.0"),
		Serial:   "SN-1",
	}, nil, 1)

	if resolution.ProfileMatched {
		t.Error("an unknown model reported a matched profile")
	}

	if resolution.Capabilities.Has(verbChamber) {
		t.Error("a capability no profile declares reached a printer nothing recognises")
	}

	if !resolution.Capabilities.Has(verbPrint) {
		t.Error("a capability one profile declares was subtracted, so the tier is an intersection")
	}

	if !resolution.Capabilities.Has(verbHome) {
		t.Error("a capability every profile declares was subtracted")
	}

	for _, want := range []string{"Voron", "2.4.0", "SN-1"} {
		if !strings.Contains(logged.String(), want) {
			t.Errorf("the warning does not name %q, so the identity to add a profile for is not in the log", want)
		}
	}

	// The models the file does cover, beside the one the printer reported. A
	// model spelled differently from every profile is how this warning is
	// reached with a printer the file was written for, and the two spellings
	// in one line are what says so without opening the file.
	for _, want := range []string{"MK4", "MINI"} {
		if !strings.Contains(logged.String(), want) {
			t.Errorf("the warning does not name %q, so a reader cannot see which models the file "+
				"covers and cannot tell a respelling from a printer nobody wrote a profile for", want)
		}
	}

	t.Logf("observed warning: %s", strings.TrimSpace(logged.String()))
}

// The covered list is bounded. It is a driver's own profile file, and a driver
// with four hundred models must not put four hundred names in one log line.
func TestTheCoveredModelsInTheWarningAreBounded(t *testing.T) {
	t.Parallel()

	const models = 40

	registry := profile.Registry{Schema: profile.CurrentSchema}
	for i := range models {
		registry.Profiles = append(registry.Profiles, profile.Profile{
			Model:        "M" + strconv.Itoa(i),
			Firmware:     profile.Range{From: "6.4.0"},
			Capabilities: connectorapi.Set{verbHome: on()},
		})
	}

	logged := &bytes.Buffer{}
	log := slog.New(slog.NewTextHandler(logged, &slog.HandlerOptions{Level: slog.LevelWarn}))

	capability.NewResolver(registry, log).Resolve(t.Context(),
		connectorapi.Set{verbHome: on()},
		connectorapi.Identity{Model: "Voron", Firmware: connectorapi.NewFirmwareLabel("2.4.0")},
		nil, 1)

	line := logged.String()

	if !strings.Contains(line, "and 8 more") {
		t.Errorf("a registry of %d models produced a warning that does not say how many it left "+
			"out: %s", models, strings.TrimSpace(line))
	}

	if strings.Contains(line, "M39") {
		t.Errorf("a registry of %d models put the last of them in the log line, so the list is "+
			"not bounded: %s", models, strings.TrimSpace(line))
	}
}

// A driver that ships no profiles at all keeps its whole baseline. There is
// nothing else available: an empty tier narrows everything away and leaves a
// printer that accepts no command, and refusing the printer contradicts the
// rule that an identity nothing covers is not an error. It is the one case the
// resolver cannot make narrow, and it is what a fresh community driver looks
// like on its first day.
func TestADriverWithNoProfilesKeepsItsWholeBaseline(t *testing.T) {
	t.Parallel()

	resolver := capability.NewResolver(
		profile.Registry{Schema: profile.CurrentSchema}, slog.New(slog.DiscardHandler))

	baseline := connectorapi.Set{verbHome: on(), verbPrint: on(), verbChamber: on()}

	resolution := resolver.Resolve(t.Context(), baseline, connectorapi.Identity{
		Model:    "Voron",
		Firmware: connectorapi.NewFirmwareLabel("2.4.0"),
		Serial:   "SN-1",
	}, nil, 1)

	if len(resolution.Capabilities) != len(baseline) {
		t.Errorf("resolved %v against a baseline of %v, and there is no profile to narrow it with",
			resolution.Capabilities.Keys(), baseline.Keys())
	}
}

func TestAMatchedProfileNarrowsAndIsReported(t *testing.T) {
	t.Parallel()

	registry := profile.Registry{
		Schema: profile.CurrentSchema,
		Profiles: []profile.Profile{{
			Model:        "MK4",
			Firmware:     profile.Range{From: "6.4.0"},
			Capabilities: connectorapi.Set{verbHome: on()},
		}},
	}

	resolver := capability.NewResolver(registry, slog.New(slog.DiscardHandler))

	resolution := resolver.Resolve(t.Context(),
		connectorapi.Set{verbHome: on(), verbPrint: on()},
		connectorapi.Identity{Model: "MK4", Firmware: connectorapi.NewFirmwareLabel("6.4.1")},
		nil, 7)

	if !resolution.ProfileMatched {
		t.Fatal("a covered identity reported no profile")
	}

	if resolution.Capabilities.Has(verbPrint) {
		t.Error("a capability the profile leaves out survived")
	}

	if resolution.Digest != resolution.Capabilities.Digest() || resolution.Digest == "" {
		t.Error("the resolution carries no usable digest")
	}

	if resolution.Epoch != 7 {
		t.Errorf("epoch = %d, want the one it was resolved for", resolution.Epoch)
	}
}

// The epoch is what tells a caller that a capability set it cached belongs to a
// session that has ended, and the published remediation for
// CAPABILITY_NOT_SUPPORTED tells callers to read the set again on a reconnect
// because of it. An epoch that does not move on a reconnect makes that published
// instruction describe something the host does not do.
func TestAReconnectMovesTheCapabilityEpoch(t *testing.T) {
	t.Parallel()

	resolver := capability.NewResolver(
		one(connectorapi.Set{verbHome: on()}), slog.New(slog.DiscardHandler))

	baseline := connectorapi.Set{verbHome: on(), verbPrint: on()}
	printer := connectorapi.Identity{Model: "MK4", Firmware: connectorapi.NewFirmwareLabel("6.4.0"), Serial: "SN-1"}

	first := resolver.Arrive(t.Context(), baseline, printer, nil)
	second := resolver.Arrive(t.Context(), baseline, printer, nil)

	if first.Epoch == 0 {
		t.Error("a live capability set was handed out on epoch zero, which is what a zero value looks like")
	}

	if second.Epoch <= first.Epoch {
		t.Errorf("epoch went %d then %d, so a set cached before the reconnect still looks current",
			first.Epoch, second.Epoch)
	}

	if second.Digest != first.Digest {
		t.Error("the same printer resolved to a different set, so this measures the digest and not the epoch")
	}
}

// One printer's reconnection must not age another's set. Epochs are per unit,
// and a shared counter would make every cached set stale on any arrival.
func TestEachUnitCountsItsOwnEpoch(t *testing.T) {
	t.Parallel()

	resolver := capability.NewResolver(
		one(connectorapi.Set{verbHome: on()}), slog.New(slog.DiscardHandler))

	baseline := connectorapi.Set{verbHome: on()}
	unit := func(serial string) connectorapi.Identity {
		return connectorapi.Identity{
			Model: "MK4", Firmware: connectorapi.NewFirmwareLabel("6.4.0"), Serial: serial,
		}
	}

	if first := resolver.Arrive(t.Context(), baseline, unit("SN-1"), nil); first.Epoch != 1 {
		t.Errorf("the first arrival of a unit is epoch %d, want 1", first.Epoch)
	}

	if other := resolver.Arrive(t.Context(), baseline, unit("SN-2"), nil); other.Epoch != 1 {
		t.Errorf("a second unit's first arrival is epoch %d, want 1: the counter is shared", other.Epoch)
	}

	if again := resolver.Arrive(t.Context(), baseline, unit("SN-1"), nil); again.Epoch != 2 {
		t.Errorf("the first unit's second arrival is epoch %d, want 2", again.Epoch)
	}
}

// Two printers arriving at once is the ordinary case for a host with a fleet,
// and an epoch handed to two of them is an epoch that identifies nothing.
func TestConcurrentArrivalsEachGetTheirOwnEpoch(t *testing.T) {
	t.Parallel()

	resolver := capability.NewResolver(
		one(connectorapi.Set{verbHome: on()}), slog.New(slog.DiscardHandler))

	const arrivals = 16

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		seen  = map[uint64]bool{}
		reuse int
	)

	for range arrivals {
		wg.Go(func() {
			resolution := resolver.Arrive(t.Context(), connectorapi.Set{verbHome: on()},
				connectorapi.Identity{
					Model: "MK4", Firmware: connectorapi.NewFirmwareLabel("6.4.0"), Serial: "SN-1",
				}, nil)

			mu.Lock()
			defer mu.Unlock()

			if seen[resolution.Epoch] {
				reuse++
			}

			seen[resolution.Epoch] = true
		})
	}

	wg.Wait()

	if reuse != 0 {
		t.Errorf("%d of %d arrivals were handed an epoch another already had", reuse, arrivals)
	}
}

// A reader of the resolution can tell the three answers apart without reading a
// log line: a profile covered the printer, none did and the driver's profiles
// bounded it, or the driver ships none and its whole baseline is in force. The
// last two are both "no profile matched" and they are not the same risk.
func TestTheResolutionConditionSaysWhichTierWasInForce(t *testing.T) {
	t.Parallel()

	now := time.Unix(0, 0).UTC()

	for _, tc := range []struct {
		name       string
		resolution capability.Resolution
		basis      capability.Basis
		status     condition.Status
		reason     string
	}{
		{
			name: "a profile covered the identity",
			resolution: capability.Resolution{
				ProfileMatched: true, Capabilities: connectorapi.Set{verbHome: on()},
			},
			basis:  capability.BasisProfile,
			status: condition.StatusTrue,
			reason: capability.ReasonProfileMatched,
		},
		{
			name: "none did, and the driver's profiles bounded it",
			resolution: capability.Resolution{
				CoverageApplied: true, Model: "Voron", Firmware: connectorapi.NewFirmwareLabel("2.4"),
			},
			basis:  capability.BasisCoverage,
			status: condition.StatusFalse,
			reason: capability.ReasonProfileUnmatched,
		},
		{
			name:       "the driver ships no profiles at all",
			resolution: capability.Resolution{Model: "Voron", Firmware: connectorapi.NewFirmwareLabel("2.4")},
			basis:      capability.BasisBaseline,
			status:     condition.StatusFalse,
			reason:     capability.ReasonNoProfiles,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.resolution.Basis(); got != tc.basis {
				t.Errorf("basis = %v, want %v", got, tc.basis)
			}

			c := tc.resolution.Condition(now, 3)
			if c.Status != tc.status || c.Reason != tc.reason {
				t.Errorf("condition = %+v", c)
			}

			if err := c.Validate(); err != nil {
				t.Errorf("the condition does not satisfy the condition shape: %v", err)
			}

			if tc.basis != capability.BasisProfile && !strings.Contains(c.Message, "Voron") {
				t.Errorf("the condition does not name the identity: %q", c.Message)
			}
		})
	}
}

// The profile tier exists to subtract per firmware, and every band this binary
// ships is open-ended, so nothing shipped exercises the subtraction. The shipping
// matrix records no difference between two firmware versions and
// TestNoNewerFirmwareChangesAnythingTheMatrixRecords is what watches for one, so
// the band here is a fixture: inventing a firmware fact and shipping it as data
// would be worse than having none, and the mechanism still has to be known to
// work before a real band is written.
//
// The candidate case is the one the version comparator exists for: a build
// made before a release belongs to the band the release ends, not to the band it
// starts.
func TestASecondFirmwareBandSubtracts(t *testing.T) {
	t.Parallel()

	registry := profile.Registry{
		Schema: profile.CurrentSchema,
		Profiles: []profile.Profile{
			{
				Model:        "MK4",
				Firmware:     profile.Range{From: "6.4.0-0", To: "6.5.0"},
				Capabilities: connectorapi.Set{verbHome: on(), verbPrint: on(), verbChamber: on()},
			},
			{
				Model:        "MK4",
				Firmware:     profile.Range{From: "6.5.0"},
				Capabilities: connectorapi.Set{verbHome: on(), verbPrint: on()},
			},
		},
	}

	if err := profile.Validate(registry); err != nil {
		t.Fatalf("two bands that meet at a boundary were refused: %v", err)
	}

	resolver := capability.NewResolver(registry, slog.New(slog.DiscardHandler))
	baseline := connectorapi.Set{verbHome: on(), verbPrint: on(), verbChamber: on()}

	for _, tc := range []struct {
		firmware string
		chamber  bool
	}{
		{firmware: "6.4.0", chamber: true},
		{firmware: "6.4.9", chamber: true},
		{firmware: "6.5.0-RC", chamber: true},
		{firmware: "6.5.0"},
		{firmware: "7.0.0"},
	} {
		t.Run(tc.firmware, func(t *testing.T) {
			t.Parallel()

			resolution := resolver.Arrive(t.Context(), baseline, connectorapi.Identity{
				Model: "MK4", Firmware: connectorapi.NewFirmwareLabel(tc.firmware), Serial: "SN-" + tc.firmware,
			}, nil)

			if !resolution.ProfileMatched {
				t.Fatalf("firmware %s matched no band", tc.firmware)
			}

			if got := resolution.Capabilities.Has(verbChamber); got != tc.chamber {
				t.Errorf("firmware %s resolves the chamber verb = %v, want %v", tc.firmware, got, tc.chamber)
			}

			if !resolution.Capabilities.Has(verbHome) {
				t.Errorf("firmware %s lost a capability both bands declare", tc.firmware)
			}
		})
	}
}

// A merged set that breaks the descriptor rules is a set nobody sanctioned, so
// logging it and commanding against it anyway is the one outcome refused here.
// The key goes, the rest of the printer keeps working, and the log names what
// went.
func TestAMalformedMergedSetIsPrunedRatherThanCommandedAgainst(t *testing.T) {
	t.Parallel()

	const argSpeed = connectorapi.Capability("core.command.start_print#speed")

	// A profile and a baseline that agree on a default the descriptor refuses.
	// Neither tier's own gate sees it: this only exists once they are merged.
	unusable := connectorapi.Value{
		Flag: true, Kind: connectorapi.KindInteger, Required: true, Default: "10",
	}

	logged := &bytes.Buffer{}
	log := slog.New(slog.NewTextHandler(logged, &slog.HandlerOptions{Level: slog.LevelError}))

	resolver := capability.NewResolver(one(connectorapi.Set{
		verbHome: on(), verbPrint: on(), argSpeed: unusable,
	}), log)

	resolution := resolver.Arrive(t.Context(),
		connectorapi.Set{verbHome: on(), verbPrint: on(), argSpeed: unusable},
		connectorapi.Identity{Model: "MK4", Firmware: connectorapi.NewFirmwareLabel("6.4.0"), Serial: "SN-1"},
		nil)

	if resolution.Capabilities.Has(argSpeed) {
		t.Error("the host resolved a capability whose own descriptor refuses it")
	}

	if problems := resolution.Capabilities.Problems(); len(problems) != 0 {
		t.Errorf("the resolved set still breaks the descriptor rules: %v", problems)
	}

	if !slices.Contains(resolution.Pruned, argSpeed) {
		t.Errorf("the resolution does not record %s as removed, so nothing downstream can tell", argSpeed)
	}

	// Pruning subtracts. A verb whose required argument went may not survive as a
	// command the caller can now send empty, and one that never had an argument
	// is untouched.
	if resolution.Capabilities.Has(verbPrint) {
		t.Error("the verb survived without the argument its descriptor requires")
	}

	if !resolution.Capabilities.Has(verbHome) {
		t.Error("a capability with no problem was removed along with the one that had")
	}

	if !strings.Contains(logged.String(), "SN-1") {
		t.Error("the error does not name the printer that produced the set")
	}

	if !strings.Contains(resolution.Condition(time.Unix(0, 0).UTC(), 1).Message, "removing 2") {
		t.Errorf("the condition does not say the set was pruned: %q",
			resolution.Condition(time.Unix(0, 0).UTC(), 1).Message)
	}
}

// spyDriver records whether the driver was reached, which is the whole question
// for host-side validation.
type spyDriver struct {
	calls   []connectorapi.Command
	patches chan connectorapi.StatePatch
}

func newSpy() *spyDriver { return &spyDriver{patches: make(chan connectorapi.StatePatch)} }

func (s *spyDriver) Identity() connectorapi.Identity {
	return connectorapi.Identity{Model: "MK4"}
}

func (s *spyDriver) Capabilities() (connectorapi.Set, uint64) { return nil, 0 }

func (s *spyDriver) Do(_ context.Context, cmd connectorapi.Command) (connectorapi.Result, error) {
	s.calls = append(s.calls, cmd)

	return connectorapi.Result{OK: true}, nil
}

func (s *spyDriver) State() <-chan connectorapi.StatePatch { return s.patches }

func (s *spyDriver) Close() error { close(s.patches); return nil }

func hostSession(spy *spyDriver) *capability.Session {
	caps := connectorapi.Set{
		verbPrint: on(),
		argPath: {
			Flag: true, Kind: connectorapi.KindString,
			Supported: []string{"a.gcode", "b.gcode"}, Default: "a.gcode",
		},
	}

	return capability.Wrap(spy, capability.Resolution{Capabilities: caps, Epoch: 1, Digest: caps.Digest()})
}

// The claim that matters: a refused argument never reaches a driver. A driver
// that is called and then declines is a driver that has to carry the branch.
func TestARefusedArgumentNeverReachesTheDriver(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		cmd  connectorapi.Command
		want fault.Code
	}{
		{
			name: "a verb outside the resolved set",
			cmd:  connectorapi.Command{Verb: verbHome},
			want: connectorapi.CapabilityNotSupported.Code(),
		},
		{
			name: "an argument the verb does not declare",
			cmd:  connectorapi.Command{Verb: verbPrint, Args: map[string]string{"speed": "200"}},
			want: connectorapi.ArgumentUnknown.Code(),
		},
		{
			name: "a value outside the supported set",
			cmd:  connectorapi.Command{Verb: verbPrint, Args: map[string]string{"path": "c.gcode"}},
			want: connectorapi.ArgumentNotSupported.Code(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			spy := newSpy()

			_, err := hostSession(spy).Do(t.Context(), tc.cmd)
			if !fault.HasCode(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}

			if len(spy.calls) != 0 {
				t.Errorf("the driver was called with %+v despite the refusal", spy.calls)
			}
		})
	}
}

// connectorapi.Session promises the capability set is immutable for the life of
// the epoch. Handing out the live map breaks that in the worst direction: a
// caller writes a key into the map Do validates against and gets a phantom
// capability from below the baseline, with a recorded digest that no longer
// describes what is in force.
func TestAMutatedCapabilitySetCannotMintACapability(t *testing.T) {
	t.Parallel()

	const factoryReset = connectorapi.Capability("core.command.factory_reset")

	spy := newSpy()
	session := hostSession(spy)

	caps, _ := session.Capabilities()
	caps[factoryReset] = on()
	caps[argPath] = connectorapi.Value{
		Flag: true, Kind: connectorapi.KindString, Supported: []string{"anything.gcode"},
	}

	if _, err := session.Do(t.Context(), connectorapi.Command{Verb: factoryReset}); !fault.HasCode(
		err, connectorapi.CapabilityNotSupported.Code(),
	) {
		t.Errorf("err = %v, want the minted capability refused", err)
	}

	if _, err := session.Do(t.Context(), connectorapi.Command{
		Verb: verbPrint, Args: map[string]string{"path": "anything.gcode"},
	}); !fault.HasCode(err, connectorapi.ArgumentNotSupported.Code()) {
		t.Errorf("err = %v, want the widened supported set refused", err)
	}

	if len(spy.calls) != 0 {
		t.Errorf("the driver was reached with %+v", spy.calls)
	}

	again, _ := session.Capabilities()
	if again.Has(factoryReset) {
		t.Error("the mutation is visible in the next read, so the set is not immutable for the epoch")
	}

	if session.Resolution().Capabilities.Has(factoryReset) {
		t.Error("Resolution hands out the live map, so the audit trail can be edited from outside")
	}
}

// The same promise for the cheap accessor the fleet listing reads. It answers
// without copying the descriptor, and the whole point of that is that nothing
// else in the process reads the list it copies from.
//
// A shared slice would be worse here than the shared map was. The listing hands
// this straight to the JSON encoder as one printer's capability array, so a
// caller that sorted it, truncated it or wrote into one element would be editing
// what every later reader of that printer is told, on a session that has not
// changed.
func TestAMutatedCapabilityNameListDoesNotReachTheSession(t *testing.T) {
	t.Parallel()

	session := hostSession(newSpy())

	want := []string{verbPrint.String(), argPath.String()}

	names, epoch := session.CapabilityNames()
	if !slices.Equal(names, want) || epoch != 1 {
		t.Fatalf("CapabilityNames() = %v at epoch %d, want %v at 1", names, epoch, want)
	}

	names[0] = "core.command.factory_reset"
	clear(names[1:])

	again, epochAgain := session.CapabilityNames()
	if !slices.Equal(again, want) {
		t.Errorf("the next read is %v, want %v: the list is shared, so one caller's "+
			"write is every later caller's answer", again, want)
	}

	if epochAgain != epoch {
		t.Errorf("the epoch moved from %d to %d with no new resolution", epoch, epochAgain)
	}

	// The set the host commands against is the other half of the promise. A
	// name list taken off the live map would be a second door onto it.
	set, _ := session.Capabilities()
	if !slices.Equal(set.Keys(), []connectorapi.Capability{verbPrint, argPath}) {
		t.Errorf("the resolved set is %v, want the two keys it was wrapped with", set.Keys())
	}
}

// Adopt is what a reconnection calls, and the name list belongs to the
// resolution rather than to the session. A list computed once at Wrap and never
// again would report the firmware that was there before the printer came back.
func TestAdoptingAResolutionMovesTheNameList(t *testing.T) {
	t.Parallel()

	session := hostSession(newSpy())

	narrowed := connectorapi.Set{verbHome: on()}
	session.Adopt(capability.Resolution{
		Capabilities: narrowed, Epoch: 2, Digest: narrowed.Digest(),
	})

	names, epoch := session.CapabilityNames()
	if !slices.Equal(names, []string{verbHome.String()}) || epoch != 2 {
		t.Fatalf("CapabilityNames() = %v at epoch %d, want the adopted resolution's", names, epoch)
	}
}

// countingDriver is the spy for the concurrent case. spyDriver appends to a
// slice, which is a race of its own and would report in place of the one being
// looked for.
type countingDriver struct {
	mu      sync.Mutex
	calls   int
	patches chan connectorapi.StatePatch
}

func (d *countingDriver) Identity() connectorapi.Identity { return connectorapi.Identity{Model: "MK4"} }

func (d *countingDriver) Capabilities() (connectorapi.Set, uint64) { return nil, 0 }

func (d *countingDriver) Do(_ context.Context, _ connectorapi.Command) (connectorapi.Result, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.calls++

	return connectorapi.Result{OK: true}, nil
}

func (d *countingDriver) State() <-chan connectorapi.StatePatch { return d.patches }

func (d *countingDriver) Close() error { return nil }

// Two goroutines that read the set must not be sharing one map with the
// validator. A shared map is a concurrent read and write, which is a fatal
// runtime error with no recovery and needs no race detector to kill the process.
func TestConcurrentReadersDoNotShareTheValidatedMap(t *testing.T) {
	t.Parallel()

	caps := connectorapi.Set{
		verbPrint: on(),
		argPath: {
			Flag: true, Kind: connectorapi.KindString,
			Supported: []string{"a.gcode", "b.gcode"}, Default: "a.gcode",
		},
	}

	driver := &countingDriver{patches: make(chan connectorapi.StatePatch)}
	session := capability.Wrap(driver, capability.Resolution{
		Capabilities: caps, Epoch: 1, Digest: caps.Digest(),
	})

	const readers = 8

	var wg sync.WaitGroup

	for i := range readers {
		wg.Go(func() {
			read, _ := session.Capabilities()
			read[connectorapi.Capability("core.command.goroutine")] = on()

			if _, err := session.Do(t.Context(), connectorapi.Command{Verb: verbPrint}); err != nil {
				t.Errorf("Do: %v", err)
			}

			if i == 0 {
				_ = session.Resolution().Capabilities.Digest()
			}
		})
	}

	wg.Wait()

	if driver.calls != readers {
		t.Errorf("the driver saw %d commands, want %d", driver.calls, readers)
	}
}

func TestAnAcceptedCommandReachesTheDriverWithItsDefaultsFilled(t *testing.T) {
	t.Parallel()

	spy := newSpy()

	result, err := hostSession(spy).Do(t.Context(), connectorapi.Command{Verb: verbPrint})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	if !result.OK {
		t.Error("the driver's result did not reach the caller")
	}

	if len(spy.calls) != 1 {
		t.Fatalf("the driver saw %d commands, want 1", len(spy.calls))
	}

	if got := spy.calls[0].Args["path"]; got != "a.gcode" {
		t.Errorf("the driver saw path %q, want the declared default filled in", got)
	}
}

// The built-in profiles are the real input this resolver will see, so the
// resolution is exercised against them rather than against a fixture only.
func TestTheBuiltinProfilesResolveForEveryModelTheyCover(t *testing.T) {
	t.Parallel()

	registry, err := profile.Builtin()
	if err != nil {
		t.Fatalf("loading the built-in profiles: %v", err)
	}

	resolver := capability.NewResolver(registry, slog.New(slog.DiscardHandler))

	for _, model := range registry.Models() {
		matched, ok := registry.Match(model, connectorapi.NewFirmwareLabel("6.4.0"))
		if !ok {
			t.Fatalf("%s: the registry does not match its own model at the floor", model)
		}

		resolution := resolver.Resolve(t.Context(),
			matched.Capabilities,
			connectorapi.Identity{Model: model, Firmware: connectorapi.NewFirmwareLabel("6.4.0")},
			nil, 1)

		if !resolution.ProfileMatched {
			t.Errorf("%s: no profile matched", model)
		}

		if len(resolution.Capabilities) != len(matched.Capabilities) {
			t.Errorf("%s: resolved %d capabilities against a baseline of %d",
				model, len(resolution.Capabilities), len(matched.Capabilities))
		}
	}
}
