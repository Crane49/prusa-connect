package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/codegen"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
)

// committedReference is the artefact this command writes, from the package
// directory.
const committedReference = "../../docs/src/content/docs/capabilities.md"

// TestTheCommittedReferenceMatchesTheData is the gate that keeps the published
// page from drifting. It is a test rather than only a make target so that it
// runs under `go test ./...` whether or not anybody remembers the target.
func TestTheCommittedReferenceMatchesTheData(t *testing.T) {
	if err := run(committedReference, true); err != nil {
		t.Errorf("%v", err)
	}
}

// TestEveryDeclaredKeyIsPublished holds the one thing the renderer can lose
// silently. Verbs and features are picked out by prefix, so a key under a
// prefix nothing here groups vanishes from the page while every other gate
// stays green.
func TestEveryDeclaredKeyIsPublished(t *testing.T) {
	registry, err := profile.Builtin()
	if err != nil {
		t.Fatalf("load the built-in profiles: %v", err)
	}

	page := string(renderReference(registry))
	coverage := registry.Coverage()

	for _, key := range coverage.Keys() {
		if !coverage.Has(key) {
			continue
		}

		if !strings.Contains(page, "`"+key.String()+"`") {
			t.Errorf("%s is declared by a shipped profile and is not on the published page", key)
		}
	}
}

// TestEveryValueKindIsExplained fails when the closed set of value kinds grows
// and nothing says what the new one carries, which would publish an empty cell.
func TestEveryValueKindIsExplained(t *testing.T) {
	for _, kind := range kinds() {
		if strings.TrimSpace(kindMeaning[kind]) == "" {
			t.Errorf("value kind %s has no meaning to publish", kind)
		}
	}
}

// TestEveryArgumentMeaningIsPublished fails on a meaning row for a key no
// shipped profile declares, which the page would never print.
func TestEveryArgumentMeaningIsPublished(t *testing.T) {
	registry, err := profile.Builtin()
	if err != nil {
		t.Fatalf("load the built-in profiles: %v", err)
	}

	coverage := registry.Coverage()

	for key := range argumentMeaning {
		if _, isArgument := key.Argument(); !isArgument || !coverage.Has(key) {
			t.Errorf("%s has a meaning and is not an argument a shipped profile declares", key)
		}
	}
}

// TestAStaleArtefactIsRefused watches the gate fail. A comparison never seen
// red proves nothing.
func TestAStaleArtefactIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capabilities.md")

	if err := os.WriteFile(path, []byte("not what the data says\n"), codegen.FilePerms); err != nil {
		t.Fatalf("write the stale artefact: %v", err)
	}

	if err := run(path, true); !errors.Is(err, errStale) {
		t.Errorf("a stale artefact reported %v, want errStale", err)
	}
}

// TestAMissingArtefactIsRefused covers the first run, where the file is absent
// rather than different.
func TestAMissingArtefactIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capabilities.md")

	if err := run(path, true); !errors.Is(err, errStale) {
		t.Errorf("a missing artefact reported %v, want errStale", err)
	}
}
