package main

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// The manifest is read the way an operator reads it, line by line and with no
// YAML library, so a default changed in the code and not in the manifest or
// the README fails here.
func TestTheDefaultsInTheManifestAreTheDefaultsInTheCode(t *testing.T) {
	t.Parallel()

	inCode := map[string]string{
		"address":           "",
		"snapshot_interval": strconv.Itoa(DefaultSnapshotInterval),
	}

	declared := manifestOptions(t)
	if len(declared) != len(inCode) {
		t.Fatalf("module.yaml declares %d settings and the code holds %d", len(declared), len(inCode))
	}

	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}

	for key, value := range inCode {
		if got, ok := declared[key]; !ok || got != value {
			t.Errorf("module.yaml gives %s the default %q, the code %q", key, got, value)
		}

		row := "| `" + key + "` | `" + value + "` |"
		if value == "" {
			row = "| `" + key + "` | `\"\"` |"
		}

		if !strings.Contains(string(readme), row) {
			t.Errorf("README.md has no row %s", row)
		}
	}
}

// manifestOptions is each option's key and default from module.yaml.
func manifestOptions(t *testing.T) map[string]string {
	t.Helper()

	held, err := os.ReadFile("module.yaml")
	if err != nil {
		t.Fatal(err)
	}

	out := map[string]string{}
	key := ""

	for line := range strings.SplitSeq(string(held), "\n") {
		trimmed := strings.TrimSpace(line)

		if value, ok := strings.CutPrefix(trimmed, "- key: "); ok {
			key = value
		}

		if value, ok := strings.CutPrefix(trimmed, "default: "); ok && key != "" {
			out[key] = strings.Trim(value, `"`)
			key = ""
		}
	}

	return out
}
