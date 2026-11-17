package app

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// routeNotFound is the code the origin's own refusal carries.
const routeNotFound = "ROUTE_NOT_FOUND"

// TestEveryPathNoRouteClaimsIsRefused is the origin's answer to a caller at the
// wrong address, and the origin root is one of those addresses.
//
// A caller that gets 200 and a body it cannot parse has no way to tell a wrong
// address from a working one. A printer is the caller that cannot ask: the
// request went through, the status says so, and the body is a document the
// firmware never reads. So every path the API does not claim carries the same
// problem document, whatever the method.
//
// core.frontend.enabled is false here because the origin root is the app's
// address on a build that carries one, and this case is about the address
// nothing claims. The two answers have to be the same bytes in both builds,
// which is what switching the app off makes testable on either.
func TestEveryPathNoRouteClaimsIsRefused(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), lines(
		"core:", "  log:", "    level: error", "  frontend:", "    enabled: false"))

	cases := map[string]struct{ method, path string }{
		"a read of the origin root":                         {http.MethodGet, "/"},
		"a write to the origin root":                        {http.MethodPost, "/"},
		"a read of a path beside the frozen dialect's own":  {http.MethodGet, "/p/nothing"},
		"a write to a path beside the frozen dialect's own": {http.MethodPost, "/p/nothing"},
		"a read of a path the application's router owns":    {http.MethodGet, "/printers"},
		"a write to a path the application's router owns":   {http.MethodPost, "/nothing-here"},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := running.call(t, testCase.method, testCase.path, "")

			require.Equal(t, http.StatusNotFound, got.status,
				"%s %s answered %d", testCase.method, testCase.path, got.status)
			require.Contains(t, got.body, routeNotFound,
				"%s %s carries no problem document", testCase.method, testCase.path)
		})
	}
}

// TestTheReferenceAndTheFrozenWireAnswerTheirOwnPaths is the other direction. A
// refusal that took a path somebody needs is worse than the defect it closes: a
// reader reaches the API reference at /docs and a printer reaches the dialect at
// the two paths its firmware has compiled in.
func TestTheReferenceAndTheFrozenWireAnswerTheirOwnPaths(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), lines("core:", "  log:", "    level: error"))

	for _, path := range []string{"/docs", "/openapi.json"} {
		served := running.call(t, http.MethodGet, path, "")
		require.Equal(t, http.StatusOK, served.status, "%s answered %d", path, served.status)
		require.NotContains(t, served.body, routeNotFound, "%s carries the origin's refusal", path)
	}

	// The dialect's own refusal for a registration carrying no fingerprint.
	// Nothing else on this origin answers that, so it says the route is the
	// driver's and not a reservation standing in front of it.
	refused := running.call(t, http.MethodPost, xbuddyRegister, "{}")
	require.Equal(t, http.StatusBadRequest, refused.status,
		"the frozen registration path answered %d: %s", refused.status, refused.body)
}
