package app

import (
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
)

// unreadableFrames is how many frames the printer in this file sends that this
// build has no reading of. It is small because the assertion is on the number
// reaching a reader and not on the driver's arithmetic, which
// internal/connector/xbuddy/hostile_test.go already holds.
const unreadableFrames = 24

// unreadableFrame is one frame a printer can put on the wire that this dialect
// cannot read: the right key with the wrong spelling. It is the inbound half of
// the simulator's "a lower-case type letter", and it is decoded here as bytes
// rather than through the driver's own codec so that an encoder bug cannot
// agree with itself on both sides.
const unreadableFrame = `{"event":123}`

// TestAPrinterSpeakingADialectThisBuildCannotReadSaysSoWhereAPersonLooks is the
// bench case the refusal counter exists for.
//
// A printer running firmware this build does not fully understand connects,
// stays connected and reports nothing. Every other observation reads exactly
// like a printer that is idle: the session is open, the capability set
// resolved, and the state tree stopped moving. The driver counts every
// unreadable frame, so a count nothing outside the driver's package reads
// leaves the two cases indistinguishable to the person standing at the
// machine.
//
// The assertion is on /api/v1/devices, which is what the developer view at
// /dev renders. A unit test on Session.Refused proves nothing new.
func TestAPrinterSpeakingADialectThisBuildCannotReadSaysSoWhereAPersonLooks(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	printer := runSimulator(t, running, simulator.Config{
		Model:    "MK4",
		Serial:   "SIM-DIALECT",
		Firmware: firmwareInsideTheProfile,
	}, "")

	listed := printers(t, running)
	require.Len(t, listed, 1)

	before := listed[0]
	require.True(t, before.Link.Counted,
		"the driver behind this printer counts no frames, so this case measures nothing")
	require.Zero(t, before.Link.RefusedFrames,
		"a printer speaking the dialect this build reads had frames refused, so the count is not the count")

	takeOffTheWire(t, running, printer, before.ID)

	// The same machine comes back speaking something this build has no reading
	// of. The identity is the one it registered under, so the driver reattaches
	// the session that is already there rather than announcing a second printer,
	// and the counter this test reads is that session's.
	conn := dialAsPrinter(t, running, printer.printer)

	for range unreadableFrames {
		require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(unreadableFrame)))
	}

	require.Eventually(t, func() bool {
		return fetch(t, running, before.ID).Link.RefusedFrames >= unreadableFrames
	}, settle, 25*time.Millisecond,
		"the printer sent %d frames this build cannot read and /api/v1/devices, which is what the "+
			"developer view renders, reports %d", unreadableFrames,
		fetch(t, running, before.ID).Link.RefusedFrames)

	after := fetch(t, running, before.ID)

	require.True(t, after.Connected,
		"the printer must still be connected, or this is a departure and not an unreadable dialect")
	require.Positive(t, after.Link.RefusedFramesPerMinute,
		"the frames were counted and the rate reads zero, so a reader is told the printer is quiet")
	require.Positive(t, after.Link.MeasuredOverSeconds,
		"a rate over no time is a number nobody can check")

	t.Logf("observed: a connected printer sent %d frames this build cannot read, and /api/v1/devices "+
		"reports connected=%v refused_frames=%d refused_frames_per_minute=%.0f measured_over_seconds=%.2f",
		unreadableFrames, after.Connected, after.Link.RefusedFrames,
		after.Link.RefusedFramesPerMinute, after.Link.MeasuredOverSeconds)
}

// dialAsPrinter opens the frozen device wire by hand, as a printer that has
// already registered.
//
// The simulator is a printer that speaks this dialect correctly, which is the
// one thing this case must not have. It registers the machine and hands over
// the credentials; the frames after that are written straight onto the socket,
// which is the only way to put a byte sequence on the wire that no dialect in
// this tree will produce.
func dialAsPrinter(t *testing.T, running *process, printer *simulator.Printer) *websocket.Conn {
	t.Helper()

	header := http.Header{}
	header.Set("Fingerprint", printer.Fingerprint())
	header.Set("Token", printer.Token())
	header.Set("User-Agent-Printer", printer.Model().Name)
	header.Set("User-Agent-Version", firmwareInsideTheProfile)

	dialer := websocket.Dialer{
		Subprotocols:     []string{"prusa-connect"},
		HandshakeTimeout: 10 * time.Second,
	}

	conn, response, err := dialer.DialContext(t.Context(), "ws://"+running.addr+"/p/ws", header)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}

	require.NoError(t, err, "the printer could not get back on the wire")

	t.Cleanup(func() { _ = conn.Close() })

	return conn
}
