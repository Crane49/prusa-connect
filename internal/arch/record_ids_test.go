package arch_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// A decision record is named by a dated id: the day in UTC and three random
// characters, such as 20261025-k7q. scripts/new-record.sh writes one, through
// make adr-new.
//
// Sequential numbers made every pull request that added a record conflict with
// every other one. Two branches took the same next number, and both edited the
// same ledger row and the same index table. A dated id is picked alone, so the
// only shared state left is the folder, and this file is the gate over it.
//
// The records written before dated ids took one dated by the day each was
// written, so the tree cites every record in one form. A numbered file is
// refused.

// recordIDShape is the dated id: eight digits of a date, a hyphen and three
// characters of lowercase base32 without i, l, o and u.
const recordIDShape = `\d{8}-[a-hjkmnp-tv-z2-7]{3}`

// recordFolder holds the records named by a dated id.
const recordFolder = "docs/decisions"

//nolint:gochecknoglobals // compiled patterns shared by the gates below.
var (
	datedRecordFile = regexp.MustCompile(`^(` + recordIDShape + `)-[a-z0-9]+(-[a-z0-9]+)*\.md$`)
	adrReference    = regexp.MustCompile(`^ADR-` + recordIDShape + `$`)
	isoDate         = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

	// legacyPartialStatus is the status five records carried before a newer
	// record named the older one in its own front matter. It stays as written.
	legacyPartialStatus = regexp.MustCompile(`^superseded in part by ADR-` + recordIDShape + `$`)
)

// recordStatuses is what a decision record written with a dated id may carry.
// Five older records keep the partial status legacyPartialStatus reads.
//
//nolint:gochecknoglobals // a table read by one gate.
var recordStatuses = map[string]bool{
	"proposed": true,
	"accepted": true,
	"rejected": true,
}

// TestEveryRecordIDNamesOneFile is the collision gate. A collision of two
// random ids is rare, and it fails the second pull request when CI tests it
// against the merged tree.
func TestEveryRecordIDNamesOneFile(t *testing.T) {
	t.Parallel()

	owner := map[string]string{}

	var violations []string

	for _, rel := range datedRecords(t) {
		id := datedRecordFile.FindStringSubmatch(filepath.Base(rel))[1]

		if held, taken := owner[id]; taken {
			violations = append(violations, fmt.Sprintf(
				"%s and %s both take the id %s. Rename the newer one with a fresh id", held, rel, id))

			continue
		}

		owner[id] = rel
	}

	reportDocs(t, violations, "one dated id names one record")
}

// TestANewRecordTakesADatedID refuses a record that took a number, and a file
// in the folder that is not a record.
func TestANewRecordTakesADatedID(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	var violations []string

	for _, name := range markdownIn(t, filepath.Join(root, filepath.FromSlash(recordFolder))) {
		if datedRecordFile.MatchString(name) || isRecordProse(name) {
			continue
		}

		violations = append(violations, fmt.Sprintf(
			"%s/%s is not named by a dated id. Write it with make adr-new TITLE=\"...\", "+
				"which names it <date>-<xxx>-<slug>.md", recordFolder, name))
	}

	reportDocs(t, violations, "a new record takes a dated id, never a number")
}

// TestEveryRecordIDCarriesARealDate refuses an id whose date is not a day, or
// is more than a day ahead of the clock, which is a typo and not a plan.
func TestEveryRecordIDCarriesARealDate(t *testing.T) {
	t.Parallel()

	latest := time.Now().UTC().AddDate(0, 0, 1)

	var violations []string

	for _, rel := range datedRecords(t) {
		id := datedRecordFile.FindStringSubmatch(filepath.Base(rel))[1]

		day, err := time.Parse("20060102", id[:8])

		switch {
		case err != nil:
			violations = append(violations, fmt.Sprintf("%s: %s is not a date", rel, id[:8]))
		case day.After(latest):
			violations = append(violations, fmt.Sprintf("%s: %s is after tomorrow", rel, id[:8]))
		}
	}

	reportDocs(t, violations, "a dated id carries a real date")
}

// TestEveryDecisionRecordHasItsFrontMatter reads what the documentation site
// builds the index from. A record the site cannot place is a record missing
// from the index, with nothing to say so.
func TestEveryDecisionRecordHasItsFrontMatter(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	dir := filepath.Join(root, "docs", "decisions")

	known := map[string]bool{}
	records := map[string]recordFrontMatter{}

	for _, name := range markdownIn(t, dir) {
		id := decisionID(name)
		if id == "" {
			continue
		}

		known["ADR-"+id] = true
		records[name] = readFrontMatter(t, filepath.Join(dir, name))
	}

	requireScanned(t, "decision records", len(records), 90,
		"Either docs/decisions moved, or decisionID stopped reading the file names.")

	var violations []string

	for _, name := range sortedKeys(records) {
		record := records[name]
		id := decisionID(name)
		dated := datedRecordFile.MatchString(name)
		where := "docs/decisions/" + name

		if !strings.HasPrefix(record.Title, "ADR-"+id+": ") {
			violations = append(violations, fmt.Sprintf("%s: the title does not start with %q", where, "ADR-"+id+": "))
		}

		violations = append(violations, checkRecordDate(where, id, record.Date, dated)...)

		switch {
		case record.Status == "":
			violations = append(violations, where+": the front matter carries no status")
		case dated && !recordStatuses[record.Status] && !legacyPartialStatus.MatchString(record.Status):
			violations = append(violations, fmt.Sprintf(
				"%s: status %q is none of proposed, accepted and rejected", where, record.Status))
		}

		for field, refs := range map[string][]string{"supersedes": record.Supersedes, "amends": record.Amends} {
			for _, ref := range refs {
				switch {
				case !adrReference.MatchString(ref):
					violations = append(violations, fmt.Sprintf(
						"%s: %s names %q, which is not written ADR-<id>", where, field, ref))
				case ref == "ADR-"+id:
					violations = append(violations, fmt.Sprintf("%s: %s names the record itself", where, field))
				case !known[ref]:
					violations = append(violations, fmt.Sprintf(
						"%s: %s names %s, and no record in docs/decisions has that id", where, field, ref))
				}
			}
		}
	}

	reportDocs(t, violations, "a decision record carries the front matter the index is built from")
}

// TestTheIDGatesRefuseWhatTheyGuard is the mutation check on the patterns the
// gates above stand on.
func TestTheIDGatesRefuseWhatTheyGuard(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]bool{
		"20261025-k7q-a-server-answers-to-one-id.md": true,
		"20261025-k7q.md":                          false,
		"20261025-k7i-a-letter-that-reads-as-1.md": false,
		"20261025-K7Q-capitals.md":                 false,
		"2026102-k7q-seven-digits.md":              false,
		"0101-a-new-number.md":                     false,
	} {
		if datedRecordFile.MatchString(name) != want {
			t.Errorf("datedRecordFile on %q = %v, want %v", name, !want, want)
		}
	}

	for ref, want := range map[string]bool{
		"ADR-20261025-k7q": true,
		"ADR-0094":         false,
		"ADR-20261025-k7":  false,
	} {
		if adrReference.MatchString(ref) != want {
			t.Errorf("adrReference on %q = %v, want %v", ref, !want, want)
		}
	}
}

// recordFrontMatter is the part of a record's front matter the gates read.
type recordFrontMatter struct {
	Title      string   `yaml:"title"`
	Date       string   `yaml:"date"`
	Status     string   `yaml:"status"`
	Supersedes []string `yaml:"supersedes"`
	Amends     []string `yaml:"amends"`
}

func readFrontMatter(t *testing.T, path string) recordFrontMatter {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	text := strings.ReplaceAll(string(body), "\r\n", "\n")

	rest, found := strings.CutPrefix(text, "---\n")
	if !found {
		t.Errorf("%s: no front matter", path)

		return recordFrontMatter{}
	}

	block, _, closed := strings.Cut(rest, "\n---\n")
	if !closed {
		t.Errorf("%s: the front matter is not closed", path)

		return recordFrontMatter{}
	}

	// A date is read as text. yaml.v3 decodes 2026-10-25 into a string field
	// as written, which is the form the gates compare.
	var record recordFrontMatter
	if err = yaml.Unmarshal([]byte(block), &record); err != nil {
		t.Errorf("%s: the front matter is not YAML: %v", path, err)
	}

	return record
}

// checkRecordDate refuses a date that is not YYYY-MM-DD, and for a dated id a
// date the id disagrees with.
func checkRecordDate(where, id, date string, dated bool) []string {
	if !isoDate.MatchString(date) {
		return []string{fmt.Sprintf("%s: date %q is not written YYYY-MM-DD", where, date)}
	}

	if dated && strings.ReplaceAll(date, "-", "") != id[:8] {
		return []string{fmt.Sprintf("%s: the date says %s and the id says %s", where, date, id[:8])}
	}

	return nil
}

// decisionID is the id a decision record's file name carries, and "" for the
// template and the prose beside the records.
func decisionID(name string) string {
	if found := datedRecordFile.FindStringSubmatch(name); found != nil {
		return found[1]
	}

	return ""
}

// datedRecords lists every file named by a dated id, relative to the root.
func datedRecords(t *testing.T) []string {
	t.Helper()

	root := repoRoot(t)

	var found []string

	for _, name := range markdownIn(t, filepath.Join(root, filepath.FromSlash(recordFolder))) {
		if datedRecordFile.MatchString(name) {
			found = append(found, recordFolder+"/"+name)
		}
	}

	requireScanned(t, "records named by a dated id", len(found), 100,
		"The folder moved, or the id shape changed.")

	return found
}

// markdownIn lists the markdown files directly in dir, sorted.
func markdownIn(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	var names []string

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			names = append(names, entry.Name())
		}
	}

	sort.Strings(names)

	return names
}

// isRecordProse names the files beside the records that are not records.
func isRecordProse(name string) bool {
	switch name {
	case "README.md", "template.md":
		return true
	}

	return false
}
