package observability_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/platform/observability"
)

// The constructor has no parameter that accepts a Fanout, so the only way to
// write this metric is the conversion below, and Validate refuses it.
func TestAHistogramMayNotCarryAFanoutLabel(t *testing.T) {
	t.Parallel()

	def := observability.NewHistogram(
		"pcl_printer_command_duration_seconds",
		"Time from queueing a command to its result.",
		observability.LatencySeconds(),
		observability.Label(observability.DeviceID), "command",
	)

	err := def.Validate()
	if !errors.Is(err, observability.ErrHistogramFanout) {
		t.Fatalf("a histogram carrying %s was accepted: %v", observability.DeviceID, err)
	}

	if !strings.Contains(err.Error(), "device_id") {
		t.Fatalf("the refusal does not name the label: %v", err)
	}
}

// The rule is about the multiplication, not about the label. One series per
// printer is affordable; one per printer per bucket is not.
func TestAFanoutLabelIsAllowedOnACounterAndAGauge(t *testing.T) {
	t.Parallel()

	defs := []observability.Def{
		observability.NewCounter("pcl_printer_errors_total", "Errors.", []observability.Label{"code"},
			observability.DeviceID),
		observability.NewGauge("pcl_printer_up", "Up.", nil, observability.DeviceID),
	}

	for _, def := range defs {
		t.Run(def.Name(), func(t *testing.T) {
			t.Parallel()

			if err := def.Validate(); err != nil {
				t.Fatalf("refused: %v", err)
			}

			if _, ok := def.Fanout(); !ok {
				t.Fatal("the definition does not report its fan-out label")
			}

			if got := def.LabelNames(); !slices.Contains(got, "device_id") {
				t.Fatalf("device_id is missing from the label names: %v", got)
			}
		})
	}
}

func TestValidateRefusesTheNamingMistakes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		def  observability.Def
		want error
	}{
		{
			name: "a counter outside the namespace",
			def:  observability.NewCounter("frames_total", "Frames.", nil),
			want: observability.ErrName,
		},
		{
			name: "a counter that is not named total",
			def:  observability.NewCounter("pcl_frames", "Frames.", nil),
			want: observability.ErrCounterSuffix,
		},
		{
			name: "a gauge that is named total",
			def:  observability.NewGauge("pcl_frames_total", "Frames.", nil),
			want: observability.ErrCounterSuffix,
		},
		{
			name: "a metric with no help",
			def:  observability.NewGauge("pcl_frames", "", nil),
			want: observability.ErrHelp,
		},
		{
			name: "a histogram not named for a base unit",
			def: observability.NewHistogram("pcl_command_time", "Time.",
				observability.LatencySeconds()),
			want: observability.ErrUnitSuffix,
		},
		{
			name: "a histogram in milliseconds",
			def: observability.NewHistogram("pcl_command_duration_milliseconds", "Time.",
				observability.LatencySeconds()),
			want: observability.ErrUnitSuffix,
		},
		{
			name: "a reserved label",
			def:  observability.NewCounter("pcl_frames_total", "Frames.", []observability.Label{"le"}),
			want: observability.ErrLabelName,
		},
		{
			name: "the same label twice",
			def: observability.NewCounter("pcl_frames_total", "Frames.",
				[]observability.Label{"driver", "driver"}),
			want: observability.ErrDuplicateLabel,
		},
		{
			name: "two fan-out labels",
			def: observability.NewCounter("pcl_frames_total", "Frames.", nil,
				observability.DeviceID, observability.DeviceID),
			want: observability.ErrFanoutCount,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if err := testCase.def.Validate(); !errors.Is(err, testCase.want) {
				t.Fatalf("want %v, got %v", testCase.want, err)
			}
		})
	}
}

// The definition set this project ships is held to its own rules.
func TestTheCatalogIsValid(t *testing.T) {
	t.Parallel()

	seen := map[string]struct{}{}

	all := append(observability.Catalog(), observability.OptIn()...)
	for _, def := range all {
		if err := def.Validate(); err != nil {
			t.Errorf("%s: %v", def.Name(), err)
		}

		if _, repeated := seen[def.Name()]; repeated {
			t.Errorf("%s is defined twice", def.Name())
		}

		seen[def.Name()] = struct{}{}
	}
}

// The per-printer set stays inside the budget, and Fleet is the only place a
// fan-out dimension comes from. The catalog leaves part of the budget free for
// a per-printer series added later.
func TestTheFleetSetIsTheWholeFanoutAllowance(t *testing.T) {
	t.Parallel()

	fanout := 0

	for _, def := range observability.Catalog() {
		if _, ok := def.Fanout(); ok {
			fanout++
		}
	}

	if fanout > observability.DefaultFanoutBudget {
		t.Fatalf("the catalog carries %d fan-out metrics, the budget is %d",
			fanout, observability.DefaultFanoutBudget)
	}

	if fanout != len(observability.Fleet()) {
		t.Errorf("%d fan-out metrics and %d in Fleet: a per-printer series is declared elsewhere",
			fanout, len(observability.Fleet()))
	}

	for _, def := range observability.Fleet() {
		if def.Kind() == observability.KindHistogram {
			t.Errorf("%s is a per-printer histogram", def.Name())
		}
	}
}

// One process can expose both sets during a migration only if no name is in
// both.
func TestTheCatalogDoesNotCollideWithTheShippingSeries(t *testing.T) {
	t.Parallel()

	catalog := map[string]struct{}{}
	for _, def := range append(observability.Catalog(), observability.OptIn()...) {
		catalog[def.Name()] = struct{}{}
	}

	for _, legacy := range observability.Legacy() {
		if _, collides := catalog[legacy.Name]; collides {
			t.Errorf("%s is exposed by both the shipping server and the catalog", legacy.Name)
		}

		if strings.HasPrefix(legacy.Name, observability.Namespace+"_") {
			t.Errorf("%s is a shipping series and carries the new namespace", legacy.Name)
		}
	}
}

// A successor that does not exist is a migration note nobody can follow, and a
// series with no successor has to say so.
func TestEverySucceededShippingSeriesNamesACatalogMetric(t *testing.T) {
	t.Parallel()

	catalog := map[string]struct{}{}
	for _, def := range append(observability.Catalog(), observability.OptIn()...) {
		catalog[def.Name()] = struct{}{}
	}

	for _, legacy := range observability.Legacy() {
		if legacy.Note == "" {
			t.Errorf("%s says nothing about what changes for a dashboard", legacy.Name)
		}

		if legacy.Successor == "" {
			continue
		}

		if _, ok := catalog[legacy.Successor]; !ok {
			t.Errorf("%s names %s as its successor, which the catalog does not define",
				legacy.Name, legacy.Successor)
		}
	}
}

// The shipping per-printer histograms have no successor. Naming them here means
// an operator reads about the break instead of finding a blank dashboard.
func TestTheShippingHistogramsCarryingADeviceLabelAreNamed(t *testing.T) {
	t.Parallel()

	var broken []string

	for _, legacy := range observability.Legacy() {
		if legacy.FanoutHistogram() {
			broken = append(broken, legacy.Name)
		}
	}

	want := []string{"queue_command_duration_seconds", "command_duration_seconds"}

	slices.Sort(broken)
	slices.Sort(want)

	if !slices.Equal(broken, want) {
		t.Fatalf("the shipping per-printer histograms are %v, the table says %v", want, broken)
	}
}
