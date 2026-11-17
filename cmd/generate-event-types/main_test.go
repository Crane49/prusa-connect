package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/codegen"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// The artefacts this command writes, from the package directory.
const (
	committedCatalogue = "../../docs/vocabularies/event-types.json"
	committedReference = "../../docs/src/content/docs/event-types.md"
)

func checking(catalogue, reference string) options {
	return options{cataloguePath: catalogue, referencePath: reference, check: true}
}

// TestTheCommittedArtefactsMatchTheTable is the gate that keeps the published
// set from drifting. It is a test rather than only a make target so that it
// runs under `go test ./...` whether or not anybody remembers the target
// (ADR-20260825-bhe rule 9).
func TestTheCommittedArtefactsMatchTheTable(t *testing.T) {
	t.Parallel()

	if err := run(event.TypeDocs(), checking(committedCatalogue, committedReference)); err != nil {
		t.Errorf("%v", err)
	}
}

// TestEveryTypeReachesBothArtefacts names the type that went missing rather
// than reporting that two files differ, and refuses to pass on an empty
// artefact.
func TestEveryTypeReachesBothArtefacts(t *testing.T) {
	t.Parallel()

	catalogue, err := codegen.ReadCatalogue[catalogueFile](committedCatalogue)
	if err != nil {
		t.Fatalf("read the committed catalogue: %v", err)
	}

	if catalogue == nil {
		t.Fatal("the committed catalogue does not exist")
	}

	if len(catalogue.Entries) == 0 {
		t.Fatal("the committed catalogue holds no entries, so this gate would pass on an empty file")
	}

	page := string(renderReference(event.TypeDocs()))

	for _, name := range event.Types() {
		if !strings.Contains(page, "`"+name+"`") {
			t.Errorf("%s is in the closed set and not on the published page", name)
		}
	}

	if len(catalogue.Entries) != len(event.Types()) {
		t.Errorf("the committed catalogue holds %d entries, the closed set holds %d",
			len(catalogue.Entries), len(event.Types()))
	}
}

// TestATypeMissingFromTheArtefactIsRefused is the first direction of ADR-20260825-bhe
// rule 8: the code declares a type and the committed artefact does not name it.
func TestATypeMissingFromTheArtefactIsRefused(t *testing.T) {
	t.Parallel()

	entries := event.TypeDocs()
	catalogue := writeCatalogue(t, entries[1:])

	err := run(entries, checking(catalogue, filepath.Join(t.TempDir(), "reference.md")))
	if !errors.Is(err, errEntriesMissing) {
		t.Errorf("a type absent from the artefact reported %v, want errEntriesMissing", err)
	}

	if !strings.Contains(err.Error(), entries[0].Type) {
		t.Errorf("the refusal does not name %s: %v", entries[0].Type, err)
	}
}

// TestANewlyDeclaredTypeIsWrittenIntoTheArtefact is the operation this command
// exists for, and it was refused.
//
// Declaring a type left the generator reporting it as missing from the
// committed artefact and telling the author to run the generator, which is the
// run that had just failed. The only way to add a type was to hand-edit the
// artefact the command owns, which is what a generated file is not for.
func TestANewlyDeclaredTypeIsWrittenIntoTheArtefact(t *testing.T) {
	t.Parallel()

	entries := event.TypeDocs()
	catalogue := writeCatalogue(t, entries[1:])
	reference := filepath.Join(t.TempDir(), "reference.md")

	err := run(entries, options{cataloguePath: catalogue, referencePath: reference})
	if err != nil {
		t.Fatalf("writing the artefact refused a newly declared type: %v", err)
	}

	written, err := os.ReadFile(catalogue)
	if err != nil {
		t.Fatalf("read the written catalogue: %v", err)
	}

	if !strings.Contains(string(written), entries[0].Type) {
		t.Errorf("the written catalogue does not name %s", entries[0].Type)
	}
}

// TestAnArtefactRowNamingNoTypeIsRefused is the other direction: the committed
// artefact names a type nothing declares. A gate that only catches one
// direction has shipped here before.
func TestAnArtefactRowNamingNoTypeIsRefused(t *testing.T) {
	t.Parallel()

	entries := event.TypeDocs()
	retired := event.TypeDoc{
		Type:        event.CoreNamespace + "printer.invented",
		Class:       event.ClassState,
		Summary:     "A type nothing declares any more.",
		Description: "Left behind by a retirement.",
		Version:     event.VocabularyVersion,
	}

	catalogue := writeCatalogue(t, append(entries, retired))

	err := run(entries, checking(catalogue, filepath.Join(t.TempDir(), "reference.md")))
	if !errors.Is(err, errEntriesUnknown) {
		t.Errorf("an artefact row naming no type reported %v, want errEntriesUnknown", err)
	}

	if !strings.Contains(err.Error(), "printer.invented") {
		t.Errorf("the refusal does not name the retired type: %v", err)
	}
}

// TestADeliberateRetirementIsAllowed covers the escape hatch.
func TestADeliberateRetirementIsAllowed(t *testing.T) {
	t.Parallel()

	entries := event.TypeDocs()
	retired := event.TypeDoc{
		Type: event.CoreNamespace + "printer.invented", Class: event.ClassState,
		Summary: "s", Description: "d", Version: event.VocabularyVersion,
	}
	catalogue := writeCatalogue(t, append(entries, retired))

	opts := checking(catalogue, filepath.Join(t.TempDir(), "reference.md"))
	opts.allowRemovals = true

	if err := run(entries, opts); errors.Is(err, errEntriesUnknown) {
		t.Errorf("-allow-removals still refused the retirement: %v", err)
	}
}

// TestAnUndescribedTypeIsRefused holds ADR-20260825-bhe rule 2 at the generator.
func TestAnUndescribedTypeIsRefused(t *testing.T) {
	t.Parallel()

	tests := map[string]event.TypeDoc{
		"no summary":     {Type: "a.b", Class: event.ClassState, Description: "d", Version: "1.0.0"},
		"no description": {Type: "a.b", Class: event.ClassState, Summary: "s", Version: "1.0.0"},
		"no class":       {Type: "a.b", Summary: "s", Description: "d", Version: "1.0.0"},
		"no version":     {Type: "a.b", Class: event.ClassState, Summary: "s", Description: "d"},
	}

	for name, entry := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			err := run([]event.TypeDoc{entry},
				checking(filepath.Join(dir, "catalogue.json"), filepath.Join(dir, "reference.md")))

			if !errors.Is(err, errUndescribed) {
				t.Errorf("%s reported %v, want errUndescribed", name, err)
			}
		})
	}
}

// TestAStaleArtefactIsRefused watches the committed-output gate fail.
func TestAStaleArtefactIsRefused(t *testing.T) {
	t.Parallel()

	catalogue := writeCatalogue(t, event.TypeDocs())
	reference := filepath.Join(t.TempDir(), "reference.md")

	if err := os.WriteFile(reference, []byte("not what the table says\n"), codegen.FilePerms); err != nil {
		t.Fatalf("write the stale artefact: %v", err)
	}

	if err := run(event.TypeDocs(), checking(catalogue, reference)); !errors.Is(err, errStale) {
		t.Errorf("a stale artefact reported %v, want errStale", err)
	}
}

// TestAMissingArtefactIsRefused covers the first run.
func TestAMissingArtefactIsRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	err := run(event.TypeDocs(),
		checking(filepath.Join(dir, "catalogue.json"), filepath.Join(dir, "reference.md")))
	if !errors.Is(err, errStale) {
		t.Errorf("a missing artefact reported %v, want errStale", err)
	}
}

// TestWritingThenCheckingIsGreen proves the generator's own output satisfies
// its own gate.
func TestWritingThenCheckingIsGreen(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	opts := options{
		cataloguePath: filepath.Join(dir, "catalogue.json"),
		referencePath: filepath.Join(dir, "reference.md"),
	}

	if err := run(event.TypeDocs(), opts); err != nil {
		t.Fatalf("write the artefacts: %v", err)
	}

	opts.check = true
	if err := run(event.TypeDocs(), opts); err != nil {
		t.Errorf("checking what the generator just wrote reported %v", err)
	}
}

func writeCatalogue(t *testing.T, entries []event.TypeDoc) string {
	t.Helper()

	encoded, err := json.MarshalIndent(catalogueFile{
		Comment:    generatedNotice,
		Vocabulary: event.VocabularyName,
		Version:    event.VocabularyVersion,
		Format:     event.VocabularyFormat,
		Namespace:  event.CoreNamespace,
		Entries:    entries,
	}, "", "  ")
	if err != nil {
		t.Fatalf("encode the catalogue: %v", err)
	}

	path := filepath.Join(t.TempDir(), "catalogue.json")
	if err = os.WriteFile(path, append(encoded, '\n'), codegen.FilePerms); err != nil {
		t.Fatalf("write the catalogue: %v", err)
	}

	return path
}
