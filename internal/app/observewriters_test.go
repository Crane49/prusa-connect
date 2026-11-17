package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/platform/observability"
)

// The three gates in this file are the owner's rule on a published surface: a
// name nothing serves is a lie, so it is removed or it is built. A metric
// catalog is exactly that kind of surface, and it failed the rule once already.
// A scrape taken during a four hundred printer load returned two of the
// thirty-eight families it declared, and the example in the published OpenAPI
// document promised a third that no code anywhere wrote.
//
// The check is that the composition root names the family. internal/app is the
// only package that may see both the device plane and a registry (ADR-20260824-kkr), so
// every writer is here, and each one reaches its family either through a
// seriesXxx constant, through the planeSeries table, or through defOf. All three
// are string literals in this package, and none of them is a comment: the parser
// reads the syntax tree and a name written only in prose does not count.
//
// The other half is TestEverySeriesConstantIsRead. Naming a family in a constant
// is not writing it, and `unused` does not refuse a constant nobody reads: a
// probe added one to this package and golangci-lint reported nothing. So the
// third gate counts the references itself. Between the three, a family cannot be
// declared without a writer, a writer cannot aim at a family nothing declares,
// and a writer cannot be deleted while its constant stands.

// TestEveryCatalogFamilyHasAWriter fails when the catalog declares a family the
// composition root never names.
func TestEveryCatalogFamilyHasAWriter(t *testing.T) {
	t.Parallel()

	named := seriesNamedInThisPackage(t)

	for _, def := range observability.Catalog() {
		if _, written := named[def.Name()]; written {
			continue
		}

		t.Errorf("the catalog declares %q and nothing in internal/app names it. "+
			"Write it where the thing it measures happens, or take it out of the catalog: "+
			"a family an operator can point a dashboard at and never see reads as healthy",
			def.Name())
	}
}

// TestEverySeriesThisPackageNamesIsDeclared is the same gate from the other
// side. A writer aimed at a family the catalog does not declare records nothing
// and says so in a debug line nobody reads, so a rename that reached the catalog
// and not the writer would be silent.
func TestEverySeriesThisPackageNamesIsDeclared(t *testing.T) {
	t.Parallel()

	declared := map[string]struct{}{}
	for _, def := range observability.Catalog() {
		declared[def.Name()] = struct{}{}
	}

	for name, file := range seriesNamedInThisPackage(t) {
		if _, ok := declared[name]; ok {
			continue
		}

		t.Errorf("%s names %q and the catalog does not declare it", file, name)
	}
}

// TestEverySeriesConstantIsRead refuses a metric name this package declares and
// never uses.
//
// It is what stops the gate above being satisfied by the constant alone. A
// writer deleted in a refactor leaves its seriesXxx constant behind, the catalog
// family still resolves to a name this package "carries", and the scrape goes
// quiet with every check green.
func TestEverySeriesConstantIsRead(t *testing.T) {
	t.Parallel()

	declared, uses := seriesConstantsAndUses(t)

	for name, at := range declared {
		if uses[name] > 1 {
			continue
		}

		t.Errorf("%s declares %s and nothing in internal/app reads it: "+
			"the writer is gone, so the family it names is published and unwritten", at, name)
	}
}

// seriesConstantsAndUses returns the constants in this package whose value is a
// metric name, and how many times each identifier appears. The declaration is
// one of those appearances, so a constant nothing reads counts exactly one.
func seriesConstantsAndUses(t *testing.T) (declared map[string]string, uses map[string]int) {
	t.Helper()

	declared = map[string]string{}
	uses = map[string]int{}

	forEachSourceFile(t, func(file *ast.File, name string) {
		ast.Inspect(file, func(node ast.Node) bool {
			if ident, ok := node.(*ast.Ident); ok {
				uses[ident.Name]++
			}

			spec, ok := node.(*ast.ValueSpec)
			if !ok || len(spec.Names) != 1 || len(spec.Values) != 1 {
				return true
			}

			if value, is := metricLiteral(spec.Values[0]); is {
				declared[spec.Names[0].Name] = name + " (" + value + ")"
			}

			return true
		})
	})

	return declared, uses
}

// metricLiteral reports whether an expression is a string literal in the
// catalog's namespace.
func metricLiteral(expr ast.Expr) (string, bool) {
	literal, ok := expr.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}

	value, err := strconv.Unquote(literal.Value)
	if err != nil || !strings.HasPrefix(value, observability.Namespace+"_") {
		return "", false
	}

	return value, true
}

// seriesNamedInThisPackage returns every metric name this package's own source
// carries as a string literal, with the file it is in.
//
// Test files are skipped. A name a test spells is a name an assertion made up,
// and counting one would let a family pass this gate on the strength of the
// gate itself.
func seriesNamedInThisPackage(t *testing.T) map[string]string {
	t.Helper()

	found := map[string]string{}

	forEachSourceFile(t, func(file *ast.File, name string) {
		ast.Inspect(file, func(node ast.Node) bool {
			expr, ok := node.(ast.Expr)
			if !ok {
				return true
			}

			value, is := metricLiteral(expr)
			if !is {
				return true
			}

			if _, seen := found[value]; !seen {
				found[value] = name
			}

			return true
		})
	})

	return found
}

// forEachSourceFile parses this package's own Go files and hands each one to
// visit. Test files are skipped: a name a test spells is a name an assertion
// made up, and counting one would let a family pass on the strength of the gate
// itself.
func forEachSourceFile(t *testing.T, visit func(*ast.File, string)) {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}

	fset := token.NewFileSet()

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		parsed, parseErr := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}

		visit(parsed, filepath.ToSlash(name))
	}
}
