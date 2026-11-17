package arch_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// A `make <name>` in a document is an instruction, and a reader runs it.
// Earlier research asks for this check after three of them were
// found naming targets deleted in `808195c`. `TestEveryCitedLineExists` reads a
// `path:N` and never a command, so no other test reads a command.
//
// The scan is every markdown file git tracks or would add, inside
// a code span or a shell-tagged fence. A bare fence is skipped because it
// carries pasted output as often as a command: earlier research quotes
// `make is /c/Program Files (x86)/GnuWin32/bin/make` from `type -a make`, and
// `is` is not a target anybody wrote.
//

// makeTargetFloor is the number of `make <name>` commands the tree carried when
// this gate was written. It is the arming check: a regex that stops matching
// leaves a green test over an empty read.
const makeTargetFloor = 600

// makeTargetNotBuilt names a target the documents write against the reason it
// resolves to nothing. Two shapes are here and no third is accepted: a record
// naming a gate it does not build, and a record naming a target that was
// deleted and saying so.
//
// It is checked both ways. An entry naming a target a Makefile declares fails,
// so an allowance cannot hide a working target; an entry no document names
// fails, so it cannot outlive what it excuses.
//
//nolint:gochecknoglobals // a table two tests read; there is nowhere cheaper.
var makeTargetNotBuilt = map[string]string{
	"bench":               "ADR-20260824-nyw rule 8 states the benchmark rule and nothing builds the target yet",
	"generate-mqtt-types": "deleted in 808195c with cmd/generate-mqtt-schema. Four documents name it to say it is gone",
	"generate-sse-types":  "deleted in 808195c with cmd/generate-sse-schema. The same four documents",
	"up": "deleted as a second name for `server-up`. docs/CHANGELOG.md names it in entries " +
		"about the server that name started",
}

// makefileTargetHeader matches a target header. It is the pattern
// scripts/check-makefile.sh check 6 uses for the same question: a name at the
// start of a line, a colon, and anything but `=` after it, so a variable
// assignment is not a target.
var makefileTargetHeader = regexp.MustCompile(`^([A-Za-z0-9_][A-Za-z0-9_.-]*):([^=]|$)`)

// makeCommand matches a `make` invocation at the start of a command, after a
// shell separator, or after `&&`. The flag group lets `make -n pre-commit`
// resolve to `pre-commit` rather than to `-n`. A `-C <dir>` is captured, so
// `make -C extensions check` is judged against extensions/Makefile.
var makeCommand = regexp.MustCompile(
	`(?:^|[;&|(]\s*|&&\s*)make\s+(?:-C\s+([A-Za-z0-9_./-]+)\s+|-[A-Za-z-]+\s+)*([A-Za-z0-9_][A-Za-z0-9_.-]*)`)

// subMakefiles are the Makefiles a document reaches with `make -C <dir>`. A
// target of one is keyed `<dir>:<name>`, so it never answers for a root target
// of the same name.
//
//nolint:gochecknoglobals // a closed list, read by one function.
var subMakefiles = []string{"extensions"}

// markdownFence matches a fence and captures its language tag.
var markdownFence = regexp.MustCompile("^\\s*```(\\S*)")

// shellFences are the language tags whose contents are commands. A tag outside
// this set is Go, YAML, JSON or a diff, where `make` is not an instruction.
//
//nolint:gochecknoglobals // a set, read once per line.
var shellFences = map[string]struct{}{
	"bash": {}, "sh": {}, "shell": {}, "zsh": {}, "console": {}, "shell-session": {},
}

// codeSpan matches an inline code span, which is where most of these are
// written: "run `make test-unit` before pushing".
var codeSpan = regexp.MustCompile("`([^`\n]+)`")

// TestEveryMakeCommandInTheDocumentsResolves is the gate. A document that tells
// a reader to run a target names one that exists, or it names one this file
// excuses with a reason.
func TestEveryMakeCommandInTheDocumentsResolves(t *testing.T) {
	t.Parallel()

	targets := everyMakefileTarget(t)
	found := makeCommandsInDocuments(t)

	total := 0
	for _, sites := range found {
		total += len(sites)
	}

	requireScanned(t, "`make <name>` commands in the markdown files",
		total, makeTargetFloor,
		"Either the command pattern stopped matching, or the file walk stopped finding\n"+
			"markdown. Every command below would then be judged against an empty read.")

	var violations []string

	for name, sites := range found {
		if _, ok := targets[name]; ok {
			continue
		}

		if _, excused := makeTargetNotBuilt[name]; excused {
			continue
		}

		for _, at := range sites {
			violations = append(violations, fmt.Sprintf(
				"%s writes `make %s`, and no Makefile declares that target", at, name))
		}
	}

	reportMakeCommands(t, violations)
}

// TestEveryUnbuiltMakeTargetIsStillUnbuiltAndStillNamed checks the allowance
// table both ways. Without it an entry survives the target being built, and an
// entry survives the last document that needed it.
func TestEveryUnbuiltMakeTargetIsStillUnbuiltAndStillNamed(t *testing.T) {
	t.Parallel()

	targets := everyMakefileTarget(t)
	found := makeCommandsInDocuments(t)

	var violations []string

	for name, reason := range makeTargetNotBuilt {
		if at, ok := targets[name]; ok {
			violations = append(violations, fmt.Sprintf(
				"makeTargetNotBuilt excuses %q as %q, and %s declares it", name, reason, at))
		}

		if len(found[name]) == 0 {
			violations = append(violations, fmt.Sprintf(
				"makeTargetNotBuilt excuses %q and no document writes `make %s` any more", name, name))
		}
	}

	reportMakeCommands(t, violations)
}

// everyMakefileTarget is every target of every Makefile in the tree, against
// the file that declares it. The root Makefile is the only one.
func everyMakefileTarget(t *testing.T) map[string]string {
	t.Helper()

	root := repoRoot(t)
	targets := map[string]string{}

	dirs := append([]string{""}, subMakefiles...)

	for _, dir := range dirs {
		makefile := filepath.ToSlash(filepath.Join(dir, "Makefile"))

		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(makefile)))
		if err != nil {
			t.Fatalf("read %s: %v", makefile, err)
		}

		for line := range strings.SplitSeq(string(body), "\n") {
			match := makefileTargetHeader.FindStringSubmatch(line)
			if match == nil {
				continue
			}

			name := match[1]
			if dir != "" {
				name = dir + ":" + name
			}

			if _, seen := targets[name]; !seen {
				targets[name] = makefile
			}
		}
	}

	requireScanned(t, "targets across the Makefiles", len(targets), 60,
		"The header pattern stopped matching, so every command would be reported as\n"+
			"naming no target.")

	return targets
}

// makeCommandsInDocuments is every `make <name>` the markdown files write,
// against the places that write it.
func makeCommandsInDocuments(t *testing.T) map[string][]string {
	t.Helper()

	root := repoRoot(t)
	found := map[string][]string{}

	// The files git tracks, and the new ones it does not ignore: what a commit
	// carries and CI reads. A gitignored note on one machine is no document.
	out, err := exec.CommandContext(t.Context(), "git", "-C", root,
		"ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", "*.md").Output()
	if err != nil {
		t.Fatalf("list the markdown files: %v", err)
	}

	for rel := range strings.SplitSeq(strings.TrimRight(string(out), "\x00"), "\x00") {
		body, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if errors.Is(readErr, fs.ErrNotExist) {
			// Deleted in the working tree and not yet committed.
			continue
		}

		if readErr != nil {
			t.Fatalf("read %s: %v", rel, readErr)
		}

		for name, line := range makeCommandsIn(string(body)) {
			for _, number := range line {
				found[name] = append(found[name], fmt.Sprintf("%s:%d", rel, number))
			}
		}
	}

	return found
}

// makeCommandsIn reads one document. A command is read inside a code span, or
// anywhere inside a fence whose language tag says the fence holds commands.
func makeCommandsIn(body string) map[string][]int {
	found := map[string][]int{}
	inFence := false
	isShell := false

	for number, line := range strings.Split(body, "\n") {
		if match := markdownFence.FindStringSubmatch(line); match != nil {
			if inFence {
				inFence, isShell = false, false
			} else {
				_, isShell = shellFences[match[1]]
				inFence = true
			}

			continue
		}

		var texts []string

		switch {
		case inFence && isShell:
			texts = []string{line}
		case inFence:
			continue
		default:
			for _, span := range codeSpan.FindAllStringSubmatch(line, -1) {
				texts = append(texts, span[1])
			}
		}

		for _, text := range texts {
			for _, match := range makeCommand.FindAllStringSubmatch(text, -1) {
				name := match[2]
				if dir := strings.TrimSuffix(match[1], "/"); dir != "" {
					name = dir + ":" + name
				}

				found[name] = append(found[name], number+1)
			}
		}
	}

	return found
}

// reportMakeCommands fails with every violation listed, so one run names all of
// them.
func reportMakeCommands(t *testing.T, violations []string) {
	t.Helper()

	if len(violations) == 0 {
		return
	}

	sort.Strings(violations)
	t.Errorf("%d make command(s) in the documents name no target:\n\t%s",
		len(violations), strings.Join(violations, "\n\t"))
}

// TestAMakeCommandWithADirectoryIsKeyedByIt proves `make -C <dir>` is judged
// against that directory's Makefile, and that a flag still resolves to the
// target after it.
func TestAMakeCommandWithADirectoryIsKeyedByIt(t *testing.T) {
	t.Parallel()

	found := makeCommandsIn("run `make -C extensions check`, then `make -n lint`\n" +
		"and `make -C extensions/ shape`\n")

	for _, want := range []string{"extensions:check", "lint", "extensions:shape"} {
		if _, ok := found[want]; !ok {
			t.Errorf("%q not read. What was read: %v", want, found)
		}
	}

	if _, ok := found["extensions"]; ok {
		t.Errorf("the directory was read as a target: %v", found)
	}
}
