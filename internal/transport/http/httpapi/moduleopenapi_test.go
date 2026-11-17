package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
)

// farmFragment is the document the module shipped, relative to its mount, as
// the plane rebases it before the mount ever sees it.
const farmFragment = `{
  "openapi": "3.1.0",
  "info": {"title": "planner", "version": "1.0.0"},
  "paths": {"/spools": {"get": {
    "operationId": "list-spools",
    "summary": "The spools on the shelf",
    "description": "Every spool the module knows about.",
    "responses": {"200": {"description": "The listing."}}
  }}}
}`

// theDescribedFarm is theFarm with a fragment behind the plane, rebased the way
// the supervision plane rebases it.
func theDescribedFarm(t *testing.T, who string) (*Server, *fakeModuleAccess) {
	t.Helper()

	instance, err := ref.Parse(moduleInstance)
	require.NoError(t, err)

	fragment, err := routesapi.ParseFragment([]byte(farmFragment))
	require.NoError(t, err)

	rebased, err := fragment.Rebase(ModuleMount(moduleNamespace))
	require.NoError(t, err)

	access := &fakeModuleAccess{
		who:      "user:operator",
		instance: instance,
		actions:  []authz.Action{authz.ActionViewResource},
	}

	plane := fakeModulePlane{
		namespace: moduleNamespace, instance: instance, handler: moduleEcho(), openapi: rebased,
	}

	return moduleServer(t, plane, access, who), access
}

// TestAModuleDocumentIsServedRebasedUnderItsMount is ADR-20260909-c7x rule 9 on the
// wire: the address is the server's, the paths are under the mount, and
// reading it took view_resource on the module's instance.
func TestAModuleDocumentIsServedRebasedUnderItsMount(t *testing.T) {
	t.Parallel()

	server, access := theDescribedFarm(t, "user:operator")

	answered := ask(t, server, http.MethodGet, ModuleOpenAPIPath(moduleNamespace), "")
	require.Equal(t, http.StatusOK, answered.Code, answered.Body.String())
	require.Contains(t, answered.Header().Get("Content-Type"), "application/json")

	var document struct {
		OpenAPI string                     `json:"openapi"`
		Paths   map[string]json.RawMessage `json:"paths"`
	}

	require.NoError(t, json.Unmarshal(answered.Body.Bytes(), &document))
	require.Equal(t, "3.1.0", document.OpenAPI)
	require.Contains(t, document.Paths, "/api/v1/modules/"+moduleNamespace+"/spools",
		"the served document does not carry the module's route under its mount")
	require.NotContains(t, document.Paths, "/spools", "the relative path leaked into the served document")

	require.Equal(t, []authz.Action{authz.ActionViewResource}, access.asked,
		"reading the document did not ask the graph for view_resource on the module's instance")
}

// TestAModuleDocumentIsRefusedForACallerWhoHoldsNothing is the refusal beside
// the permit, on the same document.
func TestAModuleDocumentIsRefusedForACallerWhoHoldsNothing(t *testing.T) {
	t.Parallel()

	server, _ := theDescribedFarm(t, "user:stranger")

	answered := ask(t, server, http.MethodGet, ModuleOpenAPIPath(moduleNamespace), "")
	require.Equal(t, http.StatusForbidden, answered.Code, answered.Body.String())
}

// TestAModuleWithNoDocumentAnswersNotFound holds the two absences to one
// answer: a namespace nothing claims, and a module that shipped no fragment,
// both read as an address that is not there.
func TestAModuleWithNoDocumentAnswersNotFound(t *testing.T) {
	t.Parallel()

	server, _ := theFarm(t)

	for name, target := range map[string]string{
		"a module that ships no fragment": ModuleOpenAPIPath(moduleNamespace),
		"a namespace nothing claims":      ModuleOpenAPIPath("com.github.aux.nobody.here"),
		"a namespace outside the grammar": ModuleOpenAPIPath("Not_A_Namespace"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			answered := ask(t, server, http.MethodGet, target, "")
			require.Equal(t, http.StatusNotFound, answered.Code, answered.Body.String())
		})
	}
}

// TestTheListingSaysWhichInstanceIsAModuleAndWhereItsDocumentIs is what lets a
// client discover enabled modules and their fragments in one call: runtime,
// namespace and the document's address ride on the instance.
func TestTheListingSaysWhichInstanceIsAModuleAndWhereItsDocumentIs(t *testing.T) {
	t.Parallel()

	store := seededStore()

	instance, err := ref.Parse(moduleInstance)
	require.NoError(t, err)

	store.instances[instance.Path()] = ResourceInstance{
		ResourceSpec: ResourceSpec{Type: "acme/planner", Enabled: true},
		Kind:         instance.Kind().String(),
		Name:         instance.ID(),
		UID:          "01J0SINKFARM",
		Runtime:      "service",
		Namespace:    moduleNamespace,
		OpenAPIURL:   ModuleOpenAPIPath(moduleNamespace),
		Source:       "file",
		Version:      store.version,
		Generation:   1,
		Status:       ResourceStatus{State: "ready", Serving: true, Conditions: []ResourceCondition{}},
	}

	server := serverWith(t, store)

	answered := call(t, server.Handler(), http.MethodGet, resourcesPath, "")
	require.Equal(t, http.StatusOK, answered.Code, answered.Body.String())

	var listing struct {
		Items []struct {
			Kind       string `json:"kind"`
			Name       string `json:"name"`
			Runtime    string `json:"runtime"`
			Namespace  string `json:"namespace"`
			OpenAPIURL string `json:"openapi_url"`
		} `json:"items"`
	}

	require.NoError(t, json.Unmarshal(answered.Body.Bytes(), &listing))

	found := map[string]bool{}

	for _, item := range listing.Items {
		switch item.Kind + ":" + item.Name {
		case moduleInstance:
			found["module"] = true

			require.Equal(t, "service", item.Runtime)
			require.Equal(t, moduleNamespace, item.Namespace)
			require.Equal(t, "/api/v1/modules/"+moduleNamespace+"/openapi.json", item.OpenAPIURL)
		case "storage:local":
			found["builtin"] = true

			require.Empty(t, item.Namespace, "a builtin instance carries a namespace")
			require.Empty(t, item.OpenAPIURL, "a builtin instance carries a document address")
		}
	}

	require.True(t, found["module"] && found["builtin"], "the listing lost an instance: %s", answered.Body.String())
}
