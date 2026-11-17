package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/codegen"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// The artefacts this command writes, from the package directory.
const (
	committedCatalogue = "../../docs/vocabularies/resource-kinds.json"
	committedReference = "../../docs/src/content/docs/resource-kinds.md"
)

// checking is the option set every gate test runs under.
func checking(catalogue, reference string) options {
	return options{cataloguePath: catalogue, referencePath: reference, check: true}
}

// TestTheCommittedArtefactsMatchTheTable is the gate that keeps the published
// set from drifting. It is a test rather than only a make target so that it
// runs under `go test ./...` whether or not anybody remembers the target
// (ADR-20260825-bhe rule 9).
func TestTheCommittedArtefactsMatchTheTable(t *testing.T) {
	t.Parallel()

	if err := run(ref.KindDocs(), checking(committedCatalogue, committedReference)); err != nil {
		t.Errorf("%v", err)
	}
}

// TestEveryKindReachesBothArtefacts is the gate the byte comparison cannot be
// on its own: it names the kind that went missing rather than reporting that
// two files differ.
func TestEveryKindReachesBothArtefacts(t *testing.T) {
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

	page := string(renderReference(ref.KindDocs()))

	for _, kind := range ref.Kinds() {
		if _, ok := ref.LookupKind(kind); !ok {
			t.Errorf("%s has no descriptor", kind)
		}

		if !strings.Contains(page, "`"+kind.String()+"`") {
			t.Errorf("%s is in the closed set and not on the published page", kind)
		}
	}

	if len(catalogue.Entries) != len(ref.Kinds()) {
		t.Errorf("the committed catalogue holds %d entries, the closed set holds %d",
			len(catalogue.Entries), len(ref.Kinds()))
	}
}

// TestAKindMissingFromTheArtefactIsRefused is the first direction of ADR-20260825-bhe
// rule 8: the code declares a kind and the committed artefact does not name it.
func TestAKindMissingFromTheArtefactIsRefused(t *testing.T) {
	t.Parallel()

	entries := ref.KindDocs()
	catalogue := writeCatalogue(t, entries[1:])

	err := run(entries, checking(catalogue, filepath.Join(t.TempDir(), "reference.md")))
	if !errors.Is(err, errEntriesMissing) {
		t.Errorf("a kind absent from the artefact reported %v, want errEntriesMissing", err)
	}

	if !strings.Contains(err.Error(), entries[0].Kind.String()) {
		t.Errorf("the refusal does not name %s: %v", entries[0].Kind, err)
	}
}

// TestAnArtefactRowNamingNoKindIsRefused is the other direction: the committed
// artefact names a kind nothing declares. A gate that only catches one
// direction has shipped here before.
func TestAnArtefactRowNamingNoKindIsRefused(t *testing.T) {
	t.Parallel()

	entries := ref.KindDocs()
	retired := ref.KindDoc{
		Kind:        ref.Kind("cupboard"),
		Summary:     "A kind nothing declares any more.",
		Description: "Left behind by a retirement.",
		Version:     ref.VocabularyVersion,
	}

	catalogue := writeCatalogue(t, append(entries, retired))

	err := run(entries, checking(catalogue, filepath.Join(t.TempDir(), "reference.md")))
	if !errors.Is(err, errEntriesUnknown) {
		t.Errorf("an artefact row naming no kind reported %v, want errEntriesUnknown", err)
	}

	if !strings.Contains(err.Error(), "cupboard") {
		t.Errorf("the refusal does not name the retired kind: %v", err)
	}
}

// TestADeliberateRetirementIsAllowed covers the escape hatch, so the
// vanished-entry gate is not the only thing standing between a breaking release
// and a hand-edited artefact.
func TestADeliberateRetirementIsAllowed(t *testing.T) {
	t.Parallel()

	entries := ref.KindDocs()
	retired := ref.KindDoc{Kind: ref.Kind("cupboard"), Summary: "s", Description: "d", Version: "1.0.0"}
	catalogue := writeCatalogue(t, append(entries, retired))

	opts := checking(catalogue, filepath.Join(t.TempDir(), "reference.md"))
	opts.allowRemovals = true

	if err := run(entries, opts); errors.Is(err, errEntriesUnknown) {
		t.Errorf("-allow-removals still refused the retirement: %v", err)
	}
}

// TestAnUndescribedKindIsRefused holds ADR-20260825-bhe rule 2 at the generator, where
// an empty cell would otherwise be published.
func TestAnUndescribedKindIsRefused(t *testing.T) {
	t.Parallel()

	tests := map[string]ref.KindDoc{
		"no summary":     {Kind: ref.KindStorage, Description: "d", Version: "1.0.0"},
		"no description": {Kind: ref.KindStorage, Summary: "s", Version: "1.0.0"},
		"no version":     {Kind: ref.KindStorage, Summary: "s", Description: "d"},
	}

	for name, entry := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			err := run([]ref.KindDoc{entry},
				checking(filepath.Join(dir, "catalogue.json"), filepath.Join(dir, "reference.md")))

			if !errors.Is(err, errUndescribed) {
				t.Errorf("%s reported %v, want errUndescribed", name, err)
			}
		})
	}
}

// TestAStaleArtefactIsRefused watches the committed-output gate fail. A
// comparison never seen red proves nothing.
func TestAStaleArtefactIsRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	catalogue := writeCatalogue(t, ref.KindDocs())
	reference := filepath.Join(dir, "reference.md")

	if err := os.WriteFile(reference, []byte("not what the table says\n"), codegen.FilePerms); err != nil {
		t.Fatalf("write the stale artefact: %v", err)
	}

	if err := run(ref.KindDocs(), checking(catalogue, reference)); !errors.Is(err, errStale) {
		t.Errorf("a stale artefact reported %v, want errStale", err)
	}
}

// TestAMissingArtefactIsRefused covers the first run, where the file is absent
// rather than different.
func TestAMissingArtefactIsRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	err := run(ref.KindDocs(),
		checking(filepath.Join(dir, "catalogue.json"), filepath.Join(dir, "reference.md")))
	if !errors.Is(err, errStale) {
		t.Errorf("a missing artefact reported %v, want errStale", err)
	}
}

// TestWritingThenCheckingIsGreen proves the generator's own output satisfies
// its own gate, so a green check on the committed files is not an artefact of
// the comparison being unreachable.
func TestWritingThenCheckingIsGreen(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	opts := options{
		cataloguePath: filepath.Join(dir, "catalogue.json"),
		referencePath: filepath.Join(dir, "reference.md"),
	}

	if err := run(ref.KindDocs(), opts); err != nil {
		t.Fatalf("write the artefacts: %v", err)
	}

	opts.check = true
	if err := run(ref.KindDocs(), opts); err != nil {
		t.Errorf("checking what the generator just wrote reported %v", err)
	}
}

// writeCatalogue puts a catalogue file holding exactly these entries in a
// temporary directory and returns its path.
func writeCatalogue(t *testing.T, entries []ref.KindDoc) string {
	t.Helper()

	encoded, err := json.MarshalIndent(catalogueFile{
		Comment:    generatedNotice,
		Vocabulary: ref.VocabularyName,
		Version:    ref.VocabularyVersion,
		Format:     ref.VocabularyFormat,
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
