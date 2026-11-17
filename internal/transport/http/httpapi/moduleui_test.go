package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// bundleDigest is what the plane recorded the fixture bundle under; the bytes
// it covers are the plane's business, and here it is the tag every file
// carries.
const bundleDigest = "sha256:" + "ab" + "cdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789ab"

// aBundle is a built bundle as the plane hands it over: an entry file, a chunk
// in a subdirectory, and a directory with nothing servable at its own name.
func aBundle() ModuleUI {
	return ModuleUI{
		Files: fstest.MapFS{
			"index.js":        {Data: []byte("import './assets/chunk.js'\n")},
			"assets/chunk.js": {Data: []byte("export const a = 1\n")},
			"style.css":       {Data: []byte("body{}\n")},
			"logo.svg":        {Data: []byte("<svg/>")},
		},
		Digest: bundleDigest,
	}
}

// assetPlanner is one asset instance under the planner namespace, and a graph that
// grants who the given actions on it.
func assetPlanner(t *testing.T, who string, actions ...authz.Action) *Server {
	t.Helper()

	instance, err := ref.Parse("asset:planner")
	require.NoError(t, err)

	access := &fakeModuleAccess{who: "user:operator", instance: instance, actions: actions}
	plane := fakeModulePlane{namespace: moduleNamespace, instance: instance, handler: ModuleUIHandler(aBundle())}

	return moduleServer(t, plane, access, who)
}

// TestAModuleFileIsServedWithItsTypeAndCacheHeaders is ADR-20260911-4p6 rule 31 as a
// browser sees it: the file under /ui/, typed from its extension, kept by the
// browser but asked for again before every use, and tagged by the bundle's
// digest so a reinstall moves the tag.
func TestAModuleFileIsServedWithItsTypeAndCacheHeaders(t *testing.T) {
	t.Parallel()

	server := assetPlanner(t, "user:operator", authz.ActionViewResource)
	base := "/api/v1/modules/" + moduleNamespace + "/ui/"

	cases := []struct {
		file        string
		contentType string
		body        string
	}{
		{file: "index.js", contentType: "text/javascript; charset=utf-8", body: "import './assets/chunk.js'\n"},
		{file: "assets/chunk.js", contentType: "text/javascript; charset=utf-8", body: "export const a = 1\n"},
		{file: "style.css", contentType: "text/css; charset=utf-8", body: "body{}\n"},
		{file: "logo.svg", contentType: "image/svg+xml", body: "<svg/>"},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()

			served := ask(t, server, http.MethodGet, base+tc.file, "")
			require.Equal(t, http.StatusOK, served.Code, served.Body.String())
			require.Equal(t, tc.body, served.Body.String())
			require.Equal(t, tc.contentType, served.Header().Get("Content-Type"))
			require.Equal(t, "nosniff", served.Header().Get("X-Content-Type-Options"))
			require.Equal(t, "private, no-cache", served.Header().Get("Cache-Control"))
			require.Equal(t, `"`+strings.TrimPrefix(bundleDigest, "sha256:")+`"`, served.Header().Get("ETag"))
		})
	}

	t.Run("a client holding the tag is answered 304 with no body", func(t *testing.T) {
		t.Parallel()

		request := ask(t, server, http.MethodGet, base+"index.js", "")
		tag := request.Header().Get("ETag")

		recorder := askWithHeader(t, server, base+"index.js", "If-None-Match", tag)
		require.Equal(t, http.StatusNotModified, recorder.Code)
		require.Empty(t, recorder.Body.String())
		require.Equal(t, "private, no-cache", recorder.Header().Get("Cache-Control"))
	})
}

// TestAModuleFileOutsideTheBundleIsRefused covers every name that must not
// reach the bundle: a dot segment, an absolute path, a backslash, a directory,
// a file that is not there, and a path under the mount that is not /ui/ at
// all. Each answers as an unrouted path, so a caller learns nothing about the
// tree from the difference.
func TestAModuleFileOutsideTheBundleIsRefused(t *testing.T) {
	t.Parallel()

	server := assetPlanner(t, "user:operator", authz.ActionViewResource)
	mount := "/api/v1/modules/" + moduleNamespace

	cases := []struct {
		name   string
		target string
	}{
		{name: "a dot segment climbing out", target: mount + "/ui/../../../etc/passwd"},
		{name: "a dot segment inside", target: mount + "/ui/assets/../index.js"},
		{name: "an absolute path", target: mount + "/ui//etc/passwd"},
		{name: "a backslash", target: mount + `/ui/assets\chunk.js`},
		{name: "a directory", target: mount + "/ui/assets"},
		{name: "the bundle root", target: mount + "/ui/"},
		{name: "a missing file", target: mount + "/ui/missing.js"},
		{name: "a path under the mount that is not a file", target: mount + "/spools"},
		{name: "the mount itself", target: mount + "/"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			refused := ask(t, server, http.MethodGet, tc.target, "")
			require.Equal(t, http.StatusNotFound, refused.Code, refused.Body.String())
		})
	}

	t.Run("a method that is not a read", func(t *testing.T) {
		t.Parallel()

		// The operator holds operate_resource too, so this reaches the
		// handler and is refused there: a file is read and never written.
		writer := assetPlanner(t, "user:operator", authz.ActionViewResource, authz.ActionOperateResource)

		refused := ask(t, writer, http.MethodPost, mount+"/ui/index.js", "x")
		require.Equal(t, http.StatusMethodNotAllowed, refused.Code, refused.Body.String())
	})
}

// TestAModuleFileIsRefusedWithoutTheGrantOrTheInstance is the authorization
// half, in both directions. A caller who holds nothing on the instance is
// refused before the file is looked at, and a namespace no enabled instance
// claims, which is what a disabled asset is to the plane, answers as unrouted.
func TestAModuleFileIsRefusedWithoutTheGrantOrTheInstance(t *testing.T) {
	t.Parallel()

	target := "/api/v1/modules/" + moduleNamespace + "/ui/index.js"

	t.Run("a caller who holds nothing", func(t *testing.T) {
		t.Parallel()

		server := assetPlanner(t, "user:stranger", authz.ActionViewResource)

		refused := ask(t, server, http.MethodGet, target, "")
		require.Equal(t, http.StatusForbidden, refused.Code, refused.Body.String())
		require.Contains(t, refused.Body.String(), "ACCESS_DENIED")
	})

	t.Run("the grant is view_resource and nothing less", func(t *testing.T) {
		t.Parallel()

		server := assetPlanner(t, "user:operator")

		refused := ask(t, server, http.MethodGet, target, "")
		require.Equal(t, http.StatusForbidden, refused.Code, refused.Body.String())
	})

	// A browser shared by two people holds the file the first one fetched and
	// sends its tag back. The second person has no grant, and the tag must
	// not turn the refusal into a 304.
	t.Run("a caller who holds nothing and sends the tag is refused", func(t *testing.T) {
		t.Parallel()

		granted := assetPlanner(t, "user:operator", authz.ActionViewResource)
		tag := ask(t, granted, http.MethodGet, target, "").Header().Get("ETag")
		require.NotEmpty(t, tag)

		stranger := assetPlanner(t, "user:stranger", authz.ActionViewResource)
		refused := askWithHeader(t, stranger, target, "If-None-Match", tag)
		require.Equal(t, http.StatusForbidden, refused.Code, refused.Body.String())
	})

	t.Run("a disabled instance claims no namespace", func(t *testing.T) {
		t.Parallel()

		instance, err := ref.Parse("asset:planner")
		require.NoError(t, err)

		access := &fakeModuleAccess{
			who: "user:operator", instance: instance, actions: []authz.Action{authz.ActionViewResource},
		}
		server := moduleServer(t, fakeModulePlane{}, access, "user:operator")

		refused := ask(t, server, http.MethodGet, target, "")
		require.Equal(t, http.StatusNotFound, refused.Code, refused.Body.String())
	})
}

// TestTheModuleFileAddressIsTheListingsAddress holds the helper the listing
// renders to the route that serves it.
func TestTheModuleFileAddressIsTheListingsAddress(t *testing.T) {
	t.Parallel()

	require.Equal(t, "/api/v1/modules/"+moduleNamespace+"/ui/", ModuleUIPath(moduleNamespace))
}

// TestTheModuleFileFamilyIsDescribedAndNotCallable is the shape the document
// gives the family: the address and its two parameters are published, and no
// method is. An operation here would reach a generated client as a method
// taking a catch-all path, which is a method nothing can call.
func TestTheModuleFileFamilyIsDescribedAndNotCallable(t *testing.T) {
	t.Parallel()

	item, published := newTestServer(t).api.OpenAPI().Paths[moduleUIFamilyPath]
	require.True(t, published, "the file family is absent from the document")
	require.NotEmpty(t, item.Description)

	for method, op := range operationsOfPath(item) {
		require.Nilf(t, op, "%s on the file family is an operation a client would generate a method for", method)
	}

	named := make([]string, 0, len(item.Parameters))
	for _, param := range item.Parameters {
		require.Equal(t, "path", param.In)
		require.True(t, param.Required)
		require.NotEmpty(t, param.Description)
		require.NotNil(t, param.Example, "a renderer has nothing to fill %s with", param.Name)

		named = append(named, param.Name)
	}

	require.ElementsMatch(t, []string{namespaceParam, "file"}, named)
}

// askWithHeader is ask with one request header set, for the conditional GET.
func askWithHeader(t *testing.T, server *Server, target, header, value string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.Header.Set(header, value)

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	return recorder
}

// TestTheListingCarriesTheUIObjectOfADrawingModuleOnly is ADR-20260911-4p6 rule 32
// over the real router. The object rides on the instance beside openapi_url,
// and a client reads the entry address, the library range and the
// contributions out of the one call the sidebar is drawn from.
func TestTheListingCarriesTheUIObjectOfADrawingModuleOnly(t *testing.T) {
	t.Parallel()

	store := seededStore()

	instance, err := ref.Parse(moduleInstance)
	require.NoError(t, err)

	drawing := exampleAssetResource()
	drawing.Version = store.version

	asset, err := ref.Parse(drawing.Kind + ":" + drawing.Name)
	require.NoError(t, err)

	store.instances[asset.Path()] = drawing

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
			Kind string      `json:"kind"`
			Name string      `json:"name"`
			UI   *ResourceUI `json:"ui"`
		} `json:"items"`
	}

	require.NoError(t, json.Unmarshal(answered.Body.Bytes(), &listing))

	found := map[string]bool{}

	for _, item := range listing.Items {
		switch item.Kind + ":" + item.Name {
		case drawing.Kind + ":" + drawing.Name:
			found["asset"] = true

			require.NotNil(t, item.UI, "a ready asset module with a ui block carries none")
			require.Equal(t, ModuleUIPath(exampleNamespace)+"ui/index.js", item.UI.EntryURL)
			require.Equal(t, ">=0.3.0 <1.0.0", item.UI.Library)
			require.Equal(t, "spools", item.UI.Contributes.Pages[0].Path)
			require.Equal(t, "spool-status", item.UI.Contributes.DeviceCard[0].ID)
			require.Equal(t, "spools", item.UI.Contributes.Settings[0].ID)
		case moduleInstance:
			found["service"] = true

			require.Nil(t, item.UI, "a module that runs a process and draws nothing carries a ui object")
		case "storage:local":
			found["builtin"] = true

			require.Nil(t, item.UI, "a builtin instance carries a ui object")
		}
	}

	require.True(t, found["asset"] && found["service"] && found["builtin"],
		"the listing lost an instance: %s", answered.Body.String())
}
