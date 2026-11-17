// Package simulator is a printer at the protocol level. It speaks the
// registration handshake, the nine-byte framing and the command lifecycle of
// Buddy firmware, for any printer model the capability profiles cover.
//
// It does not emulate the vendor's firmware instruction by instruction. It
// runs over whatever net.Conn its Dial hook returns, which in a unit test is a
// net.Pipe. testing/synctest excludes network I/O from durable blocking, so
// anything on a real socket leaves virtual time. A test that drives the
// simulator over a pipe stays inside a bubble, and a six-week partition costs
// microseconds.
//
// Nothing about a printer model is written here. Every model is projected from
// internal/printer/profile, which is extracted from the shipping firmware
// matrix and held to it by a divergence test. The simulator's behaviour comes
// from profiles.json and is never coded per model. The projection happens once
// per process when the first printer is built, so there is no second copy of
// the matrix to keep in step.
//
// Every failure mode is an explicit input. Nothing in the package is random: a
// simulator that fails at random teaches people to rerun.
package simulator

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// The two capability namespaces the wire has a spelling for. A key in neither
// is refused by the projection and not skipped, because the simulator would
// answer wrongly for a capability it does not know.
const (
	verbPrefix    = "core.command."
	featurePrefix = "core.feature."
)

// ErrUnknownModel is a model name no capability profile covers. There is no
// fallback, because a simulator must not answer for a model nobody described.
var ErrUnknownModel = errors.New("no capability profile covers this model")

// Model is one printer model's wire vocabulary: the identity it reports and the
// commands it answers.
//
// It is a projection of one profile.Profile. The two vocabularies differ in
// case and in shape: a profile spells core.command.set_nozzle_temperature and
// holds each argument as a key beside its verb, while a J frame carries
// SET_NOZZLE_TEMPERATURE with its arguments in kwargs. modelFor is the only
// place that writes the correspondence down.
//
// Obtain one from ModelByName. The zero Model answers no command, because the
// index a lookup goes through is built by the projection.
type Model struct {
	// Name is the model as the profile spells it, which is also what the
	// printer reports in the User-Agent-Printer header.
	Name string
	// DeviceTypeID is the code this model reports, and what registration
	// carries as printer_type.
	DeviceTypeID string
	// FirmwareDeviceTypeID is the code whose firmware build this model runs. It
	// differs from DeviceTypeID for the models that share a firmware image.
	FirmwareDeviceTypeID string
	// Firmware is the range the profile covers, closed at the bottom and open
	// at the top. Its lower bound is the firmware a printer reports when the
	// caller names none.
	Firmware profile.Range
	// Features are the firmware feature flags, in the wire spelling, sorted.
	Features []string
	// Commands are the verbs this model accepts, sorted by name. The order
	// makes a SET_VALUE frame resolve to the same verb on every run.
	Commands []Command
	// byName indexes Commands. It is built once by the projection and shared by
	// every copy of the model, so a whole fleet of printers costs one index.
	byName map[string]Command
}

// Command is one verb a model accepts, in the spelling the wire uses.
type Command struct {
	// Name is the wire verb, such as SET_NOZZLE_TEMPERATURE.
	Name string
	// Arguments are the kwargs the verb accepts, keyed by the name a J frame
	// carries them under and valued by the profile's own descriptor. A verb
	// accepts exactly these and no others.
	Arguments map[string]connectorapi.Value
}

// Models returns the name of every model the simulator can be, sorted. The
// error is the embedded profile data failing to load, which only a broken
// build causes.
func Models() ([]string, error) {
	loaded, err := builtin()
	if err != nil {
		return nil, err
	}

	return slices.Clone(loaded.names), nil
}

// ModelByName returns a model by name, matched without regard to case because
// firmware and gcode metadata disagree about it, as profile lookup does.
//
// A name nothing covers is ErrUnknownModel, wrapped with the names that are
// covered.
func ModelByName(name string) (Model, error) {
	loaded, err := builtin()
	if err != nil {
		return Model{}, err
	}

	model, ok := loaded.byName[strings.ToLower(name)]
	if !ok {
		return Model{}, fmt.Errorf("%w: %q. Known models: %s",
			ErrUnknownModel, name, strings.Join(loaded.names, ", "))
	}

	return model, nil
}

// CapabilityKeyFor returns the capability key a wire verb is the spelling of.
// It is the inverse of the projection. The round trip is lossless only while
// no verb carries a character the case change loses, and
// TestTheWireSpellingRoundTripsThroughTheCapabilityVocabulary checks that.
//
// It is exported so a test can ask this package and internal/printer/capability
// the same question and compare the answers. That shows the simulator refuses
// what the host would have refused.
func CapabilityKeyFor(command string) connectorapi.Capability {
	return connectorapi.Capability(verbPrefix + strings.ToLower(command))
}

// Command returns the verb by wire name. ok is false when this model does not
// declare it.
func (m Model) Command(name string) (Command, bool) {
	c, ok := m.byName[name]

	return c, ok
}

// verbForArguments finds the verb a SET_VALUE frame asks for.
//
// The shipping connector rewrites several capability verbs into a single
// SET_VALUE frame before they reach the wire. profiles.json does not record
// that: it holds the host's capability vocabulary and not which frame a verb
// travels as. The argument set is the only thing in the data that can say
// which verb a SET_VALUE means, so this matches on it. No model is named
// here: a model gains a setter by a profile edit.
//
// It is confined to verbs named SET_*, because every rewritten verb is one.
// Matching on an argument set alone would let SET_VALUE{path} land on
// CREATE_FOLDER.
func (m Model) verbForArguments(kwargs map[string]any) (Command, bool) {
	if len(kwargs) == 0 {
		return Command{}, false
	}

	for _, command := range m.Commands {
		if len(command.Arguments) != len(kwargs) || !strings.HasPrefix(command.Name, "SET_") {
			continue
		}

		if declaresExactly(command, kwargs) {
			return command, true
		}
	}

	return Command{}, false
}

func declaresExactly(command Command, kwargs map[string]any) bool {
	for name := range command.Arguments {
		if _, present := kwargs[name]; !present {
			return false
		}
	}

	return true
}

// catalog is every model the profiles cover, indexed the two ways a caller asks
// for one.
type catalog struct {
	// names are the models in the profile's own spelling, sorted, each once.
	names []string
	// byName is keyed by the lower-cased name, as profile lookup matches.
	byName map[string]Model
}

// builtin projects the embedded profiles once per process.
//
// The parse and the projection are the costly part and do not depend on the
// printer being built, so they run once and the models are shared. Each Model
// carries its own command index, so nothing after this allocates per printer.
var builtin = sync.OnceValues(func() (catalog, error) {
	registry, err := profile.Builtin()
	if err != nil {
		return catalog{}, fmt.Errorf("load the builtin capability profiles: %w", err)
	}

	return catalogFor(registry)
})

// catalogFor projects a whole registry.
//
// A model with more than one firmware band keeps the first, in file order, as
// profile.Registry.Models does. The simulator cannot be a firmware version, so
// it has no way to choose between two bands. The first in file order is a
// stable choice, and map order would not be.
func catalogFor(registry profile.Registry) (catalog, error) {
	loaded := catalog{
		names:  make([]string, 0, len(registry.Profiles)),
		byName: make(map[string]Model, len(registry.Profiles)),
	}

	for _, p := range registry.Profiles {
		key := strings.ToLower(p.Model)
		if _, seen := loaded.byName[key]; seen {
			continue
		}

		model, err := modelFor(p)
		if err != nil {
			return catalog{}, err
		}

		loaded.names = append(loaded.names, model.Name)
		loaded.byName[key] = model
	}

	slices.Sort(loaded.names)

	return loaded, nil
}

// modelFor projects one capability profile onto the wire.
//
// A profile key is present or absent and is never false. A verb whose Flag is
// unset is a verb the model does not have, so it is dropped here and not
// projected as a disabled verb.
//
// An argument key whose verb is absent is dropped with it. The wire has no way
// to send an argument without its verb.
func modelFor(p profile.Profile) (Model, error) {
	model := Model{
		Name:                 p.Model,
		DeviceTypeID:         p.DeviceTypeID,
		FirmwareDeviceTypeID: p.FirmwareDeviceTypeID,
		Firmware:             p.Firmware,
	}

	verbs := map[string]bool{}
	arguments := map[string]map[string]connectorapi.Value{}

	for _, key := range p.Capabilities.Keys() {
		value := p.Capabilities[key]
		if !value.Flag {
			continue
		}

		verb, isCommand := wireVerb(key.Verb())

		switch name, isArgument := key.Argument(); {
		case isCommand && isArgument:
			if arguments[verb] == nil {
				arguments[verb] = map[string]connectorapi.Value{}
			}

			arguments[verb][name] = value
		case isCommand:
			verbs[verb] = true
		case strings.HasPrefix(string(key), featurePrefix):
			model.Features = append(model.Features, wireFeature(key))
		default:
			return Model{}, fmt.Errorf(
				"%s: capability %q is neither a command nor a feature, so the simulator has no "+
					"wire spelling for it", p.Model, key)
		}
	}

	model.Commands = make([]Command, 0, len(verbs))
	model.byName = make(map[string]Command, len(verbs))

	for verb := range verbs {
		model.Commands = append(model.Commands, Command{Name: verb, Arguments: arguments[verb]})
	}

	slices.SortFunc(model.Commands, func(a, b Command) int { return strings.Compare(a.Name, b.Name) })
	slices.Sort(model.Features)

	for _, command := range model.Commands {
		model.byName[command.Name] = command
	}

	return model, nil
}

// wireVerb returns the wire spelling of a command capability. ok is false for
// any other key.
func wireVerb(key connectorapi.Capability) (string, bool) {
	suffix, ok := strings.CutPrefix(string(key), verbPrefix)
	if !ok {
		return "", false
	}

	return strings.ToUpper(suffix), true
}

// wireFeature returns the wire spelling of a firmware feature flag.
func wireFeature(key connectorapi.Capability) string {
	return strings.ToUpper(strings.TrimPrefix(string(key), featurePrefix))
}
