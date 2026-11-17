package arch_test

import (
	"os/exec"
	"strings"
	"testing"
)

// runMake runs one make target at the repository root with one variable set,
// and returns what it printed and whether it failed.
func runMake(t *testing.T, target, variable string) (string, error) {
	t.Helper()

	makeBinary, err := exec.LookPath("make")
	if err != nil {
		t.Skipf("no make on PATH, so the Makefile cannot be run here: %v", err)
	}

	if _, err = exec.LookPath("sh"); err != nil {
		t.Skipf("no POSIX shell on PATH, so the Makefile cannot be run here: %v", err)
	}

	cmd := exec.Command(makeBinary, "-s", "--no-print-directory", target, variable)
	cmd.Dir = repoRoot(t)

	output, runErr := cmd.CombinedOutput()

	return string(output), runErr
}

// TestTheMakefileRefusesAnUnknownBuildTag holds the guard at the top of the
// Makefile. go build accepts any word as a tag, so a misspelt extra tag builds
// a binary without the part it names and says nothing.
func TestTheMakefileRefusesAnUnknownBuildTag(t *testing.T) {
	t.Parallel()

	output, err := runMake(t, "help", "EXTRA_BUILD_TAGS=observabilty")
	if err == nil {
		t.Fatalf("make accepted EXTRA_BUILD_TAGS=observabilty. What it printed:\n%s", output)
	}

	for _, want := range []string{`"observabilty"`, "observability"} {
		if !strings.Contains(output, want) {
			t.Errorf("the refusal does not name %s. What it printed:\n%s", want, output)
		}
	}
}

// TestTheMakefileAcceptsTheTagsItKnows proves the guard does not refuse the
// tags adapters/tags.json names.
func TestTheMakefileAcceptsTheTagsItKnows(t *testing.T) {
	t.Parallel()

	output, err := runMake(t, "help", "EXTRA_BUILD_TAGS=observability postgres")
	if err != nil {
		t.Fatalf("make refused two tags it knows: %v\n%s", err, output)
	}
}
