package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/codegen"
	"github.com/prusa3d/prusa-connect/internal/kernel/deviceissue"
)

// The artefacts this command writes, from the package directory.
const (
	committedCatalogue = "../../docs/vocabularies/device-issues.json"
	committedReference = "../../docs/src/content/docs/device-issues.md"
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

	if err := run(deviceissue.Docs(), checking(committedCatalogue, committedReference)); err != nil {
		t.Errorf("%v", err)
	}
}

// TestEveryReasonReachesBothArtefacts names the reason that went missing rather
// than reporting that two files differ, and refuses to pass on an empty
// artefact.
func TestEveryReasonReachesBothArtefacts(t *testing.T) {
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

	page := string(renderReference(deviceissue.Docs()))

	for _, reason := range deviceissue.Reasons() {
		if !strings.Contains(page, "`"+reason.String()+"`") {
			t.Errorf("%s is in the closed set and not on the published page", reason)
		}
	}

	if len(catalogue.Entries) != len(deviceissue.Reasons()) {
		t.Errorf("the committed catalogue holds %d entries, the closed set holds %d",
			len(catalogue.Entries), len(deviceissue.Reasons()))
	}
}

// TestThePageCarriesWhatToDoAboutEveryReason is what separates this page from a
// list of tokens. A reader opening it is deciding whether to walk to a printer,
// so a reason with no action published is a badge with nothing behind it.
func TestThePageCarriesWhatToDoAboutEveryReason(t *testing.T) {
	t.Parallel()

	page := string(renderReference(deviceissue.Docs()))

	for _, doc := range deviceissue.Docs() {
		if !strings.Contains(page, doc.Action) {
			t.Errorf("%s is on the page and what to do about it is not", doc.Reason)
		}

		if !strings.Contains(page, "**severity** `"+doc.Severity.String()+"`") {
			t.Errorf("%s is on the page and its severity is not", doc.Reason)
		}
	}
}

// TestAReasonMissingFromTheArtefactIsRefused is the first direction of
// ADR-20260825-bhe rule 8: the code declares a reason and the committed artefact does
// not name it.
func TestAReasonMissingFromTheArtefactIsRefused(t *testing.T) {
	t.Parallel()

	entries := deviceissue.Docs()
	catalogue := writeCatalogue(t, entries[1:])

	err := run(entries, checking(catalogue, filepath.Join(t.TempDir(), "reference.md")))
	if !errors.Is(err, errEntriesMissing) {
		t.Errorf("a reason absent from the artefact reported %v, want errEntriesMissing", err)
	}

	if !strings.Contains(err.Error(), entries[0].Reason.String()) {
		t.Errorf("the refusal does not name %s: %v", entries[0].Reason, err)
	}
}

// TestANewlyDeclaredReasonIsWrittenIntoTheArtefact is the operation this
// command exists for. The event-type generator refused it once: declaring an
// entry left the run that adds it reporting the entry as missing and telling
// the author to run the generator, which is the run that had just failed.
func TestANewlyDeclaredReasonIsWrittenIntoTheArtefact(t *testing.T) {
	t.Parallel()

	entries := deviceissue.Docs()
	catalogue := writeCatalogue(t, entries[1:])
	reference := filepath.Join(t.TempDir(), "reference.md")

	if err := run(entries, options{cataloguePath: catalogue, referencePath: reference}); err != nil {
		t.Fatalf("writing the artefact refused a newly declared reason: %v", err)
	}

	written, err := os.ReadFile(catalogue)
	if err != nil {
		t.Fatalf("read the written catalogue: %v", err)
	}

	if !strings.Contains(string(written), entries[0].Reason.String()) {
		t.Errorf("the written catalogue does not name %s", entries[0].Reason)
	}
}

// TestAnArtefactRowNamingNoReasonIsRefused is the other direction: the
// committed artefact names a reason nothing declares. A gate that only catches
// one direction has shipped here before.
func TestAnArtefactRowNamingNoReasonIsRefused(t *testing.T) {
	t.Parallel()

	entries := deviceissue.Docs()
	retired := deviceissue.Doc{
		Reason:      "invented",
		Severity:    deviceissue.SeverityWarning,
		Summary:     "A reason nothing declares any more.",
		Description: "Left behind by a retirement.",
		Action:      "Nothing.",
		Version:     deviceissue.VocabularyVersion,
	}

	catalogue := writeCatalogue(t, append(entries, retired))

	err := run(entries, checking(catalogue, filepath.Join(t.TempDir(), "reference.md")))
	if !errors.Is(err, errEntriesUnknown) {
		t.Errorf("an artefact row naming no reason reported %v, want errEntriesUnknown", err)
	}

	if !strings.Contains(err.Error(), "invented") {
		t.Errorf("the refusal does not name the retired reason: %v", err)
	}
}

// TestADeliberateRetirementIsAllowed covers the escape hatch.
func TestADeliberateRetirementIsAllowed(t *testing.T) {
	t.Parallel()

	entries := deviceissue.Docs()
	retired := deviceissue.Doc{
		Reason: "invented", Severity: deviceissue.SeverityWarning,
		Summary: "s", Description: "d", Action: "a", Version: deviceissue.VocabularyVersion,
	}
	catalogue := writeCatalogue(t, append(entries, retired))

	opts := checking(catalogue, filepath.Join(t.TempDir(), "reference.md"))
	opts.allowRemovals = true

	if err := run(entries, opts); errors.Is(err, errEntriesUnknown) {
		t.Errorf("-allow-removals still refused the retirement: %v", err)
	}
}

// TestAnUndescribedReasonIsRefused holds ADR-20260825-bhe rule 2 at the generator. The
// action is one of the cells, because a reason a reader cannot act on is worse
// than no reason at all.
func TestAnUndescribedReasonIsRefused(t *testing.T) {
	t.Parallel()

	tests := map[string]deviceissue.Doc{
		"no reason": {
			Severity: deviceissue.SeverityInfo, Summary: "s", Description: "d", Action: "a",
			Version: deviceissue.VocabularyVersion,
		},
		"no summary": {
			Reason: "a_reason", Severity: deviceissue.SeverityInfo, Description: "d", Action: "a",
			Version: deviceissue.VocabularyVersion,
		},
		"no description": {
			Reason: "a_reason", Severity: deviceissue.SeverityInfo, Summary: "s", Action: "a",
			Version: deviceissue.VocabularyVersion,
		},
		"no action": {
			Reason: "a_reason", Severity: deviceissue.SeverityInfo, Summary: "s", Description: "d",
			Version: deviceissue.VocabularyVersion,
		},
		"no severity": {
			Reason: "a_reason", Summary: "s", Description: "d", Action: "a",
			Version: deviceissue.VocabularyVersion,
		},
		"no version": {
			Reason: "a_reason", Severity: deviceissue.SeverityInfo, Summary: "s", Description: "d",
			Action: "a",
		},
	}

	for name, entry := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			err := run([]deviceissue.Doc{entry},
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

	catalogue := writeCatalogue(t, deviceissue.Docs())
	reference := filepath.Join(t.TempDir(), "reference.md")

	if err := os.WriteFile(reference, []byte("not what the table says\n"), codegen.FilePerms); err != nil {
		t.Fatalf("write the stale artefact: %v", err)
	}

	if err := run(deviceissue.Docs(), checking(catalogue, reference)); !errors.Is(err, errStale) {
		t.Errorf("a stale artefact reported %v, want errStale", err)
	}
}

// TestAMissingArtefactIsRefused covers the first run.
func TestAMissingArtefactIsRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	err := run(deviceissue.Docs(),
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

	if err := run(deviceissue.Docs(), opts); err != nil {
		t.Fatalf("write the artefacts: %v", err)
	}

	opts.check = true
	if err := run(deviceissue.Docs(), opts); err != nil {
		t.Errorf("checking what the generator just wrote reported %v", err)
	}
}

func writeCatalogue(t *testing.T, entries []deviceissue.Doc) string {
	t.Helper()

	encoded, err := json.MarshalIndent(catalogueFile{
		Comment:    generatedNotice,
		Vocabulary: deviceissue.VocabularyName,
		Version:    deviceissue.VocabularyVersion,
		Format:     deviceissue.VocabularyFormat,
		Severities: deviceissue.Severities(),
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
