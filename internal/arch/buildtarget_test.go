package arch_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// buildFiles are every file that tells a machine how to compile this project.
//
// A release pipeline runs once a tag, and a tag is the worst place to learn that
// the command moved. Four of the eight published binaries were unbuildable
// between 62971c6, which moved the entry point under cmd/, and 95c85d8, which
// added this gate: Dockerfile.build still said `go build .`, and the repository
// root holds no Go file. Nothing failed, because nothing ran.
var buildFiles = []string{
	".github/dockerfiles/Dockerfile.build",
	".github/workflows/release.yml",
	".github/workflows/ci.yml",
	".github/workflows/test-build.yml",
	".github/workflows/wizard-smoke.yml",
	"docker/server/Dockerfile",
	"Makefile",
}

// goBuildInvocation finds a `go build` and hands back everything after it. A
// `go build` behind a `#` is a comment, not a build.
var goBuildInvocation = regexp.MustCompile(`(?m)^[^#]*\bgo build\b(.*)$`)

// TestEveryBuildFileNamesARealCommand fails when a build file compiles a package
// path that holds no `package main`.
//
// `go build .` in a directory with no Go files is not a slow failure. It fails
// the moment somebody runs it, and nothing in the release path runs it before a
// tag. The gate needs no toolchain: it reads the operand, resolves it against
// the tree, and asks whether a command is there.
func TestEveryBuildFileNamesARealCommand(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	var (
		violations []string
		checked    int
	)

	for _, name := range buildFiles {
		path := filepath.Join(root, filepath.FromSlash(name))

		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s is listed as a build file and cannot be read: %v", name, err)
		}

		// A line ending in a backslash continues, and the operand is regularly
		// on the line after it. Join them first: the invocation this gate was
		// written for is a continuation, and reading a line at a time is exactly
		// how it stayed invisible.
		text := strings.ReplaceAll(string(raw), "\\"+"\n", " ")

		for _, match := range goBuildInvocation.FindAllStringSubmatch(text, -1) {
			target, ok := operandAfterOutput(strings.Fields(match[1]))
			if !ok {
				continue
			}

			checked++

			if reason := whyNotACommand(root, target); reason != "" {
				violations = append(violations, name+" builds "+target+", and "+reason)
			}
		}
	}

	requireScanned(t, "build invocations naming a relative package path", checked, 1,
		"Either the invocations moved, or the pattern stopped matching them.")

	if len(violations) > 0 {
		t.Errorf("%d build invocation(s) name a package that cannot be built:\n  %s",
			len(violations), strings.Join(violations, "\n  "))
	}

	t.Logf("%d relative build target(s) across %d build file(s) name a real command",
		checked, len(buildFiles))
}

// operandAfterOutput takes the package operand as the word after the `-o` pair,
// and reports whether it is a relative path this gate can resolve.
//
// The last word is the obvious answer and the wrong one: several invocations
// here chain into `&& upx ...`, so the line ends in somebody else's argument.
// Every `go build` this project writes passes `-o`, and the operand follows the
// output path, so that pair is the anchor.
func operandAfterOutput(fields []string) (string, bool) {
	for i, field := range fields {
		if field != "-o" || i+2 >= len(fields) {
			continue
		}

		target := fields[i+2]
		if strings.HasPrefix(target, "./") || target == "." {
			return target, true
		}

		return "", false
	}

	return "", false
}

// whyNotACommand returns the sentence to put after "and", or "" when the path
// does hold a command.
func whyNotACommand(root, target string) string {
	dir := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(target, "./")))

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "that directory does not exist"
	}

	fset := token.NewFileSet()

	var sawGo bool

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}

		sawGo = true

		file, parseErr := parser.ParseFile(fset, filepath.Join(dir, entry.Name()), nil, parser.PackageClauseOnly)
		if parseErr != nil {
			continue
		}

		if file.Name.Name == "main" {
			return ""
		}
	}

	if !sawGo {
		return "that directory holds no Go file, so the build answers `no Go files`"
	}

	return "no file there declares `package main`, so the build produces no binary"
}
