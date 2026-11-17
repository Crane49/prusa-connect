package main

import (
	"errors"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/codegen/openapidoc"
	"github.com/prusa3d/prusa-connect/internal/docsref"
)

// The gates on the reference pages. Each fails, none warns:
//
//  1. a tag with no page, an operation that is not on its tag's page, a
//     registry entry that is not on its page, a package with no page, and an
//     exported Go identifier that is not on its package's page. The tests
//     read the sources with their own hands, so a reader in the command that
//     skips something is caught by a reader here that does not
//  2. a committed region that differs from the generated one, a missing
//     page, and a page that lost its markers, which is what `-check` refuses
//     and what `go test ./...` refuses here without anybody remembering the
//     target
//  3. two entries claiming one anchor
//  4. a page still on the generator's placeholder. This one is held apart
//     from gate 2, so a generator run stays green while the prose is owed,
//     and the failure names the page

const (
	errorCodesPath = "docs/errors/error-codes.json"
	eventTypesPath = "docs/vocabularies/event-types.json"
	authzPath      = "docs/vocabularies/authorization.json"

	// The three pages the registries are published on, beside the reference
	// directory. Each is a whole generated page, and an operation's problem
	// statuses link to the first.
	errorCodesPageFile = "error-codes.md"
	eventTypesPageFile = "event-types.md"
	authzPageFile      = "authorization-vocabulary.md"
)

// placeholderPages are the pages still carrying the placeholder this command
// writes when it creates a page. The list is a ratchet on the model of
// ADR-20260826-gtb rule 9: a page with a TODO that is not listed fails, and a listed
// page whose TODO is gone fails until its line is removed, so the list only
// shrinks. A person writes the page and removes the line in one change.
//
//nolint:gochecknoglobals // a table one test reads; there is nowhere cheaper.
var placeholderPages []string

// repoRoot is the tree as the command reads it. A test runs in the package
// directory, which is two levels under the root.
func repoRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve the repository root: %v", err)
	}

	return root
}

func committed(t *testing.T) options {
	t.Helper()

	root := repoRoot(t)

	return options{
		documentPath:  filepath.Join(root, filepath.FromSlash(defaultDocumentPath)),
		sdkDir:        filepath.Join(root, defaultSDKDir),
		extensionsDir: filepath.Join(root, defaultExtensionsDir),
		referenceDir:  filepath.Join(root, filepath.FromSlash(defaultReferenceDir)),
	}
}

// committedPage reads one page under the reference directory, or fails
// naming the page and the target that writes it.
func committedPage(t *testing.T, opts options, name string) string {
	t.Helper()

	path := filepath.Join(opts.referenceDir, filepath.FromSlash(name))

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v. Run make %s and commit it", path, err, makeTarget)
	}

	return string(body)
}

// TestTheCommittedPagesMatchTheSources is gate 2, as a test so it runs under
// `go test ./...`.
func TestTheCommittedPagesMatchTheSources(t *testing.T) {
	t.Parallel()

	opts := committed(t)
	opts.check = true

	if err := run(opts); err != nil {
		t.Fatalf("the committed pages are stale: %v", err)
	}
}

// TestEveryTagHasAPageAndEveryOperationIsOnIt is gate 1 for the API. Every
// tag the document declares has a page, every operation is anchored on the
// page of its tag and on no other, and no page sits under api/ for a tag the
// document does not declare (ADR-20260910-he3 rule 4).
func TestEveryTagHasAPageAndEveryOperationIsOnIt(t *testing.T) {
	t.Parallel()

	opts := committed(t)

	document, err := openapidoc.Read(opts.documentPath)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := document.Operations()
	if err != nil {
		t.Fatal(err)
	}

	ops := operationsOf(decoded)

	requireAtLeast(t, "tags in the document", len(document.Tags), 10)
	requireAtLeast(t, "operations in the document", len(ops), 50)

	pages := map[string]string{}
	for _, tag := range document.Tags {
		pages[tag.Name] = committedPage(t, opts, apiDir+"/"+apiPageName(tag.Name))
	}

	for _, op := range ops {
		anchor := docsref.Anchor(op.ID)

		for tag, body := range pages {
			onPage := strings.Contains(body, "{#"+anchor+"}")
			belongs := slices.Contains(op.Tags, tag)

			switch {
			case belongs && !onPage:
				t.Errorf("%s is not on %s: no heading carries {#%s}. Run make %s and commit it",
					op.ID, apiPageName(tag), anchor, makeTarget)
			case !belongs && onPage:
				t.Errorf("%s is on %s, which is not its tag's page", op.ID, apiPageName(tag))
			}
		}
	}

	for _, name := range pagesUnder(t, filepath.Join(opts.referenceDir, apiDir)) {
		if _, declared := pages[strings.TrimSuffix(name, ".md")]; !declared {
			t.Errorf("%s/%s is a page for a tag the document does not declare", apiDir, name)
		}
	}
}

// TestEveryRegistryEntryIsOnItsPage is gate 1 for the three registries an
// operation's problem statuses and a subscription's types point at. Each
// registry is read here from its committed JSON, and each entry is looked
// for on the page that publishes it.
func TestEveryRegistryEntryIsOnItsPage(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	opts := committed(t)

	codes, err := readRegistry[errorCodesFile](filepath.Join(root, filepath.FromSlash(errorCodesPath)))
	if err != nil {
		t.Fatal(err)
	}

	events, err := readRegistry[eventTypesFile](filepath.Join(root, filepath.FromSlash(eventTypesPath)))
	if err != nil {
		t.Fatal(err)
	}

	authz, err := readRegistry[authzFile](filepath.Join(root, filepath.FromSlash(authzPath)))
	if err != nil {
		t.Fatal(err)
	}

	requireAtLeast(t, "error codes", len(codes.Codes), 40)
	requireAtLeast(t, "event types", len(events.Entries), 10)
	requireAtLeast(t, "authorization actions", len(authz.Actions), 5)

	codesPage := committedPage(t, opts, "../"+errorCodesPageFile)
	for _, c := range codes.Codes {
		requireAnchored(t, codesPage, errorCodesPageFile, docsref.Anchor(c.Code), c.Code)
	}

	eventsPage := committedPage(t, opts, "../"+eventTypesPageFile)
	for _, e := range events.Entries {
		requireAnchored(t, eventsPage, eventTypesPageFile, docsref.Anchor(e.Type), e.Type)
	}

	authzPage := committedPage(t, opts, "../"+authzPageFile)
	for _, a := range authz.Actions {
		requireAnchored(t, authzPage, authzPageFile, docsref.AnchorUnder("action", a.Action), a.Action)
	}

	for _, ty := range authz.Types {
		requireAnchored(t, authzPage, authzPageFile, docsref.AnchorUnder("type", ty.Type), ty.Type)
	}

	for _, r := range authz.Relations {
		requireAnchored(t, authzPage, authzPageFile, docsref.AnchorUnder("relation", r.Relation), r.Relation)
	}
}

// TestEveryPackageHasAPageAndEveryExportedIdentifierIsOnIt is gate 1 for the
// sdk. The packages are walked here with go/doc directly, so a directory the
// command's walk skips, or a kind of declaration its renderer drops, is a
// red line here and not a silent gap on a page. A page under go-sdk/ with no
// package behind it is refused too.
func TestEveryPackageHasAPageAndEveryExportedIdentifierIsOnIt(t *testing.T) {
	t.Parallel()

	opts := committed(t)

	packages := 0
	identifiers := 0
	expected := map[string]bool{}

	err := filepath.WalkDir(opts.sdkDir, func(dir string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !entry.IsDir() {
			return nil
		}

		if dir != opts.sdkDir && (entry.Name() == "internal" || entry.Name() == "testdata") {
			return filepath.SkipDir
		}

		rel, err := filepath.Rel(opts.sdkDir, dir)
		if err != nil {
			return err
		}

		rel = filepath.ToSlash(rel)

		pkg := docPackage(t, dir, rel)
		if pkg == nil {
			return nil
		}

		packages++
		expected[goPageName(rel)] = true

		pageName := goDir + "/" + goPageName(rel)
		body := committedPage(t, opts, pageName)

		requireAnchored(t, body, pageName, goPackageAnchor(rel), "package "+rel)

		for _, name := range exportedNames(pkg) {
			identifiers++

			recv, base, _ := strings.Cut(name, ".")
			if base == "" {
				recv, base = "", recv
			}

			requireAnchored(t, body, pageName, goAnchor(rel, recv, base), rel+"."+name)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	requireAtLeast(t, "importable sdk packages", packages, 20)
	requireAtLeast(t, "exported sdk identifiers", identifiers, 1000)

	for _, name := range pagesUnder(t, filepath.Join(opts.referenceDir, goDir)) {
		if !expected[name] {
			t.Errorf("%s/%s is a page for a package the sdk does not hold", goDir, name)
		}
	}
}

// TestEveryPageIntroIsWritten is gate 4. A page this command created carries
// a TODO until a person writes it, and the list above says which pages are
// still owed.
// TestEveryExtensionHasAPage is gate 1 for the official extensions. Every
// folder under extensions/ with a module.yaml has a page, the index links
// it, and no page sits under extensions/ for a folder that is gone.
func TestEveryExtensionHasAPage(t *testing.T) {
	t.Parallel()

	opts := committed(t)

	exts, err := readExtensions(opts.extensionsDir)
	if err != nil {
		t.Fatal(err)
	}

	requireAtLeast(t, "official extensions", len(exts), 4)

	index := committedPage(t, opts, extDir+"/"+extIndexName)
	want := map[string]bool{extIndexName: true}

	for _, ext := range exts {
		name := extPageName(ext.Folder)
		want[name] = true

		committedPage(t, opts, extDir+"/"+name)

		if !strings.Contains(index, "](./"+name+")") {
			t.Errorf("the index of %s does not link %s", extDir, name)
		}
	}

	for _, name := range pagesUnder(t, filepath.Join(opts.referenceDir, extDir)) {
		if !want[name] {
			t.Errorf("%s/%s is a page for an extension extensions/ does not hold", extDir, name)
		}
	}
}

func TestEveryPageIntroIsWritten(t *testing.T) {
	t.Parallel()

	opts := committed(t)

	listed := map[string]bool{}
	for _, name := range placeholderPages {
		listed[name] = true
	}

	var found []string

	for _, dir := range []string{apiDir, goDir, extDir} {
		for _, name := range pagesUnder(t, filepath.Join(opts.referenceDir, dir)) {
			pageName := dir + "/" + name
			found = append(found, pageName)

			prose, err := handWritten([]byte(committedPage(t, opts, pageName)))
			if err != nil {
				t.Errorf("%s: %v", pageName, err)

				continue
			}

			owed := strings.Contains(prose, placeholderMark)

			switch {
			case owed && !listed[pageName]:
				t.Errorf("%s is still on the generator's placeholder: a person writes what it is for, "+
					"how it works and when to use it, and removes the %s line", pageName, placeholderMark)
			case !owed && listed[pageName]:
				t.Errorf("%s is written. Remove it from placeholderPages", pageName)
			}
		}
	}

	requireAtLeast(t, "reference pages", len(found), 30)

	for _, name := range placeholderPages {
		if !slices.Contains(found, name) {
			t.Errorf("placeholderPages names %s, which is not a page. Remove the line", name)
		}
	}
}

// TestTheGeneratorNeverTouchesProse is the other half of ADR-20260910-he3 rule 2. A
// person's text above and below the markers survives a run, and only the
// region moves.
func TestTheGeneratorNeverTouchesProse(t *testing.T) {
	t.Parallel()

	opts := committed(t)
	opts.referenceDir = t.TempDir()

	if err := run(opts); err != nil {
		t.Fatalf("first run: %v", err)
	}

	path := filepath.Join(opts.referenceDir, apiDir, apiPageName("devices"))

	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	prose, err := handWritten(original)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(prose, placeholderMark) {
		t.Fatalf("a fresh page carries no %s for the intro gate to refuse", placeholderMark)
	}

	const above, below = "A person wrote this above.\n\n", "\n\nAnd this below.\n"

	edited := strings.Replace(string(original), regionStartPrefix, above+regionStartPrefix, 1)
	edited = strings.Replace(edited, regionEnd+"\n", regionEnd+below, 1)

	if err = os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}

	if err = run(opts); err != nil {
		t.Fatalf("second run: %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(after) != edited {
		t.Fatalf("a run changed a page whose region was current:\n%s", after)
	}
}

// TestCheckRefusesAStaleRegionAndAMissingPage is gate 2's mechanism: a page
// whose region moved is refused with the sentinel, a page that is not there
// is refused too, a page that lost its markers is refused by name, and a
// page edited outside the region passes.
func TestCheckRefusesAStaleRegionAndAMissingPage(t *testing.T) {
	t.Parallel()

	opts := committed(t)
	opts.referenceDir = t.TempDir()

	if err := run(opts); err != nil {
		t.Fatalf("first run: %v", err)
	}

	opts.check = true

	if err := run(opts); err != nil {
		t.Fatalf("a page just written is reported stale: %v", err)
	}

	path := filepath.Join(opts.referenceDir, goDir, goPageName("busapi"))

	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	write := func(body string) {
		t.Helper()

		if writeErr := os.WriteFile(path, []byte(body), 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
	}

	write(strings.Replace(string(original), regionStartPrefix, "Prose above.\n\n"+regionStartPrefix, 1))

	if err = run(opts); err != nil {
		t.Fatalf("a page edited outside its region failed -check: %v", err)
	}

	write(strings.Replace(string(original), regionEnd, "edited by hand\n"+regionEnd, 1))

	if err = run(opts); !errors.Is(err, errStale) {
		t.Fatalf("an edited region passed -check: %v", err)
	}

	write(strings.Replace(string(original), regionEnd, "", 1))

	if err = run(opts); !errors.Is(err, errNoRegion) {
		t.Fatalf("a page without its closing marker was not refused as such: %v", err)
	}

	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}

	if err = run(opts); !errors.Is(err, errStale) {
		t.Fatalf("a missing page passed -check: %v", err)
	}
}

// TestSpliceReplacesOnlyTheRegion holds the splice to its two edges: the
// bytes before the opening marker and after the closing one are the same
// bytes afterwards.
func TestSpliceReplacesOnlyTheRegion(t *testing.T) {
	t.Parallel()

	region := regionOf("the x tag", "regenerate.", "new body")
	committed := []byte("head\n\n" + string(regionOf("the x tag", "regenerate.", "old body")) + "\n\ntail\n")

	got, err := splice(committed, region)
	if err != nil {
		t.Fatal(err)
	}

	want := "head\n\n" + string(region) + "\n\ntail\n"
	if string(got) != want {
		t.Fatalf("splice =\n%s\nwant\n%s", got, want)
	}

	if _, err = splice([]byte("no markers\n"), region); !errors.Is(err, errNoRegion) {
		t.Fatalf("a page with no markers was spliced: %v", err)
	}

	if _, err = splice([]byte(regionEnd+"\n"+regionStartPrefix+"x -->\n"), region); !errors.Is(err, errNoRegion) {
		t.Fatalf("markers in the wrong order were spliced: %v", err)
	}
}

// TestTwoEntriesCannotShareAnAnchor is gate 3. An anchor is a link somebody
// wrote down, and it points at one thing.
func TestTwoEntriesCannotShareAnAnchor(t *testing.T) {
	t.Parallel()

	taken := anchors{}

	if err := taken.claim("busapi-maxorigins", "busapi.MaxOrigins"); err != nil {
		t.Fatal(err)
	}

	err := taken.claim("busapi-maxorigins", "busapi.MaxORIGINS")
	if !errors.Is(err, errDuplicateName) {
		t.Fatalf("a second claim on one anchor was accepted: %v", err)
	}
}

// docPackage reads one directory with go/doc, or nil when it holds no Go
// file outside the tests.
func docPackage(t *testing.T, dir, rel string) *doc.Package {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()

	var files []*ast.File

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, parseErr := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ParseComments)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}

		files = append(files, file)
	}

	if len(files) == 0 {
		return nil
	}

	pkg, err := doc.NewFromFiles(fset, files, sdkModulePath+"/"+rel)
	if err != nil {
		t.Fatalf("document %s: %v", rel, err)
	}

	return pkg
}

// exportedNames is every exported identifier go/doc lists for a package, a
// method as `Type.Method`.
func exportedNames(pkg *doc.Package) []string {
	var names []string

	values := func(groups []*doc.Value) {
		for _, group := range groups {
			names = append(names, group.Names...)
		}
	}

	values(pkg.Consts)
	values(pkg.Vars)

	for _, f := range pkg.Funcs {
		names = append(names, f.Name)
	}

	for _, ty := range pkg.Types {
		names = append(names, ty.Name)

		values(ty.Consts)
		values(ty.Vars)

		for _, f := range ty.Funcs {
			names = append(names, f.Name)
		}

		for _, m := range ty.Methods {
			names = append(names, ty.Name+"."+m.Name)
		}
	}

	return names
}

// pagesUnder lists the markdown files of one page directory by name.
func pagesUnder(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v. Run make %s and commit it", dir, err, makeTarget)
	}

	var names []string

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			names = append(names, entry.Name())
		}
	}

	return names
}

func requireAnchored(t *testing.T, page, pageName, anchor, owner string) {
	t.Helper()

	if !strings.Contains(page, "{#"+anchor+"}") {
		t.Errorf("%s is not on %s: no heading carries {#%s}. Run make %s and commit it",
			owner, pageName, anchor, makeTarget)
	}
}

// requireAtLeast arms a gate: a reader that finds nothing proves nothing.
func requireAtLeast(t *testing.T, what string, count, floor int) {
	t.Helper()

	if count < floor {
		t.Fatalf("read %d %s, under the floor of %d, so this test proved nothing", count, what, floor)
	}
}
