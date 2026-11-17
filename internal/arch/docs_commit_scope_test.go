package arch_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A docs: commit carrying Go code hides that code from anybody who later looks
// for it. `e4ba9a4 feat(connectors)` describes mountConnectors in a body whose
// own diff holds neither the function nor the file, because the 141 lines that
// wrote it went in under a six-line documentation edit. Earlier research
// rule 3.
//
// The rule cannot be a path rule. A .go file whose changed lines are all
// comments is documentation, and two commits in the measured window are exactly
// that. So the gate judges the staged diff, and the diff is an argument, which
// is what lets this run the script itself over a fixture rather than a second
// copy of its logic.
const docsScopeGate = "scripts/check-docs-commit-scope.sh"

// TestTheDocsScopeGateReadsTheDiffItIsGiven runs the gate over a fixture per
// case. The three accepting cases are what stop the refusal being a script that
// refuses everything.
func TestTheDocsScopeGateReadsTheDiffItIsGiven(t *testing.T) {
	t.Parallel()

	const header = "diff --git a/internal/app/x.go b/internal/app/x.go\n" +
		"--- a/internal/app/x.go\n+++ b/internal/app/x.go\n@@ -1,1 +1,1 @@\n"

	cases := []struct {
		name    string
		subject string
		diff    string
		refused bool
	}{
		{
			name:    "a docs commit changing a declaration",
			subject: "docs: a probe",
			diff:    header + "-func old() {}\n+func replacement() {}\n",
			refused: true,
		},
		{
			name:    "a docs commit changing comments alone",
			subject: "docs: a probe",
			diff:    header + "-// the reason it was.\n+// the reason it is.\n",
			refused: false,
		},
		{
			name:    "a docs commit changing prose inside a string literal",
			subject: "docs: a probe",
			diff:    header + "-\t\"the old sentence\",\n+\t\"the new sentence\",\n",
			refused: false,
		},
		{
			name: "a style commit changing a declaration",
			// The prefix table gives style: formatting with no logic, so
			// formatting a Go file changes code lines by definition.
			subject: "style(app): a probe",
			diff:    header + "-func old() {}\n+func replacement() {}\n",
			refused: false,
		},
		{
			name:    "a docs commit with an empty diff",
			subject: "docs(backlog): a probe",
			diff:    "",
			refused: false,
		},
	}

	root := repoRoot(t)

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			diffPath := filepath.Join(t.TempDir(), "staged.diff")
			if err := os.WriteFile(diffPath, []byte(testCase.diff), 0o600); err != nil {
				t.Fatalf("write the fixture diff: %v", err)
			}

			cmd := exec.Command("sh", docsScopeGate, testCase.subject, diffPath)
			cmd.Dir = root
			out, err := cmd.CombinedOutput()

			switch {
			case testCase.refused && err == nil:
				t.Errorf("%s accepted %q over this diff, so it is inert:\n%s\n%s",
					docsScopeGate, testCase.subject, testCase.diff, out)
			case !testCase.refused && err != nil:
				t.Errorf("%s refused %q over this diff, and a gate that refuses "+
					"everything is switched off within the day:\n%s\n%s",
					docsScopeGate, testCase.subject, testCase.diff, out)
			case testCase.refused && !strings.Contains(string(out), "changes Go code"):
				t.Errorf("%s refused %q without saying why:\n%s",
					docsScopeGate, testCase.subject, out)
			}
		})
	}
}

// TestTheCommitMsgHookCallsTheDocsScopeGate is the wiring half. A gate nothing
// calls is the way this class of check dies, which is why every gate script
// here is held against its callers.
func TestTheCommitMsgHookCallsTheDocsScopeGate(t *testing.T) {
	t.Parallel()

	hook := readFile(t, filepath.Join(".githooks", "commit-msg"))

	if !strings.Contains(hook, "check-docs-commit-scope.sh") {
		t.Errorf(".githooks/commit-msg does not name %s, so it runs nowhere.", docsScopeGate)
	}
}

// readFile reads a file relative to the repository root, with line endings
// made LF so a checkout on Windows reads the same bytes.
func readFile(t *testing.T, path string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(path)))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return strings.ReplaceAll(string(body), "\r\n", "\n")
}
