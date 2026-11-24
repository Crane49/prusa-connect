package httpapi

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// Principals resolves the caller of a request to the principal the
// authorization graph is keyed by. Authentication belongs to another package;
// this is the seam it plugs into.
//
// There is no default implementation, and a header the client sets is never an
// identity. The interface exists so that who a caller is gets decided in one
// place.
type Principals interface {
	// Principal returns the caller's identity. A refusal is reported as a
	// Fault carrying a registered code; any other error is answered as an
	// invalid token, with the cause in the log line and not in the response.
	Principal(r *http.Request) (string, error)
	// Caller is Principal with the scopes the request's credential is limited
	// to. The transport puts the scopes on the request context, where every
	// decision of the authorization graph reads them.
	Caller(r *http.Request) (Caller, error)
}

// Caller is who a request authenticates as, and what its credential may use of
// that principal's grants.
type Caller struct {
	// Principal is the graph subject, user:<id> or service_account:<id>.
	Principal string
	// Scopes limit the credential. Nil is a credential with no limit beyond
	// the grants: a browser session, or a token another issuer signed.
	Scopes []authz.Scope
}

// Unscoped is the Caller of a Principals implementation whose credentials
// carry no scopes, so it can answer Caller from its Principal.
func Unscoped(principal string, err error) (Caller, error) {
	return Caller{Principal: principal}, err
}

// withCaller puts a resolved caller on a request context: the principal under
// this package's key, and the scopes where the authorization graph reads them.
func withCaller(ctx context.Context, caller Caller) context.Context {
	return authz.WithScopes(context.WithValue(ctx, principalKey{}, caller.Principal), caller.Scopes)
}

// principalKey is the context key the resolved principal is carried under.
// requestKey carries the raw request and writerKey its response writer.
// headKey is the mark headMiddleware leaves on a request it rewrote.
type (
	principalKey struct{}
	requestKey   struct{}
	writerKey    struct{}
	headKey      struct{}
)

// principalFrom returns the principal the middleware resolved, empty when the
// request did not pass through it.
func principalFrom(ctx context.Context) string {
	principal, _ := ctx.Value(principalKey{}).(string)

	return principal
}

// PrincipalFrom returns the principal the middleware resolved for this request,
// empty when nothing did.
//
// It is exported for the composition root, which records who decided an
// admission against the registration. The middleware is the only thing that
// writes it and nothing outside this package can, so an exported read hands out
// no way to claim an identity.
func PrincipalFrom(ctx context.Context) string { return principalFrom(ctx) }

// requestFrom returns the request the operation was called for, nil when it did
// not pass through requestMiddleware.
func requestFrom(ctx context.Context) *http.Request {
	request, _ := ctx.Value(requestKey{}).(*http.Request)

	return request
}

// writerFrom returns the response writer the operation was called for, nil when
// it did not pass through requestMiddleware.
//
// A handler needs it to move the connection's read or write deadline, which
// Huma does not offer. A body that takes longer than ReadTimeout to arrive is
// normal for a file and a failure for everything else, so the deadline is
// lifted per request and the server's timeout stays short.
func writerFrom(ctx context.Context) http.ResponseWriter {
	writer, _ := ctx.Value(writerKey{}).(http.ResponseWriter)

	return writer
}

// requestMiddleware carries the raw request and its response writer into a
// handler's context. Two things need them. Logout revokes the credential the
// caller presented, and only the Sessions implementation knows which header or
// cookie carries it. A file transfer moves the connection's deadlines as it
// makes progress.
func requestMiddleware(ctx huma.Context, next func(huma.Context)) {
	request, writer := humachi.Unwrap(ctx)

	next(huma.WithValue(huma.WithValue(ctx, requestKey{}, request), writerKey{}, writer))
}

// headMiddleware answers HEAD from the GET handler. RFC 9110 §9.3.2 requires
// that of a general-purpose server, and an uptime probe sends HEAD. net/http
// drops the body of a HEAD response, but only after the handler has produced
// it, so a handler whose body is costly checks headOf.
//
// It runs before the router. A separate HEAD route would be a route the
// OpenAPI document does not publish, and verifyDocumented refuses such a
// route. With the method rewritten, HEAD can only reach a handler the document
// already describes as GET.
func headMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			next.ServeHTTP(w, r)

			return
		}

		asGet := r.Clone(context.WithValue(r.Context(), headKey{}, true))
		asGet.Method = http.MethodGet

		next.ServeHTTP(w, asGet)
	})
}

// headOf reports whether this request arrived as a HEAD. An operation that
// produces its body at a cost needs to know, since net/http discards the body
// only after it is written, and a stream written into it never finishes.
func headOf(ctx context.Context) bool {
	head, _ := ctx.Value(headKey{}).(bool)

	return head
}

// listValuedRequestHeaders are the request headers this API reads that RFC 9110
// defines as an ABNF list. Folding is valid only for these, so the set is
// written out.
//
//nolint:gochecknoglobals // a fixed set the middleware reads; a const cannot hold a slice.
var listValuedRequestHeaders = []string{"If-Match", "If-None-Match"}

// foldListHeaderMiddleware joins repeated field lines of a list-valued header
// into the single line RFC 9110 §5.3 says they are equal to.
//
// It is a middleware because the lines are lost before a handler runs. Huma
// reads a header parameter with Header.Get, which returns the first field line
// and discards the rest. Without the fold,
//
//	If-Match: "v0"
//	If-Match: "v1"
//
// fails a precondition that the equivalent `If-Match: "v0", "v1"` passes. A
// client, a cache and a proxy may all emit either form, and §5.3 says a
// recipient must treat them the same.
func foldListHeaderMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, name := range listValuedRequestHeaders {
			lines := r.Header.Values(name)
			if len(lines) <= 1 {
				continue
			}

			r.Header.Set(name, strings.Join(lines, ", "))
		}

		next.ServeHTTP(w, r)
	})
}

// traceMiddleware puts this request's W3C trace id on the context, so a Fault
// rendered anywhere below carries the id of the log line holding its cause.
func traceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(withTraceID(r.Context(), traceIDOf(r))))
	})
}

// loggerMiddleware carries the server's logger into every request, so the
// failure answered at the edge is logged through the logger the server was
// built with.
func loggerMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(withLogger(r.Context(), logger)))
		})
	}
}

// recoverMiddleware answers a panicking handler with a redacted problem
// document, so the client does not see a closed connection.
// http.ErrAbortHandler is re-panicked because it is the documented way for a
// handler to hang up on purpose.
//
// The panic value is wrapped into the fault, so the request's failure line
// carries it. The stack goes on a line of its own, under the same trace id,
// because this goroutine is the only place that still holds where the handler
// stopped.
func recoverMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}

				if err, ok := recovered.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(recovered)
				}

				ctx := r.Context()
				loggerFrom(ctx).ErrorContext(ctx, "a handler panicked",
					slog.String("trace_id", traceIDFrom(ctx)),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.String("panic", fmt.Sprint(recovered)),
					slog.String("stack", string(debug.Stack())))

				writeProblem(w, r, httpfault.Unexpected("panic serving request").
					Wrap(fmt.Errorf("%w: %v", errPanicked, recovered)))
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// bodyLimitMiddleware bounds what a request body may allocate. The reader
// reports the breach on the read that crosses it, carrying the limit in an
// *http.MaxBytesError, which faultFor turns into the body-too-large code.
//
// It is the only enforcer of the bound. Huma's per-operation limiter is
// switched off at registration, because it defaults to 1 MiB whatever the
// configured limit is and reports the breach with no limit in it.
//
// A request on a staged module route is held to the limit the operator
// approved for that route, and every other module request to the document
// bound. The module host holds a staged body to the same limit
// (ADR-20261120-xys rule 6).
//
// A file or module request whose Content-Length is over its limit is refused
// here, before a byte of the body is read. Otherwise the client sends the
// whole limit first, and the connection is closed under it.
func bodyLimitMiddleware(documents, uploads, upgrades int64, modules ModulePlane) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			limit, declared, refusal := documents, false, httpfault.BodyTooLarge

			if carriesAnUpgrade(r) {
				limit, declared = upgrades, true
			} else if carriesAFile(r) {
				limit, declared, refusal = uploads, true, httpfault.UploadTooLarge
			} else if surface, inner, toModule := moduleTarget(r, modules); toModule {
				declared = true

				if staged, isStaged := stagedLimit(surface, r.Method, inner); isStaged {
					limit = staged
				}
			}

			if declared && r.ContentLength > limit {
				writeProblem(w, r, refusal(limit))

				return
			}

			r.Body = http.MaxBytesReader(w, r.Body, limit)

			next.ServeHTTP(w, r.WithContext(withBodyLimit(r.Context(), limit)))
		})
	}
}

// moduleTarget is the module surface a request reaches and its path under
// the module's mount: under the modules group, or under a root prefix a
// module claims. Like carriesAFile it runs before chi has matched a route, so
// the path is matched by hand. A request with no body takes no bound, so the
// plane is not asked for one.
func moduleTarget(r *http.Request, modules ModulePlane) (ModuleSurface, string, bool) {
	if modules == nil || r.ContentLength == 0 {
		return ModuleSurface{}, "", false
	}

	if rest, under := strings.CutPrefix(r.URL.Path, modulesPathPrefix+"/"); under {
		// The plane is asked only for a namespace a module could have been
		// given, as the mount asks it (moduleHandler).
		namespace, _, _ := strings.Cut(rest, "/")
		if extension.Namespace(namespace).Validate() != nil {
			return ModuleSurface{}, "", false
		}

		surface, claimed := modules.Module(namespace)
		if !claimed || surface.Mount != "" {
			return ModuleSurface{}, "", false
		}

		inner := innerPath(r.URL.Path, ModuleMount(namespace))
		if servedByCore(inner) {
			return ModuleSurface{}, "", false
		}

		return surface, inner, true
	}

	if rootReserved(r.URL.Path) {
		return ModuleSurface{}, "", false
	}

	surface, claimed := modules.Root(r.URL.Path)
	if !claimed || surface.Mount == "" {
		return ModuleSurface{}, "", false
	}

	return surface, innerPath(r.URL.Path, surface.Mount), true
}

// coreUnderMount is the first segment of every path under a module's
// namespace that the core serves itself: the module's state, its OpenAPI
// document and its UI files. chi routes them before the module's mount, so a
// module's staged route never reaches them and must not widen their body
// bound. TestNoStagedRouteWidensACoreOperation walks the router against it.
var coreUnderMount = func() []string {
	under := ModuleMount("{" + namespaceParam + "}")
	own := []string{}

	for _, path := range []string{moduleStatePath, moduleStateKeyPath, moduleOpenAPIPath, moduleUIFamilyPath} {
		own = append(own, rootOf(strings.TrimPrefix(path, under)))
	}

	return own
}()

// servedByCore reports whether a path under a module's namespace is one the
// core serves.
func servedByCore(inner string) bool {
	return slices.Contains(coreUnderMount, rootOf(inner))
}

// innerPath is the path with the mount taken off, as a module's route names
// it.
func innerPath(path, mount string) string {
	rest := strings.TrimPrefix(path, mount)
	if rest == "" {
		return "/"
	}

	return rest
}

// mebibyte is one MiB in bytes, the unit a staged route's limit is written in.
const mebibyte = 1 << 20

// stagedLimit is the limit in bytes of the staged route a request is on, and
// false for a request on any other route.
func stagedLimit(surface ModuleSurface, method, inner string) (int64, bool) {
	for _, route := range surface.Staged {
		if route.Matches(method, inner) {
			return int64(route.MaxMiB) * mebibyte, true
		}
	}

	return 0, false
}

// carriesAFile reports whether this request's body is a file and not a
// document, and so which of the two bounds applies.
//
// It matches on the route and not on Content-Type, because the client chooses
// the header and the server chooses the route. A client cannot widen its own
// limit by changing the label of what it sends.
//
// It runs before chi has matched a route, so there is no pattern to read and
// the prefix is matched by hand. TestOnlyTheUploadRoutesCarryTheUploadBound
// checks this against the registered routes, so a new upload route missing
// here fails that test.
func carriesAFile(r *http.Request) bool {
	if carriesAnUpgrade(r) {
		return true
	}

	// The prefix has to end a segment. Without the separator every path that
	// only begins with those bytes would take the upload bound.
	if !strings.HasPrefix(r.URL.Path, storagesPathPrefix+"/") {
		return false
	}

	switch r.Method {
	case http.MethodPut:
		return true
	case http.MethodPost:
		// POST reaches several routes under this prefix and only the content
		// route carries a file. The others take a small JSON document and must
		// keep the document bound, so the content route is matched by its last
		// segment.
		return strings.HasSuffix(r.URL.Path, "/content")
	default:
		return false
	}
}

// carriesAnUpgrade reports whether this request's body is an upgrade bundle,
// which takes its own bound and not the storage one.
func carriesAnUpgrade(r *http.Request) bool {
	return r.Method == http.MethodPost && (r.URL.Path == upgradeStaged || r.URL.Path == upgradeBundle)
}

// principalMiddleware runs for every registered operation. It is a Huma
// middleware and not a router one, because the document and the reference are
// not operations and are not gated by identity.
func principalMiddleware(
	api huma.API,
	principals Principals,
	logger *slog.Logger,
) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if isPublic(ctx.Operation()) {
			next(ctx)

			return
		}

		request, _ := humachi.Unwrap(ctx)

		caller, err := principals.Caller(request)
		if err != nil {
			refusal := identityFault(err)

			logRefusal(ctx.Context(), logger, ctx.URL().Path, err)

			// The response is the last thing this request does, so a failure to
			// write it cannot be reported to the client.
			_ = huma.WriteErr(api, ctx, refusal.Status(), "", refusal)

			return
		}

		next(huma.WithContext(ctx, withCaller(ctx.Context(), caller)))
	}
}

// ErrNoCredential marks a refusal of a request that presented no credential
// at all. A Principals implementation wraps it into the refusal it returns. A
// browser asks who it is before anybody signs in, so such a refusal is logged
// at debug, and a credential that was presented and refused is logged as a
// warning.
var ErrNoCredential = errors.New("no credential presented")

// logRefusal logs why a request's identity was refused, at the level the
// refusal deserves.
func logRefusal(ctx context.Context, logger *slog.Logger, path string, err error) {
	level := slog.LevelWarn
	if errors.Is(err, ErrNoCredential) {
		level = slog.LevelDebug
	}

	logger.Log(ctx, level, "identity refused",
		slog.String("trace_id", traceIDFrom(ctx)),
		slog.String("path", path),
		slog.Any("error", err))
}

// isPublic reports whether the operation declares OpenAPI's empty security
// requirement. The published document then says which operations need no
// identity, and there is no second list beside the router to keep in step.
func isPublic(op *huma.Operation) bool {
	return op != nil && op.Security != nil && len(op.Security) == 0
}

// identityFault keeps a Fault raised by the Principals implementation, and
// turns anything else into an invalid token so an implementation's internal
// error text cannot reach a client.
func identityFault(err error) *fault.Fault {
	if f, ok := errors.AsType[*fault.Fault](err); ok {
		return f
	}

	return fault.AuthTokenInvalid.New("the request carries no usable identity")
}

// servedMiddleware records one served request against the seam Config.Served
// carries. A nil seam is a deployment that measures nothing, and it costs the
// request an interface comparison.
//
// The route is chi's template and never the path. A path carries printer ids
// and file names, so a series per path would be a series per printer per file,
// and the metric catalog avoids that cardinality. A request that matched no
// route is recorded under unroutedTemplate, so an operator sees a client
// calling unpublished paths as one series.
func servedMiddleware(served func(method, route string, status int, took time.Duration)) func(
	http.Handler,
) http.Handler {
	if served == nil {
		return func(next http.Handler) http.Handler { return next }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			counted := &statusWriter{ResponseWriter: w, status: http.StatusOK}

			// The template is read after the handler, because chi fills it in as
			// it routes. Reading it first gives the empty string on every
			// request.
			//
			// A hijacked request is not recorded. The device wire upgrades to a
			// websocket and holds it for as long as the printer is switched on,
			// and a session measured as a request would put hours into a
			// histogram whose buckets stop at ten seconds.
			defer func() {
				if counted.hijacked {
					return
				}

				served(r.Method, routeTemplate(r), counted.status, time.Since(started))
			}()

			next.ServeHTTP(counted, r)
		})
	}
}

// unroutedTemplate is the route label for a request that matched nothing.
const unroutedTemplate = "unrouted"

// routeTemplate is the pattern a request matched, as chi recorded it.
func routeTemplate(r *http.Request) string {
	routed := chi.RouteContext(r.Context())
	if routed == nil {
		return unroutedTemplate
	}

	if pattern := routed.RoutePattern(); pattern != "" {
		return pattern
	}

	return unroutedTemplate
}

// statusWriter remembers the status code a handler wrote, which
// http.ResponseWriter does not expose.
//
// It forwards Flush and Unwrap. The event stream flushes every frame, and
// http.ResponseController reaches the real writer through Unwrap, so a wrapper
// that carried neither would turn a live stream into a buffered response.
type statusWriter struct {
	http.ResponseWriter

	status   int
	written  bool
	hijacked bool
}

func (w *statusWriter) WriteHeader(status int) {
	if !w.written {
		w.status = status
		w.written = true
	}

	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	w.written = true

	return w.ResponseWriter.Write(b)
}

// Unwrap is what http.ResponseController follows to reach the real writer, so
// a handler that sets a write deadline or hijacks still can.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Flush forwards to the writer underneath when it can. The event stream writes
// a frame and flushes it, and a wrapper that swallowed this would hold every
// frame until the response ended.
func (w *statusWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// ErrNotHijackable is a handler asking for the connection under a writer that
// cannot give it up.
var ErrNotHijackable = errors.New("the response writer cannot be hijacked")

// errPanicked labels the recovered value inside the fault, so the request's
// failure line names it as a panic and not as any other unexpected failure.
var errPanicked = errors.New("panic")

// Hijack hands the connection to a handler that wants it.
//
// gorilla/websocket asserts http.Hijacker on the writer it is given and answers
// 500 when the assertion fails, so a wrapper without this method turns every
// printer's upgrade into an error.
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, ErrNotHijackable
	}

	conn, buffered, err := hijacker.Hijack()
	if err != nil {
		return nil, nil, fmt.Errorf("hijack the connection: %w", err)
	}

	w.hijacked = true

	return conn, buffered, nil
}
