package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// reapingConfig is a core whose shipping dialect closes a session shortly after
// the printer stops answering. The key defaults to zero, which lets a session
// outlive any absence, so a test that never sets it has no departure to observe.
const reapingConfig = "core:\n" +
	"  log:\n" +
	"    level: error\n" +
	"  connectors:\n" +
	"    xbuddy:\n" +
	"      type: xbuddy\n" +
	"      config:\n" +
	"        offline_grace: 500ms\n"

// TestAPrinterWhoseSessionEndedIsStillReadableAndSaysWhyItWent is the bench
// case: a machine that was on the list five minutes ago is switched off, and an
// operator asks what happened to it.
//
// Deleting the entry instead drops the printer from the listing and makes the
// id answer 404, which is the answer an id nobody ever registered gets. An
// operator then cannot tell a printer that went away from one that was never
// here, and the last state it reported goes with it.
//
// The assertion is on the published listing and on the published printer path,
// because those are the bytes an operator reads.
func TestAPrinterWhoseSessionEndedIsStillReadableAndSaysWhyItWent(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), reapingConfig)

	printer := runSimulator(t, running, simulator.Config{
		Model:  "MK4",
		Serial: "SIM-DEPART",
	}, "")

	before := printers(t, running)
	require.Len(t, before, 1)
	require.NotEmpty(t, before[0].State, "the printer reported no state, so there is none to keep")

	id := before[0].ID

	printer.stop()

	require.Eventually(t, func() bool {
		return fetch(t, running, id).Departed != nil
	}, settle, 25*time.Millisecond, "the printer's session never ended, so nothing was retained")

	listing := running.get(t, "/api/v1/devices")
	t.Logf("observed: GET /api/v1/devices answered %d %s", listing.status, listing.body)

	after := printers(t, running)
	require.Len(t, after, 1, "a printer whose session ended left the listing")

	require.False(t, after[0].Connected, "a printer with no session reports a session open")
	require.NotNil(t, after[0].Departed, "the listing does not say the printer went")
	require.Equal(t, departureSessionEnded, after[0].Departed.Reason)
	require.False(t, after[0].Departed.At.IsZero(), "the departure carries no time")
	require.Equal(t, before[0].State, after[0].State,
		"the last state the printer reported was not kept")

	// The whole point of the record: the two answers differ. A printer that was
	// here is 200 with a reason, and an id nothing ever answered to is 404.
	never := running.get(t, "/api/v1/devices/00000000-0000-4000-8000-000000000000")
	t.Logf("observed: an id nothing registered answered %d %s", never.status, never.body)
	require.Equal(t, http.StatusNotFound, never.status)
}

// TestACommandForAPrinterWhoseSessionEndedIsRefusedAndNotQueued is the other
// half of keeping the record. A printer that is away with its session open
// queues a command until it comes back; a printer whose session has ended has
// no queue to wait in, and a caller told nothing would wait for ever.
//
// The refusal is this server's, so it is a problem document carrying a
// registered code. It is not the printer's own words: no machine was reached.
func TestACommandForAPrinterWhoseSessionEndedIsRefusedAndNotQueued(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), reapingConfig)

	printer := runSimulator(t, running, simulator.Config{
		Model:  "MK4",
		Serial: "SIM-COMMAND-AFTER",
	}, "")

	listed := printers(t, running)
	require.Len(t, listed, 1)

	id := listed[0].ID

	printer.stop()

	require.Eventually(t, func() bool {
		return fetch(t, running, id).Departed != nil
	}, settle, 25*time.Millisecond, "the printer's session never ended")

	refused := running.call(t, http.MethodPost, "/api/v1/devices/"+id+"/commands",
		`{"verb":"`+nozzleVerb+`","args":{"nozzle_temperature":"215"}}`)

	t.Logf("observed: POST /api/v1/devices/%s/commands answered %d %s", id, refused.status, refused.body)

	require.Equal(t, http.StatusConflict, refused.status, refused.body)
	require.Contains(t, refused.body, fault.PrinterSessionEnded.Code().String(),
		"the refusal carries no code a client can branch on")
	require.NotContains(t, refused.body, "machine_reason",
		"a refusal this server decided carries the machine's own word")

	files := running.get(t, "/api/v1/devices/"+id+"/files")
	t.Logf("observed: GET /api/v1/devices/%s/files answered %d %s", id, files.status, files.body)
	require.Equal(t, http.StatusConflict, files.status, files.body)

	storages := running.get(t, "/api/v1/devices/"+id+"/storages")
	require.Equal(t, http.StatusConflict, storages.status, storages.body)
}

// TestAPrinterThatComesBackIsNoLongerDeparted holds the other direction. A
// record left behind for a machine that is answering again is a screen telling
// an operator the printer is gone while it prints.
func TestAPrinterThatComesBackIsNoLongerDeparted(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)

	id := arrive(t, plane, "SIM-RETURNS")

	entry, live := plane.entry(id)
	require.True(t, live)

	plane.retirePrinter(entry)

	back, seen := plane.known(id)
	require.True(t, seen, "the departure was not recorded")
	require.NotNil(t, back.departure())

	arrive(t, plane, "SIM-RETURNS")

	returned, seen := plane.known(id)
	require.True(t, seen)
	require.Nil(t, returned.departure(), "the printer is answering and still reads as gone")

	page, err := plane.List(t.Context(), fleetOwner, httpapi.DeviceQuery{Page: httpapi.Page{}})
	require.NoError(t, err)
	require.Len(t, page.Items, 1, "one printer that left and came back is two rows")
	require.Nil(t, page.Items[0].Departed)
}

// TestTheDepartedRecordIsBounded is the cost of keeping it. The shipping
// connector admits a printer that presents a fingerprint, so anything that can
// reach the registration path can mint identities faster than an operator can
// look at them, and a record kept for every one of them is a way to fill this
// process's memory with no socket held open at all.
func TestTheDepartedRecordIsBounded(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)

	const over = 25

	for i := range departedLimit + over {
		id := arrive(t, plane, fmt.Sprintf("SIM-%05d", i))

		entry, live := plane.entry(id)
		require.True(t, live)

		plane.retirePrinter(entry)
	}

	plane.mu.Lock()
	held := len(plane.departed)
	plane.mu.Unlock()

	t.Logf("observed: %d printers departed and the plane holds %d records", departedLimit+over, held)

	require.LessOrEqual(t, held, departedLimit, "the departed record grows without bound")

	// The oldest departure is the one dropped, so the printers an operator is
	// most likely still looking for are the ones that are there.
	last := plane.idOf(connectorapi.Identity{
		Vendor: "Prusa", Model: "MK4", Serial: fmt.Sprintf("SIM-%05d", departedLimit+over-1),
	})

	_, seen := plane.known(last)
	require.True(t, seen, "the newest departure was dropped instead of the oldest")
}

// TestADepartureSaysWhyOnTheStream is the same fact on the other transport. A
// reader watching the stream marks the printer as gone, and a reader that has
// to ask the listing why is a reader making a request the frame could have
// saved.
func TestADepartureSaysWhyOnTheStream(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)

	id := arrive(t, plane, "SIM-DEPARTURE-FRAME")

	entry, live := plane.entry(id)
	require.True(t, live)

	plane.retirePrinter(entry)

	payload := departureOf(entry)
	t.Logf("observed: the departure frame carries %s", payload)

	var went struct {
		Printer string `json:"device_id"`
		Reason  string `json:"reason"`
	}

	require.NoError(t, json.Unmarshal(payload, &went))
	require.Equal(t, id, went.Printer)
	require.Equal(t, departureSessionEnded, went.Reason)
}

// TestAPrinterGoingWhileTheServerStopsIsNotReportedAsHavingLeft separates the
// two reasons. Every printer's session ends when this process stops, and
// reporting four hundred machines as having gone away is a screen full of
// alarms about a server that was asked to stop.
func TestAPrinterGoingWhileTheServerStopsIsNotReportedAsHavingLeft(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)

	id := arrive(t, plane, "SIM-SHUTDOWN")

	entry, live := plane.entry(id)
	require.True(t, live)

	plane.mu.Lock()
	plane.closed = true
	plane.mu.Unlock()

	plane.retirePrinter(entry)

	went := entry.departure()
	require.NotNil(t, went)
	require.Equal(t, departureServerStopping, went.Reason)

	t.Logf("observed: a printer released during shutdown departed with reason %q at %s",
		went.Reason, went.At.Format(time.RFC3339Nano))
}
