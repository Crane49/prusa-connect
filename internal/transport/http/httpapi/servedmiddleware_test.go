package httpapi

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

// record is one call the timing seam was given.
type record struct {
	method string
	route  string
	status int
	took   time.Duration
}

// TestTheTimingMiddlewareRecordsTheTemplateAndNotThePath is the whole reason
// the route label exists. A path carries printer ids and file names, so a series
// per path is a series per printer per file, which is the cardinality the metric
// catalog is built to avoid.
func TestTheTimingMiddlewareRecordsTheTemplateAndNotThePath(t *testing.T) {
	t.Parallel()

	var got []record

	mux := chi.NewRouter()
	mux.Use(servedMiddleware(func(method, route string, status int, took time.Duration) {
		got = append(got, record{method: method, route: route, status: status, took: took})
	}))

	mux.Get("/api/v1/devices/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	answer, err := server.Client().Get(server.URL + "/api/v1/devices/p0fa0311c075316e0f244094")
	require.NoError(t, err)
	require.NoError(t, answer.Body.Close())

	require.Len(t, got, 1)
	require.Equal(t, http.MethodGet, got[0].method)
	require.Equal(t, "/api/v1/devices/{id}", got[0].route,
		"the path was recorded rather than the template, which is one series per printer")
	require.Equal(t, http.StatusTeapot, got[0].status)
}

// TestARequestThatMatchedNoRouteIsRecordedUnderOneLabel keeps a 404 sweep from
// being one series per path a client guessed at.
func TestARequestThatMatchedNoRouteIsRecordedUnderOneLabel(t *testing.T) {
	t.Parallel()

	var got []record

	mux := chi.NewRouter()
	mux.Use(servedMiddleware(func(method, route string, status int, took time.Duration) {
		got = append(got, record{method: method, route: route, status: status, took: took})
	}))

	// One route, so the router builds its chain. A chi mux with no route at all
	// serves its NotFound handler without the middleware stack.
	mux.Get("/api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	answer, err := server.Client().Get(server.URL + "/no/such/path")
	require.NoError(t, err)
	require.NoError(t, answer.Body.Close())

	require.Len(t, got, 1)
	require.Equal(t, unroutedTemplate, got[0].route)
	require.Equal(t, http.StatusNotFound, got[0].status)
}

// TestTheTimingMiddlewareLetsAHandlerHijackTheConnection is the defect this
// wrapper introduced and this test refuses.
//
// gorilla/websocket asserts http.Hijacker on the writer it is handed and answers
// 500 when the assertion fails. The device wire upgrades that way, so a wrapper
// with no Hijack method stops every printer connecting, and the only symptom is
// an empty fleet.
//
// A hijacked request is also not recorded: the session lasts as long as the
// printer is switched on, and one measured as a request puts hours into a
// histogram whose buckets stop at ten seconds.
func TestTheTimingMiddlewareLetsAHandlerHijackTheConnection(t *testing.T) {
	t.Parallel()

	var (
		got       []record
		hijackErr error
	)

	mux := chi.NewRouter()
	mux.Use(servedMiddleware(func(method, route string, status int, took time.Duration) {
		got = append(got, record{method: method, route: route, status: status, took: took})
	}))

	taken := make(chan struct{})

	mux.Get("/p/ws", func(w http.ResponseWriter, _ *http.Request) {
		defer close(taken)

		hijacker, ok := w.(http.Hijacker)
		if !ok {
			hijackErr = errors.New("the handler was given a writer that is not an http.Hijacker")

			return
		}

		conn, _, err := hijacker.Hijack()
		if err != nil {
			hijackErr = err

			return
		}

		_, _ = conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\n\r\n"))
		_ = conn.Close()
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	conn, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
	require.NoError(t, err)

	defer func() { _ = conn.Close() }()

	_, err = conn.Write([]byte("GET /p/ws HTTP/1.1\r\nHost: x\r\n\r\n"))
	require.NoError(t, err)

	line, err := bufio.NewReader(conn).ReadString('\n')
	require.NoError(t, err)
	require.Contains(t, line, "101 Switching Protocols")

	<-taken

	require.NoError(t, hijackErr)
	require.Empty(t, got, "a hijacked connection was recorded as one request that took its whole session")
}
