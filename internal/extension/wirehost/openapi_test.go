package wirehost_test

import (
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/extension/wirehost"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
)

// TestAModuleFragmentCrossesTheWireOnPluginInfo launches the example module and
// reads back the fragment it ships. The document comes in the plugin.info
// answer, the host checks it, and what the host keeps is the parsed fragment
// with the module's own relative paths.
func TestAModuleFragmentCrossesTheWireOnPluginInfo(t *testing.T) {
	t.Parallel()

	child, err := wirehost.Launch(t.Context(), wirehost.Options{
		Instance: "sink:example",
		Path:     builtModule.file(t),
		Logger:   slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("launch the example module: %v", err)
	}

	t.Cleanup(func() { _ = child.Close() })

	if len(child.Info().OpenAPI) == 0 {
		t.Fatal("the example module's plugin.info carries no openapi member")
	}

	fragment := child.OpenAPI()
	if fragment == nil {
		t.Fatal("the host kept no fragment off a child that shipped one")
	}

	if paths := fragment.Paths(); !slices.Contains(paths, "/spools") {
		t.Errorf("the fragment lists %v, and the module serves /spools", paths)
	}

	if !child.Info().Answers(routesapi.MethodServe) || !child.ServesRoutes() {
		t.Error("routesapi.Describe named no routes.serve, and the host mounts nothing for a module that names none")
	}
}

// TestAChildWithNoRoutesNeedsNoFragment covers the opposite case. A child that
// names no routes ships no fragment, and the host keeps nothing and mounts
// nothing.
func TestAChildWithNoRoutesNeedsNoFragment(t *testing.T) {
	t.Parallel()

	child, _ := launch(t, wirehost.Options{})

	if len(child.Info().OpenAPI) != 0 || child.OpenAPI() != nil {
		t.Error("blobchild ships no fragment, and the host kept one")
	}

	if child.ServesRoutes() {
		t.Error("blobchild names no routes, and the host reads it as serving some")
	}
}

// TestAModuleWithRoutesAndNoFragmentIsRefused checks across a process boundary
// that the host mounts nothing it has no description for. A child that names
// routes.serve and describes none of its routes is refused at launch, and the
// reason names the module, says a fragment is required, and names the call that
// attaches one. With the fragment, the same child launches and is mounted.
func TestAModuleWithRoutesAndNoFragmentIsRefused(t *testing.T) {
	t.Parallel()

	t.Run("no fragment", func(t *testing.T) {
		t.Parallel()

		child, err := wirehost.Launch(t.Context(), wirehost.Options{
			Instance: "blob-example",
			Path:     builtChild.file(t),
			Args:     []string{"-routes"},
		})
		if err == nil {
			_ = child.Close()

			t.Fatal("a module that names routes.serve and ships no fragment was launched")
		}

		if !errors.Is(err, wirehost.ErrRoutesUndescribed) {
			t.Fatalf("the refusal is %v, and a module with undescribed routes is %v",
				err, wirehost.ErrRoutesUndescribed)
		}

		for _, said := range []string{
			"example-blob", "a fragment is required for served routes", "routesapi.Describe",
		} {
			if !strings.Contains(err.Error(), said) {
				t.Errorf("the refusal reads %q, and an author is told %q", err.Error(), said)
			}
		}
	})

	t.Run("with a fragment", func(t *testing.T) {
		t.Parallel()

		child, _ := launch(t, wirehost.Options{Args: []string{
			"-routes", "-openapi",
			`{"openapi":"3.1.0","paths":{"/spools":{"get":{"operationId":"a","summary":"s","description":"d"}}}}`,
		}})

		if child.OpenAPI() == nil || !child.ServesRoutes() {
			t.Error("a module with routes and a fragment is what the host mounts, and it kept neither")
		}
	})
}

// TestAChildWhoseFragmentBreaksTheRuleIsRefused covers a fragment that is
// present and wrong. The launch is refused, and the reason says what an author
// has to fix.
func TestAChildWhoseFragmentBreaksTheRuleIsRefused(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		fragment string
		says     string
	}{
		"junk": {fragment: `"not a document"`, says: "does not parse"},
		"an absolute path": {
			fragment: `{"openapi":"3.1.0","paths":{"/api/v1/modules/x/spools":{"get":` +
				`{"operationId":"a","summary":"s","description":"d"}}}}`,
			says: "not relative",
		},
		"an operation with no description": {
			fragment: `{"openapi":"3.1.0","paths":{"/spools":{"get":{"operationId":"a","summary":"s"}}}}`,
			says:     "GET /spools carries no description",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			child, err := wirehost.Launch(t.Context(), wirehost.Options{
				Instance: "blob-example",
				Path:     builtChild.file(t),
				Args:     []string{"-openapi", tc.fragment},
			})
			if err == nil {
				_ = child.Close()

				t.Fatal("a child whose fragment breaks the rule was launched")
			}

			if !errors.Is(err, routesapi.ErrFragment) {
				t.Fatalf("the refusal is %v, and a fragment the host cannot publish is %v",
					err, routesapi.ErrFragment)
			}

			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("the refusal reads %q, and an author is told %q", err.Error(), tc.says)
			}
		})
	}
}

// TestAModuleFragmentRebasesUnderARootPrefix covers the host side of the path
// rule. The fragment a child ships is relative to wherever the operator mounts
// it, so it rebases under a root prefix such as /graphql the same way it does
// under the namespace group, and the child never learns which it got.
func TestAModuleFragmentRebasesUnderARootPrefix(t *testing.T) {
	t.Parallel()

	child, err := wirehost.Launch(t.Context(), wirehost.Options{
		Instance: "sink:example",
		Path:     builtModule.file(t),
		Logger:   slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("launch the example module: %v", err)
	}

	t.Cleanup(func() { _ = child.Close() })

	fragment := child.OpenAPI()
	if fragment == nil {
		t.Fatal("the host kept no fragment off a child that shipped one")
	}

	for _, mount := range []string{"/graphql", "/api/v1/modules/com.github.aux.acme/"} {
		rebased, rebaseErr := fragment.Rebase(mount)
		if rebaseErr != nil {
			t.Fatalf("rebase under %s: %v", mount, rebaseErr)
		}

		var document struct {
			Paths map[string]json.RawMessage `json:"paths"`
		}

		if err = json.Unmarshal(rebased, &document); err != nil {
			t.Fatalf("the document rebased under %s does not parse: %v", mount, err)
		}

		want := strings.TrimSuffix(mount, "/") + "/spools"
		if _, ok := document.Paths[want]; !ok {
			t.Errorf("rebased under %s, the document names %v and not %s",
				mount, slices.Sorted(maps.Keys(document.Paths)), want)
		}

		if _, still := document.Paths["/spools"]; still {
			t.Errorf("rebased under %s, the relative path is still in the document", mount)
		}
	}

	if paths := fragment.Paths(); !slices.Contains(paths, "/spools") {
		t.Errorf("the fragment the host kept was rebased in place: %v", paths)
	}
}
