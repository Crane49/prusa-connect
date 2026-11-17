package supervisor

import (
	"maps"
	"sync"
	"sync/atomic"

	"github.com/prusa3d/prusa-connect/internal/extension/host"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// The series every supervised instance records, whatever contract it serves.
// The names are the same across contracts, so one dashboard works for storage,
// sinks and anything added later.
const (
	MetricCalls    = "resource_calls_total"
	MetricDuration = "resource_call_duration_seconds"
	// MetricRejected counts calls refused before they reached the resource. It
	// is separate from MetricCalls because a refusal has no latency, and
	// putting one in the duration histogram would drag the high percentiles
	// towards zero at the moment the resource stops answering.
	MetricRejected = "resource_rejected_total"
	MetricDropped  = "resource_dropped_total"
	MetricPanics   = "resource_panics_total"
	MetricStarts   = "resource_starts_total"
	MetricBreaker  = "resource_breaker_transitions_total"
	MetricStates   = "resource_state_transitions_total"
	// MetricStale counts status reports discarded because the construction
	// that sent them is gone.
	MetricStale = "resource_stale_reports_total"
	// MetricWedged counts resources that did not return within a budget the host
	// could not enforce, and were abandoned.
	MetricWedged = "resource_wedged_total"
)

// The metric handle types come from the host package, so a resource that counts
// something of its own imports that package and not the supervision plane.
type (
	// Counter is a monotonically increasing series.
	Counter = host.Counter
	// Observer records a distribution.
	Observer = host.Observer
	// Metrics gives metric handles already labelled with their instance.
	Metrics = host.Metrics
	// Reporter is how a resource reports its own runtime status.
	Reporter = host.Reporter
	// Handle is what a resource receives from its host.
	Handle = host.Handle
)

// Binder returns the metric handles for one instance. A host implements it to
// connect the supervisor to client_golang. Nil means the instance records
// nothing.
type Binder func(r ref.Ref) Metrics

// NopMetrics returns metric handles that record nothing, for a host with no
// metrics registry and for tests that are not about metrics.
func NopMetrics() Metrics { return host.NopMetrics() }

// instanceMetrics caches the handles the supervisor uses on every call, so a
// call reads a field instead of looking up a map. The per-op children are
// cached copy-on-write, because one contract has a small, fixed set of
// operations and they are all seen after a few calls.
type instanceMetrics struct {
	base Metrics

	starts  Counter
	dropped Counter
	panics  Counter
	breaker Counter
	states  Counter
	stale   Counter
	wedged  Counter

	mu  sync.Mutex
	ops atomic.Pointer[map[string]*opMetrics]
}

func newInstanceMetrics(m Metrics) *instanceMetrics {
	if m == nil {
		m = NopMetrics()
	}

	return &instanceMetrics{
		base:    m,
		starts:  m.Counter(MetricStarts),
		dropped: m.Counter(MetricDropped),
		panics:  m.Counter(MetricPanics),
		breaker: m.Counter(MetricBreaker),
		states:  m.Counter(MetricStates),
		stale:   m.Counter(MetricStale),
		wedged:  m.Counter(MetricWedged),
	}
}

// op returns the cached children for one operation name.
func (im *instanceMetrics) op(name string) *opMetrics {
	if current := im.ops.Load(); current != nil {
		if found, ok := (*current)[name]; ok {
			return found
		}
	}

	im.mu.Lock()
	defer im.mu.Unlock()

	current := im.ops.Load()
	if current != nil {
		if found, ok := (*current)[name]; ok {
			return found
		}
	}

	child := im.base.With("op", name)
	built := &opMetrics{
		duration: child.Histogram(MetricDuration),
		rejected: child.Counter(MetricRejected),
		outcomes: map[Class]Counter{},
	}

	for _, class := range []Class{ClassSuccess, ClassCancelled, ClassClient, ClassRecoverable, ClassPermanent} {
		built.outcomes[class] = child.With("outcome", class.String()).Counter(MetricCalls)
	}

	next := map[string]*opMetrics{name: built}
	if current != nil {
		maps.Copy(next, *current)
		next[name] = built
	}

	im.ops.Store(&next)

	return built
}

// opMetrics are the handles for one operation of one instance.
type opMetrics struct {
	duration Observer
	rejected Counter
	outcomes map[Class]Counter
}

// observe records one call that reached the resource.
func (om *opMetrics) observe(class Class, seconds float64) {
	om.duration.Observe(seconds)

	if counter, ok := om.outcomes[class]; ok {
		counter.Add(1)
	}
}

// reject records one call the host refused. It records no duration. A refusal
// took no time in the resource, and recording it as a fast call would make a
// latency panel look healthiest at the moment it is most wrong.
func (om *opMetrics) reject() { om.rejected.Add(1) }
