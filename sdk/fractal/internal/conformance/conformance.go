// Package conformance checks the conformance table of a fractal package. A
// table maps each MUST sentence of the Fractal specification to the test that
// proves it.
package conformance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// Row keys one sentence of the specification to the test that proves it.
type Row struct {
	// Section is the heading the sentence is under in the specification.
	Section string
	// Must is the sentence, shortened to its clause.
	Must string
	// Test is the name of the test that proves it.
	Test string
}

// TestFunctions reads the test files of the directory and returns the name of
// every function declared in them. It fails the test when the directory
// cannot be read, when a file does not parse, or when it finds no function.
func TestFunctions(t *testing.T, dir string) map[string]bool {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the package directory %s: %v", dir, err)
	}

	set := token.NewFileSet()
	found := map[string]bool{}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		file, parseErr := parser.ParseFile(set, entry.Name(), nil, 0)
		if parseErr != nil {
			t.Fatalf("read %s: %v", entry.Name(), parseErr)
		}

		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil {
				found[fn.Name.Name] = true
			}
		}
	}

	if len(found) == 0 {
		t.Fatalf("no test function was read from %s, so the table proves nothing", dir)
	}

	return found
}

// NamesRealTests fails for every row that names a test the package does not
// have.
func NamesRealTests(t *testing.T, rows []Row) {
	t.Helper()

	found := TestFunctions(t, ".")
	for _, row := range rows {
		if !found[row.Test] {
			t.Errorf("%s: %q names %s, which is not a test in this package", row.Section, row.Must, row.Test)
		}
	}
}

// IsWellFormed fails for a row with an empty cell, a row whose test name is
// not a test name, and a sentence written twice.
func IsWellFormed(t *testing.T, rows []Row) {
	t.Helper()

	seen := map[string]string{}
	for _, row := range rows {
		if row.Section == "" || row.Must == "" || row.Test == "" {
			t.Errorf("an empty cell in row %+v", row)
		}

		if !strings.HasPrefix(row.Test, "Test") {
			t.Errorf("%s names %s, which is not a test name", row.Must, row.Test)
		}

		if first, ok := seen[row.Must]; ok {
			t.Errorf("the rule %q is in the table twice, as %s and %s", row.Must, first, row.Test)
		}

		seen[row.Must] = row.Test
	}
}
