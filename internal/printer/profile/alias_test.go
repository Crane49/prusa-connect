package profile_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// firmwareFloor is a version every shipped profile covers, so a spelling test
// fails on the spelling and never on the range.
const firmwareFloor = "6.4.0"

// The gate the alias table exists for. Every model this build ships is reached
// by every spelling a printer plausibly sends for it, and a model that is not
// is a machine held to the coverage tier and offered verbs it does not have.
//
// The spellings are generated from the model rather than listed, so a model
// added to profiles.json is covered the moment it is added. A spelling that no
// rule reaches is a row in aliases.json, which is a data change and not a
// release.
func TestEveryModelIsReachedByEverySpellingAPrinterSends(t *testing.T) {
	t.Parallel()

	registry, err := profile.Builtin()
	if err != nil {
		t.Fatalf("loading the built-in profiles: %v", err)
	}

	models := registry.Models()
	if len(models) == 0 {
		t.Fatal("the built-in registry carries no model, so this gate compares nothing")
	}

	for _, model := range models {
		for _, reported := range spellingsOf(t, registry, model) {
			matched, ok := registry.Match(reported, label(firmwareFloor))

			switch {
			case !ok:
				t.Errorf("a printer reporting %q matched no profile, and %s is in the file",
					reported, model)
			case !strings.EqualFold(matched.Model, model):
				t.Errorf("a printer reporting %q matched %s, want %s", reported, matched.Model, model)
			}
		}
	}
}

// spellingsOf is what "a spelling a printer plausibly sends" means, in one
// place. Each entry is a shape observed somewhere: the file's own name, the
// device type code a registration carries, the case the gcode metadata uses,
// and the separators a firmware or a person puts between the words.
func spellingsOf(t *testing.T, registry profile.Registry, model string) []string {
	t.Helper()

	spellings := []string{
		model,
		strings.ToUpper(model),
		strings.ToLower(model),
		strings.ReplaceAll(strings.ReplaceAll(model, "_", ""), ".", ""),
		strings.ReplaceAll(strings.ReplaceAll(model, "_", " "), ".", " "),
		"  " + model + "  ",
		spaceAtCaseBoundaries(model),
	}

	code := deviceTypeOf(registry, model)
	if code == "" {
		t.Errorf("%s carries no device type code, which is the spelling a registration reports", model)

		return spellings
	}

	return append(spellings, code)
}

// spaceAtCaseBoundaries writes "CoreOne" as "Core One", which is the spelling a
// person types and a marketing name uses.
func spaceAtCaseBoundaries(model string) string {
	var out strings.Builder

	for i := range len(model) {
		c := model[i]
		if i > 0 && c >= 'A' && c <= 'Z' && model[i-1] >= 'a' && model[i-1] <= 'z' {
			out.WriteByte(' ')
		}

		out.WriteByte(c)
	}

	return out.String()
}

func deviceTypeOf(registry profile.Registry, model string) string {
	for _, candidate := range registry.Profiles {
		if strings.EqualFold(candidate.Model, model) {
			return candidate.DeviceTypeID
		}
	}

	return ""
}

// The device type code is what a real registration reports, and the row on
// the old backlog that sent this work names it as the string that matched nothing. This
// pins the four Core One codes by value rather than by derivation, so a
// generator that stopped writing the codes out is a failure here too.
func TestADeviceTypeCodeReachesItsModel(t *testing.T) {
	t.Parallel()

	registry, err := profile.Builtin()
	if err != nil {
		t.Fatalf("loading the built-in profiles: %v", err)
	}

	for code, want := range map[string]string{
		"7.1.0":  "CoreOne",
		"7.10.0": "CoreOne_INDX",
		"8.1.0":  "CoreOneL",
		"8.10.0": "CoreOneL_INDX",
		"1.4.0":  "MK4",
		"1.3.10": "MK3.9S",
	} {
		matched, ok := registry.Match(code, label(firmwareFloor))
		if !ok {
			t.Errorf("a printer reporting %q matched no profile", code)

			continue
		}

		if matched.Model != want {
			t.Errorf("a printer reporting %q matched %s, want %s", code, matched.Model, want)
		}
	}
}

// A firmware device type is shared between models, so it names no single one.
// Matching on it would give an MK3.9 the MK4 profile, which is the phantom
// capability the intersection exists to forbid.
func TestASharedFirmwareDeviceTypeIsNotASpelling(t *testing.T) {
	t.Parallel()

	registry, err := profile.Builtin()
	if err != nil {
		t.Fatalf("loading the built-in profiles: %v", err)
	}

	shared := 0

	for _, candidate := range registry.Profiles {
		if candidate.FirmwareDeviceTypeID == candidate.DeviceTypeID {
			continue
		}

		shared++

		// The code may reach the model that owns it, and must never reach this
		// one, which only runs its firmware image.
		matched, ok := registry.Match(candidate.FirmwareDeviceTypeID, label(firmwareFloor))
		if ok && strings.EqualFold(matched.Model, candidate.Model) {
			t.Errorf("%s is reached by %q, which is the firmware it shares with another model",
				candidate.Model, candidate.FirmwareDeviceTypeID)
		}
	}

	if shared == 0 {
		t.Error("no model reports a firmware device type of its own, so this gate watches nothing")
	}
}

// A spelling nothing recognises has to stay unmatched. The coverage tier and
// the warning beside it are the answer, and a normaliser loose enough to guess
// would hand out a capability set instead.
func TestASpellingNoRuleReachesMatchesNothing(t *testing.T) {
	t.Parallel()

	registry, err := profile.Builtin()
	if err != nil {
		t.Fatalf("loading the built-in profiles: %v", err)
	}

	for _, reported := range []string{
		"", " ", "...", "Voron", "Bambu X1C", "MK3S", "MK5", "Core Two", "9.1.0", "710",
	} {
		if matched, ok := registry.Match(reported, label(firmwareFloor)); ok {
			t.Errorf("a printer reporting %q was given the %s profile", reported, matched.Model)
		}
	}
}

func TestAnAliasReachesTheModelItNames(t *testing.T) {
	t.Parallel()

	registry := aliased(profile.Alias{Spelling: "Prusa Machine 7", Model: "CoreOne", Source: "a test"})

	matched, ok := registry.Match("prusa machine 7", label(firmwareFloor))
	if !ok {
		t.Fatal("a printer reporting an alias spelling matched no profile")
	}

	if matched.Model != "CoreOne" {
		t.Errorf("the alias reached %s, want CoreOne", matched.Model)
	}
}

func TestValidateRefusesAnAliasThatCannotBeActedOn(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		alias profile.Alias
		want  error
	}{
		{
			name:  "a model no profile covers",
			alias: profile.Alias{Spelling: "Prusa Machine 7", Model: "Voron", Source: "a test"},
			want:  profile.ErrUnknownAliasModel,
		},
		{
			name:  "a spelling another model already answers to",
			alias: profile.Alias{Spelling: "core-one", Model: "MK4", Source: "a test"},
			want:  profile.ErrAmbiguousSpelling,
		},
		{
			name:  "a device type another model already answers to",
			alias: profile.Alias{Spelling: "1.4.0", Model: "CoreOne", Source: "a test"},
			want:  profile.ErrAmbiguousSpelling,
		},
		{
			name:  "no spelling at all",
			alias: profile.Alias{Model: "CoreOne", Source: "a test"},
			want:  profile.ErrInvalidProfile,
		},
		{
			name:  "no source",
			alias: profile.Alias{Spelling: "Prusa Machine 7", Model: "CoreOne"},
			want:  profile.ErrInvalidProfile,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := profile.Validate(aliased(tc.alias))
			if !errors.Is(err, tc.want) {
				t.Errorf("validating %+v gave %v, want %v", tc.alias, err, tc.want)
			}
		})
	}
}

// Two models the normaliser cannot tell apart is the one way this rule grants a
// printer another machine's set, so the file is refused rather than matched by
// its own order.
func TestValidateRefusesTwoModelsOneSpellingReaches(t *testing.T) {
	t.Parallel()

	registry := profile.Registry{
		Schema: profile.CurrentSchema,
		Profiles: []profile.Profile{
			{
				Model: "Core.One", DeviceTypeID: "7.1.0",
				Firmware: profile.Range{From: firmwareFloor}, Capabilities: connectorapi.Set{verbHome: {Flag: true}},
			},
			{
				Model: "Core_One", DeviceTypeID: "7.2.0",
				Firmware: profile.Range{From: firmwareFloor}, Capabilities: connectorapi.Set{verbHome: {Flag: true}},
			},
		},
	}

	if err := profile.Validate(registry); !errors.Is(err, profile.ErrAmbiguousSpelling) {
		t.Errorf("two models one spelling reaches were accepted: %v", err)
	}
}

// The shipped file has to survive its own rule, or every gate above is green
// over a registry the binary refuses at startup.
func TestTheBuiltinModelsAreThirteenDistinctSpellings(t *testing.T) {
	t.Parallel()

	registry, err := profile.Builtin()
	if err != nil {
		t.Fatalf("loading the built-in profiles: %v", err)
	}

	if err = profile.Validate(registry); err != nil {
		t.Errorf("the built-in registry does not satisfy the spelling rules: %v", err)
	}
}

// aliased is the built-in registry with one row added, so an alias case is run
// against the models this build really ships.
func aliased(rows ...profile.Alias) profile.Registry {
	registry, err := profile.Builtin()
	if err != nil {
		panic("loading the built-in profiles: " + err.Error())
	}

	registry.Aliases = append(registry.Aliases, rows...)

	return registry
}
