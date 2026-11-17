package profile_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

const nozzleArgument = connectorapi.Capability("core.command.set_nozzle_temperature#nozzle_temperature")

// The hand-written bound file is data nothing generates, so the only thing
// holding it to the profiles beside it is this.
func TestTheBuiltinBoundsCoverTheModelsTheyName(t *testing.T) {
	t.Parallel()

	registry, err := profile.Builtin()
	if err != nil {
		t.Fatalf("loading the built-in profiles: %v", err)
	}

	if len(registry.Bounds) == 0 {
		t.Fatal("the built-in registry carries no argument bounds, so no setpoint is ranged")
	}

	models := map[string]bool{}
	for _, model := range registry.Models() {
		models[strings.ToLower(model)] = true
	}

	for _, bound := range registry.Bounds {
		if bound.Source == "" {
			t.Errorf("the bound on %s names no source", bound.Argument)
		}

		if bound.Min > bound.Max {
			t.Errorf("%s runs from %v to %v", bound.Argument, bound.Min, bound.Max)
		}

		if len(bound.Models) == 0 {
			t.Errorf("the bound on %s names no model", bound.Argument)
		}

		for _, model := range bound.Models {
			if !models[strings.ToLower(model)] {
				t.Errorf("%s bounds %s, which no profile covers", bound.Argument, model)

				continue
			}

			assertBoundReachedTheProfile(t, registry, bound, model)
		}
	}
}

// assertBoundReachedTheProfile is the other half: a row that loads and never
// reaches the capability set is a number nothing enforces.
func assertBoundReachedTheProfile(t *testing.T, registry profile.Registry, bound profile.Bound, model string) {
	t.Helper()

	for _, candidate := range registry.Profiles {
		if !strings.EqualFold(candidate.Model, model) {
			continue
		}

		value, ok := candidate.Capabilities[bound.Argument]
		if !ok || !value.Flag {
			t.Errorf("%s bounds %s, which that model does not carry", bound.Argument, model)

			continue
		}

		if !value.HasMin || !value.HasLimit {
			t.Errorf("%s of %s carries no range after the bounds were applied", bound.Argument, model)

			continue
		}

		if value.Min < bound.Min || value.Limit > bound.Max {
			t.Errorf("%s of %s resolved to %v..%v, wider than the %v..%v the file states",
				bound.Argument, model, value.Min, value.Limit, bound.Min, bound.Max)
		}
	}
}

// Every model in the file is bounded on every setpoint it carries. A model
// added to profiles.json and not to bounds.json is a printer whose operator can
// still send 9999.
func TestEveryModelIsBoundedOnEverySetpointItCarries(t *testing.T) {
	t.Parallel()

	registry, err := profile.Builtin()
	if err != nil {
		t.Fatalf("loading the built-in profiles: %v", err)
	}

	setpoints := []connectorapi.Capability{
		nozzleArgument,
		"core.command.set_bed_temperature#bed_temperature",
		"core.command.set_chamber_target_temp#chamber.target_temp",
		"core.command.set_speed#speed",
		"core.command.set_flow#flow",
	}

	for _, candidate := range registry.Profiles {
		for _, key := range setpoints {
			value, ok := candidate.Capabilities[key]
			if !ok || !value.Flag {
				continue
			}

			if !value.HasMin || !value.HasLimit {
				t.Errorf("%s carries %s with no range, so bounds.json has no row for it",
					candidate.Model, key)
			}
		}
	}
}

// A row may only narrow. The merge with a driver's baseline is an intersection,
// so a row wider than what the profile already says has to leave the profile
// alone rather than loosen it.
func TestAWiderBoundNarrowsNothing(t *testing.T) {
	t.Parallel()

	tight := connectorapi.Set{
		"core.command.set_nozzle_temperature": {Flag: true},
		nozzleArgument: {
			Flag: true, Kind: connectorapi.KindInteger, Required: true,
			Min: 0, HasMin: true, Limit: 240, HasLimit: true,
		},
	}

	registry := profile.Registry{
		Schema: profile.CurrentSchema,
		Profiles: []profile.Profile{{
			Model: "MODEL", DeviceTypeID: "9.9.9", FirmwareDeviceTypeID: "9.9.9",
			Firmware: profile.Range{From: "1.0.0"}, Capabilities: tight,
		}},
		Bounds: []profile.Bound{{
			Argument: nozzleArgument, Models: []string{"MODEL"},
			Min: -50, Max: 900, Source: "a row somebody got wrong",
		}},
	}

	loaded := reload(t, registry)

	value := loaded.Profiles[0].Capabilities[nozzleArgument]
	if value.Min != 0 || value.Limit != 240 {
		t.Errorf("a wider row moved the profile's range to %v..%v, want 0..240", value.Min, value.Limit)
	}
}

// A narrower row is the point of the file.
func TestANarrowerBoundReplacesTheProfilesRange(t *testing.T) {
	t.Parallel()

	registry := profile.Registry{
		Schema: profile.CurrentSchema,
		Profiles: []profile.Profile{{
			Model: "MODEL", DeviceTypeID: "9.9.9", FirmwareDeviceTypeID: "9.9.9",
			Firmware: profile.Range{From: "1.0.0"},
			Capabilities: connectorapi.Set{
				"core.command.set_nozzle_temperature": {Flag: true},
				nozzleArgument: {
					Flag: true, Kind: connectorapi.KindInteger, Required: true,
					Min: 0, HasMin: true, Limit: 300, HasLimit: true,
				},
			},
		}},
		Bounds: []profile.Bound{{
			Argument: nozzleArgument, Models: []string{"MODEL"},
			Min: 10, Max: 280, Source: "the published ceiling of a machine somebody measured",
		}},
	}

	loaded := reload(t, registry)

	value := loaded.Profiles[0].Capabilities[nozzleArgument]
	if value.Min != 10 || value.Limit != 280 {
		t.Errorf("the narrower row left the range at %v..%v, want 10..280", value.Min, value.Limit)
	}
}

// A row naming a model or an argument the file does not carry is refused rather
// than ignored, because a bound nothing applies reads as a bound in force.
func TestValidateRefusesABoundThatCannotBeActedOn(t *testing.T) {
	t.Parallel()

	base := func() profile.Registry {
		return profile.Registry{
			Schema: profile.CurrentSchema,
			Profiles: []profile.Profile{{
				Model: "MODEL", DeviceTypeID: "9.9.9", FirmwareDeviceTypeID: "9.9.9",
				Firmware: profile.Range{From: "1.0.0"},
				Capabilities: connectorapi.Set{
					"core.command.set_nozzle_temperature": {Flag: true},
					nozzleArgument: {
						Flag: true, Kind: connectorapi.KindInteger, Required: true,
					},
				},
			}},
		}
	}

	for _, testCase := range []struct {
		name  string
		bound profile.Bound
		want  error
	}{
		{
			name: "a model no profile covers",
			bound: profile.Bound{Argument: nozzleArgument, Models: []string{"MK9000"},
				Min: 0, Max: 300, Source: "measured"},
			want: profile.ErrUnknownBoundModel,
		},
		{
			name: "a verb rather than an argument",
			bound: profile.Bound{Argument: "core.command.set_nozzle_temperature",
				Models: []string{"MODEL"}, Min: 0, Max: 300, Source: "measured"},
			want: profile.ErrInvalidBound,
		},
		{
			name: "an argument the model does not carry",
			bound: profile.Bound{Argument: "core.command.set_flow#flow",
				Models: []string{"MODEL"}, Min: 0, Max: 300, Source: "measured"},
			want: profile.ErrInvalidBound,
		},
		{
			name: "its ends the wrong way round",
			bound: profile.Bound{Argument: nozzleArgument, Models: []string{"MODEL"},
				Min: 300, Max: 0, Source: "measured"},
			want: profile.ErrInvalidBound,
		},
		{
			name: "no source",
			bound: profile.Bound{Argument: nozzleArgument, Models: []string{"MODEL"},
				Min: 0, Max: 300},
			want: profile.ErrInvalidBound,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			registry := base()
			registry.Bounds = []profile.Bound{testCase.bound}

			err := profile.Validate(registry)
			if !errors.Is(err, testCase.want) {
				t.Errorf("validation said %v, want %v", err, testCase.want)
			}
		})
	}
}

// Two rows for one argument of one model would make the answer depend on file
// order.
func TestValidateRefusesTwoBoundsOnOneArgument(t *testing.T) {
	t.Parallel()

	one := profile.Bound{Argument: nozzleArgument, Models: []string{"MODEL"},
		Min: 0, Max: 300, Source: "measured"}
	two := one
	two.Max = 280

	registry := profile.Registry{
		Schema: profile.CurrentSchema,
		Profiles: []profile.Profile{{
			Model: "MODEL", DeviceTypeID: "9.9.9", FirmwareDeviceTypeID: "9.9.9",
			Firmware: profile.Range{From: "1.0.0"},
			Capabilities: connectorapi.Set{
				"core.command.set_nozzle_temperature": {Flag: true},
				nozzleArgument: {
					Flag: true, Kind: connectorapi.KindInteger, Required: true,
				},
			},
		}},
		Bounds: []profile.Bound{one, two},
	}

	if err := profile.Validate(registry); !errors.Is(err, profile.ErrInvalidBound) {
		t.Errorf("validation said %v, want %v", err, profile.ErrInvalidBound)
	}
}

// reload writes a registry out and reads it back, which is the path a
// deployment's own profile file takes and the only one that applies its bounds.
func reload(t *testing.T, registry profile.Registry) profile.Registry {
	t.Helper()

	encoded, err := json.Marshal(registry)
	if err != nil {
		t.Fatalf("encoding the registry: %v", err)
	}

	loaded, err := profile.Load(encoded)
	if err != nil {
		t.Fatalf("loading the registry back: %v", err)
	}

	return loaded
}
