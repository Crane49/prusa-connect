package arch_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The onboarding guide's whole value is that a reader can open the file it
// names. A page whose paths have rotted is worse than no page, because it is
// believed: a developer follows a path, finds nothing, and stops trusting the
// section rather than the one line that moved.
//
// This is that gate. It reads every repository path the guide mentions, in
// prose and inside its diagrams alike, and fails on one that no longer exists.
const onboardingDir = "docs/src/content/docs/onboarding"

// onboardingPages are the pages of the guide and the number of repository
// paths each carried when this gate was written.
//
// The floor is what stops the gate passing vacuously. Rewriting a page in
// generalities, or replacing full paths with bare file names the extractor
// cannot resolve, would otherwise leave a green test over a page that names
// nothing checkable. A page that legitimately sheds paths has its number
// edited in the same change.
//
//nolint:gochecknoglobals // a table two tests read; there is nowhere cheaper.
var onboardingPages = []struct {
	file  string
	paths int
}{
	{file: "index.md", paths: 18},
	{file: "the-entry-point.md", paths: 18},
	{file: "the-model.md", paths: 35},
	{file: "the-data-flow.md", paths: 25},
	{file: "the-sdk.md", paths: 23},
	{file: "registries-and-names.md", paths: 27},
	{file: "where-things-live.md", paths: 129},
	{file: "when-things-go-wrong.md", paths: 40},
}

// repositoryPath matches a path rooted at a top-level entry of this repository,
// optionally followed by a line reference. The leading boundary is a character
// that cannot appear in a path, so `internal/app` inside a sentence, a table
// cell, a code span or an ascii diagram is all found the same way.
//
// A bare file name is deliberately not matched. `hub.go` names no directory, so
// there is nothing to resolve it against and no way to tell a real reference
// from a word; the floor above is what covers a page that drifted into writing
// them.
var repositoryPath = regexp.MustCompile(
	`(?:^|[^A-Za-z0-9_./-])((?:[a-z][a-z0-9-]*/)+[A-Za-z0-9_.-]+)(?::\d+)?`)

// TestTheOnboardingGuideNamesPathsThatExist is the gate. Every repository path
// the guide mentions resolves, or the page says where to look and is wrong.
func TestTheOnboardingGuideNamesPathsThatExist(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	top := topLevelEntries(t, root)

	var violations []string

	for _, page := range onboardingPages {
		body := onboardingPage(t, root, page.file)

		for _, found := range pathsIn(body, top) {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(found.path))); err != nil {
				violations = append(violations, fmt.Sprintf("%s/%s:%d names %q, which does not exist",
					onboardingDir, page.file, found.line, found.path))
			}
		}
	}

	reportOnboarding(t, violations)
}

// TestTheOnboardingGuideStillNamesEnoughPaths is the floor. A page that stopped
// naming files stopped doing the one job the section exists for, and the gate
// above would pass on it without noticing.
func TestTheOnboardingGuideStillNamesEnoughPaths(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	top := topLevelEntries(t, root)

	var violations []string

	for _, page := range onboardingPages {
		body := onboardingPage(t, root, page.file)

		unique := map[string]struct{}{}
		for _, found := range pathsIn(body, top) {
			unique[found.path] = struct{}{}
		}

		if len(unique) < page.paths {
			violations = append(violations, fmt.Sprintf(
				"%s/%s names %d distinct repository paths, and this gate expects at least %d",
				onboardingDir, page.file, len(unique), page.paths))
		}
	}

	reportOnboarding(t, violations)
}

// TestEveryOnboardingPageIsListed refuses a page added to the directory and not
// to the table, which would otherwise be a page nothing checks.
func TestEveryOnboardingPageIsListed(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	entries, err := os.ReadDir(filepath.Join(root, onboardingDir))
	if err != nil {
		t.Fatalf("read %s: %v", onboardingDir, err)
	}

	listed := map[string]struct{}{}
	for _, page := range onboardingPages {
		listed[page.file] = struct{}{}
	}

	var violations []string

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}

		if _, ok := listed[entry.Name()]; !ok {
			violations = append(violations, fmt.Sprintf(
				"%s/%s is not in onboardingPages, so nothing checks the paths it names",
				onboardingDir, entry.Name()))
		}
	}

	reportOnboarding(t, violations)
}

// reference is one path a page names, and the line it is on.
type reference struct {
	path string
	line int
}

// pathsIn extracts every repository path from one page. A match whose first
// segment is not a top-level entry of this repository is not a repository path:
// `com.prusa.connect`, a URL, and `device:p4f2/state` are all rejected there
// rather than by a list of exceptions that would go stale.
func pathsIn(body string, top map[string]struct{}) []reference {
	var found []reference

	for number, line := range strings.Split(body, "\n") {
		for _, match := range repositoryPath.FindAllStringSubmatch(line, -1) {
			path := match[1]

			if _, ok := top[strings.SplitN(path, "/", 2)[0]]; !ok {
				continue
			}

			found = append(found, reference{path: path, line: number + 1})
		}
	}

	return found
}

// topLevelEntries is what this repository has at its root. It is read rather
// than listed, so a new top-level directory is covered the day it is added.
//
// An entry git ignores is left out. A build writes `bin/`, and a page that
// names `bin/ci-local/<id>.log` names an output, which no clone holds. Reading
// it as a repository path made these gates fail on a machine that had run a
// build and pass on a fresh clone.
func topLevelEntries(t *testing.T, root string) map[string]struct{} {
	t.Helper()

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read the repository root: %v", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	ignored := ignoredEntries(t, root, names)

	top := make(map[string]struct{}, len(entries))
	for _, name := range names {
		if _, skip := ignored[name]; !skip {
			top[name] = struct{}{}
		}
	}

	return top
}

// ignoredEntries answers which of the names, each relative to root, a
// .gitignore rule matches. git check-ignore exits 1 when it matches none, and
// that is an answer, not a fault.
func ignoredEntries(t *testing.T, root string, names []string) map[string]struct{} {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", append([]string{"check-ignore", "--"}, names...)...)
	cmd.Dir = root

	out, err := cmd.Output()

	var exit *exec.ExitError
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
		t.Fatalf("asking git which root entries are ignored: %v", err)
	}

	ignored := map[string]struct{}{}

	for line := range strings.SplitSeq(normaliseNewlines(string(out)), "\n") {
		if name := strings.TrimSpace(line); name != "" {
			ignored[name] = struct{}{}
		}
	}

	return ignored
}

func onboardingPage(t *testing.T, root, file string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(root, onboardingDir, file))
	if err != nil {
		t.Fatalf("read %s/%s: %v", onboardingDir, file, err)
	}

	return string(body)
}

// reportOnboarding fails with every violation listed, because fixing one path
// at a time across eight pages is eight runs of the suite.
func reportOnboarding(t *testing.T, violations []string) {
	t.Helper()

	if len(violations) == 0 {
		return
	}

	sort.Strings(violations)
	t.Errorf("%d claim(s) of the onboarding guide are wrong about the tree:\n\t%s",
		len(violations), strings.Join(violations, "\n\t"))
}

// TestTopLevelEntriesLeaveOutWhatGitIgnores pins the rule above on a scratch
// repository, because the real one has no `bin/` on a fresh clone and a check
// there would pass without looking.
func TestTopLevelEntriesLeaveOutWhatGitIgnores(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	git := exec.CommandContext(t.Context(), "git", "init", "--quiet", root)
	if out, err := git.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	for _, dir := range []string{"bin/ci-local", "cmd/server"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o750); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("bin/\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	top := topLevelEntries(t, root)

	if _, ok := top["cmd"]; !ok {
		t.Errorf("topLevelEntries left out cmd, which nothing ignores: %v", top)
	}

	if _, ok := top["bin"]; ok {
		t.Errorf("topLevelEntries kept bin, which .gitignore names: %v", top)
	}

	if got := pathsIn("Each lane writes to `bin/ci-local/x.log` and `cmd/server`.", top); len(got) != 1 ||
		got[0].path != "cmd/server" {
		t.Errorf("pathsIn read %v, and only cmd/server is a repository path", got)
	}
}
