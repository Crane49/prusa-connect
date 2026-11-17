package simulator_test

import (
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

const (
	verbPrefix    = "core.command."
	featurePrefix = "core.feature."
)

// The simulator can be every model the profiles cover, and only those.
//
// The projection is total: a profile carrying a capability the wire has no
// spelling for is an error rather than a model with a hole in it, so this is
// also what says every key in the shipping file is one of the two kinds
// modelFor knows how to project.
func TestTheSimulatorCanBeEveryModelTheProfilesCover(t *testing.T) {
	t.Parallel()

	registry, err := profile.Builtin()
	if err != nil {
		t.Fatalf("load the builtin profiles: %v", err)
	}

	models, err := simulator.Models()
	if err != nil {
		t.Fatalf("project the models: %v", err)
	}

	covered := registry.Models()
	slices.Sort(covered)

	if !slices.Equal(models, covered) {
		t.Fatalf("the simulator can be %v, the profiles cover %v", models, covered)
	}

	for _, name := range covered {
		model, byNameErr := simulator.ModelByName(strings.ToLower(name))
		if byNameErr != nil {
			t.Errorf("the simulator cannot be a %s, which the profiles cover: %v", name, byNameErr)

			continue
		}

		if model.Name != name {
			t.Errorf("looking up %q found %q, and the name is what the printer reports in "+
				"User-Agent-Printer", strings.ToLower(name), model.Name)
		}

		if len(model.Commands) == 0 {
			t.Errorf("%s answers no command, so the projection dropped its whole vocabulary", name)
		}
	}
}

// A model nothing covers is refused, and the refusal says what is covered.
func TestAModelNoProfileCoversIsRefused(t *testing.T) {
	t.Parallel()

	_, err := simulator.ModelByName("MK9000")
	if err == nil {
		t.Fatal("the simulator agreed to be an MK9000, which no profile covers")
	}

	if !strings.Contains(err.Error(), "MK9000") {
		t.Errorf("the refusal does not name the model asked for: %v", err)
	}
}

// The transform between the two vocabularies is total in both directions.
//
// A profile spells a verb in lower case and the wire spells it in upper case,
// so the simulator's vocabulary is the profile's keys upper-cased and
// CapabilityKeyFor is the inverse. That inverse is only safe while no key
// carries a character the round trip loses, and this is what says so rather
// than assuming it.
func TestTheWireSpellingRoundTripsThroughTheCapabilityVocabulary(t *testing.T) {
	t.Parallel()

	registry, err := profile.Builtin()
	if err != nil {
		t.Fatalf("load the builtin profiles: %v", err)
	}

	for _, p := range registry.Profiles {
		for _, key := range p.Capabilities.Keys() {
			verb := key.Verb()

			suffix, isCommand := strings.CutPrefix(string(verb), verbPrefix)
			if !isCommand {
				continue
			}

			if round := simulator.CapabilityKeyFor(strings.ToUpper(suffix)); round != verb {
				t.Errorf("%s: %q does not survive the trip through its wire spelling, it becomes %q",
					p.Model, verb, round)
			}
		}
	}
}

// The refusal the simulator produces is the refusal the host would have
// produced.
//
// The simulator answers "is this command legal for me" off its projection of
// one profile and internal/printer/capability answers it off the resolved
// intersection of the three tiers, which drops a verb whose required argument
// the intersection removed and drops an argument whose verb went with it.
// They are separate code paths, and a simulator that permitted something the
// host refuses, or refused something the host permits, would be teaching the
// suite the wrong answer.
//
// Arguments are checked alongside the verbs because they are half of the
// answer: a verb both sides accept, whose argument one of them thinks is
// required and the other optional, is the same defect one refusal later.
func TestARefusalMatchesWhatTheCapabilityResolverWouldRefuse(t *testing.T) {
	t.Parallel()

	registry, err := profile.Builtin()
	if err != nil {
		t.Fatalf("load the builtin profiles: %v", err)
	}

	baseline := everyCapability(registry)
	resolver := capability.NewResolver(registry, slog.New(slog.DiscardHandler))
	verbs := everyVerb(registry)

	for _, p := range registry.Profiles {
		t.Run(p.Model, func(t *testing.T) {
			t.Parallel()

			model, modelErr := simulator.ModelByName(p.Model)
			if modelErr != nil {
				t.Fatalf("the simulator cannot be a %s: %v", p.Model, modelErr)
			}

			resolution := resolver.Resolve(t.Context(), baseline, connectorapi.Identity{
				Model:    p.Model,
				Firmware: connectorapi.NewFirmwareLabel(p.Firmware.From),
				Serial:   "SIM",
			}, nil, 1)

			if !resolution.ProfileMatched {
				t.Fatalf("no profile covers %s at firmware %s, which is the identity the "+
					"simulator reports", p.Model, p.Firmware.From)
			}

			for _, verb := range verbs {
				command, accepts := model.Command(verb)
				key := simulator.CapabilityKeyFor(verb)

				if resolves := resolution.Capabilities.Has(key); accepts != resolves {
					t.Errorf("%s: the simulator accepts=%t and the resolver permits=%t for %s",
						p.Model, accepts, resolves, verb)

					continue
				}

				compareArguments(t, p.Model, command, key, resolution.Capabilities)
			}

			compareFeatures(t, p.Model, model.Features, resolution.Capabilities)
		})
	}
}

// compareFeatures holds the firmware feature flags to the same standard as the
// verbs. They are the other half of what a profile declares, and the wire
// spells them the same way it spells a verb: upper-cased, with the namespace
// dropped.
func compareFeatures(t *testing.T, model string, features []string, resolved connectorapi.Set) {
	t.Helper()

	var permitted []string

	for _, key := range resolved.Keys() {
		if suffix, isFeature := strings.CutPrefix(string(key), featurePrefix); isFeature &&
			resolved[key].Flag {
			permitted = append(permitted, strings.ToUpper(suffix))
		}
	}

	slices.Sort(permitted)

	if !slices.Equal(features, permitted) {
		t.Errorf("%s: the simulator declares features %v, the resolver permits %v",
			model, features, permitted)
	}
}

// compareArguments holds one verb's arguments to the resolved set, both ways: an
// argument the simulator declares that the host does not permit is a command
// the simulator would accept and the host would never send, and one the host
// permits that the simulator does not declare is a command the host sends and
// the simulator refuses as UNKNOWN_ARGUMENT.
func compareArguments(
	t *testing.T,
	model string,
	command simulator.Command,
	verb connectorapi.Capability,
	resolved connectorapi.Set,
) {
	t.Helper()

	for name, argument := range command.Arguments {
		key := connectorapi.ArgumentKey(verb, name)

		permitted, ok := resolved[key]
		if !ok || !permitted.Flag {
			t.Errorf("%s: the simulator accepts %s on %s, which the resolver does not permit",
				model, name, command.Name)

			continue
		}

		if permitted.Kind != argument.Kind || permitted.Required != argument.Required {
			t.Errorf("%s: the simulator reads %s on %s as kind %s required=%t, the resolver as "+
				"kind %s required=%t", model, name, command.Name,
				argument.Kind, argument.Required, permitted.Kind, permitted.Required)
		}
	}

	for _, key := range resolved.Keys() {
		if key.Verb() != verb || !resolved[key].Flag {
			continue
		}

		name, isArgument := key.Argument()
		if !isArgument {
			continue
		}

		if _, declared := command.Arguments[name]; !declared {
			t.Errorf("%s: the resolver permits %s on %s, which the simulator would refuse as %s",
				model, name, command.Name, simulator.ReasonUnknownArgument)
		}
	}
}

// everyCapability is the union of every profile, standing in for a driver
// baseline that implements the whole vocabulary. A baseline is the ceiling and
// a profile may only narrow it, so a union here is what lets the profile tier
// be the thing under test.
func everyCapability(registry profile.Registry) connectorapi.Set {
	union := connectorapi.Set{}

	for _, p := range registry.Profiles {
		for key, value := range p.Capabilities {
			if _, seen := union[key]; !seen {
				union[key] = value
			}
		}
	}

	return union
}

// everyVerb is every command any model declares, which is the question set both
// answers are checked against. Asking each model only about its own verbs would
// never catch a verb it wrongly accepts.
func everyVerb(registry profile.Registry) []string {
	seen := map[string]bool{}

	var verbs []string

	for _, p := range registry.Profiles {
		for _, key := range p.Capabilities.Keys() {
			suffix, isCommand := strings.CutPrefix(string(key.Verb()), verbPrefix)
			if !isCommand || seen[suffix] {
				continue
			}

			seen[suffix] = true

			verbs = append(verbs, strings.ToUpper(suffix))
		}
	}

	slices.Sort(verbs)

	return verbs
}
