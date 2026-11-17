package arch_test

import (
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// AGENTS.md lists "an unanchored .gitignore rule excluding a directory that
// exists locally" among the things that break quietly here, and nothing read a
// .gitignore at all.
//
// The rule that cost the most was the other shape. `internal/frontend/*`
// survived the quarantine moving that directory, so `make build` wrote six
// artefacts nobody could see were untracked. The rules
// were repointed on 2026-08-27 and nothing stopped the next move repeating it.
//
// What is checkable is narrow and it is the shape that failed: a rule naming a
// path whose parent directory is not in the tree is pointed at nothing. The
// leaf is deliberately not checked. `docs/dist/` and `frontend/node_modules/`
// are absent on a fresh clone and are correct.
const gitignoreFloor = 4

// gitignoreParentAbsent names a rule whose parent a build creates rather than
// the tree carrying it, against what creates it. It is checked both ways: an
// entry naming a parent the index carries fails, so the list cannot outlive
// what it describes. Presence on disk is not that signal. A build directory is
// absent on a fresh clone and present on a machine that has built, and the
// entry is right in both places.
//
//nolint:gochecknoglobals // a table read by one test.
var gitignoreParentAbsent = map[string]string{
	"docs/public": "docs/scripts/build-swagger.mjs:21 resolves `public/swagger` and " +
		"writes the bundled Swagger UI into it, so the directory appears with the " +
		"first `make docs` and is absent on a fresh clone.",
}

// TestEveryIgnoreRuleNamesAPathThatExists is the gate.
func TestEveryIgnoreRuleNamesAPathThatExists(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	files := ignoreFiles(t, root)

	requireScanned(t, ".gitignore files in the tree", len(files), gitignoreFloor,
		"Either they moved, or the walk stopped reaching them.")

	var (
		violations []string
		judged     int
		pointedAt  = map[string]bool{}
	)

	for _, file := range files {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}

		base := path.Dir(file)
		if base == "." {
			base = ""
		}

		for _, parent := range ignoredParents(string(body), base) {
			judged++
			pointedAt[parent] = true

			_, excused := gitignoreParentAbsent[parent]

			if tracked(t, root, parent) {
				if excused {
					violations = append(violations, "gitignoreParentAbsent names "+parent+
						", which the tree carries. Delete the entry.")
				}

				continue
			}

			if excused {
				continue
			}

			if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(parent))); statErr == nil {
				continue
			}

			violations = append(violations, file+": a rule names "+parent+
				"/, and there is no such directory.\n\t  A rule pointed at a path that moved goes on"+
				" ignoring nothing, and the output it\n\t  was written for becomes untracked with"+
				" nothing saying so. Repoint it, delete it,\n\t  or record it in gitignoreParentAbsent"+
				" against what creates the directory.")
		}
	}

	for parent := range gitignoreParentAbsent {
		if !pointedAt[parent] {
			violations = append(violations, "gitignoreParentAbsent names "+parent+
				", which no rule points at any more. Delete the entry.")
		}
	}

	requireScanned(t, "ignore rules naming a path", judged, 6,
		"Either the rules moved, or ignoredParents stopped reading them.")

	reportDocs(t, violations, "an ignore rule names a path the tree has")
}

// TestNoDirectoryIsNeitherTrackedNorIgnored is the other half of the same rot.
//
// The rules above are read against the tree. This reads the tree against the
// rules, and the shape it refuses is a whole directory that is neither in the
// index nor ignored: invisible to a fresh clone, present on this machine, and
// named by nothing. `internal/frontend/`, `ui/` and `frontend/.astro/` were all
// three of those, left behind by two retired frontends, and the last of them
// made the application's eslint fail on any checkout that carried it.
//
// git answers both questions, because git is what a clone and a build obey.
// `--directory` collapses a wholly untracked directory to one entry, so a new
// package is reported once rather than once per file in it.
func TestNoDirectoryIsNeitherTrackedNorIgnored(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	cmd := exec.CommandContext(t.Context(), "git", "ls-files", "--others",
		"--exclude-standard", "--directory", "--no-empty-directory")
	cmd.Dir = root

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("listing the untracked paths: %v", err)
	}

	var violations []string

	for line := range strings.SplitSeq(normaliseNewlines(string(out)), "\n") {
		entry := strings.TrimSpace(line)
		if entry == "" || !strings.HasSuffix(entry, "/") {
			continue
		}

		violations = append(violations, entry+" is neither tracked nor ignored.\n\t  "+
			"A fresh clone does not carry it and no rule says it belongs here.\n\t  "+
			"Commit it if the tree owns it, ignore it if a build writes it, or delete it.")
	}

	reportDocs(t, violations, "the rule that a directory is tracked or ignored")
}

// tracked says whether the index carries anything under a path. It is the
// question "does a clone get this", which os.Stat cannot answer: a build output
// is on this disk and in no clone.
func tracked(t *testing.T, root, dir string) bool {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", "ls-files", "--", dir)
	cmd.Dir = root

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("reading the index for %s: %v", dir, err)
	}

	return len(strings.TrimSpace(string(out))) > 0
}

// ignoredParents is every directory a rule in one .gitignore expects to find,
// relative to the repository root.
//
// A rule with no slash matches at any depth and names no path. A rule whose
// parent carries a wildcard names no single path either. Both are skipped.
func ignoredParents(body, base string) []string {
	var parents []string

	seen := map[string]bool{}

	for line := range strings.SplitSeq(normaliseNewlines(body), "\n") {
		rule := strings.TrimSpace(line)
		if rule == "" || strings.HasPrefix(rule, "#") {
			continue
		}

		rule = strings.TrimPrefix(rule, "!")
		rule = strings.TrimSuffix(rule, "/")
		rule = strings.TrimPrefix(rule, "/")

		if !strings.Contains(rule, "/") {
			continue
		}

		parent := path.Dir(rule)

		// "." is the .gitignore's own directory, which is always there.
		if parent == "." || strings.ContainsAny(parent, "*?[") {
			continue
		}

		full := path.Join(base, parent)
		if seen[full] {
			continue
		}

		seen[full] = true

		parents = append(parents, full)
	}

	return parents
}

// ignoreFiles is every .gitignore the repository carries. A directory a build
// or an install writes is skipped: a Python virtualenv and a node_modules each
// ship one, and reading those makes the gate judge somebody else's rules.
func ignoreFiles(t *testing.T, root string) []string {
	t.Helper()

	skip := map[string]bool{
		"node_modules": true, ".venv": true, "dist": true, ".astro": true,
		".git": true, "_": true, "vendor": true,
	}

	var found []string

	err := filepath.WalkDir(root, func(p string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			if skip[entry.Name()] {
				return filepath.SkipDir
			}

			return nil
		}

		if entry.Name() != ".gitignore" {
			return nil
		}

		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}

		found = append(found, filepath.ToSlash(rel))

		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}

	return found
}
