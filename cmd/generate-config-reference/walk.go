package main

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// row is one key of the reference table.
type row struct {
	// Path is the dotted path an operator writes, such as core.api.addr.
	Path string
	// Type is what a value of that key looks like in the file.
	Type string
	// Default is what the core uses when the file leaves the key out.
	Default string
	// Required is when a deployment has to set the key, or "no".
	Required string
	// Doc is the sentence from the table beside the struct.
	Doc string
}

// block is one top-level block of the file and the keys under it.
type block struct {
	// Name is the heading the page prints.
	Name string
	// Intro is the line under that heading.
	Intro string
	// Rows are the keys, in the order the struct declares them.
	Rows []row
}

// The words the page prints in the type column. They describe the value an
// operator writes in the file. They are not Go type names.
const (
	typeText     = "text"
	typeSwitch   = "true or false"
	typeNumber   = "number"
	typeDuration = "duration"
	typeTextList = "list of text"
	typeMapping  = "mapping"
)

// nothing is the text in the default column for a key that has no default.
const nothing = "-"

// words is the type-column text for each kind.
//
//nolint:gochecknoglobals // a lookup table read and never written.
var words = map[config.Kind]string{
	config.KindText:     typeText,
	config.KindSwitch:   typeSwitch,
	config.KindNumber:   typeNumber,
	config.KindDuration: typeDuration,
	config.KindTextList: typeTextList,
	config.KindMapping:  typeMapping,
}

// walk returns one row for each key under path. config.Describe follows the
// same yaml tags as the decoder. The sentences come from keys.
func walk(path string, target reflect.Type, defaults reflect.Value, keys map[string]config.Key) []row {
	fields := config.Describe(path, target, defaults)
	rows := make([]row, 0, len(fields))

	for _, field := range fields {
		fallback := nothing
		if field.Kind != config.KindMapping || field.Default.IsValid() {
			fallback = defaultOf(field.Kind, field.Default)
		}

		rows = append(rows, leaf(field.Path, words[field.Kind], fallback, keys))
	}

	return rows
}

// leaf returns the row of one key. A key with no entry in keys gets an empty
// sentence, which checkDocumented reports.
func leaf(path, kind, fallback string, keys map[string]config.Key) row {
	documented := keys[path]

	value := fallback
	if documented.Default != "" {
		value = documented.Default
	}

	required := documented.Required
	if required == "" {
		required = "no"
	}

	return row{Path: path, Type: kind, Default: value, Required: required, Doc: documented.Doc}
}

// defaultOf is the default-column text. A key with no default prints a dash.
func defaultOf(kind config.Kind, defaults reflect.Value) string {
	if !defaults.IsValid() {
		return nothing
	}

	if kind == config.KindDuration {
		return "`" + config.WrittenDuration(time.Duration(defaults.Int())) + "`"
	}

	//exhaustive:ignore // the kinds a configuration key can be.
	switch defaults.Kind() {
	case reflect.Bool:
		return "`" + strconv.FormatBool(defaults.Bool()) + "`"
	case reflect.Int, reflect.Int64:
		if defaults.Int() == 0 {
			return nothing
		}

		return "`" + strconv.FormatInt(defaults.Int(), 10) + "`"
	case reflect.String:
		if defaults.String() == "" {
			return nothing
		}

		return "`" + defaults.String() + "`"
	case reflect.Slice, reflect.Array, reflect.Map:
		if defaults.Len() == 0 {
			return "empty"
		}
	}

	return nothing
}

func pathsOf(blocks []block) map[string]struct{} {
	out := map[string]struct{}{}

	for _, one := range blocks {
		for _, each := range one.Rows {
			out[each.Path] = struct{}{}
		}
	}

	return out
}

func render(one block) string {
	var body strings.Builder

	fmt.Fprintf(&body, "### %s\n\n%s\n\n", one.Name, one.Intro)
	body.WriteString("| key | type | default | required | what it does |\n|---|---|---|---|---|\n")

	for _, each := range one.Rows {
		fmt.Fprintf(&body, "| <a id=\"%s\"></a>`%s` | %s | %s | %s | %s |\n",
			config.KeyAnchor(each.Path), each.Path, each.Type, each.Default, each.Required, each.Doc)
	}

	return body.String()
}
