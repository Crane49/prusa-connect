package observability

// Counter is a series that only increases. It is a narrow view, so a call site
// cannot reach the registry through the handle it was given.
type Counter interface {
	// Add increases the series by delta.
	Add(delta float64)
}

// Gauge is a series that moves in both directions.
type Gauge interface {
	// Set replaces the current value.
	Set(value float64)
	// Add moves the current value by delta, which may be negative.
	Add(delta float64)
}

// Observer records one value into a distribution.
type Observer interface {
	// Observe records one value.
	Observe(value float64)
}

// NopCounter returns a counter that records nothing.
func NopCounter() Counter { return nop{} }

// NopGauge returns a gauge that records nothing.
func NopGauge() Gauge { return nop{} }

// NopObserver returns an observer that records nothing.
func NopObserver() Observer { return nop{} }

type nop struct{}

func (nop) Add(float64)     {}
func (nop) Set(float64)     {}
func (nop) Observe(float64) {}
