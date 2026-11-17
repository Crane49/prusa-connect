package app

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
)

// The store keeps the series a printer reported, not a sample of it.
//
// A history provider is an ordinary subscriber, and every subscriber was fed on
// the queue built for a screen: the newest value per metric per coalescing
// window and nothing else. That is right for a browser, which draws one value,
// and wrong for a store, which is asked later what the series was and can only
// answer with what it was handed. A printer reporting faster than the window is
// what makes the difference visible, so this one reports at samplePeriod, which
// is a quarter of stream.DefaultInterval.

// samplePeriod is the simulated printer's telemetry cadence here. Anything at
// or above stream.DefaultInterval would pass on a conflating feed, because
// there would be nothing for a window to conflate.
const samplePeriod = 50 * time.Millisecond

// TestAStoreHoldsEveryReadingAPrinterReported drives a known number of readings
// and counts the rows. The bound it asserts against is arithmetic and not a
// guess: a conflating feed can leave at most one row per metric per window, so
// a run of a measured length has a ceiling nothing on that feed can pass.
func TestAStoreHoldsEveryReadingAPrinterReported(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := storingPast(t, dir)

	// The clock starts before the printer does, so the reporting window this
	// counts against is longer than the one that produced the rows and the
	// ceiling below is generous rather than tight.
	began := time.Now()
	stop := dialInReporting(t, running, "SIM-SAMPLES")

	time.Sleep(3 * time.Second)

	// Stopped before the rows are counted, so nothing is being produced while
	// the last flush lands and the window is closed at both ends.
	stop()

	reporting := time.Since(began)

	store := historyFile(dir)
	busiest := `SELECT COALESCE(MAX(n), 0) FROM ` +
		`(SELECT count(*) AS n FROM history_sample GROUP BY series_id)`

	// The flush is on a timer, so the count is read once it has stopped moving
	// rather than at a moment picked by this test.
	var kept int

	require.Eventually(t, func() bool {
		next := countRows(t, store, busiest)
		settled := next > 0 && next == kept
		kept = next

		return settled
	}, settle, historyFlushInterval, "the store never settled on a row count")

	reported := int(reporting / samplePeriod)
	ceiling := int(reporting/stream.DefaultInterval) + 1

	t.Logf("observed: a printer reported for %s at one reading per %s, about %d readings per metric; "+
		"%s holds %d rows for its busiest metric, where a conflating feed could hold at most %d",
		reporting.Round(time.Millisecond), samplePeriod, reported, store, kept, ceiling)

	require.Greater(t, kept, ceiling,
		"the store holds one reading per coalescing window, which is a sample of the series and not the series")

	// And close to what was reported rather than merely above the ceiling. Half
	// is the slack for the moment between the process starting and the printer
	// being admitted, which is inside the window this counts against.
	require.Greater(t, kept, reported/2,
		"the store holds far fewer readings than the printer reported")
}

// dialInReporting dials one simulated printer in at samplePeriod and returns the
// call that stops it. It is dialInFleet with a cadence: the default is 250ms,
// which is slower than the coalescing window and so conflates nothing.
func dialInReporting(t *testing.T, running *process, serial string) func() {
	t.Helper()

	printer, err := simulator.New(simulator.Config{
		Model:      "MK4",
		Serial:     serial,
		BaseURL:    "http://" + running.addr,
		IdlePeriod: samplePeriod,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan error, 1)

	go func() { done <- printer.RegisterAndRun(ctx) }()

	stopped := false
	stop := func() {
		if stopped {
			return
		}

		stopped = true

		cancel()
		<-done
	}

	t.Cleanup(stop)

	require.Eventually(t, func() bool {
		listed := printers(t, running)

		return len(listed) == 1 && telemetered(listed[0].State)
	}, settle, 25*time.Millisecond, "the simulated printer never appeared in the listing")

	return stop
}
