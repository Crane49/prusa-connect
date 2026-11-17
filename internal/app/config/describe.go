package config

import (
	"reflect"
	"strings"
	"time"
)

// Kind is what a value of one key looks like in the file. The reference page
// prints it as a word and the first-run wizard draws a field for it.
type Kind string

// The kinds a key can be.
const (
	KindText     Kind = "text"
	KindSwitch   Kind = "switch"
	KindNumber   Kind = "number"
	KindDuration Kind = "duration"
	KindTextList Kind = "text_list"
	KindMapping  Kind = "mapping"
)

// Placeholder stands in a path for a segment the operator names: a connector
// instance, a resource kind or a resource instance.
const Placeholder = "<name>"

// Field is one key the decoder reads, with the value Default() gives it.
type Field struct {
	// Path is the dotted path an operator writes, such as core.api.addr.
	Path string
	// Kind is what a value of the key looks like.
	Kind Kind
	// Default is the value Default() holds for the key. It is invalid for a key
	// under a placeholder and for a nil pointer, which both have no default.
	Default reflect.Value
}

// Describe returns one field for each key under path that the yaml decoder
// reads into target. It follows the yaml tags the decoder follows and skips
// every field the decoder skips, so the list is the set of keys a file may
// write. defaults is the value of target that Default() returns.
func Describe(path string, target reflect.Type, defaults reflect.Value) []Field {
	switch {
	case target.Kind() == reflect.Pointer:
		return Describe(path, target.Elem(), deref(defaults))
	case target == yamlNodeType:
		return []Field{{Path: path, Kind: KindMapping}}
	case target.Kind() == reflect.Map && target.Elem().Kind() == reflect.Struct:
		return Describe(path+"."+Placeholder, target.Elem(), reflect.Value{})
	case target.Kind() == reflect.Struct:
		return describeStruct(path, target, defaults)
	}

	return []Field{{Path: path, Kind: kindOf(target), Default: deref(defaults)}}
}

// describeStruct collects the fields of every struct field the decoder reads.
func describeStruct(path string, target reflect.Type, defaults reflect.Value) []Field {
	var fields []Field

	for index := range target.NumField() {
		field := target.Field(index)
		if !field.IsExported() {
			continue
		}

		name, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
		if name == "-" {
			continue
		}

		if name == "" {
			name = strings.ToLower(field.Name)
		}

		var fieldDefaults reflect.Value
		if defaults.IsValid() && defaults.Kind() == reflect.Struct {
			fieldDefaults = defaults.Field(index)
		}

		fields = append(fields, Describe(path+"."+name, field.Type, fieldDefaults)...)
	}

	return fields
}

// durationType is time.Duration, which is an integer to reflect and a duration
// to an operator.
//
//nolint:gochecknoglobals // one reflect type resolved once and only read.
var durationType = reflect.TypeFor[time.Duration]()

// kindOf is the kind of one Go type.
func kindOf(target reflect.Type) Kind {
	if target == durationType {
		return KindDuration
	}

	//exhaustive:ignore // every other kind is text, which is the fallback.
	switch target.Kind() {
	case reflect.Bool:
		return KindSwitch
	case reflect.Int, reflect.Int64:
		return KindNumber
	case reflect.Slice, reflect.Array:
		return KindTextList
	case reflect.Map:
		return KindMapping
	case reflect.Pointer:
		return kindOf(target.Elem())
	}

	return KindText
}

// deref follows a pointer to the value behind it, and reports an invalid value
// for a nil one.
func deref(value reflect.Value) reflect.Value {
	for value.IsValid() && value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return reflect.Value{}
		}

		value = value.Elem()
	}

	return value
}
