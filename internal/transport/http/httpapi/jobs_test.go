package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestTheFleetJobsRouteHandsTheQueryToTheFleet holds the binding: every
// parameter a caller sends reaches the fleet, and the page the fleet answers is
// what the caller reads, running print and cursor included.
func TestTheFleetJobsRouteHandsTheQueryToTheFleet(t *testing.T) {
	t.Parallel()

	fleet := &fakeFleet{printer: examplePrinter(), fleetJobs: exampleJobPage()}
	handler := serverWithFleet(t, fleet).Handler()

	rec := call(t, handler, http.MethodGet,
		"/api/v1/jobs?phase=running&device_id="+examplePrinterID+"&limit=20&cursor="+exampleJobCursor, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var page JobPage

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))
	require.Len(t, page.Items, 3)
	require.Nil(t, page.Items[0].Ended, "the running print lost its open end")
	require.Equal(t, examplePrinterID, page.Items[0].DeviceID)
	require.Equal(t, exampleJobCursor, page.Cursor)

	require.Equal(t, []JobQuery{{
		DeviceID: examplePrinterID, Phase: JobPhaseRunning, Limit: 20, Cursor: exampleJobCursor,
	}}, fleet.fleetJobQueries, "the handler dropped part of the query the caller sent")
}

// TestTheFleetJobsRouteRefusesAPhaseItDoesNotKnow is the other half: a phase
// outside running and ended is refused before the fleet is asked, and never
// answered with an empty page a client would read as a farm that printed
// nothing.
func TestTheFleetJobsRouteRefusesAPhaseItDoesNotKnow(t *testing.T) {
	t.Parallel()

	fleet := &fakeFleet{printer: examplePrinter(), fleetJobs: exampleJobPage()}
	handler := serverWithFleet(t, fleet).Handler()

	rec := call(t, handler, http.MethodGet, "/api/v1/jobs?phase=paused", "")
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "core.validation/REQUEST_INVALID")
	require.Empty(t, fleet.fleetJobQueries, "a phase the route does not know reached the fleet")
}

// TestTheFleetJobsRouteHandsARangeOfEndsToTheFleet holds from and to on the
// binding, and refuses them where they cannot hold a print: after each other,
// or beside phase running, whose prints have no end.
func TestTheFleetJobsRouteHandsARangeOfEndsToTheFleet(t *testing.T) {
	t.Parallel()

	fleet := &fakeFleet{printer: examplePrinter(), fleetJobs: exampleJobPage()}
	handler := serverWithFleet(t, fleet).Handler()

	rec := call(t, handler, http.MethodGet,
		"/api/v1/jobs?phase=ended&from=2026-09-01T00:00:00Z&to=2026-10-01T00:00:00Z", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, []JobQuery{{
		Phase: JobPhaseEnded,
		From:  time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC),
	}}, fleet.fleetJobQueries, "the handler dropped the range the caller sent")

	for _, query := range []string{
		"/api/v1/jobs?from=2026-10-01T00:00:00Z&to=2026-09-01T00:00:00Z",
		"/api/v1/jobs?phase=running&from=2026-09-01T00:00:00Z",
	} {
		refused := call(t, handler, http.MethodGet, query, "")
		require.Equal(t, http.StatusUnprocessableEntity, refused.Code, query)
		require.Contains(t, refused.Body.String(), "core.validation/REQUEST_INVALID", query)
	}

	require.Len(t, fleet.fleetJobQueries, 1, "a range that holds no print reached the fleet")
}
