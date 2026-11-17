package manifest

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// Card is one card a module offers for a dashboard. The page draws the card
// picker from these fields before the bundle loads. The bundle registers the
// component that draws the card.
type Card struct {
	// ID is the card's name within its list, one DNS label. The card's type on
	// a dashboard is the module's namespace, a slash and this id.
	ID string
	// Title is what the card picker and the card's default heading read.
	Title string
	// Description is one sentence the card picker shows under the title.
	Description string
	// Icon is the name of an icon the library publishes, and empty for the
	// default.
	Icon Icon
	// Size is the size a new card starts at, in cells of a 12-column grid.
	Size CardSize
	// Least is the smallest the card may be made. It is never larger than
	// Size.
	Least CardSize
	// Settings is the schema of the card's settings. The page draws the form
	// from it and checks every value against it.
	Settings []CardSetting
}

// CardSize is a size in cells of a 12-column grid.
type CardSize struct {
	// W is how many columns the card covers.
	W int
	// H is how many rows the card covers.
	H int
}

// SettingKind is the kind of one field of a card's settings. The set is
// closed, so the page can draw a control for every field.
type SettingKind string

// The kinds of a card setting.
const (
	// SettingPrinter is one printer the person may see, by its id.
	SettingPrinter SettingKind = "printer"
	// SettingPrinters is a list of printers, by their ids.
	SettingPrinters SettingKind = "printers"
	// SettingTag is one tag the fleet uses.
	SettingTag SettingKind = "tag"
	// SettingCamera is one camera, by its name.
	SettingCamera SettingKind = "camera"
	// SettingChoice is one value out of the options.
	SettingChoice SettingKind = "choice"
	// SettingChoices is any of the options, as a list.
	SettingChoices SettingKind = "choices"
	// SettingNumber is a number between Min and Max, in steps of Step.
	SettingNumber SettingKind = "number"
	// SettingText is one line of text, at most MaxBytes long.
	SettingText SettingKind = "text"
	// SettingNote is text of several lines, at most MaxBytes long.
	SettingNote SettingKind = "note"
	// SettingSwitch is on or off.
	SettingSwitch SettingKind = "switch"
)

// SettingKinds is every kind a card setting may be, in the order the
// reference lists them.
func SettingKinds() []SettingKind {
	return []SettingKind{
		SettingPrinter, SettingPrinters, SettingTag, SettingCamera, SettingChoice,
		SettingChoices, SettingNumber, SettingText, SettingNote, SettingSwitch,
	}
}

// CardSetting is one field of a card's settings.
type CardSetting struct {
	// Key is the name the value is kept under in the card's settings.
	Key string
	// Kind is what the field holds, and which control the page draws.
	Kind SettingKind
	// Label is what the form shows beside the control.
	Label string
	// Default is the value a new card starts with. It replaces a stored value
	// that fails the check. It is always set: a string, a list of strings, a
	// number or a boolean.
	Default any
	// Options are the values of a choice or choices field, in the order the
	// form shows them.
	Options []SettingOption
	// Min and Max bound a number field, and are nil where the manifest set no
	// bound. Step is the step between two values, and nil for any number.
	Min, Max, Step *float64
	// MaxBytes bounds a text or note field, counted as UTF-8 bytes.
	MaxBytes int
}

// SettingOption is one value of a choice or choices field.
type SettingOption struct {
	// Value is what the card's settings keep.
	Value string
	// Label is what the form shows for it.
	Label string
}

// The bounds of a card entry.
const (
	// MaxCardColumns is the width of the grid the sizes are written for.
	MaxCardColumns = 12
	// MaxCardRows is the tallest a card may be.
	MaxCardRows = 32
	// MaxCardDescriptionBytes bounds the sentence under a card's title.
	MaxCardDescriptionBytes = 200
	// MaxCardSettings bounds the fields of one card's settings.
	MaxCardSettings = 16
	// MaxSettingOptions bounds the options of one choice field.
	MaxSettingOptions = 32
	// DefaultTextBytes and MaxTextBytes bound a text field.
	DefaultTextBytes = 200
	MaxTextBytes     = 2000
	// DefaultNoteBytes and MaxNoteBytes bound a note field.
	DefaultNoteBytes = 2000
	MaxNoteBytes     = 3000
)

// The keys a card entry and its parts may have.
//
//nolint:gochecknoglobals // closed tables read at parse, never written.
var (
	cardKeys          = []string{"id", "title", "description", "icon", "size", "least", "settings"}
	sizeKeys          = []string{"w", "h"}
	settingKeys       = []string{"key", "kind", "label", "default", "options", "min", "max", "step", "max_bytes"}
	settingOptionKeys = []string{"value", "label"}
)

// MaxCardSettingKeyBytes bounds a key a card's settings keep a value under:
// a short lower-case name a page can read as a property.
const MaxCardSettingKeyBytes = 32

// readCards reads the cards list of ui.contributes.
func readCards(declared map[string]any, key string) ([]Card, error) {
	entries, err := nodeList(declared, "ui.contributes", key)
	if err != nil || entries == nil {
		return nil, err
	}

	out := make([]Card, 0, len(entries))
	taken := make([]string, 0, len(entries))

	for index, entry := range entries {
		where := fmt.Sprintf("ui.contributes.%s[%d]", key, index)

		card, readErr := readCard(entry, where)
		if readErr != nil {
			return nil, readErr
		}

		if err = refuseRepeat(where+".id", append(taken, card.ID)); err != nil {
			return nil, err
		}

		taken = append(taken, card.ID)
		out = append(out, card)
	}

	return out, nil
}

// readCard reads one card entry.
func readCard(entry map[string]any, where string) (Card, error) {
	if err := refuseUnknown(entry, where, cardKeys); err != nil {
		return Card{}, err
	}

	var (
		card Card
		err  error
	)

	if card.ID, err = label(entry, where, "id"); err != nil {
		return Card{}, err
	}

	if card.Title, err = title(entry, where); err != nil {
		return Card{}, err
	}

	if card.Description, err = requiredText(entry, where, "description"); err != nil {
		return Card{}, err
	}

	if len(card.Description) > MaxCardDescriptionBytes {
		return Card{}, fmt.Errorf("%w: %s.description is %d bytes and the limit is %d",
			ErrInvalid, where, len(card.Description), MaxCardDescriptionBytes)
	}

	if card.Icon, err = readIcon(entry, where); err != nil {
		return Card{}, err
	}

	if card.Size, err = readSize(entry, where, "size"); err != nil {
		return Card{}, err
	}

	if card.Least, err = readSize(entry, where, "least"); err != nil {
		return Card{}, err
	}

	if card.Least.W > card.Size.W || card.Least.H > card.Size.H {
		return Card{}, fmt.Errorf("%w: %s.least is %dx%d and is larger than %s.size, %dx%d",
			ErrInvalid, where, card.Least.W, card.Least.H, where, card.Size.W, card.Size.H)
	}

	if card.Settings, err = readSettings(entry, where); err != nil {
		return Card{}, err
	}

	return card, nil
}

// readSize reads a required size, held to the grid a dashboard has.
func readSize(entry map[string]any, where, key string) (CardSize, error) {
	block, present, err := node(entry, where, key)
	if err != nil {
		return CardSize{}, err
	}

	if !present {
		return CardSize{}, fmt.Errorf("%w: %s is missing, and a card names its size in cells",
			ErrInvalid, at(where, key))
	}

	place := at(where, key)
	if err = refuseUnknown(block, place, sizeKeys); err != nil {
		return CardSize{}, err
	}

	var size CardSize

	if size.W, err = whole(block, place, "w"); err != nil {
		return CardSize{}, err
	}

	if size.H, err = whole(block, place, "h"); err != nil {
		return CardSize{}, err
	}

	if size.W < 1 || size.W > MaxCardColumns {
		return CardSize{}, fmt.Errorf("%w: %s.w is %d, and a card covers 1 to %d columns",
			ErrInvalid, place, size.W, MaxCardColumns)
	}

	if size.H < 1 || size.H > MaxCardRows {
		return CardSize{}, fmt.Errorf("%w: %s.h is %d, and a card covers 1 to %d rows",
			ErrInvalid, place, size.H, MaxCardRows)
	}

	return size, nil
}

// readSettings reads the optional settings schema of one card.
func readSettings(entry map[string]any, where string) ([]CardSetting, error) {
	fields, err := nodeList(entry, where, "settings")
	if err != nil || fields == nil {
		return nil, err
	}

	if len(fields) > MaxCardSettings {
		return nil, fmt.Errorf("%w: %s.settings has %d fields and the limit is %d",
			ErrInvalid, where, len(fields), MaxCardSettings)
	}

	out := make([]CardSetting, 0, len(fields))
	taken := make([]string, 0, len(fields))

	for index, field := range fields {
		place := fmt.Sprintf("%s.settings[%d]", where, index)

		setting, readErr := readSetting(field, place)
		if readErr != nil {
			return nil, readErr
		}

		if err = refuseRepeat(place+".key", append(taken, setting.Key)); err != nil {
			return nil, err
		}

		taken = append(taken, setting.Key)
		out = append(out, setting)
	}

	return out, nil
}

// readSetting reads one field of a card's settings.
func readSetting(field map[string]any, where string) (CardSetting, error) {
	if err := refuseUnknown(field, where, settingKeys); err != nil {
		return CardSetting{}, err
	}

	var (
		setting CardSetting
		err     error
	)

	if setting.Key, err = requiredText(field, where, "key"); err != nil {
		return CardSetting{}, err
	}

	if !extension.IsConfigKey(setting.Key) || len(setting.Key) > MaxCardSettingKeyBytes {
		return CardSetting{}, fmt.Errorf("%w: %s.key is %q, and a key is a lower-case letter followed by at "+
			"most 31 lower-case letters, digits and underscores", ErrInvalid, where, setting.Key)
	}

	kind, err := requiredText(field, where, "kind")
	if err != nil {
		return CardSetting{}, err
	}

	setting.Kind = SettingKind(kind)
	if !slices.Contains(SettingKinds(), setting.Kind) {
		return CardSetting{}, fmt.Errorf("%w: %s.kind is %q, and the kinds are %s",
			ErrInvalid, where, kind, kindNames())
	}

	if setting.Label, err = requiredText(field, where, "label"); err != nil {
		return CardSetting{}, err
	}

	if err = readBounds(field, where, &setting); err != nil {
		return CardSetting{}, err
	}

	if setting.Default, err = readDefault(field, where, setting); err != nil {
		return CardSetting{}, err
	}

	return setting, nil
}

// readBounds reads the bounds one kind takes. It refuses a bound that belongs
// to another kind.
func readBounds(field map[string]any, where string, setting *CardSetting) error {
	choice := setting.Kind == SettingChoice || setting.Kind == SettingChoices
	number := setting.Kind == SettingNumber
	words := setting.Kind == SettingText || setting.Kind == SettingNote

	for key, allowed := range map[string]bool{
		"options": choice, "min": number, "max": number, "step": number, "max_bytes": words,
	} {
		if _, present := field[key]; present && !allowed {
			return fmt.Errorf("%w: %s.%s does not apply to a %s field", ErrInvalid, where, key, setting.Kind)
		}
	}

	switch {
	case choice:
		return readSettingOptions(field, where, setting)
	case number:
		return readNumberBounds(field, where, setting)
	case words:
		return readMaxBytes(field, where, setting)
	default:
		return nil
	}
}

// readSettingOptions reads the options of a choice or choices field.
func readSettingOptions(field map[string]any, where string, setting *CardSetting) error {
	options, err := nodeList(field, where, "options")
	if err != nil {
		return err
	}

	if len(options) == 0 || len(options) > MaxSettingOptions {
		return fmt.Errorf("%w: %s.options has %d options, and a %s field has 1 to %d",
			ErrInvalid, where, len(options), setting.Kind, MaxSettingOptions)
	}

	values := make([]string, 0, len(options))

	for index, option := range options {
		place := fmt.Sprintf("%s.options[%d]", where, index)
		if err = refuseUnknown(option, place, settingOptionKeys); err != nil {
			return err
		}

		var held SettingOption

		if held.Value, err = requiredText(option, place, "value"); err != nil {
			return err
		}

		if held.Label, err = requiredText(option, place, "label"); err != nil {
			return err
		}

		if err = refuseRepeat(place+".value", append(values, held.Value)); err != nil {
			return err
		}

		values = append(values, held.Value)
		setting.Options = append(setting.Options, held)
	}

	return nil
}

// readNumberBounds reads the optional min, max and step of a number field.
func readNumberBounds(field map[string]any, where string, setting *CardSetting) error {
	var err error

	if setting.Min, err = optionalNumber(field, where, "min"); err != nil {
		return err
	}

	if setting.Max, err = optionalNumber(field, where, "max"); err != nil {
		return err
	}

	if setting.Step, err = optionalNumber(field, where, "step"); err != nil {
		return err
	}

	if setting.Min != nil && setting.Max != nil && *setting.Min > *setting.Max {
		return fmt.Errorf("%w: %s.min is %v and is larger than %s.max, %v",
			ErrInvalid, where, *setting.Min, where, *setting.Max)
	}

	if setting.Step != nil && *setting.Step <= 0 {
		return fmt.Errorf("%w: %s.step is %v, and a step is larger than 0", ErrInvalid, where, *setting.Step)
	}

	return nil
}

// readMaxBytes reads the bound of a text or note field, with the kind's own
// default when the manifest sets none.
func readMaxBytes(field map[string]any, where string, setting *CardSetting) error {
	fallback, limit := DefaultTextBytes, MaxTextBytes
	if setting.Kind == SettingNote {
		fallback, limit = DefaultNoteBytes, MaxNoteBytes
	}

	bound, err := whole(field, where, "max_bytes")
	if err != nil {
		return err
	}

	if _, present := field["max_bytes"]; !present {
		bound = fallback
	}

	if bound < 1 || bound > limit {
		return fmt.Errorf("%w: %s.max_bytes is %d, and a %s field holds 1 to %d bytes",
			ErrInvalid, where, bound, setting.Kind, limit)
	}

	setting.MaxBytes = bound

	return nil
}

// optionalNumber reads a number key that may be absent, which answers nil.
func optionalNumber(tree map[string]any, where, key string) (*float64, error) {
	held, present := tree[key]
	if !present || held == nil {
		return nil, nil //nolint:nilnil // an absent bound is no bound and no error.
	}

	value, ok := numberOf(held)
	if !ok {
		return nil, fmt.Errorf("%w: %s is %s, expected a number", ErrInvalid, at(where, key), shapeOf(held))
	}

	return &value, nil
}

// numberOf reads a number the decoder wrote as an int or a float64, and
// refuses a value that is not a finite number.
func numberOf(held any) (float64, bool) {
	var value float64

	switch typed := held.(type) {
	case int:
		value = float64(typed)
	case int64:
		value = float64(typed)
	case float64:
		value = typed
	default:
		return 0, false
	}

	return value, !math.IsNaN(value) && !math.IsInf(value, 0)
}

// readDefault reads the default of one field and checks it against the kind
// and the bounds. A field with no default takes the kind's own.
func readDefault(field map[string]any, where string, setting CardSetting) (any, error) {
	held, present := field["default"]
	if !present || held == nil {
		return kindDefault(setting), nil
	}

	place := at(where, "default")

	switch setting.Kind {
	case SettingPrinter, SettingTag, SettingCamera, SettingText, SettingNote:
		return textDefault(held, place, setting)
	case SettingPrinters:
		return textListDefault(held, place, nil)
	case SettingChoice:
		value, err := textDefault(held, place, setting)
		if err != nil {
			return nil, err
		}

		if !slices.ContainsFunc(setting.Options, func(o SettingOption) bool { return o.Value == value }) {
			return nil, fmt.Errorf("%w: %s is %q, which is not one of the options", ErrInvalid, place, value)
		}

		return value, nil
	case SettingChoices:
		return textListDefault(held, place, setting.Options)
	case SettingNumber:
		return numberDefault(held, place, setting)
	case SettingSwitch:
		value, isBool := held.(bool)
		if !isBool {
			return nil, fmt.Errorf("%w: %s is %s, expected true or false", ErrInvalid, place, shapeOf(held))
		}

		return value, nil
	default:
		return nil, fmt.Errorf("%w: %s belongs to a kind with no default", ErrInvalid, place)
	}
}

// kindDefault is the default a field of one kind takes when the manifest
// gives none.
func kindDefault(setting CardSetting) any {
	switch setting.Kind {
	case SettingPrinters, SettingChoices:
		return []string{}
	case SettingNumber:
		if setting.Min != nil {
			return *setting.Min
		}

		return float64(0)
	case SettingSwitch:
		return false
	case SettingChoice:
		return setting.Options[0].Value
	case SettingPrinter, SettingTag, SettingCamera, SettingText, SettingNote:
		return ""
	default:
		return ""
	}
}

// textDefault reads a text default, held to the field's byte bound.
func textDefault(held any, place string, setting CardSetting) (string, error) {
	value, isText := held.(string)
	if !isText {
		return "", fmt.Errorf("%w: %s is %s, expected text", ErrInvalid, place, shapeOf(held))
	}

	if setting.MaxBytes > 0 && len(value) > setting.MaxBytes {
		return "", fmt.Errorf(
			"%w: %s is %d bytes and the field holds %d",
			ErrInvalid,
			place,
			len(value),
			setting.MaxBytes,
		)
	}

	if setting.Kind == SettingText && strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("%w: %s holds a line break, and a text field is one line", ErrInvalid, place)
	}

	return value, nil
}

// textListDefault reads a list default. With options, every member is one of
// them and none is there twice.
func textListDefault(held any, place string, options []SettingOption) ([]string, error) {
	items, isList := held.([]any)
	if !isList {
		return nil, fmt.Errorf("%w: %s is %s, expected a list", ErrInvalid, place, shapeOf(held))
	}

	out := make([]string, 0, len(items))

	for index, item := range items {
		value, isText := item.(string)
		if !isText {
			return nil, fmt.Errorf("%w: %s[%d] is %s, expected text", ErrInvalid, place, index, shapeOf(item))
		}

		if options != nil && !slices.ContainsFunc(options, func(o SettingOption) bool { return o.Value == value }) {
			return nil, fmt.Errorf("%w: %s[%d] is %q, which is not one of the options", ErrInvalid, place, index, value)
		}

		if slices.Contains(out, value) {
			return nil, fmt.Errorf("%w: %s holds %q twice", ErrInvalid, place, value)
		}

		out = append(out, value)
	}

	return out, nil
}

// numberDefault reads a number default, held to the field's bounds.
func numberDefault(held any, place string, setting CardSetting) (float64, error) {
	value, ok := numberOf(held)
	if !ok {
		return 0, fmt.Errorf("%w: %s is %s, expected a number", ErrInvalid, place, shapeOf(held))
	}

	if setting.Min != nil && value < *setting.Min || setting.Max != nil && value > *setting.Max {
		return 0, fmt.Errorf("%w: %s is %v, which is outside the field's bounds", ErrInvalid, place, value)
	}

	return value, nil
}

// kindNames is every setting kind, spelt for a refusal.
func kindNames() string {
	kinds := SettingKinds()
	names := make([]string, len(kinds))

	for i, kind := range kinds {
		names[i] = string(kind)
	}

	return joinNames(names)
}
