package arch_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// ADR-20260826-53y rule 3: a page names no repository path above its tier in its body.
// A `run` page names none under `internal/`, `sdk/` or `cmd/`. An `extend` page
// names none under `internal/`. A `build` page is unrestricted.
//
// The tier is what a page may assume its reader holds. A self-hoster who writes
// no Go cannot check a claim written as `internal/app/config/config.go:437`, so
// the path is a promise made to somebody who cannot read it.
//
// One counter. A path above the tier is drift, and the fix is rewriting the
// sentence. The baseline carried a second counter while `legacy/` existed,
// because a path there fell to zero by deletion rather than by writing, and a
// single number would have credited the deletion as documentation work. That
// directory is gone and so is the counter.
//
// The frontmatter is outside both counters. ADR-20260826-53y rule 4 gives
// `rewrite.implementedBy` its own check, in published_docs_test.go, because
// ADR-20260824-dym requires that field to name the code a page describes.
const tierBaselineFile = "scripts/tier-paths.baseline"

// tierPageFloor and tierPathFloor arm the gate. A frontmatter parser that stops
// finding `audience`, or a path pattern that stops matching, would otherwise
// leave a green test over an empty read.
const (
	tierPageFloor = 40
	tierPathFloor = 200
)

// restrictedFor is the prefix set a tier forbids in a page body. `build` is
// unrestricted and is absent from the map.
//
//nolint:gochecknoglobals // a table two tests read; there is nowhere cheaper.
var restrictedFor = map[string][]string{
	"run":    {"internal/", "sdk/", "cmd/"},
	"extend": {"internal/"},
}

// audienceLine reads the tier out of a page's frontmatter.
var audienceLine = regexp.MustCompile(`(?m)^audience:\s*(\w+)\s*$`)

// TestNoPageNamesAPathAboveItsTier is the gate, as a ratchet. A count that
// rises fails, a count that falls fails until the baseline is corrected, and a
// page with no line is held to zero.
func TestNoPageNamesAPathAboveItsTier(t *testing.T) {
	t.Parallel()

	counted, pages, paths := tierCounts(t)

	requireScanned(t, "published pages with a tier", pages, tierPageFloor,
		"The frontmatter parser stopped finding `audience`, so no page would be judged.")
	requireScanned(t, "repository paths in the page bodies", paths, tierPathFloor,
		"The path pattern stopped matching, so every counter would read zero and the\n"+
			"baseline would be reported as owed a correction on every line.")

	base := tierBaseline(t)

	var violations []string

	for key, count := range counted {
		expected := base[key]

		switch {
		case count > expected:
			violations = append(violations, fmt.Sprintf(
				"%s counter %q is %d, over the %d this baseline allows. "+
					"Rewrite the sentence rather than raising the line",
				key.page, key.counter, count, expected))
		case count < expected:
			violations = append(violations, fmt.Sprintf(
				"%s counter %q is %d, under the %d in the baseline. "+
					"Correct the line to %d in this change, or delete it at zero",
				key.page, key.counter, count, expected, count))
		}
	}

	for key, expected := range base {
		if _, ok := counted[key]; !ok {
			violations = append(violations, fmt.Sprintf(
				"the baseline allows %s %d for counter %q and the page names none. "+
					"Delete the line", key.page, expected, key.counter))
		}
	}

	reportTierPaths(t, violations)
}

// tierKey identifies one baseline line.
type tierKey struct {
	counter string
	page    string
}

// tierCounts is every non-zero counter across the published pages, with the
// number of pages read and the number of repository paths seen. The last two
// are what requireScanned holds, because a counter of zero everywhere is the
// reading this gate cannot tell from a clean tree.
func tierCounts(t *testing.T) (map[tierKey]int, int, int) {
	t.Helper()

	root := repoRoot(t)
	top := topLevelEntries(t, root)
	counted := map[tierKey]int{}
	pages, paths := 0, 0

	for _, page := range publishedPages(t, root) {
		tier, body := tierAndBody(t, root, page)
		if tier == "" {
			t.Errorf("%s/%s carries no `audience`, which the collection schema requires",
				publishedDocsDir, page)

			continue
		}

		pages++

		restricted, bounded := restrictedFor[tier]

		for _, found := range pathsIn(body, top) {
			paths++

			if !bounded {
				continue
			}

			for _, prefix := range restricted {
				if strings.HasPrefix(found.path, prefix) {
					counted[tierKey{counter: "above", page: page}]++

					break
				}
			}
		}
	}

	return counted, pages, paths
}

// tierAndBody splits one page into its tier and the body under the
// frontmatter. The body is what rule 3 restricts, and the frontmatter is what
// rule 4 checks separately, so the two never share a counter.
func tierAndBody(t *testing.T, root, page string) (string, string) {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(publishedDocsDir), filepath.FromSlash(page)))
	if err != nil {
		t.Fatalf("read %s/%s: %v", publishedDocsDir, page, err)
	}

	lines := strings.Split(strings.TrimPrefix(string(raw), "\ufeff"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", string(raw)
	}

	for number, line := range lines[1:] {
		if strings.TrimSpace(line) != "---" {
			continue
		}

		frontmatter := strings.Join(lines[1:number+1], "\n")
		body := strings.Join(lines[number+2:], "\n")

		match := audienceLine.FindStringSubmatch(frontmatter)
		if match == nil {
			return "", body
		}

		return match[1], body
	}

	return "", string(raw)
}

// tierBaseline reads scripts/tier-paths.baseline.
func tierBaseline(t *testing.T) map[tierKey]int {
	t.Helper()

	root := repoRoot(t)

	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(tierBaselineFile)))
	if err != nil {
		t.Fatalf("read %s: %v", tierBaselineFile, err)
	}

	base := map[tierKey]int{}

	for number, line := range strings.Split(string(raw), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		field := strings.Split(line, "\t")
		if len(field) != 3 {
			t.Fatalf("%s:%d is %q, and a line is three tab-separated fields: counter, page, count",
				tierBaselineFile, number+1, line)
		}

		count, convErr := strconv.Atoi(field[2])
		if convErr != nil {
			t.Fatalf("%s:%d has count %q: %v", tierBaselineFile, number+1, field[2], convErr)
		}

		if field[0] != "above" {
			t.Fatalf("%s:%d names counter %q, and the only counter is `above`",
				tierBaselineFile, number+1, field[0])
		}

		base[tierKey{counter: field[0], page: field[1]}] = count
	}

	if len(base) == 0 {
		t.Fatalf("%s parsed to no lines, so the ratchet would hold every page to zero",
			tierBaselineFile)
	}

	return base
}

// reportTierPaths fails with every violation listed, because a page at a time
// across twenty pages is twenty runs of the suite.
func reportTierPaths(t *testing.T, violations []string) {
	t.Helper()

	if len(violations) == 0 {
		return
	}

	sort.Strings(violations)
	t.Errorf("%d tier counter(s) disagree with %s:\n\t%s",
		len(violations), tierBaselineFile, strings.Join(violations, "\n\t"))
}
