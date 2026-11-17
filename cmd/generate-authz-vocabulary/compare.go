package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/codegen"
)

// The errors of the checks. Each one fails the run.
var (
	// errUndescribed reports an entry with no summary, description or version.
	errUndescribed = errors.New("an authorization entry is undescribed")
	// errEntriesMissing reports a name the code declares that the committed
	// artefact lacks.
	errEntriesMissing = errors.New("authorization entries are missing from the committed artefact")
	// errEntriesUnknown reports a row in the committed artefact that the code
	// does not declare.
	errEntriesUnknown = errors.New("the committed artefact names authorization entries nothing declares")
	// errStale reports a committed artefact that differs from the generated
	// one.
	errStale = errors.New("committed artefact differs from the closed sets")
)

// catalogueFile is the machine-readable artefact. It includes the
// vocabulary's version and format, so a consumer knows which set it reads.
type catalogueFile struct {
	Comment    string              `json:"$comment"`
	Vocabulary string              `json:"vocabulary"`
	Version    string              `json:"version"`
	Format     string              `json:"format"`
	Actions    []authz.ActionDoc   `json:"actions"`
	Types      []authz.TypeDoc     `json:"types"`
	Relations  []authz.RelationDoc `json:"relations"`
	Shapes     []authz.ShapeDoc    `json:"shapes"`
}

// checkDescribed fails when an entry has no summary, description or version.
func checkDescribed(set vocabulary) error {
	var offences []string

	for _, entry := range set.Actions {
		offences = append(offences, described("action "+entry.Action.String(),
			entry.Summary, entry.Description, entry.Version)...)
	}

	for _, entry := range set.Types {
		offences = append(offences, described("type "+entry.Type.String(),
			entry.Summary, entry.Description, entry.Version)...)
	}

	for _, entry := range set.Relations {
		offences = append(offences, described("relation "+entry.Relation.String(),
			entry.Summary, entry.Description, entry.Version)...)
	}

	if len(offences) == 0 {
		return nil
	}

	slices.Sort(offences)

	return fmt.Errorf("%w: %s", errUndescribed, strings.Join(offences, "; "))
}

func described(what, summary, description, version string) []string {
	var out []string

	if strings.TrimSpace(summary) == "" {
		out = append(out, what+" has no summary")
	}

	if strings.TrimSpace(description) == "" {
		out = append(out, what+" has no description")
	}

	if version == "" {
		out = append(out, what+" carries no vocabulary version")
	}

	return out
}

// vocabularySide is the names of one of the four sets, in the committed
// artefact and in the code.
type vocabularySide struct {
	what      string
	committed []string
	current   []string
}

// sides returns the four sets, so checkMissing and checkUnknown compare the
// same names.
func sides(committed *catalogueFile, set vocabulary) []vocabularySide {
	return []vocabularySide{
		{"action", actionNames(committed.Actions), actionNames(set.Actions)},
		{"type", typeNames(committed.Types), typeNames(set.Types)},
		{"relation", relationNames(committed.Relations), relationNames(set.Relations)},
		{"shape", shapeNames(committed.Shapes), shapeNames(set.Shapes)},
	}
}

// checkMissing fails when the code declares a name the committed artefact
// lacks. It runs only under -check, because a write fixes that state.
func checkMissing(committed *catalogueFile, set vocabulary) error {
	if committed == nil {
		return nil
	}

	var missing []string

	for _, side := range sides(committed, set) {
		missing = append(missing, label(side.what, notIn(side.current, side.committed))...)
	}

	if len(missing) == 0 {
		return nil
	}

	return fmt.Errorf("%w: %s. Run make generate-authz-vocabulary and commit the result",
		errEntriesMissing, strings.Join(missing, ", "))
}

// checkUnknown fails when the committed artefact has a name the code does not
// declare. It also runs on a write: a grant in a database may still name the
// entry, so removing one needs -allow-removals.
func checkUnknown(committed *catalogueFile, set vocabulary, allowRemovals bool) error {
	if committed == nil || allowRemovals {
		return nil
	}

	var unknown []string

	for _, side := range sides(committed, set) {
		unknown = append(unknown, label(side.what, notIn(side.committed, side.current))...)
	}

	if len(unknown) == 0 {
		return nil
	}

	return fmt.Errorf(
		"%w: %s. Restore the declaration, or pass -allow-removals if this is a deliberate "+
			"retirement in a breaking release",
		errEntriesUnknown, strings.Join(unknown, ", "))
}

func label(what string, names []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, what+" "+name)
	}

	return out
}

// notIn returns the names in from and not in in, sorted.
func notIn(from, in []string) []string {
	var out []string

	for _, name := range from {
		if !slices.Contains(in, name) {
			out = append(out, name)
		}
	}

	slices.Sort(out)

	return out
}

func actionNames(entries []authz.ActionDoc) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Action.String())
	}

	return out
}

func typeNames(entries []authz.TypeDoc) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Type.String())
	}

	return out
}

func relationNames(entries []authz.RelationDoc) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Relation.String())
	}

	return out
}

func shapeNames(entries []authz.ShapeDoc) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, shapeString(entry))
	}

	return out
}

// shapeString returns the shape as object#relation@subject.
func shapeString(s authz.ShapeDoc) string {
	subject := s.SubjectType.String()
	if s.SubjectRelation != "" {
		subject += "#" + s.SubjectRelation.String()
	}

	return s.ObjectType.String() + "#" + s.Relation.String() + "@" + subject
}

func renderCatalogue(set vocabulary) ([]byte, error) {
	encoded, err := json.MarshalIndent(catalogueFile{
		Comment:    generatedNotice,
		Vocabulary: authz.VocabularyName,
		Version:    authz.VocabularyVersion,
		Format:     authz.VocabularyFormat,
		Actions:    set.Actions,
		Types:      set.Types,
		Relations:  set.Relations,
		Shapes:     set.Shapes,
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode the authorization vocabulary: %w", err)
	}

	return append(encoded, '\n'), nil
}

// compareFile fails with errStale when the committed file differs from want.
func compareFile(path string, want []byte) error {
	return codegen.CompareFile(path, want, errStale, "generate-authz-vocabulary")
}
