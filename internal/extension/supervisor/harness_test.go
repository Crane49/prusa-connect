package supervisor_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// target is the contract these tests supervise. It is not an interface, because
// Instance[T] is generic over anything, and the real contracts are covered in
// internal/extension/supervised.
type target struct {
	do func(ctx context.Context) error
}

// call is the one operation, wrapped the way a contract adapter wraps one.
func call(ctx context.Context, inst *supervisor.Instance[*target], op string) error {
	return supervisor.CallVoid(ctx, inst, op, func(callCtx context.Context, t *target) error {
		return t.do(callCtx)
	})
}

func testRef(t *testing.T, id string) ref.Ref {
	t.Helper()

	r, err := ref.New(ref.KindStorage, id)
	require.NoError(t, err)

	return r
}

// fixedRand pins the jitter so a ladder can be asserted at its bounds instead of
// within a range. Full jitter draws over [0,ceiling), and 1 is its supremum.
func fixedRand() func() float64 { return func() float64 { return 1 } }

// testPolicy is the default policy with the jitter pinned and the startup
// spread removed, so a test measures the ladder and not the spread.
func testPolicy() supervisor.Policy {
	policy := supervisor.DefaultPolicy()
	policy.Backoff.Spread = 0
	policy.Backoff.Rand = fixedRand()

	return policy
}

// starts returns a Start that returns the same working target every time.
func starts(do func(context.Context) error) func(context.Context, supervisor.Handle) (*target, error) {
	return func(context.Context, supervisor.Handle) (*target, error) {
		return &target{do: do}, nil
	}
}

// ok is a resource operation that always succeeds.
func ok(context.Context) error { return nil }

// metricSink records what the wrapper measured, so a test can check which
// series moved.
type metricSink struct {
	mu       sync.Mutex
	observed map[string][]float64
	counted  map[string]float64
}

func newMetricSink() *metricSink {
	return &metricSink{observed: map[string][]float64{}, counted: map[string]float64{}}
}

func (s *metricSink) binder() supervisor.Binder {
	return func(r ref.Ref) supervisor.Metrics {
		return metricsView{sink: s, labels: "resource=" + r.ID()}
	}
}

func (s *metricSink) observations(key string) []float64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]float64(nil), s.observed[key]...)
}

func (s *metricSink) count(key string) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.counted[key]
}

type metricsView struct {
	sink   *metricSink
	labels string
}

func (v metricsView) Counter(name string) supervisor.Counter {
	return metricHandle{sink: v.sink, key: name + "{" + v.labels + "}"}
}

func (v metricsView) Histogram(name string) supervisor.Observer {
	return metricHandle{sink: v.sink, key: name + "{" + v.labels + "}"}
}

func (v metricsView) With(key, value string) supervisor.Metrics {
	return metricsView{sink: v.sink, labels: v.labels + "," + key + "=" + value}
}

type metricHandle struct {
	sink *metricSink
	key  string
}

func (h metricHandle) Add(delta float64) {
	h.sink.mu.Lock()
	defer h.sink.mu.Unlock()

	h.sink.counted[h.key] += delta
}

func (h metricHandle) Observe(value float64) {
	h.sink.mu.Lock()
	defer h.sink.mu.Unlock()

	h.sink.observed[h.key] = append(h.sink.observed[h.key], value)
}

// recorder collects values written on the supervision goroutine and read on the
// test goroutine. synctest.Wait parks the writer in practice, but the memory
// model does not promise it, and a test that relies on that fails somewhere
// else.
type recorder[V any] struct {
	mu     sync.Mutex
	values []V
}

func (r *recorder[V]) add(v V) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.values = append(r.values, v)
}

func (r *recorder[V]) all() []V {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]V(nil), r.values...)
}

// harness runs one instance's supervision loop and records every status it
// publishes, so a test can assert on the transitions and not on whichever state
// it happens to see.
type harness struct {
	inst *supervisor.Instance[*target]

	mu       sync.Mutex
	states   []string
	statuses []supervisor.Status

	cancel context.CancelFunc
	done   chan struct{}
}

// newHarness constructs the instance without running it.
func newHarness(t *testing.T, cfg supervisor.Config[*target]) *harness {
	t.Helper()

	h := &harness{done: make(chan struct{})}

	inner := cfg.OnStatus
	cfg.OnStatus = func(s supervisor.Status) {
		h.mu.Lock()
		h.states = append(h.states, s.State.String())
		h.statuses = append(h.statuses, s)
		h.mu.Unlock()

		if inner != nil {
			inner(s)
		}
	}

	inst, err := supervisor.New(cfg)
	require.NoError(t, err)

	h.inst = inst

	return h
}

// bubbleContext returns a context created inside the synctest bubble. A context
// from outside it is not bubbled, so a goroutine blocked on its Done channel
// never counts as durably blocked and the fake clock never advances.
func bubbleContext() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

// run starts the supervision loop and stops it when the test ends.
func (h *harness) run(t *testing.T) {
	t.Helper()

	runCtx, cancel := bubbleContext()
	h.cancel = cancel

	go func() {
		defer close(h.done)

		_ = h.inst.Run(runCtx)
	}()
}

// stop ends supervision and waits for the loop, so no goroutine outlives the
// synctest bubble.
func (h *harness) stop() {
	if h.cancel == nil {
		return
	}

	h.cancel()
	<-h.done
}

// seen returns every state published so far, in order.
func (h *harness) seen() []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	return append([]string(nil), h.states...)
}

// state returns the current derived state.
func (h *harness) state() string { return h.inst.State().String() }
