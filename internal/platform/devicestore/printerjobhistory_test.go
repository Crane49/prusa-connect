package devicestore

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// endedAt makes one finished print, started at an offset from startedAt and
// ended an hour later.
func endedAt(device, number string, offset time.Duration) PrinterJob {
	job := aRunningJob(device, number)
	job.Started = startedAt.Add(offset)
	job.State = "FINISHED"
	job.Ended = job.Started.Add(time.Hour)

	return job
}

// twoMachinesOneGrant registers two printers, writes prints on both, and gives
// one person a viewer grant on the first alone.
func twoMachinesOneGrant(t *testing.T, store fixture) {
	t.Helper()

	aRegisteredDevice(t, store, "p-alpha", "Alpha")
	aRegisteredDevice(t, store, "p-beta", "Beta")

	require.NoError(t, store.SaveDeviceJobs(t.Context(),
		endedAt("p-alpha", "1", 0),
		endedAt("p-alpha", "2", 2*time.Hour),
		endedAt("p-beta", "9", 4*time.Hour),
	))

	require.NoError(t, store.Write(t.Context(), mustTuple(t, "device:p-alpha#viewer@user:ivy")))
}

// The permission is resolved inside the query, so a page over the whole fleet
// carries the prints of the devices the subject may observe and nothing of the
// others.
func TestTheHistoryPageCarriesOnlyThePrintsTheSubjectMayObserve(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		twoMachinesOneGrant(t, store)

		rows, err := store.EndedJobs(t.Context(), subjectOf(t, "user:ivy"), JobWindow{Limit: 50})
		require.NoError(t, err)

		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.JobID)
		}

		require.Equal(t, []string{"p-alpha:2", "p-alpha:1"}, ids,
			"the page carries a print of a machine the subject reaches nothing on")
		require.JSONEq(t, endedAt("p-alpha", "2", 0).Objects, rows[0].Objects,
			"the history page lost the objects a finished print ended with")

		// The refusal's other direction: a person the graph gave nothing reads
		// an empty page rather than somebody else's prints.
		none, err := store.EndedJobs(t.Context(), subjectOf(t, "user:mallory"), JobWindow{Limit: 50})
		require.NoError(t, err)
		require.Empty(t, none)
	})
}

// A page over one device is the same query with the device named, and a device
// the subject reaches nothing on carries no row even when it is named.
func TestTheHistoryPageOverOneDeviceIsResolvedTheSameWay(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		twoMachinesOneGrant(t, store)

		ivy := subjectOf(t, "user:ivy")

		mine, err := store.EndedJobs(t.Context(), ivy, JobWindow{DeviceID: "p-alpha", Limit: 50})
		require.NoError(t, err)
		require.Len(t, mine, 2)

		theirs, err := store.EndedJobs(t.Context(), ivy, JobWindow{DeviceID: "p-beta", Limit: 50})
		require.NoError(t, err)
		require.Empty(t, theirs, "a device named in the query answered without a grant on it")
	})
}

// From and To bound when the print ended. Either one alone bounds that end
// only, and a range that holds no print answers nothing.
func TestTheHistoryPageIsBoundedByWhenThePrintEnded(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		twoMachinesOneGrant(t, store)

		ivy := subjectOf(t, "user:ivy")

		// p-alpha:1 ended at startedAt+1h and p-alpha:2 at startedAt+3h.
		cases := map[string]struct {
			window JobWindow
			want   []string
		}{
			"from after the first": {
				window: JobWindow{From: startedAt.Add(2 * time.Hour), Limit: 50},
				want:   []string{"p-alpha:2"},
			},
			"to before the second": {
				window: JobWindow{To: startedAt.Add(2 * time.Hour), Limit: 50},
				want:   []string{"p-alpha:1"},
			},
			"a range holding both": {
				window: JobWindow{From: startedAt, To: startedAt.Add(10 * time.Hour), Limit: 50},
				want:   []string{"p-alpha:2", "p-alpha:1"},
			},
			"a range holding neither": {
				window: JobWindow{From: startedAt.Add(20 * time.Hour), Limit: 50},
				want:   nil,
			},
		}

		for name, tc := range cases {
			rows, err := store.EndedJobs(t.Context(), ivy, tc.window)
			require.NoErrorf(t, err, "%s", name)

			ids := make([]string, 0, len(rows))
			for _, row := range rows {
				ids = append(ids, row.JobID)
			}

			if len(tc.want) == 0 {
				require.Emptyf(t, ids, "%s", name)

				continue
			}

			require.Equalf(t, tc.want, ids, "%s", name)
		}
	})
}

// A page resumes where the last one stopped, and the running print is on
// neither: it has no end, and the jobs read already answers it.
func TestTheHistoryPageResumesAndLeavesTheRunningPrintOut(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		twoMachinesOneGrant(t, store)

		running := aRunningJob("p-alpha", "3")
		running.Started = startedAt.Add(6 * time.Hour)
		require.NoError(t, store.SaveDeviceJobs(t.Context(), running))

		ivy := subjectOf(t, "user:ivy")

		first, err := store.EndedJobs(t.Context(), ivy, JobWindow{Limit: 1})
		require.NoError(t, err)
		require.Len(t, first, 1)
		require.Equal(t, "p-alpha:2", first[0].JobID)

		next, err := store.EndedJobs(t.Context(), ivy, JobWindow{
			AfterStarted: first[0].Started, AfterID: first[0].JobID, Limit: 50,
		})
		require.NoError(t, err)
		require.Len(t, next, 1)
		require.Equal(t, "p-alpha:1", next[0].JobID)
	})
}

// The page size a caller asks for never decides how much memory this server
// reserves. A request for a huge page reads maxPageLimit rows and reserves
// room for that many.
func TestTheHistoryPageReservesNoMoreThanItsUpperLimit(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		twoMachinesOneGrant(t, store)

		rows, err := store.EndedJobs(t.Context(), subjectOf(t, "user:ivy"),
			JobWindow{Limit: 50_000_000})
		require.NoError(t, err)
		require.LessOrEqual(t, cap(rows), maxPageLimit,
			"a caller's page size decided the size of the allocation")
	})
}
