package observability

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// Bucket definition errors.
var (
	// ErrNoClassicBounds is a bucket definition with no explicit bounds.
	// Setting NativeHistogramBucketFactor replaces client_golang's default
	// bucket set with an empty one, so a definition without explicit bounds
	// serves a text scraper a histogram with no buckets at all.
	ErrNoClassicBounds = errors.New("classic bounds are required alongside a native factor")
	// ErrBoundsNotIncreasing is a bound that does not sit above the one before
	// it. client_golang requires strictly increasing bounds.
	ErrBoundsNotIncreasing = errors.New("classic bounds must be strictly increasing")
	// ErrInfiniteBound is an explicit +Inf bound. client_golang appends it.
	ErrInfiniteBound = errors.New("the +Inf bound is implicit")
	// ErrFactorNotAboveOne is a native factor of one or less, which turns
	// native buckets off and leaves the definition serving classic buckets only.
	ErrFactorNotAboveOne = errors.New("the native factor must be above one")
	// ErrNoBucketCeiling is a native definition with no cap on populated
	// buckets, which is unbounded memory per series.
	ErrNoBucketCeiling = errors.New("the native bucket ceiling must be positive")
	// ErrNoResetInterval is a native definition that may reset at any moment,
	// which makes a rate over it meaningless.
	ErrNoResetInterval = errors.New("the native reset interval must be positive")
)

// The native histogram settings every histogram here shares. A factor of 1.1
// puts eight buckets in each power of two, which client_golang's documentation
// gives as the balance between cost and accuracy. The ceiling and the reset
// interval bound what one series may cost in memory.
const (
	nativeFactor     = 1.1
	nativeMaxBuckets = 160
	nativeMinReset   = time.Hour
)

// Buckets is one bucket definition served two ways: the explicit bounds a text
// scrape reads as _bucket{le=...}, and the exponential resolution a protobuf
// scrape reads as a native histogram. client_golang builds both from one
// HistogramOpts, so a deployment moves to native histograms by changing its
// scrape config and never by instrumenting the code again.
type Buckets struct {
	// Classic are the upper bounds of the text-format buckets, without the
	// implicit +Inf. They are required whenever Factor is set.
	Classic []float64
	// Factor is the upper bound on how much wider each native bucket is than
	// the one below it.
	Factor float64
	// MaxBuckets caps the populated native buckets of one series before the
	// histogram widens its zero bucket or drops resolution.
	MaxBuckets uint32
	// MinResetInterval is the shortest time that may pass before a histogram
	// over MaxBuckets resets instead of losing resolution.
	MinResetInterval time.Duration
}

// LatencySeconds is the definition for anything measured in seconds: an HTTP
// request, a printer command, a resource call. The bounds run from a
// sub-millisecond local call to ten seconds, by which point a caller has given
// up.
func LatencySeconds() Buckets {
	return native([]float64{
		.0005, .001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10,
	})
}

// SizeBytes is the definition for a payload measured in bytes, from a
// telemetry frame to a gcode upload.
func SizeBytes() Buckets {
	return native([]float64{
		256, 1024, 4096, 16384, 65536, 262144, 1 << 20, 1 << 22, 1 << 24, 1 << 26,
	})
}

// Validate reports what is wrong with a bucket definition.
func (b Buckets) Validate() error {
	var problems []error

	if len(b.Classic) == 0 {
		problems = append(problems, ErrNoClassicBounds)
	}

	problems = append(problems, b.boundProblems()...)

	if b.Factor <= 1 {
		problems = append(problems, fmt.Errorf("%w: %v", ErrFactorNotAboveOne, b.Factor))
	}

	if b.MaxBuckets == 0 {
		problems = append(problems, ErrNoBucketCeiling)
	}

	if b.MinResetInterval <= 0 {
		problems = append(problems, ErrNoResetInterval)
	}

	return errors.Join(problems...)
}

// boundProblems reports the bounds that are out of order or infinite.
func (b Buckets) boundProblems() []error {
	var problems []error

	for i, bound := range b.Classic {
		if math.IsInf(bound, 1) {
			problems = append(problems, ErrInfiniteBound)

			continue
		}

		if i > 0 && bound <= b.Classic[i-1] {
			problems = append(problems, fmt.Errorf("%w: %v then %v",
				ErrBoundsNotIncreasing, b.Classic[i-1], bound))
		}
	}

	return problems
}

// native pairs explicit bounds with the shared native settings, so every metric
// uses the same native settings.
func native(classic []float64) Buckets {
	return Buckets{
		Classic:          classic,
		Factor:           nativeFactor,
		MaxBuckets:       nativeMaxBuckets,
		MinResetInterval: nativeMinReset,
	}
}
