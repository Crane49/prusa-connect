//go:build observability

package prom

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/prusa3d/prusa-connect/internal/platform/observability"
)

// Compiled reports that this build links a Prometheus client. The composition
// root reads it to refuse a configuration asking for metrics this binary cannot
// serve, so the operator learns at start-up and not at the first scrape.
const Compiled = true

// Registry is the process's metric registry: every Def registered once at
// construction, and bound handles handed out afterwards.
//
// Handles are bound at construction. A handle looked up per call is a map probe
// on a path that runs once per frame per printer, and an error returned per
// call is an error nobody handles.
type Registry struct {
	registry   *prometheus.Registry
	counters   map[string]*prometheus.CounterVec
	gauges     map[string]*prometheus.GaugeVec
	histograms map[string]*prometheus.HistogramVec
}

// New validates every definition, registers it, and returns a registry serving
// them. One bad definition refuses the whole set: a process that started with
// part of its metrics would report health about a fleet it is not measuring.
func New(opts Options, defs ...observability.Def) (*Registry, error) {
	if err := validate(opts, defs); err != nil {
		return nil, err
	}

	reg := &Registry{
		registry:   prometheus.NewRegistry(),
		counters:   map[string]*prometheus.CounterVec{},
		gauges:     map[string]*prometheus.GaugeVec{},
		histograms: map[string]*prometheus.HistogramVec{},
	}

	for _, def := range defs {
		if err := reg.register(def); err != nil {
			return nil, err
		}
	}

	return reg, nil
}

// Counter returns the handle for one label combination of a counter.
func (r *Registry) Counter(def observability.Def, values ...string) (observability.Counter, error) {
	vec, ok := r.counters[def.Name()]
	if !ok {
		return nil, fmt.Errorf("%w: counter %q", ErrNotRegistered, def.Name())
	}

	handle, err := vec.GetMetricWithLabelValues(values...)
	if err != nil {
		return nil, fmt.Errorf("%w: counter %q: %w", ErrLabelValues, def.Name(), err)
	}

	return handle, nil
}

// Gauge returns the handle for one label combination of a gauge.
func (r *Registry) Gauge(def observability.Def, values ...string) (observability.Gauge, error) {
	vec, ok := r.gauges[def.Name()]
	if !ok {
		return nil, fmt.Errorf("%w: gauge %q", ErrNotRegistered, def.Name())
	}

	handle, err := vec.GetMetricWithLabelValues(values...)
	if err != nil {
		return nil, fmt.Errorf("%w: gauge %q: %w", ErrLabelValues, def.Name(), err)
	}

	return handle, nil
}

// Observer returns the handle for one label combination of a histogram.
func (r *Registry) Observer(def observability.Def, values ...string) (observability.Observer, error) {
	vec, ok := r.histograms[def.Name()]
	if !ok {
		return nil, fmt.Errorf("%w: histogram %q", ErrNotRegistered, def.Name())
	}

	handle, err := vec.GetMetricWithLabelValues(values...)
	if err != nil {
		return nil, fmt.Errorf("%w: histogram %q: %w", ErrLabelValues, def.Name(), err)
	}

	return handle, nil
}

// Drop removes every series of a metric whose labels match the ones given, and
// reports how many it removed. Labels the metric does not declare match
// nothing.
//
// It keeps a fan-out dimension bounded by the current fleet and not by every
// machine the process has ever spoken to. A printer that departs stops having a
// value for pcl_printer_up, and a series left behind would report 0 for a
// machine nobody is looking for, for as long as the process runs. The match is
// partial because one printer's id reaches its own series under several other
// label values, and the caller knows the id and not the rest.
func (r *Registry) Drop(def observability.Def, labels map[string]string) int {
	match := prometheus.Labels(labels)

	switch def.Kind() {
	case observability.KindCounter:
		if vec, ok := r.counters[def.Name()]; ok {
			return vec.DeletePartialMatch(match)
		}
	case observability.KindGauge:
		if vec, ok := r.gauges[def.Name()]; ok {
			return vec.DeletePartialMatch(match)
		}
	case observability.KindHistogram:
		if vec, ok := r.histograms[def.Name()]; ok {
			return vec.DeletePartialMatch(match)
		}
	}

	return 0
}

// Handler serves the registry.
//
// OpenMetrics negotiation is on because it carries exemplars. A scrape that
// fails on one broken collector still returns the rest: a metrics endpoint
// answering 500 during an incident tells nobody anything.
func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.registry, promhttp.HandlerOpts{
		ErrorHandling:     promhttp.ContinueOnError,
		EnableOpenMetrics: true,
	})
}

// Gatherer is the registry as client_golang's collection interface.
//
// It is exported so that contrib/bridges/prometheus can turn a Gatherer into an
// OTel Producer. Exporting over OTLP is then a config block that builds a
// reader, and the same code is not instrumented twice.
func (r *Registry) Gatherer() prometheus.Gatherer { return r.registry }

// register builds one collector from a definition.
func (r *Registry) register(def observability.Def) error {
	var collector prometheus.Collector

	switch def.Kind() {
	case observability.KindCounter:
		vec := prometheus.NewCounterVec(counterOpts(def), def.LabelNames())
		r.counters[def.Name()] = vec
		collector = vec
	case observability.KindGauge:
		vec := prometheus.NewGaugeVec(gaugeOpts(def), def.LabelNames())
		r.gauges[def.Name()] = vec
		collector = vec
	case observability.KindHistogram:
		vec := prometheus.NewHistogramVec(histogramOpts(def), def.LabelNames())
		r.histograms[def.Name()] = vec
		collector = vec
	default:
		return fmt.Errorf("%w: %q is %s", observability.ErrKind, def.Name(), def.Kind())
	}

	if err := r.registry.Register(collector); err != nil {
		return fmt.Errorf("register %q: %w", def.Name(), err)
	}

	return nil
}

func counterOpts(def observability.Def) prometheus.CounterOpts {
	return prometheus.CounterOpts{Name: def.Name(), Help: def.Help()}
}

func gaugeOpts(def observability.Def) prometheus.GaugeOpts {
	return prometheus.GaugeOpts{Name: def.Name(), Help: def.Help()}
}

// histogramOpts builds both bucket forms from one definition: the explicit
// bounds a text scrape reads, and the native resolution a protobuf scrape
// reads. A factor named without bounds would leave the text scrape with no
// buckets, and Buckets.Validate refuses that before this point.
func histogramOpts(def observability.Def) prometheus.HistogramOpts {
	buckets := def.Buckets()

	return prometheus.HistogramOpts{
		Name:                            def.Name(),
		Help:                            def.Help(),
		Buckets:                         buckets.Classic,
		NativeHistogramBucketFactor:     buckets.Factor,
		NativeHistogramMaxBucketNumber:  buckets.MaxBuckets,
		NativeHistogramMinResetDuration: buckets.MinResetInterval,
	}
}
