package devicestore

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// startedAt is the instant the cases below start a print at. It is chosen the
// way recordedAt is, so a timestamp that came back in the wrong location or
// with a truncated fractional part is a failure rather than a value close
// enough to pass.
var startedAt = time.Date(2026, 8, 28, 9, 14, 22, 0, time.UTC)

// aRunningJob is one print with every column carrying something, so a column
// dropped from the write or from the read is a difference rather than two zero
// values agreeing. It has no end, which is what makes it the current one.
func aRunningJob(device, number string) PrinterJob {
	return PrinterJob{
		JobID:         device + ":" + number,
		DeviceID:      device,
		DeviceJobID:   number,
		Path:          "/usb/tangle-" + number + ".bgcode",
		DisplayName:   "Tangle " + number,
		State:         "PRINTING",
		Progress:      41.5,
		TimeRemaining: 1_320,
		Started:       startedAt,
		Objects:       `{"count":2,"objects":[{"id":0,"cancelled":false},{"id":1,"cancelled":true}]}`,
	}
}

// TestAPrintSurvivesAFreshHandle is the store half of the restart, for a job
// rather than for the device it runs on. The row is written through one handle,
// that handle is closed, and the print is read back through another one.
//
// Writing and reading through one open Store would prove nothing: every value
// would still be in this process's memory, and a store that held its jobs in a
// map would pass.
func TestAPrintSurvivesAFreshHandle(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		handles := e.OpenShared(t, 2)

		first := openFixture(t, e, migrated(t, e, handles[0]))

		aRegisteredDevice(t, first, "p-alpha", "Alpha")
		require.NoError(t, first.SaveDeviceJobs(t.Context(), aRunningJob("p-alpha", "7")))
		require.NoError(t, handles[0].Close())

		// The start a second process makes against the same database.
		second := openFixture(t, e, migrated(t, e, handles[1]))

		found, err := second.DeviceJobs(t.Context(), "p-alpha", currentAndLast)
		require.NoError(t, err)
		require.Len(t, found, 1)

		want := aRunningJob("p-alpha", "7")

		require.Equal(t, want.JobID, found[0].JobID)
		require.Equal(t, want.DeviceID, found[0].DeviceID)
		require.Equal(t, want.DeviceJobID, found[0].DeviceJobID)
		require.Equal(t, want.Path, found[0].Path)
		require.Equal(t, want.DisplayName, found[0].DisplayName)
		require.Equal(t, want.State, found[0].State)
		require.InDelta(t, want.Progress, found[0].Progress, 0.0001)
		require.Equal(t, want.TimeRemaining, found[0].TimeRemaining)
		require.JSONEq(t, want.Objects, found[0].Objects)
		require.True(t, startedAt.Equal(found[0].Started), "started came back as %s", found[0].Started)

		require.True(t, found[0].Running(),
			"a print with no end read back as one that had ended, so the null was lost")
	})
}

// currentAndLast is how many rows the reads below ask for. It is the app
// plane's number, spelled again here rather than imported: internal/authz is
// below internal/app and must not know what the caller does with the answer.
const currentAndLast = 2

// TestAPrintMovesThroughItsStatesAndEndsOnce walks one print the way a machine
// does: it starts, it pauses, it resumes, and it stops. Every step is the same
// row, because the id is derived from the machine's own job number, and the end
// is written once.
//
// The last part is the one a restart depends on. A process that comes back
// derives the same row out of the state tree it restored and writes it again,
// so an upsert that took the newest end would move the end of a print that
// finished before the last shutdown to the moment this one started.
func TestAPrintMovesThroughItsStatesAndEndsOnce(t *testing.T) {
	t.Parallel()

	steps := []struct {
		name     string
		state    string
		progress float64
		ended    time.Time
		running  bool
	}{
		{name: "the print starts", state: "PRINTING", progress: 0, running: true},
		{name: "the operator pauses it", state: "PAUSED", progress: 20, running: true},
		{name: "the operator resumes it", state: "PRINTING", progress: 20, running: true},
		{
			name: "the print finishes", state: "FINISHED", progress: 100,
			ended: startedAt.Add(time.Hour), running: false,
		},
		{
			// The machine goes back to idle after the print. The row keeps the
			// end it already had and keeps saying how the print ended.
			name: "the machine goes idle", state: "FINISHED", progress: 100,
			ended: startedAt.Add(2 * time.Hour), running: false,
		},
	}

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		aRegisteredDevice(t, store, "p-alpha", "Alpha")

		for _, step := range steps {
			t.Run(step.name, func(t *testing.T) {
				job := aRunningJob("p-alpha", "7")
				job.State = step.state
				job.Progress = step.progress
				job.Ended = step.ended

				require.NoError(t, store.SaveDeviceJobs(t.Context(), job))

				found, err := store.DeviceJobs(t.Context(), "p-alpha", currentAndLast)
				require.NoError(t, err)
				require.Len(t, found, 1, "the print became more than one row")

				require.Equal(t, step.state, found[0].State)
				require.InDelta(t, step.progress, found[0].Progress, 0.0001)
				require.Equal(t, step.running, found[0].Running())

				require.True(t, startedAt.Equal(found[0].Started),
					"the start moved to %s", found[0].Started)
			})
		}

		// The end is the first one written and not the last.
		found, err := store.DeviceJobs(t.Context(), "p-alpha", currentAndLast)
		require.NoError(t, err)
		require.True(t, startedAt.Add(time.Hour).Equal(found[0].Ended),
			"the end moved to %s, so a second write over a finished print rewrote it", found[0].Ended)
	})
}

// TestAFinishedPrintIsNotReopenedByALaterWrite is the freeze the upsert makes,
// measured against the write that broke it.
//
// The row is derived from the device's state tree on every flush that moved it,
// and a tree can restate an older reading: a restart reads one back out of the
// record store and the machine reuses its own job numbers, so a print that
// stopped at 17 per cent was rewritten as PRINTING at 8 per cent by a write
// that arrived after it. A person reading the machine's last print was then
// told it is still running.
func TestAFinishedPrintIsNotReopenedByALaterWrite(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		aRegisteredDevice(t, store, "p-alpha", "Alpha")

		finished := aRunningJob("p-alpha", "7")
		finished.State = "STOPPED"
		finished.Progress = 17
		finished.Ended = startedAt.Add(time.Hour)

		require.NoError(t, store.SaveDeviceJobs(t.Context(), finished))

		// The stale write: an earlier reading of the same print, arriving after
		// the one that ended it.
		stale := aRunningJob("p-alpha", "7")
		stale.State = "PRINTING"
		stale.Progress = 8
		stale.Path = "/usb/something-else.bgcode"
		stale.Objects = ""

		require.NoError(t, store.SaveDeviceJobs(t.Context(), stale))

		found, err := store.DeviceJobs(t.Context(), "p-alpha", currentAndLast)
		require.NoError(t, err)
		require.Len(t, found, 1)

		require.Equal(t, "STOPPED", found[0].State,
			"a print that ended was reopened by a later write")
		require.InDelta(t, 17.0, found[0].Progress, 0.0001)
		require.Equal(t, finished.Path, found[0].Path)
		require.JSONEq(t, finished.Objects, found[0].Objects,
			"the objects a finished print ended with were rewritten by a later write")
		require.False(t, found[0].Running())
	})
}

// TestTheJobsReadAnswersTheCurrentPrintAndTheOneBefore is what the route is
// for. A machine that has printed twice and is printing a third time reports
// the third as current and the second as last, and never reports one row as
// both.
func TestTheJobsReadAnswersTheCurrentPrintAndTheOneBefore(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		aRegisteredDevice(t, store, "p-alpha", "Alpha")
		aRegisteredDevice(t, store, "p-beta", "Beta")

		first := aRunningJob("p-alpha", "1")
		first.State = "FINISHED"
		first.Ended = startedAt.Add(time.Hour)

		second := aRunningJob("p-alpha", "2")
		second.Started = startedAt.Add(2 * time.Hour)
		second.State = "STOPPED"
		second.Ended = startedAt.Add(3 * time.Hour)

		third := aRunningJob("p-alpha", "3")
		third.Started = startedAt.Add(4 * time.Hour)

		// A print on another machine, which must not reach this device's
		// answer however recent it is.
		elsewhere := aRunningJob("p-beta", "9")
		elsewhere.Started = startedAt.Add(5 * time.Hour)

		require.NoError(t, store.SaveDeviceJobs(t.Context(), first, second, third, elsewhere))

		found, err := store.DeviceJobs(t.Context(), "p-alpha", currentAndLast)
		require.NoError(t, err)
		require.Len(t, found, currentAndLast, "the read is newest first and bounded")

		require.Equal(t, "p-alpha:3", found[0].JobID)
		require.True(t, found[0].Running())

		require.Equal(t, "p-alpha:2", found[1].JobID)
		require.False(t, found[1].Running())
		require.Equal(t, "STOPPED", found[1].State)

		// A device that has never printed answers nothing rather than
		// answering somebody else's print.
		aRegisteredDevice(t, store, "p-gamma", "Gamma")

		none, err := store.DeviceJobs(t.Context(), "p-gamma", currentAndLast)
		require.NoError(t, err)
		require.Empty(t, none)
	})
}

// TestRemovingADeviceTakesItsPrints is the foreign key doing what the record's
// does. A print is state about a row in the directory, so an id reused after a
// machine is replaced inherits no print, exactly as it inherits no grant.
func TestRemovingADeviceTakesItsPrints(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		aRegisteredDevice(t, store, "p-alpha", "Alpha")

		require.NoError(t, store.SaveDeviceJobs(t.Context(), aRunningJob("p-alpha", "7")))
		require.NoError(t, store.RemoveDevice(t.Context(), "p-alpha"))

		found, err := store.DeviceJobs(t.Context(), "p-alpha", currentAndLast)
		require.NoError(t, err)
		require.Empty(t, found, "the cascade left the print of a device that has gone")

		// The same id registered again is a different machine, and it starts
		// with no history.
		aRegisteredDevice(t, store, "p-alpha", "Alpha again")

		found, err = store.DeviceJobs(t.Context(), "p-alpha", currentAndLast)
		require.NoError(t, err)
		require.Empty(t, found)
	})
}

// TestAPrintForAnUnregisteredDeviceIsRefusedByName is the other half of the
// foreign key. The two engines spell the violation differently, so the refusal
// is classified by asking the directory and has to name the device a caller can
// act on.
func TestAPrintForAnUnregisteredDeviceIsRefusedByName(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		err := store.SaveDeviceJobs(t.Context(), aRunningJob("p-nobody", "7"))

		require.ErrorIs(t, err, ErrPrinterJobUnknownDevice)
		require.Contains(t, err.Error(), "p-nobody",
			"the refusal has to name the device to be actionable")
	})
}

// TestAPrintWithNoIdIsRefusedBeforeTheTransactionOpens holds the two guards the
// write has, both of which are the caller's mistake rather than the database's.
func TestAPrintWithNoIdIsRefusedBeforeTheTransactionOpens(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		job  PrinterJob
	}{
		{name: "no job id", job: PrinterJob{DeviceID: "p-alpha", Started: startedAt}},
		{name: "no device", job: PrinterJob{JobID: "p-alpha:7", Started: startedAt}},
	}

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		aRegisteredDevice(t, store, "p-alpha", "Alpha")

		for _, one := range cases {
			t.Run(one.name, func(t *testing.T) {
				require.Error(t, store.SaveDeviceJobs(t.Context(), one.job))

				found, err := store.DeviceJobs(t.Context(), "p-alpha", currentAndLast)
				require.NoError(t, err)
				require.Empty(t, found, "a refused write left a row behind")
			})
		}
	})
}

// TestWritingNoPrintsIsNotAnError is the empty flush, which happens on every
// tick of a fleet where nothing is printing.
func TestWritingNoPrintsIsNotAnError(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		require.NoError(t, store.SaveDeviceJobs(t.Context()))

		// And a read that asks for nothing answers nothing rather than the
		// whole table.
		found, err := store.DeviceJobs(t.Context(), "p-alpha", 0)
		require.NoError(t, err)
		require.Empty(t, found)
	})
}

// TestTheDeviceRecordCarriesTheStateAndTheCurrentJob is the promotion 0012
// makes. Both columns are written and read by name, so a column dropped from
// either statement is a value that comes back empty rather than two zero values
// agreeing.
func TestTheDeviceRecordCarriesTheStateAndTheCurrentJob(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		state string
		job   string
	}{
		{name: "a machine printing", state: "PRINTING", job: "p-alpha:7"},
		{name: "a machine that stopped", state: "STOPPED", job: ""},
		{name: "a machine nothing has heard from", state: "", job: ""},
	}

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		aRegisteredDevice(t, store, "p-alpha", "Alpha")

		for _, one := range cases {
			t.Run(one.name, func(t *testing.T) {
				record := aFullRecord("p-alpha")
				record.MachineState = one.state
				record.CurrentJobID = one.job

				require.NoError(t, store.SaveDeviceRecords(t.Context(), record))

				found, err := store.DeviceRecords(t.Context())
				require.NoError(t, err)
				require.Len(t, found, 1)

				require.Equal(t, one.state, found[0].MachineState)
				require.Equal(t, one.job, found[0].CurrentJobID)
			})
		}
	})
}

// TestTheLatestPrintOfEveryDeviceIsReadInOneQuery is what the record flush
// loads at start: for each device, the print it began last, ended or not.
func TestTheLatestPrintOfEveryDeviceIsReadInOneQuery(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		aRegisteredDevice(t, store, "p-alpha", "Alpha")
		aRegisteredDevice(t, store, "p-beta", "Beta")
		aRegisteredDevice(t, store, "p-gamma", "Gamma")

		earlier := aRunningJob("p-alpha", "7")
		earlier.Ended = startedAt.Add(time.Hour)

		later := aRunningJob("p-alpha", "8")
		later.Started = startedAt.Add(2 * time.Hour)

		beta := aRunningJob("p-beta", "7")
		beta.Ended = startedAt.Add(time.Hour)

		require.NoError(t, store.SaveDeviceJobs(t.Context(), earlier, later, beta))

		latest, err := store.LatestDeviceJobs(t.Context())
		require.NoError(t, err)
		require.Len(t, latest, 2, "a device with no print has a latest print")

		require.Equal(t, later.JobID, latest["p-alpha"].JobID)
		require.Equal(t, "8", latest["p-alpha"].DeviceJobID)
		require.True(t, latest["p-alpha"].Running())

		require.Equal(t, beta.JobID, latest["p-beta"].JobID)
		require.False(t, latest["p-beta"].Running())
	})
}

// TestAPrintIsReadByItsID is the read behind GET /api/v1/jobs/{job_id}. An id
// the table does not hold is an answer and not an error.
func TestAPrintIsReadByItsID(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		aRegisteredDevice(t, store, "p-alpha", "Alpha")

		job := aRunningJob("p-alpha", "7")
		require.NoError(t, store.SaveDeviceJobs(t.Context(), job))

		found, held, err := store.PrinterJobByID(t.Context(), job.JobID)
		require.NoError(t, err)
		require.True(t, held)
		require.Equal(t, job.DeviceID, found.DeviceID)
		require.Equal(t, job.Path, found.Path)
		require.Equal(t, "7", found.DeviceJobID)

		_, held, err = store.PrinterJobByID(t.Context(), "0f7a3c1e-2b4d-4e6f-8a9b-1c2d3e4f5a6b")
		require.NoError(t, err)
		require.False(t, held)
	})
}

// TestEndingAnAbandonedPrintKeepsEveryReading is the end the flush stamps on a
// print the machine replaced without saying how it ended. Only the end is
// written, and an end already there is kept.
func TestEndingAnAbandonedPrintKeepsEveryReading(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		aRegisteredDevice(t, store, "p-alpha", "Alpha")

		job := aRunningJob("p-alpha", "7")
		require.NoError(t, store.SaveDeviceJobs(t.Context(), job))

		first := startedAt.Add(time.Hour)
		require.NoError(t, store.EndPrinterJob(t.Context(), job.JobID, first, "unknown"))
		require.NoError(t, store.EndPrinterJob(t.Context(), job.JobID, first.Add(time.Hour), "failed"))

		found, held, err := store.PrinterJobByID(t.Context(), job.JobID)
		require.NoError(t, err)
		require.True(t, held)
		require.True(t, first.Equal(found.Ended), "the end moved to %s", found.Ended)
		require.Equal(t, "unknown", found.Outcome, "a second end rewrote the outcome the first wrote")
		require.Equal(t, job.State, found.State, "ending the print rewrote the machine's last word")
		require.InDelta(t, job.Progress, found.Progress, 0.0001)
	})
}

// TestAPrintKeepsTheOutcomeItEndedWith holds the outcome to the write that
// ends the print. A running print carries none, even when a reading names one,
// and a later write cannot change the one the end wrote.
func TestAPrintKeepsTheOutcomeItEndedWith(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		aRegisteredDevice(t, store, "p-alpha", "Alpha")

		running := aRunningJob("p-alpha", "7")
		running.Outcome = "finished"
		require.NoError(t, store.SaveDeviceJobs(t.Context(), running))

		found, held, err := store.PrinterJobByID(t.Context(), running.JobID)
		require.NoError(t, err)
		require.True(t, held)
		require.Empty(t, found.Outcome, "a running print carried an outcome")

		ended := aRunningJob("p-alpha", "7")
		ended.State = "STOPPED"
		ended.Ended = startedAt.Add(time.Hour)
		ended.Outcome = "cancelled"
		require.NoError(t, store.SaveDeviceJobs(t.Context(), ended))

		later := ended
		later.Outcome = "finished"
		require.NoError(t, store.SaveDeviceJobs(t.Context(), later))

		found, held, err = store.PrinterJobByID(t.Context(), running.JobID)
		require.NoError(t, err)
		require.True(t, held)
		require.Equal(t, "cancelled", found.Outcome, "a write after the end changed the outcome")
	})
}
