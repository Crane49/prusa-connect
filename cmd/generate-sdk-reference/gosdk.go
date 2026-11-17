package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/doc"
	"go/doc/comment"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/docsref"
)

// The Go SDK is read as `go doc` reads it: every importable package under
// sdk/, reduced to its exported identifiers with go/doc. Test files and
// sdk/internal are left out.

const (
	// sdkModulePath is the import path of the sdk module.
	sdkModulePath = "github.com/prusa3d/prusa-connect/sdk"

	// pkgGoDev is the base URL for a doc link to a package outside the sdk.
	pkgGoDev = "https://pkg.go.dev"
)

var (
	errNoPackages    = errors.New("no package was found under the sdk directory")
	errTwoPackages   = errors.New("a directory holds two packages")
	errDuplicateName = errors.New("two identifiers render the same anchor")
)

// goPackage is one importable package and its exported surface.
type goPackage struct {
	// Rel is the path under the sdk directory, such as `busapi` or
	// `authapi/authapitest`.
	Rel        string
	ImportPath string
	Name       string
	// Doc is the package comment as markdown.
	Doc      string
	Synopsis string
	Entries  []goEntry
}

// goEntry is one exported identifier.
type goEntry struct {
	// Kind is const, var, func, type or method.
	Kind string
	// Name is the identifier, with the type in front for a method:
	// `SubscribeParams.Validate`.
	Name string
	// Level is the heading level: 3 for a package-level identifier, 4 for a
	// method, a constructor or a constant that belongs to a type.
	Level int
	// Decl is the declaration as Go source without its body or its comment.
	Decl string
	// Doc is the comment as markdown, empty when the declaration has none.
	Doc string
}

const (
	entryLevel  = 3
	memberLevel = 4

	// tabWidth is the tab width go/printer aligns a declaration with.
	tabWidth = 8
)

// readGoSDK lists every importable package under dir, sorted by path. All are
// parsed before any is rendered, because comments link across packages.
func readGoSDK(dir string) ([]goPackage, error) {
	var parsed []parsedPackage

	err := filepath.WalkDir(dir, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !entry.IsDir() {
			return nil
		}

		if current != dir && skippedDir(entry.Name()) {
			return filepath.SkipDir
		}

		pkg, err := parseGoPackage(dir, current)
		if err != nil {
			return err
		}

		if pkg != nil {
			parsed = append(parsed, *pkg)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(parsed) == 0 {
		return nil, fmt.Errorf("%w: %s", errNoPackages, dir)
	}

	sort.Slice(parsed, func(i, j int) bool { return parsed[i].rel < parsed[j].rel })

	known := knownAnchors(parsed)
	packages := make([]goPackage, 0, len(parsed))

	for _, pkg := range parsed {
		render := newGoRenderer(pkg, known)

		packages = append(packages, goPackage{
			Rel:        pkg.rel,
			ImportPath: pkg.importPath,
			Name:       pkg.doc.Name,
			Doc:        render.markdown(pkg.doc.Doc, entryLevel-1),
			Synopsis:   pkg.doc.Synopsis(pkg.doc.Doc),
			Entries:    render.entries(pkg.doc),
		})
	}

	return packages, nil
}

// skippedDir reports a directory nobody outside the module can import.
func skippedDir(name string) bool {
	return name == "internal" || name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// parsedPackage is one directory read with go/doc, before rendering.
type parsedPackage struct {
	rel        string
	importPath string
	fset       *token.FileSet
	files      map[string]*ast.File
	doc        *doc.Package
}

// parseGoPackage reads one directory. It returns nil for one with no Go files.
func parseGoPackage(root, dir string) (*parsedPackage, error) {
	fset := token.NewFileSet()

	files, err := parseGoFiles(fset, dir)
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, nil //nolint:nilnil // a directory without Go files is not a package and not a failure.
	}

	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return nil, fmt.Errorf("relativise %s: %w", dir, err)
	}

	rel = filepath.ToSlash(rel)
	importPath := sdkModulePath

	if rel != "." {
		importPath = path.Join(sdkModulePath, rel)
	}

	byName := make(map[string]*ast.File, len(files))
	for _, file := range files {
		byName[fset.Position(file.Pos()).Filename] = file
	}

	docPkg, err := doc.NewFromFiles(fset, files, importPath)
	if err != nil {
		return nil, fmt.Errorf("document %s: %w", dir, err)
	}

	return &parsedPackage{rel: rel, importPath: importPath, fset: fset, files: byName, doc: docPkg}, nil
}

// knownAnchors is every anchor on every page: one per package and one per
// exported identifier. An anchor includes its package, so one set is enough.
func knownAnchors(parsed []parsedPackage) map[string]bool {
	known := map[string]bool{}

	for _, pkg := range parsed {
		known[goPackageAnchor(pkg.rel)] = true

		values := func(groups []*doc.Value) {
			for _, group := range groups {
				for _, name := range group.Names {
					known[goAnchor(pkg.rel, "", name)] = true
				}
			}
		}

		values(pkg.doc.Consts)
		values(pkg.doc.Vars)

		for _, f := range pkg.doc.Funcs {
			known[goAnchor(pkg.rel, "", f.Name)] = true
		}

		for _, t := range pkg.doc.Types {
			known[goAnchor(pkg.rel, "", t.Name)] = true

			values(t.Consts)
			values(t.Vars)

			for _, f := range t.Funcs {
				known[goAnchor(pkg.rel, "", f.Name)] = true
			}

			for _, m := range t.Methods {
				known[goAnchor(pkg.rel, t.Name, m.Name)] = true
			}
		}
	}

	return known
}

// parseGoFiles parses the non-test Go files of one directory in name order.
// Two package names in one directory is an error.
func parseGoFiles(fset *token.FileSet, dir string) ([]*ast.File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}

	var (
		files []*ast.File
		name  string
	)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		file, parseErr := parser.ParseFile(fset, filepath.Join(dir, entry.Name()), nil, parser.ParseComments)
		if parseErr != nil {
			return nil, fmt.Errorf("parse %s: %w", entry.Name(), parseErr)
		}

		if name != "" && file.Name.Name != name {
			return nil, fmt.Errorf("%w: %s holds %s and %s", errTwoPackages, dir, name, file.Name.Name)
		}

		name = file.Name.Name

		files = append(files, file)
	}

	return files, nil
}

// goRenderer renders one package's declarations and comments as markdown.
type goRenderer struct {
	fset   *token.FileSet
	files  map[string]*ast.File
	parser *comment.Parser
	rel    string
	// known is every anchor on every page. A doc link is written only when
	// its anchor is in it.
	known map[string]bool
}

func newGoRenderer(pkg parsedPackage, known map[string]bool) *goRenderer {
	return &goRenderer{fset: pkg.fset, files: pkg.files, parser: pkg.doc.Parser(), rel: pkg.rel, known: known}
}

// entries lists the exported identifiers in go/doc order: constants,
// variables, functions, then each type with its members.
func (r *goRenderer) entries(pkg *doc.Package) []goEntry {
	var out []goEntry

	out = append(out, r.values("const", pkg.Consts, entryLevel)...)
	out = append(out, r.values("var", pkg.Vars, entryLevel)...)

	for _, f := range pkg.Funcs {
		out = append(out, r.function(f, "", entryLevel))
	}

	for _, t := range pkg.Types {
		out = append(out, goEntry{
			Kind:  "type",
			Name:  t.Name,
			Level: entryLevel,
			Decl:  r.typeDecl(t.Decl),
			Doc:   r.markdown(t.Doc, entryLevel),
		})

		out = append(out, r.values("const", t.Consts, memberLevel)...)
		out = append(out, r.values("var", t.Vars, memberLevel)...)

		for _, f := range t.Funcs {
			out = append(out, r.function(f, "", memberLevel))
		}

		for _, m := range t.Methods {
			out = append(out, r.function(m, t.Name, memberLevel))
		}
	}

	return out
}

// values renders one entry per exported name of a const or var group. The
// comment is the name's own, else the line comment beside it, else the group's.
func (r *goRenderer) values(kind string, groups []*doc.Value, level int) []goEntry {
	var out []goEntry

	for _, group := range groups {
		for _, spec := range group.Decl.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}

			text := group.Doc
			if value.Doc != nil {
				text = value.Doc.Text()
			} else if value.Comment != nil {
				text = value.Comment.Text()
			}

			decl := r.valueDecl(group.Decl.Tok, value)

			for _, name := range value.Names {
				if !ast.IsExported(name.Name) {
					continue
				}

				out = append(out, goEntry{
					Kind:  kind,
					Name:  name.Name,
					Level: level,
					Decl:  decl,
					Doc:   r.markdown(text, level),
				})
			}
		}
	}

	return out
}

func (r *goRenderer) function(f *doc.Func, owner string, level int) goEntry {
	name := f.Name
	kind := "func"

	if owner != "" {
		name = owner + "." + f.Name
		kind = "method"
	}

	return goEntry{
		Kind:  kind,
		Name:  name,
		Level: level,
		Decl:  r.funcDecl(f.Decl),
		Doc:   r.markdown(f.Doc, level),
	}
}

// funcDecl prints a signature without its body or its comment.
func (r *goRenderer) funcDecl(decl *ast.FuncDecl) string {
	bare := *decl
	bare.Body = nil
	bare.Doc = nil

	return r.source(&bare, nil)
}

// valueDecl prints one spec as its own declaration, `const MaxOrigins = 512`,
// even when the source grouped it.
func (r *goRenderer) valueDecl(tok token.Token, spec *ast.ValueSpec) string {
	bare := *spec
	bare.Doc = nil
	bare.Comment = nil

	return r.source(&ast.GenDecl{Tok: tok, Specs: []ast.Spec{&bare}}, nil)
}

// typeDecl prints a type with the comments inside it. Field and method
// comments are part of the reference.
func (r *goRenderer) typeDecl(decl *ast.GenDecl) string {
	bare := *decl
	bare.Doc = nil
	bare.Specs = make([]ast.Spec, 0, len(decl.Specs))

	for _, spec := range decl.Specs {
		if ts, ok := spec.(*ast.TypeSpec); ok {
			copied := *ts
			copied.Doc = nil
			copied.Comment = nil
			bare.Specs = append(bare.Specs, &copied)

			continue
		}

		bare.Specs = append(bare.Specs, spec)
	}

	var comments []*ast.CommentGroup
	if file := r.files[r.fset.Position(decl.Pos()).Filename]; file != nil {
		comments = file.Comments
	}

	return r.source(&bare, comments)
}

func (r *goRenderer) source(node ast.Node, comments []*ast.CommentGroup) string {
	var buf bytes.Buffer

	config := printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: tabWidth}

	var err error
	if comments == nil {
		err = config.Fprint(&buf, r.fset, node)
	} else {
		err = config.Fprint(&buf, r.fset, &printer.CommentedNode{Node: node, Comments: comments})
	}

	if err != nil {
		// go/printer fails only on a malformed AST, which the parser has
		// already refused.
		return "// " + err.Error()
	}

	return strings.ReplaceAll(buf.String(), "\t", "    ")
}

// markdown renders a doc comment. Headings get no explicit id, because
// go/doc's `hdr-` ids contain underscores the site's heading-id plugin rejects.
func (r *goRenderer) markdown(text string, level int) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}

	pr := &comment.Printer{
		HeadingLevel: level + 1,
		HeadingID:    func(*comment.Heading) string { return "" },
		DocLinkURL:   r.docLinkURL,
	}

	return strings.TrimSpace(string(pr.Markdown(r.parser.Parse(text))))
}

func (r *goRenderer) docLinkURL(link *comment.DocLink) string {
	rel := r.rel

	switch {
	case link.ImportPath == "":
	case strings.HasPrefix(link.ImportPath, sdkModulePath):
		rel = strings.TrimPrefix(strings.TrimPrefix(link.ImportPath, sdkModulePath), "/")
	default:
		return link.DefaultURL(pkgGoDev)
	}

	anchor := goPackageAnchor(rel)
	if link.Name != "" {
		anchor = goAnchor(rel, link.Recv, link.Name)
	}

	// A link to a field or to an unexported name has no anchor on any page.
	// It is printed as text.
	if !r.known[anchor] {
		return ""
	}

	// Another package is another page in the same directory.
	if rel != r.rel {
		return goPageName(rel) + "#" + anchor
	}

	return "#" + anchor
}

// goAnchor is the anchor of one identifier: package path, receiver if any,
// and name. `busapi.MaxOrigins` is `#busapi-maxorigins`.
func goAnchor(rel, recv, name string) string {
	identifier := rel + "." + name
	if recv != "" {
		identifier = rel + "." + recv + "." + name
	}

	return docsref.Anchor(identifier)
}

// goPackageAnchor is the anchor of a package heading: `#busapi`, or
// `#authapi-authapitest` for a nested package.
func goPackageAnchor(rel string) string {
	return docsref.Anchor(rel)
}
