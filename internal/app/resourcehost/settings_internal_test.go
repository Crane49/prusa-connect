package resourcehost

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// moduleWith is a declared service instance whose manifest recorded options and
// whose operator wrote block.
func moduleWith(options []install.Option, block string) *serviceSpec {
	return &serviceSpec{
		typeName: "com.github.acme.spools/spools",
		module:   &install.ModuleEntry{Options: options},
		config:   json.RawMessage(block),
	}
}

// shelf is the settings list of a module with one number, one nested block and
// one list.
func shelf() []install.Option {
	return []install.Option{
		{Key: "shelf_size", Type: extension.OptionInt, Default: "500", Summary: "How many spools."},
		{Key: "token", Type: extension.OptionSecret, Required: true, Summary: "What it presents upstream."},
		{
			Key: "limits", Type: extension.OptionObject,
			Default: `{"per_device":10,"per_day":100}`, Summary: "What it will not go over.",
		},
		{Key: "materials", Type: extension.OptionList, Default: `["pla","petg"]`, Summary: "What it tracks."},
	}
}

// settled is the merged block as a map, for a case to read one key of.
func settled(t *testing.T, declared *serviceSpec) map[string]any {
	t.Helper()

	merged, err := moduleSettings(declared, declared.config)
	if err != nil {
		t.Fatalf("merge the settings: %v", err)
	}

	var held map[string]any
	if err = json.Unmarshal(merged, &held); err != nil {
		t.Fatalf("read the merged settings: %v", err)
	}

	return held
}

// TestTheDefaultsAreWhatAModuleStartsOn: an operator who wrote nothing gets the
// configuration the module shipped with itself.
func TestTheDefaultsAreWhatAModuleStartsOn(t *testing.T) {
	t.Parallel()

	options := shelf()
	options[1].Required = false

	held := settled(t, moduleWith(options, ""))

	if held["shelf_size"] != float64(500) {
		t.Errorf("shelf_size starts at %v, want the declared default", held["shelf_size"])
	}

	if _, written := held["token"]; written {
		t.Errorf("a key with no default is written as %v", held["token"])
	}
}

// TestTheOperatorOverridesOneKeyAtATime: writing one key keeps the defaults of
// every other key, at the top level and inside a nested block. A list is
// replaced whole, because half the author's list and half the operator's is
// neither of them.
func TestTheOperatorOverridesOneKeyAtATime(t *testing.T) {
	t.Parallel()

	held := settled(t, moduleWith(shelf(), `{
		"token": "abc",
		"limits": {"per_day": 5},
		"materials": ["asa"]
	}`))

	if held["shelf_size"] != float64(500) {
		t.Errorf("a key nobody wrote reads as %v", held["shelf_size"])
	}

	limits, isBlock := held["limits"].(map[string]any)
	if !isBlock {
		t.Fatalf("limits reads as %v", held["limits"])
	}

	if limits["per_day"] != float64(5) {
		t.Errorf("the key the operator wrote reads as %v", limits["per_day"])
	}

	if limits["per_device"] != float64(10) {
		t.Errorf("a nested key nobody wrote reads as %v, want the declared default", limits["per_device"])
	}

	materials, isList := held["materials"].([]any)
	if !isList || len(materials) != 1 || materials[0] != "asa" {
		t.Errorf("the list reads as %v, want the operator's whole list", held["materials"])
	}
}

// TestAKeyTheModuleDoesNotDeclareRefusesTheStart: the refusal names the key and
// what the module does read, so an operator fixes the block without reading the
// module's source.
func TestAKeyTheModuleDoesNotDeclareRefusesTheStart(t *testing.T) {
	t.Parallel()

	_, err := moduleSettings(moduleWith(shelf(), `{"shelfsize": 10}`), json.RawMessage(`{"shelfsize": 10}`))
	if !errors.Is(err, ErrSettingsUnknown) {
		t.Fatalf("the refusal is %v, want the one for an undeclared key", err)
	}

	for _, want := range []string{`"shelfsize"`, `"shelf_size"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %s: %v", want, err)
		}
	}
}

// TestARequiredKeyNobodyWroteRefusesTheStart: a key the module cannot run
// without and no server can work out stops the instance, with the key named.
func TestARequiredKeyNobodyWroteRefusesTheStart(t *testing.T) {
	t.Parallel()

	declared := moduleWith(shelf(), `{"shelf_size": 10}`)

	_, err := moduleSettings(declared, declared.config)
	if !errors.Is(err, ErrSettingsMissing) {
		t.Fatalf("the refusal is %v, want the one for a required key", err)
	}

	if !strings.Contains(err.Error(), `"token"`) {
		t.Errorf("the refusal does not name the key: %v", err)
	}
}

// TestAModuleThatDeclaresNoSettingsTakesNone: a config block written for one is
// a block nothing will ever read, so it is refused rather than dropped.
func TestAModuleThatDeclaresNoSettingsTakesNone(t *testing.T) {
	t.Parallel()

	declared := moduleWith(nil, `{"shelf_size": 10}`)

	if _, err := moduleSettings(declared, declared.config); !errors.Is(err, ErrSettingsUnknown) {
		t.Fatalf("the refusal is %v, want the one for an undeclared key", err)
	}

	empty := moduleWith(nil, "")

	merged, err := moduleSettings(empty, empty.config)
	if err != nil {
		t.Fatalf("a module with no settings and no block: %v", err)
	}

	if merged != nil {
		t.Fatalf("a module with no settings is sent %s", merged)
	}
}

// TestADeclaredDefaultThatIsNotItsTypeRefusesTheStart: the manifest writes a
// default the way an operator would, and one that does not read as its declared
// type is the module author's mistake rather than the operator's.
func TestADeclaredDefaultThatIsNotItsTypeRefusesTheStart(t *testing.T) {
	t.Parallel()

	declared := moduleWith([]install.Option{
		{Key: "shelf_size", Type: extension.OptionInt, Default: "many", Summary: "How many spools."},
	}, "")

	if _, err := moduleSettings(declared, declared.config); !errors.Is(err, ErrSettingsShape) {
		t.Fatalf("the refusal is %v, want the one for a value that is not its type", err)
	}
}
