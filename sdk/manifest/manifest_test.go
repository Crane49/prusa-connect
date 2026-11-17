package manifest_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// decode is the reader every test here brings. A JSON document is a YAML
// document, and encoding/json is the one decoder this module may import, so
// the rules are proven on the same trees the host's yaml.Unmarshal produces.
func decode(document []byte, into any) error {
	//nolint:wrapcheck // the decoder a caller passes in reports its own errors.
	return json.Unmarshal(document, into)
}

// full is a manifest with every key filled in, so a case that changes one is
// testing that key and nothing else.
const full = `{
  "name": "spools",
  "kind": "module","license":"MIT",
  "version": "1.4.2",
  "contract_major": 1,
  "require": {
    "core": "0.9.0",
    "core-core.feature.ui": "*",
    "com.github.aux.acme.planner": "^2.0"
  },
  "suggest": {"com.github.aux.acme.slicer": "Slices a project before it is planned"},
  "conflict": {"com.github.aux.acme.old": "<2.0"},
  "permissions": ["view_device", "operate_device"],
  "checks": ["operate"],
  "check_absent_people": true,
  "list_people": true,
  "records_mib": 128,
  "events": {"loaded": "spool.loaded"},
  "calls": ["com.github.aux.acme.slicer"],
  "openapi": "api/openapi.json",
  "platforms": ["linux_amd64", "linux_arm64"],
  "build": {"linux_amd64": "make build GOOS=linux GOARCH=amd64"},
  "title": "Spool tracker",
  "codeowner": "acme",
  "issue_tracker": "https://github.com/aux/acme/issues",
  "lifecycle": "beta",
  "ui": {
    "entry": "ui/index.js",
    "library": ">=0.3.0 <1.0.0",
    "contributes": {
      "pages": [{"path": "spools", "title": "Spools", "icon": "layers", "hidden": false}],
      "device_card": [{"id": "spool-status", "title": "Loaded spool"}],
      "settings": [{"id": "spools", "title": "Spools"}]
    }
  }
}`

func TestAFullManifestReadsEveryKey(t *testing.T) {
	t.Parallel()

	held, err := manifest.Parse("module.yaml", []byte(full), decode)
	if err != nil {
		t.Fatalf("a full manifest was refused: %v", err)
	}

	if held.Name != "spools" || held.Kind != "module" || held.Version != "1.4.2" || held.License != "MIT" {
		t.Errorf("the identity read as %q %q %q %q", held.Name, held.Kind, held.Version, held.License)
	}

	if held.ContractMajor != 1 || held.Core != "0.9.0" {
		t.Errorf("the contract read as %d and core %q", held.ContractMajor, held.Core)
	}

	if len(held.Requires) != 3 || held.Requires[0].Module != "com.github.aux.acme.planner" ||
		held.Requires[1].Core != "0.9.0" || held.Requires[2].Capability != "core.feature.ui" {
		t.Errorf("requires read as %+v", held.Requires)
	}

	if len(held.Permissions) != 2 || held.Events["loaded"] != "spool.loaded" {
		t.Errorf("the requests read as %v and %v", held.Permissions, held.Events)
	}

	if !slices.Equal(held.Checks, []string{"operate"}) {
		t.Errorf("the checks read as %v", held.Checks)
	}

	if !held.CheckAbsentPeople {
		t.Error("check_absent_people read as false")
	}

	if len(held.Calls) != 1 || held.Calls[0] != "com.github.aux.acme.slicer" {
		t.Errorf("calls read as %v", held.Calls)
	}

	if !held.ListPeople {
		t.Error("list_people read as false")
	}

	if held.RecordsMiB != 128 {
		t.Errorf("records_mib read as %d", held.RecordsMiB)
	}

	if held.OpenAPI != "api/openapi.json" || len(held.Platforms) != 2 || len(held.Build) != 1 {
		t.Errorf("the release read as %q %v %v", held.OpenAPI, held.Platforms, held.Build)
	}

	if held.Title != "Spool tracker" {
		t.Errorf("title read as %q", held.Title)
	}

	if held.Codeowner != "acme" || held.Lifecycle != "beta" || held.IssueTracker == "" {
		t.Errorf("the catalogue keys read as %q %q %q", held.Codeowner, held.Lifecycle, held.IssueTracker)
	}

	if held.UI == nil {
		t.Fatal("the ui block was dropped")
	}
}

func TestAManifestWithNoUIBlockCarriesNone(t *testing.T) {
	t.Parallel()

	held, err := manifest.Parse("module.yaml", []byte(`{"name":"planner","kind":"connector","license":"MIT"}`), decode)
	if err != nil {
		t.Fatalf("a manifest with no ui block was refused: %v", err)
	}

	if held.UI != nil {
		t.Errorf("a module that draws nothing carries a ui block: %+v", held.UI)
	}

	if held.CheckAbsentPeople {
		t.Error("a manifest that does not ask about absent people was read as asking")
	}

	if held.ListPeople {
		t.Error("a manifest that does not ask to list people was read as asking")
	}
}

// TestTheTopLevelKeysAreValidated is the table for everything outside the ui
// block. Each case names the key it breaks.
func TestTheTopLevelKeysAreValidated(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		document string
		refuses  string
	}{
		"name absent":      {`{"kind":"module"}`, "name is empty"},
		"name empty":       {`{"name":"  ","kind":"module","license":"MIT"}`, "name is empty"},
		"name not text":    {`{"name":7,"kind":"module"}`, "name is a number"},
		"kind absent":      {`{"name":"spools"}`, "kind is empty"},
		"license absent":   {`{"name":"s","kind":"module"}`, "license is empty"},
		"license empty":    {`{"name":"s","kind":"module","license":" "}`, "license is empty"},
		"license not text": {`{"name":"s","kind":"module","license":["MIT"]}`, "license is a list"},
		"license with an escape": {
			`{"name":"s","kind":"module","license":"MIT\u001b[31m"}`,
			"control or invisible character",
		},
		"license reversed": {
			`{"name":"s","kind":"module","license":"MIT\u202eTIM"}`,
			"control or invisible character",
		},
		"license over two lines": {
			`{"name":"s","kind":"module","license":"MIT\nApache"}`,
			"control or invisible character",
		},
		"license with a zero width": {
			`{"name":"s","kind":"module","license":"M\u200bIT"}`,
			"control or invisible character",
		},
		"license too long": {
			`{"name":"s","kind":"module","license":"` + strings.Repeat("x", 201) + `"}`,
			"license is 201 characters",
		},
		"version not a triple": {
			`{"name":"s","kind":"module","license":"MIT","version":"1.4"}`,
			"version is \"1.4\"",
		},
		"version zero padded": {`{"name":"s","kind":"module","license":"MIT","version":"1.04.2"}`, "zero-padded"},
		"contract major a word": {
			`{"name":"s","kind":"module","license":"MIT","contract_major":"one"}`,
			"contract_major is text",
		},
		"contract major a float": {`{"name":"s","kind":"module","license":"MIT","contract_major":1.5}`, "whole number"},
		"contract major below": {
			`{"name":"s","kind":"module","license":"MIT","contract_major":-1}`,
			"expected 1 or more",
		},
		"core not a constraint": {
			`{"name":"s","kind":"module","license":"MIT","require":{"core":"latest"}}`,
			"require.core is \"latest\"",
		},
		"a capability with a version": {
			`{"name":"s","kind":"module","license":"MIT","require":{"core-device.list":"^1"}}`,
			"a capability has no version",
		},
		"require not a map":    {`{"name":"s","kind":"module","license":"MIT","require":[]}`, "require is a list"},
		"requires is gone":     {`{"name":"s","kind":"module","license":"MIT","requires":[]}`, "does not read"},
		"the core key is gone": {`{"name":"s","kind":"module","license":"MIT","core":"1.0.0"}`, "does not read"},
		"permissions repeated": {
			`{"name":"s","kind":"module","license":"MIT","permissions":["a","a"]}`,
			`permissions names "a" twice`,
		},
		"checks repeated": {
			`{"name":"s","kind":"module","license":"MIT","checks":["a","a"]}`,
			`checks names "a" twice`,
		},
		"checks a permission too": {
			`{"name":"s","kind":"module","license":"MIT","permissions":["operate"],"checks":["operate"]}`,
			`checks names "operate", which permissions names too`,
		},
		"permissions empty item": {
			`{"name":"s","kind":"module","license":"MIT","permissions":["a",""]}`,
			"permissions[1] is empty",
		},
		"check absent people a word": {
			`{"name":"s","kind":"module","license":"MIT","check_absent_people":"yes"}`,
			"check_absent_people is text, expected true or false",
		},
		"list people a word": {
			`{"name":"s","kind":"module","license":"MIT","list_people":"yes"}`,
			"list_people is text, expected true or false",
		},
		"records mib a word": {
			`{"name":"s","kind":"module","license":"MIT","records_mib":"lots"}`,
			"records_mib is text, expected a whole number",
		},
		"records mib negative": {`{"name":"s","kind":"module","license":"MIT","records_mib":-1}`, "records_mib is -1"},
		"records mib past a gibibyte": {
			`{"name":"s","kind":"module","license":"MIT","records_mib":1025}`,
			"records_mib is 1025, expected 1 to 1024",
		},
		"calls not a namespace": {
			`{"name":"s","kind":"module","license":"MIT","calls":["Slicer"]}`,
			`calls names "Slicer", which is not the namespace of a module`,
		},
		"calls one label": {`{"name":"s","kind":"module","license":"MIT","calls":["slicer"]}`, "not the namespace"},
		"calls repeated": {
			`{"name":"s","kind":"module","license":"MIT","calls":["com.acme.a","com.acme.a"]}`,
			`calls names "com.acme.a" twice`,
		},
		"calls too many": {
			`{"name":"s","kind":"module","license":"MIT","calls":["a.b0","a.b1","a.b2","a.b3","a.b4","a.b5","a.b6","a.b7",` +
				`"a.b8","a.b9","a.c0","a.c1","a.c2","a.c3","a.c4","a.c5","a.c6"]}`,
			"the limit is 16",
		},
		"events value empty": {
			`{"name":"s","kind":"module","license":"MIT","events":{"loaded":""}}`,
			`events["loaded"] is empty`,
		},
		"events not a table": {
			`{"name":"s","kind":"module","license":"MIT","events":["loaded"]}`,
			"events is a list",
		},
		"openapi absolute": {
			`{"name":"s","kind":"module","license":"MIT","openapi":"/openapi.json"}`,
			"starts with no slash",
		},
		"openapi climbs": {
			`{"name":"s","kind":"module","license":"MIT","openapi":"../openapi.json"}`,
			"climb out of it",
		},
		"platforms repeated": {
			`{"name":"s","kind":"module","license":"MIT","platforms":["linux_amd64","linux_amd64"]}`,
			"twice",
		},
		"build not a table": {`{"name":"s","kind":"module","license":"MIT","build":"make"}`, "build is text"},
		"unknown key":       {`{"name":"s","kind":"module","license":"MIT","permision":["a"]}`, "does not read"},
		"namespace declared": {
			`{"name":"s","kind":"module","license":"MIT","namespace":"com.acme"}`,
			"derives the namespace",
		},
		"a list not a mapping": {`[]`, "cannot unmarshal"},
		"an empty document":    {`null`, "the document is empty"},
		"not a document":       {`{`, "unexpected end"},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := manifest.Parse("module.yaml", []byte(testCase.document), decode)
			if err == nil {
				t.Fatalf("%s was accepted", name)
			}

			if !strings.Contains(err.Error(), testCase.refuses) {
				t.Errorf("the refusal is %q and does not name %q", err.Error(), testCase.refuses)
			}
		})
	}
}

func TestEveryRefusalIsErrInvalid(t *testing.T) {
	t.Parallel()

	_, err := manifest.Parse("module.yaml", []byte(`{"kind":"module"}`), decode)
	if !errors.Is(err, manifest.ErrInvalid) {
		t.Errorf("a refusal does not answer to ErrInvalid: %v", err)
	}
}

func TestARefusalNamesTheFile(t *testing.T) {
	t.Parallel()

	_, err := manifest.Parse("acme/module.yaml", []byte(`{"kind":"module"}`), decode)
	if err == nil || !strings.Contains(err.Error(), "acme/module.yaml") {
		t.Errorf("the refusal does not name the file: %v", err)
	}
}

func TestNoDecoderIsRefused(t *testing.T) {
	t.Parallel()

	_, err := manifest.Parse("module.yaml", []byte(full), nil)
	if err == nil || !strings.Contains(err.Error(), "no decoder") {
		t.Errorf("a parse with no decoder is %v", err)
	}
}

func TestFromTreeReadsADocumentAnotherReaderDecoded(t *testing.T) {
	t.Parallel()

	held, err := manifest.FromTree(map[string]any{"name": "spools", "kind": "module", "license": "MIT"})
	if err != nil {
		t.Fatalf("a tree was refused: %v", err)
	}

	if held.Name != "spools" {
		t.Errorf("the name read as %q", held.Name)
	}
}

// TestAManifestNamesTheLinkKind: a link runs in a process of its own, so a
// module declares itself under that kind the way it declares any other.
func TestAManifestNamesTheLinkKind(t *testing.T) {
	t.Parallel()

	document := `{"name":"fractal","kind":"link","license":"MIT","version":"1.0.0","contract_major":1}`

	held, err := manifest.Parse("module.yaml", []byte(document), decode)
	if err != nil {
		t.Fatalf("read a link module's manifest: %v", err)
	}

	if held.Kind != "link" {
		t.Errorf("the manifest reads kind %q", held.Kind)
	}

	if held.ContractMajor != 1 {
		t.Errorf("the manifest reads contract_major %d", held.ContractMajor)
	}
}
