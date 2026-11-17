package schema_test

import (
	"errors"
	"math"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
)

// TestReadScalars tests ReadString, ReadBool, ReadInt and ReadFloat. Each must
// decode a valid value, and return for an invalid value the same error that
// Check returns.
func TestReadScalars(t *testing.T) {
	t.Parallel()

	t.Run("string", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			raw  string
			want string
			err  error
		}{
			{`"hi"`, "hi", nil},
			{`"a\"b"`, `a"b`, nil},
			{escapedAccent, "aéb", nil},
			{`1`, "", schema.ErrWrongType},
			{`null`, "", schema.ErrWrongType},
			{`"hi`, "", schema.ErrMalformed},
		}
		for _, c := range cases {
			got, err := schema.ReadString([]byte(c.raw))
			if !errors.Is(err, c.err) || got != c.want {
				t.Fatalf("ReadString(%q) = %q, %v; want %q, %v", c.raw, got, err, c.want, c.err)
			}
		}
	})

	t.Run("bool", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			raw  string
			want bool
			err  error
		}{
			{`true`, true, nil},
			{`false`, false, nil},
			{`  true `, true, nil},
			{`1`, false, schema.ErrWrongType},
			{`tru`, false, schema.ErrMalformed},
		}
		for _, c := range cases {
			got, err := schema.ReadBool([]byte(c.raw))
			if !errors.Is(err, c.err) || got != c.want {
				t.Fatalf("ReadBool(%q) = %v, %v; want %v, %v", c.raw, got, err, c.want, c.err)
			}
		}
	})

	t.Run("int", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			raw  string
			want int64
			err  error
		}{
			{`0`, 0, nil},
			{`-0`, 0, nil},
			{`42`, 42, nil},
			{`-42`, -42, nil},
			{`9223372036854775807`, math.MaxInt64, nil},
			{`-9223372036854775808`, math.MinInt64, nil},
			{`9223372036854775808`, 0, schema.ErrIntRange},
			{`1.0`, 0, schema.ErrWrongType},
			{`1e3`, 0, schema.ErrWrongType},
			{`"1"`, 0, schema.ErrWrongType},
		}
		for _, c := range cases {
			got, err := schema.ReadInt([]byte(c.raw))
			if !errors.Is(err, c.err) || got != c.want {
				t.Fatalf("ReadInt(%q) = %d, %v; want %d, %v", c.raw, got, err, c.want, c.err)
			}
		}
	})

	t.Run("float", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			raw  string
			want float64
			err  error
		}{
			{`1`, 1, nil},
			{`1.0`, 1, nil},
			{`1e3`, 1000, nil},
			{`-1.5`, -1.5, nil},
			{`"1.5"`, 0, schema.ErrWrongType},
			{`null`, 0, schema.ErrWrongType},
			{`1e-700`, 0, nil},
			{`1e700`, 0, schema.ErrFloatRange},
		}
		for _, c := range cases {
			got, err := schema.ReadFloat([]byte(c.raw))
			if !errors.Is(err, c.err) || got != c.want {
				t.Fatalf("ReadFloat(%q) = %v, %v; want %v, %v", c.raw, got, err, c.want, c.err)
			}
		}
	})
}

// TestRawValuesRoundTrip encodes a Go value with a Raw function, checks that a
// property of that type accepts the result, and decodes it with the matching
// Read function. The decoded value must equal the first value.
func TestRawValuesRoundTrip(t *testing.T) {
	t.Parallel()
	d := sampleDefinition(t)

	label, _ := d.Property("label")
	for _, s := range []string{"", "hi", `a"b`, "a\nb", "héllo"} {
		raw := schema.RawString(s)
		if err := label.Check(raw); err != nil {
			t.Fatalf("Check refused RawString(%q): %v", s, err)
		}
		back, err := schema.ReadString(raw)
		if err != nil || back != s {
			t.Fatalf("ReadString(RawString(%q)) = %q, %v", s, back, err)
		}
	}

	count, _ := d.Property("count")
	for _, v := range []int64{0, 1, -1, math.MaxInt64, math.MinInt64} {
		raw := schema.RawInt(v)
		if err := count.Check(raw); err != nil {
			t.Fatalf("Check refused RawInt(%d): %v", v, err)
		}
		back, err := schema.ReadInt(raw)
		if err != nil || back != v {
			t.Fatalf("ReadInt(RawInt(%d)) = %d, %v", v, back, err)
		}
	}

	ratio, _ := d.Property("ratio")
	for _, v := range []float64{0, 1, -1.5, 1e21, 1e-21, math.SmallestNonzeroFloat64, math.MaxFloat64} {
		raw, err := schema.RawFloat(v)
		if err != nil {
			t.Fatalf("RawFloat(%v): %v", v, err)
		}
		if bad := ratio.Check(raw); bad != nil {
			t.Fatalf("Check refused RawFloat(%v), which is %q: %v", v, raw, bad)
		}
		back, err := schema.ReadFloat(raw)
		if err != nil || back != v {
			t.Fatalf("ReadFloat(RawFloat(%v)) = %v, %v", v, back, err)
		}
	}

	onoff, _ := d.Property("onoff")
	for _, v := range []bool{true, false} {
		raw := schema.RawBool(v)
		if err := onoff.Check(raw); err != nil {
			t.Fatalf("Check refused RawBool(%v): %v", v, err)
		}
		back, err := schema.ReadBool(raw)
		if err != nil || back != v {
			t.Fatalf("ReadBool(RawBool(%v)) = %v, %v", v, back, err)
		}
	}

	maybe, _ := d.Property("maybe")
	if err := maybe.Check(schema.RawNull()); err != nil {
		t.Fatalf("Check of a nullable property refused RawNull: %v", err)
	}
}

// TestRawFloatRefusesWhatJSONCannotCarry tests that RawFloat returns
// ErrNotFinite for a not-a-number value and for both infinities.
func TestRawFloatRefusesWhatJSONCannotCarry(t *testing.T) {
	t.Parallel()
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := schema.RawFloat(v); !errors.Is(err, schema.ErrNotFinite) {
			t.Fatalf("RawFloat(%v) = %v, want ErrNotFinite", v, err)
		}
	}
}

// TestEnumHelpers tests the methods of Enum: Len, Values, Has, Raw and Value.
func TestEnumHelpers(t *testing.T) {
	t.Parallel()
	r := loadFiles(t, sampleFile)
	e, ok := r.Enum("Colour")
	if !ok {
		t.Fatal("Colour is not in the registry")
	}
	if e.Len() != 3 {
		t.Fatalf("Len = %d, want 3", e.Len())
	}
	var values []string
	for v := range e.Values() {
		values = append(values, v)
	}
	if len(values) != 3 || values[0] != "RED" || values[2] != "BLUE" {
		t.Fatalf("Values gave %v", values)
	}
	if !e.Has("GREEN") || e.Has("PINK") {
		t.Fatal("Has is false for the listed value GREEN, or true for PINK, which is not listed")
	}

	raw, err := e.Raw("RED")
	if err != nil {
		t.Fatalf("Raw: %v", err)
	}
	back, err := e.Value(raw)
	if err != nil || back != "RED" {
		t.Fatalf("Value(Raw(RED)) = %q, %v", back, err)
	}
	if _, rawErr := e.Raw("PINK"); !errors.Is(rawErr, schema.ErrUnknownEnumValue) {
		t.Fatalf("Raw(PINK) = %v, want ErrUnknownEnumValue", rawErr)
	}
	if _, pinkErr := e.Value([]byte(`"PINK"`)); !errors.Is(pinkErr, schema.ErrUnknownEnumValue) {
		t.Fatalf("Value(PINK) = %v, want ErrUnknownEnumValue", pinkErr)
	}
	if _, valErr := e.Value([]byte(escapedRED)); valErr != nil {
		t.Fatalf("Value refused the listed value RED written with an escape: %v", valErr)
	}
	if _, numErr := e.Value([]byte(`1`)); !errors.Is(numErr, schema.ErrWrongType) {
		t.Fatalf("Value(1) = %v, want ErrWrongType", numErr)
	}
	if _, brokenErr := e.Value([]byte(`"a`)); !errors.Is(brokenErr, schema.ErrMalformed) {
		t.Fatalf("Value of a string with no closing quote = %v, want ErrMalformed", brokenErr)
	}
}

// TestTypeAndAccessNames tests the String and Parse functions of Type and
// Access: the names a definition file uses, and "unknown" for a value that is
// not a type or an access mode.
func TestTypeAndAccessNames(t *testing.T) {
	t.Parallel()
	cases := []struct {
		typ  schema.Type
		name string
	}{
		{schema.TypeString, "string"},
		{schema.TypeInt, "int"},
		{schema.TypeFloat, "float"},
		{schema.TypeBool, "bool"},
		{schema.TypeEnum, "enum"},
		{0, "unknown"},
		{99, "unknown"},
	}
	for _, c := range cases {
		if got := c.typ.String(); got != c.name {
			t.Fatalf("Type(%d).String() = %q, want %q", c.typ, got, c.name)
		}
	}
	for _, name := range []string{"string", "int", "float", "bool"} {
		if _, ok := schema.ParseType(name); !ok {
			t.Fatalf("ParseType refused the built-in type name %q", name)
		}
	}
	for _, name := range []string{"enum", "Colour", "", "String"} {
		if _, ok := schema.ParseType(name); ok {
			t.Fatalf("ParseType accepted %q, which is not a built-in type name", name)
		}
	}
	if schema.AccessRO.String() != "ro" || schema.AccessRW.String() != "rw" {
		t.Fatal("Access.String is not ro for AccessRO and rw for AccessRW")
	}
	if schema.Access(0).String() != "unknown" {
		t.Fatal("Access(0).String is not unknown")
	}
	if schema.AccessRO.Writable() || !schema.AccessRW.Writable() {
		t.Fatal("Writable is true for AccessRO, or false for AccessRW")
	}
	if _, ok := schema.ParseAccess("RO"); ok {
		t.Fatal("ParseAccess accepted the upper case name RO")
	}
}

// TestEnumOnANonEnumType tests that ValueType.Enum returns ok false for a
// string type, and that String returns "string" for it.
func TestEnumOnANonEnumType(t *testing.T) {
	t.Parallel()
	d := sampleDefinition(t)
	label, _ := d.Property("label")
	if _, ok := label.Type().Enum(); ok {
		t.Fatal("Enum returned ok for a string type")
	}
	if label.Type().String() != "string" {
		t.Fatalf("String of a string type = %q", label.Type().String())
	}
}
