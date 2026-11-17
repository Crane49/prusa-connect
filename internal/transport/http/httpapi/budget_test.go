package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// budgetedRouter is a router carrying the budget middleware and one route with
// the given budget, serving handler. It is the middleware on its own: what the
// production server puts in the table is the subject of the test below it.
func budgetedRouter(budget time.Duration, pattern string, handler http.HandlerFunc) *chi.Mux {
	mux := chi.NewRouter()
	table := newBudgets()

	mux.Use(budgetMiddleware(mux, table))
	mux.Get(pattern, handler)

	table.set(map[string]time.Duration{http.MethodGet + " " + pattern: budget})

	return mux
}

// TestAHandlerThatOutlivesItsBudgetIsRefused is the whole of what this
// middleware is for. A client that goes away already stops the work; before
// this, a client that stayed waited for as long as the handler took.
func TestAHandlerThatOutlivesItsBudgetIsRefused(t *testing.T) {
	t.Parallel()

	const budget = 20 * time.Millisecond

	// A handler that waits on the context and answers nothing, which is every
	// handler here waiting on a printer, a module or the database.
	mux := budgetedRouter(budget, "/api/v1/slow", func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	answer, err := server.Client().Get(server.URL + "/api/v1/slow")
	require.NoError(t, err)

	defer func() { _ = answer.Body.Close() }()

	require.Equal(t, http.StatusGatewayTimeout, answer.StatusCode)
	require.Equal(t, problemMediaType, answer.Header.Get("Content-Type"))

	var problem struct {
		Code   string         `json:"code"`
		Params map[string]any `json:"params"`
	}

	require.NoError(t, json.NewDecoder(answer.Body).Decode(&problem))
	require.Equal(t, httpfault.CodeRequestTimedOut().String(), problem.Code)
	require.InDelta(t, float64(budget.Milliseconds()), problem.Params["timeout_ms"], 0,
		"the refusal names no budget, so the caller cannot tell what it ran out of")
}

// TestAHandlerUnderItsBudgetIsUntouched is the other half. A budget that
// answered for a handler that finished in time would be a server refusing its
// own successful requests.
func TestAHandlerUnderItsBudgetIsUntouched(t *testing.T) {
	t.Parallel()

	mux := budgetedRouter(time.Minute, "/api/v1/quick", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("done"))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	answer, err := server.Client().Get(server.URL + "/api/v1/quick")
	require.NoError(t, err)

	defer func() { _ = answer.Body.Close() }()

	require.Equal(t, http.StatusTeapot, answer.StatusCode)
}

// TestARouteThatPublishesNoOperationTakesNoBudget is why the table is a table
// rather than one number. A printer's websocket, a module's own surface and the
// frontend are served here and published nowhere, and every one of them is
// meant to outlive any request budget.
func TestARouteThatPublishesNoOperationTakesNoBudget(t *testing.T) {
	t.Parallel()

	mux := chi.NewRouter()
	table := newBudgets()

	mux.Use(budgetMiddleware(mux, table))

	var bounded bool

	mux.Get("/p/ws", func(w http.ResponseWriter, r *http.Request) {
		_, bounded = r.Context().Deadline()

		w.WriteHeader(http.StatusOK)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	answer, err := server.Client().Get(server.URL + "/p/ws")
	require.NoError(t, err)
	require.NoError(t, answer.Body.Close())

	require.False(t, bounded, "a mounted path was given a deadline, which would cut a printer's wire")
}

// TestTheStreamingRoutesCarryTheLongerBudget reads the table the production
// server built. A route that holds its answer open and forgot to declare it
// would be cut at the request budget, which is a stream that ends after thirty
// seconds and a 4 GiB upload nobody can finish.
func TestTheStreamingRoutesCarryTheLongerBudget(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	streaming := []string{
		http.MethodGet + " " + EventsPath,
		http.MethodGet + " " + cameraStreamPath,
		http.MethodPost + " " + deviceFilesPath,
		http.MethodPost + " " + devicePrintPath,
		http.MethodGet + " " + storageContentPath,
		http.MethodPut + " " + storageContentPath,
		http.MethodPost + " " + storageContentPath,
	}

	for _, route := range streaming {
		budget, found := server.spending.forRoute(route)
		require.True(t, found, "%s is published and carries no budget at all", route)
		require.Equal(t, defaultStreamBudget, budget,
			"%s holds its answer open and was given the request budget", route)
	}

	budget, found := server.spending.forRoute(http.MethodGet + " " + healthPath)
	require.True(t, found)
	require.Equal(t, defaultRequestBudget, budget,
		"a route that answers at once was given the streaming budget")
}
