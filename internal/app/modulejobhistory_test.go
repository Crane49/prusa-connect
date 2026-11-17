package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/platform/devicestore"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
)

// readingHistory is the instance every case here reads the history as.
const readingHistory = "sink:statistics"

// endedPrint is one finished print on a device, ended at the instant given.
func endedPrint(device, number string, ended time.Time) devicestore.PrinterJob {
	return devicestore.PrinterJob{
		JobID:       device + ":" + number,
		DeviceID:    device,
		Path:        "/usb/part-" + number + ".bgcode",
		DisplayName: "Part " + number,
		State:       "FINISHED",
		Progress:    100,
		Started:     ended.Add(-time.Hour),
		Ended:       ended,
	}
}

// twoPrinters puts two machines in a plane, writes one finished print on each,
// and answers the id of the first and when its print ended. The second machine
// is what a case with no grant on it reads nothing of.
func twoPrinters(t *testing.T, graph *authz.Store, plane *devices) (string, time.Time) {
	t.Helper()

	alpha := arrive(t, plane, "SIM-HISTORY-A")
	beta := arrive(t, plane, "SIM-HISTORY-B")

	ended := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)

	require.NoError(t, recordsOf(t, graph).SaveDeviceJobs(t.Context(),
		endedPrint(alpha, "1", ended),
		endedPrint(beta, "9", ended.Add(time.Minute)),
	))

	return alpha, ended
}

// A module reading the history of the whole fleet is answered the prints of
// the devices its instance may observe, and none of the others. The permission
// is the query's own, so a device it reaches nothing on is absent from the
// rows and from the paging.
func TestAModulesHistoryCarriesOnlyWhatItMayObserve(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	alpha, _ := twoPrinters(t, graph, plane)

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, alpha, authz.RelViewer, readingHistory)))

	fleet := moduleFleet{
		fleet:    plane,
		grants:   graph,
		standing: moduleStanding{principal: readingHistory, ceiling: []authz.Action{authz.ActionObserve}},
	}

	page, err := fleet.JobHistory(t.Context(), deviceapi.JobHistoryParams{Limit: 50})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, alpha+":1", page.Items[0].ID)
}

// A module whose ceiling does not name observe reads nothing, whatever the
// graph granted its instance.
func TestAModulesHistoryIsHeldToItsCeiling(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	alpha, _ := twoPrinters(t, graph, plane)

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, alpha, authz.RelViewer, readingHistory)))

	fleet := moduleFleet{
		fleet:    plane,
		grants:   graph,
		standing: moduleStanding{principal: readingHistory, ceiling: []authz.Action{authz.ActionOperate}},
	}

	_, err := fleet.JobHistory(t.Context(), deviceapi.JobHistoryParams{Limit: 50})
	require.ErrorIs(t, err, deviceapi.ErrDenied)
}

// The range narrows the page to the prints that ended inside it, on the wire
// and on the HTTP route alike, because both reach one query.
func TestTheHistoryRangeNarrowsThePage(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	alpha, ended := twoPrinters(t, graph, plane)

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, alpha, authz.RelViewer, readingHistory)))

	fleet := moduleFleet{
		fleet:    plane,
		grants:   graph,
		standing: moduleStanding{principal: readingHistory, ceiling: []authz.Action{authz.ActionObserve}},
	}

	inside, err := fleet.JobHistory(t.Context(), deviceapi.JobHistoryParams{
		From: ended.Add(-time.Minute), To: ended.Add(time.Minute), Limit: 50,
	})
	require.NoError(t, err)
	require.Len(t, inside.Items, 1, "the range that holds the print answered nothing")

	outside, err := fleet.JobHistory(t.Context(), deviceapi.JobHistoryParams{
		From: ended.Add(time.Hour), Limit: 50,
	})
	require.NoError(t, err)
	require.Empty(t, outside.Items)

	// The same range through the HTTP plane, on the device it names.
	page, err := plane.JobHistory(t.Context(), readingHistory, alpha, httpapi.DeviceJobQuery{
		From: ended.Add(-time.Minute), To: ended.Add(time.Minute), Limit: 50,
	})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
}
