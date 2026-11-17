package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/codegen"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// The errors of the checks. Each one fails the run.
var (
	// errUndescribed reports a type with an empty summary, description, class
	// or version.
	errUndescribed = errors.New("an event type is undescribed")
	// errEntriesMissing reports a type the code declares that the committed
	// artefact lacks.
	errEntriesMissing = errors.New("event types are missing from the committed artefact")
	// errEntriesUnknown reports a type in the committed artefact that the code
	// does not declare.
	errEntriesUnknown = errors.New("the committed artefact names event types nothing declares")
	// errStale reports a committed artefact that differs from the generated
	// one.
	errStale = errors.New("committed artefact differs from the descriptor table")
)

// catalogueFile is the machine-readable artefact. It includes the
// vocabulary's version and format and the namespace reserved for core types.
type catalogueFile struct {
	Comment    string          `json:"$comment"`
	Vocabulary string          `json:"vocabulary"`
	Version    string          `json:"version"`
	Format     string          `json:"format"`
	Namespace  string          `json:"namespace"`
	Entries    []event.TypeDoc `json:"entries"`
}

// checkDescribed fails when a type would publish an empty cell.
func checkDescribed(entries []event.TypeDoc) error {
	var offences []string

	for _, entry := range entries {
		switch {
		case strings.TrimSpace(entry.Summary) == "":
			offences = append(offences, entry.Type+" has no summary")
		case strings.TrimSpace(entry.Description) == "":
			offences = append(offences, entry.Type+" has no description")
		case !entry.Class.Valid():
			offences = append(offences, entry.Type+" carries no event class")
		case entry.Version == "":
			offences = append(offences, entry.Type+" carries no vocabulary version")
		}
	}

	if len(offences) == 0 {
		return nil
	}

	slices.Sort(offences)

	return fmt.Errorf("%w: %s", errUndescribed, strings.Join(offences, "; "))
}

// checkDrift compares the declared types with the committed artefact. A type
// missing from the artefact fails only under -check, because a write fixes
// it. A type only in the artefact always fails unless -allow-removals is set,
// because a consumer may still use it. With no committed file there is
// nothing to compare.
func checkDrift(
	committed *catalogueFile, current []event.TypeDoc, allowRemovals, writing bool,
) error {
	if committed == nil {
		return nil
	}

	missing := namesNotIn(current, committed.Entries)
	unknown := namesNotIn(committed.Entries, current)

	var problems []error

	if len(missing) > 0 && !writing {
		problems = append(problems, fmt.Errorf(
			"%w: %s. Run make generate-event-types and commit the result",
			errEntriesMissing, strings.Join(missing, ", ")))
	}

	if len(unknown) > 0 && !allowRemovals {
		problems = append(problems, fmt.Errorf(
			"%w: %s. Restore the descriptor, or pass -allow-removals if this is a deliberate "+
				"retirement in a breaking release",
			errEntriesUnknown, strings.Join(unknown, ", ")))
	}

	return errors.Join(problems...)
}

// namesNotIn returns the types named by from and not by in, sorted.
func namesNotIn(from, in []event.TypeDoc) []string {
	present := make(map[string]struct{}, len(in))
	for _, entry := range in {
		present[entry.Type] = struct{}{}
	}

	var out []string

	for _, entry := range from {
		if _, ok := present[entry.Type]; !ok {
			out = append(out, entry.Type)
		}
	}

	slices.Sort(out)

	return out
}

func renderCatalogue(entries []event.TypeDoc) ([]byte, error) {
	encoded, err := json.MarshalIndent(catalogueFile{
		Comment:    generatedNotice,
		Vocabulary: event.VocabularyName,
		Version:    event.VocabularyVersion,
		Format:     event.VocabularyFormat,
		Namespace:  event.CoreNamespace,
		Entries:    entries,
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode the event-type set: %w", err)
	}

	return append(encoded, '\n'), nil
}

// compareFile fails with errStale when the committed file differs from want.
func compareFile(path string, want []byte) error {
	return codegen.CompareFile(path, want, errStale, "generate-event-types")
}
