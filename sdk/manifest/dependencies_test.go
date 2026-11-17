package manifest_test

import (
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// The dependency keys of ADR-20261027-e6k, which are Composer's: require,
// suggest and conflict.

func TestAManifestDeclaresItsModules(t *testing.T) {
	t.Parallel()

	document := `{
	  "name": "planner", "kind": "sink","license":"MIT",
	  "require": {
	    "core": ">=0.9",
	    "core-state.get": "*",
	    "com.github.aux.acme.threemf": "^1.2@beta"
	  },
	  "suggest": {"com.github.aux.acme.parts": "Shows the parts of a plan"},
	  "conflict": {"com.github.aux.acme.old": "<2.0"},
	  "calls": ["com.github.aux.acme.threemf", "com.github.aux.acme.parts"]
	}`

	held, err := manifest.Parse("module.yaml", []byte(document), decode)
	if err != nil {
		t.Fatalf("a manifest with dependencies was refused: %v", err)
	}

	want := manifest.Dependency{Module: "com.github.aux.acme.threemf", Version: "^1.2@beta"}
	if dependencies := held.Dependencies(); len(dependencies) != 1 || dependencies[0] != want {
		t.Errorf("require read as %+v", held.Requires)
	}

	if held.Core != ">=0.9" {
		t.Errorf("the core read as %q", held.Core)
	}

	if len(held.Suggests) != 1 || held.Suggests[0].Reason != "Shows the parts of a plan" {
		t.Errorf("suggest read as %+v", held.Suggests)
	}

	if len(held.Conflicts) != 1 || held.Conflicts[0].Version != "<2.0" {
		t.Errorf("conflict read as %+v", held.Conflicts)
	}
}

func TestADependencyIsHeldToItsShape(t *testing.T) {
	t.Parallel()

	cases := map[string]struct{ document, refuses string }{
		"a constraint nobody reads": {
			`{"name":"s","kind":"module","license":"MIT","require":{"a.b":"one"}}`, "require.a.b",
		},
		"a module of one label": {
			`{"name":"s","kind":"module","license":"MIT","require":{"acme":"^1"}}`, "not the namespace of a module",
		},
		"required and in conflict": {
			`{"name":"s","kind":"module","license":"MIT","require":{"a.b":"^1"},"conflict":{"a.b":"<1.2"}}`,
			"under both require and conflict",
		},
		"a conflict that is no constraint": {
			`{"name":"s","kind":"module","license":"MIT","conflict":{"a.b":"soon"}}`, "conflict.a.b",
		},
		"a call to a module not named": {
			`{"name":"s","kind":"module","license":"MIT","calls":["a.b"]}`, "neither require nor suggest names",
		},
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
