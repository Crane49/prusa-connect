package observability

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Namespace prefixes every series this project exposes, so a scrape of a box
// running several exporters says which of them a series came from.
const Namespace = "pcl"

// Definition errors. Each one is a mistake in the code that a scrape would
// otherwise turn into an outage or a useless panel.
var (
	// ErrHistogramFanout is a fan-out dimension on a bucketed metric. That
	// costs one series per device per bucket, which the box this runs on
	// cannot hold.
	ErrHistogramFanout = errors.New("a histogram may not carry a fan-out label")
	// ErrFanoutCount is more than one fan-out dimension on one metric. Their
	// series counts multiply.
	ErrFanoutCount = errors.New("a metric may carry at most one fan-out label")
	// ErrName is a metric name Prometheus will not accept, or one missing the
	// project namespace.
	ErrName = errors.New("invalid metric name")
	// ErrHelp is a metric with no help text. A scrape carries it and an
	// operator reads it.
	ErrHelp = errors.New("a metric needs help text")
	// ErrCounterSuffix is a counter that does not end in _total, or a metric
	// that is not a counter and does.
	ErrCounterSuffix = errors.New("only a counter is named _total")
	// ErrUnitSuffix is a histogram whose name does not end in a base unit.
	ErrUnitSuffix = errors.New("a histogram is named for the base unit it measures")
	// ErrLabelName is a label name Prometheus reserves or will not accept.
	ErrLabelName = errors.New("invalid label name")
	// ErrDuplicateLabel is the same dimension declared twice.
	ErrDuplicateLabel = errors.New("duplicate label")
	// ErrKind is a definition built by something other than the constructors.
	ErrKind = errors.New("unknown metric kind")
)

// namePattern is the metric and label name grammar from the Prometheus data
// model.
var namePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// baseUnits are the suffixes a histogram may end in. Prometheus' naming
// practices allow base units only, which rules out milliseconds and percent.
var baseUnits = [...]string{
	"seconds", "bytes", "ratio", "celsius", "joules", "grams", "volts", "amperes", "meters",
}

// reservedLabels are the names a metric may not declare. Prometheus attaches
// them itself, and a collision is either refused at registration or renamed at
// scrape.
var reservedLabels = [...]Label{"le", "quantile", "job", "instance", "__name__"}

// Kind is what a definition becomes when it is registered.
type Kind uint8

const (
	// KindCounter is a series that only increases.
	KindCounter Kind = iota
	// KindGauge is a series that moves in both directions.
	KindGauge
	// KindHistogram is a bucketed distribution.
	KindHistogram
)

// String names the kind.
func (k Kind) String() string {
	switch k {
	case KindCounter:
		return "counter"
	case KindGauge:
		return "gauge"
	case KindHistogram:
		return "histogram"
	default:
		return "unknown"
	}
}

// Def is one metric as it will be registered. Its fields are unexported and the
// constructors are the only way to get one, so NewHistogram, which has no
// parameter that accepts a Fanout, is the only way to define a histogram.
type Def struct {
	name    string
	help    string
	kind    Kind
	labels  []Label
	fanout  []Fanout
	buckets Buckets
}

// NewCounter defines a series that only increases. It may carry one fan-out
// dimension, which costs one series per device.
func NewCounter(name, help string, labels []Label, fanout ...Fanout) Def {
	return Def{name: name, help: help, kind: KindCounter, labels: labels, fanout: fanout}
}

// NewGauge defines a series that moves in both directions. Like a counter it
// may carry one fan-out dimension.
//
// A gauge answering "when did this last happen" carries a timestamp and not an
// age. An age has to be refreshed by a ticker, and it reads as fresh while the
// process is blocked.
func NewGauge(name, help string, labels []Label, fanout ...Fanout) Def {
	return Def{name: name, help: help, kind: KindGauge, labels: labels, fanout: fanout}
}

// NewHistogram defines a bucketed distribution from one bucket definition that
// serves classic and native scrapes alike.
//
// There is no fan-out parameter. A per-device histogram is one series per
// device per bucket. A fan-out name converted to a plain Label to get past this
// signature is refused by Validate.
func NewHistogram(name, help string, buckets Buckets, labels ...Label) Def {
	return Def{name: name, help: help, kind: KindHistogram, labels: labels, buckets: buckets}
}

// Name is the series name.
func (d Def) Name() string { return d.name }

// Help is the description a scrape carries.
func (d Def) Help() string { return d.help }

// Kind is counter, gauge or histogram.
func (d Def) Kind() Kind { return d.kind }

// Buckets is the bucket definition, meaningful for a histogram only.
func (d Def) Buckets() Buckets { return d.buckets }

// Fanout reports whether this metric carries a per-device dimension, and which.
func (d Def) Fanout() (Fanout, bool) {
	if len(d.fanout) == 0 {
		return "", false
	}

	return d.fanout[0], true
}

// LabelNames are the dimensions in the order their values are supplied,
// ordinary labels first and the fan-out dimension last.
func (d Def) LabelNames() []string {
	names := make([]string, 0, len(d.labels)+len(d.fanout))

	for _, label := range d.labels {
		names = append(names, string(label))
	}

	for _, f := range d.fanout {
		names = append(names, string(f))
	}

	return names
}

// Validate reports every problem with a definition at once, so all of them can
// be fixed in one pass.
func (d Def) Validate() error {
	problems := d.nameProblems()
	problems = append(problems, d.labelProblems()...)
	problems = append(problems, d.fanoutProblems()...)

	if d.kind == KindHistogram {
		if err := d.buckets.Validate(); err != nil {
			problems = append(problems, fmt.Errorf("%s: %w", d.name, err))
		}
	}

	return errors.Join(problems...)
}

// nameProblems holds a name to Prometheus' naming practices: the project
// namespace, the grammar, the _total suffix on counters only, and a base unit
// on anything bucketed.
func (d Def) nameProblems() []error {
	var problems []error

	switch {
	case !namePattern.MatchString(d.name):
		problems = append(problems, fmt.Errorf("%w: %q", ErrName, d.name))
	case !strings.HasPrefix(d.name, Namespace+"_"):
		problems = append(problems, fmt.Errorf("%w: %q does not start with %s_", ErrName, d.name, Namespace))
	}

	if d.help == "" {
		problems = append(problems, fmt.Errorf("%w: %q", ErrHelp, d.name))
	}

	if total := strings.HasSuffix(d.name, "_total"); total != (d.kind == KindCounter) {
		problems = append(problems, fmt.Errorf("%w: %q is a %s", ErrCounterSuffix, d.name, d.kind))
	}

	if d.kind == KindHistogram && !hasBaseUnit(d.name) {
		problems = append(problems, fmt.Errorf("%w: %q", ErrUnitSuffix, d.name))
	}

	return problems
}

// labelProblems reports reserved, malformed and repeated dimensions.
func (d Def) labelProblems() []error {
	var problems []error

	seen := make(map[string]struct{}, len(d.labels)+len(d.fanout))

	for _, name := range d.LabelNames() {
		if !namePattern.MatchString(name) || strings.HasPrefix(name, "__") {
			problems = append(problems, fmt.Errorf("%w: %q on %q", ErrLabelName, name, d.name))
		}

		if slices.Contains(reservedLabels[:], Label(name)) {
			problems = append(problems, fmt.Errorf("%w: %q is reserved, on %q", ErrLabelName, name, d.name))
		}

		if _, repeated := seen[name]; repeated {
			problems = append(problems, fmt.Errorf("%w: %q on %q", ErrDuplicateLabel, name, d.name))
		}

		seen[name] = struct{}{}
	}

	return problems
}

// fanoutProblems reads the label names and not the static types, so a fan-out
// dimension converted to a plain Label to get past the constructor is refused
// here.
func (d Def) fanoutProblems() []error {
	var problems []error

	if len(d.fanout) > 1 {
		problems = append(problems, fmt.Errorf("%w: %q carries %d", ErrFanoutCount, d.name, len(d.fanout)))
	}

	if d.kind != KindHistogram {
		return problems
	}

	for _, name := range d.LabelNames() {
		if isFanout(Label(name)) {
			problems = append(problems, fmt.Errorf("%w: %q on %q", ErrHistogramFanout, name, d.name))
		}
	}

	return problems
}

// hasBaseUnit reports whether a name ends in one of the units Prometheus'
// naming practices allow.
func hasBaseUnit(name string) bool {
	for _, unit := range baseUnits {
		if strings.HasSuffix(name, "_"+unit) {
			return true
		}
	}

	return false
}
