package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
)

// valueSeeds are the raw values FuzzCheckValue starts from: a valid value of
// every type, and invalid values that differ from a valid one by a character
// or two.
var valueSeeds = []string{
	`"hi"`, `""`, `"a\"b"`, `"aé"`, `"\u00"`, `"a`, "\"a\nb\"",
	`0`, `-0`, `1`, `-42`, `01`, `+1`, `1.`, `1.0`, `1e3`, `1E+3`, `1e`, `.5`,
	`9223372036854775807`, `-9223372036854775808`, `9223372036854775808`,
	`999999999999999999999999999999`,
	`true`, `false`, `tru`, `null`, `nul`, `NULL`,
	`{}`, `[]`, `{"a":1}`, `[1,2]`, `[1,]`, `{`, ``, ` `, "\t\n", `1 2`, `"RED"`,
	`"PINK"`, `  "RED"  `,
}

// FuzzCheckValue tests that every value Property.Check accepts is valid JSON
// for encoding/json. Check has its own JSON scanner, and this test finds a
// value the scanner accepts and encoding/json refuses.
func FuzzCheckValue(f *testing.F) {
	for _, s := range valueSeeds {
		f.Add(s)
	}
	d := fuzzDefinition(f)
	names := []fractal.PropertyName{"label", "count", "ratio", "onoff", "colour", "maybe"}
	f.Fuzz(func(t *testing.T, s string) {
		raw := json.RawMessage(s)
		accepted := false
		for _, name := range names {
			p, ok := d.Property(name)
			if !ok {
				t.Fatalf("no property %q", name)
			}
			if err := p.Check(raw); err == nil {
				accepted = true
			}
		}
		if accepted && !json.Valid(raw) {
			t.Fatalf("a property accepted %q, which is not valid JSON", s)
		}
		if !accepted {
			return
		}
		if schema.IsNull(raw) {
			return
		}
		// When Check accepts a value, one of ReadString, ReadFloat and ReadBool
		// must decode it without an error.
		_, strErr := schema.ReadString(raw)
		_, floatErr := schema.ReadFloat(raw)
		_, boolErr := schema.ReadBool(raw)
		if strErr != nil && floatErr != nil && boolErr != nil {
			t.Fatalf("Check accepted %q, and ReadString, ReadFloat and ReadBool all refuse it", s)
		}
	})
}

// FuzzAddJSON tests that AddJSON and Build do not panic on any input. When both
// accept a file, every name in the Registry must be valid, every enum type must
// have an enumeration, and no method may require more parameters than it has.
func FuzzAddJSON(f *testing.F) {
	f.Add(sampleFile)
	f.Add(`{}`)
	f.Add(`{"interface":"A1"}`)
	f.Add(`{"enums":[{"name":"E","values":["A"]}]}`)
	f.Add(`{"interface":"A1","properties":[{"name":"a","type":"int","access":"ro"}]}`)
	f.Add(`{"interface":"A1","interface":"B1"}`)
	f.Fuzz(func(t *testing.T, s string) {
		b := schema.NewBuilder()
		if err := b.AddJSON("fuzz.json", []byte(s)); err != nil {
			return
		}
		r, buildErr := b.Build()
		if buildErr != nil {
			return
		}
		for d := range r.Definitions() {
			if err := d.Name().Validate(); err != nil {
				t.Fatalf("a loaded interface name is not valid: %v", err)
			}
			for p := range d.Properties() {
				if err := p.Name().Validate(); err != nil {
					t.Fatalf("a loaded property name is not valid: %v", err)
				}
				if p.Type().Kind() == schema.TypeEnum {
					if e, ok := p.Type().Enum(); !ok || e == nil || e.Len() == 0 {
						t.Fatalf("%s has an enum type with no enumeration", p.Name())
					}
				}
			}
			for m := range d.Methods() {
				if m.RequiredParams() > m.NumParams() {
					t.Fatalf("%s requires more parameters than it declares", m.Name())
				}
			}
		}
	})
}

// fuzzDefinition builds the sample definition for a fuzz target.
func fuzzDefinition(f *testing.F) *schema.Definition {
	f.Helper()
	b := schema.NewBuilder()
	if err := b.AddJSON("sample.json", []byte(sampleFile)); err != nil {
		f.Fatal(err)
	}
	r, err := b.Build()
	if err != nil {
		f.Fatal(err)
	}
	d, ok := r.Definition("Sample1")
	if !ok {
		f.Fatal("Sample1 is not in the registry")
	}
	return d
}
