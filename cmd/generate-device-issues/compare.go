package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/codegen"
	"github.com/prusa3d/prusa-connect/internal/kernel/deviceissue"
)

// The errors of the checks. Each one fails the run.
var (
	// errUndescribed reports a reason with an empty summary, description,
	// action, severity or version.
	errUndescribed = errors.New("a device-issue reason is undescribed")
	// errEntriesMissing reports a reason the code declares that the committed
	// artefact lacks.
	errEntriesMissing = errors.New("device-issue reasons are missing from the committed artefact")
	// errEntriesUnknown reports a reason in the committed artefact that the
	// code does not declare.
	errEntriesUnknown = errors.New("the committed artefact names device-issue reasons nothing declares")
	// errStale reports a committed artefact that differs from the generated
	// one.
	errStale = errors.New("committed artefact differs from the descriptor table")
)

// catalogueFile is the machine-readable artefact. It includes the
// vocabulary's version and format and the severities in order.
type catalogueFile struct {
	Comment    string                 `json:"$comment"`
	Vocabulary string                 `json:"vocabulary"`
	Version    string                 `json:"version"`
	Format     string                 `json:"format"`
	Severities []deviceissue.Severity `json:"severities"`
	Entries    []deviceissue.Doc      `json:"entries"`
}

// checkDescribed fails when a reason would publish an empty cell.
func checkDescribed(entries []deviceissue.Doc) error {
	var offences []string

	for _, entry := range entries {
		name := entry.Reason.String()

		switch {
		case strings.TrimSpace(name) == "":
			offences = append(offences, "an entry carries no reason")
		case strings.TrimSpace(entry.Summary) == "":
			offences = append(offences, name+" has no summary")
		case strings.TrimSpace(entry.Description) == "":
			offences = append(offences, name+" has no description")
		case strings.TrimSpace(entry.Action) == "":
			offences = append(offences, name+" says nothing about what to do")
		case !entry.Severity.Valid():
			offences = append(offences, name+" carries no severity")
		case entry.Version == "":
			offences = append(offences, name+" carries no vocabulary version")
		}
	}

	if len(offences) == 0 {
		return nil
	}

	slices.Sort(offences)

	return fmt.Errorf("%w: %s", errUndescribed, strings.Join(offences, "; "))
}

// checkDrift compares the declared reasons with the committed artefact. A
// reason missing from the artefact fails only under -check, because a write
// fixes it. A reason only in the artefact always fails unless -allow-removals
// is set, because a client may still use it. With no committed file there is
// nothing to compare.
func checkDrift(
	committed *catalogueFile, current []deviceissue.Doc, allowRemovals, writing bool,
) error {
	if committed == nil {
		return nil
	}

	missing := namesNotIn(current, committed.Entries)
	unknown := namesNotIn(committed.Entries, current)

	var problems []error

	if len(missing) > 0 && !writing {
		problems = append(problems, fmt.Errorf(
			"%w: %s. Run make generate-device-issues and commit the result",
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

// namesNotIn returns the reasons named by from and not by in, sorted.
func namesNotIn(from, in []deviceissue.Doc) []string {
	present := make(map[deviceissue.Reason]struct{}, len(in))
	for _, entry := range in {
		present[entry.Reason] = struct{}{}
	}

	var out []string

	for _, entry := range from {
		if _, ok := present[entry.Reason]; !ok {
			out = append(out, entry.Reason.String())
		}
	}

	slices.Sort(out)

	return out
}

func renderCatalogue(entries []deviceissue.Doc) ([]byte, error) {
	encoded, err := json.MarshalIndent(catalogueFile{
		Comment:    generatedNotice,
		Vocabulary: deviceissue.VocabularyName,
		Version:    deviceissue.VocabularyVersion,
		Format:     deviceissue.VocabularyFormat,
		Severities: deviceissue.Severities(),
		Entries:    entries,
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode the device-issue set: %w", err)
	}

	return append(encoded, '\n'), nil
}

// compareFile fails with errStale when the committed file differs from want.
func compareFile(path string, want []byte) error {
	return codegen.CompareFile(path, want, errStale, "generate-device-issues")
}
