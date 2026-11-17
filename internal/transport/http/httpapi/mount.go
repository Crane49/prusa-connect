package httpapi

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// apiRoot is the prefix every published operation lives under. A mount may not
// claim a path inside it, and a mount that catches everything is not allowed to
// answer one either: see guard.
const apiRoot = "/api/"

// Mount is a handler served on a path the OpenAPI document does not publish.
//
// It exists for the wires this server does not own. Shipping printer firmware
// dials absolute paths the OpenAPI document never described, and a
// browser asks for the frontend at the origin root; neither is an operation a
// generated client would ever call, and publishing them as ones would describe
// a websocket handshake and a bundle of Javascript as REST. Everything a client
// calls still registers through huma.Register.
//
// The mount belongs here rather than beside the server because of what
// TestNothingOutsideTheSealedPackageMountsAServeMux holds: a composition root
// that wrapped Server.Handler() to add these paths would serve them past every
// layer this package has. Handing them in means one router still answers the
// whole origin, chi.Walk still enumerates every pattern, and the exception is a
// declared value with a reason on it.
type Mount struct {
	// Pattern is the chi pattern the handler answers on, such as /p/* for a
	// frozen device wire or /* for a single-page frontend.
	Pattern string
	// Handler answers it. The pattern is not stripped: a driver whose devices
	// dial absolute paths needs the path it was given, and one that does not
	// wraps itself in http.StripPrefix before it gets here.
	Handler http.Handler
	// Reason says in one sentence why this path is not an operation. It is
	// required, so every path left out of the contract carries a reason a
	// reviewer can read.
	Reason string
	// Device marks a wire a device dials and not a path a person reaches.
	// It is what the device-facing listener serves, and it is the whole of what
	// that listener serves: see Server.DeviceHandler.
	Device bool
}

// NotRouted answers a path no route claims, with the problem document this
// server's own router writes for one.
//
// A mount whose pattern is wider than what it answers hands a request here,
// so one address reads the same whether it fell through a wildcard or reached
// no pattern at all. A device dialling an address nothing serves needs that: a
// 200 and a page is a success it cannot use, and neither end sees an error.
func NotRouted() http.Handler { return http.HandlerFunc(notFound) }

// validateMounts reports the first mount this server will not serve. It runs
// before the router is built, so a rejected mount registers no route.
func validateMounts(mounts []Mount) error {
	seen := make([]string, 0, len(mounts))

	for _, mount := range mounts {
		if err := mount.validate(); err != nil {
			return err
		}

		if slices.Contains(seen, mount.Pattern) {
			return fmt.Errorf("%w: %s is mounted twice", ErrMountRefused, mount.Pattern)
		}

		seen = append(seen, mount.Pattern)
	}

	return nil
}

// validate holds one mount to what it may claim.
func (m Mount) validate() error {
	if m.Handler == nil {
		return fmt.Errorf("%w: %q has no handler", ErrMountRefused, m.Pattern)
	}

	if m.Reason == "" {
		return fmt.Errorf("%w: %s carries no reason, and an unexplained path is one nobody reviews",
			ErrMountRefused, m.Pattern)
	}

	if !strings.HasPrefix(m.Pattern, "/") {
		return fmt.Errorf("%w: %q is not a path", ErrMountRefused, m.Pattern)
	}

	if reserved(m.Pattern) {
		return fmt.Errorf("%w: %s is this server's own, and a handler there would answer a path "+
			"the OpenAPI document describes", ErrMountRefused, m.Pattern)
	}

	return nil
}

// PatternsOf is the mounted patterns: what the route-and-document diff is told
// to expect, and what the composition root logs at startup as the places a
// printer may dial.
func PatternsOf(mounts []Mount) []string {
	out := make([]string, 0, len(mounts))
	for _, mount := range mounts {
		out = append(out, mount.Pattern)
	}

	return out
}

// guard is what a mount is wrapped in before it reaches the router.
//
// A mount that catches everything catches this server's own paths too: chi
// falls back to /* for /api/v1/nothing-here, and a single-page frontend would
// answer it with a page saying the route is fine. The refusal has to be the
// router's own, so it is written here and not left to the handler, and it is
// the same bytes an unmounted server answers with.
func guard(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reserved(r.URL.Path) {
			notFound(w, r)

			return
		}

		handler.ServeHTTP(w, r)
	})
}

// rootCatchAll is the pattern a mount claims when it answers every address this
// server does not route itself.
const rootCatchAll = "/*"

// navigations holds the mount at the origin root to the method a browser
// navigation uses.
//
// That mount is a single-page fallback: a path with no file behind it is a
// route the application's own router owns, so it answers a page for any
// address. A navigation is a GET, and a HEAD arrives here rewritten into one by
// headMiddleware. A request with any other method came from a caller that meant
// a different server, and a page is a success that caller cannot use. A device
// dialling an origin whose connector is not mounted is one such caller.
//
// It is the same rule guard holds one step further out. A mount that catches
// everything answers what it owns, and the refusal for the rest is the
// router's own.
func navigations(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			notFound(w, r)

			return
		}

		handler.ServeHTTP(w, r)
	})
}

// reserved reports whether a path belongs to this server and not to a mount
// beside it: an operation, the metrics exposition, the document, or the
// reference that renders it.
//
// It matches a subtree and not only the exact path, because /openapi.json/extra
// and /docs/anything are this server's names too: a mount answering under one
// of them publishes a page at a URL a reader would take for the contract. The
// exposition is here because core.observability.addr moves it to a listener
// of its own, and a scraper still pointed at this one must read 404, not the
// web app's entry page.
func reserved(path string) bool {
	for _, own := range append([]string{strings.TrimSuffix(apiRoot, "/"), metricsPath}, ownPaths...) {
		if path == own || strings.HasPrefix(path, own+"/") {
			return true
		}
	}

	return false
}

// ownPaths are the routes that carry the contract rather than implement it. They
// are the same four infrastructureRoutes names, which is deliberate: a path this
// server serves and a path a mount may not claim are one list.
//
//nolint:gochecknoglobals // a closed list read at validation time, never written.
var ownPaths = []string{specJSONPath, specYAMLPath, docsPath, docsScriptPath}
