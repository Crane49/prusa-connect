package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/codegen"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
)

// The errors of the checks. Every check fails the command. None only warns.
var (
	// errUndescribed reports a row with an empty cell: no model, no display
	// name, no vendor, no icon or no connector.
	errUndescribed = errors.New("a device model is undescribed")
	// errEntriesMissing reports a model that the catalogue has and the
	// committed file does not.
	errEntriesMissing = errors.New("device models are missing from the committed artefact")
	// errEntriesUnknown reports a model that the committed file has and the
	// catalogue does not.
	errEntriesUnknown = errors.New("the committed artefact names device models the catalogue does not")
	// errStale reports a committed file that differs from the generated one.
	errStale = errors.New("committed artefact differs from the model catalogue")
)

// catalogueFile is the JSON file this command writes. It has the version and
// the format of the vocabulary as members, so a program that reads the file
// can tell which release of the vocabulary it has.
//
// The file does not list the printer drivers, which the API response does.
// The drivers in a binary depend on the modules it was built with, and this
// file is the same for every build.
type catalogueFile struct {
	Comment    string             `json:"$comment"`
	Vocabulary string             `json:"vocabulary"`
	Version    string             `json:"version"`
	Format     string             `json:"format"`
	Entries    []profile.ModelDoc `json:"entries"`
}

// checkDescribed fails on a row with an empty cell. It checks the display name
// and the icon as well as the model, because a web page needs both to show
// the row.
func checkDescribed(entries []profile.ModelDoc) error {
	var offences []string

	for _, entry := range entries {
		name := entry.Model

		switch {
		case strings.TrimSpace(name) == "":
			offences = append(offences, "a row carries no model")
		case strings.TrimSpace(entry.DisplayName) == "":
			offences = append(offences, name+" has no display name")
		case strings.TrimSpace(entry.Vendor) == "":
			offences = append(offences, name+" names no vendor")
		case strings.TrimSpace(entry.Icon) == "":
			offences = append(offences, name+" has no icon key")
		case strings.TrimSpace(entry.Connector) == "":
			offences = append(offences, name+" names no connector")
		}
	}

	if len(offences) == 0 {
		return nil
	}

	slices.Sort(offences)

	return fmt.Errorf("%w: %s", errUndescribed, strings.Join(offences, "; "))
}

// checkDrift compares the models of the catalogue with the models of the
// committed file. A model missing from the file fails with -check, and
// writing the file fixes it. A model that is only in the file fails with and
// without -check, unless the flag -allow-removals is set: a client may still
// look up the model of a device record in the published file.
//
// When there is no committed file, there is nothing to compare, and only the
// errStale check applies.
func checkDrift(committed *catalogueFile, current []profile.ModelDoc, allowRemovals, writing bool) error {
	if committed == nil {
		return nil
	}

	missing := namesNotIn(current, committed.Entries)
	unknown := namesNotIn(committed.Entries, current)

	var problems []error

	if len(missing) > 0 && !writing {
		problems = append(problems, fmt.Errorf(
			"%w: %s. Run make generate-device-models and commit the result",
			errEntriesMissing, strings.Join(missing, ", ")))
	}

	if len(unknown) > 0 && !allowRemovals {
		problems = append(problems, fmt.Errorf(
			"%w: %s. Restore the row, or pass -allow-removals if this is a retirement an earlier "+
				"release deprecated",
			errEntriesUnknown, strings.Join(unknown, ", ")))
	}

	return errors.Join(problems...)
}

// namesNotIn returns the models named by from and not by in, sorted.
func namesNotIn(from, in []profile.ModelDoc) []string {
	present := make(map[string]struct{}, len(in))
	for _, entry := range in {
		present[entry.Model] = struct{}{}
	}

	var out []string

	for _, entry := range from {
		if _, ok := present[entry.Model]; !ok {
			out = append(out, entry.Model)
		}
	}

	slices.Sort(out)

	return out
}

func renderCatalogue(entries []profile.ModelDoc) ([]byte, error) {
	encoded, err := json.MarshalIndent(catalogueFile{
		Comment:    generatedNotice,
		Vocabulary: profile.VocabularyName,
		Version:    profile.VocabularyVersion,
		Format:     profile.VocabularyFormat,
		Entries:    entries,
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode the device-model set: %w", err)
	}

	return append(encoded, '\n'), nil
}

// compareFile fails with errStale when the committed file differs from the
// generated one, and names the make target that writes it again.
func compareFile(path string, want []byte) error {
	return codegen.CompareFile(path, want, errStale, "generate-device-models")
}
