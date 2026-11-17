package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// TestALinkReadingReachesAReaderWhoDidNotAskAgain is the one fact about a
// printer that no other frame carries.
//
// A printer whose frames are all being refused publishes no telemetry, keeps
// its session and reports reachable, so every other frame about it says the
// machine is fine. A reader watching the stream could not learn it at all, and
// the developer view asked the listing again every five seconds to find out.
//
// The assertion is on the published event stream, which is what the page reads.
func TestALinkReadingReachesAReaderWhoDidNotAskAgain(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	printer := runSimulator(t, running, simulator.Config{
		Model:    "MK4",
		Serial:   "SIM-LINK-FRAME",
		Firmware: firmwareInsideTheProfile,
	}, "")

	listed := printers(t, running)
	require.Len(t, listed, 1)

	id := listed[0].ID

	seen := watch(t, running)

	takeOffTheWire(t, running, printer, id)

	// The same machine comes back speaking something this build has no reading
	// of, which is a printer on firmware this version does not fully understand.
	conn := dialAsPrinter(t, running, printer.printer)

	for range unreadableFrames {
		require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(unreadableFrame)))
	}

	require.Eventually(t, func() bool {
		return len(seen.framesOfType(event.TypeDeviceLinkChanged)) > 0
	}, settle, 100*time.Millisecond,
		"the printer sent %d frames this build cannot read and no reader was told",
		unreadableFrames)

	carried := seen.framesOfType(event.TypeDeviceLinkChanged)
	t.Logf("observed: the stream carried %s %s", event.TypeDeviceLinkChanged, carried[0].data)

	// The data line is the whole envelope, and the reading is its data member.
	var envelope struct {
		Data struct {
			Printer string             `json:"device_id"`
			Driver  string             `json:"driver"`
			Link    httpapi.DeviceLink `json:"link"`
		} `json:"data"`
	}

	require.NoError(t, json.Unmarshal([]byte(carried[0].data), &envelope))

	reading := envelope.Data

	require.Equal(t, id, reading.Printer)
	require.NotEmpty(t, reading.Driver)
	require.True(t, reading.Link.Counted, "the frame reports no measurement at all")
	require.Positive(t, reading.Link.RefusedFrames,
		"the frame says the printer refused nothing, so a reader is told it is quiet")

	// The frame carries what the listing carries, so a page holding a printer
	// replaces one member rather than reading the whole roster again.
	require.Equal(t, fetch(t, running, id).Link.RefusedFrames, reading.Link.RefusedFrames)
}

// TestAQuietLinkIsNotAFramePerTick is the cost of reading on a tick. Every
// printer is sampled every interval whether or not anything moved, and a fleet
// of four hundred idle printers publishing a frame each would be the stream's
// whole traffic saying nothing happened.
func TestAQuietLinkIsNotAFramePerTick(t *testing.T) {
	t.Parallel()

	report := &linkReport{}
	quiet := httpapi.DeviceLink{Counted: true}

	for range 10 {
		require.False(t, report.worth(quiet), "a link that has not moved was published")
	}
}

// TestAStormThatStopsTakesItselfOffTheScreen is the frame after the last one.
//
// A reader holds the reading it was last given. If the last frame of a storm
// says four thousand refusals a minute, a reader that is told nothing more
// keeps that on screen for a printer that recovered, and the person watching it
// goes to a machine that is working.
func TestAStormThatStopsTakesItselfOffTheScreen(t *testing.T) {
	t.Parallel()

	report := &linkReport{published: httpapi.DeviceLink{Counted: true}}

	storm := httpapi.DeviceLink{Counted: true, RefusedFrames: 24, RefusedFramesPerMinute: 5708}
	require.True(t, report.worth(storm), "a link that started refusing was not published")

	worse := httpapi.DeviceLink{Counted: true, RefusedFrames: 51, RefusedFramesPerMinute: 6100}
	require.True(t, report.worth(worse), "a storm that is still running stopped being reported")

	// The totals stop moving and the rate falls away with the window.
	settled := httpapi.DeviceLink{Counted: true, RefusedFrames: 51, RefusedFramesPerMinute: 12}
	require.True(t, report.worth(settled), "the storm ended and the reader was left holding the rate")

	over := httpapi.DeviceLink{Counted: true, RefusedFrames: 51}
	require.False(t, report.worth(over), "a printer that has been quiet since is still publishing")
	require.False(t, report.worth(over))
}

// TestADriverThatCountsNothingPublishesNothing holds the third state apart. A
// driver answering neither counter reports counted false, which is no
// measurement rather than a clean link, and nothing about it ever changes.
func TestADriverThatCountsNothingPublishesNothing(t *testing.T) {
	t.Parallel()

	report := &linkReport{}

	for range 5 {
		require.False(t, report.worth(httpapi.DeviceLink{}),
			"a driver that counts nothing published a reading")
	}
}
