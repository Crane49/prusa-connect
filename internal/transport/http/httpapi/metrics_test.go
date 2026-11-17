package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTheMetricsEndpointServesTheRegistryThroughTheSealedRouter is an earlier design decision
// on the surface the seal covers. /metrics is a published operation rather than
// a handler mounted beside the router, which is why bypassProbePaths in
// contract_test.go does not name it.
//
// The exposition reaches a scraper through a registered operation, so
// chi.Walk sees it, the document publishes it, and verifyDocumented would
// refuse a server whose router and document disagreed about it.
func TestTheMetricsEndpointServesTheRegistryThroughTheSealedRouter(t *testing.T) {
	t.Parallel()

	const exposition = "# HELP pcl_build_info Always 1.\npcl_build_info{version=\"test\"} 1\n"

	server := serverWithMetrics(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(exposition))
	}))

	scraped := call(t, server.Handler(), http.MethodGet, metricsPath, "")

	require.Equal(t, http.StatusOK, scraped.Code, scraped.Body.String())
	require.Equal(t, exposition, scraped.Body.String())
	require.Contains(t, scraped.Header().Get("Content-Type"), "text/plain")

	// The router and the document agree about it, which is what makes it an
	// operation rather than a second mux.
	require.NoError(t, verifyDocumented(server.mux, server.api, nil))
	require.Contains(t, publishedOperations(server.api), http.MethodGet+" "+metricsPath)
}

// TestAScrapeOfABuildWithNoRegistryIs404 keeps a build carrying no metrics
// client distinguishable from one that is measuring nothing. An empty 200
// reads as a healthy process with nothing to report, and a scraper alerts on
// the wrong thing.
func TestAScrapeOfABuildWithNoRegistryIs404(t *testing.T) {
	t.Parallel()

	server := serverWithMetrics(t, nil)

	scraped := call(t, server.Handler(), http.MethodGet, metricsPath, "")

	require.Equal(t, http.StatusNotFound, scraped.Code)
	require.Equal(t, notCompiled, scraped.Body.String())
	require.NoError(t, verifyDocumented(server.mux, server.api, nil),
		"the operation is published whether or not this build can answer it")
}

// TestAScrapeWithNoIdentityIsRefused is why the operation does not carry
// noSecurity. The exposition names every printer this deployment has seen and
// how many there are, which is the fleet-shaped fact the printer listing
// refuses to a caller holding no grant.
func TestAScrapeWithNoIdentityIsRefused(t *testing.T) {
	t.Parallel()

	server, err := New(metricsConfig(nil, fixedPrincipal("")))
	require.NoError(t, err)

	scraped := call(t, server.Handler(), http.MethodGet, metricsPath, "")
	requireProblem(t, scraped, http.StatusUnauthorized)
}

func serverWithMetrics(t *testing.T, handler http.Handler) *Server {
	t.Helper()

	server, err := New(metricsConfig(handler, fixedPrincipal("user:operator")))
	require.NoError(t, err)

	return server
}

func metricsConfig(handler http.Handler, who Principals) Config {
	return Config{
		Version:    "test",
		Principals: who,
		Resources:  seededStore(),
		Presets:    seededPresets(),
		Endpoints:  seededEndpoints(),
		Grants:     &fakeGrantStore{},
		Sessions:   newFakeSessions(),
		Accounts:   &fakeAccounts{},
		Readiness:  &fakeReadiness{report: readyReport()},
		Devices:    noPrinters{},
		Events:     noEvents{},
		Stores:     noStorages{},
		Metrics:    handler,
		// Permitted, so every case above this one measures what it is about
		// rather than the grant. The grant has cases of its own below.
		FleetTelemetry: &fleetGrant{allowed: true},
	}
}

// fleetGrant is the decision /metrics takes, as a test double. It records the
// principal it was asked about, so a case can hold that the route asks about
// the caller and not about something else.
type fleetGrant struct {
	allowed bool
	err     error
	asked   string
}

func (f *fleetGrant) CanReadFleetTelemetry(_ context.Context, principal string) (bool, error) {
	f.asked = principal

	return f.allowed, f.err
}

// /metrics takes a named grant. Identity alone let any account that could log
// in read every device this deployment has seen, through the device_id label
// every family carries. That is the
// fleet-shaped fact the device listing refuses to a caller holding no grant, so
// it was a permission enforced on one transport and not the other.
//
// The refusal is 403 and not 404. A scraper whose service account lost its
// grant has to be able to tell that from a build with no metrics client, or the
// operator debugs the wrong thing.
func TestAScrapeByACallerWithNoFleetGrantIsRefused(t *testing.T) {
	t.Parallel()

	grant := &fleetGrant{allowed: false}

	config := metricsConfig(servesExposition(), fixedPrincipal("user:nobody"))
	config.FleetTelemetry = grant

	server, err := New(config)
	require.NoError(t, err)

	scraped := call(t, server.Handler(), http.MethodGet, metricsPath, "")

	require.Equal(t, http.StatusForbidden, scraped.Code, scraped.Body.String())
	require.NotContains(t, scraped.Body.String(), "pcl_build_info",
		"the exposition was written to a caller the graph refused")
	require.Equal(t, "user:nobody", grant.asked, "the route decided about somebody other than the caller")
}

// The holder of the grant is served, so the refusal above is a decision and not
// a route that stopped working. A scraper's service account holding one org
// tuple reaches every device in the org, which is the grant an operator writes.
func TestAScrapeByTheHolderOfTheFleetGrantIsServed(t *testing.T) {
	t.Parallel()

	grant := &fleetGrant{allowed: true}

	config := metricsConfig(servesExposition(), fixedPrincipal("service_account:scraper"))
	config.FleetTelemetry = grant

	server, err := New(config)
	require.NoError(t, err)

	scraped := call(t, server.Handler(), http.MethodGet, metricsPath, "")

	require.Equal(t, http.StatusOK, scraped.Code, scraped.Body.String())
	require.Contains(t, scraped.Body.String(), "pcl_build_info")
	require.Equal(t, "service_account:scraper", grant.asked)
}

// A build that wired no decider refuses. The exposition is the fleet, and a
// build that named no decider has not decided that everybody may read it: it
// has failed to say. Serving on a nil port is how an authorization check
// becomes optional by accident.
func TestAScrapeWithNoDeciderWiredIsRefused(t *testing.T) {
	t.Parallel()

	config := metricsConfig(servesExposition(), fixedPrincipal("user:operator"))
	config.FleetTelemetry = nil

	server, err := New(config)
	require.NoError(t, err)

	scraped := call(t, server.Handler(), http.MethodGet, metricsPath, "")

	require.Equal(t, http.StatusForbidden, scraped.Code, scraped.Body.String())
	require.NotContains(t, scraped.Body.String(), "pcl_build_info")
}

// A listener of its own serves the exposition to a caller carrying neither
// identity nor a grant, which is the whole of what core.observability.addr
// buys. The socket is the boundary, so there is nothing here to refuse with:
// the principal is empty and no decider is wired, and both would refuse on the
// API listener.
//
// The per-device labels reach that caller too. Whoever reaches the port reads
// the fleet, which is the cost the address is chosen to cover.
func TestTheMetricsListenerServesTheExpositionWithNoCredential(t *testing.T) {
	t.Parallel()

	config := metricsConfig(servesExposition(), fixedPrincipal(""))
	config.MetricsListener = true
	config.FleetTelemetry = nil

	server, err := New(config)
	require.NoError(t, err)

	scraped := call(t, server.MetricsHandler(), http.MethodGet, metricsPath, "")

	require.Equal(t, http.StatusOK, scraped.Code, scraped.Body.String())
	require.Contains(t, scraped.Body.String(), "pcl_build_info")
	require.Contains(t, scraped.Body.String(), `pcl_printer_up{device_id="d1"}`)
}

// The exposition is all that listener answers. A collector reaching the socket
// reads the metrics and no other route this process serves, so publishing the
// port to a network is publishing one document and not an API.
func TestTheMetricsListenerAnswersNothingElse(t *testing.T) {
	t.Parallel()

	config := metricsConfig(servesExposition(), fixedPrincipal("user:operator"))
	config.MetricsListener = true

	server, err := New(config)
	require.NoError(t, err)

	for _, path := range []string{"/api/v1/health", "/api/v1/devices", "/", "/openapi.json"} {
		answered := call(t, server.MetricsHandler(), http.MethodGet, path, "")
		require.Equal(t, http.StatusNotFound, answered.Code, path)
	}

	posted := call(t, server.MetricsHandler(), http.MethodPost, metricsPath, "")
	require.Equal(t, http.StatusMethodNotAllowed, posted.Code)
}

// One listener serves it, never both. A deployment that named an address moved
// the exposition there whole, so the API listener answers 404 at the path and
// the document it publishes names no operation for it.
//
// The router and the document still agree, which is the seal: what changed is
// that both of them stopped carrying the route, not that one did.
func TestAMetricsListenerTakesTheExpositionOffTheAPIListener(t *testing.T) {
	t.Parallel()

	config := metricsConfig(servesExposition(), fixedPrincipal("user:operator"))
	config.MetricsListener = true

	server, err := New(config)
	require.NoError(t, err)

	scraped := call(t, server.Handler(), http.MethodGet, metricsPath, "")

	require.Equal(t, http.StatusNotFound, scraped.Code)
	require.NotContains(t, scraped.Body.String(), "pcl_build_info")
	require.NotContains(t, publishedOperations(server.api), http.MethodGet+" "+metricsPath)
	require.NoError(t, verifyDocumented(server.mux, server.api, nil))
}

// A build carrying no metrics client says so on that listener too, in the same
// bytes and the same status the API listener answers. An operator who gave the
// exposition a port of its own still has to tell an unbuilt client from a client
// measuring nothing.
func TestAScrapeOfTheMetricsListenerOfABuildWithNoRegistryIs404(t *testing.T) {
	t.Parallel()

	config := metricsConfig(nil, fixedPrincipal("user:operator"))
	config.MetricsListener = true

	server, err := New(config)
	require.NoError(t, err)

	scraped := call(t, server.MetricsHandler(), http.MethodGet, metricsPath, "")

	require.Equal(t, http.StatusNotFound, scraped.Code)
	require.Equal(t, notCompiled, scraped.Body.String())
}

// servesExposition is a registry handler with two recognisable families in it,
// so a case can assert that the body did or did not reach the caller, and that
// the per-device labels went with it.
func servesExposition() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte("# HELP pcl_build_info Always 1.\npcl_build_info{version=\"test\"} 1\n" +
			"# HELP pcl_printer_up Whether a printer answered.\npcl_printer_up{device_id=\"d1\"} 1\n"))
	})
}
