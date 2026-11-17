// Package host defines what a resource receives when it is constructed: a
// logger, metric handles and a way to report its own runtime status. All three
// belong to one instance and to one construction of it.
//
// The package defines interfaces and imports only the standard library, so a
// resource that takes a Handle does not depend on the supervision plane.
package host

import "log/slog"

// Counter is a monotonically increasing series.
type Counter interface {
	// Add increases the counter by delta.
	Add(delta float64)
}

// Observer records a distribution.
type Observer interface {
	// Observe records one value.
	Observe(value float64)
}

// Metrics gives a resource metric handles that already carry its instance
// labels. A resource never sees the metrics registry, so it cannot record a
// series under another instance's name.
type Metrics interface {
	// Counter returns the counter named name.
	Counter(name string) Counter
	// Histogram returns the distribution named name.
	Histogram(name string) Observer
	// With returns a child carrying one more label.
	With(key, value string) Metrics
}

// Reporter is how a resource reports what the host cannot see: background work
// that failed after Start returned, a credential the remote revoked, media that
// was removed. It reports runtime health only. A resource cannot report
// starting, stopping or stopped, so it cannot disagree with the host about the
// lifecycle.
//
// A Reporter belongs to the construction that received it. A goroutine that
// outlives Close may still call it, and the host discards what it says instead
// of applying it to the next construction.
type Reporter interface {
	// OK reports that the resource's own work is healthy. Calling it again is
	// cheap: a report that changes nothing publishes nothing.
	OK()
	// Recoverable reports a failure that retrying can fix.
	Recoverable(err error)
	// Permanent reports a failure that retrying cannot fix.
	Permanent(err error)
}

// Handle is everything a resource receives from its host.
type Handle struct {
	// Logger already carries the instance's kind and name.
	Logger *slog.Logger
	// Metrics already carry the instance's name.
	Metrics Metrics
	// Status reports runtime health for this construction of the instance.
	Status Reporter
}

// Discard returns a handle that logs nowhere, counts nothing and reports
// nothing, for a caller that has no host to offer.
func Discard() Handle {
	return Handle{
		Logger:  slog.New(slog.DiscardHandler),
		Metrics: NopMetrics(),
		Status:  NopReporter(),
	}
}

// NopMetrics returns metric handles that record nothing.
func NopMetrics() Metrics { return nopMetrics{} }

// NopReporter returns a reporter that discards what it is told.
func NopReporter() Reporter { return nopReporter{} }

type nopMetrics struct{}

func (nopMetrics) Counter(string) Counter    { return nopHandle{} }
func (nopMetrics) Histogram(string) Observer { return nopHandle{} }
func (nopMetrics) With(_, _ string) Metrics  { return nopMetrics{} }

type nopHandle struct{}

func (nopHandle) Add(float64)     {}
func (nopHandle) Observe(float64) {}

type nopReporter struct{}

func (nopReporter) OK()               {}
func (nopReporter) Recoverable(error) {}
func (nopReporter) Permanent(error)   {}
