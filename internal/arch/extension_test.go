package arch_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// The registration rule of sdk/extension holds only while every registrant goes
// through an entry point that takes a descriptor. RegisterFactory does not: it
// predates the descriptor, and the caller it was kept for went with the retired
// server.
//
// This is the gate that keeps it from gaining a new one. A registrant that
// reaches for RegisterFactory would ship an entry with no summary, no version,
// no config keys and no stability level, and it would appear in the published
// reference as nothing at all.
var registryImports = map[string]bool{
	modulePath + "/sdk/storageapi": true,
	modulePath + "/sdk/eventsapi":  true,
	modulePath + "/sdk/authapi":    true,
}

func TestOnlyLegacyRegistersWithoutADescriptor(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	var offences []string

	scanned := 0

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			return skipDir(root, path)
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		scanned++

		found, scanErr := undescribedRegistrations(path)
		if scanErr != nil {
			return scanErr
		}

		offences = append(offences, found...)

		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}

	requireScanned(t, "Go files under the walked tree", scanned, 1)

	for _, where := range offences {
		t.Errorf("%s registers through RegisterFactory, which takes no descriptor.\n"+
			"The entry reaches an operator's configuration and appears in the published extension\n"+
			"reference as nothing at all: no summary, no version, no config keys, no stability level.\n"+
			"Use Register, which takes the descriptor the reference is generated from.", where)
	}
}

// A registration is a value before it is a call. A package that binds the entry
// point to a variable and calls it through that name puts the call somewhere the
// scan of this file never associates with the registry package.
func TestTheRegistrationGateSeesTheValueAsWellAsTheCall(t *testing.T) {
	t.Parallel()

	source := "package sample\n\n" +
		"import \"" + modulePath + "/sdk/storageapi\"\n\n" +
		"var RegisterFactory = storageapi.RegisterFactory\n\n" +
		"func init() { storageapi.RegisterFactory(\"direct\", nil) }\n"

	path := filepath.Join(t.TempDir(), "sample.go")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}

	found, err := undescribedRegistrations(path)
	if err != nil {
		t.Fatalf("scanning the fixture: %v", err)
	}

	if len(found) != 2 {
		t.Errorf("the gate reports %d offence(s) in a file that both binds and calls RegisterFactory: %v",
			len(found), found)
	}
}

// A registration made by a test never ships in a product binary, which is why
// the generator's TestEveryRegisteredExtensionIsDocumented excludes a test
// package. This gate reads the same tree and answers the same question, so it
// draws the line in the same place.
func TestTheRegistrationGateReadsProductCodeOnly(t *testing.T) {
	t.Parallel()

	source := "package sample\n\n" +
		"import \"" + modulePath + "/sdk/storageapi\"\n\n" +
		"func TestSomething() { storageapi.RegisterFactory(\"from-a-test\", nil) }\n"

	path := filepath.Join(t.TempDir(), "sample_test.go")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}

	found, err := undescribedRegistrations(path)
	if err != nil {
		t.Fatalf("scanning the fixture: %v", err)
	}

	if len(found) != 0 {
		t.Errorf("the gate reports %d offence(s) in a test file: %v", len(found), found)
	}
}

// A namespace is derived by walking the stack past the packages a registration
// is routed through, and that list is fixed text inside sdk/extension. A wrapper
// that moves and is not listed puts its own frames in front of the author's, so
// every plugin it registers is attributed to the wrapper's module. A wrapper
// that is added and not listed does the same.
//
// Neither shows up as a compile error, so this is the gate: every listed package
// exists, and every package that calls extension.Declare is listed.
func TestTheNamespaceWalkKnowsEveryWrapper(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	known := extension.KnownWrappers()

	if len(known) == 0 {
		t.Fatal("sdk/extension lists no wrapper packages, so this gate is comparing against nothing")
	}

	for _, pkg := range known {
		dir, ok := strings.CutPrefix(pkg, modulePath+"/")
		if !ok {
			t.Errorf("the wrapper list holds %q, which is outside this module", pkg)

			continue
		}

		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(dir))); err != nil {
			t.Errorf("sdk/extension lists %q as a registration wrapper and it is not there.\n"+
				"Every plugin registering through it would be attributed to its module instead of the\n"+
				"author's. Update KnownWrappers in sdk/extension/namespace.go: %v", pkg, err)
		}
	}

	callers, err := packagesCalling(root, "Declare", modulePath+"/sdk/extension")
	if err != nil {
		t.Fatalf("scanning for callers of extension.Declare: %v", err)
	}

	requireScanned(t, "packages calling extension.Declare", len(callers), 1,
		"Either the call moved off "+modulePath+"/sdk/extension, or the scan is not reaching it.")

	for _, pkg := range callers {
		if !slices.Contains(known, pkg) {
			t.Errorf("%s calls extension.Declare and is not in KnownWrappers, so it stands between a\n"+
				"plugin author and the namespace walk: every name it registers is attributed to its own\n"+
				"module. Add it to KnownWrappers in sdk/extension/namespace.go", pkg)
		}
	}
}

// packagesCalling reports the first-party packages that call a function on
// imported, excluding the package that defines it.
func packagesCalling(root, function, imported string) ([]string, error) {
	var found []string

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			return skipDir(root, path)
		}

		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		calls, scanErr := callsOn(path, function, imported)
		if scanErr != nil || !calls {
			return scanErr
		}

		dir, relErr := filepath.Rel(root, filepath.Dir(path))
		if relErr != nil {
			return relErr
		}

		found = append(found, modulePath+"/"+filepath.ToSlash(dir))

		return nil
	})
	if err != nil {
		return nil, err
	}

	slices.Sort(found)

	return slices.Compact(found), nil
}

// callsOn reports whether one file calls function on the package it imports as
// imported.
func callsOn(path, function, imported string) (bool, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return false, err
	}

	local := ""

	for _, spec := range file.Imports {
		unquoted, unquoteErr := strconv.Unquote(spec.Path.Value)
		if unquoteErr != nil || unquoted != imported {
			continue
		}

		local = imported[strings.LastIndex(imported, "/")+1:]
		if spec.Name != nil {
			local = spec.Name.Name
		}
	}

	if local == "" {
		return false, nil
	}

	calls := false

	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != function {
			return true
		}

		if ident, isIdent := sel.X.(*ast.Ident); isIdent && ident.Name == local {
			calls = true
		}

		return true
	})

	return calls, nil
}

// skipDir keeps the walk out of the quarantine, the vendor tree and anything
// that is not first-party Go.
func skipDir(root, path string) error {
	name := filepath.Base(path)
	if name == "vendor" || name == "node_modules" || name == "frontend" ||
		(strings.HasPrefix(name, ".") && path != root) {
		return filepath.SkipDir
	}

	return nil
}

// undescribedRegistrations reports every mention of RegisterFactory on one of
// the packages that owns a registry, whether it is called or bound to a name and
// called elsewhere. The receiver is checked so that a method of somebody else's
// called RegisterFactory is not reported.
//
// A test file is left alone: a registration made by a test is not a product
// extension, which is where the generator's
// TestEveryRegisteredExtensionIsDocumented draws the same line.
func undescribedRegistrations(path string) ([]string, error) {
	if strings.HasSuffix(path, "_test.go") {
		return nil, nil
	}

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}

	names := map[string]bool{}

	for _, spec := range file.Imports {
		imported, unquoteErr := strconv.Unquote(spec.Path.Value)
		if unquoteErr != nil || !registryImports[imported] {
			continue
		}

		local := imported[strings.LastIndex(imported, "/")+1:]
		if spec.Name != nil {
			local = spec.Name.Name
		}

		names[local] = true
	}

	// A file inside one of those packages spells the call without a receiver
	// and defines it, so it is left alone.
	if len(names) == 0 {
		return nil, nil
	}

	var offences []string

	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "RegisterFactory" {
			return true
		}

		if ident, isIdent := sel.X.(*ast.Ident); isIdent && names[ident.Name] {
			offences = append(offences, fset.Position(sel.Pos()).String())
		}

		return true
	})

	return offences, nil
}
