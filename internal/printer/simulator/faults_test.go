package simulator_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
)

// startPrinting registers and runs the printer, then starts a print of path and
// returns once the printer is printing.
func startPrinting(t *testing.T, host *host, printer *simulator.Printer, path string) {
	t.Helper()

	sessions := make(chan string, 8)
	host.onSession = func(fingerprint string) { sessions <- fingerprint }

	ctx, cancel := context.WithCancel(t.Context())

	require.NoError(t, printer.Register(ctx))

	done := make(chan error, 1)
	go func() { done <- printer.Run(ctx) }()

	t.Cleanup(func() {
		cancel()
		<-done
		host.stop()
	})

	<-sessions

	_, err := host.send(printer.Fingerprint(), "START_PRINT", map[string]any{"path": path})
	require.NoError(t, err)

	synctest.Wait()
	require.True(t, printer.Printing())
}

func TestAFailedJobLeavesThePrinterInError(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		host := newHost(t)
		printer, err := simulator.New(simulator.Config{
			Model: "MK4", BaseURL: host.baseURL(), Dial: host.dial,
			IdlePeriod: time.Hour, PrintingPeriod: time.Second, JobFrames: 1000,
		})
		require.NoError(t, err)
		require.False(t, printer.FailJob(), "an idle printer has no print to fail")

		startPrinting(t, host, printer, "/usb/tangle.bgcode")
		require.True(t, printer.FailJob())
		require.Equal(t, "ERROR", printer.State())
		require.False(t, printer.Printing())
	})
}

func TestTheNextPrintTakesTheFramesItWasGiven(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		host := newHost(t)
		printer, err := simulator.New(simulator.Config{
			Model: "MK4", BaseURL: host.baseURL(), Dial: host.dial,
			IdlePeriod: time.Hour, PrintingPeriod: time.Second, JobFrames: 1000,
		})
		require.NoError(t, err)
		printer.SetJobFrames(3)

		startPrinting(t, host, printer, "/usb/tangle.bgcode")
		// The loop sleeps on the idle period until its first frame.
		time.Sleep(time.Hour + 5*time.Second)
		synctest.Wait()
		require.Equal(t, "FINISHED", printer.State())
	})
}
