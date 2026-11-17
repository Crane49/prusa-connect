package prom_test

import (
	"errors"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/platform/observability"
	"github.com/prusa3d/prusa-connect/internal/platform/observability/prom"
)

// A histogram may not carry a per-device label. The test carries no build tag,
// so the rule is checked in the build that links a client and in the one that
// does not.
func TestTheRegistryRefusesAHistogramCarryingAFanoutLabel(t *testing.T) {
	t.Parallel()

	_, err := prom.New(prom.Options{}, observability.NewHistogram(
		"pcl_printer_command_duration_seconds",
		"Time from queueing a command to its result.",
		observability.LatencySeconds(),
		observability.Label(observability.DeviceID), "command",
	))

	if !errors.Is(err, observability.ErrHistogramFanout) {
		t.Fatalf("a per-printer histogram was registered: %v", err)
	}
}

// The budget is over the whole process and not over one metric at a time.
func TestTheRegistryRefusesMoreFanoutMetricsThanTheBudget(t *testing.T) {
	t.Parallel()

	// Two per-printer metrics on top of the shipped catalog, which puts the
	// count over the default budget. They are written here and not taken from
	// OptIn, which is empty: the claim is about the budget and not about any
	// one metric.
	defs := append(observability.Catalog(),
		observability.NewGauge("pcl_probe_one_ratio", "One per printer.", nil, observability.DeviceID),
		observability.NewGauge("pcl_probe_two_ratio", "Another per printer.", nil, observability.DeviceID),
	)

	if _, err := prom.New(prom.Options{}, defs...); !errors.Is(err, prom.ErrFanoutBudget) {
		t.Fatalf("two more per-printer metrics registered inside the default budget: %v", err)
	}

	budget := prom.Options{FanoutBudget: len(defs)}
	if _, err := prom.New(budget, defs...); err != nil {
		t.Fatalf("a raised budget still refused them: %v", err)
	}
}

// The whole shipped definition set goes through the real registration path.
func TestTheCatalogRegisters(t *testing.T) {
	t.Parallel()

	if _, err := prom.New(prom.Options{}, observability.Catalog()...); err != nil {
		t.Fatalf("the catalog does not register: %v", err)
	}
}

func TestAHandleIsBoundForEveryKind(t *testing.T) {
	t.Parallel()

	counter := observability.NewCounter("pcl_frames_total", "Frames.", []observability.Label{"driver"})
	gauge := observability.NewGauge("pcl_printer_up", "Up.", nil, observability.DeviceID)
	histogram := observability.NewHistogram("pcl_call_duration_seconds", "Calls.",
		observability.LatencySeconds(), "driver")

	registry, err := prom.New(prom.Options{}, counter, gauge, histogram)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	counterHandle, err := registry.Counter(counter, "xbuddy")
	if err != nil {
		t.Fatalf("counter handle: %v", err)
	}

	gaugeHandle, err := registry.Gauge(gauge, "mk4-017")
	if err != nil {
		t.Fatalf("gauge handle: %v", err)
	}

	observerHandle, err := registry.Observer(histogram, "xbuddy")
	if err != nil {
		t.Fatalf("observer handle: %v", err)
	}

	counterHandle.Add(1)
	gaugeHandle.Set(1)
	observerHandle.Observe(0.25)

	if registry.Handler() == nil {
		t.Fatal("the registry serves no handler")
	}
}

func TestAnUnregisteredMetricIsRefused(t *testing.T) {
	t.Parallel()

	registered := observability.NewCounter("pcl_frames_total", "Frames.", nil)
	stranger := observability.NewCounter("pcl_other_total", "Other.", nil)

	registry, err := prom.New(prom.Options{}, registered)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	if _, err = registry.Counter(stranger); !errors.Is(err, prom.ErrNotRegistered) {
		t.Fatalf("want %v, got %v", prom.ErrNotRegistered, err)
	}

	if _, err = registry.Gauge(registered); !errors.Is(err, prom.ErrNotRegistered) {
		t.Fatalf("a counter was handed out as a gauge: %v", err)
	}
}
