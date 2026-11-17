package devicestore

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// jobIDsOf is the ids of a page, in the order the page carries them.
func jobIDsOf(rows []PrinterJob) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.JobID)
	}

	return ids
}

// twoMachinesPrinting is twoMachinesOneGrant with a print running on each
// machine, started after every print that has ended.
func twoMachinesPrinting(t *testing.T, store fixture) {
	t.Helper()

	twoMachinesOneGrant(t, store)

	alpha := aRunningJob("p-alpha", "3")
	alpha.Started = startedAt.Add(6 * time.Hour)
	beta := aRunningJob("p-beta", "10")
	beta.Started = startedAt.Add(7 * time.Hour)

	require.NoError(t, store.SaveDeviceJobs(t.Context(), alpha, beta))
}

// The page across the fleet carries the running print with the ended ones, and
// only on the devices the subject may observe.
func TestTheFleetJobPageCarriesOnlyThePrintsTheSubjectMayObserve(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		twoMachinesPrinting(t, store)

		rows, err := store.FleetJobs(t.Context(), subjectOf(t, "user:ivy"), JobFilter{Limit: 50})
		require.NoError(t, err)
		require.Equal(t, []string{"p-alpha:3", "p-alpha:2", "p-alpha:1"}, jobIDsOf(rows),
			"the page carries a print of a machine the subject reaches nothing on, or leaves the running one out")
		require.True(t, rows[0].Running(), "the newest print is the one on the bed")

		none, err := store.FleetJobs(t.Context(), subjectOf(t, "user:mallory"), JobFilter{Limit: 50})
		require.NoError(t, err)
		require.Empty(t, none, "a subject with no grant read somebody else's prints")
	})
}

// The phase narrows the page to the running prints or the ended ones, and the
// device narrows it to one machine. A device the subject may not observe
// answers no row even when it is named.
func TestTheFleetJobPageIsNarrowedByPhaseAndDevice(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		twoMachinesPrinting(t, store)

		ivy := subjectOf(t, "user:ivy")

		cases := map[string]struct {
			filter JobFilter
			want   []string
		}{
			"running": {JobFilter{Phase: JobPhaseRunning, Limit: 50}, []string{"p-alpha:3"}},
			"ended": {
				JobFilter{Phase: JobPhaseEnded, Limit: 50},
				[]string{"p-alpha:2", "p-alpha:1"},
			},
			"one device the subject observes": {
				JobFilter{DeviceID: "p-alpha", Limit: 50},
				[]string{"p-alpha:3", "p-alpha:2", "p-alpha:1"},
			},
			"ended on one device": {
				JobFilter{DeviceID: "p-alpha", Phase: JobPhaseEnded, Limit: 50},
				[]string{"p-alpha:2", "p-alpha:1"},
			},
			"one device it may not observe": {JobFilter{DeviceID: "p-beta", Limit: 50}, []string{}},
			"running where it may not observe": {
				JobFilter{DeviceID: "p-beta", Phase: JobPhaseRunning, Limit: 50}, []string{},
			},
		}

		for name, tc := range cases {
			rows, err := store.FleetJobs(t.Context(), ivy, tc.filter)
			require.NoErrorf(t, err, "%s", name)
			require.Equalf(t, tc.want, jobIDsOf(rows), "%s", name)
		}
	})
}

// A page resumes where the last one stopped, across the running print and the
// ended ones alike.
func TestTheFleetJobPageResumesAfterTheCursor(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		twoMachinesPrinting(t, store)

		ivy := subjectOf(t, "user:ivy")

		first, err := store.FleetJobs(t.Context(), ivy, JobFilter{Limit: 2})
		require.NoError(t, err)
		require.Equal(t, []string{"p-alpha:3", "p-alpha:2"}, jobIDsOf(first))

		last := first[len(first)-1]

		next, err := store.FleetJobs(t.Context(), ivy, JobFilter{
			AfterStarted: last.Started, AfterID: last.JobID, Limit: 50,
		})
		require.NoError(t, err)
		require.Equal(t, []string{"p-alpha:1"}, jobIDsOf(next))
	})
}

// The page size a caller asks for never decides how much memory this server
// reserves, and a phase outside the three is refused rather than answered with
// an empty page.
func TestTheFleetJobPageIsBounded(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		twoMachinesPrinting(t, store)

		ivy := subjectOf(t, "user:ivy")

		rows, err := store.FleetJobs(t.Context(), ivy, JobFilter{Limit: 50_000_000})
		require.NoError(t, err)
		require.LessOrEqual(t, cap(rows), maxPageLimit,
			"a caller's page size decided the size of the allocation")

		_, err = store.FleetJobs(t.Context(), ivy, JobFilter{Phase: "paused", Limit: 50})
		require.Error(t, err, "a phase outside the three read as a fleet that printed nothing")
	})
}

// Every page walks an index in page order and stops at the limit. SQLite names
// a sort in its plan as a temporary B-tree; Postgres chooses by statistics and
// prints no comparable line, so only SQLite is read.
func TestEveryFleetJobPageWalksAnIndex(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		if e.Name != "sqlite" {
			return
		}

		store := newStore(t, e)

		for onDevice, phases := range fleetJobsStatements {
			for phase, statement := range phases {
				plan := queryPlan(t, store, statement)

				joined := strings.Join(plan, "\n")
				require.NotContainsf(t, joined, "TEMP B-TREE",
					"device %v phase %q sorts the prints instead of walking an index:\n%s", onDevice, phase, joined)
				require.Containsf(t, joined, "USING INDEX printer_job_",
					"device %v phase %q reads printer_job through no index:\n%s", onDevice, phase, joined)
			}
		}
	})
}

// queryPlan is the lines SQLite's EXPLAIN QUERY PLAN answers for one statement.
func queryPlan(t *testing.T, store fixture, statement string) []string {
	t.Helper()

	rows, err := store.db.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+statement,
		make([]any, strings.Count(statement, "?"))...)
	require.NoError(t, err)

	defer func() { require.NoError(t, rows.Close()) }()

	var plan []string

	for rows.Next() {
		var id, parent, unused int

		var detail string

		require.NoError(t, rows.Scan(&id, &parent, &unused, &detail))
		plan = append(plan, detail)
	}

	require.NoError(t, rows.Err())

	return plan
}
