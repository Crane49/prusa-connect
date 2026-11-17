package main

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// TestEverySlotExampleParses reads each slot's YAML example with the parser
// a module's manifest goes through. It fails when the reference page shows a
// key the parser refuses, such as devices on a slot handed no device.
func TestEverySlotExampleParses(t *testing.T) {
	t.Parallel()

	for _, slot := range manifest.Slots() {
		t.Run(string(slot.Name), func(t *testing.T) {
			t.Parallel()

			example := "    " + strings.ReplaceAll(yamlExample(slot), "\n", "\n    ")
			document := "name: example\nkind: asset\nlicense: MIT\nui:\n  entry: ui/index.js\n" +
				"  library: \">=0.1.0 <1.0.0\"\n  contributes:\n" + example + "\n"

			if _, err := manifest.Parse("module.yaml", []byte(document), yaml.Unmarshal); err != nil {
				t.Fatalf("the example is refused: %v\n%s", err, document)
			}
		})
	}
}
