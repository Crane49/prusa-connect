//go:build nodashboards

package httpapi

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestABuildWithoutDashboardsServesNoneAndSaysSo is the printer's build: no
// dashboard operation is registered, the document names none, and the page is
// told the home page is the printer list.
func TestABuildWithoutDashboardsServesNoneAndSaysSo(t *testing.T) {
	t.Parallel()

	cfg := exampleConfig(&gcodeFleetStub{printer: devPrinter()}, seededStore(), seededPresets())
	cfg.Principals = fixedPrincipal("user:operator")

	server, err := New(cfg)
	require.NoError(t, err)

	for _, path := range []string{"/api/v1/dashboards", "/api/v1/dashboards/3"} {
		rec := call(t, server.Handler(), http.MethodGet, path, "")
		require.Equalf(t, http.StatusNotFound, rec.Code, "%s: %s", path, rec.Body.String())
	}

	require.False(t, uiSettings(t, server.Handler()).Dashboards)

	for path := range server.api.OpenAPI().Paths {
		require.NotContains(t, path, "/api/v1/dashboards")
	}

	for _, tag := range server.api.OpenAPI().Tags {
		require.NotEqual(t, dashboardsTag, tag.Name)
	}
}
