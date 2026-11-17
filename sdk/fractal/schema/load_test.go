package schema_test

import (
	"errors"
	"testing"
	"testing/fstest"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
)

// TestLoadAcceptsAWellFormedFile loads sampleFile and tests that the Registry
// has its interface, its enumeration, its properties and its methods.
func TestLoadAcceptsAWellFormedFile(t *testing.T) {
	t.Parallel()
	r := loadFiles(t, sampleFile)
	if r.NumDefinitions() != 1 {
		t.Fatalf("NumDefinitions = %d, want 1", r.NumDefinitions())
	}
	if r.NumEnums() != 1 {
		t.Fatalf("NumEnums = %d, want 1", r.NumEnums())
	}
	d, ok := r.Definition("Sample1")
	if !ok {
		t.Fatal("Sample1 is not in the registry")
	}
	if d.NumProperties() != 8 {
		t.Fatalf("NumProperties = %d, want 8", d.NumProperties())
	}
	if d.NumMethods() != 4 {
		t.Fatalf("NumMethods = %d, want 4", d.NumMethods())
	}
	colour, ok := d.Property("colour")
	if !ok {
		t.Fatal("no property colour")
	}
	if colour.Type().Kind() != schema.TypeEnum {
		t.Fatalf("colour kind = %v, want enum", colour.Type().Kind())
	}
	enum, ok := colour.Type().Enum()
	if !ok || enum.Name() != "Colour" {
		t.Fatalf("colour enum = %v, %v", enum, ok)
	}
	if colour.Type().String() != "Colour" {
		t.Fatalf("colour type name = %q, want Colour", colour.Type().String())
	}
}

// TestLoadKeepsTheOrderOfTheFile tests that properties and parameters keep the
// order of the arrays in the file. An RPC sends parameters by position, so
// their order must not change.
func TestLoadKeepsTheOrderOfTheFile(t *testing.T) {
	t.Parallel()
	d := sampleDefinition(t)
	var names []fractal.PropertyName
	for p := range d.Properties() {
		names = append(names, p.Name())
	}
	want := []fractal.PropertyName{
		"label", "count", "ratio", "onoff", "colour", "maybe", "reported", "sensor",
	}
	if len(names) != len(want) {
		t.Fatalf("got %d properties, want %d", len(names), len(want))
	}
	for i, n := range want {
		if names[i] != n {
			t.Fatalf("property %d is %q, want %q", i, names[i], n)
		}
	}
	move, ok := d.Method("Move")
	if !ok {
		t.Fatal("no method Move")
	}
	if move.NumParams() != 3 || move.RequiredParams() != 2 {
		t.Fatalf("Move has %d params, %d required", move.NumParams(), move.RequiredParams())
	}
	if move.Param(2).Name() != "feedrate" || !move.Param(2).Optional() {
		t.Fatalf("Move param 2 is %v", move.Param(2))
	}
}

// TestLoadRefusesABadFile tests every rule AddJSON checks, each with a file
// that breaks that rule. A case with no wanted error only requires that
// AddJSON returns a *LoadError.
func TestLoadRefusesABadFile(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		file string
		want error
	}{
		{
			"an unknown field",
			`{"interface":"A1","descriptions":[]}`,
			nil,
		},
		{
			"a duplicate key",
			`{"interface":"A1","interface":"B1"}`,
			schema.ErrDuplicateKey,
		},
		{
			"a duplicate key inside a property",
			`{"interface":"A1","properties":[{"name":"a","name":"b","type":"int","access":"ro"}]}`,
			schema.ErrDuplicateKey,
		},
		{
			"nothing declared",
			`{}`,
			schema.ErrEmptyFile,
		},
		{
			"properties without an interface",
			`{"enums":[{"name":"E","values":["A"]}],"properties":[{"name":"a","type":"int","access":"ro"}]}`,
			schema.ErrMissingInterface,
		},
		{
			"an exposure without an interface",
			`{"enums":[{"name":"E","values":["A"]}],"exposure":"upward"}`,
			schema.ErrMissingInterface,
		},
		{
			"an interface name outside the grammar",
			`{"interface":"1A"}`,
			fractal.ErrBadFirstCharacter,
		},
		{
			"a property name outside the grammar",
			`{"interface":"A1","properties":[{"name":"a-b","type":"int","access":"ro"}]}`,
			fractal.ErrBadCharacter,
		},
		{
			"a method name outside the grammar",
			`{"interface":"A1","methods":[{"name":"Do It"}]}`,
			fractal.ErrBadCharacter,
		},
		{
			"a parameter name outside the grammar",
			`{"interface":"A1","methods":[{"name":"Do","params":[{"name":"","type":"int"}]}]}`,
			fractal.ErrEmpty,
		},
		{
			"two properties with one name",
			`{"interface":"A1","properties":[` +
				`{"name":"a","type":"int","access":"ro"},{"name":"a","type":"bool","access":"ro"}]}`,
			schema.ErrDuplicateName,
		},
		{
			"two methods with one name",
			`{"interface":"A1","methods":[{"name":"Do"},{"name":"Do"}]}`,
			schema.ErrDuplicateName,
		},
		{
			"two parameters with one name",
			`{"interface":"A1","methods":[{"name":"Do","params":[` +
				`{"name":"a","type":"int"},{"name":"a","type":"int"}]}]}`,
			schema.ErrDuplicateName,
		},
		{
			"an enumeration with no value",
			`{"enums":[{"name":"E","values":[]}]}`,
			schema.ErrEmptyEnum,
		},
		{
			"an enumeration with an empty value",
			`{"enums":[{"name":"E","values":[""]}]}`,
			schema.ErrEmptyEnumValue,
		},
		{
			"an enumeration with a value twice",
			`{"enums":[{"name":"E","values":["A","A"]}]}`,
			schema.ErrDuplicateName,
		},
		{
			"an enumeration name outside the grammar",
			`{"enums":[{"name":"9E","values":["A"]}]}`,
			fractal.ErrBadFirstCharacter,
		},
		{
			"an access mode that is neither ro nor rw",
			`{"interface":"A1","properties":[{"name":"a","type":"int","access":"readonly"}]}`,
			schema.ErrUnknownAccess,
		},
		{
			"no access mode at all",
			`{"interface":"A1","properties":[{"name":"a","type":"int"}]}`,
			schema.ErrUnknownAccess,
		},
		{
			"a required parameter after an optional one",
			`{"interface":"A1","methods":[{"name":"Do","params":[` +
				`{"name":"a","type":"int","optional":true},{"name":"b","type":"int"}]}]}`,
			schema.ErrRequiredAfterOptional,
		},
		{
			"two json values in the file",
			`{"interface":"A1"} {"interface":"B1"}`,
			nil,
		},
		{
			"a type that is a JSON number and not a string",
			`{"interface":"A1","properties":[{"name":"a","type":1,"access":"ro"}]}`,
			nil,
		},
		{
			"a second json value after the first",
			`{"interface":"A1","properties":[]} {"interface":"B1"}`,
			nil,
		},
		{
			"a stray brace after the json value",
			`{"interface":"A1","properties":[]} }`,
			nil,
		},
		{
			"a stray bracket after the json value",
			`{"interface":"A1","properties":[]} ]`,
			nil,
		},
		{
			"text after the json value",
			`{"interface":"A1","properties":[]} and a note somebody typed`,
			nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			b := schema.NewBuilder()
			err := b.AddJSON("bad.json", []byte(c.file))
			if err == nil {
				t.Fatal("AddJSON accepted the invalid file")
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("AddJSON = %v, want %v", err, c.want)
			}
			var le *schema.LoadError
			if !errors.As(err, &le) {
				t.Fatalf("AddJSON returned %T, want a *schema.LoadError", err)
			}
			if le.Source != "bad.json" {
				t.Fatalf("LoadError.Source = %q, want bad.json", le.Source)
			}
		})
	}
}

// TestLoadReadsTheExposure tests that a definition returns the exposure its
// file states, and that a file with no exposure, or with a word the format
// does not have, is refused (fractal.md, Interface).
func TestLoadReadsTheExposure(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		file string
		want schema.Exposure
		err  error
	}{
		{"upward", `{"interface":"A1","exposure":"upward"}`, schema.ExposureUpward, nil},
		{"downward", `{"interface":"A1","exposure":"downward"}`, schema.ExposureDownward, nil},
		{"both", `{"interface":"A1","exposure":"both"}`, schema.ExposureBoth, nil},
		{"none", `{"interface":"A1"}`, 0, schema.ErrUnknownExposure},
		{"a word the format does not have", `{"interface":"A1","exposure":"Upward"}`, 0, schema.ErrUnknownExposure},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			b := schema.NewBuilder()
			err := b.AddJSON("a.json", []byte(c.file))
			var got schema.Exposure
			if err == nil {
				r, buildErr := b.Build()
				if buildErr != nil {
					t.Fatalf("Build: %v", buildErr)
				}
				d, _ := r.Definition("A1")
				got = d.Exposure()
			}
			if got != c.want || !errors.Is(err, c.err) {
				t.Fatalf("the exposure is %s with the error %v, want %s with %v", got, err, c.want, c.err)
			}
		})
	}
}

// TestBuildRefusesAnUnresolvableFile tests the rules that need every file:
// each type name must be a built-in type or a declared enumeration, and no two
// files may declare the same interface or enumeration. Build returns these
// errors, because AddJSON reads one file at a time.
func TestBuildRefusesAnUnresolvableFile(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		files []string
		want  error
	}{
		{
			"an unknown property type",
			[]string{
				`{"interface":"A1","exposure":"upward","properties":[{"name":"a","type":"decimal","access":"ro"}]}`,
			},
			schema.ErrUnknownType,
		},
		{
			"a property type that names an enumeration no file declares",
			[]string{
				`{"interface":"A1","exposure":"upward","properties":[{"name":"a","type":"Colour","access":"ro"}]}`,
			},
			schema.ErrUnknownType,
		},
		{
			"an unknown parameter type",
			[]string{
				`{"interface":"A1","exposure":"upward","methods":[{"name":"Do","params":[{"name":"a","type":"Colour"}]}]}`,
			},
			schema.ErrUnknownType,
		},
		{
			"an unknown return type",
			[]string{
				`{"interface":"A1","exposure":"upward","methods":[{"name":"Do","returns":"Colour"}]}`,
			},
			schema.ErrUnknownType,
		},
		{
			"the word enum used as a type",
			[]string{
				`{"interface":"A1","exposure":"upward","properties":[{"name":"a","type":"enum","access":"ro"}]}`,
			},
			schema.ErrUnknownType,
		},
		{
			"one interface in two files",
			[]string{`{"interface":"A1","exposure":"upward"}`, `{"interface":"A1","exposure":"upward"}`},
			schema.ErrDuplicateName,
		},
		{
			"one enumeration in two files",
			[]string{`{"enums":[{"name":"E","values":["A"]}]}`, `{"enums":[{"name":"E","values":["B"]}]}`},
			schema.ErrDuplicateName,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			b := schema.NewBuilder()
			for i, f := range c.files {
				if err := b.AddJSON(string(rune('a'+i))+".json", []byte(f)); err != nil {
					t.Fatalf("AddJSON: %v", err)
				}
			}
			_, err := b.Build()
			if !errors.Is(err, c.want) {
				t.Fatalf("Build = %v, want %v", err, c.want)
			}
		})
	}
}

// TestBuildSharesAnEnumerationBetweenFiles tests that two interfaces in two
// files can use an enumeration that a third file declares, and that both get
// the same *Enum. The Prusa definition files depend on this: enums.json
// declares every enumeration.
func TestBuildSharesAnEnumerationBetweenFiles(t *testing.T) {
	t.Parallel()
	r := loadFiles(t,
		`{"enums":[{"name":"Colour","values":["RED"]}]}`,
		`{"interface":"A1","exposure":"upward","properties":[{"name":"a","type":"Colour","access":"ro"}]}`,
		`{"interface":"B1","exposure":"upward","properties":[{"name":"b","type":"Colour","access":"ro"}]}`,
	)
	a, _ := r.Definition("A1")
	b, _ := r.Definition("B1")
	pa, _ := a.Property("a")
	pb, _ := b.Property("b")
	ea, _ := pa.Type().Enum()
	eb, _ := pb.Type().Enum()
	if ea != eb {
		t.Fatal("the properties of A1 and B1 have different *Enum values for the type Colour")
	}
}

// TestLoadFromAFileSystem tests Load on an fs.FS. With no pattern Load reads
// every "*.json" file in the root, so the broken file makes it fail. With
// patterns it reads only the files that match.
func TestLoadFromAFileSystem(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"enums.json": {Data: []byte(`{"enums":[{"name":"Colour","values":["RED"]}]}`)},
		"A1.json": {
			Data: []byte(
				`{"interface":"A1","exposure":"upward","properties":[{"name":"a","type":"Colour","access":"ro"}]}`,
			),
		},
		"notes.txt":   {Data: []byte("ignored")},
		"B1/B1.json":  {Data: []byte(`{"interface":"B1"}`)},
		"broken.json": {Data: []byte(`{`)},
	}
	if _, err := schema.Load(fsys); err == nil {
		t.Fatal("Load with no pattern accepted broken.json, which is not valid JSON")
	}
	r, err := schema.Load(fsys, "A1.json", "enums.json")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if r.NumDefinitions() != 1 || r.NumEnums() != 1 {
		t.Fatalf("Load read %d interfaces and %d enumerations, want 1 and 1", r.NumDefinitions(), r.NumEnums())
	}
}

// TestRegistryLookupsMissAKnownName tests that Definition, Enum, Property and
// Method return ok false for a name that was not loaded.
func TestRegistryLookupsMissAKnownName(t *testing.T) {
	t.Parallel()
	r := loadFiles(t, sampleFile)
	if _, ok := r.Definition("Other1"); ok {
		t.Fatal("Definition returned ok for Other1, which was not loaded")
	}
	if _, ok := r.Enum("Shade"); ok {
		t.Fatal("Enum returned ok for Shade, which was not loaded")
	}
	d, _ := r.Definition("Sample1")
	if _, ok := d.Property("nothing"); ok {
		t.Fatal("Property returned ok for a property Sample1 does not have")
	}
	if _, ok := d.Method("Nothing"); ok {
		t.Fatal("Method returned ok for a method Sample1 does not have")
	}
}
