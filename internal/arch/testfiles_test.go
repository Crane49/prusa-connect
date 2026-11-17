package arch_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// testFuncPrefixes maps the prefix of a function go test collects to the
// parameter type that completes the signature. A function carrying both is a
// test, wherever it is written.
//
//nolint:gochecknoglobals // a closed lookup table, never written to.
var testFuncPrefixes = map[string]string{
	"Test":      "*testing.T",
	"Benchmark": "*testing.B",
	"Fuzz":      "*testing.F",
}

// skippedTreeDirs are directory names the walk does not descend into. None of
// them holds a Go package this repository builds.
//
//nolint:gochecknoglobals // a closed list, read by one case.
var skippedTreeDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"bin":          true,
	"dist":         true,
	"testdata":     true,
}

// TestEveryTestFunctionLivesInAFileGoCompilesAsATest is the third member of the
// class taggedtests_test.go and artefacts_test.go already hold: a lane that does
// not run and does not say so.
//
// The go command collects a test only from a file whose name ends in `_test.go`.
// A file named `command_test_with_coverage.go` ends in `_coverage.go`, so it is
// an ordinary source file, and the `func TestCommandQueueFlowWithCoverage` in it
// is an ordinary function. `go test -run TestCommandQueueFlowWithCoverage`
// answers `ok ... [no tests to run]`, and a caller reading the exit status sees
// a pass over nothing. That is what `make coverage-integration` reported for as
// long as the file carried that name, and the coverage percentage it printed
// beside it made the answer look measured.
//
// The name is the whole rule, so this reads names rather than intent. A helper
// that a test file calls belongs beside it under `_test.go` too, because a
// helper compiled into the package ships in the binary.
func TestEveryTestFunctionLivesInAFileGoCompilesAsATest(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	var (
		scanned int
		found   []string
	)

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			if skippedTreeDirs[entry.Name()] {
				return fs.SkipDir
			}

			return nil
		}

		name := entry.Name()

		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}

		scanned++

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}

		for _, fn := range collectedTestFuncs(t, path) {
			found = append(found, filepath.ToSlash(rel)+": "+fn)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk the repository: %v", err)
	}

	// The floor is on the files read, not on the functions found. A healthy
	// tree finds zero, so the finding count says nothing about whether the walk
	// reached anything.
	requireScanned(t, "Go files outside _test.go", scanned, 400,
		"Every package in both modules holds one, so a count near zero means the walk stopped at the root.")

	if len(found) > 0 {
		sort.Strings(found)
		t.Errorf("these functions are never collected, because their file is not a _test.go file:\n%s\n"+
			"go test reads the file name, so `go test -run Name` over one of them answers\n"+
			"\"ok ... [no tests to run]\" and the caller reads a pass. Rename the file.",
			strings.Join(found, "\n"))
	}
}

// collectedTestFuncs returns the names of top-level functions in one file that
// go test would collect if the file were named `_test.go`.
func collectedTestFuncs(t *testing.T, path string) []string {
	t.Helper()

	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	file, err := parser.ParseFile(token.NewFileSet(), path, source, parser.SkipObjectResolution)
	if err != nil {
		// A file this repository does not build is not this gate's finding.
		// The build itself refuses it.
		return nil
	}

	var out []string

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue
		}

		wantParam, collected := testFuncPrefixes[collectedPrefix(fn.Name.Name)]
		if collected && soleParamType(fn) == wantParam {
			out = append(out, fn.Name.Name)
		}
	}

	return out
}

// collectedPrefix returns the prefix of a name go test would collect, or an
// empty string. The character after the prefix must not be a lower-case letter,
// which is the rule in testing's own documentation: `Testify` is not a test.
func collectedPrefix(name string) string {
	for prefix := range testFuncPrefixes {
		rest, ok := strings.CutPrefix(name, prefix)
		if !ok || rest == "" {
			continue
		}

		if rest[0] >= 'a' && rest[0] <= 'z' {
			continue
		}

		return prefix
	}

	return ""
}

// soleParamType renders the type of a function's only parameter, as it is
// written in the source. A function with any other shape is not collected.
func soleParamType(fn *ast.FuncDecl) string {
	params := fn.Type.Params.List
	if len(params) != 1 || len(params[0].Names) != 1 || fn.Type.Results != nil {
		return ""
	}

	star, ok := params[0].Type.(*ast.StarExpr)
	if !ok {
		return ""
	}

	selector, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return ""
	}

	pkg, ok := selector.X.(*ast.Ident)
	if !ok {
		return ""
	}

	return "*" + pkg.Name + "." + selector.Sel.Name
}
