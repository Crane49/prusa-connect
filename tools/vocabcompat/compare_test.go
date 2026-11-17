package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The gate is exercised against the real committed artefacts, one mutation at a
// time, the way tools/apicompat/classify_test.go exercises the API classifier. A
// rule with no mutation is a rule nobody has seen fire.
//
// The first mutation is not invented. fa8f5cc3 took the authorization
// vocabulary from 20 published entries to 22 and left the version at 2.0.0, and
// every gate in the tree passed. TestTheGateRefusesTheMergeThatMovedASetAtAStillVersion
// reconstructs that commit out of the committed document and requires the
// refusal, then moves the version and requires the pass.

// theActionsCa658479Added are the two entries that merge put into the published
// authorization set. Both are in the committed artefact today.
var theActionsCa658479Added = []string{"read_storage", "write_storage"}

func TestTheCommittedVocabulariesAgreeWithTheBaseline(t *testing.T) {
	t.Parallel()

	out, code := runIn(t, "..", "..")

	if code != exitOK {
		t.Fatalf("the committed tree is refused, exit %d:\n%s", code, out)
	}

	// A green that read nothing says nothing. Every vocabulary in the baseline
	// has to appear in the report by name.
	for _, name := range []string{"authorization", "device-issues", "event-types", "resource-kinds"} {
		if !strings.Contains(out, "\n"+name+": version ") {
			t.Errorf("the report never judged %q, so the gate passed over it:\n%s", name, out)
		}
	}
}

func TestTheGateRefusesTheMergeThatMovedASetAtAStillVersion(t *testing.T) {
	t.Parallel()

	root := stage(t)

	// The tree as fa8f5cc3 found it: 20 published actions at version 2.0.0.
	editBaseline(t, root, func(file map[string]any) {
		authorization := vocabulary(t, file, "authorization")
		authorization["version"] = "2.0.0"
		set := authorization["sets"].(map[string]any)["actions"].(map[string]any)
		set["entries"] = without(t, set["entries"], theActionsCa658479Added)
	})

	// The tree as fa8f5cc3 left it: the two actions published, the version still
	// reading 2.0.0 because it is written into the artefact from the Go constant.
	editArtefact(t, root, "authorization", func(file map[string]any) {
		file["version"] = "2.0.0"
	})

	out, code := runIn(t, root)
	if code != exitRefused {
		t.Fatalf("fa8f5cc3's change was accepted, exit %d:\n%s", code, out)
	}

	for _, want := range []string{"entry-added", "read_storage", "write_storage", "demands a minor bump"} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal never mentions %q:\n%s", want, out)
		}
	}

	// The same change with the version moved is what the commit should have been.
	editArtefact(t, root, "authorization", func(file map[string]any) {
		file["version"] = "2.1.0"
	})

	out, code = runIn(t, root)
	if code != exitOK {
		t.Fatalf("the same change at 2.1.0 is still refused, exit %d:\n%s", code, out)
	}
}

func TestARetiredEntryDemandsAMajor(t *testing.T) {
	t.Parallel()

	root := stage(t)

	editArtefact(t, root, "event-types", func(file map[string]any) {
		file["entries"] = file["entries"].([]any)[1:]
		file["version"] = eventTypeVersion(t, root)
	})

	out, code := runIn(t, root)
	if code != exitRefused {
		t.Fatalf("an entry left the set on a minor bump, exit %d:\n%s", code, out)
	}

	if !strings.Contains(out, "entry-removed") || !strings.Contains(out, "demands a major bump") {
		t.Errorf("the refusal does not name the retirement:\n%s", out)
	}

	editArtefact(t, root, "event-types", func(file map[string]any) {
		file["version"] = nextEventTypeMajor(t, root)
	})

	if again, rerun := runIn(t, root); rerun != exitOK {
		t.Fatalf("the same retirement on a major bump is refused, exit %d:\n%s", rerun, again)
	}
}

func TestAFrozenReleaseRefusesARetirementAtAnyVersion(t *testing.T) {
	t.Parallel()

	root := stage(t)

	writeFile(t, filepath.Join(root, "VERSION"), []byte("1.0.0\n"))

	editArtefact(t, root, "event-types", func(file map[string]any) {
		file["entries"] = file["entries"].([]any)[1:]
		file["version"] = nextEventTypeMajor(t, root)
	})

	out, code := runIn(t, root)
	if code != exitRefused {
		t.Fatalf("a frozen release let an entry leave the set, exit %d:\n%s", code, out)
	}

	if !strings.Contains(out, "at or above 1.0.0") {
		t.Errorf("the refusal does not name the freeze:\n%s", out)
	}

	// The floor is what decides it, and nothing else moved.
	writeFile(t, filepath.Join(root, "VERSION"), []byte("0.9.9\n"))

	if again, rerun := runIn(t, root); rerun != exitOK {
		t.Fatalf("the same retirement below the floor is refused, exit %d:\n%s", rerun, again)
	}
}

// The four cases below drive the deprecation process a published set retires an
// entry through: it is announced first, it stays for the window, and the
// announcement is not taken back by regenerating.

func TestADeprecatedEntryRetiresOnceTheWindowHasPassed(t *testing.T) {
	t.Parallel()

	root := stage(t)

	leaving := firstEventType(t, root)

	writeFile(t, filepath.Join(root, "VERSION"), []byte("1.4.0\n"))
	deprecateInBaseline(t, root, leaving, "1.0.0")
	retireTheFirstEventType(t, root)

	out, code := runIn(t, root)
	if code != exitOK {
		t.Fatalf("an entry deprecated in 1.0.0 could not be removed in 1.4.0, exit %d:\n%s", code, out)
	}
}

func TestAnEntryNobodyDeprecatedDoesNotRetire(t *testing.T) {
	t.Parallel()

	root := stage(t)

	writeFile(t, filepath.Join(root, "VERSION"), []byte("1.4.0\n"))
	retireTheFirstEventType(t, root)

	out, code := runIn(t, root)
	if code != exitRefused {
		t.Fatalf("an unannounced entry left a frozen set, exit %d:\n%s", code, out)
	}

	if !strings.Contains(out, "left the published set with no deprecation") {
		t.Errorf("the refusal does not say the announcement is missing:\n%s", out)
	}
}

func TestAnEntryDeprecatedTooRecentlyDoesNotRetire(t *testing.T) {
	t.Parallel()

	root := stage(t)

	leaving := firstEventType(t, root)

	writeFile(t, filepath.Join(root, "VERSION"), []byte("1.2.0\n"))
	deprecateInBaseline(t, root, leaving, "1.0.0")
	retireTheFirstEventType(t, root)

	out, code := runIn(t, root)
	if code != exitRefused {
		t.Fatalf("an entry left two minors after its deprecation, exit %d:\n%s", code, out)
	}

	for _, want := range []string{"was deprecated in 1.0.0", "may be removed from 1.3.0 onwards"} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal never says %q:\n%s", want, out)
		}
	}
}

func TestADeprecationIsNotWithdrawnByRegenerating(t *testing.T) {
	t.Parallel()

	root := stage(t)

	deprecateInBaseline(t, root, firstEventType(t, root), "1.0.0")

	out, code := runIn(t, root)
	if code != exitRefused {
		t.Fatalf("a published deprecation disappeared, exit %d:\n%s", code, out)
	}

	for _, want := range []string{"deprecation-withdrawn", "is still published"} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal never says %q:\n%s", want, out)
		}
	}
}

func TestAMarkerNamingNoReleaseIsRefusedRatherThanIgnored(t *testing.T) {
	t.Parallel()

	root := stage(t)

	editArtefact(t, root, "event-types", func(file map[string]any) {
		file["entries"].([]any)[0].(map[string]any)["deprecated"] = map[string]any{"replaced_by": "something"}
	})

	out, code := runIn(t, root)
	if code != exitUsage {
		t.Fatalf("a marker with no release was read as an announcement, exit %d:\n%s", code, out)
	}

	if !strings.Contains(out, "names no release") {
		t.Errorf("the refusal does not ask for the release:\n%s", out)
	}
}

// eventTypeVersion is the content version the accepted baseline holds the
// event types at, and nextEventTypeMajor is the bump a retirement demands of
// it. Both are read out of the baseline rather than written here: a literal
// goes stale the next time the published set moves, and these cases would then
// be refused for the version instead of for the mutation they are testing.
func eventTypeVersion(t *testing.T, root string) string {
	t.Helper()

	var file map[string]any
	if err := json.Unmarshal(readFile(t, filepath.Join(root, defaultBaseline)), &file); err != nil {
		t.Fatalf("parse the baseline: %v", err)
	}

	version, held := vocabulary(t, file, "event-types")["version"].(string)
	if !held {
		t.Fatal("the baseline gives the event types no version, so these cases have nothing to move")
	}

	return version
}

func nextEventTypeMajor(t *testing.T, root string) string {
	t.Helper()

	version := eventTypeVersion(t, root)

	major, _, found := strings.Cut(version, ".")

	number, err := strconv.Atoi(major)
	if !found || err != nil {
		t.Fatalf("the baseline's event-type version reads %q, and a version is three numbers", version)
	}

	return strconv.Itoa(number+1) + ".0.0"
}

// firstEventType is the identity of the entry the two helpers below deprecate
// and retire. It is read out of the artefact rather than written here, so a
// reordered table does not turn these cases into no mutation at all.
func firstEventType(t *testing.T, root string) string {
	t.Helper()

	path := filepath.Join(root, defaultArtefacts, "event-types.json")

	var file map[string]any
	if err := json.Unmarshal(readFile(t, path), &file); err != nil {
		t.Fatalf("parse the event-type artefact: %v", err)
	}

	entry, held := file["entries"].([]any)[0].(map[string]any)["type"].(string)
	if !held {
		t.Fatal("the first event type carries no type, so these cases have no identity to use")
	}

	return entry
}

// deprecateInBaseline marks one entry as announced in the accepted baseline,
// which is where the gate reads an earlier release's announcement from.
func deprecateInBaseline(t *testing.T, root, entry, since string) {
	t.Helper()

	editBaseline(t, root, func(file map[string]any) {
		set := vocabulary(t, file, "event-types")["sets"].(map[string]any)["entries"].(map[string]any)
		set["deprecated"] = map[string]any{entry: since}
	})
}

// retireTheFirstEventType removes the entry firstEventType names, on the major
// bump a retirement demands.
func retireTheFirstEventType(t *testing.T, root string) {
	t.Helper()

	editArtefact(t, root, "event-types", func(file map[string]any) {
		file["entries"] = file["entries"].([]any)[1:]
		file["version"] = nextEventTypeMajor(t, root)
	})
}

func TestAFieldAddedToAnEntryDemandsAFormatMinor(t *testing.T) {
	t.Parallel()

	root := stage(t)

	editArtefact(t, root, "device-issues", func(file map[string]any) {
		file["entries"].([]any)[0].(map[string]any)["retry_after"] = "30s"
	})

	out, code := runIn(t, root)
	if code != exitRefused {
		t.Fatalf("a new field shipped at a still format, exit %d:\n%s", code, out)
	}

	if !strings.Contains(out, "field-added") || !strings.Contains(out, "entries.retry_after") {
		t.Errorf("the refusal does not name the field:\n%s", out)
	}

	editArtefact(t, root, "device-issues", func(file map[string]any) {
		file["format"] = "1.1"
	})

	if again, rerun := runIn(t, root); rerun != exitOK {
		t.Fatalf("the same field at format 1.1 is refused, exit %d:\n%s", rerun, again)
	}
}

func TestAFieldRemovedFromAnEntryDemandsAFormatMajor(t *testing.T) {
	t.Parallel()

	root := stage(t)

	editArtefact(t, root, "resource-kinds", func(file map[string]any) {
		for _, entry := range file["entries"].([]any) {
			delete(entry.(map[string]any), "configurable")
		}

		file["format"] = "1.2"
	})

	out, code := runIn(t, root)
	if code != exitRefused {
		t.Fatalf("a field left the artefact on a format minor, exit %d:\n%s", code, out)
	}

	if !strings.Contains(out, "field-removed") {
		t.Errorf("the refusal does not name the field:\n%s", out)
	}

	editArtefact(t, root, "resource-kinds", func(file map[string]any) {
		file["format"] = "2.0"
	})

	if again, rerun := runIn(t, root); rerun != exitOK {
		t.Fatalf("the same removal at format 2.0 is refused, exit %d:\n%s", rerun, again)
	}
}

func TestAVersionThatGoesBackwardsIsRefused(t *testing.T) {
	t.Parallel()

	root := stage(t)

	editArtefact(t, root, "authorization", func(file map[string]any) {
		file["version"] = "3.1.0"
	})

	out, code := runIn(t, root)
	if code != exitRefused {
		t.Fatalf("a version that went backwards was accepted, exit %d:\n%s", code, out)
	}

	if !strings.Contains(out, "went backwards") {
		t.Errorf("the refusal does not say what happened:\n%s", out)
	}
}

func TestAVocabularyWithNoVersionIsRefused(t *testing.T) {
	t.Parallel()

	root := stage(t)

	editArtefact(t, root, "event-types", func(file map[string]any) {
		file["version"] = ""
	})

	out, code := runIn(t, root)
	if code != exitRefused {
		t.Fatalf("a vocabulary shipped with no version, exit %d:\n%s", code, out)
	}

	if !strings.Contains(out, "ADR-20260825-bhe rule 3") {
		t.Errorf("the refusal does not name the rule:\n%s", out)
	}
}

func TestAnArtefactTheBaselineDoesNotNameIsRefused(t *testing.T) {
	t.Parallel()

	root := stage(t)

	writeFile(t, filepath.Join(root, defaultArtefacts, "job-states.json"),
		[]byte(`{"vocabulary":"job-states","version":"1.0.0","format":"1.0","entries":[]}`))

	out, code := runIn(t, root)
	if code != exitUsage {
		t.Fatalf("a vocabulary with no baseline shipped, exit %d:\n%s", code, out)
	}

	if !strings.Contains(out, "is not in the baseline") {
		t.Errorf("the refusal does not say what is missing:\n%s", out)
	}
}

func TestASetWithNoKeyIsRefusedRatherThanGuessedAt(t *testing.T) {
	t.Parallel()

	root := stage(t)

	editBaseline(t, root, func(file map[string]any) {
		set := vocabulary(t, file, "event-types")["sets"].(map[string]any)["entries"].(map[string]any)
		set["key"] = []any{}
	})

	out, code := runIn(t, root)
	if code != exitUsage {
		t.Fatalf("a set with no identity was compared anyway, exit %d:\n%s", code, out)
	}

	if !strings.Contains(out, "no key") {
		t.Errorf("the refusal does not ask for the key:\n%s", out)
	}
}

func TestAnEmptyBaselineIsAUsageErrorRatherThanAPass(t *testing.T) {
	t.Parallel()

	root := stage(t)

	editBaseline(t, root, func(file map[string]any) {
		file["vocabularies"] = map[string]any{}
	})

	out, code := runIn(t, root)
	if code != exitUsage {
		t.Fatalf("an empty baseline passed, exit %d:\n%s", code, out)
	}

	if !strings.Contains(out, "pass over nothing") {
		t.Errorf("the refusal does not say the gate read nothing:\n%s", out)
	}
}

func TestABaselineNamingAVocabularyWithNoArtefactIsRefused(t *testing.T) {
	t.Parallel()

	root := stage(t)

	if err := os.Remove(filepath.Join(root, defaultArtefacts, "device-issues.json")); err != nil {
		t.Fatalf("remove the artefact: %v", err)
	}

	out, code := runIn(t, root)
	if code != exitUsage {
		t.Fatalf("a published set stopped being published by losing its file, exit %d:\n%s", code, out)
	}

	if !strings.Contains(out, "device-issues") {
		t.Errorf("the refusal does not name the vocabulary:\n%s", out)
	}
}

// stage copies the real committed artefacts, the real baseline and a release
// into a temporary tree, so every case mutates the tree the project ships
// rather than a fixture somebody wrote to pass.
func stage(t *testing.T) string {
	t.Helper()

	root := t.TempDir()

	source := filepath.Join("..", "..", defaultArtefacts)

	entries, err := os.ReadDir(source)
	if err != nil {
		t.Fatalf("read %s: %v", source, err)
	}

	if made := os.MkdirAll(filepath.Join(root, defaultArtefacts), 0o750); made != nil {
		t.Fatalf("make the artefact directory: %v", made)
	}

	copied := 0

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		writeFile(t, filepath.Join(root, defaultArtefacts, entry.Name()),
			readFile(t, filepath.Join(source, entry.Name())))

		copied++
	}

	if copied == 0 {
		t.Fatalf("%s holds no artefact, so every case below would run on an empty tree", source)
	}

	if made := os.MkdirAll(filepath.Join(root, "tools", "vocabcompat"), 0o750); made != nil {
		t.Fatalf("make the baseline directory: %v", made)
	}

	writeFile(t, filepath.Join(root, defaultBaseline), readFile(t, filepath.Join("..", "..", defaultBaseline)))
	writeFile(t, filepath.Join(root, defaultVersion), []byte("0.1.0\n"))

	// The staged tree is what the committed tree is, so it starts green. A case
	// that refuses below is refusing its own mutation and nothing else.
	if out, code := runIn(t, root); code != exitOK {
		t.Fatalf("the staged tree is refused before any mutation, exit %d:\n%s", code, out)
	}

	return root
}

// runIn runs the gate against a tree and returns everything it printed.
func runIn(t *testing.T, root ...string) (string, int) {
	t.Helper()

	var out bytes.Buffer

	code := run([]string{"-root", filepath.Join(root...), "check"}, &out, &out)

	return out.String(), code
}

// editArtefact rewrites one vocabulary artefact in a staged tree.
func editArtefact(t *testing.T, root, name string, edit func(map[string]any)) {
	t.Helper()

	edited(t, filepath.Join(root, defaultArtefacts, name+".json"), edit)
}

// editBaseline rewrites the baseline in a staged tree.
func editBaseline(t *testing.T, root string, edit func(map[string]any)) {
	t.Helper()

	edited(t, filepath.Join(root, defaultBaseline), edit)
}

// edited applies one change to a JSON file in place.
func edited(t *testing.T, path string, edit func(map[string]any)) {
	t.Helper()

	var file map[string]any
	if err := json.Unmarshal(readFile(t, path), &file); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	edit(file)

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		t.Fatalf("render %s: %v", path, err)
	}

	writeFile(t, path, data)
}

// vocabulary reaches one vocabulary inside the baseline.
func vocabulary(t *testing.T, file map[string]any, name string) map[string]any {
	t.Helper()

	all, held := file["vocabularies"].(map[string]any)
	if !held {
		t.Fatalf("the baseline carries no vocabularies")
	}

	one, held := all[name].(map[string]any)
	if !held {
		t.Fatalf("the baseline carries no %q", name)
	}

	return one
}

// without removes named entries from a baseline set, failing when one is absent
// so a renamed entry does not quietly turn the case into no mutation at all.
func without(t *testing.T, entries any, drop []string) []any {
	t.Helper()

	held := make(map[string]bool, len(drop))
	for _, name := range drop {
		held[name] = true
	}

	var kept []any

	for _, entry := range entries.([]any) {
		if !held[entry.(string)] {
			kept = append(kept, entry)
		}
	}

	if want := len(entries.([]any)) - len(drop); len(kept) != want {
		t.Fatalf("removing %v left %d entries and not %d, so the mutation is not the one this case names",
			drop, len(kept), want)
	}

	return kept
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return data
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()

	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
