package arch_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The demo is the one command a person types to see this server working, on a
// machine with no printer. internal/app holds what it does and
// TestTheDemoStandsUpAFleetACallerCanList holds that it works. This file holds
// the half that lives outside Go: whether a person can find it.
//
// Four ways it stops being reachable, each of them silent:
//
//   - the make target goes, and the command is reachable only by somebody who
//     already knows the directory under cmd/ is there
//   - the target stays and builds a different package, so `make demo` starts a
//     server with no printers and reports success
//   - `demo` leaves .PHONY, and a file or directory of that name makes make
//     report the target up to date without running it
//   - the README and the getting-started page stop naming it, which is where a
//     person who has just cloned this looks
const (
	demoTarget  = "demo"
	demoPackage = "./cmd/demo"
	demoMain    = "cmd/demo/main.go"
	demoPage    = "docs/src/content/docs/getting-started.md"
)

// demoCallers are the files that have to name the demo, the text each one owes,
// and what that text is for.
//
// The Makefile describes the target in prose as well as wiring it, so `code`
// says to read the file with its comment lines dropped: a match inside a
// comment passes when the wiring has been deleted.
var demoCallers = []struct {
	path string
	call string
	why  string
	code bool
}{
	{"Makefile", demoTarget + ":", "the target a person runs", true},
	{"README.md", "make " + demoTarget, "the first page a person who cloned this reads", false},
	{demoPage, "make " + demoTarget, "the page that says what a first run looks like", false},
}

func TestTheDemoIsReachableFromAMakeTarget(t *testing.T) {
	root := repoRoot(t)

	if _, err := os.Stat(filepath.Join(root, demoMain)); err != nil {
		t.Fatalf("%s is missing, so there is no demo to reach: %v", demoMain, err)
	}

	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("reading Makefile: %v", err)
	}

	if !declaredPhony(t, uncommented(string(makefile)), demoTarget) {
		t.Errorf("Makefile does not name %q on a .PHONY line.\n"+
			"It is what keeps a file of that name from silencing the target.", demoTarget)
	}

	for _, c := range demoCallers {
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

// TestTheDemoTargetBuildsAndRunsTheDemoCommand is the half a match anywhere in
// the Makefile cannot hold. A recipe that builds the shipping binary instead
// starts a core with no printers, prints no fleet, and exits zero.
//
// It was watched red against a recipe pointed at ./cmd/prusa-connect,
// which the whole-file match above passed.
func TestTheDemoTargetBuildsAndRunsTheDemoCommand(t *testing.T) {
	recipe := makeTargets(t, repoRoot(t))[demoTarget].body
	if recipe == "" {
		t.Fatalf("the Makefile declares no %s target, or its recipe is empty", demoTarget)
	}

	if !strings.Contains(recipe, "go build") || !strings.Contains(recipe, demoPackage+"\n") {
		t.Errorf("the %s recipe does not build %s:\n%s", demoTarget, demoPackage, recipe)
	}

	// The built artefact has to be run, not only built. A recipe that stops at
	// the compile reports success over a demo nobody saw.
	if !strings.Contains(recipe, "bin/demo") {
		t.Errorf("the %s recipe never runs what it built:\n%s", demoTarget, recipe)
	}
}
