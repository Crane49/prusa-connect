package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/codegen"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/vocabulary"
)

// The artefacts this command writes, from the package directory.
const (
	committedCatalogue = "../../docs/vocabularies/device-models.json"
	committedReference = "../../docs/src/content/docs/device-models.md"
)

func checking(catalogue, reference string) options {
	return options{cataloguePath: catalogue, referencePath: reference, check: true}
}

// TestTheCommittedArtefactsMatchTheCatalogue is the gate that keeps the
// published set from drifting. It is a test as well as a make target, so it
// runs under `go test ./...` whether or not anybody remembers the target.
func TestTheCommittedArtefactsMatchTheCatalogue(t *testing.T) {
	t.Parallel()

	if err := run(profile.ModelDocs(), checking(committedCatalogue, committedReference)); err != nil {
		t.Errorf("%v", err)
	}
}

// TestEveryModelReachesBothArtefacts names the model that went missing rather
// than reporting that two files differ, and refuses to pass on an empty
// artefact.
func TestEveryModelReachesBothArtefacts(t *testing.T) {
	t.Parallel()

	catalogue, err := codegen.ReadCatalogue[catalogueFile](committedCatalogue)
	if err != nil {
		t.Fatalf("read the committed catalogue: %v", err)
	}

	if catalogue == nil {
		t.Fatal("the committed catalogue does not exist")
	}

	models := profile.ModelDocs()
	if len(models) == 0 {
		t.Fatal("the catalogue holds no model, so this gate would pass on an empty set")
	}

	page := string(renderReference(models))

	for _, model := range models {
		if !strings.Contains(page, "`"+model.Model+"`") {
			t.Errorf("%s is in the catalogue and not on the published page", model.Model)
		}
	}

	if len(catalogue.Entries) != len(models) {
		t.Errorf("the committed catalogue holds %d rows, the catalogue holds %d",
			len(catalogue.Entries), len(models))
	}
}

func TestARowWithAnEmptyCellIsRefused(t *testing.T) {
	t.Parallel()

	err := checkDescribed([]profile.ModelDoc{{Model: "MK4", Vendor: "Prusa Research"}})
	if !errors.Is(err, errUndescribed) {
		t.Fatalf("a row with no display name was published: %v", err)
	}

	if !strings.Contains(err.Error(), "MK4") {
		t.Errorf("the refusal does not name the row: %v", err)
	}
}

// The drift gate runs both ways: a model the catalogue gained and the artefact
// does not carry, and a row the artefact carries that the catalogue does not.
func TestDriftIsReportedInBothDirections(t *testing.T) {
	t.Parallel()

	committed := &catalogueFile{Entries: []profile.ModelDoc{{Model: "MK4"}}}

	added := checkDrift(committed, []profile.ModelDoc{{Model: "MK4"}, {Model: "MK5"}}, false, false)
	if !errors.Is(added, errEntriesMissing) || !strings.Contains(added.Error(), "MK5") {
		t.Errorf("a model the artefact does not carry was accepted: %v", added)
	}

	removed := checkDrift(committed, nil, false, false)
	if !errors.Is(removed, errEntriesUnknown) || !strings.Contains(removed.Error(), "MK4") {
		t.Errorf("a row the catalogue does not name was accepted: %v", removed)
	}

	if deliberate := checkDrift(committed, nil, true, false); deliberate != nil {
		t.Errorf("a deliberate retirement was refused: %v", deliberate)
	}
}

// A deprecated row says so on the page, so a person reading the set is told
// before the release that removes it.
func TestADeprecatedModelSaysSoOnThePage(t *testing.T) {
	t.Parallel()

	page := string(renderReference([]profile.ModelDoc{{
		Model:       "MK4",
		DisplayName: "Prusa MK4",
		Vendor:      "Prusa Research",
		Icon:        profile.IconMK4,
		Connector:   profile.ConnectorXBuddy,
		Deprecated:  vocabulary.Deprecate("0.2.0", "MK4S"),
	}}))

	for _, want := range []string{"Deprecated in 0.2.0", "MK4S"} {
		if !strings.Contains(page, want) {
			t.Errorf("the page never says %q", want)
		}
	}
}
