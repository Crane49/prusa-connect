package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/codegen"
)

// The artefacts this command writes, from the package directory.
const (
	committedCatalogue = "../../docs/vocabularies/authorization.json"
	committedReference = "../../docs/src/content/docs/authorization-vocabulary.md"
)

func checking(catalogue, reference string) options {
	return options{cataloguePath: catalogue, referencePath: reference, check: true}
}

// TestTheCommittedArtefactsMatchTheClosedSets is the gate that keeps the
// published vocabulary from drifting. It is a test rather than only a make
// target so that it runs under `go test ./...` whether or not anybody remembers
// the target (ADR-20260825-bhe rule 9).
func TestTheCommittedArtefactsMatchTheClosedSets(t *testing.T) {
	t.Parallel()

	if err := run(current(), checking(committedCatalogue, committedReference)); err != nil {
		t.Errorf("%v", err)
	}
}

// TestEveryNameReachesBothArtefacts names what went missing rather than
// reporting that two files differ, and refuses to pass on an empty artefact.
func TestEveryNameReachesBothArtefacts(t *testing.T) {
	t.Parallel()

	catalogue, err := codegen.ReadCatalogue[catalogueFile](committedCatalogue)
	if err != nil {
		t.Fatalf("read the committed catalogue: %v", err)
	}

	if catalogue == nil {
		t.Fatal("the committed catalogue does not exist")
	}

	set := current()

	sizes := map[string][2]int{
		"actions":   {len(catalogue.Actions), len(set.Actions)},
		"types":     {len(catalogue.Types), len(set.Types)},
		"relations": {len(catalogue.Relations), len(set.Relations)},
		"shapes":    {len(catalogue.Shapes), len(set.Shapes)},
	}

	for what, pair := range sizes {
		if pair[1] == 0 {
			t.Fatalf("the code declares no %s, so this gate would pass on an empty file", what)
		}

		if pair[0] != pair[1] {
			t.Errorf("the committed catalogue holds %d %s, the code declares %d", pair[0], what, pair[1])
		}
	}

	page := string(renderReference(set))

	for _, action := range authz.Actions() {
		if !strings.Contains(page, "`"+action.String()+"`") {
			t.Errorf("action %s is in the closed set and not on the published page", action)
		}
	}

	for _, typ := range authz.Types() {
		if !strings.Contains(page, "`"+typ.String()+"`") {
			t.Errorf("type %s is in the closed set and not on the published page", typ)
		}
	}

	for _, relation := range authz.Relations() {
		if !strings.Contains(page, "`"+relation.String()+"`") {
			t.Errorf("relation %s is in the closed set and not on the published page", relation)
		}
	}
}

// TestAnEntryMissingFromTheArtefactIsRefused is the first direction of
// ADR-20260825-bhe rule 8, on each of the four sets: the code declares a name and the
// committed artefact does not carry it.
func TestAnEntryMissingFromTheArtefactIsRefused(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*catalogueFile){
		"action":   func(f *catalogueFile) { f.Actions = f.Actions[1:] },
		"type":     func(f *catalogueFile) { f.Types = f.Types[1:] },
		"relation": func(f *catalogueFile) { f.Relations = f.Relations[1:] },
		"shape":    func(f *catalogueFile) { f.Shapes = f.Shapes[1:] },
	}

	for name, drop := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			catalogue := writeCatalogue(t, mutate(current(), drop))

			err := run(current(), checking(catalogue, filepath.Join(t.TempDir(), "reference.md")))
			if !errors.Is(err, errEntriesMissing) {
				t.Errorf("a dropped %s reported %v, want errEntriesMissing", name, err)
			}
		})
	}
}

// TestAnArtefactRowNamingNothingIsRefused is the other direction, on each of
// the four sets. A gate that only catches one direction has shipped here
// before.
func TestAnArtefactRowNamingNothingIsRefused(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*catalogueFile){
		"action": func(f *catalogueFile) {
			f.Actions = append(f.Actions, authz.ActionDoc{
				Action: "detonate", Domain: authz.TypeDevice, Summary: "s", Description: "d", Version: "1.0.0",
			})
		},
		"type": func(f *catalogueFile) {
			f.Types = append(f.Types, authz.TypeDoc{
				Type: "cupboard", Summary: "s", Description: "d", Version: "1.0.0",
			})
		},
		"relation": func(f *catalogueFile) {
			f.Relations = append(f.Relations, authz.RelationDoc{
				Relation: "custodian", Summary: "s", Description: "d", Version: "1.0.0",
			})
		},
		"shape": func(f *catalogueFile) {
			f.Shapes = append(f.Shapes, authz.ShapeDoc{
				ObjectType: authz.TypeDevice, Relation: "custodian", SubjectType: authz.TypeUser,
			})
		},
	}

	for name, add := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			catalogue := writeCatalogue(t, mutate(current(), add))

			err := run(current(), checking(catalogue, filepath.Join(t.TempDir(), "reference.md")))
			if !errors.Is(err, errEntriesUnknown) {
				t.Errorf("an extra %s row reported %v, want errEntriesUnknown", name, err)
			}
		})
	}
}

// TestADeliberateRetirementIsAllowed covers the escape hatch, so the
// vanished-entry gate is not the only thing between a breaking release and a
// hand-edited artefact.
func TestADeliberateRetirementIsAllowed(t *testing.T) {
	t.Parallel()

	catalogue := writeCatalogue(t, mutate(current(), func(f *catalogueFile) {
		f.Relations = append(f.Relations, authz.RelationDoc{
			Relation: "custodian", Summary: "s", Description: "d", Version: "1.0.0",
		})
	}))

	opts := checking(catalogue, filepath.Join(t.TempDir(), "reference.md"))
	opts.allowRemovals = true

	if err := run(current(), opts); errors.Is(err, errEntriesUnknown) {
		t.Errorf("-allow-removals still refused the retirement: %v", err)
	}
}

// TestAnUndescribedEntryIsRefused holds ADR-20260825-bhe rule 2 at the generator.
func TestAnUndescribedEntryIsRefused(t *testing.T) {
	t.Parallel()

	tests := map[string]vocabulary{
		"action with no summary": {
			Actions: []authz.ActionDoc{{Action: authz.ActionAdminister, Description: "d", Version: "1.0.0"}},
		},
		"type with no description": {
			Types: []authz.TypeDoc{{Type: authz.TypeUser, Summary: "s", Version: "1.0.0"}},
		},
		"relation with no version": {
			Relations: []authz.RelationDoc{{Relation: authz.RelViewer, Summary: "s", Description: "d"}},
		},
	}

	for name, set := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			err := run(set, checking(filepath.Join(dir, "catalogue.json"), filepath.Join(dir, "reference.md")))

			if !errors.Is(err, errUndescribed) {
				t.Errorf("%s reported %v, want errUndescribed", name, err)
			}
		})
	}
}

// TestAStaleArtefactIsRefused watches the committed-output gate fail.
func TestAStaleArtefactIsRefused(t *testing.T) {
	t.Parallel()

	catalogue := writeCatalogue(t, mutate(current(), func(*catalogueFile) {}))
	reference := filepath.Join(t.TempDir(), "reference.md")

	if err := os.WriteFile(reference, []byte("not what the sets say\n"), codegen.FilePerms); err != nil {
		t.Fatalf("write the stale artefact: %v", err)
	}

	if err := run(current(), checking(catalogue, reference)); !errors.Is(err, errStale) {
		t.Errorf("a stale artefact reported %v, want errStale", err)
	}
}

// TestAMissingArtefactIsRefused covers the first run, where the file is absent
// rather than different.
func TestAMissingArtefactIsRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	err := run(current(),
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

	if err := run(current(), opts); err != nil {
		t.Fatalf("write the artefacts: %v", err)
	}

	opts.check = true
	if err := run(current(), opts); err != nil {
		t.Errorf("checking what the generator just wrote reported %v", err)
	}
}

// TestAddingANameRegeneratesRatherThanRefusing guards the write path against
// its own gate. The drift gate's missing direction fires on exactly the state a
// new action leaves behind, so a write that runs it deadlocks: the only remedy
// the error offers is the command that refused. Writing over a committed
// artefact short of a name must regenerate it, and only -check may refuse.
func TestAddingANameRegeneratesRatherThanRefusing(t *testing.T) {
	t.Parallel()

	catalogue := writeCatalogue(t, mutate(current(), func(f *catalogueFile) {
		f.Actions = f.Actions[1:]
	}))

	opts := options{cataloguePath: catalogue, referencePath: filepath.Join(t.TempDir(), "reference.md")}
	if err := run(current(), opts); err != nil {
		t.Fatalf("regenerating over an artefact short of a name reported %v", err)
	}

	opts.check = true
	if err := run(current(), opts); err != nil {
		t.Errorf("checking what the generator just wrote reported %v", err)
	}
}

// TestRemovingANameNeedsTheRetirementFlagOnTheWritePath keeps the retirement
// ratchet where the person removing an action actually stands. Gating both drift
// directions on -check would let a regeneration drop a name silently, and the
// -check that follows would then compare a committed artefact that agrees with
// the code, so nothing would ever ask.
func TestRemovingANameNeedsTheRetirementFlagOnTheWritePath(t *testing.T) {
	t.Parallel()

	// A committed row naming what the code does not declare is the state left
	// behind by removing an action from the Go vocabulary.
	catalogue := writeCatalogue(t, mutate(current(), func(f *catalogueFile) {
		f.Actions = append(f.Actions, authz.ActionDoc{
			Action: "detonate", Domain: authz.TypeDevice, Summary: "s", Description: "d", Version: "1.0.0",
		})
	}))

	opts := options{cataloguePath: catalogue, referencePath: filepath.Join(t.TempDir(), "reference.md")}

	err := run(current(), opts)
	if !errors.Is(err, errEntriesUnknown) {
		t.Errorf("regenerating over a retired action reported %v, want errEntriesUnknown", err)
	}

	opts.allowRemovals = true
	if err = run(current(), opts); err != nil {
		t.Errorf("-allow-removals still refused the write: %v", err)
	}
}

// mutate builds a catalogue file from the current sets and applies one change
// to it, which is how each direction of the drift gate is watched failing.
func mutate(set vocabulary, change func(*catalogueFile)) catalogueFile {
	file := catalogueFile{
		Comment:    generatedNotice,
		Vocabulary: authz.VocabularyName,
		Version:    authz.VocabularyVersion,
		Format:     authz.VocabularyFormat,
		Actions:    set.Actions,
		Types:      set.Types,
		Relations:  set.Relations,
		Shapes:     set.Shapes,
	}

	change(&file)

	return file
}

func writeCatalogue(t *testing.T, file catalogueFile) string {
	t.Helper()

	encoded, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		t.Fatalf("encode the catalogue: %v", err)
	}

	path := filepath.Join(t.TempDir(), "catalogue.json")
	if err = os.WriteFile(path, append(encoded, '\n'), codegen.FilePerms); err != nil {
		t.Fatalf("write the catalogue: %v", err)
	}

	return path
}

// TestGeneratingWritesAnEntryTheArtefactDoesNotHave is the state of the tree the
// moment somebody declares a new action, type, relation or shape: the code has
// it and the committed artefact does not. Writing the artefact is what closes
// that gap, so the generator has to be able to run.
//
// It was not able to. The missing-entry gate ran in both modes and its message
// named `make generate-authz-vocabulary` as the remedy, which is the command it
// had just refused. No command could add an entry, and the artefacts say at the
// top that they are never edited by hand.
func TestGeneratingWritesAnEntryTheArtefactDoesNotHave(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	catalogue := filepath.Join(dir, "catalogue.json")
	reference := filepath.Join(dir, "reference.md")

	stale := mutate(current(), func(f *catalogueFile) { f.Types = f.Types[1:] })

	encoded, err := json.MarshalIndent(stale, "", "  ")
	if err != nil {
		t.Fatalf("encode the catalogue: %v", err)
	}

	if err = os.WriteFile(catalogue, append(encoded, '\n'), codegen.FilePerms); err != nil {
		t.Fatalf("write the catalogue: %v", err)
	}

	opts := options{cataloguePath: catalogue, referencePath: reference}
	if err = run(current(), opts); err != nil {
		t.Fatalf("the generator refused to write the entry it exists to add: %v", err)
	}

	// The check is the arming half. A generator that wrote nothing at all would
	// leave the file the test wrote and report no error.
	opts.check = true
	if err = run(current(), opts); err != nil {
		t.Errorf("checking what the generator just wrote reported %v", err)
	}
}
