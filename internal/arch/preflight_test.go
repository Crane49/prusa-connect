package arch_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The bench preflight is the check a person runs while standing at a printer
// that will not connect. It reads the Windows adapter, neighbour and firewall
// tables, so it is PowerShell and has no POSIX sibling.
//
// A script nobody can find helps nobody, and this is how that happens:
//
//   - the make target goes, and the script is reachable from a documentation
//     page alone. A developer has to know it exists before it can help them
//   - the target stays and stops handing the script to the interpreter, so it
//     execs a shell that checks nothing and exits zero
//   - the page stops naming the target, so the target is the one nobody runs
//   - `preflight` leaves .PHONY, and a file or directory of that name makes
//     make report the target up to date without running it
//
// What is deliberately not held here: whether each check is correct. Every one
// is a reading of one machine, and the evidence for each is on the old backlog and in the
// page below.
const (
	preflightScript = "scripts/bench-preflight.ps1"
	preflightPage   = "docs/src/content/docs/bench-preflight.md"
	preflightTarget = "preflight"

	// preflightVar is how the recipe spells the script. The definition is
	// checked separately, so a recipe naming the variable names the file.
	preflightVar = "$(PREFLIGHT_SCRIPT)"
)

// preflightCallers are the files that have to name the preflight, the exact
// text each one owes, and what that text is for.
//
// A Makefile describes the preflight in prose as well as wiring it, so a match
// inside a comment passes when the wiring has been deleted. `code` is what says
// to read the file with its comment lines dropped.
var preflightCallers = []struct {
	path string
	call string
	why  string
	code bool
}{
	{"Makefile", preflightTarget + ":", "the target a developer runs", true},
	{
		"Makefile", "PREFLIGHT_SCRIPT := " + preflightScript,
		"the one definition every line of the recipe resolves through", true,
	},
	// The .PHONY declaration is checked by declaredPhony below rather than
	// here. This table matches a literal, and the Makefile names every target
	// on one .PHONY line, so a literal ".PHONY: preflight" is absent from a
	// file that does declare it.
	{preflightPage, "make " + preflightTarget, "the page that tells a reader the target exists", false},
}

// declaredPhony reports whether the Makefile names target on a .PHONY line.
// make silences a target that a file or directory of the same name shadows,
// and .PHONY is what stops that. The rule is that make treats it as phony, so
// this reads the declaration's words rather than matching one spelling of it.
func declaredPhony(t *testing.T, makefile, target string) bool {
	t.Helper()

	declared := 0

	for line := range strings.SplitSeq(makefile, "\n") {
		rest, found := strings.CutPrefix(strings.TrimSpace(line), ".PHONY:")
		if !found {
			continue
		}

		for name := range strings.FieldsSeq(rest) {
			declared++

			if name == target {
				return true
			}
		}
	}

	requireScanned(t, "names on .PHONY lines in the Makefile", declared, 1,
		"The declaration moved, so this gate read nothing and proved nothing.")

	return false
}

func TestThePreflightIsReachableFromAMakeTarget(t *testing.T) {
	root := repoRoot(t)

	if _, err := os.Stat(filepath.Join(root, preflightScript)); err != nil {
		t.Fatalf("%s is missing, so nothing checks the bench: %v", preflightScript, err)
	}

	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("reading Makefile: %v", err)
	}

	if !declaredPhony(t, uncommented(string(makefile)), preflightTarget) {
		t.Errorf("Makefile does not name %q on a .PHONY line.\n"+
			"It is what keeps a file of that name from silencing the target.", preflightTarget)
	}

	for _, c := range preflightCallers {
		body, readErr := os.ReadFile(filepath.Join(root, c.path))
		if readErr != nil {
			t.Fatalf("reading %s: %v", c.path, readErr)
		}

		text := string(body)
		if c.code {
			text = uncommented(text)
		}

		if !strings.Contains(text, c.call) {
			t.Errorf("%s carries no %q outside its comments.\nIt is what holds %s.",
				c.path, c.call, c.why)
		}
	}
}

// TestThePreflightTargetHandsTheScriptToEveryInterpreter is the half a match
// anywhere in the Makefile cannot hold. The recipe hands the shell to an
// interpreter with `exec`, so an exec line that does not carry the script runs
// something else and reports its exit status as the preflight's.
//
// It was watched red against a recipe whose interpreter arguments were replaced
// with `-Command "exit 0"`, which the whole-file match above passed.
func TestThePreflightTargetHandsTheScriptToEveryInterpreter(t *testing.T) {
	recipe := preflightRecipe(t)

	handed := 0

	for line := range strings.SplitSeq(recipe, "\n") {
		if !strings.Contains(line, "exec ") {
			continue
		}

		handed++

		if !strings.Contains(line, preflightVar) {
			t.Errorf("the %s recipe execs an interpreter without %s:\n%s\nIt would report the exit "+
				"status of something that checked nothing.", preflightTarget, preflightVar, strings.TrimSpace(line))
		}
	}

	// pwsh where a cross-platform install provides it, and Windows PowerShell
	// otherwise. One exec line means one of the two paths stopped running the
	// script rather than being handed a different one.
	const interpreters = 2

	if handed != interpreters {
		t.Errorf("the %s recipe execs %d interpreter(s), and there are %d ways to run PowerShell here.",
			preflightTarget, handed, interpreters)
	}

	// The script may be absent from a checkout, and a recipe that does not look
	// hands PowerShell a path that does not resolve.
	if !strings.Contains(recipe, "! -f "+preflightVar) {
		t.Errorf("the %s recipe never tests that %s is present.", preflightTarget, preflightVar)
	}
}

// TestThePreflightTargetNamesWhichInterpreterItTook holds earlier research
// on this target: a check over a developer machine says what it observed,
// so a green line always names where it came from.
//
// The last of the three lines is the one that matters most. Without it a
// machine carrying neither interpreter runs no check and exits zero, which is
// the shape of every gate this branch has found reporting green over nothing.
func TestThePreflightTargetNamesWhichInterpreterItTook(t *testing.T) {
	recipe := preflightRecipe(t)

	announcements := []struct {
		text string
		what string
	}{
		{"through pwsh", "the interpreter a cross-platform install provides"},
		{"through Windows PowerShell", "the interpreter every Windows machine has"},
		{"did not run", "a machine with neither, which is a failure and never a skip"},
	}

	for _, a := range announcements {
		if !strings.Contains(recipe, a.text) {
			t.Errorf("the %s recipe never says %q, so it does not name %s.",
				preflightTarget, a.text, a.what)
		}
	}

	if !strings.HasSuffix(strings.TrimRight(recipe, "\r\n \t"), "exit 1") {
		t.Errorf("the %s recipe reaches its end without exit 1, so a machine that ran no check "+
			"reports success.", preflightTarget)
	}
}

// preflightRecipe returns the target's own recipe lines. A make recipe is the
// indented block under the target, and it ends at the first line that is
// neither indented nor blank.
func preflightRecipe(t *testing.T) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(repoRoot(t), "Makefile"))
	if err != nil {
		t.Fatalf("reading the Makefile: %v", err)
	}

	lines := strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n")

	start := -1

	for i, line := range lines {
		if strings.HasPrefix(line, preflightTarget+":") {
			start = i + 1

			break
		}
	}

	if start < 0 {
		t.Fatalf("the Makefile declares no %s target", preflightTarget)
	}

	var recipe []string

	for _, line := range lines[start:] {
		if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "\t") {
			break
		}

		recipe = append(recipe, line)
	}

	if len(recipe) == 0 {
		t.Fatalf("the %s target has an empty recipe, so it runs nothing", preflightTarget)
	}

	return strings.Join(recipe, "\n")
}
