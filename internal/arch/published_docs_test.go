package arch_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ADR-20260824-dym asks every published page to record the part of the tree it
// describes, in `rewrite.implementedBy`. ADR-20260826-53y rule 4 puts that field
// outside the tier restriction and gives it a check of its own: every path in
// it resolves, at every tier.
//
// The field is the one place a reader is told where the code behind a page
// lives, and it is frontmatter, so no prose gate reads it. A package that
// moves leaves the field naming a directory that is not there, and the build,
// the link checker and the citation gate all pass over it.
const publishedDocsDir = "docs/src/content/docs"

// TestEveryImplementedByPathResolves is ADR-20260826-53y rule 4. A page that names the
// code it describes is only useful while that name resolves.
func TestEveryImplementedByPathResolves(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	var violations []string

	for _, page := range publishedPages(t, root) {
		for _, named := range implementedByOf(t, root, page) {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(named.path))); err != nil {
				violations = append(violations, fmt.Sprintf(
					"%s/%s:%d names %q in rewrite.implementedBy, which does not exist",
					publishedDocsDir, page, named.line, named.path))
			}
		}
	}

	reportPublished(t, violations)
}

// TestEveryPublishedPageNamesWhatImplementsIt is the floor under the gate
// above. A page with an empty `implementedBy` passes it without being checked,
// so deleting the field is the cheapest way to make a stale path go green.
func TestEveryPublishedPageNamesWhatImplementsIt(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	var violations []string

	for _, page := range publishedPages(t, root) {
		if len(implementedByOf(t, root, page)) == 0 {
			violations = append(violations, fmt.Sprintf(
				"%s/%s carries no rewrite.implementedBy, so nothing says where its code is",
				publishedDocsDir, page))
		}
	}

	reportPublished(t, violations)
}

// generatedRecordPages is the folder under the content directory that the
// site build writes and git ignores.
const generatedRecordPages = "records"

// publishedPages lists every page of the docs collection, by its path under
// the content directory. It is read rather than listed, so a page added
// tomorrow is checked the day it lands.
func publishedPages(t *testing.T, root string) []string {
	t.Helper()

	dir := filepath.Join(root, filepath.FromSlash(publishedDocsDir))

	var pages []string

	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// docs/scripts/records.mjs writes records/ on every site build, and
		// docs/.gitignore keeps it out of the tree. A clean checkout has none
		// of it, so a gate reading it locally would judge a different tree.
		if entry.IsDir() {
			if path == filepath.Join(dir, generatedRecordPages) {
				return filepath.SkipDir
			}

			return nil
		}

		switch filepath.Ext(entry.Name()) {
		case ".md", ".mdx":
		default:
			return nil
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		pages = append(pages, filepath.ToSlash(rel))

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", publishedDocsDir, err)
	}

	sort.Strings(pages)

	return pages
}

// implementedByOf reads one page's `rewrite.implementedBy` entries and the
// line each sits on.
//
// The frontmatter is read with a line scanner rather than a YAML parser. This
// module vendors no YAML dependency for a test, the field is a flat list of
// scalars in every page here, and a line number is what makes a failure
// something a reader can open.
func implementedByOf(t *testing.T, root, page string) []reference {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(publishedDocsDir), filepath.FromSlash(page)))
	if err != nil {
		t.Fatalf("read %s/%s: %v", publishedDocsDir, page, err)
	}

	// Some pages here are saved with a UTF-8 byte order mark, which would
	// otherwise hide the opening frontmatter fence from the scanner below.
	lines := strings.Split(strings.TrimPrefix(string(body), "\ufeff"), "\n")

	var (
		found  []reference
		inside bool
		listed bool
	)

	for number, raw := range lines {
		line := strings.TrimRight(raw, "\r")

		if number == 0 {
			if strings.TrimSpace(line) != "---" {
				return nil
			}

			inside = true

			continue
		}

		if !inside {
			break
		}

		if strings.TrimSpace(line) == "---" {
			break
		}

		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "implementedBy:") {
			listed = true

			continue
		}

		if !listed {
			continue
		}

		if item, ok := strings.CutPrefix(trimmed, "- "); ok {
			found = append(found, reference{
				path: strings.Trim(strings.TrimSpace(item), `"'`),
				line: number + 1,
			})

			continue
		}

		// Anything that is not a list item ends the list, which is the next
		// key of the frontmatter.
		listed = false
	}

	return found
}

// reportPublished fails with every violation listed, so one run names all of
// them rather than the first.
func reportPublished(t *testing.T, violations []string) {
	t.Helper()

	if len(violations) == 0 {
		return
	}

	sort.Strings(violations)
	t.Errorf("%d published page(s) are wrong about the tree:\n\t%s",
		len(violations), strings.Join(violations, "\n\t"))
}
