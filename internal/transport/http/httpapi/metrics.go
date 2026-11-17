package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// metricsPath is where a scraper reads this process. It is the conventional
// path and not one under /api/v1, because an operator writes one scrape
// configuration for every exporter, and a different path would need a line of
// configuration per deployment.
const metricsPath = "/metrics"

// notCompiled is what a scrape of a build carrying no registry gets, with a
// 404. An empty 200 would read as a healthy process with nothing to report,
// and a scraper would alert on the wrong thing.
const notCompiled = "metrics are not compiled into this build\n"

// registerMetrics publishes the process's metric registry as an operation.
//
// It is called only where the exposition has no listener of its own.
// core.observability.addr asks for the exposition on its own socket, so an
// operator who set it gets no such operation here and a 404 at that path. See
// metricsRouter.
//
// It is an operation and not a second mux. This package seals its router
// (TestNothingOutsideTheSealedPackageMountsAServeMux), and every path the
// router answers must be in the document. A metrics endpoint mounted beside
// the operations would be a path the router answers and the document does not
// describe.
//
// It carries identity and a named grant. The exposition names every printer
// this deployment has seen and how many there are, and the device listing
// refuses that to anybody holding no grant. With identity alone, any account
// that can log in could read the whole fleet through this route.
//
// The grant is fleet-wide and the answer is all or nothing. A scrape is machine
// to machine, so there is no caller to narrow the document for, and narrowing
// it would give two scrapers two different series sets under one metric name.
// So the endpoint takes a named grant and the per-device labels stay. See
// FleetTelemetryAccess.
func registerMetrics(api huma.API, handler http.Handler, access FleetTelemetryAccess) {
	if handler == nil {
		handler = http.HandlerFunc(notCompiledHandler)
	}

	register(api, huma.Operation{
		OperationID: "get-metrics",
		Method:      http.MethodGet,
		Path:        metricsPath,
		Summary:     "The Prometheus exposition for this process",
		Description: "Serves the process's metric registry in the Prometheus text format, negotiating " +
			"OpenMetrics when the scraper asks for it. A build compiled without the observability " +
			"tag links no metrics client and answers 404 here, which is a scrape target that says " +
			"it has nothing rather than one that reports a healthy process with no series. A build that " +
			"links the client answers 404 too while core.observability.enabled is false.",
		Tags:      []string{"system"},
		Errors:    []int{http.StatusUnauthorized, http.StatusForbidden},
		Responses: metricsResponse(),
	}, func(ctx context.Context, _ *struct{}) (*huma.StreamResponse, error) {
		if err := allowScrape(ctx, access); err != nil {
			return nil, err
		}

		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			// The exposition is written by the metrics client's own handler,
			// which negotiates the format against the scraper's Accept header.
			// Huma cannot model that: the body is a content negotiation, not a
			// schema.
			request, writer := humachi.Unwrap(hctx)
			handler.ServeHTTP(writer, request)
		}}, nil
	})
}

// metricsResponse documents the body, which Huma cannot derive from a streaming
// operation's function-valued Body.
func metricsResponse() map[string]*huma.Response {
	return map[string]*huma.Response{
		"200": {
			Description: "The exposition, one metric family per block, in the Prometheus text format " +
				"or in OpenMetrics when the scraper negotiated it.",
			Content: map[string]*huma.MediaType{
				"text/plain": {Schema: &huma.Schema{Type: huma.TypeString}},
			},
		},
		"404": {
			Description: "This build links no metrics client, so there is nothing to serve.",
			Content: map[string]*huma.MediaType{
				"text/plain": {Schema: &huma.Schema{Type: huma.TypeString}},
			},
		},
	}
}

// notCompiledHandler is what a server built with no registry answers, in the
// same bytes and the same status as a build with no observability tag. Both
// have no metrics, so a deployment does not need to tell them apart.
func notCompiledHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)

	// The status is already written, so a failed write cannot be reported.
	_, _ = w.Write([]byte(notCompiled))
}

// FleetTelemetryAccess decides whether a caller may read an answer covering the
// whole fleet. It is the permission /metrics takes, and it is defined here
// because this is the package that consumes it.
type FleetTelemetryAccess interface {
	// CanReadFleetTelemetry reports whether this principal reaches telemetry on
	// every device this deployment knows about. A deployment that knows about
	// none has nothing to withhold and permits the read.
	CanReadFleetTelemetry(ctx context.Context, principal string) (bool, error)
}

// allowScrape takes the decision for one scrape.
//
// A nil decider refuses. The exposition covers the fleet, and a build that
// wired no decider has not decided that everyone may read it. The refusal is
// the same one a caller holding no grant gets.
func allowScrape(ctx context.Context, access FleetTelemetryAccess) error {
	if access == nil {
		return scrapeRefused()
	}

	allowed, err := access.CanReadFleetTelemetry(ctx, principalFrom(ctx))
	if err != nil {
		return err
	}

	if !allowed {
		return scrapeRefused()
	}

	return nil
}

// scrapeRefused is the one refusal this route has. A caller reaching telemetry
// on some devices and not all of them gets it, and so does a caller reaching
// none. The exposition is one document over the fleet, so a grant on part of
// the fleet reads none of it.
func scrapeRefused() error {
	return fault.New(authz.CodeAccessDenied(),
		"reading the metrics exposition takes telemetry on every device this server has seen").With(
		fault.StringParam("object", "fleet"),
		fault.StringParam("action", authz.ActionObserve.String()),
	)
}

// metricsRouter is what the metrics listener answers: the exposition at
// /metrics, and nothing else this process serves.
//
// It takes no credential. The socket is the boundary, as a collector expects:
// etcd, CoreDNS and ingress-nginx each serve their exposition on a port a
// deployment publishes to its own network. An operator asks for it by naming
// an address (core.observability.addr), and nothing binds otherwise.
//
// The API listener then does not serve /metrics at all. registerMetrics is not
// called, so the path is a 404 there and the document names no such operation.
// Access to a scrape is decided by whether the deployment publishes the
// address, and not by a key.
//
// The router stays sealed. This router is built here, inside the package that
// owns the routes, out of the same Config, so nothing outside the package
// assembles a route and the document still describes every path the API
// origin answers. deviceRouter is built the same way.
//
// Every request passes the middleware the API router uses, so a scrape is
// traced, counted and recovered like any other. It does not pass the CORS
// allow-list, because only a browser needs one.
func metricsRouter(cfg Config) *chi.Mux {
	exposition := cfg.Metrics
	if exposition == nil {
		exposition = http.HandlerFunc(notCompiledHandler)
	}

	mux := chi.NewMux()
	mux.Use(middlewareStack(cfg)...)
	mux.NotFound(notFound)
	mux.MethodNotAllowed(methodNotAllowed)
	mux.Method(http.MethodGet, metricsPath, exposition)

	return mux
}
