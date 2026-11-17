package arch_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// A link to a numbered record carries the number twice: once in the text a
// person reads and once in the path a reader follows. Nothing compared the two.
//
// A link labelled with one number and pointing at another resolves, so make
// docs-links-check passes over it, and a reader searching for the number in
// the text finds a different record.
//
// The comparison is the same parse numbered_docs_test.go already does, applied
// to a link rather than to a file.
const (
	numberedLinkFloor = 400
	linkedDocFloor    = 100
)

// A markdown inline link. The target stops at the first space, because a title
// may follow it, and the text may not nest brackets.
var markdownLink = regexp.MustCompile(`\[([^\]\[]+)\]\(([^)\s]+)`)

// The series, keyed by what a target's base name looks like. A number in
// the text is only compared when the text states it in one of the shapes below,
// because a link text may carry a date or a count that names nothing.
//
//nolint:gochecknoglobals // a table read by one test; there is nowhere cheaper to put it.
var numberedLinkSeries = []struct {
	kind      string
	fromFile  *regexp.Regexp
	fromTexts []*regexp.Regexp
}{
	{
		kind:     "ADR",
		fromFile: regexp.MustCompile(`^(` + recordIDShape + `)-[a-z0-9-]+\.md$`),
		fromTexts: []*regexp.Regexp{
			regexp.MustCompile(`\bADR-(` + recordIDShape + `)\b`),
			regexp.MustCompile(`(?i)\bADR[- ]?(\d{1,4})\b`),
			regexp.MustCompile(`(?i)^decision\s+(\d{1,4})\b`),
		},
	},
}

// seriesFolder is the folder a series with dated ids keeps its files in.
//
//nolint:gochecknoglobals // a table read by one function.
var seriesFolder = map[string]string{
	"ADR": "decisions",
}

// TestEveryNumberedLinkNamesTheNumberItPointsAt is the gate.
func TestEveryNumberedLinkNamesTheNumberItPointsAt(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	var (
		violations []string
		judged     int
		read       int
	)

	for _, path := range linkedMarkdownFiles(t, root) {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}

		read++

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			t.Fatalf("relativise %s: %v", path, relErr)
		}

		for _, link := range markdownLink.FindAllStringSubmatch(string(body), -1) {
			text, target := link[1], link[2]

			kind, held, isNumbered := numberOfTarget(filepath.ToSlash(rel), target)
			if !isNumbered {
				continue
			}

			claimed, states := numberInText(kind, text)
			if !states {
				continue
			}

			judged++

			if claimed != held {
				violations = append(violations, fmt.Sprintf(
					"%s: %q links %s %s and calls it %s",
					filepath.ToSlash(rel), text, kind, held, claimed))
			}
		}
	}

	requireScanned(t, "markdown files under docs/ and .agents/", read, linkedDocFloor,
		"Either the directories moved, or the .md suffix did.")
	requireScanned(t, "links naming the number they point at", judged, numberedLinkFloor,
		"Either markdownLink stopped matching, or the series patterns did.")

	reportDocs(t, violations, "a link naming a numbered record names the number it points at")
}

// numberOfTarget reads the record number out of a link target, resolved against
// the source file. A target that names no numbered record is not this gate's
// business.
func numberOfTarget(source, target string) (kind, number string, ok bool) {
	path, _, _ := strings.Cut(target, "#")
	if path == "" {
		return "", "", false
	}

	base := filepath.Base(filepath.ToSlash(path))
	folder := filepath.Base(filepath.Dir(filepath.Join(filepath.Dir(source), filepath.FromSlash(path))))

	for _, series := range numberedLinkSeries {
		found := series.fromFile.FindStringSubmatch(base)
		if found == nil {
			continue
		}

		// A dated id has the same shape in more than one series, so only
		// the folder says which series it is in.
		if datedRecordFile.MatchString(base) && folder != seriesFolder[series.kind] {
			continue
		}

		return series.kind, normaliseRecordNumber(found[1]), true
	}

	return "", "", false
}

// numberInText reads the number a link's own words claim, in the shapes that
// series states one. Anything else is text that happens to hold a digit.
func numberInText(kind, text string) (number string, ok bool) {
	for _, series := range numberedLinkSeries {
		if series.kind != kind {
			continue
		}

		for _, pattern := range series.fromTexts {
			found := pattern.FindStringSubmatch(text)
			if found == nil {
				continue
			}

			return normaliseRecordNumber(found[1]), true
		}
	}

	return "", false
}

// normaliseRecordNumber makes "0007", "07" and "7" one number, and leaves a
// dated id as written.
func normaliseRecordNumber(held string) string {
	if number, err := strconv.Atoi(held); err == nil {
		return strconv.Itoa(number)
	}

	return held
}

// linkedMarkdownFiles is the input set: every markdown file in the two trees
// that cite numbered records, plus the two at the repository root. Generated
// output is skipped, because a stale number there is a defect in its generator.
func linkedMarkdownFiles(t *testing.T, root string) []string {
	t.Helper()

	skip := map[string]bool{
		"node_modules": true,
		"dist":         true,
		".astro":       true,
	}

	var found []string

	for _, dir := range []string{"docs", ".agents"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if entry.IsDir() {
				if skip[entry.Name()] ||
					path == filepath.Join(root, filepath.FromSlash(publishedDocsDir), generatedRecordPages) {
					return filepath.SkipDir
				}

				return nil
			}

			if strings.HasSuffix(entry.Name(), ".md") {
				found = append(found, path)
			}

			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", dir, err)
		}
	}

	for _, name := range []string{"README.md", "AGENTS.md"} {
		found = append(found, filepath.Join(root, name))
	}

	return found
}
