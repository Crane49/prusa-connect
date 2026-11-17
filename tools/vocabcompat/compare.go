package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
)

// The bump a difference demands of the number that publishes it. They order, so
// the demand over a whole vocabulary is the largest of its differences.
const (
	bumpNone = iota
	bumpMinor
	bumpMajor
)

// bumpNames renders a demand for a person reading the refusal.
var bumpNames = map[int]string{bumpNone: "nothing", bumpMinor: "a minor", bumpMajor: "a major"}

// deprecatedField is the member an entry carries while it is on its way out. It
// is sdk/vocabulary.Deprecation on the Go side, and the same three letters in
// every artefact, so this gate reads one shape for every set.
const deprecatedField = "deprecated"

// setShape is one list inside a vocabulary artefact, reduced to what a consumer
// can branch on: which entries are in the set, and which fields each entry
// carries. The prose is left out, because prose is neutral under ADR-20260825-zkq rule
// 6 and a version that moved for a reworded sentence is still a version that
// moved.
type setShape struct {
	// Key names the fields that identify an entry. A list of plain strings
	// carries none and the string is its own identity. A set the baseline gives
	// no key is refused rather than guessed at, because an identity guessed
	// wrong reports every entry as removed and added at once.
	Key []string `json:"key"`
	// Fields is the union of the field names the entries carry. The deprecation
	// marker is not one of them: it is compared as a deprecation below, so
	// announcing one is not read as a reshaped artefact.
	Fields []string `json:"fields"`
	// Entries is every entry's identity, sorted.
	Entries []string `json:"entries"`
	// Deprecated is the release each deprecated entry was marked in, by the
	// entry's identity. An entry nothing deprecated is absent. A removal is
	// allowed only for an entry this map already carried when the baseline was
	// accepted, which is what makes the announcement come first.
	Deprecated map[string]string `json:"deprecated,omitempty"`
}

// surface is one vocabulary as this gate compares it.
type surface struct {
	// Version is the content version, which moves when the set moves.
	Version string `json:"version"`
	// Format is the artefact's own shape version, MAJOR.MINOR, which moves when
	// the JSON a consumer parses changes shape.
	Format string `json:"format"`
	// Envelope is the scalar keys beside the sets, such as the namespace the
	// event types are published under.
	Envelope []string `json:"envelope"`
	// Sets is every list in the artefact, by its key in the JSON object.
	Sets map[string]setShape `json:"sets"`
}

// baseline is the surface the tree published when it was last accepted, plus
// the vocabularies that ship with no version at all.
type baseline struct {
	Comment string `json:"$comment"`
	Schema  int    `json:"schema"`
	Taken   string `json:"taken"`
	// Unversioned is the debt ADR-20260827-qvk rule 2 allows today and nothing may add
	// to. A vocabulary is removed from this list when it gains a version, and a
	// new vocabulary that would need a line here is refused instead.
	//
	// One kind of entry here is not debt. A per-install set has no content
	// version to carry, because its entries are what an operator installed and
	// not what the build ships. ADR-20260831-nbj rule 22 gives it no artefact and
	// ADR-20260827-qvk rule 1 leaves its version unmoved by a declaration. The device
	// states are the one today, and its `why` says so. Nothing else may be
	// added.
	Unversioned []unversioned `json:"unversioned"`
	// Vocabularies is every artefact under docs/vocabularies, by its name.
	Vocabularies map[string]surface `json:"vocabularies"`
}

// unversioned is one vocabulary that publishes an empty version.
type unversioned struct {
	Vocabulary string `json:"vocabulary"`
	Why        string `json:"why"`
}

// finding is one difference between the baseline and the artefact, with the
// bump it demands of the version that publishes it.
type finding struct {
	Vocabulary string
	Kind       string
	Detail     string
	// Content is the bump this difference demands of the content version.
	Content int
	// Format is the bump it demands of the format version.
	Format int
	// Breaking marks a difference a released consumer cannot survive: something
	// left the published surface. A frozen release refuses it whatever the
	// version says.
	Breaking bool
	// Retirement marks an entry that left a published set. It is breaking as
	// well, and a frozen release judges it by the deprecation that announced it
	// rather than refusing it outright, so it is counted separately.
	Retirement bool
	// Since is the release the baseline says this entry was deprecated in, for a
	// retirement that was announced. It is empty for one that was not.
	Since string
}

// readBaseline reads the committed baseline.
func readBaseline(path string) (baseline, error) {
	var out baseline

	data, err := os.ReadFile(path)
	if err != nil {
		return out, fmt.Errorf("read the baseline %s: %w", path, err)
	}

	if err = json.Unmarshal(data, &out); err != nil {
		return out, fmt.Errorf("parse the baseline %s: %w", path, err)
	}

	if len(out.Vocabularies) == 0 {
		return out, fmt.Errorf("%s names no vocabulary, so this gate would pass over nothing", path)
	}

	return out, nil
}

// readSurface reduces one artefact to its surface. The keys come from the
// baseline, so a set the baseline does not know is refused by name.
func readSurface(path string, keys map[string][]string) (surface, string, error) {
	out := surface{Sets: map[string]setShape{}}

	data, err := os.ReadFile(path)
	if err != nil {
		return out, "", fmt.Errorf("read the artefact %s: %w", path, err)
	}

	var raw map[string]json.RawMessage
	if err = json.Unmarshal(data, &raw); err != nil {
		return out, "", fmt.Errorf("parse the artefact %s: %w", path, err)
	}

	name := scalar(raw["vocabulary"])
	if name == "" {
		return out, "", fmt.Errorf("%s carries no \"vocabulary\" name", path)
	}

	out.Version = scalar(raw["version"])
	out.Format = scalar(raw["format"])

	for key, value := range raw {
		if key == "$comment" || key == "version" || key == "format" {
			continue
		}

		var list []json.RawMessage
		if listErr := json.Unmarshal(value, &list); listErr != nil {
			out.Envelope = append(out.Envelope, key)

			continue
		}

		shape, setErr := readSet(path, key, list, keys[key])
		if setErr != nil {
			return out, "", setErr
		}

		out.Sets[key] = shape
	}

	sort.Strings(out.Envelope)

	return out, name, nil
}

// readSet reduces one list to its identities and its field names.
func readSet(path, name string, list []json.RawMessage, key []string) (setShape, error) {
	shape := setShape{Key: key, Fields: []string{}, Entries: []string{}}
	fields := map[string]bool{}

	for index, item := range list {
		var text string
		if err := json.Unmarshal(item, &text); err == nil {
			shape.Entries = append(shape.Entries, text)

			continue
		}

		var entry map[string]json.RawMessage
		if err := json.Unmarshal(item, &entry); err != nil {
			return shape, fmt.Errorf("%s: %s[%d] is neither a string nor an object", path, name, index)
		}

		for field := range entry {
			if field == deprecatedField {
				continue
			}

			fields[field] = true
		}

		if len(key) == 0 {
			return shape, fmt.Errorf(
				"%s: the set %q holds objects and the baseline gives it no key.\n"+
					"Add \"key\" to that set in the baseline, naming the fields that identify an entry. "+
					"An identity this gate guessed would report every entry as removed and added at once",
				path, name)
		}

		identity, err := identify(path, name, index, entry, key)
		if err != nil {
			return shape, err
		}

		shape.Entries = append(shape.Entries, identity)

		if err = shape.readDeprecation(path, name, identity, entry); err != nil {
			return shape, err
		}
	}

	for field := range fields {
		shape.Fields = append(shape.Fields, field)
	}

	sort.Strings(shape.Fields)
	sort.Strings(shape.Entries)

	return shape, nil
}

// readDeprecation records one entry's marker on the shape, and records nothing
// for an entry that carries none.
func (s *setShape) readDeprecation(
	path, name, identity string, entry map[string]json.RawMessage,
) error {
	since, marked, err := deprecatedSince(path, name, identity, entry)
	if err != nil || !marked {
		return err
	}

	if s.Deprecated == nil {
		s.Deprecated = map[string]string{}
	}

	s.Deprecated[identity] = since

	return nil
}

// deprecatedSince reads one entry's deprecation marker. It reports the release
// the marker names, and false for an entry that carries none.
//
// A marker naming no release is refused rather than ignored. The window is
// counted from that release, so a marker without one announces a removal no
// gate can time.
func deprecatedSince(
	path, name, identity string, entry map[string]json.RawMessage,
) (string, bool, error) {
	raw, marked := entry[deprecatedField]
	if !marked || string(raw) == "null" {
		return "", false, nil
	}

	var marker struct {
		Since string `json:"since"`
	}

	if err := json.Unmarshal(raw, &marker); err != nil {
		return "", false, fmt.Errorf("%s: %s %s carries a %q that is not an object: %w",
			path, name, identity, deprecatedField, err)
	}

	if strings.TrimSpace(marker.Since) == "" {
		return "", false, fmt.Errorf(
			"%s: %s %s is deprecated and names no release under \"since\".\n"+
				"The window an entry stays published is counted from that release, so a marker "+
				"without one cannot be held to it", path, name, identity)
	}

	return marker.Since, true, nil
}

// identify joins the key fields into one identity.
func identify(path, name string, index int, entry map[string]json.RawMessage, key []string) (string, error) {
	parts := make([]string, 0, len(key))

	for _, field := range key {
		value := scalar(entry[field])
		if value == "" {
			return "", fmt.Errorf("%s: %s[%d] carries no %q, and the baseline names it as the key",
				path, name, index, field)
		}

		parts = append(parts, value)
	}

	return strings.Join(parts, "/"), nil
}

// scalar reads a JSON string, returning an empty string for anything else.
func scalar(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return ""
	}

	return text
}

// differences classifies one vocabulary. Every difference names the bump it
// demands, and the caller takes the largest.
func differences(name string, was, now surface) []finding {
	var found []finding

	add := func(kind, detail string, content, format int, breaking bool) {
		found = append(found, finding{
			Vocabulary: name, Kind: kind, Detail: detail,
			Content: content, Format: format, Breaking: breaking,
		})
	}

	for _, key := range missing(was.Envelope, now.Envelope) {
		add("envelope-removed", key, bumpNone, bumpMajor, true)
	}

	for _, key := range missing(now.Envelope, was.Envelope) {
		add("envelope-added", key, bumpNone, bumpMinor, false)
	}

	for _, set := range sortedKeys(was.Sets) {
		current, held := now.Sets[set]
		if !held {
			add("set-removed", set, bumpMajor, bumpMajor, true)

			continue
		}

		found = append(found, setDifferences(name, set, was.Sets[set], current)...)
	}

	for _, set := range sortedKeys(now.Sets) {
		if _, held := was.Sets[set]; !held {
			add("set-added", set, bumpMinor, bumpMinor, false)
		}
	}

	return found
}

// sortedEntries returns the identities a deprecation map names, in order, so a
// report reads the same twice.
func sortedEntries(marked map[string]string) []string {
	out := make([]string, 0, len(marked))
	for entry := range marked {
		out = append(out, entry)
	}

	sort.Strings(out)

	return out
}

// setDifferences classifies one list inside a vocabulary: what entered it, what
// left it, what was announced as leaving, and how the shape of an entry moved.
func setDifferences(name, set string, previous, current setShape) []finding {
	var found []finding

	add := func(kind, detail string, content, format int, breaking bool) {
		found = append(found, finding{
			Vocabulary: name, Kind: kind, Detail: detail,
			Content: content, Format: format, Breaking: breaking,
		})
	}

	for _, entry := range missing(previous.Entries, current.Entries) {
		found = append(found, finding{
			Vocabulary: name, Kind: "entry-removed", Detail: set + " " + entry,
			Content: bumpMajor, Format: bumpNone, Breaking: true,
			Retirement: true, Since: previous.Deprecated[entry],
		})
	}

	for _, entry := range missing(current.Entries, previous.Entries) {
		add("entry-added", set+" "+entry, bumpMinor, bumpNone, false)
	}

	// A deprecation is an announcement and not a removal, so it moves no
	// version. Taking one back is a promise withdrawn, and the gate refuses it
	// until somebody accepts the baseline on purpose.
	for _, entry := range current.Entries {
		if previous.Deprecated[entry] == "" && current.Deprecated[entry] != "" {
			add("deprecation-added", set+" "+entry+" in "+current.Deprecated[entry],
				bumpNone, bumpNone, false)
		}
	}

	for _, entry := range sortedEntries(previous.Deprecated) {
		if slices.Contains(current.Entries, entry) && current.Deprecated[entry] == "" {
			add("deprecation-withdrawn", set+" "+entry+", announced in "+previous.Deprecated[entry],
				bumpNone, bumpNone, false)
		}
	}

	for _, field := range missing(previous.Fields, current.Fields) {
		add("field-removed", set+"."+field, bumpNone, bumpMajor, true)
	}

	for _, field := range missing(current.Fields, previous.Fields) {
		add("field-added", set+"."+field, bumpNone, bumpMinor, false)
	}

	return found
}

// missing returns every element of first that second does not hold.
func missing(first, second []string) []string {
	held := make(map[string]bool, len(second))
	for _, item := range second {
		held[item] = true
	}

	var out []string

	for _, item := range first {
		if !held[item] {
			out = append(out, item)
		}
	}

	sort.Strings(out)

	return out
}

// sortedKeys returns a map's keys in order, so a report reads the same twice.
func sortedKeys(sets map[string]setShape) []string {
	out := make([]string, 0, len(sets))
	for key := range sets {
		out = append(out, key)
	}

	sort.Strings(out)

	return out
}

// satisfied reports whether the version moved by at least the demanded bump.
func satisfied(was, now version, demand int) bool {
	switch demand {
	case bumpMajor:
		return now.major > was.major
	case bumpMinor:
		return now.major > was.major || (now.major == was.major && now.minor > was.minor)
	default:
		return true
	}
}

// parseFormat reads a format version, which is MAJOR.MINOR and not the triple a
// content version is. The artefacts have carried two numbers since they were
// created, and a format is the artefact's shape rather than its contents.
func parseFormat(where, text string) (version, error) {
	if strings.Count(text, ".") == 1 {
		text += ".0"
	}

	return parseVersion(where, text)
}
