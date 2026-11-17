package arch_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// composeDir is the only package this repository publishes for build-time
// composition, and composeEntry is the whole of what it publishes.
const (
	composeDir      = "compose"
	composeEntry    = "Main"
	composeCaller   = "cmd/prusa-connect"
	composeImport   = "github.com/prusa3d/prusa-connect/compose"
	composeCallText = "compose." + composeEntry + "()"
)

// TestTheCompositionFacadeExportsOneFunction is ADR-20260901-7t4 rule 6's gate.
//
// The façade is a public API this project keeps working forever, and the record
// bought it by promising it stays one function wide: an adapter compiles against
// sdk/ alone, and a composition module needs one call. Every exported symbol
// added here is a compatibility obligation nobody voted for, and the cheap place
// to notice one is the moment it is written.
func TestTheCompositionFacadeExportsOneFunction(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	entries, err := os.ReadDir(filepath.Join(root, composeDir))
	if err != nil {
		t.Fatalf("reading %s: %v", composeDir, err)
	}

	var (
		exported  []string
		filesRead int
	)

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		filesRead++

		exported = append(exported, exportedSymbols(t, filepath.Join(root, composeDir, name))...)
	}

	requireScanned(t, "non-test Go files in "+composeDir, filesRead, 1)

	var violations []string

	for _, symbol := range exported {
		if symbol != "func "+composeEntry {
			violations = append(violations, composeDir+" exports "+symbol+
				", and ADR-20260901-7t4 rule 6 gives it one exported entry function and nothing else. "+
				"A composition module needs the call; anything more is public API forever")
		}
	}

	if len(exported) == 0 {
		violations = append(violations, composeDir+" exports nothing, so a composition module has nothing to call")
	}

	refuse(t, violations)
}

// TestTheShippedCommandCallsTheCompositionFacade holds the second half of rule
// 6. The shipped binary is a composition with no adapters, so it goes through
// the same entry point a composed one does; a command that called internal/app
// directly would leave the façade untested by every build anybody runs.
func TestTheShippedCommandCallsTheCompositionFacade(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	entries, err := os.ReadDir(filepath.Join(root, composeCaller))
	if err != nil {
		t.Fatalf("reading %s: %v", composeCaller, err)
	}

	var imports, calls, filesRead int

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		filesRead++

		source := read(t, root, composeCaller+"/"+name)
		if strings.Contains(source, `"`+composeImport+`"`) {
			imports++
		}

		if strings.Contains(source, composeCallText) {
			calls++
		}
	}

	requireScanned(t, "non-test Go files in "+composeCaller, filesRead, 1)

	var violations []string

	if imports == 0 {
		violations = append(violations, composeCaller+" does not import "+composeImport)
	}

	if calls == 0 {
		violations = append(violations, composeCaller+" does not call "+composeCallText+
			", so the shipped binary and a composed one start differently")
	}

	refuse(t, violations)
}

// exportedSymbols is every exported top-level declaration in one file, rendered
// as "func Name", "type Name", "const Name" or "var Name" so a refusal says
// what was added and not only that something was.
func exportedSymbols(t *testing.T, path string) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	var out []string

	for _, decl := range file.Decls {
		switch declaration := decl.(type) {
		case *ast.FuncDecl:
			if declaration.Recv == nil && declaration.Name.IsExported() {
				out = append(out, "func "+declaration.Name.Name)
			}
		case *ast.GenDecl:
			out = append(out, exportedNames(declaration)...)
		}
	}

	return out
}

// exportedNames is the exported names one const, var or type block declares.
func exportedNames(declaration *ast.GenDecl) []string {
	var out []string

	for _, spec := range declaration.Specs {
		switch named := spec.(type) {
		case *ast.TypeSpec:
			if named.Name.IsExported() {
				out = append(out, "type "+named.Name.Name)
			}
		case *ast.ValueSpec:
			for _, name := range named.Names {
				if name.IsExported() {
					out = append(out, declaration.Tok.String()+" "+name.Name)
				}
			}
		}
	}

	return out
}

// generatorDir is the command that publishes the extension reference.
const generatorDir = "cmd/generate-extensions"

// TestTheGeneratorReachesAdaptersThroughTheCompositionRoot is ADR-20260901-7t4 rule
// 14's gate.
//
// A blank-import list of its own, beside the composition root's, writes every
// reference adapter down twice, and an adapter that reaches one list and not
// the other is published in one artefact and missing from the other. The
// generator imports internal/app/builtin, which links every adapters/ package,
// so the shipped set has one author.
func TestTheGeneratorReachesAdaptersThroughTheCompositionRoot(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	files := goFilesUnder(t, generatorDir)
	requireScanned(t, "Go files in "+generatorDir, len(files), 1)

	adapterPrefix := `"` + modulePath + `/adapters/`

	var violations []string

	for _, path := range files {
		source := read(t, root, path)

		for line := range strings.SplitSeq(source, "\n") {
			if strings.Contains(line, adapterPrefix) {
				violations = append(violations, generatorDir+" imports "+strings.TrimSpace(line)+
					" directly. It links every adapters/ package through internal/app/builtin, "+
					"and a second list is one more place a new adapter has to be written down")
			}
		}
	}

	refuse(t, violations)
}

// dynamicLoaders are the ways a Go binary loads code it was not linked with.
// plugin.Open is the standard library's; the rest are the cgo route to the same
// system call under its usual names.
//
//nolint:gochecknoglobals // a closed list, never written to.
var dynamicLoaders = []string{"plugin.Open", "plugin.Plugin", "dlopen", "dlsym", "LoadLibrary"}

// TestNothingLoadsCodeAtRuntime is ADR-20260901-7t4 rule 2's gate.
//
// Every adapter is compiled in, for every kind, and "no core patching" means
// the build configuration decides the contents rather than a source edit, and
// not that something is loaded later. The owner ruled dynamic loading out on
// 2026-09-01, and Go's own plugin package would take the static binary with it:
// it forbids static linking, pins the toolchain to the exact build that made
// the plugin, and cannot unload.
//
// The binary check already proves the artefact links nothing dynamically. This
// proves the source never asks to, which is the half a stripped binary cannot
// answer.
func TestNothingLoadsCodeAtRuntime(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	var (
		scanned    int
		violations []string
	)

	for _, tree := range []string{"cmd", "compose", "internal", "sdk", "adapters"} {
		for _, path := range goFilesUnder(t, tree) {
			scanned++

			source := read(t, root, path)

			for _, loader := range dynamicLoaders {
				if strings.Contains(source, loader) {
					violations = append(violations, path+" names "+loader+
						", and ADR-20260901-7t4 rule 2 compiles every adapter in. A binary that loads code "+
						"at runtime is not the static binary this product ships")
				}
			}

			// The standard library's plugin package, imported. The bare word is
			// also an ordinary log key and a doc-comment noun, so the match is
			// the import line and not the word.
			for line := range strings.SplitSeq(source, "\n") {
				if trimmed := strings.TrimSpace(line); trimmed == `"plugin"` || trimmed == `_ "plugin"` {
					violations = append(violations, path+" imports the standard library's plugin package, "+
						"and ADR-20260901-7t4 rule 2 compiles every adapter in")
				}
			}
		}
	}

	requireScanned(t, "Go files", scanned, 100)

	refuse(t, violations)
}
