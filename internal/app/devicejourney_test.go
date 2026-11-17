package app

// The device plane's half of the add-a-device and device pages: the name an
// operator gives a machine, the fleet filters and the order, the page of
// finished prints, and retiring a machine somebody took away.

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// TestOnlyAManagerNamesADevice is ADR-20260826-5yj rule 1 against the graph. What a
// person calls a machine is part of the machine's identity, so it enters where
// a label enters: above the operator who runs prints on it.
func TestOnlyAManagerNamesADevice(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arrive(t, plane, "SIM-NAME")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOperator, operator)))
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))

	_, err := plane.SetName(t.Context(), operator, id, "Bench")
	require.Error(t, err, "an operator renamed a machine, which is an administrative act")
	requireClassRefusal(t, err, true)

	_, err = plane.SetName(t.Context(), "user:nobody", id, "Bench")
	requireClassRefusal(t, err, false)

	held, err := plane.SetName(t.Context(), manager, id, "Bench")
	require.NoError(t, err)
	require.Equal(t, "Bench", held)

	// Both callers read it, because reading the name takes what reading the
	// device takes. A name narrows a listing and decides nothing.
	for _, who := range []string{manager, operator} {
		device, readErr := plane.Get(t.Context(), who, id)
		require.NoError(t, readErr)
		require.Equal(t, "Bench", device.Name)
	}
}

// TestAnEmptyNameGivesTheDeviceBackTheOneTheMachineReported is what makes a name
// removable. A device nobody has named reads under the model and the serial the
// machine reported, and clearing the name is how a person gets that back.
func TestAnEmptyNameGivesTheDeviceBackTheOneTheMachineReported(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arrive(t, plane, "SIM-UNNAME")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))

	reported, err := plane.Get(t.Context(), manager, id)
	require.NoError(t, err)
	require.NotEmpty(t, reported.Name)

	_, err = plane.SetName(t.Context(), manager, id, "Bench")
	require.NoError(t, err)

	cleared, err := plane.SetName(t.Context(), manager, id, "")
	require.NoError(t, err)
	require.Equal(t, reported.Name, cleared,
		"clearing the name left the device under something other than what the machine reported")

	device, err := plane.Get(t.Context(), manager, id)
	require.NoError(t, err)
	require.Equal(t, reported.Name, device.Name)
}

// TestANameIsHeldToItsBounds is the three rules the write holds a name to. A
// control character matters most: a name is written into an event payload and a
// log line, and a newline in one forges a frame.
func TestANameIsHeldToItsBounds(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arrive(t, plane, "SIM-BOUNDS")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))

	_, err := plane.SetName(t.Context(), manager, id, "Bench\nOK")
	require.Error(t, err, "a name carrying a newline was written")

	long := make([]byte, maxDeviceNameRunes+1)
	for i := range long {
		long[i] = 'a'
	}

	_, err = plane.SetName(t.Context(), manager, id, string(long))
	require.Error(t, err, "a name over the limit was written")

	// The trim is the third, and it is the one a person cannot see: two names
	// differing only in trailing space read the same on a screen.
	held, err := plane.SetName(t.Context(), manager, id, "  Bench  ")
	require.NoError(t, err)
	require.Equal(t, "Bench", held)
}

// TestTheNameSurvivesTheMachineComingBack is why the name is on the record and
// not on the directory row. That row is rewritten from the reported model and
// serial on every arrival, so a name a person typed there would last until the
// next handshake.
func TestTheNameSurvivesTheMachineComingBack(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arrive(t, plane, "SIM-STICKY")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))

	_, err := plane.SetName(t.Context(), manager, id, "Bench")
	require.NoError(t, err)

	// The same machine again, which is what a reconnection is: the same serial
	// resolves to the same device id and registers the directory row again.
	require.Equal(t, id, arrive(t, plane, "SIM-STICKY"))

	device, err := plane.Get(t.Context(), manager, id)
	require.NoError(t, err)
	require.Equal(t, "Bench", device.Name, "the arrival overwrote the name a person typed")
}

// TestTheListingNarrowsByStateModelAndSearch is every clause of the filter over
// one fleet, so a clause that widened instead of narrowing would show here.
func TestTheListingNarrowsByStateModelAndSearch(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	printingSession := arriveSpeaking(t, plane, "SIM-AAA")
	idleSession := arriveSpeaking(t, plane, "SIM-BBB")

	printing, idle := plane.idOf(printingSession.identity), plane.idOf(idleSession.identity)

	for _, id := range []string{printing, idle} {
		require.NoError(t, graph.Write(t.Context(),
			tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))
	}

	printingSession.report(t, plane, "/state", `"PRINTING"`)
	idleSession.report(t, plane, "/state", `"IDLE"`)

	_, err := plane.SetName(t.Context(), manager, printing, "Brno bench")
	require.NoError(t, err)

	only := func(t *testing.T, query httpapi.DeviceQuery) []string {
		t.Helper()

		page, listErr := plane.List(t.Context(), manager, query)
		require.NoError(t, listErr)
		require.Equal(t, len(page.Items), page.Total, "the total and the page disagree")

		ids := make([]string, 0, len(page.Items))
		for _, item := range page.Items {
			ids = append(ids, item.ID)
		}

		return ids
	}

	require.Equal(t, []string{printing}, only(t, httpapi.DeviceQuery{States: []string{"printing"}}),
		"the state filter is case sensitive, and a machine's own spelling is the machine's")
	require.Equal(t, []string{printing}, only(t, httpapi.DeviceQuery{Search: "brno"}))
	require.Len(t, only(t, httpapi.DeviceQuery{Search: "Prusa MK4"}), 2,
		"the search finds a machine by the model name a page shows for it")
	require.Empty(t, only(t, httpapi.DeviceQuery{Search: "Prusa XL"}))
	require.Len(t, only(t, httpapi.DeviceQuery{Models: []string{"MK4"}}), 2)
	require.Empty(t, only(t, httpapi.DeviceQuery{Models: []string{"XL"}}))
}

// TestTheListingOrdersByTheFieldTheCallerNames is the order over the whole
// authorized population rather than over the page, which is what lets a client
// page through a sorted fleet and walk it once.
func TestTheListingOrdersByTheFieldTheCallerNames(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	first := arrive(t, plane, "SIM-ORDER-1")
	second := arrive(t, plane, "SIM-ORDER-2")

	for _, id := range []string{first, second} {
		require.NoError(t, graph.Write(t.Context(),
			tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))
	}

	_, err := plane.SetName(t.Context(), manager, first, "Zebra")
	require.NoError(t, err)
	_, err = plane.SetName(t.Context(), manager, second, "Alpha")
	require.NoError(t, err)

	names := func(t *testing.T, query httpapi.DeviceQuery) []string {
		t.Helper()

		page, listErr := plane.List(t.Context(), manager, query)
		require.NoError(t, listErr)

		out := make([]string, 0, len(page.Items))
		for _, item := range page.Items {
			out = append(out, item.Name)
		}

		return out
	}

	require.Equal(t, []string{"Alpha", "Zebra"},
		names(t, httpapi.DeviceQuery{Sort: sortByName}))
	require.Equal(t, []string{"Zebra", "Alpha"},
		names(t, httpapi.DeviceQuery{Sort: sortByName, Order: orderDescending}))

	// The order is applied before the page is cut, so the second page of a
	// sorted fleet is the second row and not the second row of the unsorted one.
	require.Equal(t, []string{"Zebra"},
		names(t, httpapi.DeviceQuery{Page: httpapi.Page{Limit: 1, Offset: 1}, Sort: sortByName}))
}

// TestAValueOutsideAClosedSetIsRefused is why the sets are closed. A caller who
// misspelled a sort field and was answered in the default order could not see
// that anything went wrong.
func TestAValueOutsideAClosedSetIsRefused(t *testing.T) {
	t.Parallel()

	plane := planeOn(t, newGraph(t))

	for _, query := range []httpapi.DeviceQuery{
		{Sort: "temperature"},
		{Order: "sideways"},
		{Retired: "maybe"},
		{Severities: []string{"fatal"}},
	} {
		_, err := plane.List(t.Context(), manager, query)
		require.Errorf(t, err, "%+v was answered rather than refused", query)
	}
}

// TestARetiredDeviceLeavesTheListingAndKeepsItsRecord is the whole of what
// retiring promises: the fleet screen is what is here now, and nothing was
// deleted.
func TestARetiredDeviceLeavesTheListingAndKeepsItsRecord(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	kept := arrive(t, plane, "SIM-KEPT")
	gone := arrive(t, plane, "SIM-GONE")

	for _, id := range []string{kept, gone} {
		require.NoError(t, graph.Write(t.Context(),
			tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))
	}

	require.NoError(t, plane.Retire(t.Context(), manager, gone))

	page, err := plane.List(t.Context(), manager, httpapi.DeviceQuery{})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	require.Equal(t, kept, page.Items[0].ID)

	// It is still there to read, under the id its prints and its grants were
	// filed on.
	device, err := plane.Get(t.Context(), manager, gone)
	require.NoError(t, err)
	require.NotNil(t, device.Retired, "the device reads as still naming its machine")
	require.Equal(t, retirementRemovedByOperator, device.Retired.Reason)
	require.False(t, device.Retired.At.IsZero(), "the retirement carries no moment")

	// And a caller asking for the retired rows gets it, which is how the
	// records of a machine that has gone are read.
	retired, err := plane.List(t.Context(), manager, httpapi.DeviceQuery{Retired: retiredOnly})
	require.NoError(t, err)
	require.Equal(t, 1, retired.Total)
	require.Equal(t, gone, retired.Items[0].ID)
}

// TestRetiringADeviceTwiceIsOneRetirement holds the moment. A retirement happens
// once, and a second one would restate the same fact with a later instant.
func TestRetiringADeviceTwiceIsOneRetirement(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arrive(t, plane, "SIM-TWICE")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))

	require.NoError(t, plane.Retire(t.Context(), manager, id))

	first, err := plane.Get(t.Context(), manager, id)
	require.NoError(t, err)

	require.NoError(t, plane.Retire(t.Context(), manager, id))

	second, err := plane.Get(t.Context(), manager, id)
	require.NoError(t, err)
	require.Equal(t, first.Retired.At, second.Retired.At)
}

// TestTheRetiredMachineComesBackUnderANewDeviceID is the other half of a
// retirement. The machine's binding is released, so a printer somebody plugged
// back in is a new device rather than one that revives a record an operator has
// finished with.
func TestTheRetiredMachineComesBackUnderANewDeviceID(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arrive(t, plane, "SIM-RETURN")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))
	require.NoError(t, plane.Retire(t.Context(), manager, id))

	require.NotEqual(t, id, arrive(t, plane, "SIM-RETURN"),
		"the machine came back on the retired id")
}

// TestAJobCursorRoundTrips is the one thing a cursor has to do. It is opaque on
// the wire, and a cursor this server did not mint is refused rather than read as
// the first page.
func TestAJobCursorRoundTrips(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.September, 11, 9, 14, 5, 0, time.UTC)

	started, id, err := parseJobCursor(renderJobCursor(at, "device:41"))
	require.NoError(t, err)
	require.Equal(t, at, started)
	require.Equal(t, "device:41", id)

	empty, none, err := parseJobCursor("")
	require.NoError(t, err)
	require.True(t, empty.IsZero())
	require.Empty(t, none)

	_, _, err = parseJobCursor("not-a-cursor")
	require.Error(t, err, "a cursor this server did not mint was read as a page")
}

// TestTheJobHistoryPagesTheFinishedPrints drives the page over the record store,
// which is where the rows are: the print on the bed has no end and never
// appears, and the cursor walks the rest once.
func TestTheJobHistoryPagesTheFinishedPrints(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	session := arriveSpeaking(t, plane, "SIM-HISTORY")
	id := plane.idOf(session.identity)

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))

	// Three prints, written as the flush writes them: the machine reports a job
	// number, then reports it finished, and the row is derived from the tree.
	for number := 1; number <= 3; number++ {
		runOneJob(t, plane, session, number)
	}

	first, err := plane.JobHistory(t.Context(), manager, id, httpapi.DeviceJobQuery{Limit: 2})
	require.NoError(t, err)
	require.Len(t, first.Items, 2)
	require.NotEmpty(t, first.Cursor, "a page with more behind it answered no cursor")

	next, err := plane.JobHistory(t.Context(), manager, id,
		httpapi.DeviceJobQuery{Limit: 2, Cursor: first.Cursor})
	require.NoError(t, err)
	require.Len(t, next.Items, 1)
	require.Empty(t, next.Cursor, "the last page answered a cursor")

	seen := map[string]bool{}
	for _, job := range append(append([]httpapi.DeviceJob{}, first.Items...), next.Items...) {
		require.NotNilf(t, job.Ended, "%s is on the history page and has not ended", job.ID)
		require.Falsef(t, seen[job.ID], "%s was answered on two pages", job.ID)

		seen[job.ID] = true
	}

	require.Len(t, seen, 3)
}

// runOneJob makes the machine report one print and then finish it, and writes
// the rows the two readings produce.
//
// The flush is called rather than waited for. It is the same function the
// timer calls, so what the store ends up holding is what a running process
// would hold, without the case sleeping for a tick.
func runOneJob(t *testing.T, plane *devices, session *speakingSession, number int) {
	t.Helper()

	session.report(t, plane, "/job/id", jsonNumber(number))
	session.report(t, plane, "/job/path", `"/usb/part.bgcode"`)
	session.report(t, plane, "/state", `"PRINTING"`)
	plane.flush()

	session.report(t, plane, "/state", `"FINISHED"`)
	plane.flush()
}

// jsonNumber renders one number the way a printer encodes it.
func jsonNumber(value int) string {
	encoded, _ := json.Marshal(value)

	return string(encoded)
}

// TestTheListingPutsTheWorstFirstUnlessAskedOtherwise is the order a farm
// reads its fleet in: what is broken, then what waits for a person, then what
// is silent, then what is fine. Ties keep the id order, so a page boundary
// never moves between two requests.
func TestTheListingPutsTheWorstFirstUnlessAskedOtherwise(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	states := []string{"IDLE", "ERROR", "FINISHED", "ATTENTION", "PRINTING"}
	byState := map[string]string{}

	for index, state := range states {
		session := arriveSpeaking(t, plane, fmt.Sprintf("SIM-URG-%d", index))
		id := plane.idOf(session.identity)
		require.NoError(t, graph.Write(t.Context(),
			tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))
		session.report(t, plane, "/state", `"`+state+`"`)
		byState[state] = id
	}

	page, err := plane.List(t.Context(), manager, httpapi.DeviceQuery{})
	require.NoError(t, err)

	require.Equal(t, sortByUrgency, page.Sort)

	got := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		got = append(got, item.ID)
	}

	normal := []string{byState["IDLE"], byState["PRINTING"]}
	slices.Sort(normal)

	require.Equal(t, append([]string{byState["ERROR"], byState["ATTENTION"], byState["FINISHED"]}, normal...), got)
}

// TestTheSeverityFilterFindsAPrinterByAnyOfItsIssues is the strip on the
// printers screen: a click on "errors" lists every printer with an error.
func TestTheSeverityFilterFindsAPrinterByAnyOfItsIssues(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)

	waiting := arriveSpeaking(t, plane, "SIM-SEV-1")
	arriveSpeaking(t, plane, "SIM-SEV-2")
	raiseDialog(t, plane, waiting, "Filament runout")

	all, err := plane.List(t.Context(), fleetOwner, httpapi.DeviceQuery{})
	require.NoError(t, err)

	severity := ""
	for _, item := range all.Items {
		if len(item.Issues) > 0 {
			severity = item.Issues[0].Severity
		}
	}
	require.NotEmpty(t, severity, "the dialog raised no issue")

	only, err := plane.List(t.Context(), fleetOwner, httpapi.DeviceQuery{Severities: []string{severity}})
	require.NoError(t, err)
	require.Equal(t, 1, only.Total)
	require.Equal(t, plane.idOf(waiting.identity), only.Items[0].ID)
}

// TestADepartedPrinterIsSilentWhateverItLastReported keeps a machine that left
// mid-print from sorting with the ones still working. The departing machine is
// given the larger id, so only its rank, and not the id tie-break, can put it
// ahead of the connected idle one.
func TestADepartedPrinterIsSilentWhateverItLastReported(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	arrive := func(serial, state string) (*speakingSession, string) {
		session := arriveSpeaking(t, plane, serial)
		id := plane.idOf(session.identity)
		require.NoError(t, graph.Write(t.Context(),
			tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))
		session.report(t, plane, "/state", `"`+state+`"`)

		return session, id
	}

	first, firstID := arrive("SIM-DEP-A", "PRINTING")
	second, secondID := arrive("SIM-DEP-B", "PRINTING")

	leaving, leavingID, stayingID := first, firstID, secondID
	if secondID > firstID {
		leaving, leavingID, stayingID = second, secondID, firstID
	}

	staying := second
	if leaving == second {
		staying = first
	}

	staying.report(t, plane, "/state", `"IDLE"`)
	closeAndAwaitDeparture(t, plane, leaving)

	page, err := plane.List(t.Context(), manager, httpapi.DeviceQuery{})
	require.NoError(t, err)

	got := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		got = append(got, item.ID)
	}

	require.Equal(t, []string{leavingID, stayingID}, got)
}
