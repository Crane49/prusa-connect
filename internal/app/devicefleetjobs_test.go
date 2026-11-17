package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// fleetJobIDs names each print of one page by its device and the job number
// the machine issued, in the order the page carries them.
func fleetJobIDs(page httpapi.JobPage) []string {
	ids := make([]string, 0, len(page.Items))
	for _, job := range page.Items {
		ids = append(ids, job.DeviceID+":"+job.DeviceJobID)
	}

	return ids
}

// twoPrintingMachines arrives two machines, finishes one print on each and
// leaves a second one running on each, and gives the manager a grant on the
// first machine alone.
func twoPrintingMachines(t *testing.T) (*devices, string, string) {
	t.Helper()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	mine := arriveSpeaking(t, plane, "SIM-FLEET-MINE")
	theirs := arriveSpeaking(t, plane, "SIM-FLEET-THEIRS")

	for _, session := range []*speakingSession{mine, theirs} {
		runOneJob(t, plane, session, 1)

		session.report(t, plane, "/job/id", jsonNumber(2))
		session.report(t, plane, "/job/path", `"/usb/second.bgcode"`)
		session.report(t, plane, "/state", `"PRINTING"`)
		plane.flush()
	}

	id := plane.idOf(mine.identity)

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))

	return plane, id, plane.idOf(theirs.identity)
}

// The fleet's jobs carry the running print beside the ended one, on the
// devices the caller may observe and on no other. A caller the graph gave
// nothing reads an empty page and no refusal, so the answer does not say that
// the fleet holds a printer at all.
func TestTheFleetJobsCarryTheRunningPrintOnObservableDevicesOnly(t *testing.T) {
	t.Parallel()

	plane, mine, theirs := twoPrintingMachines(t)

	page, err := plane.ListJobs(t.Context(), manager, httpapi.JobQuery{})
	require.NoError(t, err)
	require.Equal(t, []string{mine + ":2", mine + ":1"}, fleetJobIDs(page),
		"the page left the running print out or carried a machine the caller reaches nothing on")
	require.Nil(t, page.Items[0].Ended, "the print on the bed carries an end")
	require.NotNil(t, page.Items[1].Ended)

	nothing, err := plane.ListJobs(t.Context(), stranger, httpapi.JobQuery{})
	require.NoError(t, err)
	require.Empty(t, nothing.Items, "a caller with no grant read the fleet's prints")

	// Naming a device the caller may not observe answers the empty page a
	// device that never printed answers, never its prints.
	named, err := plane.ListJobs(t.Context(), manager, httpapi.JobQuery{DeviceID: theirs})
	require.NoError(t, err)
	require.Empty(t, named.Items, "a device named in the query answered without a grant on it")
}

// The phase narrows the page, and the cursor walks it once.
func TestTheFleetJobsAreNarrowedByPhaseAndPagedByCursor(t *testing.T) {
	t.Parallel()

	plane, mine, _ := twoPrintingMachines(t)

	running, err := plane.ListJobs(t.Context(), manager,
		httpapi.JobQuery{Phase: httpapi.JobPhaseRunning})
	require.NoError(t, err)
	require.Equal(t, []string{mine + ":2"}, fleetJobIDs(running))

	ended, err := plane.ListJobs(t.Context(), manager,
		httpapi.JobQuery{Phase: httpapi.JobPhaseEnded})
	require.NoError(t, err)
	require.Equal(t, []string{mine + ":1"}, fleetJobIDs(ended))

	first, err := plane.ListJobs(t.Context(), manager, httpapi.JobQuery{Limit: 1})
	require.NoError(t, err)
	require.Equal(t, []string{mine + ":2"}, fleetJobIDs(first))
	require.NotEmpty(t, first.Cursor, "a page with more behind it answered no cursor")

	next, err := plane.ListJobs(t.Context(), manager,
		httpapi.JobQuery{Limit: 1, Cursor: first.Cursor})
	require.NoError(t, err)
	require.Equal(t, []string{mine + ":1"}, fleetJobIDs(next))
	require.Empty(t, next.Cursor, "the last page answered a cursor")

	_, err = plane.ListJobs(t.Context(), manager, httpapi.JobQuery{Cursor: "not-a-cursor"})

	var raised *fault.Fault
	require.ErrorAs(t, err, &raised, "a cursor this server did not mint was read as a page")
	require.Equal(t, "core.validation/REQUEST_INVALID", raised.Code().String())
}

// A print keeps how it ended. The driver's word rides on the write that ends
// the print, the fleet's page carries it, and a print whose machine said
// nothing reads unknown. It is what a report counts successes by, long after
// the settled frame has left the history.
func TestAnEndedPrintCarriesItsOutcome(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	machine := arriveSpeaking(t, plane, "SIM-FLEET-OUTCOME")

	machine.report(t, plane, "/job/id", jsonNumber(1))
	machine.report(t, plane, "/job/path", `"/usb/failed.bgcode"`)
	machine.report(t, plane, "/state", `"PRINTING"`)
	plane.flush()

	machine.report(t, plane, "/job/outcome", `"failed"`)
	machine.report(t, plane, "/state", `"STOPPED"`)
	plane.flush()

	// A driver clears the outcome when the next print starts, as it does
	// every other reading of the print before.
	machine.report(t, plane, "/job/outcome", `""`)
	runOneJob(t, plane, machine, 2)

	id := plane.idOf(machine.identity)
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))

	page, err := plane.ListJobs(t.Context(), manager, httpapi.JobQuery{Phase: httpapi.JobPhaseEnded})
	require.NoError(t, err)
	require.Equal(t, []string{id + ":2", id + ":1"}, fleetJobIDs(page))
	require.Equal(t, "unknown", page.Items[0].Outcome, "a print whose machine said nothing read an outcome")
	require.Equal(t, "failed", page.Items[1].Outcome, "the outcome the print ended with was not kept")
}

// From and to narrow the fleet's page to the prints that ended inside them,
// and the running print, which has not ended, is never on it.
func TestTheFleetJobsAreNarrowedByWhenThePrintEnded(t *testing.T) {
	t.Parallel()

	plane, mine, _ := twoPrintingMachines(t)

	page, err := plane.ListJobs(t.Context(), manager, httpapi.JobQuery{
		From: time.Now().Add(-24 * time.Hour), To: time.Now().Add(24 * time.Hour),
	})
	require.NoError(t, err)
	require.Equal(t, []string{mine + ":1"}, fleetJobIDs(page),
		"a range of ends carried a running print, or left the ended one out")

	later, err := plane.ListJobs(t.Context(), manager, httpapi.JobQuery{
		From: time.Now().Add(time.Hour),
	})
	require.NoError(t, err)
	require.Empty(t, later.Items, "a print that ended before from was on the page")
}
