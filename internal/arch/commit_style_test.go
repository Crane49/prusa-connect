package arch_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The commit-message gate of .agents/rules/git-discipline.md lives in
// scripts/check-commit-style.sh. A shell script is not reachable from Go, so
// what these cases hold is the wiring and the thresholds, which is where this
// class of gate dies.
//
// It dies in one of five ways, and each has a case below.
//
//   - the hook stops calling the script, and every commit passes because
//     nothing reads it. `make install-hooks` is the only thing that points
//     core.hooksPath at .githooks, so a clone that never ran it has no hook,
//     which is why the same script has to run at push and in CI as well
//   - a threshold is edited without the rule text moving with it, so the rule
//     a person reads and the number a machine enforces drift apart
//   - the prefix a rule permits and the prefix the gate accepts drift apart
//   - a floor is emptied, and range mode judges nothing while still printing
//     a tick. There is one floor per rule, because a rule starts at the commit
//     that added it
//   - a check lands in the script and no rule credits it, so a reader who is
//     refused cannot look up what refused them
//
// What is deliberately not held here: whether the checks are correct. That is
// proved by watching each one fail on a message written to break it, and the
// probes are recorded in earlier research and the rules in git-discipline.md.
const commitStyleScript = "scripts/check-commit-style.sh"

// commitStyleCallers are the files that have to invoke the gate, the exact call
// each one owes, and what that call is for. A gate in a hook alone is a gate for
// the author who installed the hook and for nobody else.
//
// The call is matched outside comments. All four files comment with `#`, and
// every one of them describes the gate in prose as well as calling it, so a
// match anywhere in the file passes when the call has been deleted. That was
// watched happening before these cases were trusted.
var commitStyleCallers = []struct {
	path string
	call string
	why  string
}{
	{".githooks/commit-msg", "check-commit-style.sh", "the message being written, the only moment it can be corrected"},
	{".githooks/pre-push", "make commit-style-check", "the push, for the author who did install the hooks"},
	{"Makefile", "bash scripts/check-commit-style.sh", "the target the hook and CI both call"},
	{".github/workflows/ci.yml", "make commit-style-check", "a clone that never ran make install-hooks"},
}

// uncommented drops every `#` comment line, so a file that only talks about the
// gate does not read as a file that runs it.
func uncommented(text string) string {
	var kept []string

	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}

		kept = append(kept, line)
	}

	return strings.Join(kept, "\n")
}

func TestTheCommitStyleGateIsCalledFromEveryPlaceThatNeedsIt(t *testing.T) {
	root := repoRoot(t)

	if _, err := os.Stat(filepath.Join(root, commitStyleScript)); err != nil {
		t.Fatalf("%s is missing, so nothing judges a commit message: %v", commitStyleScript, err)
	}

	for _, c := range commitStyleCallers {
		body, err := os.ReadFile(filepath.Join(root, c.path))
		if err != nil {
			t.Fatalf("reading %s: %v", c.path, err)
		}

		if !strings.Contains(uncommented(string(body)), c.call) {
			t.Errorf("%s runs no %q outside its comments.\nIt is what judges %s.",
				c.path, c.call, c.why)
		}
	}
}

// commitStyleNumbers are the thresholds the script enforces and the rule states.
// Both files carry each one, so a change to either without the other fails here.
var commitStyleNumbers = []struct {
	inScript string
	inRule   string
	what     string
}{
	{"SUBJECT_MAX=72", "72 characters", "the subject length"},
	{"BODY_MAX=20", "20 lines of prose", "the body length"},
	{"CONTRAST_MAX=600", "6.00 per thousand words", "the contrast frame budget"},
	{"WINDOW=50", "last 50 commits", "the window the frame is measured over"},
	{"WINDOW_MIN_WORDS=800", "800 prose words", "the floor under the window"},
}

func TestTheCommitStyleThresholdsMatchTheRuleThatStatesThem(t *testing.T) {
	root := repoRoot(t)

	script, err := os.ReadFile(filepath.Join(root, commitStyleScript))
	if err != nil {
		t.Fatalf("reading %s: %v", commitStyleScript, err)
	}

	rule, err := os.ReadFile(filepath.Join(root, ".agents/rules/git-discipline.md"))
	if err != nil {
		t.Fatalf("reading the rule: %v", err)
	}

	for _, n := range commitStyleNumbers {
		if !strings.Contains(string(script), n.inScript) {
			t.Errorf("%s no longer sets %s, which is %s", commitStyleScript, n.inScript, n.what)
		}

		if !strings.Contains(string(rule), n.inRule) {
			t.Errorf("git-discipline.md no longer states %q, which is %s.\n"+
				"A number a gate enforces and a rule nobody can read is not a rule.", n.inRule, n.what)
		}
	}
}

// Check 5 refuses a subject that does not open with a prefix. The list of
// prefixes it accepts is git-discipline.md's own table, and the two drift the
// moment somebody adds a prefix to one of them. `build:` was used three times
// on this branch and is in neither, which is what this case is here to surface
// the next time somebody reaches for it.
func TestTheCommitPrefixTableAndTheGateHoldTheSameList(t *testing.T) {
	root := repoRoot(t)

	script, err := os.ReadFile(filepath.Join(root, commitStyleScript))
	if err != nil {
		t.Fatalf("reading %s: %v", commitStyleScript, err)
	}

	rule, err := os.ReadFile(filepath.Join(root, ".agents/rules/git-discipline.md"))
	if err != nil {
		t.Fatalf("reading the rule: %v", err)
	}

	// The prefix table's rows are `| `+"`prefix:`"+` | when |`.
	row := regexp.MustCompile("(?m)^\\| `([a-z]+):` \\|")

	found := row.FindAllStringSubmatch(string(rule), -1)
	if len(found) < 10 {
		t.Fatalf("git-discipline.md's prefix table yielded %d rows, so this case read the wrong thing", len(found))
	}

	// The gate holds them as one alternation, so each has to appear inside it.
	alt := regexp.MustCompile(`/\^\(([a-z|]+)\)\(`)

	inGate := alt.FindStringSubmatch(string(script))
	if inGate == nil {
		t.Fatal("scripts/check-commit-style.sh carries no prefix alternation, so check 5 refuses nothing")
	}

	gate := "|" + inGate[1] + "|"

	for _, m := range found {
		if !strings.Contains(gate, "|"+m[1]+"|") {
			t.Errorf("git-discipline.md lists the prefix %q and check 5 does not accept it.\n"+
				"A commit a rule permits and a gate refuses is worse than neither.", m[1])
		}
	}

	for p := range strings.SplitSeq(inGate[1], "|") {
		if !row.MatchString("| `" + p + ":` |") {
			continue
		}

		if !strings.Contains(string(rule), "| `"+p+":` |") {
			t.Errorf("check 5 accepts the prefix %q and git-discipline.md's table does not list it.", p)
		}
	}
}

// commitStyleFloors are the assignments range mode reads to decide what it may
// judge. FLOOR is where checks 1 to 5 start and ATTRIB_FLOOR is where check 6
// does, because git-discipline.md rule 11 starts each rule at the commit that
// added it and check 6 was added later.
var commitStyleFloors = []struct {
	assign string
	checks string
}{
	{"FLOOR=", "checks 1 to 5"},
	{"ATTRIB_FLOOR=", "check 6"},
}

// A floor is what keeps a rule off every message written before it. An empty
// floor makes range mode refuse to run, and a floor naming a commit this clone
// does not hold makes it refuse as well, so neither can pass green.
func TestEveryCommitStyleFloorNamesACommitThisCloneHolds(t *testing.T) {
	root := repoRoot(t)

	body, err := os.ReadFile(filepath.Join(root, commitStyleScript))
	if err != nil {
		t.Fatalf("reading %s: %v", commitStyleScript, err)
	}

	if _, lookErr := exec.LookPath("git"); lookErr != nil {
		t.Skipf("no git on this machine, so a floor cannot be resolved: %v", lookErr)
	}

	for _, f := range commitStyleFloors {
		var floor string

		for line := range strings.SplitSeq(string(body), "\n") {
			if after, ok := strings.CutPrefix(line, f.assign); ok {
				floor = strings.TrimSpace(after)
				break
			}
		}

		if floor == "" {
			t.Errorf("%s sets no %s, so range mode judges nothing for %s",
				commitStyleScript, strings.TrimSuffix(f.assign, "="), f.checks)

			continue
		}

		cmd := exec.Command("git", "cat-file", "-e", floor+"^{commit}")
		cmd.Dir = root

		if runErr := cmd.Run(); runErr != nil {
			t.Errorf("%s%s is not a commit in this clone, so range mode refuses every run.\n"+
				"A shallow checkout does not hold it either, which is why CI checks out at depth 0.",
				f.assign, floor)
		}
	}
}

// commitStyleChecks are the checks the script's own header enumerates and the
// rule's enforcement table credits. A check added to one and not the other is a
// gate nobody can look up, which is how check 6 came to be needed.
var commitStyleChecks = []struct {
	inScript string
	inRule   string
}{
	{"#   1. an em dash in a commit message", "| 9 | check 1,"},
	{"#   2. a subject over 72 characters", "| 7 | check 2,"},
	{"#   3. a body over 20 lines of prose", "| 1, 2 | check 3,"},
	{"#   4. the contrast frame over 6.00 per thousand words", "| 10 | check 4,"},
	{"#   5. a subject that does not begin with a prefix", "| 8 | check 5,"},
	{"#   6. a Co-Authored-By: line anywhere in the message", "| check 6, a `Co-Authored-By:` line"},
	{"#   7. a fix: commit with no Fixes: trailer", "| 12 | check 7,"},
}

func TestEveryCommitStyleCheckIsNamedByTheRuleThatCreditsIt(t *testing.T) {
	root := repoRoot(t)

	script, err := os.ReadFile(filepath.Join(root, commitStyleScript))
	if err != nil {
		t.Fatalf("reading %s: %v", commitStyleScript, err)
	}

	rule, err := os.ReadFile(filepath.Join(root, ".agents/rules/git-discipline.md"))
	if err != nil {
		t.Fatalf("reading the rule: %v", err)
	}

	for _, c := range commitStyleChecks {
		if !strings.Contains(string(script), c.inScript) {
			t.Errorf("%s no longer enumerates %q in its header", commitStyleScript, c.inScript)
		}

		if !strings.Contains(string(rule), c.inRule) {
			t.Errorf("git-discipline.md's enforcement table no longer carries %q.\n"+
				"A check no rule credits is a gate a reader cannot look up.", c.inRule)
		}
	}
}

// The range CI hands both commit gates. A whole-branch range is not the range
// an event added: `origin/main..HEAD` on `restructure` is 831 commits and 13
// failures whoever pushes has not written. So no spelling of one belongs in the
// workflow.
//
// The pattern is deliberately wider than the one line that was there. Any
// remote-tracking ref on the left of a two-dot range ending at HEAD is the same
// mistake wearing another branch name.
const ciWorkflow = ".github/workflows/ci.yml"

var ciWholeBranchRange = regexp.MustCompile(`origin/[A-Za-z0-9._/-]+\.\.HEAD`)

func TestTheRangeCIJudgesIsNeverTheWholeBranch(t *testing.T) {
	root := repoRoot(t)

	body, err := os.ReadFile(filepath.Join(root, ciWorkflow))
	if err != nil {
		t.Fatalf("reading %s: %v", ciWorkflow, err)
	}

	if found := ciWholeBranchRange.FindString(uncommented(string(body))); found != "" {
		t.Errorf("%s resolves the commit range to %q.\n"+
			"That is every commit the branch carries, not the ones this event added,\n"+
			"so it fails a push on messages its author never wrote.", ciWorkflow, found)
	}
}
