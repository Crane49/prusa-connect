package arch_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The core knows no dialect by name.
//
// A driver declares what it is, and the core reads the declaration: the brands
// and the setup step from Dialect, the routes from Routes, the profiles from
// Profiles. A branch on a name in the core is a feature a driver written
// outside this repository cannot have, because it cannot add itself to the
// branch.
//
// internal/app/builtin is where the shipped set is composed, so the names of
// the dialects this binary carries belong there and nowhere else under
// internal/app.

// dialectRoots are the trees a driver's own package lives in.
//
//nolint:gochecknoglobals // read by the two tests below, never written to.
var dialectRoots = []string{"internal/connector", "adapters/connector"}

// dialectNameHolders are the files under internal/app that may name a dialect.
//
//nolint:gochecknoglobals // read beside dialectRoots, for the same reason.
var dialectNameHolders = []string{"internal/app/builtin"}

// TestNoDialectIsNamedInTheCoreOutsideBuiltin is the gate.
func TestNoDialectIsNamedInTheCoreOutsideBuiltin(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	names := dialectTypeNames(t, root)
	requireScanned(t, "registered dialect names", len(names), 2,
		"Either a tree in dialectRoots moved, or a driver stopped declaring its type name as a "+
			"constant the walk can read.")

	var violations []string

	files := 0

	walk(t, filepath.Join(root, "internal/app"), func(path, rel string) {
		if held(rel) {
			return
		}

		files++

		violations = append(violations, namedDialects(t, path, rel, names)...)
	})

	requireScanned(t, "Go files read under internal/app", files, 40,
		"Either internal/app moved, or the walk stopped reaching it.")

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("%d string(s) under internal/app name a dialect this build carries:\n%s\n\n"+
			"The core reads what a driver declares and never names one. Move the name to "+
			"internal/app/builtin, which is where the shipped set is composed, and ask it for the "+
			"name from internal/app.",
			len(violations), strings.Join(violations, "\n"))
	}
}

// held reports whether this file is one of the composition files that may name
// a dialect.
func held(rel string) bool {
	for _, holder := range dialectNameHolders {
		if strings.HasPrefix(rel, holder+"/") {
			return true
		}
	}

	return false
}

// dialectTypeNames is the name each registered driver answers to in
// configuration, read off the `Type` constant every driver declares.
func dialectTypeNames(t *testing.T, root string) map[string]string {
	t.Helper()

	names := map[string]string{}

	for _, tree := range dialectRoots {
		walk(t, filepath.Join(root, tree), func(path, rel string) {
			for name := range typeConstants(t, path) {
				names[name] = rel
			}
		})
	}

	return names
}

// typeConstants reads `const Type connectorapi.TypeName = "..."` out of one
// file. That declaration is how a driver names itself, so the set of them is
// the set of names the core must not know.
func typeConstants(t *testing.T, path string) map[string]bool {
	t.Helper()

	found := map[string]bool{}

	for _, spec := range valueSpecs(t, path) {
		if len(spec.Names) != 1 || spec.Names[0].Name != "Type" || len(spec.Values) != 1 {
			continue
		}

		literal, ok := spec.Values[0].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			continue
		}

		name, err := strconv.Unquote(literal.Value)
		if err != nil || name == "" {
			continue
		}

		found[name] = true
	}

	return found
}

// namedDialects is every string literal in one file that is a dialect's name,
// and where it is written.
func namedDialects(t *testing.T, path, rel string, names map[string]string) []string {
	t.Helper()

	fileSet := token.NewFileSet()

	parsed, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", rel, err)
	}

	var found []string

	ast.Inspect(parsed, func(node ast.Node) bool {
		literal, isLiteral := node.(*ast.BasicLit)
		if !isLiteral || literal.Kind != token.STRING {
			return true
		}

		text, unquoteErr := strconv.Unquote(literal.Value)
		if unquoteErr != nil {
			return true
		}

		for name, declared := range names {
			if !namesDialect(text, name) {
				continue
			}

			found = append(found, "    "+rel+":"+
				strconv.Itoa(fileSet.Position(literal.Pos()).Line)+
				" names "+name+", declared in "+declared)
		}

		return true
	})

	return found
}

// namesDialect reports whether text carries name as a word of its own. A
// dialect's name is written into a configuration line as well as on its own, so
// matching the whole literal alone would miss "      type: xbuddy".
func namesDialect(text, name string) bool {
	for at := 0; ; {
		found := strings.Index(text[at:], name)
		if found < 0 {
			return false
		}

		start := at + found
		end := start + len(name)

		if !wordByte(text, start-1) && !wordByte(text, end) {
			return true
		}

		at = start + 1
	}
}

// wordByte reports whether the byte at this offset continues a word. An offset
// outside the text is not part of one.
func wordByte(text string, at int) bool {
	if at < 0 || at >= len(text) {
		return false
	}

	c := text[at]

	return c == '_' || c == '-' || c == '.' ||
		('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || ('0' <= c && c <= '9')
}

// valueSpecs is every constant and variable declaration in one file.
func valueSpecs(t *testing.T, path string) []*ast.ValueSpec {
	t.Helper()

	fileSet := token.NewFileSet()

	parsed, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	var specs []*ast.ValueSpec

	ast.Inspect(parsed, func(node ast.Node) bool {
		if spec, ok := node.(*ast.ValueSpec); ok {
			specs = append(specs, spec)
		}

		return true
	})

	return specs
}

// walk calls visit for each non-test Go file under tree, with its path and its
// path relative to the repository root.
func walk(t *testing.T, tree string, visit func(path, rel string)) {
	t.Helper()

	root := repoRoot(t)

	err := filepath.WalkDir(tree, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			return skipVendored(entry.Name())
		}

		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		visit(path, filepath.ToSlash(rel))

		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", tree, err)
	}
}
