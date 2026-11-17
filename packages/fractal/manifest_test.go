package main

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// TestTheReadmeKeyTableIsTheManifest: the key table of README.md has the same
// keys, types, defaults and sentences as the options list of module.yaml. An
// operator reads the README, and the host refuses a key that module.yaml does
// not declare.
func TestTheReadmeKeyTableIsTheManifest(t *testing.T) {
	t.Parallel()

	declared := manifestOptions(t)
	written := readmeKeys(t)

	if len(declared) != len(written) {
		t.Fatalf("module.yaml declares %d keys and README.md lists %d", len(declared), len(written))
	}

	for index, option := range declared {
		row := written[index]

		if row.key != option.Key {
			t.Fatalf("row %d of the README table is %q and module.yaml declares %q",
				index, row.key, option.Key)
		}

		if row.shape != option.Type {
			t.Errorf("the type of %s is %q in the README and %q in module.yaml", row.key, row.shape, option.Type)
		}

		if want := renderedDefault(option); row.fallback != want {
			t.Errorf("the default of %s is %q in the README and %q in module.yaml",
				row.key, row.fallback, want)
		}

		if row.summary != option.Summary {
			t.Errorf("the sentence for %s is\n  README:      %s\n  module.yaml: %s",
				row.key, row.summary, option.Summary)
		}
	}
}

// renderedDefault returns the text of the Default column for one option:
// "required" for a required key, "none" for a key with no declared default,
// and the default in backticks otherwise.
func renderedDefault(option declaredOption) string {
	switch {
	case option.Required:
		return "required"
	case option.Default == "":
		return "none"
	}

	return "`" + option.Default + "`"
}

// declaredOption is one entry of the options list in module.yaml.
//
// manifestOptions reads the file line by line and uses no YAML library,
// because this Go module has no YAML dependency and the test does not add one.
type declaredOption struct {
	Key      string
	Type     string
	Required bool
	Default  string
	Summary  string
}

// manifestOptions returns the options of module.yaml, in the order of the file.
func manifestOptions(t *testing.T) []declaredOption {
	t.Helper()

	document, err := os.ReadFile("module.yaml")
	if err != nil {
		t.Fatalf("read module.yaml: %v", err)
	}

	_, after, found := strings.Cut(string(document), "\noptions:\n")
	if !found {
		t.Fatal("module.yaml declares no options block")
	}

	var (
		out  []declaredOption
		held *declaredOption
	)

	for line := range strings.SplitSeq(after, "\n") {
		trimmed := strings.TrimSpace(line)

		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
			continue
		case !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "-"):
			// The first line at the left margin ends the options block.
			return finish(t, out, held)
		case strings.HasPrefix(trimmed, "- "):
			out = finished(out, held)
			held = &declaredOption{}
			trimmed = strings.TrimPrefix(trimmed, "- ")
		}

		if held == nil {
			continue
		}

		key, value, split := strings.Cut(trimmed, ": ")
		if !split {
			continue
		}

		switch key {
		case "key":
			held.Key = unquote(value)
		case "type":
			held.Type = unquote(value)
		case "required":
			held.Required = value == "true"
		case "default":
			held.Default = unquote(value)
		case "summary":
			held.Summary = unquote(value)
		}
	}

	return finish(t, out, held)
}

// finish appends the last entry and fails the test when the list is empty.
func finish(t *testing.T, out []declaredOption, held *declaredOption) []declaredOption {
	t.Helper()

	out = finished(out, held)
	if len(out) == 0 {
		t.Fatal("module.yaml declares no settings")
	}

	return out
}

// finished appends held to out. held is nil before the first entry.
func finished(out []declaredOption, held *declaredOption) []declaredOption {
	if held == nil {
		return out
	}

	return append(out, *held)
}

// unquote removes the single or double quotes around a YAML value.
func unquote(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		return value[1 : len(value)-1]
	}

	return value
}

// readmeRow is one row of the key table in README.md.
type readmeRow struct {
	key      string
	shape    string
	fallback string
	summary  string
}

// readmeKeys returns the rows of the table under the "All settings" heading of
// README.md, in the order of the file.
func readmeKeys(t *testing.T) []readmeRow {
	t.Helper()

	page, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}

	_, after, found := strings.Cut(string(page), "### All settings")
	if !found {
		t.Fatal("README.md has no All settings section")
	}

	section, _, _ := strings.Cut(after, "### Settings inside")

	var rows []readmeRow

	for line := range strings.SplitSeq(section, "\n") {
		cells := tableCells(line)
		if len(cells) != 4 || !strings.HasPrefix(cells[0], "`") {
			continue
		}

		rows = append(rows, readmeRow{
			key:      strings.Trim(cells[0], "`"),
			shape:    cells[1],
			fallback: cells[2],
			summary:  cells[3],
		})
	}

	if len(rows) == 0 {
		t.Fatal("the All settings section of README.md has no table rows")
	}

	return rows
}

// tableCells splits one markdown table row into its cells. It returns nil for a
// line that is not a table row.
func tableCells(line string) []string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
		return nil
	}

	parts := strings.Split(strings.Trim(line, "|"), "|")

	out := make([]string, 0, len(parts))
	for _, part := range parts {
		out = append(out, strings.TrimSpace(part))
	}

	return out
}

// TestTheManifestParsesWithTheSdk parses module.yaml the way the host parses
// it, with a YAML decoder and sdk/manifest. The other test in this file reads
// the file line by line, so a value that YAML rejects still reads as a setting
// there. `module add` refuses such a release, and this test is the gate that
// catches it before the release is built.
func TestTheManifestParsesWithTheSdk(t *testing.T) {
	t.Parallel()

	document, err := os.ReadFile("module.yaml")
	if err != nil {
		t.Fatalf("read module.yaml: %v", err)
	}

	declared, err := manifest.Parse("module.yaml", document, yaml.Unmarshal)
	if err != nil {
		t.Fatalf("parse module.yaml: %v", err)
	}

	if declared.Name != name {
		t.Errorf("module.yaml names %q and this process answers %q", declared.Name, name)
	}

	if declared.Version != release.String() {
		t.Errorf("module.yaml is version %q and this process answers %q",
			declared.Version, release.String())
	}

	if len(declared.Options) != len(manifestOptions(t)) {
		t.Errorf("the decoder read %d settings and the line reader read %d",
			len(declared.Options), len(manifestOptions(t)))
	}
}

// TestTheManifestAsksForFiles: module.yaml asks for the port files and the
// data directory, requires the storage and print calls, and names the first
// core that serves them (ADR-20261023-7r3 rule 25).
func TestTheManifestAsksForFiles(t *testing.T) {
	t.Parallel()

	document, err := os.ReadFile("module.yaml")
	if err != nil {
		t.Fatalf("read module.yaml: %v", err)
	}

	declared, err := manifest.Parse("module.yaml", document, yaml.Unmarshal)
	if err != nil {
		t.Fatalf("parse module.yaml: %v", err)
	}

	type asks struct {
		Core        string
		Requires    []manifest.Requirement
		Permissions []string
		Needs       manifest.Needs
	}

	want := asks{
		Permissions: []string{"observe", "operate", "read_storage"},
		Requires: []manifest.Requirement{
			{Capability: "access.check"}, {Capability: "bus.subscribe"}, {Capability: "device.command"},
			{Capability: "device.get"}, {Capability: "device.list"}, {Capability: "device.print"},
			{Capability: "storage.list"}, {Capability: "storage.read"}, {Capability: "storage.stat"},
		},
		Needs: manifest.Needs{
			Listen: []manifest.Listen{{
				Name: "files", Network: manifest.NetworkTCP, Port: 8443,
				Why: "the servers below fetch the files this server shares over HTTPS",
			}},
			Data: true,
		},
	}

	got := asks{declared.Core, declared.Requires, declared.Permissions, declared.Needs}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("module.yaml asks for %+v, want %+v", got, want)
	}
}
