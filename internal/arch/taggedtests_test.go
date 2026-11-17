package arch_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/arch"
)

// A test file behind a build tag is absent from an untagged run, and a package
// whose test files are all absent prints ok.
//
// `go test -short ./internal/authz/` prints ok over 117 skipped cases. That is
// how a lockout rule shipped with the door it existed to close still open: nine
// new cases had been written and every one of them skipped. CI is not affected,
// because .github/workflows/ci.yml passes the whole tag set. The defect is that
// a developer's green is not CI's green, and the developer reads theirs first.
//
// So `make test-unit` names every tag-gated package again with its tag, and this
// is what keeps that list complete. A package that grows a tagged test file and
// is not added to the recipe fails here on the day the file lands.
//
// What it does not hold: whether the tests inside then run. A suite can be
// reached by its tag and still skip for a missing DSN, which is what
// AUTHZ_REQUIRED_ENGINES is for.
const taggedTestRoots = "internal, sdk, adapters, cmd, test"

// taggedTestDirs are the directories walked, relative to the repository root.
//
//nolint:gochecknoglobals // a closed list, read by two cases.
var taggedTestDirs = []string{"internal", "sdk", "adapters", "cmd", "test"}

// tagsRunByAnotherTarget names the make target that runs a tag, where it is not
// test-unit. The entry is a claim, not an exemption: the gate reads the named
// target's own recipe and refuses it when that target does not reach the
// package either.
//
// postgres is here because the engine needs a container. test-engines starts the
// fixture and sets AUTHZ_REQUIRED_ENGINES to both engines, so a run that reached
// only SQLite fails there rather than passing quietly.
//
//nolint:gochecknoglobals // a closed lookup table, never written to.
var tagsRunByAnotherTarget = map[string]string{
	"postgres":      "test-engines",
	"printer":       "test-printer",
	"noreference":   "test-printer",
	"nodevtools":    "test-printer",
	"noothermodels": "test-printer",
	"nodashboards":  "test-printer",
}

// taggedTestTarget is the make target that owes a tag a run.
func taggedTestTarget(tag string) string {
	if target, ok := tagsRunByAnotherTarget[tag]; ok {
		return target
	}

	return "test-unit"
}

// A tagged test package: where it lives, and the tag that reaches it.
type taggedPackage struct {
	dir  string
	tag  string
	file string
}

// positiveTags returns the tags a //go:build line requires to be SET, so that
// the file is compiled only when they are given. A negated term is the
// complement of one of those, and a file carrying only negations is in every
// untagged run already, so it is not a package this gate is about.
//
// The expression is read as words. A constraint this repository does not write,
// such as one mixing || with a negation, contributes each of its plain
// identifiers, and the cost of that is naming a package in the recipe that did
// not strictly need it.
func positiveTags(constraint string) []string {
	var out []string

	for field := range strings.FieldsSeq(constraint) {
		field = strings.Trim(field, "()")

		switch field {
		case "", "&&", "||":
			continue
		}

		if strings.HasPrefix(field, "!") {
			continue
		}

		// A release tag, a GOOS or a GOARCH is the toolchain's own vocabulary
		// and is not something a make recipe passes with -tags.
		if strings.HasPrefix(field, "go1.") || knownPlatformTerm(field) {
			continue
		}

		out = append(out, field)
	}

	return out
}

// knownPlatformTerm reports whether a build term is one the toolchain sets from
// the target rather than one a recipe passes.
func knownPlatformTerm(term string) bool {
	switch term {
	case "windows", "linux", "darwin", "js", "wasip1", "unix",
		"amd64", "arm64", "arm", "386", "wasm", "cgo", "race", "ignore":
		return true
	}

	return false
}

// buildConstraintOf returns the //go:build expression of a Go file, or "".
// Only the lines before the package clause are read, which is where the
// toolchain reads it too.
func buildConstraintOf(t *testing.T, path string) string {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	for line := range strings.SplitSeq(string(body), "\n") {
		line = strings.TrimSpace(line)

		if after, ok := strings.CutPrefix(line, "//go:build"); ok {
			return strings.TrimSpace(after)
		}

		if strings.HasPrefix(line, "package ") {
			return ""
		}
	}

	return ""
}

// taggedTestPackages walks the roots and returns every test file that is
// reachable only with a tag, and the count of test files read.
func taggedTestPackages(t *testing.T, root string) ([]taggedPackage, int) {
	t.Helper()

	var (
		found []taggedPackage
		read  int
	)

	for _, dir := range taggedTestDirs {
		walkErr := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
				return nil
			}

			read++

			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}

			rel = filepath.ToSlash(rel)

			for _, tag := range positiveTags(buildConstraintOf(t, path)) {
				found = append(found, taggedPackage{dir: filepath.ToSlash(filepath.Dir(rel)), tag: tag, file: rel})
			}

			return nil
		})
		if walkErr != nil {
			t.Fatalf("walking %s: %v", dir, walkErr)
		}
	}

	sort.Slice(found, func(i, j int) bool { return found[i].file < found[j].file })

	return found, read
}

// recipeOf returns the lines of one make target's recipe. A recipe is the
// indented block under the target header, and it ends at the first line that is
// neither indented nor blank.
func recipeOf(t *testing.T, root, target string) []string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("reading the Makefile: %v", err)
	}

	var (
		lines []string
		in    bool
	)

	for line := range strings.SplitSeq(string(body), "\n") {
		if strings.HasPrefix(line, target+":") {
			in = true

			continue
		}

		if !in {
			continue
		}

		if strings.TrimSpace(line) == "" {
			break
		}

		if !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, " ") {
			break
		}

		lines = append(lines, withDetectedPrinterTags(t, root, body, expandMakeVariables(body, line)))
	}

	return lines
}

// withDetectedPrinterTags adds to a recipe line the tags it asks
// detect-build-tags for, when it asks about the printer's configuration. Those
// tags are not written in the Makefile, because the printer configuration
// implies them: nodashboards is one. The answer is the build-printer tag set,
// which runs the same detection the recipe does.
func withDetectedPrinterTags(t *testing.T, root string, makefile []byte, line string) string {
	t.Helper()

	printerConfig := makeVariableValue(makefile, "PRINTER_CONFIG")
	if printerConfig == "" || !strings.Contains(line, "detect-build-tags") ||
		!strings.Contains(line, printerConfig) {
		return line
	}

	sets, err := arch.TagSets(root)
	if err != nil {
		t.Fatalf("reading what the printer configuration implies: %v", err)
	}

	return line + " " + sets["build-printer"]
}

// makeVariableUse is one variable a recipe line expands.
var makeVariableUse = regexp.MustCompile(`\$\(([A-Z_]+)\)`)

// expandMakeVariables replaces each variable a recipe line names with the words
// the Makefile gives it. The printer tests take their tags from PRINTER_TAGS,
// so a line that names the variable reaches the tags it holds.
func expandMakeVariables(makefile []byte, line string) string {
	for range 5 {
		expanded := makeVariableUse.ReplaceAllStringFunc(line, func(use string) string {
			name := makeVariableUse.FindStringSubmatch(use)[1]

			return makeVariableValue(makefile, name)
		})
		if expanded == line {
			break
		}

		line = expanded
	}

	return line
}

// makeVariableValue returns the words of every assignment to one variable,
// with the comment at the end of each line removed.
func makeVariableValue(makefile []byte, name string) string {
	var words []string

	for line := range strings.SplitSeq(string(makefile), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != name {
			continue
		}

		switch fields[1] {
		case "=", ":=", "+=", "?=":
		default:
			continue
		}

		value, _, _ := strings.Cut(strings.Join(fields[2:], " "), "#")
		words = append(words, strings.Fields(value)...)
	}

	return strings.Join(words, " ")
}

// recipeReaches reports whether a recipe line runs the given package with the
// given tag. Both halves have to be on the one line: a `-tags` somewhere in the
// recipe and a package operand somewhere else is two different commands.
func recipeReaches(line, dir, tag string) bool {
	if !strings.Contains(line, "-tags") {
		return false
	}

	var tagged bool

	for field := range strings.FieldsSeq(strings.NewReplacer(`"`, " ", "'", " ").Replace(line)) {
		if field == tag {
			tagged = true

			break
		}
	}

	if !tagged {
		return false
	}

	for field := range strings.FieldsSeq(line) {
		operand, ok := strings.CutPrefix(field, "./")
		if !ok {
			continue
		}

		operand = strings.TrimSuffix(strings.TrimSuffix(operand, "..."), "/")
		if operand == "" || dir == operand || strings.HasPrefix(dir, operand+"/") {
			return true
		}
	}

	return false
}

// TestEveryTagGatedTestPackageIsRunByTestUnit is the gate. A package whose tests
// only exist behind a tag is named again in the recipe, with that tag.
func TestEveryTagGatedTestPackageIsRunByTestUnit(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	tagged, read := taggedTestPackages(t, root)

	requireScanned(t, "test files under "+taggedTestRoots, read, 300)
	requireScanned(t, "test files reachable only with a build tag", len(tagged), 3,
		"A file that lost its //go:build line lowers this. Edit the floor in the same change.")

	var violations []string

	for _, pkg := range tagged {
		target := taggedTestTarget(pkg.tag)
		recipe := recipeOf(t, root, target)

		requireScanned(t, "recipe lines under the Makefile's "+target+" target", len(recipe), 1,
			"The target was renamed or its recipe moved, and this gate then reads nothing.")

		reached := false

		for _, line := range recipe {
			if recipeReaches(line, pkg.dir, pkg.tag) {
				reached = true

				break
			}
		}

		if !reached {
			violations = append(violations, fmt.Sprintf(
				"%s is compiled only with -tags %s, and make %s runs no line naming %s with that tag.\n"+
					"    An untagged run reports ok over a package whose tests were all absent.",
				pkg.file, pkg.tag, target, pkg.dir))
		}
	}

	if len(violations) > 0 {
		t.Errorf("%d tag-gated test package(s) are absent from make test-unit:\n  %s",
			len(violations), strings.Join(violations, "\n  "))
	}
}

// TestTheTagGatedRecipeNamesNothingThatIsGone is the other direction. A tag or a
// package named in the recipe and carried by no test file is a run that proves
// nothing, and it is how this list stops describing the tree.
func TestTheTagGatedRecipeNamesNothingThatIsGone(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	tagged, _ := taggedTestPackages(t, root)
	recipe := recipeOf(t, root, "test-unit")

	requireScanned(t, "recipe lines under the Makefile's test-unit target", len(recipe), 1)

	var violations []string

	for _, line := range recipe {
		if !strings.Contains(line, "-tags") {
			continue
		}

		matched := false

		for _, pkg := range tagged {
			if recipeReaches(line, pkg.dir, pkg.tag) {
				matched = true

				break
			}
		}

		if !matched {
			violations = append(violations, strings.TrimSpace(line))
		}
	}

	if len(violations) > 0 {
		t.Errorf("%d line(s) of make test-unit pass a tag no test file under %s requires:\n  %s",
			len(violations), taggedTestRoots, strings.Join(violations, "\n  "))
	}
}
