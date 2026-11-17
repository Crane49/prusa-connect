package schema

import (
	"encoding/json"
	"iter"
	"slices"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// Type is the kind of value a property, a parameter or a method result has.
type Type uint8

// The types a definition file can use.
const (
	// TypeString is a JSON string.
	TypeString Type = iota + 1
	// TypeInt is a JSON number with no fraction and no exponent that fits in a
	// signed 64-bit integer.
	TypeInt
	// TypeFloat is any JSON number that is not too large for a float64.
	TypeFloat
	// TypeBool is JSON true or false.
	TypeBool
	// TypeEnum is a JSON string listed in a named enumeration.
	TypeEnum
)

// typeNames has the name a definition file uses for each built-in type. "enum"
// is only the text of Type.String. A file writes the name of the enumeration.
var typeNames = [...]string{
	TypeString: "string",
	TypeInt:    "int",
	TypeFloat:  "float",
	TypeBool:   "bool",
	TypeEnum:   "enum",
}

// String returns the name a definition file uses for the type: "string", "int",
// "float" or "bool". It returns "enum" for TypeEnum and "unknown" for any other
// value.
func (t Type) String() string {
	if int(t) <= 0 || int(t) >= len(typeNames) {
		return "unknown"
	}
	return typeNames[t]
}

// ParseType returns the built-in type with the name s. ok is false for any
// other string, the name of an enumeration included.
func ParseType(s string) (t Type, ok bool) {
	for i := 1; i < len(typeNames); i++ {
		if Type(i) != TypeEnum && typeNames[i] == s {
			return Type(i), true
		}
	}
	return 0, false
}

// Access is the access mode of a property. It says whether a DES may change the
// property.
type Access uint8

// The access modes.
const (
	// AccessRO means read-only. Only the node that implements the interface
	// sets the value, and a DES must not name the property.
	AccessRO Access = iota + 1
	// AccessRW means read-write. A node above may ask for a new value with a
	// DES.
	AccessRW
)

// String returns the name a definition file uses for the mode: "ro" or "rw".
// It returns "unknown" for any other value.
func (a Access) String() string {
	switch a {
	case AccessRO:
		return "ro"
	case AccessRW:
		return "rw"
	}
	return "unknown"
}

// Writable reports whether a DES may change the property. It is true for
// AccessRW.
func (a Access) Writable() bool { return a == AccessRW }

// ParseAccess returns the access mode with the name s, which is "ro" or "rw".
// ok is false for any other string.
func ParseAccess(s string) (a Access, ok bool) {
	switch s {
	case "ro":
		return AccessRO, true
	case "rw":
		return AccessRW, true
	}
	return 0, false
}

// Enum is an enumeration: a name and a list of string values. A definition file
// declares it once, and any interface uses it by that name.
type Enum struct {
	name   fractal.Identifier
	values []string
}

// Name returns the name of the enumeration.
func (e *Enum) Name() fractal.Identifier { return e.name }

// Len returns the number of values.
func (e *Enum) Len() int { return len(e.values) }

// Values returns an iterator over the values, in the order the definition file
// lists them.
func (e *Enum) Values() iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, v := range e.values {
			if !yield(v) {
				return
			}
		}
	}
}

// Has reports whether v is one of the values of the enumeration.
func (e *Enum) Has(v string) bool { return slices.Contains(e.values, v) }

// hasBytes reports whether plain equals one of the values of the enumeration.
// plain is the bytes between the quotes of a JSON string with no escape.
func (e *Enum) hasBytes(plain []byte) bool {
	for _, have := range e.values {
		if len(have) == len(plain) && have == string(plain) {
			return true
		}
	}
	return false
}

// Value decodes raw as a JSON string and returns it when it is one of the
// values of the enumeration. It returns ErrWrongType when raw is not a string,
// and ErrUnknownEnumValue when the enumeration does not list the string.
func (e *Enum) Value(raw json.RawMessage) (string, error) {
	form, b := classify(raw)
	if form != formString {
		return "", typeErr(form)
	}
	if plain, ok := plainString(b); ok {
		if !e.hasBytes(plain) {
			return "", ErrUnknownEnumValue
		}
		return string(plain), nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return "", ErrMalformed
	}
	if !e.Has(s) {
		return "", ErrUnknownEnumValue
	}
	return s, nil
}

// Raw encodes v as a JSON string. It returns ErrUnknownEnumValue when the
// enumeration does not list v.
func (e *Enum) Raw(v string) (json.RawMessage, error) {
	if !e.Has(v) {
		return nil, ErrUnknownEnumValue
	}
	return RawString(v), nil
}

// has reports whether the JSON string b, with its quotes, is one of the values
// of the enumeration. It decodes b only when b has an escape.
func (e *Enum) has(b []byte) bool {
	if plain, ok := plainString(b); ok {
		return e.hasBytes(plain)
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return false
	}
	return e.Has(s)
}

// ValueType is the type of one property, parameter or method result. For
// TypeEnum it also has the enumeration that lists the valid values.
type ValueType struct {
	kind Type
	enum *Enum
}

// Kind returns the Type.
func (v ValueType) Kind() Type { return v.kind }

// Enum returns the enumeration that lists the valid values. ok is false when
// Kind is not TypeEnum.
func (v ValueType) Enum() (e *Enum, ok bool) {
	if v.kind != TypeEnum {
		return nil, false
	}
	return v.enum, true
}

// String returns the type name a definition file uses. For an enum type that is
// the name of the enumeration.
func (v ValueType) String() string {
	if v.kind == TypeEnum && v.enum != nil {
		return string(v.enum.name)
	}
	return v.kind.String()
}

// Check returns nil when raw is a valid value of this type. nullable says
// whether JSON null is valid too. Otherwise Check returns the reason:
// ErrNotNullable, ErrWrongType, ErrMalformed, ErrIntRange, ErrFloatRange or
// ErrUnknownEnumValue.
func (v ValueType) Check(raw json.RawMessage, nullable bool) error {
	form, b := classify(raw)
	if form == formNull {
		if nullable {
			return nil
		}
		return ErrNotNullable
	}
	switch v.kind {
	case TypeString:
		return wantForm(form == formString, form)
	case TypeBool:
		return wantForm(form == formBool, form)
	case TypeInt:
		return checkInteger(form, b)
	case TypeFloat:
		return checkNumber(form, b)
	case TypeEnum:
		return v.checkEnum(form, b)
	}
	return typeErr(form)
}

// wantForm returns nil when ok is true, and the error for a value of the wrong
// form otherwise.
func wantForm(ok bool, form valueForm) error {
	if ok {
		return nil
	}
	return typeErr(form)
}

// checkInteger checks an "int" value: a number with no fraction and no
// exponent, inside the signed 64-bit range.
func checkInteger(form valueForm, b []byte) error {
	if form != formInteger {
		return typeErr(form)
	}
	if _, ok := parseInt64(b); !ok {
		return ErrIntRange
	}
	return nil
}

// checkNumber checks a "float" value: any JSON number that is not too large
// for a float64.
func checkNumber(form valueForm, b []byte) error {
	if form != formInteger && form != formNumber {
		return typeErr(form)
	}
	if _, ok := parseFloat64(b); !ok {
		return ErrFloatRange
	}
	return nil
}

// checkEnum checks an enum value: a JSON string that the enumeration lists.
func (v ValueType) checkEnum(form valueForm, b []byte) error {
	if form != formString {
		return typeErr(form)
	}
	if !v.enum.has(b) {
		return ErrUnknownEnumValue
	}
	return nil
}

// typeErr returns ErrMalformed for bytes that are not valid JSON, and
// ErrWrongType for valid JSON of another type.
func typeErr(form valueForm) error {
	if form == formInvalid {
		return ErrMalformed
	}
	return ErrWrongType
}
