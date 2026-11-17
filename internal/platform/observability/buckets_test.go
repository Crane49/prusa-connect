package observability_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/internal/platform/observability"
)

// One definition serves both scrapes: a text scraper reads the explicit bounds,
// a protobuf scraper reads the exponential resolution, and neither is
// configured on its own.
func TestOneDefinitionCarriesBothBucketSets(t *testing.T) {
	t.Parallel()

	for name, buckets := range map[string]observability.Buckets{
		"latency": observability.LatencySeconds(),
		"size":    observability.SizeBytes(),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := buckets.Validate(); err != nil {
				t.Fatalf("refused: %v", err)
			}

			if len(buckets.Classic) == 0 {
				t.Error("no classic bounds, so a text scrape carries no buckets")
			}

			if buckets.Factor <= 1 {
				t.Error("no native factor, so a protobuf scrape carries no native histogram")
			}

			if buckets.MaxBuckets == 0 || buckets.MinResetInterval <= 0 {
				t.Error("the native buckets are unbounded")
			}
		})
	}
}

func TestValidateRefusesABucketDefinitionThatServesOnlyOneScraper(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		buckets observability.Buckets
		want    error
	}{
		{
			name: "a native factor with no explicit bounds",
			buckets: observability.Buckets{
				Factor: 1.1, MaxBuckets: 160, MinResetInterval: time.Hour,
			},
			want: observability.ErrNoClassicBounds,
		},
		{
			name: "bounds that do not increase",
			buckets: observability.Buckets{
				Classic: []float64{1, 1}, Factor: 1.1, MaxBuckets: 160, MinResetInterval: time.Hour,
			},
			want: observability.ErrBoundsNotIncreasing,
		},
		{
			name: "an explicit infinite bound",
			buckets: observability.Buckets{
				Classic:    []float64{1, math.Inf(1)},
				Factor:     1.1,
				MaxBuckets: 160, MinResetInterval: time.Hour,
			},
			want: observability.ErrInfiniteBound,
		},
		{
			name: "no native factor",
			buckets: observability.Buckets{
				Classic: []float64{1, 2}, MaxBuckets: 160, MinResetInterval: time.Hour,
			},
			want: observability.ErrFactorNotAboveOne,
		},
		{
			name: "no ceiling on the native buckets",
			buckets: observability.Buckets{
				Classic: []float64{1, 2}, Factor: 1.1, MinResetInterval: time.Hour,
			},
			want: observability.ErrNoBucketCeiling,
		},
		{
			name: "no reset interval",
			buckets: observability.Buckets{
				Classic: []float64{1, 2}, Factor: 1.1, MaxBuckets: 160,
			},
			want: observability.ErrNoResetInterval,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if err := testCase.buckets.Validate(); !errors.Is(err, testCase.want) {
				t.Fatalf("want %v, got %v", testCase.want, err)
			}
		})
	}
}
