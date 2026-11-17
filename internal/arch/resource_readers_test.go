package arch_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// A supervised kind is constructed, started and reported on whether or not
// anything reads its instances back. Nothing inside the supervision plane can
// tell the two apart: an instance nobody asks anything still reaches ready and
// still reports serving=true on /api/v1/resources.
//
// auth was in that state. An operator who declared core/oidc watched auth:idp
// turn ready while every request was served as one unauthenticated subject,
// because no call site anywhere named ref.KindAuth. A security control that
// reports itself running while it is bypassed is worse than one that is absent.
//
// So internal/kernel/ref holds the written answer in readerOfKind, and this file
// is the check on it: a kind the table calls read is a kind a real call site
// names, and every other supervised kind is refused at load instead. A kind
// added the same way is in neither half and fails here.
//
// The table is read out of the source rather than imported, because ADR-20260824-kkr
// leaves internal/arch importing nothing else internal.

// readerMethod is the host call that hands a kind's running instances to the
// code that uses them. internal/app/resourcehost.Host.Supervising is the only
// one: Host.Instance takes a Ref and kind-agnostic plumbing calls it too, so it
// says nothing about which kinds have a consumer.
const readerMethod = "Supervising"

// readerTableVar and supervisedFunc are where the written answer lives.
const (
	readerTableVar = "readerOfKind"
	supervisedFunc = "SupervisedKinds"
	kindsFile      = "internal/kernel/ref/ref.go"
)

// readerFloor is the number of call sites this gate refuses to run below. Three
// kinds are read today, one call site each, and a scan that found fewer has
// stopped reading rather than found a clean tree.
const readerFloor = 3

func TestEverySupervisedKindIsReadBackOrRefused(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	table := readerTable(t, root)
	sites, scanned := supervisingCallSites(t, root)

	requireScanned(t, readerMethod+" call sites naming a kind", scanned, readerFloor,
		"A reader that names its kind through a variable is invisible here; name ref.KindX at the call.")

	// A camera is a device kind: no file declares one, so nothing refuses it at load.
	supervised := supervisedKinds(t, root)
	maps.DeleteFunc(sites, func(kind, _ string) bool { return !slices.Contains(supervised, kind) })

	declared := make([]string, 0, len(table))

	for kind, read := range table {
		if read {
			declared = append(declared, kind)
		}
	}

	named := sortedKeys(sites)
	slices.Sort(declared)

	if !slices.Equal(named, declared) {
		t.Errorf("%s says %v is read back and the tree reads %v.\n"+
			"A kind the table calls read with no call site reports ready and answers nothing.\n"+
			"A kind at a call site the table calls unread is refused at load, so the reader never runs.\n"+
			"Call sites: %s", readerTableVar, declared, named, describe(sites))
	}

	for kind, read := range table {
		where, reads := sites[kind]
		if !read && reads {
			t.Errorf("%s lists %s unread and %s is called on it at %s", readerTableVar, kind, readerMethod, where)
		}
	}
}

// TestTheReaderTableCoversTheSupervisedSet holds the partition total. A kind
// that is neither read nor refused is the state auth shipped in, and the point
// of the table is that there is no third answer.
func TestTheReaderTableCoversTheSupervisedSet(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	table := readerTable(t, root)
	supervised := supervisedKinds(t, root)

	requireScanned(t, "supervised kinds in "+supervisedFunc, len(supervised), readerFloor)

	covered := sortedKeys(table)
	slices.Sort(supervised)

	if !slices.Equal(covered, supervised) {
		t.Fatalf("%s covers %v and %s returns %v", readerTableVar, covered, supervisedFunc, supervised)
	}
}

// TestTheReaderScanSeesACall is the gate on the gate. A scanner that matched
// nothing would pass every check above by reading an empty tree, and the floor
// alone does not prove it recognises the shape it looks for.
func TestTheReaderScanSeesACall(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	write := func(name, kind string) {
		t.Helper()

		source := "package sample\n\nfunc read(h host) { _ = h." + readerMethod + "(ref." + kind + ") }\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	write("sample.go", "KindCamera")
	write("sample_test.go", "KindDB")

	sites, scanned := supervisingCallSites(t, dir)

	if _, found := sites["KindCamera"]; !found {
		t.Errorf("the scan missed a %s(ref.KindCamera) call: %v", readerMethod, sites)
	}

	if _, found := sites["KindDB"]; found {
		t.Error("the scan read a call in a _test.go file, so a test could satisfy this gate")
	}

	if scanned != 1 {
		t.Errorf("the scan counted %d call sites in the fixture, want 1", scanned)
	}
}

// readerTable reads readerOfKind out of internal/kernel/ref, keyed by the Go
// identifier of each kind. The identifier is what a call site names, so nothing
// here has to know how a kind is spelled in configuration.
func readerTable(t *testing.T, root string) map[string]bool {
	t.Helper()

	table := make(map[string]bool)

	ast.Inspect(kindsSource(t, root), func(node ast.Node) bool {
		spec, ok := node.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || spec.Names[0].Name != readerTableVar || len(spec.Values) != 1 {
			return true
		}

		literal, ok := spec.Values[0].(*ast.CompositeLit)
		if !ok {
			return true
		}

		for _, element := range literal.Elts {
			pair, paired := element.(*ast.KeyValueExpr)
			if !paired {
				continue
			}

			key, keyed := pair.Key.(*ast.Ident)
			value, valued := pair.Value.(*ast.Ident)

			if !keyed || !valued {
				continue
			}

			table[key.Name] = value.Name == "true"
		}

		return false
	})

	if len(table) == 0 {
		t.Fatalf("%s holds no %s entries; the gate is reading the wrong file", kindsFile, readerTableVar)
	}

	return table
}

// supervisedKinds reads the identifiers SupervisedKinds returns.
func supervisedKinds(t *testing.T, root string) []string {
	t.Helper()

	var kinds []string

	ast.Inspect(kindsSource(t, root), func(node ast.Node) bool {
		decl, ok := node.(*ast.FuncDecl)
		if !ok || decl.Name.Name != supervisedFunc || decl.Recv != nil {
			return true
		}

		// The literal's elements, not every identifier in the body: the type
		// []Kind is an identifier too, and counting it would put a kind nobody
		// declared into the set.
		ast.Inspect(decl.Body, func(inner ast.Node) bool {
			literal, composite := inner.(*ast.CompositeLit)
			if !composite {
				return true
			}

			for _, element := range literal.Elts {
				if ident, named := element.(*ast.Ident); named {
					kinds = append(kinds, ident.Name)
				}
			}

			return false
		})

		return false
	})

	if len(kinds) == 0 {
		t.Fatalf("%s names no kinds in %s", kindsFile, supervisedFunc)
	}

	return kinds
}

// kindsSource parses the file both written answers live in.
func kindsSource(t *testing.T, root string) *ast.File {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(kindsFile))

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing %s: %v", kindsFile, err)
	}

	return file
}

// supervisingCallSites maps each kind identifier named at a Supervising call to
// where it is named, and reports how many calls it read. A test file is left
// out: a reader that exists only in a test hands no instance to anything a
// customer runs.
func supervisingCallSites(t *testing.T, root string) (map[string]string, int) {
	t.Helper()

	sites := make(map[string]string)
	scanned := 0

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

		found, callErr := readerCalls(path, root, sites)
		scanned += found

		return callErr
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}

	return sites, scanned
}

// readerCalls records every ref.KindX handed to a Supervising call in one file.
//
// It reads the argument rather than the receiver's type, because the kind is
// what this gate is about and a selector on ref is unambiguous. A method
// declaration is not a call expression, so the host's own definition of
// Supervising is not mistaken for a consumer of every kind.
func readerCalls(path, root string, sites map[string]string) (int, error) {
	fset := token.NewFileSet()

	found := 0

	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return 0, err
	}

	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}

		method, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || method.Sel.Name != readerMethod {
			return true
		}

		kind, named := kindArgument(call.Args[0])
		if !named {
			return true
		}

		where, relErr := filepath.Rel(root, path)
		if relErr != nil {
			where = path
		}

		sites[kind] = filepath.ToSlash(where) + ":" + strconv.Itoa(fset.Position(call.Pos()).Line)
		found++

		return true
	})

	return found, nil
}

// kindArgument reads the identifier out of a ref.KindX argument, and reports
// false for anything else. A caller passing a variable is invisible to this
// gate, which is what the floor on the call-site count exists to notice.
func kindArgument(arg ast.Expr) (string, bool) {
	selector, ok := arg.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}

	pkg, ok := selector.X.(*ast.Ident)
	if !ok || pkg.Name != "ref" || !strings.HasPrefix(selector.Sel.Name, "Kind") {
		return "", false
	}

	return selector.Sel.Name, true
}

// describe renders the call sites for a refusal, in kind order.
func describe(sites map[string]string) string {
	lines := make([]string, 0, len(sites))
	for _, kind := range sortedKeys(sites) {
		lines = append(lines, kind+" at "+sites[kind])
	}

	return strings.Join(lines, ", ")
}
