package manifest_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// withOptions is a whole manifest carrying the options list under test, as JSON
// because a JSON document is a YAML document and the standard library reads it.
func withOptions(options string) []byte {
	return []byte(`{
		"name": "spools",
		"kind": "sink","license":"MIT",
		"version": "1.0.0",
		"contract_major": 1,
		"options": ` + options + `
	}`)
}

// TestTheDeclaredSettingsAreReadInOrder: the options list is a module's own
// default configuration, so the host needs every field of every entry and the
// order the author wrote them in.
func TestTheDeclaredSettingsAreReadInOrder(t *testing.T) {
	t.Parallel()

	held, err := manifest.Parse("module.yaml", withOptions(`[
		{"key": "shelf_size", "type": "int", "default": "500", "summary": "How many spools this module keeps."},
		{"key": "token", "type": "secret", "required": true, "summary": "What this module presents upstream."}
	]`), json.Unmarshal)
	if err != nil {
		t.Fatalf("read the manifest: %v", err)
	}

	want := []manifest.Option{
		{
			Key:     "shelf_size",
			Type:    extension.OptionInt,
			Default: "500",
			Summary: "How many spools this module keeps.",
		},
		{
			Key:      "token",
			Type:     extension.OptionSecret,
			Required: true,
			Summary:  "What this module presents upstream.",
		},
	}

	if len(held.Options) != len(want) {
		t.Fatalf("the manifest declares %d settings, want %d", len(held.Options), len(want))
	}

	for index, option := range held.Options {
		if option != want[index] {
			t.Errorf("setting %d reads as %+v, want %+v", index, option, want[index])
		}
	}
}

// TestASettingAnOperatorCannotActOnIsRefused: every refusal here reaches the
// author at build time. A key with no sentence, a type nobody knows or a
// required key with a default beside it would each reach an operator as a
// documented setting that is not one.
func TestASettingAnOperatorCannotActOnIsRefused(t *testing.T) {
	t.Parallel()

	for name, options := range map[string]string{
		"a key with no name":                 `[{"type": "int", "summary": "How many."}]`,
		"a key that is not lower_snake_case": `[{"key": "ShelfSize", "type": "int", "summary": "How many."}]`,
		"a type nobody knows":                `[{"key": "shelf_size", "type": "integer", "summary": "How many."}]`,
		"a key with no sentence":             `[{"key": "shelf_size", "type": "int"}]`,
		"required with a default":            `[{"key": "token", "type": "secret", "required": true, "default": "x", "summary": "A token."}]`,
		"one key written twice": `[
			{"key": "shelf_size", "type": "int", "summary": "How many."},
			{"key": "shelf_size", "type": "int", "summary": "How many, again."}
		]`,
		"an entry carrying a key this version does not read": `[
			{"key": "shelf_size", "type": "int", "summary": "How many.", "secret": true}
		]`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := manifest.Parse("module.yaml", withOptions(options), json.Unmarshal)
			if !errors.Is(err, manifest.ErrInvalid) {
				t.Fatalf("the refusal is %v, want an invalid manifest", err)
			}

			if !strings.Contains(err.Error(), "options") {
				t.Errorf("the refusal does not name the options list: %v", err)
			}
		})
	}
}

// TestAManifestWithNoOptionsTakesNoSettings: a module that declares none is not
// a module with an empty list, and the host refuses a config block written for
// one.
func TestAManifestWithNoOptionsTakesNoSettings(t *testing.T) {
	t.Parallel()

	held, err := manifest.Parse("module.yaml", []byte(`{
		"name": "spools", "kind": "sink","license":"MIT", "version": "1.0.0", "contract_major": 1
	}`), json.Unmarshal)
	if err != nil {
		t.Fatalf("read the manifest: %v", err)
	}

	if held.Options != nil {
		t.Fatalf("a manifest with no options list declares %+v", held.Options)
	}
}
