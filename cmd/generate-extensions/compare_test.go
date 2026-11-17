package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/codegen"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// descriptorFor mints one descriptor the way a registration does, so a test
// case reads the same identity the generator would.
func descriptorFor(t *testing.T, kind extension.Kind, name string) extension.Descriptor {
	t.Helper()

	descriptor, err := extension.Declare(kind, "1.0.0", name, extension.Doc{
		Summary:     "An entry used only by this command's tests.",
		Description: "It registers nothing and exists to exercise the diff.",
		Version:     "1.0.0",
		Contract:    "1.0.0",
		Stability:   extension.StabilityExperimental,
	})
	if err != nil {
		t.Fatalf("Declare(%q): %v", name, err)
	}

	return descriptor
}

func TestAVanishedEntryStopsTheRun(t *testing.T) {
	t.Parallel()

	committed := []extension.Descriptor{
		descriptorFor(t, extension.KindStorage, "kept"),
		descriptorFor(t, extension.KindStorage, "gone"),
	}
	current := []extension.Descriptor{committed[0]}

	diff := diffCatalogue(committed, current)

	if len(diff.Removed) != 1 || diff.Removed[0].Name() != "gone" {
		t.Fatalf("the diff reports %d removals: %+v", len(diff.Removed), entryNames(diff.Removed))
	}

	err := checkRemovals(diff, false)
	if !errors.Is(err, errEntriesRemoved) {
		t.Fatalf("an entry that vanished did not stop the run: %v", err)
	}

	if !strings.Contains(err.Error(), "gone") {
		t.Errorf("the refusal does not name the entry that vanished: %v", err)
	}

	if err = checkRemovals(diff, true); err != nil {
		t.Errorf("-allow-removals did not let a deliberate retirement through: %v", err)
	}
}

// An addition is invisible to a configuration that does not name it, so it is
// reported and does not fail the run.
func TestAnAddedEntryDoesNotStopTheRun(t *testing.T) {
	t.Parallel()

	committed := []extension.Descriptor{descriptorFor(t, extension.KindStorage, "kept")}
	current := append(slicesOf(committed), descriptorFor(t, extension.KindStorage, "added"))

	diff := diffCatalogue(committed, current)

	if len(diff.Added) != 1 || diff.Added[0].Name() != "added" {
		t.Fatalf("the diff reports %d additions: %+v", len(diff.Added), entryNames(diff.Added))
	}

	if err := checkRemovals(diff, false); err != nil {
		t.Errorf("an addition stopped the run: %v", err)
	}
}

// One name may be taken once per extension point, so the diff has to compare
// kind and name together or a storage backend disappearing looks like an event
// sink that is still there.
func TestTheDiffComparesKindAndName(t *testing.T) {
	t.Parallel()

	committed := []extension.Descriptor{descriptorFor(t, extension.KindStorage, "shared")}
	current := []extension.Descriptor{descriptorFor(t, extension.KindEventSink, "shared")}

	diff := diffCatalogue(committed, current)

	if len(diff.Removed) != 1 || len(diff.Added) != 1 {
		t.Errorf("the diff treated two kinds of %q as one entry: removed %d, added %d",
			"shared", len(diff.Removed), len(diff.Added))
	}
}

// The diff reads each side into a set keyed by kind and qualified name, so a
// name held twice collapses into one key and the run agrees with itself. A
// duplicate is the one thing that reaches the published page twice, so the
// comparison has to see it rather than deduplicate it.
func TestOneNameHeldTwiceStopsTheRun(t *testing.T) {
	t.Parallel()

	entry := descriptorFor(t, extension.KindStorage, "held-twice")

	err := checkDuplicates("the registries", []extension.Descriptor{entry, entry})
	if !errors.Is(err, errDuplicateEntries) {
		t.Fatalf("a catalogue holding one qualified name twice passed the comparison: %v", err)
	}

	if !strings.Contains(err.Error(), "held-twice") {
		t.Errorf("the refusal does not name the entry held twice: %v", err)
	}

	if err = checkDuplicates("the registries", []extension.Descriptor{entry}); err != nil {
		t.Errorf("a catalogue holding one name once was refused: %v", err)
	}
}

// A summary and a description are published as prose, and markdown carries html
// through, so a tag an entry wrote would run on the page every plugin author
// reads. A pipe in a value published inside a table ends the column it sits in.
func TestTheReferenceDoesNotPublishMarkupAnEntryCarries(t *testing.T) {
	t.Parallel()

	doc := extension.Doc{
		Summary:     "Posts events <img src=x onerror=alert(1)> to one endpoint.",
		Description: "A paragraph carrying <script>fetch('/admin')</script>.",
		Version:     "1.0.0",
		Contract:    "1.0.0",
		Stability:   extension.StabilityExperimental,
		Options: []extension.Option{
			{Key: "url", Type: extension.OptionURL, Default: "a|b", Summary: "the <b>endpoint</b>"},
		},
	}

	descriptor, err := extension.Declare(extension.KindEventSink, "1.0.0", "injected", doc)
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}

	page := string(renderReference([]extension.Descriptor{descriptor}))

	for _, markup := range []string{"<img", "<script>", "<b>"} {
		if strings.Contains(page, markup) {
			t.Errorf("the reference publishes %q as markup", markup)
		}
	}

	if !strings.Contains(page, `a\|b`) {
		t.Errorf("a pipe in a config default ends the column it sits in:\n%s", page)
	}
}

func TestTheCommittedArtefactDecodesBackIntoDescriptors(t *testing.T) {
	t.Parallel()

	entries := []extension.Descriptor{descriptorFor(t, extension.KindStorage, "round-trip")}

	snapshot, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	file, err := renderCatalogueFile(append(snapshot, '\n'))
	if err != nil {
		t.Fatalf("renderCatalogueFile: %v", err)
	}

	path := t.TempDir() + "/extensions.json"
	if err = codegen.WriteFile(path, file); err != nil {
		t.Fatalf("codegen.WriteFile: %v", err)
	}

	decoded, err := readCatalogueFile(path)
	if err != nil {
		t.Fatalf("readCatalogueFile: %v", err)
	}

	if len(decoded) != 1 || decoded[0].String() != entries[0].String() {
		t.Errorf("the committed artefact decoded to %+v", entryNames(decoded))
	}
}

func TestTheReferenceRendersEveryDescriptorField(t *testing.T) {
	t.Parallel()

	doc := extension.Doc{
		Summary:     "Posts events to one endpoint.",
		Description: "The longer paragraph.",
		Version:     "2.3.4",
		Contract:    "1.0.0",
		Stability:   extension.StabilityDeprecated,
		ReplacedBy:  "webhook-v2",
		Emits: []extension.Emission{
			{Type: "job.updated", Version: "1.1.0", Summary: "a job changed"},
		},
		Publishes: []extension.Destination{
			{Address: "v1/devices/printers/{printerId}/data/state", Version: "1.2.0", Summary: "a state changed"},
		},
		Options: []extension.Option{
			{Key: "url", Type: extension.OptionURL, Required: true, Summary: "the endpoint"},
			{Key: "timeout", Type: extension.OptionDuration, Default: "5s", Summary: "how long a post waits"},
		},
		Requires: []extension.Requirement{{Capability: "core.event.stream", Since: "1.0.0"}},
	}

	descriptor, err := extension.Declare(extension.KindEventSink, "1.0.0", "rendered", doc)
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}

	page := string(renderReference([]extension.Descriptor{descriptor}))

	for _, want := range []string{
		"Event sinks", "sdk/eventsapi", "`rendered`", descriptor.Module(),
		"2.3.4", "1.0.0", "deprecated", "`webhook-v2`",
		"Posts events to one endpoint.", "The longer paragraph.",
		"`job.updated`", "1.1.0", "a job changed",
		"Published on its own transport",
		"`v1/devices/printers/{printerId}/data/state`", "1.2.0", "a state changed",
		// "required" rather than a bare "yes": ADR-20260826-swm rule 6 wants a value
		// that still means something read aloud, away from its column header.
		"`url`", "url", "required", "the endpoint",
		"`timeout`", "duration", "`5s`", "how long a post waits",
		"`core.event.stream`",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the reference does not carry %q", want)
		}
	}
}

func slicesOf(in []extension.Descriptor) []extension.Descriptor {
	out := make([]extension.Descriptor, len(in))
	copy(out, in)

	return out
}
