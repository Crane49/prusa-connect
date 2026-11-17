package arch_test

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The printer build is the binary that runs on the printer's own board. Only `make
// build-printer` may pass the `printer` tag or a tag that leaves a part out,
// and every such tag needs the owner's confirmation. These tests read the
// Makefile and hold it to those rules.

// printerTarget is the one target allowed to pass the printer tags.
const printerTarget = "build-printer"

// printerProfileDir is the one package allowed to read the `printer` tag. The
// tag changes default values, and that package holds them.
const printerProfileDir = "internal/kernel/buildprofile"

var (
	makeTargetHeader = regexp.MustCompile(`^([A-Za-z0-9_.-]+):([^=]|$)`)
	quotedTags       = regexp.MustCompile(`-tags[\s=]+(?:"([^"]*)"|([^\s"]+))`)
	printerTagsLine  = regexp.MustCompile(`^PRINTER_TAGS\s*(:=|\+=|\?=|=)(.*)$`)
	printerTagRule   = regexp.MustCompile(`^ADR-(\d{8}-[a-z0-9]{3}) rule (\d+)$`)
)

// makeRecipes returns the recipe lines of every target in the Makefile, by
// target. A line ending in a backslash is joined to the next one, so a
// `go build` on a continuation line is read with the command it belongs to.
func makeRecipes(t *testing.T, root string) map[string][]string {
	t.Helper()

	lines := makefileLines(t, root)
	recipes := map[string][]string{}
	target := ""

	for _, line := range lines {
		if match := makeTargetHeader.FindStringSubmatch(line); match != nil {
			target = match[1]
			if _, seen := recipes[target]; !seen {
				recipes[target] = nil
			}

			continue
		}

		switch {
		case strings.HasPrefix(line, "\t"):
			if target != "" {
				recipes[target] = append(recipes[target], line)
			}
		case strings.TrimSpace(line) == "", strings.HasPrefix(line, "#"):
			// make keeps a recipe going across blank lines and comments.
		default:
			target = ""
		}
	}

	return recipes
}

// makefileLines reads the Makefile with every continuation joined.
func makefileLines(t *testing.T, root string) []string {
	t.Helper()

	file, err := os.Open(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("open the Makefile: %v", err)
	}

	defer func() { _ = file.Close() }()

	var (
		lines   []string
		pending string
	)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if joined, ok := strings.CutSuffix(line, `\`); ok {
			pending += joined + " "

			continue
		}

		lines = append(lines, pending+line)
		pending = ""
	}

	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("read the Makefile: %v", scanErr)
	}

	return lines
}

// printerOnlyTags returns the tags on a build line that belong to the printer
// build: `printer`, every tag starting with `no`, and a reference to
// PRINTER_TAGS.
func printerOnlyTags(line string) []string {
	var found []string

	if strings.Contains(line, "$(PRINTER_TAGS)") {
		found = append(found, "$(PRINTER_TAGS)")
	}

	for _, match := range quotedTags.FindAllStringSubmatch(line, -1) {
		for tag := range strings.FieldsSeq(match[1] + " " + match[2]) {
			if tag == "printer" || strings.HasPrefix(tag, "no") {
				found = append(found, tag)
			}
		}
	}

	return found
}

// TestOnlyThePrinterTargetBuildsWithPrinterTags fails when a Makefile target
// other than build-printer compiles a binary with the printer tag or with a
// tag that leaves a part out. The normal builds keep every part and read no
// printer default. A test run may pass such a tag, because it builds no
// binary anybody installs.
func TestOnlyThePrinterTargetBuildsWithPrinterTags(t *testing.T) {
	t.Parallel()

	recipes := makeRecipes(t, repoRoot(t))

	builds := 0

	for target, lines := range recipes {
		for _, line := range lines {
			if !strings.Contains(line, "go build") {
				continue
			}

			builds++

			tags := printerOnlyTags(line)
			if target == printerTarget {
				if !slices.Contains(tags, "printer") || !slices.Contains(tags, "$(PRINTER_TAGS)") {
					t.Errorf("%s builds without the printer tag or without $(PRINTER_TAGS):\n\t%s",
						target, strings.TrimSpace(line))
				}

				continue
			}

			if len(tags) > 0 {
				t.Errorf("%s passes %s to go build. Only %s may, because every other build keeps "+
					"every part and the normal defaults:\n\t%s",
					target, strings.Join(tags, " "), printerTarget, strings.TrimSpace(line))
			}
		}
	}

	if _, ok := recipes[printerTarget]; !ok {
		t.Fatalf("the Makefile has no %s target, so this test read nothing", printerTarget)
	}

	if builds < 5 {
		t.Fatalf("the Makefile has %d go build lines, under the floor of 5, so the scan stopped reaching them",
			builds)
	}
}

// TestOnlyTheBuildProfileReadsThePrinterTag fails when a Go file outside the
// build profile package names the `printer` tag in its build constraint. The
// printer tag changes default values only. Code that the printer build leaves
// out sits behind a tag of its own in PRINTER_TAGS.
func TestOnlyTheBuildProfileReadsThePrinterTag(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	read := 0
	inProfile := 0

	for _, dir := range []string{"internal", "cmd", "adapters", "compose", "sdk", "tools", "test"} {
		walkErr := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if entry.IsDir() && (entry.Name() == "node_modules" || entry.Name() == "testdata") {
				return filepath.SkipDir
			}

			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
				return nil
			}

			read++

			if !constraintNames(buildConstraintOf(t, path), "printer") {
				return nil
			}

			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}

			if filepath.ToSlash(filepath.Dir(rel)) == printerProfileDir {
				inProfile++

				return nil
			}

			t.Errorf("%s reads the printer tag. Only %s may, because the tag changes default values "+
				"and removes no code", filepath.ToSlash(rel), printerProfileDir)

			return nil
		})
		if walkErr != nil && !os.IsNotExist(walkErr) {
			t.Fatalf("walk %s: %v", dir, walkErr)
		}
	}

	if read < 1000 {
		t.Fatalf("read %d Go files, under the floor of 1000, so the walk stopped reaching them", read)
	}

	if inProfile < 2 {
		t.Fatalf("%s has %d files that read the printer tag. It needs one for the printer build and one "+
			"for every other build", printerProfileDir, inProfile)
	}
}

// constraintNames reports whether a build constraint names the tag, set or
// negated.
func constraintNames(constraint, tag string) bool {
	for field := range strings.FieldsSeq(strings.NewReplacer("(", " ", ")", " ", "!", " ").Replace(constraint)) {
		if field == tag {
			return true
		}
	}

	return false
}

// TestEveryPrinterTagNamesItsRule fails when a tag in PRINTER_TAGS has no rule
// behind it. Each tag is added on a line of its own, and the comment on that
// line names the rule, such as `ADR-20260925-gv7 rule 27`. The rule exists in
// that record and names the tag. A tag that no Go file reads is refused too,
// because a misspelt tag leaves nothing out and the build still succeeds.
func TestEveryPrinterTagNamesItsRule(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	read := constraintTags(t, root)

	declared := 0

	for _, line := range makefileLines(t, root) {
		match := printerTagsLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}

		value, comment, _ := strings.Cut(match[2], "#")
		tags := strings.Fields(value)
		comment = strings.TrimSpace(comment)

		if match[1] != "+=" {
			declared++

			if len(tags) > 0 {
				t.Errorf("PRINTER_TAGS %s names %s. The variable starts empty, and each tag is added "+
					"with += on a line of its own", match[1], strings.Join(tags, " "))
			}

			continue
		}

		if len(tags) != 1 {
			t.Errorf("a PRINTER_TAGS line adds %d tags. Add one tag per line, so each comment names the "+
				"rule of its own tag:\n\t%s", len(tags), line)

			continue
		}

		tag := tags[0]
		if !strings.HasPrefix(tag, "no") {
			t.Errorf("PRINTER_TAGS adds %q. A tag that leaves a part out starts with no", tag)
		}

		if problem := ruleNamesTag(t, root, comment, tag); problem != "" {
			t.Errorf("PRINTER_TAGS adds %s with the comment %q: %s", tag, comment, problem)
		}

		if !read[tag] {
			t.Errorf("PRINTER_TAGS adds %s, and no Go file reads that tag, so the printer build leaves "+
				"nothing out", tag)
		}
	}

	if declared != 1 {
		t.Fatalf("the Makefile declares PRINTER_TAGS %d times, want once", declared)
	}
}

// ruleNamesTag returns why comment does not name a rule that names tag, or ""
// when it does. The comment reads `ADR-<id> rule <n>`, and rule n of that
// record mentions the tag in backticks.
func ruleNamesTag(t *testing.T, root, comment, tag string) string {
	t.Helper()

	found := printerTagRule.FindStringSubmatch(comment)
	if found == nil {
		return "the comment does not read ADR-<id> rule <n>"
	}

	files, err := filepath.Glob(filepath.Join(root, "docs", "decisions", found[1]+"-*.md"))
	if err != nil || len(files) != 1 {
		return "no record in docs/decisions has the id " + found[1]
	}

	body, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read %s: %v", files[0], err)
	}

	rule := ruleText(string(body), found[2])
	if rule == "" {
		return "the record has no rule " + found[2]
	}

	if !strings.Contains(rule, "`"+tag+"`") {
		return "rule " + found[2] + " does not name the tag"
	}

	return ""
}

// ruleText returns rule n of a record: its numbered line and the indented
// lines that continue it.
func ruleText(body, n string) string {
	var (
		rule strings.Builder
		in   bool
	)

	for line := range strings.SplitSeq(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, n+". "):
			in = true
		case in && strings.HasPrefix(line, " "):
		default:
			in = false
		}

		if in {
			rule.WriteString(line)
			rule.WriteString("\n")
		}
	}

	return rule.String()
}

// constraintTags returns every tag a Go file under internal or cmd names in its
// build constraint.
func constraintTags(t *testing.T, root string) map[string]bool {
	t.Helper()

	tags := map[string]bool{}

	for _, dir := range []string{"internal", "cmd"} {
		walkErr := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
				return nil
			}

			constraint := strings.NewReplacer("(", " ", ")", " ", "!", " ").Replace(buildConstraintOf(t, path))
			for field := range strings.FieldsSeq(constraint) {
				tags[field] = true
			}

			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", dir, walkErr)
		}
	}

	return tags
}
