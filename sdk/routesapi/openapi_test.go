package routesapi_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// spools is the smallest fragment that passes: one path, one operation, the
// three members the rule asks for.
const spools = `{
  "openapi": "3.1.0",
  "info": {"title": "spools", "version": "1.0.0"},
  "paths": {
    "/spools": {
      "get": {
        "operationId": "list-spools",
        "summary": "The spools on the shelf",
        "description": "Every spool the module knows about.",
        "responses": {"200": {"description": "The listing."}}
      }
    }
  }
}`

func TestAFragmentIsRebasedUnderTheMount(t *testing.T) {
	t.Parallel()

	fragment, err := routesapi.ParseFragment([]byte(spools))
	if err != nil {
		t.Fatalf("parse the fragment: %v", err)
	}

	if got := fragment.Paths(); len(got) != 1 || got[0] != "/spools" {
		t.Fatalf("the fragment lists %v, and it describes /spools", got)
	}

	rebased, err := fragment.Rebase("/api/v1/modules/com.github.aux.acme.planner/")
	if err != nil {
		t.Fatalf("rebase the fragment: %v", err)
	}

	var document struct {
		OpenAPI string                     `json:"openapi"`
		Info    map[string]string          `json:"info"`
		Paths   map[string]json.RawMessage `json:"paths"`
	}

	if err = json.Unmarshal(rebased, &document); err != nil {
		t.Fatalf("the rebased document does not parse: %v", err)
	}

	if _, ok := document.Paths["/api/v1/modules/com.github.aux.acme.planner/spools"]; !ok {
		t.Errorf("the rebased document names %v, and the path belongs under the mount", document.Paths)
	}

	if _, still := document.Paths["/spools"]; still {
		t.Error("the relative path is still in the rebased document")
	}

	if document.OpenAPI != "3.1.0" || document.Info["title"] != "spools" {
		t.Errorf("a member other than paths changed on the way through: %s", rebased)
	}
}

func TestDescribeAttachesTheFragmentToTheInfo(t *testing.T) {
	t.Parallel()

	info, err := routesapi.Describe(wirerpc.Info{
		Name: "spools", Version: "1.0.0", Contract: wirerpc.Contract,
	}, []byte(spools))
	if err != nil {
		t.Fatalf("describe: %v", err)
	}

	if _, err = routesapi.ParseFragment(info.OpenAPI); err != nil {
		t.Fatalf("what Describe attached does not parse back: %v", err)
	}

	if info.Name != "spools" {
		t.Errorf("the info came back as %q", info.Name)
	}

	if !info.Answers(routesapi.MethodServe) {
		t.Errorf("the info names %v, and a described module serves %s", info.Serves, routesapi.MethodServe)
	}

	again, err := routesapi.Describe(info, []byte(spools))
	if err != nil {
		t.Fatalf("describe twice: %v", err)
	}

	if len(again.Serves) != 1 {
		t.Errorf("describing twice named %v, and a method is named once", again.Serves)
	}

	_, err = routesapi.Describe(wirerpc.Info{}, []byte(`{"openapi":"3.1.0"}`))
	if !errors.Is(err, routesapi.ErrFragment) {
		t.Errorf("a fragment the host refuses was attached: %v", err)
	}
}

// TestAFragmentTheHostCannotPublishIsRefused is the rule, one case per clause,
// each with the words an author reads.
func TestAFragmentTheHostCannotPublishIsRefused(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		document string
		says     string
	}{
		"not JSON": {document: `{`, says: "does not parse"},
		"a JSON array": {
			document: `[]`, says: "does not parse",
		},
		"another OpenAPI line": {
			document: strings.Replace(spools, `"3.1.0"`, `"3.0.3"`, 1), says: "declares 3.1",
		},
		"no paths": {
			document: `{"openapi":"3.1.0","paths":{}}`, says: "at least one path",
		},
		"a path that is not relative": {
			document: strings.Replace(spools, `"/spools"`, `"spools"`, 1), says: "not relative",
		},
		"a path under the core's own prefix": {
			document: strings.Replace(spools, `"/spools"`, `"/api/v1/modules/x/spools"`, 1),
			says:     "not relative",
		},
		"a dot segment": {
			document: strings.Replace(spools, `"/spools"`, `"/../spools"`, 1), says: "dot segment",
		},
		"no operation on the path": {
			document: `{"openapi":"3.1.0","paths":{"/spools":{"summary":"nothing"}}}`,
			says:     "declares no operation",
		},
		"no operationId": {
			document: strings.Replace(spools, `"operationId": "list-spools",`, ``, 1),
			says:     "GET /spools carries no operationId",
		},
		"no summary": {
			document: strings.Replace(spools, `"summary": "The spools on the shelf",`, ``, 1),
			says:     "GET /spools carries no summary",
		},
		"no description": {
			document: strings.Replace(spools, `"description": "Every spool the module knows about.",`, ``, 1),
			says:     "GET /spools carries no description",
		},
		"two operations with one id": {
			document: strings.Replace(spools, `"get": {`, `"post": {
        "operationId": "list-spools",
        "summary": "s",
        "description": "d"
      },
      "get": {`, 1),
			says: "share the operationId",
		},
		"over the bound": {
			document: `{"openapi":"3.1.0","paths":{},"x":"` +
				strings.Repeat("a", routesapi.MaxFragmentBytes) + `"}`,
			says: "the limit is",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := routesapi.ParseFragment([]byte(tc.document))
			if !errors.Is(err, routesapi.ErrFragment) {
				t.Fatalf("the fragment was accepted, or refused with something else: %v", err)
			}

			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("the refusal reads %q, and an author is told %q", err.Error(), tc.says)
			}
		})
	}
}

func TestTheZeroFragmentRebasesToNothing(t *testing.T) {
	t.Parallel()

	if _, err := (routesapi.Fragment{}).Rebase("/api/v1/modules/x"); !errors.Is(err, routesapi.ErrFragment) {
		t.Errorf("the zero Fragment rebased to a document: %v", err)
	}
}
