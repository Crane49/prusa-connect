package app

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"sync"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/platform/observability"
	"github.com/prusa3d/prusa-connect/internal/platform/observability/prom"
)

// The label keys the supervision plane's own handles are bound under. They are
// the catalog's names for the two dimensions every supervised instance has, and
// they are constants because the binder writes them and the catalog reads them.
const (
	labelKind      = "kind"
	labelResource  = "resource"
	labelOperation = "operation"
	labelOutcome   = "outcome"
)

// The two label keys the supervisor itself adds through Metrics.With. They are
// its spelling and not the catalog's, so this file translates them.
const (
	supervisorOpLabel      = "op"
	supervisorOutcomeLabel = "outcome"
)

// observed is the process's metric registry and the handles the composition
// root binds out of it.
//
// It is built before the logger, because the logger counts its own lines into
// the registry, and before the supervision plane, because every instance the
// plane starts is bound to it as it starts.
type observed struct {
	registry *prom.Registry
	log      *slog.Logger
	// fleet is the device plane's handles, resolved once. They are on the hot
	// path: one event is published per state patch, which is fifteen per frame
	// per printer. Resolving a handle per call would cost a label hash and two
	// slice allocations there.
	fleet fleetMeters
}

// newObserved registers the catalog. One bad definition refuses the whole set,
// because a process that started with half its metrics would report a healthy
// fleet it is not measuring.
//
// A build without the observability tag links no Prometheus client. The
// definitions are still validated here and the handles are still handed out,
// so a wrong definition fails in both builds.
func newObserved() (*observed, error) {
	registry, err := prom.New(prom.Options{}, observability.Catalog()...)
	if err != nil {
		return nil, fmt.Errorf("register the metric catalog: %w", err)
	}

	o := &observed{registry: registry}
	o.fleet = o.newFleetMeters()

	return o, nil
}

// requireBuiltParts refuses a run whose file asks for an optional part this
// binary does not carry. `make build` derives the compiler's tags from the same
// keys, so the two agree when the binary was built from this file. They
// disagree when a prebuilt binary is pointed at a file written for another one.
//
// The refusal comes at start. Without it, a file that asks for metrics on a
// binary with no client would look right until the first scrape answered 404,
// and the operator would read that as the server being down.
func requireBuiltParts(core config.Core) error {
	if core.Observability.On() && !prom.Compiled {
		return fmt.Errorf("%w: core.observability.enabled is true and this binary links no "+
			"metrics client. Build it with the observability tag, which `make build` adds "+
			"from that same key", config.ErrInvalid)
	}

	return nil
}

// attach gives the registry the logger it reports a refused handle on. It is
// separate from construction because the logger is built from the registry:
// the counting handler needs the counters.
func (o *observed) attach(logger *slog.Logger) { o.log = logger }

// handler is the exposition, served at /metrics as an operation of the sealed
// router. A build with no metrics client answers 404 there.
func (o *observed) handler() http.Handler { return o.registry.Handler() }

// metricsOff is what a scrape reads while core.observability.enabled is false.
const metricsOff = "metrics are off: core.observability.enabled is false\n"

// served is the exposition when the file switched the metrics on, and a 404
// that names the key when it did not. The switch is read when the server
// starts, whatever the binary was built with, so a release binary that carries
// the client serves nothing until the file asks for it. The registry counts
// either way, because the log and the readiness report read the counters too.
func (o *observed) served(on bool) http.Handler {
	// A build with no client answers its own 404 whatever the key says,
	// because changing the key alone would not fix it.
	if on || !prom.Compiled {
		return o.handler()
	}

	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, metricsOff, http.StatusNotFound)
	})
}

// logging is what the counting slog handler is built from: one handle per level
// for each of the two series, resolved once.
//
// A level the registry will not answer for fails the start. The catalog
// declares both series with a level label, so this fails only when a
// definition changed.
func (o *observed) logging() (observability.LogConfig, error) {
	emitted, err := o.levelCounters(defOf("pcl_log_lines_total"))
	if err != nil {
		return observability.LogConfig{}, err
	}

	dropped, err := o.levelCounters(defOf("pcl_log_lines_dropped_total"))
	if err != nil {
		return observability.LogConfig{}, err
	}

	return observability.LogConfig{
		Emitted: func(level string) observability.Counter { return emitted[level] },
		Dropped: func(level string) observability.Counter { return dropped[level] },
	}, nil
}

// levelCounters resolves one handle per level up front, so nothing on the log
// path is a registry lookup.
func (o *observed) levelCounters(def observability.Def) (map[string]observability.Counter, error) {
	handles := map[string]observability.Counter{}

	for _, level := range observability.LogLevels() {
		handle, err := o.registry.Counter(def, level)
		if err != nil {
			return nil, fmt.Errorf("bind %s at level %s: %w", def.Name(), level, err)
		}

		handles[level] = handle
	}

	return handles, nil
}

// counter, gauge and observer resolve one catalog handle by name.
//
// A name the registry will not answer for records nothing and is logged at
// debug. A metric this process cannot write is a gap in a dashboard and not a
// reason to stop serving printers. The catalog is compiled in, so one of these
// fails only when a definition changed under its writer, and
// TestEveryCatalogFamilyHasAWriter catches that.
func (o *observed) counter(name string, values ...string) observability.Counter {
	if o == nil {
		return observability.NopCounter()
	}

	handle, err := o.registry.Counter(defOf(name), values...)
	if err != nil {
		o.report(name, err)

		return observability.NopCounter()
	}

	return handle
}

func (o *observed) gauge(name string, values ...string) observability.Gauge {
	if o == nil {
		return observability.NopGauge()
	}

	handle, err := o.registry.Gauge(defOf(name), values...)
	if err != nil {
		o.report(name, err)

		return observability.NopGauge()
	}

	return handle
}

func (o *observed) observer(name string, values ...string) observability.Observer {
	if o == nil {
		return observability.NopObserver()
	}

	handle, err := o.registry.Observer(defOf(name), values...)
	if err != nil {
		o.report(name, err)

		return observability.NopObserver()
	}

	return handle
}

// binder connects the supervision plane to the registry, so every supervised
// resource is measured the same way whoever wrote it. The host emits the
// series and the resource never does, so a community plugin cannot invent a
// name or label a series with the wrong instance.
func (o *observed) binder() supervisor.Binder {
	// The supervisor takes a nil binder for an instance nothing measures, so a
	// plane built with no registry needs no branch of its own.
	if o == nil {
		return nil
	}

	return func(instance ref.Ref) supervisor.Metrics {
		return planeMetrics{
			owner: o,
			labels: map[string]string{
				labelKind:     instance.Kind().String(),
				labelResource: instance.ID(),
			},
		}
	}
}

// planeMetrics is one instance's view of the registry. It translates the
// supervisor's own metric names, which are contract-shaped and carry no
// namespace, into the catalog's published series.
//
// The translation lives here. The supervisor is in the extension group and
// may not reach a registry. The catalog is a shared vocabulary and must not
// hold one caller's spellings.
type planeMetrics struct {
	owner  *observed
	labels map[string]string
}

var _ supervisor.Metrics = planeMetrics{}

// planeSeries maps each name the supervisor writes to the catalog series that
// publishes it. A name that is not here records nothing and is logged at
// debug, so a series the plane produces and the catalog does not carry is a
// gap somebody can see.
//
// Every name the supervisor writes is here. The breaker counter carries the
// same two dimensions as the others. Which way a breaker moved is read off the
// instance's own BreakerClosed condition, which the API and the event stream
// carry.
var planeSeries = map[string]string{
	supervisor.MetricCalls:    "pcl_resource_operations_total",
	supervisor.MetricDuration: "pcl_resource_operation_duration_seconds",
	supervisor.MetricRejected: "pcl_resource_rejected_total",
	supervisor.MetricStarts:   "pcl_resource_starts_total",
	supervisor.MetricDropped:  "pcl_resource_dropped_total",
	supervisor.MetricPanics:   "pcl_resource_panics_total",
	supervisor.MetricStates:   "pcl_resource_state_transitions_total",
	supervisor.MetricStale:    "pcl_resource_stale_reports_total",
	supervisor.MetricWedged:   "pcl_resource_wedged_total",
	supervisor.MetricBreaker:  "pcl_breaker_transitions_total",
}

// With returns a child carrying one more label, under the catalog's key for it.
// A key the catalog has no dimension for is carried and never read. Refusing
// it here would stop the process from starting when the supervisor adds a
// label.
func (m planeMetrics) With(key, value string) supervisor.Metrics {
	labels := make(map[string]string, len(m.labels)+1)
	maps.Copy(labels, m.labels)
	labels[catalogLabel(key)] = value

	return planeMetrics{owner: m.owner, labels: labels}
}

// Counter returns the handle for one label combination of a counter.
func (m planeMetrics) Counter(name string) supervisor.Counter {
	def, ok := m.definition(name, observability.KindCounter)
	if !ok {
		return observability.NopCounter()
	}

	handle, err := m.owner.registry.Counter(def, m.values(def)...)
	if err != nil {
		m.owner.report(name, err)

		return observability.NopCounter()
	}

	return handle
}

// Histogram returns the handle for one label combination of a distribution.
func (m planeMetrics) Histogram(name string) supervisor.Observer {
	def, ok := m.definition(name, observability.KindHistogram)
	if !ok {
		return observability.NopObserver()
	}

	handle, err := m.owner.registry.Observer(def, m.values(def)...)
	if err != nil {
		m.owner.report(name, err)

		return observability.NopObserver()
	}

	return handle
}

// definition resolves the catalog series behind one supervisor metric name.
func (m planeMetrics) definition(name string, kind observability.Kind) (observability.Def, bool) {
	published, mapped := planeSeries[name]
	if !mapped {
		m.owner.report(name, errNoCatalogSeries)

		return observability.Def{}, false
	}

	def := defOf(published)
	if def.Kind() != kind {
		m.owner.report(name, fmt.Errorf("%w: %s is a %s", errNoCatalogSeries, published, def.Kind()))

		return observability.Def{}, false
	}

	return def, true
}

// values orders this instance's labels the way the definition declares them. A
// dimension nothing supplied is the empty string, which Prometheus treats as
// an absent label.
func (m planeMetrics) values(def observability.Def) []string {
	names := def.LabelNames()
	values := make([]string, 0, len(names))

	for _, name := range names {
		values = append(values, m.labels[name])
	}

	return values
}

// report logs that a series the plane writes reaches no catalog metric. It is
// debug and not a warning: the gap comes from the build and not from the
// deployment, and an operator can do nothing about it.
func (o *observed) report(name string, err error) {
	if o == nil || o.log == nil {
		return
	}

	o.log.Debug("a supervision metric reaches no catalog series", "metric", name, "error", err.Error())
}

// catalogLabel translates the supervisor's label keys to the catalog's. Only
// the keys the supervisor adds are here. Any other key is carried under its
// own name and read by nothing.
func catalogLabel(key string) string {
	switch key {
	case supervisorOpLabel:
		return labelOperation
	case supervisorOutcomeLabel:
		return labelOutcome
	}

	return key
}

// defOf returns the catalog definition with this name. The catalog is a
// compiled-in table, so a name that is not in it is a programming error in this
// file. The zero Def fails Validate, and the registry then refuses the lookup.
//
// The table is built once, because Catalog builds three slices and every
// definition's label slice on each call. A lookup happens per published event,
// which is one per state patch. Scanning the catalog there was measured at
// 74.7 per cent of all allocations in a run with four hundred printers.
func defOf(name string) observability.Def { return catalogByName()[name] }

// catalogByName is the compiled-in catalog as a lookup, resolved on first use.
var catalogByName = sync.OnceValue(func() map[string]observability.Def {
	all := observability.Catalog()

	byName := make(map[string]observability.Def, len(all))
	for _, def := range all {
		byName[def.Name()] = def
	}

	return byName
})

// errNoCatalogSeries reports a supervision metric the catalog does not publish.
var errNoCatalogSeries = errors.New("no catalog series")
