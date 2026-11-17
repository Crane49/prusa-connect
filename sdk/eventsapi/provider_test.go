package eventsapi_test

import (
	"errors"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

type stubProcessor struct{ closed bool }

func (s *stubProcessor) PrinterStateChanged(eventsapi.PrinterState)   {}
func (s *stubProcessor) PrinterLabelsChanged(eventsapi.PrinterLabels) {}
func (s *stubProcessor) PrinterDeleted(string)                        {}
func (s *stubProcessor) JobUpdated(eventsapi.JobInfo)                 {}
func (s *stubProcessor) LabelDeleted(string)                          {}
func (s *stubProcessor) FactObserved(eventsapi.Fact)                  {}
func (s *stubProcessor) Close() error                                 { s.closed = true; return nil }

// registrations names a type nothing has registered yet.
//
// A registration cannot be taken out of the gathering list again, so a fixed
// name is registered once and the run after that starts from a list that
// already holds it. Under -count=2 the first line below failed, which is the
// trap somebody chasing a flake with -count falls into first.
//
//nolint:gochecknoglobals // the counter has to outlive the case that reads it.
var registrations atomic.Uint64

func doc() extension.Doc {
	return extension.Doc{
		Summary:     "A processor for a test.",
		Description: "It records the spec it was built from and does nothing else.",
		Version:     "1.0.0",
		Contract:    eventsapi.Contract,
		Stability:   extension.StabilityStable,
	}
}

func TestRegisterBuildAndIsRegistered(t *testing.T) {
	t.Parallel()

	typ := "test-amqp-" + strconv.FormatUint(registrations.Add(1), 10)

	var gotCfg map[string]any

	registration, err := eventsapi.Declare(typ, doc(),
		func(spec eventsapi.ProviderSpec) (eventsapi.Processor, error) {
			gotCfg = spec.Config

			return &stubProcessor{}, nil
		})
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}

	registry, err := eventsapi.NewRegistryFrom(registration)
	if err != nil {
		t.Fatalf("NewRegistryFrom: %v", err)
	}

	p, err := registry.BuildProvider(eventsapi.ProviderSpec{Type: typ, Config: map[string]any{"url": "x"}})
	if err != nil {
		t.Fatalf("BuildProvider: %v", err)
	}

	if p == nil {
		t.Fatal("BuildProvider returned nil processor")
	}

	if gotCfg["url"] != "x" {
		t.Fatalf("factory did not receive config: %v", gotCfg)
	}
}

func TestBuildProviderUnknownType(t *testing.T) {
	t.Parallel()

	registry, err := eventsapi.NewRegistryFrom()
	if err != nil {
		t.Fatalf("NewRegistryFrom: %v", err)
	}

	_, err = registry.BuildProvider(eventsapi.ProviderSpec{Type: "nope-not-registered"})
	if !errors.Is(err, eventsapi.ErrUnknownProvider) {
		t.Fatalf("expected ErrUnknownProvider, got %v", err)
	}
}
