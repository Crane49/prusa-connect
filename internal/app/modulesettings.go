package app

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// The settings one install writes into a module's declaration, read from the
// file `module add -settings` names or from the request the API received, and
// held against what the declaration already stores.

// settingsOf reads the settings one install writes: the operator's file when
// the command named one, or the settings the API caller sent. Neither returns
// no settings, and the module then runs on the defaults its manifest declares.
//
// It runs before anything is written, so settings that are not a JSON object
// stop the install and leave the deployment as it was. The host checks every
// key against the manifest when it starts the module.
func settingsOf(request moduleRequest) (json.RawMessage, error) {
	if request.settings == "" {
		return settingsObject(request.given, "the settings")
	}

	raw, err := os.ReadFile(request.settings)
	if err != nil {
		return nil, fmt.Errorf("read the settings: %w", err)
	}

	return settingsObject(raw, "the settings in "+request.settings)
}

// settingsObject refuses settings that are not a JSON object. Empty settings
// are none at all.
func settingsObject(raw json.RawMessage, where string) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var held map[string]any
	if err := json.Unmarshal(raw, &held); err != nil {
		return nil, settingsError{fmt.Errorf("read %s: %w", where, err)}
	}

	return raw, nil
}

// keptSettings is what an install writes: the settings settingsOf reads from
// the request, held against the settings already stored.
//
// No settings at all keeps what the stored block can carry, so an upgrade does
// not wipe what the operator wrote. Settings that were sent go through the rule
// put-resource uses: the marker a read answers in place of a credential means
// "keep the stored one", and it is refused where nothing is stored.
//
// A stored value outlives the reinstall only where carriedSettings allows it,
// so a credential never reaches a listing, or another module, unmasked.
func keptSettings(
	namespace string, release manifest.Manifest, request moduleRequest, declared resource.Declaration,
) (json.RawMessage, error) {
	given, err := settingsOf(request)
	if err != nil {
		return nil, err
	}

	options := release.Options

	stored, err := carriedSettings(namespace+extension.QualifiedSeparator+release.Name, options, declared)
	if err != nil {
		return nil, err
	}

	if len(given) == 0 {
		return stored, nil
	}

	kept, err := resource.Restore(options, true, given, stored)
	if err != nil {
		return nil, settingsError{fmt.Errorf("%s is what a read answers in place of a credential, and "+
			"this module has none stored to keep. Write the credential itself: %w",
			resource.RedactedValue, err)}
	}

	return kept, nil
}

// carriedSettings is the part of the stored block a reinstall may keep.
//
// A listing masks a value by the options of the declaration it reads, so a
// stored value may only stay where the new options mask it the same way the old
// ones did:
//
//   - nothing crosses a change of type. Another namespace is another module,
//     and its binary must not receive this one's credentials
//   - a key stays only when the new manifest declares it with the type it had.
//     A secret retyped as a string, or an option taken out, would be read back
//     in plain text
//   - when the set of secret and address keys changed, a block or a list that
//     could hold one of those keys inside it is dropped too, because the masking
//     reaches into nested values by key name
func carriedSettings(
	typeName string, options []extension.Option, declared resource.Declaration,
) (json.RawMessage, error) {
	stored := declared.Spec.Config
	if len(stored) == 0 || declared.Spec.Type != typeName || declared.Spec.Module == nil {
		return nil, nil
	}

	was := make(map[string]extension.OptionType, len(declared.Spec.Module.Options))
	for _, option := range declared.Spec.Module.Options {
		was[option.Key] = option.Type
	}

	now := make(map[string]extension.OptionType, len(options))
	for _, option := range options {
		now[option.Key] = option.Type
	}

	masked := maskedKeys(was) == maskedKeys(now)

	// A stored block that does not parse is not carried. The install then
	// writes what it was sent, as a fresh install does.
	block, readable := settingsBlock(stored)
	if !readable {
		return nil, nil
	}

	for key := range block {
		shape, declared := now[key]
		nested := shape == extension.OptionObject || shape == extension.OptionList

		if !declared || was[key] != shape || (!masked && nested) {
			delete(block, key)
		}
	}

	if len(block) == 0 {
		return nil, nil
	}

	carried, err := json.Marshal(block)
	if err != nil {
		return nil, fmt.Errorf("render the kept settings: %w", err)
	}

	return carried, nil
}

// settingsBlock reads a stored block key by key, and reports false for one
// that is not a JSON object.
func settingsBlock(stored json.RawMessage) (map[string]json.RawMessage, bool) {
	var block map[string]json.RawMessage

	return block, json.Unmarshal(stored, &block) == nil
}

// maskedKeys is the secret and address keys of one option list, as one string
// two lists can be compared by.
func maskedKeys(options map[string]extension.OptionType) string {
	keys := make([]string, 0, len(options))

	for key, shape := range options {
		if shape == extension.OptionSecret || shape == extension.OptionURL {
			keys = append(keys, key+"="+string(shape))
		}
	}

	slices.Sort(keys)

	return strings.Join(keys, ",")
}

// settingsError is settings this install cannot write. The API answers it as a
// fault on the request's settings, and the command prints the sentence.
type settingsError struct{ err error }

func (e settingsError) Error() string { return e.err.Error() }

func (e settingsError) Unwrap() error { return e.err }
