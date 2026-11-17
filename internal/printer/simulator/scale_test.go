package simulator_test

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
)

// The load target is 1 to 400 printers at 1 to 4 Hz telemetry.
// Earlier research puts 400 out of reach of instruction-level emulation
// by two orders of magnitude and finding 16 puts it here, where a printer is a
// goroutine rather than a QEMU process.
//
// This is the measurement rather than the claim. It runs the whole fleet at the
// idle cadence over net.Pipe, waits for every printer to register, connect,
// answer a command and deliver its frames, and reports what that cost. Nothing
// is asserted about the numbers: a wall-clock assertion is a flake on a loaded
// runner. What is asserted is that all four hundred got there.
//
// PRINTERSIM_SCALE_PRINTERS, PRINTERSIM_SCALE_FRAMES and
// PRINTERSIM_SCALE_CADENCE turn the same test into the nightly soak, and
// dropping the cadence is how the ceiling is found rather than the cadence.
func TestFourHundredPrintersRegisterConnectAndStream(t *testing.T) {
	fleet := envInt(t, "PRINTERSIM_SCALE_PRINTERS", 400)
	frames := envInt(t, "PRINTERSIM_SCALE_FRAMES", 4)
	cadence := envDuration(t, "PRINTERSIM_SCALE_CADENCE", 250*time.Millisecond)

	counters := &fleetCounters{
		telemetryTarget: int64(frames),
		fleet:           int64(fleet),
		perPrinter:      make(map[string]*atomic.Int64, fleet),
		streamed:        make(chan struct{}),
		answered:        make(chan struct{}),
	}

	host := newHost(t)
	defer host.stop()

	sessions := make(chan string, fleet)
	host.onSession = func(fingerprint string) { sessions <- fingerprint }
	host.onMessage = counters.observe

	printers := make([]*simulator.Printer, 0, fleet)

	for i := range fleet {
		printer, err := simulator.New(simulator.Config{
			Model:      "MK4",
			Serial:     fmt.Sprintf("SIM-%04d", i),
			BaseURL:    host.baseURL(),
			Dial:       host.dial,
			IdlePeriod: cadence,
		})
		if err != nil {
			t.Fatalf("new printer %d: %v", i, err)
		}

		counters.register(printer.Fingerprint())
		printers = append(printers, printer)
	}

	ctx, cancel := context.WithCancel(t.Context())

	defer cancel()

	before := heapInUse()
	baseGoroutines := runtime.NumGoroutine()
	start := time.Now()

	var group sync.WaitGroup

	for _, printer := range printers {
		group.Go(func() {
			if err := printer.RegisterAndRun(ctx); err != nil {
				t.Errorf("printer %s: %v", printer.Serial(), err)
			}
		})
	}

	for connected := range fleet {
		select {
		case <-sessions:
		case <-time.After(time.Minute):
			t.Fatalf("only %d of %d printers connected", connected, fleet)
		}
	}

	connectedAt := time.Since(start)

	for _, printer := range printers {
		if _, err := host.send(printer.Fingerprint(), "SEND_INFO", nil); err != nil {
			t.Fatalf("send to %s: %v", printer.Serial(), err)
		}
	}

	waitFor(t, counters.answered, "every printer to answer SEND_INFO")

	answered := time.Since(start)

	waitFor(t, counters.streamed, fmt.Sprintf("every printer to deliver %d telemetry frames", frames))

	elapsed := time.Since(start)
	peak := heapInUse()
	goroutines := runtime.NumGoroutine()

	cancel()
	group.Wait()

	total := counters.telemetry.Load()
	ideal := time.Duration(frames) * cadence

	t.Logf("%d printers at %v telemetry cadence, over net.Pipe, in one process:", fleet, cadence)
	t.Logf("  registered and connected:  %v", connectedAt.Round(time.Millisecond))
	t.Logf("  all answered SEND_INFO:    %v", answered.Round(time.Millisecond))
	t.Logf("  %d telemetry frames each:   %v (the cadence alone is %v)",
		frames, elapsed.Round(time.Millisecond), ideal)
	t.Logf("  telemetry frames total:    %d, %.0f per second",
		total, float64(total)/elapsed.Seconds())
	t.Logf("  goroutines:                %d, %.1f per printer, both ends in one process",
		goroutines, float64(goroutines-baseGoroutines)/float64(fleet))
	t.Logf("  heap in use:               %s at rest, %s at peak, %s per printer, both ends",
		bytes(before), bytes(peak), bytes((peak-before)/uint64(fleet)))
}

// fleetCounters is what the host reports back, aggregated. It is shared by
// every printer's reader goroutine, so nothing in here may be a plain map
// written to after the fleet starts.
type fleetCounters struct {
	telemetryTarget int64
	fleet           int64

	telemetry atomic.Int64
	events    atomic.Int64
	satisfied atomic.Int64

	// perPrinter is written only while the fleet is being built and read only
	// once it is running, so it needs no lock of its own.
	perPrinter map[string]*atomic.Int64

	streamed chan struct{}
	answered chan struct{}
}

func (c *fleetCounters) register(fingerprint string) {
	c.perPrinter[fingerprint] = &atomic.Int64{}
}

func (c *fleetCounters) observe(fingerprint string, payload map[string]any) {
	if _, isEvent := payload["event"]; isEvent {
		if c.events.Add(1) == c.fleet {
			close(c.answered)
		}

		return
	}

	c.telemetry.Add(1)

	counter, ok := c.perPrinter[fingerprint]
	if !ok {
		return
	}

	if counter.Add(1) == c.telemetryTarget && c.satisfied.Add(1) == c.fleet {
		close(c.streamed)
	}
}

func waitFor(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()

	select {
	case <-done:
	case <-time.After(time.Minute):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func heapInUse() uint64 {
	runtime.GC()

	var stats runtime.MemStats

	runtime.ReadMemStats(&stats)

	return stats.HeapInuse
}

func bytes(value uint64) string {
	const unit = 1024

	switch {
	case value >= unit*unit:
		return fmt.Sprintf("%.1f MiB", float64(value)/(unit*unit))
	case value >= unit:
		return fmt.Sprintf("%.1f KiB", float64(value)/unit)
	default:
		return strconv.FormatUint(value, 10) + " B"
	}
}

func envDuration(t *testing.T, name string, fallback time.Duration) time.Duration {
	t.Helper()

	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}

	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		t.Fatalf("%s is %q, want a positive duration", name, raw)
	}

	return value
}

func envInt(t *testing.T, name string, fallback int) int {
	t.Helper()

	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}

	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		t.Fatalf("%s is %q, want a positive integer", name, raw)
	}

	return value
}
