package app

import (
	"encoding/base64"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/platform/devicestore"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// Two prints that started inside the same millisecond are two pages, and a
// client walking the cursor reads both. The cursor carries the whole instant
// of the last row, so the statement that resumes the page compares against the
// row the page really stopped at.
func TestThePagesOfAHistoryCarryEveryPrintOfTheSameMillisecond(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	device := arrive(t, plane, "SIM-HISTORY-CURSOR")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, device, authz.RelViewer, readingHistory)))

	// The later print sorts first and its id sorts before the earlier print's,
	// so a cursor rounded down to the millisecond leaves the earlier one out of
	// every page.
	base := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Hour)

	later := devicestore.PrinterJob{
		JobID: device + ":1", DeviceID: device, State: "FINISHED",
		Started: base.Add(400 * time.Microsecond), Ended: base.Add(time.Minute),
	}
	earlier := devicestore.PrinterJob{
		JobID: device + ":2", DeviceID: device, State: "FINISHED",
		Started: base, Ended: base.Add(time.Minute),
	}

	require.NoError(t, recordsOf(t, graph).SaveDeviceJobs(t.Context(), later, earlier))

	var (
		read   []string
		cursor string
	)

	for range 4 {
		page, err := plane.JobHistory(t.Context(), readingHistory, device,
			httpapi.DeviceJobQuery{Limit: 1, Cursor: cursor})
		require.NoError(t, err)

		for _, job := range page.Items {
			read = append(read, job.ID)
		}

		cursor = page.Cursor
		if cursor == "" {
			break
		}
	}

	require.Equal(t, []string{later.JobID, earlier.JobID}, read,
		"a print the page stopped one tick before was stepped over")
}

// A cursor whose instant is written in another unit is refused rather than read
// as an instant in 1970. Read that way it answers no prints and no cursor,
// which a client reads as the end of a history it has not walked.
func TestACursorFromAnotherUnitIsRefused(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	device := arrive(t, plane, "SIM-HISTORY-STALE")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, device, authz.RelViewer, readingHistory)))

	started := time.Now().UTC().Add(-time.Hour)

	require.NoError(t, recordsOf(t, graph).SaveDeviceJobs(t.Context(), devicestore.PrinterJob{
		JobID: device + ":1", DeviceID: device, State: "FINISHED",
		Started: started, Ended: started.Add(time.Minute),
	}))

	inMilliseconds := base64.RawURLEncoding.EncodeToString([]byte(
		strconv.FormatInt(started.UnixMilli(), 10) + jobCursorSeparator + device + ":1"))

	_, err := plane.JobHistory(t.Context(), readingHistory, device,
		httpapi.DeviceJobQuery{Limit: 50, Cursor: inMilliseconds})
	require.Error(t, err, "a cursor in another unit was read as the first page of nothing")
	var raised *fault.Fault
	require.ErrorAs(t, err, &raised, "the refusal carries no code a client can branch on")
	require.Equal(t, "core.validation/REQUEST_INVALID", raised.Code().String())
}

// A printer with no clock reports a start before the floor a cursor is read
// back against. The page ends rather than handing out a cursor the next
// request would refuse, so a client is never stuck on one it cannot use.
func TestAPageEndsRatherThanMintACursorTheParserRefuses(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	device := arrive(t, plane, "SIM-HISTORY-NOCLOCK")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, device, authz.RelViewer, readingHistory)))

	// A board that came up with no clock reports 1970.
	noClock := time.Unix(0, 0).UTC()

	require.NoError(t, recordsOf(t, graph).SaveDeviceJobs(t.Context(),
		devicestore.PrinterJob{
			JobID: device + ":1", DeviceID: device, State: "FINISHED",
			Started: noClock.Add(time.Minute), Ended: noClock.Add(time.Hour),
		},
		devicestore.PrinterJob{
			JobID: device + ":2", DeviceID: device, State: "FINISHED",
			Started: noClock, Ended: noClock.Add(time.Hour),
		},
	))

	page, err := plane.JobHistory(t.Context(), readingHistory, device,
		httpapi.DeviceJobQuery{Limit: 1})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Empty(t, page.Cursor,
		"the page handed out a cursor the next request refuses")

	// The refusal is the proof: a cursor minted from that instant is one this
	// server will not read back.
	_, err = plane.JobHistory(t.Context(), readingHistory, device,
		httpapi.DeviceJobQuery{
			Limit:  1,
			Cursor: renderJobCursor(noClock.Add(time.Minute), device+":1"),
		})
	require.Error(t, err)
}
