package resourcehost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/extension/registry"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// The refusals for the settings of one instance, for a module and for a type
// compiled into this binary.
var (
	// ErrSettingsUnknown reports a key the type does not declare: a module's
	// manifest, or a compiled-in type's descriptor.
	ErrSettingsUnknown = errors.New("resourcehost: the config block carries a key this type does not read")

	// ErrSettingsMissing reports a required key the operator did not write and
	// the declaration gives no default for.
	ErrSettingsMissing = errors.New("resourcehost: a required setting was not written")

	// ErrSettingsShape reports a value written in a shape the declared type
	// cannot hold.
	ErrSettingsShape = errors.New("resourcehost: a setting is not the shape its type says")

	// ErrSettingsUnreadable reports a module the operator wrote settings for
	// and which answers no plugin.configure.
	ErrSettingsUnreadable = errors.New("resourcehost: this module takes no settings")
)

// moduleSettings is the block one module starts with: the default of every
// option its manifest declared, with the operator's config block written over
// it.
//
// The merge is per key at every depth. A nested block merges key by key, so an
// operator who writes one key of it keeps the defaults of the rest. A list is
// replaced whole, because a mix of the author's list and the operator's list
// is what neither of them wrote.
func moduleSettings(declared *serviceSpec, written json.RawMessage) (json.RawMessage, error) {
	options := declaredOptions(declared)

	operator, err := settingsObject(written)
	if err != nil {
		return nil, err
	}

	if len(options) == 0 {
		if len(operator) == 0 {
			return nil, nil
		}

		return nil, fmt.Errorf("%w: %s. This module's manifest declares no settings",
			ErrSettingsUnknown, quoted(slices.Sorted(maps.Keys(operator))))
	}

	if err = holdToOptions(options, operator); err != nil {
		return nil, err
	}

	merged, err := defaultsOf(options)
	if err != nil {
		return nil, err
	}

	mergeInto(merged, operator)

	if err = requiredWritten(options, merged); err != nil {
		return nil, err
	}

	encoded, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("render the settings of %s: %w", declared.typeName, err)
	}

	return encoded, nil
}

// builtinSettings checks one compiled-in instance's config block against the
// options its type declared, before the type is constructed.
//
// A module's block is checked by the same rules. An undeclared key is
// probably a typo, and the setting the operator meant would not be applied. An
// instance cannot work without a required key. The check is made here so the
// answer is the same for every type, whatever decoder the type uses.
//
// No defaults are merged in. A compiled-in type applies its own defaults.
func builtinSettings(
	types *registry.Registry, hostKeys []extension.Option,
	kind ref.Kind, typeName string, written json.RawMessage,
) error {
	declared, known := registeredOptions(types, kind, typeName)
	if !known {
		return nil
	}

	options := append(slices.Clone(declared), hostKeys...)

	operator, err := settingsObject(written)
	if err != nil {
		return err
	}

	if len(operator) == 0 {
		return requiredWritten(options, operator)
	}

	if len(options) == 0 {
		return fmt.Errorf("%w: %s. This type reads no settings",
			ErrSettingsUnknown, quoted(slices.Sorted(maps.Keys(operator))))
	}

	if err = holdToOptions(options, operator); err != nil {
		return err
	}

	if err = writtenAsDeclared(options, operator); err != nil {
		return err
	}

	return requiredWritten(options, operator)
}

// registeredOptions is the settings one registered type declares, and false for
// a name this build has no type for. It returns no error for an unknown name,
// because knownType reports that before this runs.
func registeredOptions(
	types *registry.Registry, kind ref.Kind, typeName string,
) ([]extension.Option, bool) {
	found, err := types.Lookup(kind, typeName)
	if err != nil {
		return nil, false
	}

	return found.Descriptor().Doc().Options, true
}

// writtenAsDeclared refuses a value whose shape the declared type cannot hold.
//
// It refuses only a clear mismatch: a block where a single value belongs, and
// a single value where a block belongs. Some decoders read a number, a
// duration or a bool from text, so a value written as text is left to the
// type that declared it.
func writtenAsDeclared(options []extension.Option, operator map[string]any) error {
	for _, option := range options {
		value, written := operator[option.Key]
		if !written || value == nil {
			continue
		}

		want, refuse := shapeOf(option.Type, value)
		if !refuse {
			continue
		}

		return fmt.Errorf("%w: %s is declared %s and is written as %s, and %s takes %s",
			ErrSettingsShape, strconv.Quote(option.Key), option.Type, writtenShape(value), option.Type, want)
	}

	return nil
}

// shapeOf reports what a declared type takes and whether the value written is
// refused.
func shapeOf(declared extension.OptionType, value any) (want string, refuse bool) {
	_, isBlock := value.(map[string]any)
	_, isList := value.([]any)

	switch declared {
	case extension.OptionObject:
		return "a block of keys", !isBlock
	case extension.OptionList:
		return "a list", isBlock
	case extension.OptionString, extension.OptionInt, extension.OptionNumber,
		extension.OptionBool, extension.OptionDuration, extension.OptionURL:
		return "one value", isBlock || isList
	case extension.OptionSecret:
		// A credential is one value, a block of headers or a list of keys. The
		// type says who may read it and not what shape it has, so no shape is
		// refused.
	}

	return "", false
}

// writtenShape names the shape the operator wrote, for the refusal.
func writtenShape(value any) string {
	switch value.(type) {
	case map[string]any:
		return "a block of keys"
	case []any:
		return "a list"
	}

	return "one value"
}

// declaredOptions is the settings list the install recorded from the module's
// manifest, as extension.Option values, and nil for an instance that is not a
// module.
func declaredOptions(declared *serviceSpec) []extension.Option {
	if declared == nil || declared.module == nil {
		return nil
	}

	out := make([]extension.Option, 0, len(declared.module.Options))
	for _, option := range declared.module.Options {
		out = append(out, extension.Option{
			Key:      option.Key,
			Type:     option.Type,
			Required: option.Required,
			Default:  option.Default,
			Summary:  option.Summary,
		})
	}

	return out
}

// settingsObject reads the operator's block. The host can merge only into an
// object, so any other value is refused with ErrSettingsShape.
func settingsObject(written json.RawMessage) (map[string]any, error) {
	if len(strings.TrimSpace(string(written))) == 0 {
		return map[string]any{}, nil
	}

	var held map[string]any
	if err := json.Unmarshal(written, &held); err != nil {
		return nil, fmt.Errorf("%w: the config block is not a mapping of settings: %w", ErrSettingsShape, err)
	}

	return held, nil
}

// holdToOptions refuses a key the type does not declare. It names every such
// key, so an operator can fix the block in one edit.
func holdToOptions(options []extension.Option, operator map[string]any) error {
	var unknown []string

	for key := range operator {
		if !slices.ContainsFunc(options, func(o extension.Option) bool { return o.Key == key }) {
			unknown = append(unknown, key)
		}
	}

	if len(unknown) == 0 {
		return nil
	}

	sort.Strings(unknown)

	return fmt.Errorf("%w: %s. This type reads %s",
		ErrSettingsUnknown, quoted(unknown), quoted(optionKeys(options)))
}

// requiredWritten refuses a start where a required key has no value, from the
// operator or from a default.
func requiredWritten(options []extension.Option, merged map[string]any) error {
	var missing []string

	for _, option := range options {
		if !option.Required {
			continue
		}

		if _, written := merged[option.Key]; !written {
			missing = append(missing, option.Key)
		}
	}

	if len(missing) == 0 {
		return nil
	}

	return fmt.Errorf("%w: %s. Write each of them under this instance's config block",
		ErrSettingsMissing, quoted(missing))
}

// defaultsOf is the module's default configuration: one entry per declared
// option that has a default, parsed from the text in the manifest.
func defaultsOf(options []extension.Option) (map[string]any, error) {
	out := make(map[string]any, len(options))

	for _, option := range options {
		if option.Default == "" {
			continue
		}

		value, err := defaultValue(option)
		if err != nil {
			return nil, err
		}

		out[option.Key] = value
	}

	return out, nil
}

// defaultValue parses one declared default into a value of its declared type.
// The manifest writes every default as text, so each type is parsed in its
// own way.
func defaultValue(option extension.Option) (any, error) {
	text := option.Default

	switch option.Type {
	case extension.OptionInt:
		held, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: the default of %s is %q, which is not a whole number",
				ErrSettingsShape, option.Key, text)
		}

		return held, nil
	case extension.OptionNumber:
		held, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: the default of %s is %q, which is not a number",
				ErrSettingsShape, option.Key, text)
		}

		return held, nil
	case extension.OptionBool:
		held, err := strconv.ParseBool(text)
		if err != nil {
			return nil, fmt.Errorf("%w: the default of %s is %q, which is not true or false",
				ErrSettingsShape, option.Key, text)
		}

		return held, nil
	case extension.OptionList, extension.OptionObject:
		var held any
		if err := json.Unmarshal([]byte(text), &held); err != nil {
			return nil, fmt.Errorf("%w: the default of %s is %q, which is not a %s written as JSON",
				ErrSettingsShape, option.Key, text, option.Type)
		}

		return held, nil
	case extension.OptionString, extension.OptionDuration, extension.OptionURL, extension.OptionSecret:
		return text, nil
	}

	return text, nil
}

// mergeInto writes the operator's block over the defaults, key by key.
//
// A value that is a mapping on both sides is merged recursively, so an
// operator who writes one key of a nested block keeps the module's defaults for
// the rest of it. Every other value, a list included, is replaced whole.
func mergeInto(into, over map[string]any) {
	for key, value := range over {
		under, isBlock := into[key].(map[string]any)

		nested, alsoBlock := value.(map[string]any)
		if isBlock && alsoBlock {
			mergeInto(under, nested)

			continue
		}

		into[key] = value
	}
}

// optionKeys is every key the module declared, in the order the manifest wrote
// them.
func optionKeys(options []extension.Option) []string {
	out := make([]string, 0, len(options))
	for _, option := range options {
		out = append(out, option.Key)
	}

	return out
}

// quoted renders a list of keys for a refusal: each quoted, comma separated.
func quoted(keys []string) string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, strconv.Quote(key))
	}

	return strings.Join(out, ", ")
}

// configure sends the child its settings, once, before any other call.
//
// A child that does not name plugin.configure in its info takes no settings.
// With an empty block such a module runs on the defaults in its own code. A
// block the operator wrote is refused, because the process would never read
// it.
//
// A module that was granted ports or a directory and answers no
// plugin.configure is started without them, because it cannot read a grant.
//
// The answer is what the module reported when it took its settings: the
// schema version of its own database after it migrated it. When the host
// cannot read the answer, it returns an empty report and no error, because the
// module has its settings in both cases.
func configure(
	ctx context.Context, instance ref.Ref, caller settingsCaller, merged json.RawMessage, grants *wirerpc.Grants,
) (wirerpc.Configured, error) {
	if !caller.Info().Answers(wirerpc.MethodConfigure) {
		if len(merged) == 0 {
			return wirerpc.Configured{}, nil
		}

		return wirerpc.Configured{}, pluginUnconfigurable.New(
			"the module was given settings and answers no plugin.configure").
			With(fault.StringParam("resource", instance.String())).
			Wrap(ErrSettingsUnreadable)
	}

	if merged == nil {
		merged = json.RawMessage("{}")
	}

	answer, err := caller.Conn().Call(ctx, wirerpc.MethodConfigure, wirerpc.Settings{Config: merged, Grants: grants})
	if err != nil {
		return wirerpc.Configured{}, pluginSettings.New("the module refused the settings it was given").
			With(fault.StringParam("resource", instance.String())).
			Wrap(err)
	}

	configured, err := wirerpc.ReadConfigured(answer)
	if err != nil {
		return wirerpc.Configured{}, nil //nolint:nilerr // an unreadable report refuses nothing, see above.
	}

	return configured, nil
}

// settingsCaller is the part of a launched child the settings call needs.
type settingsCaller interface {
	Info() wirerpc.Info
	Conn() *wirerpc.Conn
}

// settingsFailure gives a refused settings block the pluginSettings code. A
// retry fixes none of these refusals, so the instance stays in failed.
func settingsFailure(instance ref.Ref, err error) error {
	switch {
	case errors.Is(err, ErrSettingsUnknown),
		errors.Is(err, ErrSettingsMissing),
		errors.Is(err, ErrSettingsShape):
		return pluginSettings.New("the instance's config block is not what this type reads").
			With(fault.StringParam("resource", instance.String())).
			Wrap(err)
	}

	return err
}
