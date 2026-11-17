//go:build !observability

package prom

import (
	"fmt"
	"net/http"

	"github.com/prusa3d/prusa-connect/internal/platform/observability"
)

// notCompiled is what a scrape of a build without the observability tag gets.
// The status is 404 and not an empty 200: an empty exposition reads as a
// healthy process with nothing to report, and a scraper would alert on the
// wrong thing.
const notCompiled = "metrics are not compiled into this build\n"

// Compiled reports that this build links no Prometheus client. The composition
// root reads it to refuse a configuration asking for metrics this binary cannot
// serve, so the operator learns at start-up and not at the first scrape.
const Compiled = false

// Registry is the same surface the observability build exposes, recording
// nothing. Definitions are still validated and handles are still returned.
// Nothing is collected and nothing is served.
type Registry struct {
	names map[string]observability.Kind
}

// New validates every definition as the observability build does, and returns a
// registry that records nothing.
func New(opts Options, defs ...observability.Def) (*Registry, error) {
	if err := validate(opts, defs); err != nil {
		return nil, err
	}

	names := make(map[string]observability.Kind, len(defs))
	for _, def := range defs {
		names[def.Name()] = def.Kind()
	}

	return &Registry{names: names}, nil
}

// Counter returns a handle that records nothing, and the same not-registered
// error the observability build returns.
func (r *Registry) Counter(def observability.Def, _ ...string) (observability.Counter, error) {
	if err := r.known(def, observability.KindCounter); err != nil {
		return nil, err
	}

	return observability.NopCounter(), nil
}

// Gauge returns a handle that records nothing.
func (r *Registry) Gauge(def observability.Def, _ ...string) (observability.Gauge, error) {
	if err := r.known(def, observability.KindGauge); err != nil {
		return nil, err
	}

	return observability.NopGauge(), nil
}

// Observer returns a handle that records nothing.
func (r *Registry) Observer(def observability.Def, _ ...string) (observability.Observer, error) {
	if err := r.known(def, observability.KindHistogram); err != nil {
		return nil, err
	}

	return observability.NopObserver(), nil
}

// Drop removes nothing, because nothing was recorded. It reports zero, as the
// observability build does for labels no series carried.
func (r *Registry) Drop(_ observability.Def, _ map[string]string) int { return 0 }

// Handler answers 404, because this build has no metrics to serve.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)

		// The status is already written, so there is nothing to do about a
		// failed write.
		_, _ = w.Write([]byte(notCompiled))
	})
}

// known reports whether a definition was registered under the kind asked for.
func (r *Registry) known(def observability.Def, kind observability.Kind) error {
	got, ok := r.names[def.Name()]
	if !ok || got != kind {
		return fmt.Errorf("%w: %s %q", ErrNotRegistered, kind, def.Name())
	}

	return nil
}
