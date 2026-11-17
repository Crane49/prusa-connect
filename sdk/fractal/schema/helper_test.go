package schema_test

import (
	"strconv"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
)

// sampleFile defines one interface that uses every type of package schema. Most
// tests load it.
const sampleFile = `{
  "interface": "Sample1",
  "exposure": "upward",
  "enums": [
    { "name": "Colour", "values": ["RED", "GREEN", "BLUE"] }
  ],
  "properties": [
    { "name": "label", "type": "string", "access": "rw" },
    { "name": "count", "type": "int", "access": "rw" },
    { "name": "ratio", "type": "float", "access": "rw" },
    { "name": "onoff", "type": "bool", "access": "rw" },
    { "name": "colour", "type": "Colour", "access": "rw" },
    { "name": "maybe", "type": "string", "access": "rw", "nullable": true },
    { "name": "reported", "type": "int", "access": "ro" },
    { "name": "sensor", "type": "float", "access": "ro", "nullable": true }
  ],
  "methods": [
    { "name": "Ping" },
    {
      "name": "Move",
      "params": [
        { "name": "x", "type": "float" },
        { "name": "y", "type": "float" },
        { "name": "feedrate", "type": "float", "optional": true }
      ]
    },
    {
      "name": "Paint",
      "params": [
        { "name": "colour", "type": "Colour" }
      ],
      "returns": "bool"
    },
    { "name": "Count", "returns": "int" }
  ]
}`

// The two raw values below each have a JSON \u escape. The backslash is a
// string of its own, so that no editor or tool replaces the escape with its
// character. The tests need the escape: it makes package schema decode the
// string with encoding/json, which a string with no escape does not reach.
const (
	// escapedRED is the enumeration value RED with its first letter escaped.
	escapedRED = `"` + `\` + `u0052ED"`
	// escapedAccent is a string with a character written as an escape.
	escapedAccent = `"a` + `\` + `u00e9b"`
)

// loadFiles builds a registry from the given definition files. It fails the
// test when AddJSON or Build returns an error.
func loadFiles(t *testing.T, files ...string) *schema.Registry {
	t.Helper()
	b := schema.NewBuilder()
	for i, data := range files {
		if err := b.AddJSON("file"+strconv.Itoa(i)+".json", []byte(data)); err != nil {
			t.Fatalf("AddJSON: %v", err)
		}
	}
	r, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return r
}

// sampleDefinition loads sampleFile and returns its definition.
func sampleDefinition(t *testing.T) *schema.Definition {
	t.Helper()
	r := loadFiles(t, sampleFile)
	d, ok := r.Definition("Sample1")
	if !ok {
		t.Fatal("Sample1 is not in the registry")
	}
	return d
}

// props builds a property map from pairs of name and raw JSON.
func props(pairs ...string) fractal.Props {
	out := make(fractal.Props, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out[fractal.PropertyName(pairs[i])] = []byte(pairs[i+1])
	}
	return out
}
