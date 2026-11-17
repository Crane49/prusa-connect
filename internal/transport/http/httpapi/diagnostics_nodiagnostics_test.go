//go:build !diagnostics

package httpapi

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestABuildWithoutDiagnosticsServesNoBundleAndSaysSo is the minimal build: the
// route is not registered although exampleConfig wires a bundle, the document names it
// nowhere, and the page is told to hide the download.
func TestABuildWithoutDiagnosticsServesNoBundleAndSaysSo(t *testing.T) {
	t.Parallel()

	cfg := exampleConfig(&fakeFleet{}, seededStore(), seededPresets())

	server, err := New(cfg)
	require.NoError(t, err)

	rec := call(t, server.Handler(), http.MethodPost, "/api/v1/diagnostics", "[]")
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	require.False(t, uiSettings(t, server.Handler()).Diagnostics)
	require.NotContains(t, server.api.OpenAPI().Paths, "/api/v1/diagnostics")
}
