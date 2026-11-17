package webhook_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/adapters/events/webhook"
	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
)

// received collects what the endpoint saw, so a test asserts on the wire shape
// rather than on the adapter's internals.
type received struct {
	mu     sync.Mutex
	bodies []map[string]any
	header http.Header
}

func (r *received) add(req *http.Request) {
	body, _ := io.ReadAll(req.Body)

	var decoded map[string]any
	_ = json.Unmarshal(body, &decoded)

	r.mu.Lock()
	defer r.mu.Unlock()

	r.bodies = append(r.bodies, decoded)
	r.header = req.Header.Clone()
}

func (r *received) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.bodies)
}

func (r *received) all() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]map[string]any, len(r.bodies))
	copy(out, r.bodies)

	return out
}

func newProcessor(t *testing.T, cfg map[string]any) eventsapi.Processor {
	t.Helper()

	p, err := webhook.New(eventsapi.ProviderSpec{Type: webhook.Type, Config: cfg})
	require.NoError(t, err)

	return p
}

func TestTheFactoryIsRegisteredUnderItsType(t *testing.T) {
	t.Parallel()

	// The whole extension mechanism is an init() calling Register. If this
	// fails, the adapter is invisible to configuration no matter how well the
	// rest of it works.
	for _, descriptor := range eventsapi.Descriptors() {
		if descriptor.Name() == webhook.Type {
			return
		}
	}

	t.Fatalf("%q is not in the gathering list this build drains", webhook.Type)
}

func TestASpecWithNoURLIsRejected(t *testing.T) {
	t.Parallel()

	_, err := webhook.New(eventsapi.ProviderSpec{Type: webhook.Type, Config: map[string]any{}})
	require.ErrorIs(t, err, webhook.ErrMissingURL)
}

func TestEventsReachTheEndpointInOrder(t *testing.T) {
	t.Parallel()

	got := &received{}
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got.add(r)
	}))
	defer server.Close()

	p := newProcessor(t, map[string]any{"url": server.URL})

	p.PrinterStateChanged(eventsapi.PrinterState{ID: "one", State: "PRINTING"})
	p.JobUpdated(eventsapi.JobInfo{ID: "job", PrinterID: "one", State: "RUNNING"})
	p.PrinterDeleted("one")

	require.NoError(t, p.Close())

	bodies := got.all()
	require.Len(t, bodies, 3)
	assert.Equal(t, "printer.state_changed", bodies[0]["kind"])
	assert.Equal(t, "job.updated", bodies[1]["kind"])
	assert.Equal(t, "printer.deleted", bodies[2]["kind"])
}

func TestConfiguredHeadersAreSent(t *testing.T) {
	t.Parallel()

	got := &received{}
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got.add(r)
	}))
	defer server.Close()

	p := newProcessor(t, map[string]any{
		"url":    server.URL,
		"header": map[string]any{"Authorization": "Bearer secret"},
	})

	p.PrinterDeleted("one")
	require.NoError(t, p.Close())

	require.Equal(t, 1, got.len())
	assert.Equal(t, "Bearer secret", got.header.Get("Authorization"))
	assert.Equal(t, "application/json", got.header.Get("Content-Type"))
}

// A sink must degrade by dropping. This is the property that keeps a dead
// endpoint from stalling the event bus, and it is the one most likely to be
// broken by somebody copying this adapter and "improving" the queue.
func TestAStalledEndpointNeverBlocksTheCaller(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		<-release
	}))

	defer func() {
		close(release)
		server.Close()
	}()

	p := newProcessor(t, map[string]any{"url": server.URL, "queue_depth": 1})

	// Far more than the queue holds, against an endpoint that never answers.
	done := make(chan struct{})

	go func() {
		defer close(done)

		for range 500 {
			p.PrinterDeleted("one")
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("offering events blocked on a stalled endpoint")
	}
}

func TestCloseReportsWhatWasDropped(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		<-release
	}))

	defer server.Close()

	p := newProcessor(t, map[string]any{"url": server.URL, "queue_depth": 1})

	for range 100 {
		p.PrinterDeleted("one")
	}

	close(release)

	err := p.Close()
	require.Error(t, err, "dropping events silently is the failure this reports")
	assert.Contains(t, err.Error(), "dropped")
}

func TestANonSuccessStatusIsReportedAtClose(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := newProcessor(t, map[string]any{"url": server.URL})

	p.PrinterDeleted("one")

	err := p.Close()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed")
}

func TestCloseIsSafeToCallTwice(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()

	p := newProcessor(t, map[string]any{"url": server.URL})

	require.NoError(t, p.Close())

	// A host shutting down after an error can reach Close on a path that
	// already ran. Panicking there turns one failure into two.
	assert.NotPanics(t, func() { _ = p.Close() })
}

func TestOfferingAfterCloseDoesNotPanic(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()

	p := newProcessor(t, map[string]any{"url": server.URL})
	require.NoError(t, p.Close())

	assert.NotPanics(t, func() {
		p.PrinterStateChanged(eventsapi.PrinterState{ID: "one", State: "IDLE"})
	})
}

func TestQueueDepthIsReadFromEitherNumberKind(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	t.Cleanup(server.Close)

	// YAML decodes a number as int or float64 depending on the decoder, and an
	// operator should not have to know which.
	for name, depth := range map[string]any{"int": 4, "float64": float64(4)} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p, err := webhook.New(eventsapi.ProviderSpec{
				Type:   webhook.Type,
				Config: map[string]any{"url": server.URL, "queue_depth": depth},
			})
			require.NoError(t, err)
			require.NoError(t, p.Close())
		})
	}
}

func TestAnUnusableQueueDepthIsRejected(t *testing.T) {
	t.Parallel()

	_, err := webhook.New(eventsapi.ProviderSpec{
		Type:   webhook.Type,
		Config: map[string]any{"url": "http://example.invalid", "queue_depth": 0},
	})
	require.Error(t, err)
}
