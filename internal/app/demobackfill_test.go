package app

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/devicestore"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

func TestTheBackfillIsTheSameEveryRun(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	printers := []demoBackfillPrinter{{ID: "a", FailShare: 0.05}, {ID: "b", FailShare: 0.25}}

	first, firstJobs, err := demoBackfill(now, printers)
	require.NoError(t, err)
	second, secondJobs, err := demoBackfill(now, printers)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, firstJobs, secondJobs)
}

func TestTheBackfillReadsLikeAFleetThatWorks(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	printers := []demoBackfillPrinter{
		{ID: "steady", FailShare: 0.05},
		{ID: "problem", FailShare: 0.25},
	}

	facts, jobs, err := demoBackfill(now, printers)
	require.NoError(t, err)
	require.Len(t, jobs, len(facts), "the prints and the history tell a different past")

	type tally struct{ jobs, failed int }

	seen := map[string]*tally{}
	lastEnded := map[string]time.Time{}

	for _, fact := range facts {
		require.Equal(t, event.TypeDeviceJobSettled, fact.Type)
		require.Equal(t, demoBackfillEpoch, fact.Epoch)

		var settled event.JobSettled
		require.NoError(t, json.Unmarshal(fact.Payload, &settled))

		row := jobs[indexOf(jobs, settled.JobID)]
		require.Equal(t, fact.Ref.ID(), row.DeviceID)
		require.Equal(t, settled.Outcome, row.Outcome, "a print kept another outcome than its fact")
		require.True(t, settled.Ended.Equal(row.Ended))

		id := fact.Ref.ID()
		if seen[id] == nil {
			seen[id] = &tally{}
		}

		seen[id].jobs++
		if settled.Outcome == event.JobFailed {
			seen[id].failed++
		}

		require.False(t, settled.Started.Before(lastEnded[id]), "prints on %s overlap", id)
		require.True(t, settled.Ended.After(settled.Started))
		require.True(t, settled.Ended.Before(now.Add(-10*time.Minute)), "a backfilled print ends too late")
		require.False(t, settled.Started.Before(now.AddDate(0, 0, -demoBackfillDays)))

		lastEnded[id] = settled.Ended
	}

	require.GreaterOrEqual(t, seen["steady"].jobs, 50)
	require.GreaterOrEqual(t, seen["problem"].jobs, 50)

	steady := float64(seen["steady"].failed) / float64(seen["steady"].jobs)
	problem := float64(seen["problem"].failed) / float64(seen["problem"].jobs)
	require.Less(t, steady, 0.12)
	require.InDelta(t, 0.25, problem, 0.1)
}

// indexOf finds a backfilled print by its id.
func indexOf(jobs []devicestore.PrinterJob, id string) int {
	return slices.IndexFunc(jobs, func(job devicestore.PrinterJob) bool { return job.JobID == id })
}
