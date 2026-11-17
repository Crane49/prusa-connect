package schema_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
)

// TestCheckByType tests every type with values Check must accept and values it
// must refuse. The refused values include a number for a string property, 1.0
// and 1e3 for an int, an integer too large for 64 bits, a string that the
// enumeration does not list, and an object for a string property.
func TestCheckByType(t *testing.T) {
	t.Parallel()
	d := sampleDefinition(t)
	cases := []struct {
		name     string
		property fractal.PropertyName
		value    string
		want     error
	}{
		{"string accepts a string", "label", `"hi"`, nil},
		{"string accepts an empty string", "label", `""`, nil},
		{"string accepts an escape", "label", `"a\"b"`, nil},
		{"string accepts a unicode escape", "label", escapedAccent, nil},
		{"string accepts leading whitespace", "label", "  \"hi\"\n", nil},
		{"string refuses a number", "label", `1`, schema.ErrWrongType},
		{"string refuses a bool", "label", `true`, schema.ErrWrongType},
		{"string refuses null", "label", `null`, schema.ErrNotNullable},
		{"string refuses an object", "label", `{"a":1}`, schema.ErrWrongType},
		{"string refuses an array", "label", `["a"]`, schema.ErrWrongType},
		{"string refuses an unterminated string", "label", `"hi`, schema.ErrMalformed},
		{"string refuses a bad escape", "label", `"a\qb"`, schema.ErrMalformed},
		{"string refuses a short unicode escape", "label", `"a\u00e"`, schema.ErrMalformed},
		{"string refuses a raw newline", "label", "\"a\nb\"", schema.ErrMalformed},
		{"string refuses two values", "label", `"a" "b"`, schema.ErrMalformed},
		{"string refuses nothing at all", "label", ``, schema.ErrMalformed},

		{"int accepts zero", "count", `0`, nil},
		{"int accepts a negative", "count", `-42`, nil},
		{"int accepts the largest int64", "count", `9223372036854775807`, nil},
		{"int accepts the smallest int64", "count", `-9223372036854775808`, nil},
		{"int refuses a fraction", "count", `1.0`, schema.ErrWrongType},
		{"int refuses an exponent", "count", `1e3`, schema.ErrWrongType},
		{"int refuses a huge integer", "count", strings.Repeat("9", 30), schema.ErrIntRange},
		{"int refuses one over int64", "count", `9223372036854775808`, schema.ErrIntRange},
		{"int refuses a leading zero", "count", `01`, schema.ErrMalformed},
		{"int refuses a leading plus", "count", `+1`, schema.ErrMalformed},
		{"int refuses a string", "count", `"1"`, schema.ErrWrongType},
		{"int refuses null", "count", `null`, schema.ErrNotNullable},

		{"float accepts an integer literal", "ratio", `1`, nil},
		{"float accepts a fraction", "ratio", `1.5`, nil},
		{"float accepts an exponent", "ratio", `1e3`, nil},
		{"float accepts a negative exponent", "ratio", `-1.5E-3`, nil},
		{"float accepts a number wider than int64", "ratio", strings.Repeat("9", 30), nil},
		{"float accepts a number too small to represent", "ratio", `1e-700`, nil},
		{"float refuses a number too large to represent", "ratio", `1e700`, schema.ErrFloatRange},
		{"float refuses a trailing dot", "ratio", `1.`, schema.ErrMalformed},
		{"float refuses a bare exponent", "ratio", `1e`, schema.ErrMalformed},
		{"float refuses a string", "ratio", `"1.5"`, schema.ErrWrongType},
		{"float refuses null", "ratio", `null`, schema.ErrNotNullable},

		{"bool accepts true", "onoff", `true`, nil},
		{"bool accepts false", "onoff", `false`, nil},
		{"bool refuses a number", "onoff", `1`, schema.ErrWrongType},
		{"bool refuses a quoted bool", "onoff", `"true"`, schema.ErrWrongType},
		{"bool refuses a prefix of true", "onoff", `tru`, schema.ErrMalformed},

		{"enum accepts a listed value", "colour", `"RED"`, nil},
		{"enum accepts an escaped listed value", "colour", escapedRED, nil},
		{"enum refuses an unlisted value", "colour", `"PINK"`, schema.ErrUnknownEnumValue},
		{"enum refuses a different case", "colour", `"red"`, schema.ErrUnknownEnumValue},
		{"enum refuses a number", "colour", `1`, schema.ErrWrongType},
		{"enum refuses null", "colour", `null`, schema.ErrNotNullable},

		{"nullable accepts null", "maybe", `null`, nil},
		{"nullable accepts a value", "maybe", `"hi"`, nil},
		{"nullable refuses a wrong type", "maybe", `1`, schema.ErrWrongType},
		{"nullable refuses a prefix of null", "maybe", `nul`, schema.ErrMalformed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			p, ok := d.Property(c.property)
			if !ok {
				t.Fatalf("no property %q", c.property)
			}
			err := p.Check([]byte(c.value))
			if !errors.Is(err, c.want) {
				t.Fatalf("Check(%q) = %v, want %v", c.value, err, c.want)
			}
		})
	}
}

// TestCheckAcceptsOnlyValidJSON tests, on a fixed list of values, the rule that
// FuzzCheckValue tests on random input: every value Check accepts is valid JSON
// for encoding/json.
func TestCheckAcceptsOnlyValidJSON(t *testing.T) {
	t.Parallel()
	d := sampleDefinition(t)
	values := []string{
		`"hi"`, `1`, `1.5`, `true`, `"RED"`, `null`, `01`, `1.`, `"a`, `tru`,
		`{`, `[1,]`, `"\uZZZZ"`, ``, `  `, `1 2`,
	}
	for p := range d.Properties() {
		for _, v := range values {
			if err := p.Check([]byte(v)); err == nil && !json.Valid([]byte(v)) {
				t.Fatalf("%s accepted %q, which is not valid JSON", p.Name(), v)
			}
		}
	}
}

// TestIsNull tests that IsNull is true only for JSON null, with or without
// whitespace around it.
func TestIsNull(t *testing.T) {
	t.Parallel()
	cases := []struct {
		value string
		want  bool
	}{
		{`null`, true},
		{"  null\n", true},
		{`"null"`, false},
		{`nul`, false},
		{`0`, false},
		{``, false},
	}
	for _, c := range cases {
		t.Run(c.value, func(t *testing.T) {
			t.Parallel()
			if got := schema.IsNull([]byte(c.value)); got != c.want {
				t.Fatalf("IsNull(%q) = %v, want %v", c.value, got, c.want)
			}
		})
	}
}
