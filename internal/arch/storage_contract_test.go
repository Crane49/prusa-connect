package arch_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// ADR-20260825-smx rule 9: every storage provider, ours and a community one, runs
// storageapitest.RunProviderContract. The record's enforcement table credits
// that rule to TestPathEscapeCatchesAnUnconfinedProvider, which proves the
// suite can catch an unconfined provider and says nothing about whether a given
// provider runs it. A provider added without the suite is confined by nothing
// and passes every other gate in this package.
//
// A provider is found by its registration rather than by its name or its
// directory, because the registration is what puts it in front of a caller. A
// directory that registers one and runs no contract suite is the finding.
const (
	storageAPIImport     = "github.com/prusa3d/prusa-connect/sdk/storageapi"
	storageAPITestImport = "github.com/prusa3d/prusa-connect/sdk/storageapi/storageapitest"

	// providerContract is the suite ADR-20260825-smx rule 9 names.
	providerContract = "RunProviderContract"
)

// storageRegistrars put a provider in the registry, so a caller can name its
// type in configuration. Declare is not one: it builds a Registration and
// hands it back, and every caller of it here is a test reading what a
// registration would hold.
var storageRegistrars = map[string]bool{
	"Register":        true,
	"RegisterFactory": true,
}

// storageContractExceptions names a directory that registers a provider and
// runs no contract suite, with the reason. An entry that stops describing a
// registering directory fails, so an exception cannot outlive what it excuses.
var storageContractExceptions = map[string]string{
	"adapters/storage/local": "the package is the registration and nothing else. The store it " +
		"builds is adapters/storage/diskstore, whose TestProviderContract runs the suite over " +
		"the same code this factory returns",
}

// storageKnownProviders are the directories this gate is known to reach. A
// directory that appears is checked without being listed here, and one that
// stops appearing fails until the list is corrected.
//
// This is the arming half, and it is what stops the check going quiet. The
// discovery walks the syntax, so renaming the alias a provider registers
// through drops that directory from the set. Nothing else notices: the
// remaining ones still run the suite, and the gate reports green over a
// provider it no longer reads.
var storageKnownProviders = []string{
	"adapters/storage/local",
	"adapters/storage/temporary",
}

// storageUnregisteredProviders are directories holding a provider that reaches
// callers without a registration. Discovery cannot find one, so it is named,
// and rule 9 is then held over it the same way. The list is empty: the one
// entry it held went with the retired server.
var storageUnregisteredProviders = []string{}

// TestEveryStorageProviderRunsTheContractSuite holds ADR-20260825-smx rule 9.
func TestEveryStorageProviderRunsTheContractSuite(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	registering := storageRegisteringDirs(t, root)
	requireScanned(t, "directories registering a storage provider", len(registering), 1,
		"Either the registration moved off "+storageAPIImport+", or the walk is not reaching it.")

	t.Logf("%d directory registers a storage provider: %s", len(registering), strings.Join(registering, ", "))

	found := map[string]bool{}
	for _, dir := range registering {
		found[dir] = true
	}

	for _, dir := range storageKnownProviders {
		if !found[dir] {
			t.Errorf("%s no longer reaches this gate, and it registered a provider.\n"+
				"A provider that leaves the set is a provider nothing checks. Either it stopped registering,"+
				" and storageKnownProviders loses the line, or the discovery stopped seeing how it registers.", dir)
		}
	}

	used := map[string]bool{}

	for _, dir := range registering {
		if _, allowed := storageContractExceptions[dir]; allowed {
			used[dir] = true

			continue
		}

		if runsProviderContract(t, root, dir) {
			continue
		}

		t.Errorf("%s registers a storage provider and no test there runs %s.\n"+
			"ADR-20260825-smx rule 9: a provider that never meets the suite is confined by nothing this repository checks.\n"+
			"Add storageapitest.%s beside the provider, or record the reason in storageContractExceptions.",
			dir, providerContract, providerContract)
	}

	for _, dir := range storageUnregisteredProviders {
		if found[dir] {
			t.Errorf("%s registers a provider again, so discovery reaches it and "+
				"storageUnregisteredProviders should lose the line.", dir)

			continue
		}

		if !runsProviderContract(t, root, dir) {
			t.Errorf("%s holds a storage provider and no test there runs %s.\n"+
				"ADR-20260825-smx rule 9: a provider that never meets the suite is confined by nothing this "+
				"repository checks.", dir, providerContract)
		}
	}

	for dir, reason := range storageContractExceptions {
		if !used[dir] {
			t.Errorf("storageContractExceptions lists %s (%s), which registers no provider. Remove the entry.",
				dir, reason)
		}
	}
}

// storageRegisteringDirs is every directory whose production code registers a
// storage provider, relative to the repository root and sorted.
func storageRegisteringDirs(t *testing.T, root string) []string {
	t.Helper()

	byDir := map[string][]string{}

	walkGoFiles(t, root, func(rel string, isTest bool) {
		if isTest || strings.HasPrefix(rel, "sdk/storageapi/") {
			return
		}

		dir := path.Dir(rel)
		byDir[dir] = append(byDir[dir], rel)
	})

	var found []string

	for dir, files := range byDir {
		if dirRegistersAProvider(t, root, files) {
			found = append(found, dir)
		}
	}

	sort.Strings(found)

	return found
}

// dirRegistersAProvider reports whether one directory's production files call a
// registrar. The alias is read first, because a package that re-exports
// RegisterFactory under its own name and calls that.
func dirRegistersAProvider(t *testing.T, root string, files []string) bool {
	t.Helper()

	local := map[string]bool{}

	parsed := make([]*ast.File, 0, len(files))

	for _, rel := range files {
		file := parseGoFile(t, root, rel)
		parsed = append(parsed, file)

		for _, name := range storageAPIAliases(file) {
			local[name] = true
		}
	}

	for _, file := range parsed {
		if callsARegistrar(file, local) {
			return true
		}
	}

	return false
}

// storageAPIAliases is every package-level name bound to a registrar, as
// `var RegisterFactory = storageapi.RegisterFactory` binds one.
func storageAPIAliases(file *ast.File) []string {
	api := importedAs(file, storageAPIImport)
	if api == "" {
		return nil
	}

	var names []string

	for _, decl := range file.Decls {
		general, ok := decl.(*ast.GenDecl)
		if !ok || general.Tok != token.VAR {
			continue
		}

		for _, spec := range general.Specs {
			value, isValue := spec.(*ast.ValueSpec)
			if !isValue {
				continue
			}

			for i, expr := range value.Values {
				if selectorNames(expr, api, storageRegistrars) && i < len(value.Names) {
					names = append(names, value.Names[i].Name)
				}
			}
		}
	}

	return names
}

// callsARegistrar reports whether the file calls a registrar, either through
// the storageapi import or through one of the package's own aliases.
func callsARegistrar(file *ast.File, aliases map[string]bool) bool {
	api := importedAs(file, storageAPIImport)

	found := false

	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || found {
			return !found
		}

		if api != "" && selectorNames(call.Fun, api, storageRegistrars) {
			found = true

			return false
		}

		if name, isIdent := call.Fun.(*ast.Ident); isIdent && aliases[name.Name] {
			found = true

			return false
		}

		return true
	})

	return found
}

// runsProviderContract reports whether any test file in the directory calls the
// suite. Both test packages are read, because a provider's contract run sits in
// either one.
func runsProviderContract(t *testing.T, root, dir string) bool {
	t.Helper()

	found := false

	walkGoFiles(t, root, func(rel string, isTest bool) {
		if found || !isTest || path.Dir(rel) != dir {
			return
		}

		file := parseGoFile(t, root, rel)

		suite := importedAs(file, storageAPITestImport)
		if suite == "" {
			return
		}

		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || found {
				return !found
			}

			if selectorNames(call.Fun, suite, map[string]bool{providerContract: true}) {
				found = true

				return false
			}

			return true
		})
	})

	return found
}

// selectorNames reports whether an expression is `pkg.Name` with Name in the
// set. It reads the syntax, so a call inside a string literal or a comment is
// not one.
func selectorNames(expr ast.Expr, pkg string, names map[string]bool) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	ident, ok := selector.X.(*ast.Ident)

	return ok && ident.Name == pkg && names[selector.Sel.Name]
}

// importedAs is the name a file refers to one import path by, or the empty
// string when the file does not import it. An aliased import is read from the
// alias, and a plain one from the last element of the path.
func importedAs(file *ast.File, importPath string) string {
	for _, spec := range file.Imports {
		unquoted, err := strconv.Unquote(spec.Path.Value)
		if err != nil || unquoted != importPath {
			continue
		}

		if spec.Name != nil {
			return spec.Name.Name
		}

		return path.Base(importPath)
	}

	return ""
}

// walkGoFiles calls back for every Go file in the repository, with its path
// relative to the root and whether it is a test file. Everything is walked: it
// ships a provider, and ADR-20260825-smx rule 9 says every provider.
func walkGoFiles(t *testing.T, root string, visit func(rel string, isTest bool)) {
	t.Helper()

	err := filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "vendor", "testdata", "dist", "frontend":
				return fs.SkipDir
			default:
				return nil
			}
		}

		if filepath.Ext(entry.Name()) != ".go" {
			return nil
		}

		rel, relErr := filepath.Rel(root, current)
		if relErr != nil {
			return relErr
		}

		visit(filepath.ToSlash(rel), strings.HasSuffix(entry.Name(), "_test.go"))

		return nil
	})
	if err != nil {
		t.Fatalf("walk the repository: %v", err)
	}
}

// parseGoFile parses one file for its declarations. The bodies are needed,
// because a registration sits inside an init().
func parseGoFile(t *testing.T, root, rel string) *ast.File {
	t.Helper()

	full := filepath.Join(root, filepath.FromSlash(rel))

	file, err := parser.ParseFile(token.NewFileSet(), full, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", rel, err)
	}

	return file
}
