//go:build observability

package app

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/machinetext"
	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
)

// TestAScrapeCarriesWhatOnePrintersOwnWordsCostTheHost is ADR-20260829-jdq rules 2, 3
// and 6 over a real socket and a real scrape.
//
// Every other gate on these rules drives the device plane directly. This one
// writes the frame onto the wire the way a machine with broken firmware would,
// reads the dialog back through the published API, and then reads the counter an
// operator would have to alert on. The 419 MB this record was written for was
// found by writing a test, and an operator running that build had no reading
// that would have shown it.
func TestAScrapeCarriesWhatOnePrintersOwnWordsCostTheHost(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, withMetrics(grantsConfig(dir)))

	printer := runSimulator(t, running, simulator.Config{
		Model:    "MK4",
		Serial:   "SIM-WIDE-TITLE",
		Firmware: firmwareInsideTheProfile,
	}, "")

	var id string

	require.Eventually(t, func() bool {
		fleet := printers(t, running)
		if len(fleet) == 0 {
			return false
		}

		id = fleet[0].ID

		return true
	}, settle, 25*time.Millisecond, "the printer never reached the listing")

	takeOffTheWire(t, running, printer, id)

	conn := dialAsPrinter(t, running, printer.printer)

	// Half the frame limit the xBuddy dialect admits, so this is a frame the
	// shipping driver accepts rather than one it would have dropped anyway.
	shouted := strings.Repeat("A", 512<<10)

	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(
		`{"event":"STATE_CHANGED","state":"ATTENTION","dialog_id":42,`+
			`"data":{"code":"13501","title":"`+shouted+`",`+
			`"text":"Insert filament, then press Retry.","buttons":["Retry","Cancel"]}}`)))

	var title string

	require.Eventually(t, func() bool {
		title = dialogOf(t, running, id)["/dialog/title"]

		return title != ""
	}, settle, 25*time.Millisecond, "the dialog never reached the state tree")

	t.Logf("observed: the printer put %d bytes at /dialog/title and GET /api/v1/devices/%s reports %d",
		len(shouted), id, len(title))

	require.Less(t, len(title), len(shouted),
		"a printer handed this server half a mebibyte and the API answers with all of it")
	require.LessOrEqual(t, len(title), machinetext.MaxBytes+len(machinetext.Marker)+2)
	require.Contains(t, title, machinetext.Marker,
		"the title was cut and a person reading it cannot tell")

	// The counter is written by the fleet sampler on the record flush's period,
	// so the first walk after the frame is what puts the series in the registry.
	var scraped string

	require.Eventually(t, func() bool {
		read := running.get(t, "/metrics")
		scraped = read.body

		return read.status == http.StatusOK &&
			strings.Contains(scraped, `pcl_printer_state_bounded_total{bound="clipped",driver="xbuddy"}`)
	}, settle, 100*time.Millisecond,
		"a value was clipped and the scrape says nothing about it")

	require.NotContains(t, strings.ReplaceAll(scraped, " ", ""),
		`pcl_printer_state_bounded_total{bound="clipped",driver="xbuddy"}0`,
		"the series is registered and nothing has incremented it")

	running.stopAndWait(t)
}
