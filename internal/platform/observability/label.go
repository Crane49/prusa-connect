package observability

// Label is a metric dimension whose value set is small, fixed and known when
// the metric is defined: a driver name, an outcome, a route template.
type Label string

// String returns the label name as Prometheus sees it.
func (l Label) String() string { return string(l) }

// Fanout is a metric dimension carrying one value per device in the fleet.
//
// It is a separate type from Label because it multiplies the series count
// instead of adding to it. NewCounter and NewGauge accept one. NewHistogram has
// no parameter that can take one, so a bucketed metric cannot pick up a
// per-device dimension by accident.
type Fanout string

// DeviceID is the fan-out dimension this project has. It costs one series per
// device on a counter or a gauge, and one per device per bucket on a
// histogram.
const DeviceID Fanout = "device_id"

// String returns the label name as Prometheus sees it.
func (f Fanout) String() string { return string(f) }

// fanoutLabels is every declared Fanout. Validation reads this list and not the
// static type, so a name that reaches a histogram as a plain Label through a
// conversion is still refused.
var fanoutLabels = [...]Fanout{DeviceID}

// isFanout reports whether a label name is one of the fan-out dimensions.
func isFanout(name Label) bool {
	for _, f := range fanoutLabels {
		if Label(f) == name {
			return true
		}
	}

	return false
}
