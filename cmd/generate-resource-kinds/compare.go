package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/codegen"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

var (
	// errUndescribed means a kind has no summary, description or version.
	errUndescribed = errors.New("a resource kind is undescribed")
	// errEntriesMissing means the committed file lacks a declared kind.
	errEntriesMissing = errors.New("resource kinds are missing from the committed artefact")
	// errEntriesUnknown means the committed file names an undeclared kind.
	errEntriesUnknown = errors.New("the committed artefact names resource kinds nothing declares")
	// errStale means a committed file differs from the generated one.
	errStale = errors.New("committed artefact differs from the descriptor table")
	// errRuntimeUndescribed means a runtime has no summary, description or
	// version.
	errRuntimeUndescribed = errors.New("a resource runtime is undescribed")
	// errRuntimesMissing means the committed file lacks a declared runtime.
	errRuntimesMissing = errors.New("resource runtimes are missing from the committed artefact")
	// errRuntimesUnknown means the committed file names an undeclared runtime.
	errRuntimesUnknown = errors.New("the committed artefact names resource runtimes nothing declares")
	// errRuntimeUnknownOnKind means a kind offers a runtime outside the set.
	errRuntimeUnknownOnKind = errors.New("a resource kind offers a runtime nothing declares")
)

// catalogueFile is the JSON file. It includes the vocabulary's content and
// format versions so a consumer can tell which set it reads.
type catalogueFile struct {
	Comment    string        `json:"$comment"`
	Vocabulary string        `json:"vocabulary"`
	Version    string        `json:"version"`
	Format     string        `json:"format"`
	Entries    []ref.KindDoc `json:"entries"`
	// Runtimes describes each runtime a kind's entry may offer.
	Runtimes []ref.RuntimeDoc `json:"runtimes"`
}

// checkDescribed refuses a kind with no summary, description or version.
func checkDescribed(entries []ref.KindDoc) error {
	var offences []string

	for _, entry := range entries {
		switch {
		case strings.TrimSpace(entry.Summary) == "":
			offences = append(offences, entry.Kind.String()+" has no summary")
		case strings.TrimSpace(entry.Description) == "":
			offences = append(offences, entry.Kind.String()+" has no description")
		case entry.Version == "":
			offences = append(offences, entry.Kind.String()+" carries no vocabulary version")
		}
	}

	if len(offences) == 0 {
		return nil
	}

	slices.Sort(offences)

	return fmt.Errorf("%w: %s", errUndescribed, strings.Join(offences, "; "))
}

// checkRuntimesDescribed refuses an undescribed runtime and a kind that
// offers a runtime outside the set, which the loader would refuse.
func checkRuntimesDescribed(entries []ref.KindDoc, runtimes []ref.RuntimeDoc) error {
	var offences []string

	for _, entry := range runtimes {
		switch {
		case strings.TrimSpace(entry.Summary) == "":
			offences = append(offences, entry.Runtime.String()+" has no summary")
		case strings.TrimSpace(entry.Description) == "":
			offences = append(offences, entry.Runtime.String()+" has no description")
		case entry.Version == "":
			offences = append(offences, entry.Runtime.String()+" carries no vocabulary version")
		}
	}

	var unknown []string

	for _, entry := range entries {
		for _, offered := range entry.Runtimes {
			if !offered.Valid() {
				unknown = append(unknown, entry.Kind.String()+" offers "+offered.String())
			}
		}
	}

	var problems []error

	if len(offences) > 0 {
		slices.Sort(offences)
		problems = append(problems, fmt.Errorf("%w: %s", errRuntimeUndescribed, strings.Join(offences, "; ")))
	}

	if len(unknown) > 0 {
		slices.Sort(unknown)
		problems = append(problems,
			fmt.Errorf("%w: %s", errRuntimeUnknownOnKind, strings.Join(unknown, "; ")))
	}

	return errors.Join(problems...)
}

// checkRuntimeDrift is checkDrift for the runtime set.
func checkRuntimeDrift(committed *catalogueFile, current []ref.RuntimeDoc, allowRemovals bool) error {
	if committed == nil {
		return nil
	}

	missing := runtimesNotIn(current, committed.Runtimes)
	unknown := runtimesNotIn(committed.Runtimes, current)

	var problems []error

	if len(missing) > 0 {
		problems = append(problems, fmt.Errorf(
			"%w: %s. Run make generate-resource-kinds and commit the result",
			errRuntimesMissing, strings.Join(missing, ", ")))
	}

	if len(unknown) > 0 && !allowRemovals {
		problems = append(problems, fmt.Errorf(
			"%w: %s. Restore the descriptor, or pass -allow-removals if this is a deliberate "+
				"retirement in a breaking release",
			errRuntimesUnknown, strings.Join(unknown, ", ")))
	}

	return errors.Join(problems...)
}

// runtimesNotIn returns the runtimes named by from and not by in, sorted.
func runtimesNotIn(from, in []ref.RuntimeDoc) []string {
	present := make(map[ref.Runtime]struct{}, len(in))
	for _, entry := range in {
		present[entry.Runtime] = struct{}{}
	}

	var out []string

	for _, entry := range from {
		if _, ok := present[entry.Runtime]; !ok {
			out = append(out, entry.Runtime.String())
		}
	}

	slices.Sort(out)

	return out
}

// checkDrift compares the committed kinds with the declared ones. A missing
// row always fails. An extra row fails unless allowRemovals is set.
func checkDrift(committed *catalogueFile, current []ref.KindDoc, allowRemovals bool) error {
	if committed == nil {
		return nil
	}

	missing := namesNotIn(current, committed.Entries)
	unknown := namesNotIn(committed.Entries, current)

	var problems []error

	if len(missing) > 0 {
		problems = append(problems, fmt.Errorf(
			"%w: %s. Run make generate-resource-kinds and commit the result",
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

// checkRetired refuses a committed kind that nothing declares. Regenerating
// would drop it silently while a configuration or a client still names it.
func checkRetired(committed *catalogueFile, current []ref.KindDoc, allowRemovals bool) error {
	if committed == nil || allowRemovals {
		return nil
	}

	unknown := namesNotIn(committed.Entries, current)
	if len(unknown) == 0 {
		return nil
	}

	return fmt.Errorf(
		"%w: %s. Restore the descriptor, or pass -allow-removals if this is a deliberate "+
			"retirement in a breaking release",
		errEntriesUnknown, strings.Join(unknown, ", "))
}

// namesNotIn returns the kinds named by from and not by in, sorted.
func namesNotIn(from, in []ref.KindDoc) []string {
	present := make(map[ref.Kind]struct{}, len(in))
	for _, entry := range in {
		present[entry.Kind] = struct{}{}
	}

	var out []string

	for _, entry := range from {
		if _, ok := present[entry.Kind]; !ok {
			out = append(out, entry.Kind.String())
		}
	}

	slices.Sort(out)

	return out
}

func renderCatalogue(entries []ref.KindDoc) ([]byte, error) {
	encoded, err := json.MarshalIndent(catalogueFile{
		Comment:    generatedNotice,
		Vocabulary: ref.VocabularyName,
		Version:    ref.VocabularyVersion,
		Format:     ref.VocabularyFormat,
		Entries:    entries,
		Runtimes:   ref.RuntimeDocs(),
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode the resource-kind set: %w", err)
	}

	return append(encoded, '\n'), nil
}

func compareFile(path string, want []byte) error {
	return codegen.CompareFile(path, want, errStale, "generate-resource-kinds")
}
