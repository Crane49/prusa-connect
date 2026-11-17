package arch_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestTheRepositoryRootCarriesNoBuildArtefacts holds go-standards.md's building
// rule. `go build ./cmd/thing` writes the binary into the working directory, so
// run from the root it leaves one there under the command's name.
//
// .gitignore covers these, so they never reach a commit. What they reach is the
// working tree, and a root full of binaries is where a stray file stops being
// noticed. The rule is `go run` for a one-shot tool and `-o bin/` for a binary
// worth keeping.
func TestTheRepositoryRootCarriesNoBuildArtefacts(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read the repository root: %v", err)
	}

	// cmd/ is the only place this repository builds from, so its directory
	// names are what a Linux build with no extension would be called.
	commands := commandNames(t, root)

	// This gate expects zero findings on every healthy run, so its green says
	// nothing about whether it read anything. Both input sets carry a floor:
	// the files in the root it judges, and the command names half the judgement
	// is made of. A cmd/ that yielded nothing would leave every extensionless
	// Linux binary in the root unnamed and the run still green.
	requireScanned(t, "files in the repository root", len(entries), 1)
	requireScanned(t, "commands under cmd/", len(commands), 1)

	var found []string

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if isBuildArtefact(entry.Name(), commands) {
			found = append(found, entry.Name())
		}
	}

	if len(found) > 0 {
		t.Errorf("the repository root carries build artefacts: %s.\n"+
			"go build ./cmd/thing writes the binary where it was run. Use go run for a\n"+
			"one-shot tool, or go build -o bin/thing. See .agents/rules/go-standards.md.",
			strings.Join(found, ", "))
	}
}

// isBuildArtefact reports whether a root-level file name is something a build
// left behind. Extensions rather than a mode check, because a Windows checkout
// reports every file as executable.
func isBuildArtefact(name string, commands []string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".exe", ".dll", ".so", ".dylib", ".test", ".out", ".a":
		return true
	}

	// A Go binary built on Linux carries no extension, so a file named after a
	// command is the other half of the check.
	return slices.Contains(commands, name)
}

// commandNames is every directory under cmd/, which is what a binary built
// from one would be called.
func commandNames(t *testing.T, root string) []string {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(root, "cmd"))
	if err != nil {
		t.Fatalf("read cmd/: %v", err)
	}

	out := make([]string, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() {
			out = append(out, entry.Name())
		}
	}

	return out
}
