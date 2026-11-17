//go:build nodevtools

package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

// The printer build serves no developer route. The router holds nothing under
// /api/v1/dev, and /dev answers 404 even with the web app mounted at the
// origin root.
func TestThePrinterBuildServesNoDeveloperRoute(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	routes := 0
	walk := func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes++

		if strings.HasPrefix(route, "/api/v1/dev/") {
			t.Errorf("the printer build serves %s %s", method, route)
		}

		return nil
	}

	require.NoError(t, chi.Walk(server.mux, walk))
	require.Greater(t, routes, 100, "the router walk read too few routes to prove anything")

	handler := appFixture(t)

	for _, path := range []string{devPagePath, devConnectorsPath, "/api/v1/dev/devices/p1/commands"} {
		rec := call(t, handler, http.MethodGet, path, "")
		require.Equalf(t, http.StatusNotFound, rec.Code, "GET %s", path)
		require.Containsf(t, rec.Header().Get("Content-Type"), "problem+json", "GET %s", path)
	}
}

// httpMethods are the keys of an OpenAPI path item that name an operation.
var httpMethods = []string{
	http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
	http.MethodDelete, http.MethodHead, http.MethodOptions, http.MethodTrace,
}

// The printer build publishes the committed API document without the
// developer operations, and with every other operation in it. So `make
// api-check` reads the normal build only, and this test holds the printer
// build to the same document. The printer's configuration also leaves the
// dashboards out, so a build carrying the nodashboards tag drops their six
// operations as well.
func TestThePrinterBuildPublishesTheNormalDocumentWithoutTheDeveloperOperations(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile(committedSpecPath)
	require.NoError(t, err)

	var document struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(body, &document))

	var (
		want    []string
		dropped int
		boards  int
	)

	for path, item := range document.Paths {
		for method, raw := range item {
			if !slices.Contains(httpMethods, strings.ToUpper(method)) {
				continue
			}

			var op struct {
				Tags []string `json:"tags"`
			}
			require.NoError(t, json.Unmarshal(raw, &op))

			if slices.Contains(op.Tags, "developer") {
				dropped++

				continue
			}

			if !dashboardsCarried && slices.Contains(op.Tags, dashboardsTag) {
				boards++

				continue
			}

			want = append(want, strings.ToUpper(method)+" "+path)
		}
	}

	sort.Strings(want)

	require.Equal(t, 3, dropped, "the committed document should carry the three developer operations")

	if !dashboardsCarried {
		require.Equal(t, 6, boards, "the committed document should carry the six dashboard operations")
	}

	require.Equal(t, want, publishedOperations(newTestServer(t).api))
}
