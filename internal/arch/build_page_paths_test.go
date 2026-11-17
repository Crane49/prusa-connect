package arch_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The onboarding guide's paths have been gated since
// `onboarding_docs_test.go` was written, and the rest of the `build` tier was
// not. ADR-20260826-53y rule 3 says a `build` page is unrestricted in what it may name
// and that every path it names resolves, so the gate belongs on all of them.
//
// This file covers the `build` pages outside `onboarding/`, which
// `TestTheOnboardingGuideNamesPathsThatExist` already reads. Together the two
// cover 21 pages and about 1,200 distinct paths, against the 320 the onboarding
// gate covered alone.
//
// A path that no longer resolves is named here with the reason, and the two
// reasons are different things. A page may name a path in order to say it is
// not there, and that prose is correct. A page may also name a path that moved,
// and that is a defect with a repair owed. The table says which, so neither is
// mistaken for the other.

// buildPages are the `build` pages outside `onboarding/`, with the number of
// distinct repository paths each carried when this gate was written.
//
// The floor stops the gate passing vacuously. A page rewritten in generalities,
// or one that replaces full paths with bare file names the extractor cannot
// resolve, would otherwise leave a green test over a page naming nothing
// checkable.
//
// A number falls when paths legitimately leave a page. Deleting the retired
// server took nine `legacy/connector/xbuddy/...` citations off
// internals/printers-and-drivers.md and two off the build gates chapter, and
// both rows moved to what the pages name now.
//
//nolint:gochecknoglobals // a table three tests read; there is nowhere cheaper.
var buildPages = []struct {
	file  string
	paths int
}{
	{file: "architecture.md", paths: 5},
	{file: "design/a-driver-declares.md", paths: 0},
	{file: "design/api-compatibility.md", paths: 0},
	{file: "design/events.md", paths: 0},
	{file: "design/features-are-modules.md", paths: 0},
	{file: "design/index.md", paths: 0},
	{file: "design/modules-ask.md", paths: 0},
	{file: "design/no-history-in-the-core.md", paths: 0},
	{file: "design/one-registry.md", paths: 0},
	{file: "design/permissions.md", paths: 0},
	{file: "design/the-core-knows-devices.md", paths: 0},
	{file: "design/the-sdk-is-the-contract.md", paths: 0},

	{file: "bench-preflight.md", paths: 6},
	{file: "creating-connectors.md", paths: 5},
	{file: "extending/build-time-modules.md", paths: 3},
	{file: "glossary.md", paths: 58},
	{file: "prusa-command-architecture.md", paths: 3},
	{file: "internals/authorization.md", paths: 32},
	{file: "internals/build-gates-and-testing.md", paths: 47},
	{file: "internals/printers-and-drivers.md", paths: 34},
	{file: "internals/the-application-core.md", paths: 24},
	{file: "internals/the-extension-host.md", paths: 15},
	{file: "internals/the-fractal-link.md", paths: 1},
	{file: "internals/the-sdk-and-extensions.md", paths: 40},
	{file: "internals/transport-and-the-stream.md", paths: 22},
	{file: "reference/beta-portal-machine-api.md", paths: 1},
}

// unresolvedOnBuildPages names every path a `build` page writes that the tree
// does not hold, against the reason.
//
// It is checked both ways. An entry naming a path that resolves fails, so an
// allowance cannot outlive the file coming back; an entry no page writes fails,
// so it cannot outlive the sentence that needed it.
//
//nolint:gochecknoglobals // a table two tests read; there is nowhere cheaper.
var unresolvedOnBuildPages = map[string]string{
	// The page names the path in order to say the thing is not there. The
	// prose is correct and there is nothing to repair.
	"internal/commands": "absence stated: the package was proposed and never written",
}

// TestEveryBuildPageNamesPathsThatExist is the gate. A `build` page names a
// path that resolves, or the path is in the table above with its reason.
func TestEveryBuildPageNamesPathsThatExist(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	top := topLevelEntries(t, root)

	var violations []string

	for _, page := range buildPages {
		_, body := tierAndBody(t, root, page.file)

		for _, found := range pathsIn(body, top) {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(found.path))); err == nil {
				continue
			}

			if _, excused := unresolvedOnBuildPages[found.path]; excused {
				continue
			}

			violations = append(violations, fmt.Sprintf("%s/%s:%d names %q, which does not exist",
				publishedDocsDir, page.file, found.line, found.path))
		}
	}

	reportBuildPages(t, violations)
}

// TestEveryBuildPageStillNamesEnoughPaths is the floor. A page that stopped
// naming files stopped doing the job its tier exists for, and the gate above
// would pass on it without noticing.
func TestEveryBuildPageStillNamesEnoughPaths(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	top := topLevelEntries(t, root)

	var violations []string

	for _, page := range buildPages {
		_, body := tierAndBody(t, root, page.file)

		unique := map[string]struct{}{}
		for _, found := range pathsIn(body, top) {
			unique[found.path] = struct{}{}
		}

		if len(unique) < page.paths {
			violations = append(violations, fmt.Sprintf(
				"%s/%s names %d distinct repository paths, and this gate expects at least %d",
				publishedDocsDir, page.file, len(unique), page.paths))
		}
	}

	reportBuildPages(t, violations)
}

// TestEveryBuildPageIsListed refuses a `build` page added outside
// `onboarding/` and not added to the table, which would be a page nothing
// checks. The onboarding pages have their own list and their own gate.
func TestEveryBuildPageIsListed(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	listed := map[string]struct{}{}
	for _, page := range buildPages {
		listed[page.file] = struct{}{}
	}

	var violations []string

	for _, page := range publishedPages(t, root) {
		if strings.HasPrefix(page, "onboarding/") {
			continue
		}

		tier, _ := tierAndBody(t, root, page)
		if tier != "build" {
			continue
		}

		if _, ok := listed[page]; !ok {
			violations = append(violations, fmt.Sprintf(
				"%s/%s is a `build` page and is not in buildPages, so nothing checks the paths it names",
				publishedDocsDir, page))
		}
	}

	reportBuildPages(t, violations)
}

// TestEveryUnresolvedBuildPathIsStillUnresolvedAndStillNamed checks the
// allowance table both ways.
func TestEveryUnresolvedBuildPathIsStillUnresolvedAndStillNamed(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	top := topLevelEntries(t, root)

	named := map[string]struct{}{}

	for _, page := range buildPages {
		_, body := tierAndBody(t, root, page.file)

		for _, found := range pathsIn(body, top) {
			named[found.path] = struct{}{}
		}
	}

	requireScanned(t, "distinct repository paths across the build pages", len(named), 237,
		"The path pattern stopped matching, so every allowance would be reported as\n"+
			"naming nothing.")

	var violations []string

	for path, reason := range unresolvedOnBuildPages {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err == nil {
			violations = append(violations, fmt.Sprintf(
				"unresolvedOnBuildPages excuses %q as %q, and the tree holds it now", path, reason))
		}

		if _, ok := named[path]; !ok {
			violations = append(violations, fmt.Sprintf(
				"unresolvedOnBuildPages excuses %q and no build page names it any more", path))
		}
	}

	reportBuildPages(t, violations)
}

// reportBuildPages fails with every violation listed, because one path at a
// time across thirteen pages is thirteen runs of the suite.
func reportBuildPages(t *testing.T, violations []string) {
	t.Helper()

	if len(violations) == 0 {
		return
	}

	sort.Strings(violations)
	t.Errorf("%d claim(s) of the build pages are wrong about the tree:\n\t%s",
		len(violations), strings.Join(violations, "\n\t"))
}
