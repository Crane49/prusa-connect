package arch_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A tag such as `actions/checkout@v5` is a pointer its owner can move, so the
// code a job runs could change with no commit here. Rule 10 of
// .agents/rules/security-supply-chain.md pins every action to the commit it
// names and keeps the version in a trailing comment for Dependabot and for a
// reader. A step that calls a workflow or an action from this repository
// (`./...`) is code under review already and carries no pin.
var (
	usesLine   = regexp.MustCompile(`^\s*(?:-\s+)?uses:\s*(\S+)(.*)$`)
	pinnedUses = regexp.MustCompile(`^[^@\s]+@[0-9a-f]{40}$`)
	tagComment = regexp.MustCompile(`^\s+#\s*v\d+(\.\d+)*\s*$`)
)

// TestEveryActionIsPinnedToACommit is the gate.
func TestEveryActionIsPinnedToACommit(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	var files []string
	for _, pattern := range []string{".github/workflows/*.yml", ".github/workflows/*.yaml", ".github/actions/*/action.yml"} {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	if len(files) < 10 {
		t.Fatalf(
			"found %d workflow files under .github, expected at least 10: the glob no longer matches the tree",
			len(files),
		)
	}

	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(root, file)
		for i, line := range strings.Split(string(body), "\n") {
			m := usesLine.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			ref := strings.Trim(m[1], `"'`)
			if strings.HasPrefix(ref, "./") {
				continue
			}
			if !pinnedUses.MatchString(ref) || !tagComment.MatchString(m[2]) {
				t.Errorf("%s:%d: `uses: %s%s` is not pinned.\n"+
					"Write `uses: org/action@<full commit sha> # vX.Y.Z`. Resolve the sha with\n"+
					"`gh api repos/<org>/<action>/git/ref/tags/<tag>`, and for an annotated tag\n"+
					"follow it with `gh api repos/<org>/<action>/git/tags/<sha>`.",
					rel, i+1, ref, m[2])
			}
		}
	}
}
