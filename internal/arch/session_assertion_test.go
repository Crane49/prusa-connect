package arch_test

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/arch"
)

const connectorapiPath = modulePath + "/sdk/connectorapi"

// asFile is the one file allowed to make a type assertion on a
// connectorapi.Session. capability.As is how the rest of the host asks a
// session for an optional interface.
const asFile = "internal/printer/capability/unwrap.go"

// sessionAccessors are the SDK accessors that assert on the session they are
// given and never look inside a wrapper. The host calls capability.As instead.
var sessionAccessors = []string{"Files", "Manages", "Uploads"}

// The host puts its own type around some sessions, such as watchedSession,
// which the dial loop puts around the session of a printer it dials. A type
// assertion on the wrapper finds only the methods the wrapper declares, so the
// host cannot reach the files, the probe or the presence of the driver's
// session inside it. Every one of those lookups goes through capability.As,
// which looks inside the wrapper.
//
// The gate type-checks the host's packages, because a variable's name does not
// show whether it is a connectorapi.Session. It refuses an assertion on any
// interface that has every method of connectorapi.Session, so a named copy of
// the type or an interface that embeds it is refused too. It uses the build-ci
// tag set, so a file behind a build tag is checked as well. An assertion on a
// session first converted to another interface, such as any(s).(T), is not
// refused: the gate sees only the type of the expression it asserts on.
func TestTheHostAsksASessionForAnOptionalInterfaceOnlyThroughAs(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	sets, err := arch.TagSets(root)
	if err != nil {
		t.Fatal(err)
	}

	pkgs := listForTypes(t, root, sets["build-ci"])

	exports := map[string]string{}
	for _, pkg := range pkgs {
		exports[pkg.ImportPath] = pkg.Export
	}

	fset := token.NewFileSet()
	imports := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		export, ok := exports[path]
		if !ok || export == "" {
			return nil, errors.New("no export data for " + path)
		}

		return os.Open(export)
	})

	connectorapi, err := imports.Import(connectorapiPath)
	if err != nil {
		t.Fatal(err)
	}

	session, ok := connectorapi.Scope().Lookup("Session").Type().Underlying().(*types.Interface)
	if !ok {
		t.Fatal("connectorapi.Session is not an interface")
	}

	var refusals []string

	checked, assertions := 0, 0

	for _, pkg := range pkgs {
		if pkg.DepOnly || !hostPackage(root, pkg.Dir) {
			continue
		}

		files := parsePackage(t, fset, pkg)
		info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}}
		config := types.Config{Importer: mappedImporter{imports, pkg.ImportMap}, FakeImportC: true}

		if _, checkErr := config.Check(pkg.ImportPath, fset, files, info); checkErr != nil {
			t.Fatalf("type-check %s: %v", pkg.ImportPath, checkErr)
		}

		checked++

		for _, file := range files {
			name, relErr := filepath.Rel(root, fset.File(file.Pos()).Name())
			if relErr != nil {
				t.Fatal(relErr)
			}

			if filepath.ToSlash(name) == asFile {
				continue
			}

			ast.Inspect(file, func(node ast.Node) bool {
				switch node := node.(type) {
				case *ast.TypeAssertExpr:
					assertions++

					if types.Implements(info.TypeOf(node.X), session) {
						refusals = append(refusals, fset.Position(node.Pos()).String()+
							": a type assertion on a connectorapi.Session")
					}
				case *ast.SelectorExpr:
					called, isFunc := info.Uses[node.Sel].(*types.Func)
					if isFunc && called.Pkg() != nil && called.Pkg().Path() == connectorapiPath &&
						slices.Contains(sessionAccessors, called.Name()) {
						refusals = append(refusals, fset.Position(node.Pos()).String()+
							": connectorapi."+called.Name())
					}
				}

				return true
			})
		}
	}

	requireScanned(t, "host packages", checked, 60)
	requireScanned(t, "type assertions", assertions, 150)

	if len(refusals) > 0 {
		t.Fatalf("%d place(s) ask a session for an optional interface without looking inside a wrapper. "+
			"Use capability.As:\n\t%s", len(refusals), strings.Join(refusals, "\n\t"))
	}
}

// typedPackage is what the gate needs from `go list -export -json`.
type typedPackage struct {
	ImportPath string            `json:"ImportPath"`
	Dir        string            `json:"Dir"`
	Export     string            `json:"Export"`
	GoFiles    []string          `json:"GoFiles"`
	CgoFiles   []string          `json:"CgoFiles"`
	ImportMap  map[string]string `json:"ImportMap"`
	DepOnly    bool              `json:"DepOnly"`
}

// listForTypes lists the host's packages and everything they import. For each
// one it gives the compiler's export data. The type checker reads imports from
// that data.
//
// Compiling that data is most of what the gate costs. CGO_ENABLED is fixed, so
// the minimal shard, which runs with cgo off, and the others, which run with it
// on, find the same build in the cache, the one `make go-cache-warm` compiles.
// The host has no cgo file, so nothing drops out of the check.
func listForTypes(t *testing.T, root, tags string) []typedPackage {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "go", "list", "-export", "-deps", "-json", "-tags", tags,
		"./internal/...", "./adapters/...", "./cmd/...")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -export: %v%s", err, arch.StderrOf(err))
	}

	var pkgs []typedPackage

	dec := json.NewDecoder(strings.NewReader(string(out)))

	for {
		var pkg typedPackage
		if decErr := dec.Decode(&pkg); decErr != nil {
			if errors.Is(decErr, io.EOF) {
				break
			}

			t.Fatalf("decode go list: %v", decErr)
		}

		pkgs = append(pkgs, pkg)
	}

	return pkgs
}

// hostPackage reports whether a package directory is the host's own code.
func hostPackage(root, dir string) bool {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return false
	}

	rel = filepath.ToSlash(rel)

	return strings.HasPrefix(rel, "internal/") || strings.HasPrefix(rel, "adapters/") ||
		strings.HasPrefix(rel, "cmd/")
}

func parsePackage(t *testing.T, fset *token.FileSet, pkg typedPackage) []*ast.File {
	t.Helper()

	names := append(slices.Clone(pkg.GoFiles), pkg.CgoFiles...)
	files := make([]*ast.File, 0, len(names))

	for _, name := range names {
		file, err := parser.ParseFile(fset, filepath.Join(pkg.Dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		files = append(files, file)
	}

	return files
}

// mappedImporter applies a package's import map. go list uses that map to say
// that an import path in the source resolves to another package.
type mappedImporter struct {
	types.Importer

	mapped map[string]string
}

func (m mappedImporter) Import(path string) (*types.Package, error) {
	if to, ok := m.mapped[path]; ok {
		path = to
	}

	return m.Importer.Import(path)
}
