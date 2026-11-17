package httpapi

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// A client that goes away cancels the work it asked for. A client that stays
// has no bound of its own: the handler waits on a printer, a module or the
// database for as long as that takes. The budget ends that wait.

// budgetKey is the metadata member an operation declares its budget class
// under. Huma carries Metadata on the registered operation and keeps it out of
// the document. How long a route may take belongs to this deployment and not
// to the published contract.
const budgetKey = "budget"

// budgetClass is which of the two configured budgets a route takes. An
// operation that declares none takes the request budget, so a route gets the
// longer one only where its registration says so.
type budgetClass string

// budgetStream is the class of a route that holds its answer open: a stream a
// reader follows until it stops reading, and a file that moves while the caller
// waits. Each one keeps a window per chunk, and that window notices a peer
// that stopped. The longer budget is only a ceiling over the whole response.
const budgetStream budgetClass = "stream"

// longRunningExtension marks an operation whose answer waits on a machine, so a
// generated client leaves its own deadline off it. `register` writes it for
// every operation that takes the stream budget.
const longRunningExtension = "x-long-running"

// streams declares the stream budget for one operation. It is a function so
// that every route asking for the longer budget says it the same way and a
// reader can find them all.
func streams() map[string]any { return map[string]any{budgetKey: budgetStream} }

// budgets is how long each route may take, keyed by the method and the path
// template, as "GET /api/v1/health".
//
// A request that matches no operation is absent from the table and takes no
// budget. The paths this server serves and does not publish are a printer's
// websocket, a module's own surface and the frontend, and each of those must
// be able to outlive any request budget.
type budgets struct {
	// byRoute is written once, after every operation is registered, and read by
	// every request afterwards. It is an atomic pointer because the write and
	// the reads happen on different goroutines.
	byRoute atomic.Pointer[map[string]time.Duration]
}

// newBudgets returns an empty table. It is built before the operations are
// registered, because the middleware that reads it goes on the router first.
func newBudgets() *budgets {
	table := &budgets{}
	table.set(map[string]time.Duration{})

	return table
}

func (b *budgets) set(table map[string]time.Duration) { b.byRoute.Store(&table) }

// forRoute is the budget for one method and path template, and whether there is
// one at all.
func (b *budgets) forRoute(key string) (time.Duration, bool) {
	table := b.byRoute.Load()
	if table == nil {
		return 0, false
	}

	budget, found := (*table)[key]

	return budget, found
}

// readFrom fills the table from the registered operations. request is what a
// route takes unless its registration declared the stream class, and stream is
// what one that did takes.
func (b *budgets) readFrom(api huma.API, request, stream time.Duration) {
	table := make(map[string]time.Duration)

	for path, item := range api.OpenAPI().Paths {
		for method, op := range operationsOfPath(item) {
			if op == nil {
				continue
			}

			budget := request
			if class, _ := op.Metadata[budgetKey].(budgetClass); class == budgetStream {
				budget = stream
			}

			table[method+" "+path] = budget
		}
	}

	b.set(table)
}

// budgetMiddleware gives every request that reaches an operation a deadline,
// and answers the caller when the deadline arrives first.
//
// It matches the route itself and does not read what chi matched, because chi
// fills the pattern in as it routes and a middleware runs before that. The
// lookup costs one walk of the routing tree per request. A deadline cannot be
// lengthened once it is set, so the per-route budget has to be known before
// the context is derived.
//
// The refusal is written after the handler returns and only when the handler
// wrote nothing. A handler here honours its context, so it returns as soon as
// the deadline fires. A handler that ignored the context could be interrupted
// only by buffering the whole response, and that would turn every stream into
// a download.
func budgetMiddleware(routes *chi.Mux, table *budgets) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			budget, bounded := table.forRoute(r.Method + " " + matchedRoute(routes, r))
			if !bounded {
				next.ServeHTTP(w, r)

				return
			}

			ctx, cancel := context.WithTimeout(withBudget(r.Context(), budget), budget)
			defer cancel()

			// statusWriter forwards the three methods a route with a budget
			// needs. The event stream flushes every frame. A transfer moves the
			// connection's deadlines through Unwrap. A hijacked connection
			// must not be answered afterwards.
			guard := &statusWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(guard, r.WithContext(ctx))

			if guard.written || guard.hijacked {
				return
			}

			if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return
			}

			writeProblem(w, r, httpfault.RequestTimedOut(budget))
		})
	}
}

// matchedRoute is the pattern this request will match, empty when it matches
// nothing. chi is asked with a route context of its own, because Find writes
// into the one it is given and the request's must reach the router untouched.
func matchedRoute(routes *chi.Mux, r *http.Request) string {
	return routes.Find(chi.NewRouteContext(), r.Method, routingPath(r))
}

// routingPath is the path chi will route on: the escaped form where the URL has
// one, which is what chi's own router reads.
func routingPath(r *http.Request) string {
	if r.URL.RawPath != "" {
		return r.URL.RawPath
	}

	return r.URL.Path
}

// budgetContextKey is the context key the budget is carried under, so a
// refusal built anywhere below can name the number the caller ran out of.
type budgetContextKey struct{}

func withBudget(ctx context.Context, budget time.Duration) context.Context {
	return context.WithValue(ctx, budgetContextKey{}, budget)
}

// budgetFrom returns the budget the middleware set, zero when the request did
// not pass through it.
func budgetFrom(ctx context.Context) time.Duration {
	budget, _ := ctx.Value(budgetContextKey{}).(time.Duration)

	return budget
}
