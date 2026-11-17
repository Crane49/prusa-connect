package app

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// Two people on one server. Neither is the fleet owner, so each sees exactly
// what the graph hands them and nothing that arrived before they were granted.
const (
	watcher = "user:nora"
	other   = "user:pavel"
)

// TestAPrincipalSeesOnlyThePrintersTheyWereGranted is an earlier design decision at the
// listing. The permission is resolved inside the query that produces the rows,
// so the page, the total and the ids are one answer about one caller.
//
// Both halves are asserted, because a listing that leaks does it through the
// count as readily as through the rows: a caller told there are three printers
// and shown one has been told the fleet is larger than their grant, which is
// the fact the grant exists to withhold.
func TestAPrincipalSeesOnlyThePrintersTheyWereGranted(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	mine := arrive(t, plane, "SIM-MINE")
	yours := arrive(t, plane, "SIM-YOURS")
	nobodys := arrive(t, plane, "SIM-NOBODYS")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, mine, authz.RelOperator, watcher)))
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, yours, authz.RelOperator, other)))

	page, err := plane.List(t.Context(), watcher, httpapi.DeviceQuery{Page: httpapi.Page{}})
	require.NoError(t, err)

	body, err := json.Marshal(page)
	require.NoError(t, err)
	t.Logf("observed: the listing answers %s with %s", watcher, body)

	require.Len(t, page.Items, 1, "a principal was shown printers they hold nothing on")
	require.Equal(t, mine, page.Items[0].ID)
	require.Equal(t, 1, page.Total,
		"the total counts printers this caller may not see, so it discloses the fleet size")

	theirs, err := plane.List(t.Context(), other, httpapi.DeviceQuery{Page: httpapi.Page{}})
	require.NoError(t, err)
	require.Len(t, theirs.Items, 1)
	require.Equal(t, yours, theirs.Items[0].ID)
	require.Equal(t, 1, theirs.Total)

	// The printer nobody was granted is on neither list, so a listing that
	// simply returned everything would have passed the two above.
	require.NotEqual(t, nobodys, page.Items[0].ID)
	require.NotEqual(t, nobodys, theirs.Items[0].ID)

	// Reading one printer answers the same way. A caller who may not see a
	// printer is told the id is not here, in the same bytes an id nothing ever
	// registered gets, so a refusal is never a way to enumerate the fleet.
	_, err = plane.Get(t.Context(), watcher, yours)
	require.Error(t, err)
	require.True(t, fault.HasCode(err, fault.PrinterNotFound.Code()),
		"a printer the caller may not see was refused with something other than absence")

	// A page past the end of what this caller may see is empty and still counts
	// only their own. Paging is the other way a filtered listing leaks.
	beyond, err := plane.List(t.Context(), watcher, httpapi.DeviceQuery{Page: httpapi.Page{Limit: 10, Offset: 10}})
	require.NoError(t, err)
	require.Empty(t, beyond.Items)
	require.Equal(t, 1, beyond.Total, "an empty page reported a total from somebody else's fleet")
}

// TestADepartedPrinterTellsAStrangerNothingAboutItself holds the order the two
// refusals run in.
//
// A printer whose session has ended is refused with a code of its own, and that
// code says the printer exists. Deciding it before the authorization check
// would hand a caller who holds nothing on the printer the one fact the whole
// surface withholds: that this server has been dialled by it.
func TestADepartedPrinterTellsAStrangerNothingAboutItself(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	id := arrive(t, plane, "SIM-STRANGER")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOperator, watcher)))

	entry, live := plane.entry(id)
	require.True(t, live)

	plane.retirePrinter(entry)

	// The principal who holds it is told the session has ended.
	_, err := plane.Command(t.Context(), watcher, id, httpapi.PrinterCommand{Verb: nozzleVerb})
	require.Error(t, err)
	require.True(t, fault.HasCode(err, fault.PrinterSessionEnded.Code()),
		"the caller who holds the printer was not told why it cannot be commanded")

	// The principal who holds nothing is told the id is not here, in the bytes
	// an id nothing ever registered gets.
	_, err = plane.Command(t.Context(), other, id, httpapi.PrinterCommand{Verb: nozzleVerb})
	require.Error(t, err)
	require.True(t, fault.HasCode(err, fault.PrinterNotFound.Code()),
		"a departed printer told a caller who holds nothing that this server has one")

	t.Logf("observed: the same departed printer answers %s to its operator and %s to a stranger",
		fault.PrinterSessionEnded.Code(), fault.PrinterNotFound.Code())
}

// TestAPrinterGrantedAfterItArrivedIsOnTheList holds the direction a listing
// that caches its population gets wrong. A printer is admitted before anybody
// is granted it, which is the ordinary order: a machine dials in and an
// operator is given it afterwards.
func TestAPrinterGrantedAfterItArrivedIsOnTheList(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	id := arrive(t, plane, "SIM-LATER")

	before, err := plane.List(t.Context(), watcher, httpapi.DeviceQuery{Page: httpapi.Page{}})
	require.NoError(t, err)
	require.Empty(t, before.Items)
	require.Zero(t, before.Total)

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOperator, watcher)))

	after, err := plane.List(t.Context(), watcher, httpapi.DeviceQuery{Page: httpapi.Page{}})
	require.NoError(t, err)
	require.Len(t, after.Items, 1)
	require.Equal(t, id, after.Items[0].ID)
}

// TestTheFleetListWalksPastOneScanWindow is the four hundred printers this
// product claims, over a directory the walk reads two hundred rows at a time.
//
// A fleet smaller than one window never runs the loop a second time, so the
// whole of the multi-window walk went untested: the offset, the termination and
// the join all ran exactly once. This fleet crosses two window boundaries.
func TestTheFleetListWalksPastOneScanWindow(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)

	const fleet = fleetScanWindow*2 + 1

	for i := range fleet {
		arrive(t, plane, fmt.Sprintf("SIM-%05d", i))
	}

	const pageSize = 100

	seen := map[string]int{}
	total := 0

	for offset := 0; offset < fleet; offset += pageSize {
		page, err := plane.List(t.Context(), fleetOwner,
			httpapi.DeviceQuery{Page: httpapi.Page{Limit: pageSize, Offset: offset}})
		require.NoError(t, err)
		require.Equal(t, fleet, page.Total, "the total is not the fleet the caller may see")

		total = page.Total

		for _, item := range page.Items {
			seen[item.ID]++
		}
	}

	t.Logf("observed: a fleet of %d walked in pages of %d reported total %d and %d distinct printers",
		fleet, pageSize, total, len(seen))

	require.Len(t, seen, fleet, "the pages did not cover the fleet exactly once")

	for id, count := range seen {
		require.Equal(t, 1, count, "printer %s was returned on more than one page", id)
	}
}

// TestAPrinterArrivingMidWalkNeitherRepeatsNorSkipsAnother is the page boundary
// under concurrent arrival.
//
// The walk reads the authorized directory two hundred rows at a time and joins
// each window against what this process holds. A printer registered between two
// of those queries moves every row after it along by one, so a walk that asked
// for the next window by its offset read one row twice and never read the one
// after it. The listing then reported a fleet of four hundred as four hundred
// printers of which one appeared twice and one was unreachable at any offset.
//
// The arrival is placed deterministically: it happens as the last row of the
// first window is visited, which is after that query returned and before the
// next one is made, and it sorts ahead of every printer already there.
func TestAPrinterArrivingMidWalkNeitherRepeatsNorSkipsAnother(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)

	const fleet = fleetScanWindow*2 + 1

	for i := range fleet {
		arrive(t, plane, fmt.Sprintf("SIM-%05d", i))
	}

	who, err := authz.ParseSubject(fleetOwner)
	require.NoError(t, err)

	seen := map[string]int{}
	visited := 0

	err = plane.walkVisible(t.Context(), who, func(row authz.DeviceRow, _ *device) {
		seen[row.ID]++
		visited++

		if visited == fleetScanWindow {
			// "MK4 AAA-ARRIVED" sorts ahead of every "MK4 SIM-00000", so this
			// row lands at the front of the directory and pushes the rest along.
			arrive(t, plane, "AAA-ARRIVED")
		}
	})
	require.NoError(t, err)

	t.Logf("observed: a printer arrived during the walk and it visited %d rows, %d of them distinct",
		visited, len(seen))

	for id, count := range seen {
		require.Equal(t, 1, count, "printer %s was visited twice in one walk", id)
	}

	// Every printer that was there when the walk began has to be in the answer.
	// One that arrived during it may or may not be, and either is correct: it
	// was not there when the caller asked.
	require.GreaterOrEqual(t, len(seen), fleet,
		"the walk skipped a printer that was there before it started")
}
