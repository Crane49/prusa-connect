package app

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
)

// The firmware either side of the range this repository's profiles start at. A
// printer below it is held to what the driver's profiles cover between them; one
// inside it is resolved against its own model's profile.
const (
	firmwareBeforeAnyProfile = "6.0.0"
	firmwareInsideTheProfile = "6.5.0"
)

// TestAPrinterThatReconnectsIsResolvedAgain is an earlier design decision where it is
// observable. The driver keeps a session across a disconnection so a queued
// command survives it, and the host resolves the printer on every connection
// because a disconnection it did not watch can carry a firmware update. A host
// that resolved and then threw the answer away reports the set it decided the
// first time, for the life of the process.
func TestAPrinterThatReconnectsIsResolvedAgain(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	first := runSimulator(t, running, simulator.Config{
		Model:    "MK4",
		Serial:   "SIM-FIRMWARE",
		Firmware: firmwareBeforeAnyProfile,
	}, "")

	listed := printers(t, running)
	require.Len(t, listed, 1)

	before := listed[0]
	require.False(t, before.ProfileMatched,
		"no profile covers firmware %s, so this case proves nothing", firmwareBeforeAnyProfile)

	// The printer is flashed and comes back on the same fingerprint, which is
	// the connection the driver reattaches rather than announcing again.
	takeOffTheWire(t, running, first, before.ID)

	runSimulator(t, running, simulator.Config{
		Model:    "MK4",
		Serial:   "SIM-FIRMWARE",
		Firmware: firmwareInsideTheProfile,
	}, first.printer.Token())

	require.Eventually(t, func() bool {
		return fetch(t, running, before.ID).ProfileMatched
	}, settle, 25*time.Millisecond,
		"the printer came back on firmware a profile covers and the host still reports the old resolution")

	after := fetch(t, running, before.ID)
	require.Equal(t, before.ID, after.ID, "the printer's identity is not stable across a reconnection")
	require.Equal(t, firmwareInsideTheProfile, after.Firmware)
	require.Greater(t, after.CapabilityEpoch, before.CapabilityEpoch,
		"the capability epoch did not move, so a client cannot tell a cached set from the one in force")
	require.NotEqual(t, before.Capabilities, after.Capabilities,
		"the resolved set is the one decided at the first arrival")
}

// TestAPrinterThatReconnectsUnchangedMovesItsEpochAndKeepsItsCapabilities is the
// other half of C5: nothing about the machine changed, the host still cannot
// promise that about time it did not observe, so the epoch moves and the set
// does not.
func TestAPrinterThatReconnectsUnchangedMovesItsEpochAndKeepsItsCapabilities(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	first := runSimulator(t, running, simulator.Config{
		Model:    "MK4",
		Serial:   "SIM-SAMEFIRMWARE",
		Firmware: firmwareInsideTheProfile,
	}, "")

	listed := printers(t, running)
	require.Len(t, listed, 1)

	before := listed[0]
	require.True(t, before.ProfileMatched)

	takeOffTheWire(t, running, first, before.ID)

	runSimulator(t, running, simulator.Config{
		Model:    "MK4",
		Serial:   "SIM-SAMEFIRMWARE",
		Firmware: firmwareInsideTheProfile,
	}, first.printer.Token())

	require.Eventually(t, func() bool {
		return fetch(t, running, before.ID).CapabilityEpoch > before.CapabilityEpoch
	}, settle, 25*time.Millisecond,
		"the printer reconnected and the capability epoch never moved")

	after := fetch(t, running, before.ID)
	require.Equal(t, before.Capabilities, after.Capabilities,
		"the same printer on the same firmware resolved to a different set")
	require.True(t, after.ProfileMatched)
	require.True(t, after.Connected)
}

// simulated is one running simulated printer and the handle that stops it.
type simulated struct {
	printer *simulator.Printer
	stop    func()
}

// takeOffTheWire stops a simulated printer and waits until the core has noticed
// the socket is gone, so what happens next is a reconnection and not a race with
// the connection that is still open.
func takeOffTheWire(t *testing.T, running *process, printer *simulated, id string) {
	t.Helper()

	printer.stop()

	require.Eventually(t, func() bool {
		return !fetch(t, running, id).Connected
	}, settle, 25*time.Millisecond, "the core never noticed the printer had gone")
}

// runSimulator dials a printer in and waits until the core has it. A token is a
// printer that is already approved, which is what a machine that reboots or is
// reflashed presents rather than registering again.
func runSimulator(t *testing.T, running *process, cfg simulator.Config, token string) *simulated {
	t.Helper()

	cfg.BaseURL = "http://" + running.addr
	cfg.ReconnectDelay = 50 * time.Millisecond

	printer, err := simulator.New(cfg)
	require.NoError(t, err)

	if token != "" {
		printer.SetToken(token)
	}

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

		return len(listed) == 1 && listed[0].Connected && telemetered(listed[0].State)
	}, settle, 25*time.Millisecond, "the simulated printer never appeared in the listing")

	return &simulated{printer: printer, stop: stop}
}
