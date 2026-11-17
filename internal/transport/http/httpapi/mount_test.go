package httpapi

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// mountedText is a handler that says which mount answered, so a test can tell
// the router matched the pattern it meant rather than something else.
func mountedText(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(body))
	})
}

// TestAMountAnswersItsOwnPathsAndNeverThisServers is the property the mount
// exists for and the one that makes it safe: a frozen device wire and a
// single-page frontend answer at the origin root, and the API's own paths are
// still refused in problem JSON even though the frontend's pattern matches
// everything.
//
// Without the guard, chi falls back to /* for /api/v1/nothing-here and the
// frontend answers a request for an operation with a page saying the route is
// fine.
func TestAMountAnswersItsOwnPathsAndNeverThisServers(t *testing.T) {
	t.Parallel()

	handler := serverWithMounts(t,
		Mount{Pattern: "/p/ws", Handler: mountedText("connector"), Reason: "a frozen device wire"},
		Mount{Pattern: "/*", Handler: mountedText("frontend"), Reason: "the frontend at the root"},
	).Handler()

	for path, want := range map[string]string{
		"/p/ws":       "connector",
		"/":           "frontend",
		"/printers":   "frontend",
		"/index.html": "frontend",
	} {
		rec := call(t, handler, http.MethodGet, path, "")

		require.Equalf(t, http.StatusOK, rec.Code, "%s reached no mount", path)
		require.Equalf(t, want, rec.Body.String(), "%s reached the wrong mount", path)
	}

	for _, path := range []string{"/api", "/api/v1", "/api/v1/nothing-here", specJSONPath + "/extra"} {
		rec := call(t, handler, http.MethodGet, path, "")

		require.Equalf(t, http.StatusNotFound, rec.Code, "%s was answered by a mount", path)
		require.Equalf(t, problemMediaType, rec.Header().Get("Content-Type"),
			"%s answered with content rather than a refusal", path)
	}
}

// TestAnOperationStillWinsOverAMountThatCatchesEverything is the other half:
// mounting /* must not take a published path away from the router.
func TestAnOperationStillWinsOverAMountThatCatchesEverything(t *testing.T) {
	t.Parallel()

	handler := serverWithMounts(t,
		Mount{Pattern: "/*", Handler: mountedText("frontend"), Reason: "the frontend at the root"},
	).Handler()

	rec := call(t, handler, http.MethodGet, healthPath, "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"status":"ok"`)
}

// TestTheExpositionOnItsOwnListenerLeavesNoPageAtMetrics holds the API
// listener to 404 at /metrics once core.observability.addr moves the
// exposition away. The web app's fallback would otherwise answer it with its
// entry page, and a scraper would read a page as a healthy target.
func TestTheExpositionOnItsOwnListenerLeavesNoPageAtMetrics(t *testing.T) {
	t.Parallel()

	frontend := Mount{Pattern: "/*", Handler: mountedText("frontend"), Reason: "the frontend at the root"}
	config := mountConfig([]Mount{frontend})
	config.MetricsListener = true

	server, err := New(config)
	require.NoError(t, err)

	rec := call(t, server.Handler(), http.MethodGet, metricsPath, "")

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, problemMediaType, rec.Header().Get("Content-Type"))
}

// TestAMountThisServerWillNotServeIsRefusedAtConstruction watches the gate
// refuse. Each case is a way the exception could have widened without anybody
// reading a diff.
func TestAMountThisServerWillNotServeIsRefusedAtConstruction(t *testing.T) {
	t.Parallel()

	cases := map[string][]Mount{
		"a mount with no reason": {
			{Pattern: "/p/ws", Handler: mountedText("connector")},
		},
		"a mount with no handler": {
			{Pattern: "/p/ws", Reason: "a frozen device wire"},
		},
		"a mount that is not a path": {
			{Pattern: "p/ws", Handler: mountedText("connector"), Reason: "a frozen device wire"},
		},
		"a mount inside the api": {
			{Pattern: "/api/v1/devices/{device_id}/secret", Handler: mountedText("x"), Reason: "why not"},
		},
		"a mount over the document": {
			{Pattern: specJSONPath, Handler: mountedText("x"), Reason: "why not"},
		},
		"one pattern mounted twice": {
			{Pattern: "/p/ws", Handler: mountedText("a"), Reason: "a frozen device wire"},
			{Pattern: "/p/ws", Handler: mountedText("b"), Reason: "the same wire again"},
		},
	}

	for name, mounts := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := New(mountConfig(mounts))
			require.ErrorIs(t, err, ErrMountRefused)
		})
	}
}

// TestAMountedRouteIsExcludedFromTheDocumentDiffOnlyWhenItWasDeclared is the
// layer that keeps the exception honest. The diff skips a pattern Config.Mounts
// named and nothing else, so a route that appeared on the router by some other
// route is still a startup failure.
func TestAMountedRouteIsExcludedFromTheDocumentDiffOnlyWhenItWasDeclared(t *testing.T) {
	t.Parallel()

	server := serverWithMounts(t,
		Mount{Pattern: "/p/ws", Handler: mountedText("connector"), Reason: "a frozen device wire"},
	)

	require.NoError(t, verifyDocumented(server.mux, server.api, []string{"/p/ws"}))

	err := verifyDocumented(server.mux, server.api, nil)
	require.ErrorIs(t, err, ErrUndocumentedRoute)
	require.Contains(t, err.Error(), "/p/ws")
}

// mountConfig is a server config that differs from the tested one in nothing
// but its mounts.
func mountConfig(mounts []Mount) Config {
	return Config{
		Version:    "test",
		Principals: fixedPrincipal("user:operator"),
		Resources:  seededStore(),
		Presets:    seededPresets(),
		Endpoints:  seededEndpoints(),
		Grants:     &fakeGrantStore{},
		Sessions:   newFakeSessions(),
		Accounts:   &fakeAccounts{},
		Readiness:  &fakeReadiness{report: readyReport()},
		Devices:    noPrinters{},
		Stores:     noStorages{},
		Events:     noEvents{},
		Mounts:     mounts,
	}
}

func serverWithMounts(t *testing.T, mounts ...Mount) *Server {
	t.Helper()

	server, err := New(mountConfig(mounts))
	require.NoError(t, err)

	return server
}
