package app

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/deviceissue"
	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// TestTheFleetListingReportsADialogAsAnIssueOverHTTP is the whole path at once:
// a real process, a printer that dialled in over a real socket, the frame a
// firmware sends when it stops and asks a person something, and the bytes a
// browser then reads.
//
// Every other case for this feature reaches the device plane directly. This one
// exists because the plane is not what a client talks to: the route, the
// authorization middleware, the projection and the JSON encoder are between
// them, and an issue that never reaches the response body is an issue nobody
// sees.
//
// The frame is written onto the socket by hand for the reason
// TestADialogTheMachineRaisedIsReadableThroughTheAPI writes one: the simulator
// raises no dialog, and what matters here is the frame a real machine sends.
func TestTheFleetListingReportsADialogAsAnIssueOverHTTP(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	printer := runSimulator(t, running, simulator.Config{
		Model:    "MK4",
		Serial:   "SIM-ISSUES",
		Firmware: firmwareInsideTheProfile,
	}, "")

	quiet := fleetPage(t, running)
	require.Len(t, quiet.Items, 1)
	require.Empty(t, quiet.Items[0].Issues, "a printer that is answering reports something wrong")
	require.Zero(t, quiet.Issues.Affected)

	id := quiet.Items[0].ID

	takeOffTheWire(t, running, printer, id)

	conn := dialAsPrinter(t, running, printer.printer)

	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(
		`{"event":"STATE_CHANGED","state":"ATTENTION","dialog_id":42,`+
			`"data":{"code":"13501","title":"Filament runout",`+
			`"text":"Insert filament, then press Retry.","buttons":["Retry","Cancel"]}}`)))

	var stopped httpapi.DevicePage

	want := []httpapi.DeviceIssue{{
		Reason:   deviceissue.ReasonAwaitingOperator.String(),
		Severity: deviceissue.SeverityError.String(),
		Detail:   "Filament runout",
	}}

	// The wait is for the whole dialog issue and nothing else. takeOffTheWire
	// leaves a disconnected issue standing until the frame above is read. The
	// state and the dialog's title can also reach the listing apart, so a wait
	// on the reason alone can read the issue before its detail is filled in.
	require.Eventually(t, func() bool {
		stopped = fleetPage(t, running)

		return len(stopped.Items) == 1 && reflect.DeepEqual(stopped.Items[0].Issues, want)
	}, settle, 25*time.Millisecond,
		"the dialog never reached the listing as %+v; it stands at %+v", want, stopped.Items)

	body, err := json.Marshal(stopped.Issues)
	require.NoError(t, err)
	t.Logf("observed: GET /api/v1/devices reports %+v and rolls it up as %s",
		stopped.Items[0].Issues, body)

	require.Equal(t, 1, stopped.Issues.Affected)
	require.Equal(t, 1, stopped.Issues.Counts[deviceissue.ReasonAwaitingOperator.String()])

	// The filter answers the question a person actually asks, and it answers it
	// with the same printer.
	narrowed := fleetPageWith(t, running, "?with_issues=true")
	require.Len(t, narrowed.Items, 1)
	require.Equal(t, id, narrowed.Items[0].ID)
	require.Equal(t, 1, narrowed.Total)

	// The vocabulary a client reads the sentence from is served by the same
	// process, so a browser that met the token has somewhere to look it up.
	read := running.get(t, "/api/v1/vocabularies/device-issues")
	require.Equal(t, http.StatusOK, read.status, read.body)
	require.Contains(t, read.body, deviceissue.ReasonAwaitingOperator.String())
	require.Contains(t, read.body, "waiting for a person")

	// The machine leaving the state clears the dialog, and the issue goes with
	// it. An issue that outlives its cause sends somebody to a printer that is
	// printing.
	require.NoError(t, conn.WriteMessage(websocket.TextMessage,
		[]byte(`{"state":"IDLE","temp_nozzle":215}`)))

	require.Eventually(t, func() bool {
		return fleetPage(t, running).Issues.Affected == 0
	}, settle, 25*time.Millisecond, "the issue outlived the dialog that raised it")

	t.Logf("observed: IDLE left the listing with nothing wrong")
}

// TestAPrinterSpeakingAnUnreadableDialectIsReportedAsAnIssue is the link half
// of the set, and it is the half a table of thresholds cannot prove.
//
// A printer running firmware this build does not fully understand connects,
// stays connected and reports nothing, and every other member of its record
// reads exactly like an idle machine. The refusal rate is what separates them,
// and without this report a person has to know to look at it. The counters behind that
// rate belong to the driver, so the case drives real frames onto a real socket
// rather than asserting against a fake that counts what it is told to.
func TestAPrinterSpeakingAnUnreadableDialectIsReportedAsAnIssue(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	printer := runSimulator(t, running, simulator.Config{
		Model:    "MK4",
		Serial:   "SIM-DIALECT-ISSUE",
		Firmware: firmwareInsideTheProfile,
	}, "")

	quiet := fleetPage(t, running)
	require.Len(t, quiet.Items, 1)
	require.True(t, quiet.Items[0].Link.Counted,
		"the driver behind this printer counts no frames, so this case measures nothing")
	require.Empty(t, quiet.Items[0].Issues)

	id := quiet.Items[0].ID

	takeOffTheWire(t, running, printer, id)

	// The same machine comes back speaking something this build has no reading
	// of, on the session the driver is already holding.
	conn := dialAsPrinter(t, running, printer.printer)

	for range unreadableFrames {
		require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(unreadableFrame)))
	}

	var noisy httpapi.DevicePage

	require.Eventually(t, func() bool {
		noisy = fleetPage(t, running)

		return noisy.Issues.Counts[deviceissue.ReasonFramesRefused.String()] == 1
	}, settle, 25*time.Millisecond,
		"the printer sent %d frames this build cannot read and the listing reports %+v",
		unreadableFrames, noisy.Items)

	require.Len(t, noisy.Items, 1)
	require.True(t, noisy.Items[0].Connected,
		"a departure would raise a different reason, and this case is about a connected machine")
	require.Positive(t, noisy.Items[0].Link.RefusedFramesPerMinute)

	require.Equal(t, []httpapi.DeviceIssue{{
		Reason:   deviceissue.ReasonFramesRefused.String(),
		Severity: deviceissue.SeverityWarning.String(),
	}}, noisy.Items[0].Issues)

	require.Equal(t, 1, noisy.Issues.Affected)

	t.Logf("observed: %d unreadable frames put %+v on a connected printer, at %.0f a minute",
		unreadableFrames, noisy.Items[0].Issues, noisy.Items[0].Link.RefusedFramesPerMinute)
}

// fleetPage reads the listing through the published API, which is the same
// bytes a client reads.
func fleetPage(t *testing.T, running *process) httpapi.DevicePage {
	t.Helper()

	return fleetPageWith(t, running, "")
}

func fleetPageWith(t *testing.T, running *process, query string) httpapi.DevicePage {
	t.Helper()

	read := running.get(t, "/api/v1/devices"+query)
	require.Equal(t, http.StatusOK, read.status, read.body)

	var page httpapi.DevicePage

	require.NoError(t, json.Unmarshal([]byte(read.body), &page))

	return page
}
