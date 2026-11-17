package manifest

import (
	"fmt"

	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// Option is one setting a module reads from its config block. It is the same
// type as extension.Option.
type Option = extension.Option

// optionKeys is every key one options entry may have.
//
//nolint:gochecknoglobals // a closed table read at parse, never written.
var optionKeys = []string{"key", "type", "required", "default", "summary"}

// readOptions reads the options list: the settings this module accepts and the
// default of each.
func readOptions(tree map[string]any) ([]Option, error) {
	entries, err := nodeList(tree, "", "options")
	if err != nil {
		return nil, err
	}

	if entries == nil {
		return nil, nil
	}

	out := make([]Option, 0, len(entries))
	seen := make(map[string]bool, len(entries))

	for index, entry := range entries {
		where := fmt.Sprintf("options[%d]", index)

		if err = refuseUnknown(entry, where, optionKeys); err != nil {
			return nil, err
		}

		held, readErr := readOption(where, entry)
		if readErr != nil {
			return nil, readErr
		}

		if seen[held.Key] {
			return nil, fmt.Errorf("%w: options names the key %q twice", ErrInvalid, held.Key)
		}

		seen[held.Key] = true

		out = append(out, held)
	}

	return out, nil
}

// readOption reads one entry of the options list. It needs a key, a type the
// host knows and a sentence.
func readOption(where string, entry map[string]any) (Option, error) {
	key, err := requiredText(entry, where, "key")
	if err != nil {
		return Option{}, err
	}

	if !extension.IsConfigKey(key) {
		return Option{}, fmt.Errorf("%w: %s.key is %q, which is not lower_snake_case", ErrInvalid, where, key)
	}

	shape, err := requiredText(entry, where, "type")
	if err != nil {
		return Option{}, err
	}

	if !extension.OptionType(shape).Valid() {
		return Option{}, fmt.Errorf("%w: %s.type is %q, which is not a setting type", ErrInvalid, where, shape)
	}

	summary, err := requiredText(entry, where, "summary")
	if err != nil {
		return Option{}, err
	}

	required, err := truth(entry, where, "required")
	if err != nil {
		return Option{}, err
	}

	fallback, err := text(entry, where, "default")
	if err != nil {
		return Option{}, err
	}

	// A required key has no default.
	if required && fallback != "" {
		return Option{}, fmt.Errorf("%w: %s is required and carries a default", ErrInvalid, where)
	}

	return Option{
		Key:      key,
		Type:     extension.OptionType(shape),
		Required: required,
		Default:  fallback,
		Summary:  summary,
	}, nil
}
