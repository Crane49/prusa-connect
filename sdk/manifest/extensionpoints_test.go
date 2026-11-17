package manifest_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// The keys of ADR-20261027-wvj: a module that reads files for the core, the
// file_types filter on the slots handed a file, and the components one bundle
// hands another.

func TestAModuleReadsFilesOfTheTypesItNames(t *testing.T) {
	t.Parallel()

	document := `{"name":"reader","kind":"sink","license":"MIT","contract_major":1,
	  "file_analysis":{"types":[".demo",".demo2"]}}`

	held, err := manifest.Parse("module.yaml", []byte(document), decode)
	if err != nil {
		t.Fatalf("a module that reads files was refused: %v", err)
	}

	if held.FileAnalysis == nil || !slices.Equal(held.FileAnalysis.Types, []string{".demo", ".demo2"}) {
		t.Errorf("file_analysis read as %+v", held.FileAnalysis)
	}
}

func TestAFileAnalysisBlockIsHeldToItsShape(t *testing.T) {
	t.Parallel()

	block := func(types string) string {
		return `{"name":"r","kind":"sink","license":"MIT","contract_major":1,"file_analysis":{"types":` + types + `}}`
	}

	cases := map[string]struct{ document, refuses string }{
		"no types": {
			`{"name":"r","kind":"sink","license":"MIT","contract_major":1,"file_analysis":{}}`,
			"types is absent",
		},
		"an empty list":           {block(`[]`), "is empty"},
		"an upper case extension": {block(`[".3MF"]`), "expected a lowercase extension"},
		"a bare word":             {block(`["3mf"]`), "expected a lowercase extension"},
		"a media type":            {block(`["model/3mf"]`), "expected a lowercase extension"},
		"a type twice":            {block(`[".a",".a"]`), "names .a twice"},
		"no process": {
			`{"name":"r","kind":"asset","license":"MIT","file_analysis":{"types":[".a"]}}`, "contract_major is not",
		},
		"a key nobody reads": {
			`{"name":"r","kind":"sink","license":"MIT","contract_major":1,"file_analysis":{"types":[".a"],"budget":2}}`,
			"budget",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := manifest.Parse("module.yaml", []byte(tc.document), decode)
			if err == nil || !strings.Contains(err.Error(), tc.refuses) {
				t.Fatalf("the refusal is %v and does not name %q", err, tc.refuses)
			}
		})
	}
}

func TestASlotHandedAFileTakesFileTypes(t *testing.T) {
	t.Parallel()

	held, err := manifest.Parse("module.yaml", []byte(withUI(`{"entry":"ui/index.js","library":">=0.0.1",
	  "contributes":{
	    "file_view":[{"id":"project","title":"Project","file_types":[".3mf"]}],
	    "file_action":[{"id":"slice","title":"Slice","file_types":[".3mf",".stl"]}],
	    "print_section":[{"id":"plates","title":"Plates"}]}}`)), decode)
	if err != nil {
		t.Fatalf("file types on a file slot were refused: %v", err)
	}

	if got := held.UI.Contributes.Sections[manifest.SlotFileView][0].FileTypes; !slices.Equal(got, []string{".3mf"}) {
		t.Errorf("the file view read file types %v", got)
	}

	if got := held.UI.Contributes.Actions[manifest.SlotFileAction][0].FileTypes; len(got) != 2 {
		t.Errorf("the file action read file types %v", got)
	}

	if got := held.UI.Contributes.Sections[manifest.SlotPrintSection][0].FileTypes; got != nil {
		t.Errorf("a section with no filter read %v, and empty is every file", got)
	}

	_, err = manifest.Parse("module.yaml", []byte(withUI(`{"entry":"ui/index.js","library":">=0.0.1",
	  "contributes":{"device_section":[{"id":"x","title":"X","file_types":[".3mf"]}]}}`)), decode)
	if err == nil || !strings.Contains(err.Error(), "file_types") {
		t.Fatalf("file types on a slot handed a device were not refused: %v", err)
	}
}

func TestAFileTypeMatchesAName(t *testing.T) {
	t.Parallel()

	types := []string{".3mf"}

	for name, want := range map[string]bool{
		"plate.3mf": true, "PLATE.3MF": true, ".3mf": false, "plate.3mf.gcode": false, "plate.stl": false,
	} {
		if got := manifest.MatchesFileType(types, name); got != want {
			t.Errorf("%s matches %v", name, got)
		}
	}

	if !manifest.MatchesFileType(nil, "anything.gcode") {
		t.Error("an empty list did not match every file")
	}
}

func TestABundleExportsAndImportsComponents(t *testing.T) {
	t.Parallel()

	document := `{"name":"spools","kind":"module","license":"MIT",
	  "suggest":{"com.github.aux.acme.reader":"Draws the project view"},
	  "ui":{"entry":"ui/index.js","library":">=0.0.1",
	    "exports":[{"name":"spool-picker"}],
	    "imports":[{"module":"com.github.aux.acme.reader","name":"project-view"}]}}`

	held, err := manifest.Parse("module.yaml", []byte(document), decode)
	if err != nil {
		t.Fatalf("a bundle with exports and imports was refused: %v", err)
	}

	if held.UI.Exports[0] != (manifest.UIExport{Name: "spool-picker"}) {
		t.Errorf("the export read as %+v", held.UI.Exports)
	}

	if held.UI.Imports[0] != (manifest.UIImport{Module: "com.github.aux.acme.reader", Name: "project-view"}) {
		t.Errorf("the import read as %+v", held.UI.Imports)
	}
}

func TestAComponentEntryIsHeldToItsShape(t *testing.T) {
	t.Parallel()

	ui := func(block string) string { return withUI(`{"entry":"ui/index.js","library":">=0.0.1",` + block + `}`) }

	cases := map[string]struct{ document, refuses string }{
		"an import naming no module": {ui(`"imports":[{"name":"view"}]`), "module is empty"},
		"an import naming no namespace": {
			ui(`"imports":[{"module":"Reader","name":"view"}]`),
			"expected a module namespace",
		},
		"an import of a module the manifest does not depend on": {
			ui(`"imports":[{"module":"com.github.aux.acme.reader","name":"view"}]`), "neither require nor suggest",
		},
		"an export named twice":      {ui(`"exports":[{"name":"p"},{"name":"p"}]`), "names p twice"},
		"an export that is no label": {ui(`"exports":[{"name":"a/b"}]`), "name"},
		"a key nobody reads":         {ui(`"exports":[{"name":"p","contract":"x"}]`), "contract"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := manifest.Parse("module.yaml", []byte(tc.document), decode)
			if err == nil || !strings.Contains(err.Error(), tc.refuses) {
				t.Fatalf("the refusal is %v and does not name %q", err, tc.refuses)
			}
		})
	}
}
