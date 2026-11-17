package arch_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The decision records name their files by an identifier the rest of the
// tree links to: the dated id record_ids_test.go describes. Every ADR cites
// other ADRs by that id. An id is therefore a name, and a name taken twice is
// a defect.
//
// It has happened twice in one day, both times by two agents writing an hour
// apart with no way to see each other: two decision records answering to one
// number. Nothing in the repository noticed either. This is that gate.
//
// The number lives in up to three places per file and all three are checked,
// because a hand rename that updates the file name and leaves the heading is
// the same defect arriving quietly.
type numberedSeries struct {
	// dir is relative to the repository root.
	dir string
	// glob selects the numbered files, excluding README and other prose.
	glob string
	// fromFileName captures the number from the base name.
	fromFileName *regexp.Regexp
	// fromHeading captures the number from the level-one heading.
	fromHeading *regexp.Regexp
	// fromFrontMatter captures the number from the front matter, nil where the
	// series does not carry one.
	fromFrontMatter *regexp.Regexp
	// sharedNumbers records a duplicate this repository means, against the
	// reason it is allowed. Everything else is a collision.
	sharedNumbers map[string]string
	// headingMayOmitNumber is true where the series has no universal
	// convention of numbering its heading. The defect being gated is a heading
	// that states the wrong number, and where a series never states one,
	// absence is a style difference rather than a stale rename.
	headingMayOmitNumber bool
}

//nolint:gochecknoglobals // a table read by two tests; there is nowhere cheaper to put it.
var numberedDocSeries = []numberedSeries{
	{
		dir:             filepath.Join("docs", "decisions"),
		glob:            "[0-9]*.md",
		fromFileName:    regexp.MustCompile(`^(` + recordIDShape + `)-[a-z0-9-]+\.md$`),
		fromHeading:     regexp.MustCompile(`(?m)^#\s+ADR-(` + recordIDShape + `):`),
		fromFrontMatter: regexp.MustCompile(`(?m)^title:\s*"ADR-(` + recordIDShape + `):`),
	},
}

// TestEveryNumberedDocumentOwnsItsNumberAlone is the collision gate. Two files
// answering to one number makes one of them unreachable through half the links
// that point at it, silently.
func TestEveryNumberedDocumentOwnsItsNumberAlone(t *testing.T) {
	t.Parallel()

	var violations []string

	for _, series := range numberedDocSeries {
		owner := map[string]string{}

		for _, path := range seriesFiles(t, series) {
			name := filepath.Base(path)

			if _, shared := series.sharedNumbers[name]; shared {
				continue
			}

			found := series.fromFileName.FindStringSubmatch(name)
			if found == nil {
				violations = append(violations,
					fmt.Sprintf("%s/%s: the file name does not carry a number in this series' shape",
						series.dir, name))

				continue
			}

			if held, taken := owner[found[1]]; taken {
				violations = append(violations,
					fmt.Sprintf("%s: %s and %s both claim number %s",
						series.dir, held, name, found[1]))

				continue
			}

			owner[found[1]] = name
		}
	}

	reportDocs(t, violations, "a numbered document under docs/ owns its number alone")
}

// TestEveryNumberedDocumentAgreesWithItself catches the half-done rename. The
// file name is what links resolve against, the front matter is what Dataview
// and the site read, and the heading is what a person reads. All three are the
// same identifier or one of those readers is wrong.
func TestEveryNumberedDocumentAgreesWithItself(t *testing.T) {
	t.Parallel()

	var violations []string

	for _, series := range numberedDocSeries {
		for _, path := range seriesFiles(t, series) {
			name := filepath.Base(path)

			if _, shared := series.sharedNumbers[name]; shared {
				continue
			}

			fromName := series.fromFileName.FindStringSubmatch(name)
			if fromName == nil {
				continue
			}

			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}

			places := []struct {
				what     string
				pattern  *regexp.Regexp
				optional bool
			}{
				{"the # heading", series.fromHeading, series.headingMayOmitNumber},
				{"the front matter", series.fromFrontMatter, false},
			}

			for _, place := range places {
				if place.pattern == nil {
					continue
				}

				found := place.pattern.FindStringSubmatch(string(body))
				if found == nil {
					if !place.optional {
						violations = append(violations,
							fmt.Sprintf("%s/%s: %s carries no number", series.dir, name, place.what))
					}

					continue
				}

				if found[1] != fromName[1] {
					violations = append(violations,
						fmt.Sprintf("%s/%s: %s says %s, the file name says %s",
							series.dir, name, place.what, found[1], fromName[1]))
				}
			}
		}
	}

	reportDocs(t, violations, "a numbered document under docs/ agrees with its own number")
}

// TestEveryAllowedDuplicateStillExists stops the shared-number list becoming
// precedent for a collision nobody meant.
func TestEveryAllowedDuplicateStillExists(t *testing.T) {
	t.Parallel()

	for _, series := range numberedDocSeries {
		for name, reason := range series.sharedNumbers {
			path := filepath.Join(repoRoot(t), series.dir, name)

			if _, err := os.Stat(path); err != nil {
				t.Errorf("%s/%s is allowed to share a number as %q, and no longer exists",
					series.dir, name, reason)
			}
		}
	}
}

// seriesFiles lists one series' numbered documents, sorted.
func seriesFiles(t *testing.T, series numberedSeries) []string {
	t.Helper()

	pattern := filepath.Join(repoRoot(t), series.dir, series.glob)

	found, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob %s: %v", pattern, err)
	}

	requireScanned(t, "documents matching "+pattern, len(found), 1,
		"The series moved, or its glob no longer describes how it is named.")

	sort.Strings(found)

	return found
}

// reportDocs fails with the violations listed. layout_test.go's report speaks
// of import edges, which these are not.
func reportDocs(t *testing.T, violations []string, law string) {
	t.Helper()

	if len(violations) == 0 {
		return
	}

	sort.Strings(violations)
	t.Errorf("%d document(s) break %s:\n\t%s",
		len(violations), law, strings.Join(violations, "\n\t"))
}
