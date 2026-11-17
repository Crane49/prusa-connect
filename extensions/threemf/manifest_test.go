package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/threemf/internal/analysis"
)

// The manifest is read here the way an operator reads it, line by line, so
// an author who changes what the code answers and not what the manifest
// promises finds out from `go test`. The core parses it with the real reader
// in internal/arch.

// TestTheManifestNamesTheTypesTheCodeReads: the host asks this module about
// the types module.yaml names, and the code must read each of them.
func TestTheManifestNamesTheTypesTheCodeReads(t *testing.T) {
	t.Parallel()

	document := manifest(t)

	_, block, found := strings.Cut(document, "\nfile_analysis:\n")
	if !found {
		t.Fatal("module.yaml has no file_analysis block")
	}

	line, _, _ := strings.Cut(block, "\n")

	declared, found := strings.CutPrefix(strings.TrimSpace(line), "types: [")
	if !found {
		t.Fatalf("file_analysis does not start with a types list: %q", line)
	}

	var types []string
	for item := range strings.SplitSeq(strings.TrimSuffix(declared, "]"), ",") {
		types = append(types, strings.TrimSpace(item))
	}

	if !slices.Equal(types, analysis.Types) {
		t.Errorf("module.yaml names %v and the code reads %v", types, analysis.Types)
	}
}

// TestTheManifestDeclaresWhatTheBundleRegisters: the host refuses a bundle
// that registers a view or a component its manifest does not declare.
func TestTheManifestDeclaresWhatTheBundleRegisters(t *testing.T) {
	t.Parallel()

	document := manifest(t)

	for _, want := range []string{
		"    file_view:\n      - id: project\n",
		"        file_types: [.3mf]\n",
		"  exports:\n    - name: project-view\n",
	} {
		if !strings.Contains(document, want) {
			t.Errorf("module.yaml does not declare %q", want)
		}
	}

	if got := scalar(document, "name"); got != name {
		t.Errorf("module.yaml names %q and the process reports %q", got, name)
	}
}
