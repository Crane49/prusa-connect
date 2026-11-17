package arch_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// docsrefPath is the one home of the anchor a generated reference publishes.
const docsrefPath = "internal/docsref"

// renderHelpers are the functions that were copied into six generators, each
// one an answer to a question ADR-20260825-rre rule 16 says is asked in one place. The
// anchor is the published contract: a reader holding `core.printer/GCODE_NOT_
// ALLOWED` from a 409 response constructs its link without opening the page,
// and a second derivation is a second answer.
//
//nolint:gochecknoglobals // a table two tests read; there is nowhere cheaper.
var renderHelpers = map[string]string{
	"anchorFor":   "docsref.Anchor or docsref.AnchorUnder",
	"escapeProse": "docsref.EscapeProse",
	"escapeSpan":  "docsref.EscapeSpan",
	"yesNo":       "docsref.YesNo",
	"orNone":      "docsref.OrNone",
}

// TestOnlyDocsrefDerivesAPublishedAnchor is ADR-20260826-swm rule 3's "one pure
// documented function, shared by every generator", as a gate rather than as an
// open item. It reads declarations rather than call sites, because the way this
// invariant breaks is six copy-pasted functions, not six callers.
//
// Test files are not scanned: a test may legitimately name a fixture after the
// thing it exercises, and `TestPlacementOfAnchorsOnSegments` in this very
// package is one.
func TestOnlyDocsrefDerivesAPublishedAnchor(t *testing.T) {
	t.Parallel()

	var violations []string

	for _, where := range []string{"cmd", "internal"} {
		for _, file := range goFilesUnder(t, where) {
			if strings.HasPrefix(filepath.ToSlash(file), docsrefPath) {
				continue
			}

			for _, declared := range functionsIn(t, filepath.Join(repoRoot(t), file)) {
				instead, copied := renderHelpers[declared]
				if !copied {
					continue
				}

				violations = append(violations, fmt.Sprintf(
					"%s declares %s, which belongs to %s. Call %s", file, declared, docsrefPath, instead))
			}
		}
	}

	reportDuplication(t, violations,
		"ADR-20260826-swm rule 3, docs/decisions/20260826-swm-a-generated-reference-is-read-by-a-person.md")
}

// TestEveryGeneratorThatPublishesAnAnchorAsksDocsrefForIt closes the route the
// declaration check alone leaves open: a generator that derives an anchor under
// a name nothing recognises. A command that writes `{#` into a page is
// publishing a link, and the only sanctioned source of one is docsref.
func TestEveryGeneratorThatPublishesAnAnchorAsksDocsrefForIt(t *testing.T) {
	t.Parallel()

	anchoring := map[string]bool{}
	importing := map[string]bool{}

	for _, file := range goFilesUnder(t, "cmd") {
		body, err := os.ReadFile(filepath.Join(repoRoot(t), file))
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}

		pkg := filepath.ToSlash(filepath.Dir(file))

		if strings.Contains(string(body), "{#") {
			anchoring[pkg] = true
		}

		if strings.Contains(string(body), `"`+modulePath+"/"+docsrefPath+`"`) {
			importing[pkg] = true
		}
	}

	requireScanned(t, "packages under cmd/ that publish an anchor", len(anchoring), 1,
		"An anchor is the `{#` form, and a walk that stops reaching cmd/ finds none.")

	var violations []string

	for _, pkg := range sortedKeys(anchoring) {
		if !importing[pkg] {
			violations = append(violations,
				pkg+" publishes an anchor and does not import "+docsrefPath)
		}
	}

	reportDuplication(t, violations, "ADR-20260826-swm rule 3")
}

// reportDuplication fails with what was found. `report` in layout_test.go
// counts import edges, and these are declarations.
func reportDuplication(t *testing.T, violations []string, law string) {
	t.Helper()

	if len(violations) == 0 {
		return
	}

	sort.Strings(violations)
	t.Errorf("%d place(s) break %s:\n\t%s",
		len(violations), law, strings.Join(violations, "\n\t"))
}

// goFilesUnder returns every non-test Go file under a directory of the
// repository, as a path relative to the root.
func goFilesUnder(t *testing.T, dir string) []string {
	t.Helper()

	root := repoRoot(t)

	var found []string

	err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		found = append(found, relative)

		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", dir, err)
	}

	sort.Strings(found)

	return found
}

// functionsIn returns the names of the functions a file declares, methods
// excluded: a method is named on its receiver and cannot be the loose copy this
// gate is looking for.
func functionsIn(t *testing.T, path string) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	var names []string

	for _, decl := range file.Decls {
		function, isFunction := decl.(*ast.FuncDecl)
		if !isFunction || function.Recv != nil {
			continue
		}

		names = append(names, function.Name.Name)
	}

	return names
}
