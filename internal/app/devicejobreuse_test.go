package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/devicestore"
	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// TestAPrintThatReusesAJobNumberIsAPrintOfItsOwn is a printer whose job counter
// starts again, which a firmware reset does.
//
// The machine's job number is the only thing the printer gives to tell one
// print from another, and it is unique inside one run of that counter and
// nowhere else. Two prints that carry the same number are still two prints:
// each one has its own file, its own start and its own end, and each one is a
// row of its own on the history page.
func TestAPrintThatReusesAJobNumberIsAPrintOfItsOwn(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	printer := dialInPrinter(t, running, "SIM-JOB-REUSE")

	fleet := printers(t, running)
	require.Len(t, fleet, 1)

	id := fleet[0].ID

	// The first print, numbered 7, from start to end.
	printer.SetJobID(7)
	sendVerb(t, running, id,
		`{"verb":"core.command.start_print","args":{"path":"usb/tangle.bgcode"}}`)

	awaitJob(t, running, id, "the first print running", func(found httpapi.DeviceJobs) bool {
		return found.Current != nil && found.Current.Path == "/usb/tangle.bgcode"
	})

	sendVerb(t, running, id, `{"verb":"core.command.stop_print","args":{}}`)

	first := awaitJob(t, running, id, "the first print ended", func(found httpapi.DeviceJobs) bool {
		return found.Last != nil && found.Last.Ended != nil
	}).Last

	t.Logf("first print: id=%s path=%s state=%s", first.ID, first.Path, first.State)

	// The printer is reset. It comes back idle with its counter started
	// again, and the next print is numbered 7 as well.
	sendVerb(t, running, id, `{"verb":"core.command.set_idle","args":{}}`)
	printer.SetJobID(7)
	sendVerb(t, running, id,
		`{"verb":"core.command.start_print","args":{"path":"usb/examples/benchy.bgcode"}}`)

	var seen httpapi.DeviceJobs

	reached := pollUntil(settle, func() bool {
		seen = deviceJobs(t, running, id)

		return seen.Current != nil && seen.Current.Path == "/usb/examples/benchy.bgcode"
	})

	t.Logf("while the second print runs, the jobs route answers current=%s last=%s",
		describeJob(seen.Current), describeJob(seen.Last))

	require.True(t, reached,
		"the printer is printing benchy.bgcode and the server reports no such print")
	require.NotEqual(t, first.ID, seen.Current.ID,
		"the second print is stored under the id of the first one")

	sendVerb(t, running, id, `{"verb":"core.command.stop_print","args":{}}`)

	var history httpapi.DeviceJobPage

	pollUntil(settle, func() bool {
		history = deviceJobHistory(t, running, id)

		return len(history.Items) == 2
	})

	for _, row := range history.Items {
		t.Logf("history row: id=%s path=%s state=%s", row.ID, row.Path, row.State)
	}

	require.Len(t, history.Items, 2, "two prints ended on this printer and the history holds a different number")
}

// dialInPrinter is dialIn for a case that drives the printer itself, so it
// hands back the simulated machine.
func dialInPrinter(t *testing.T, running *process, serial string) *simulator.Printer {
	t.Helper()

	printer, err := simulator.New(simulator.Config{
		Model:   "MK4",
		Serial:  serial,
		BaseURL: "http://" + running.addr,
	})
	require.NoError(t, err)

	ctx, stop := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan error, 1)

	go func() { done <- printer.RegisterAndRun(ctx) }()

	t.Cleanup(func() {
		stop()
		<-done
	})

	require.Eventually(t, func() bool {
		listed := printers(t, running)

		return len(listed) == 1 && telemetered(listed[0].State)
	}, settle, 25*time.Millisecond, "the simulated printer never appeared in the listing")

	return printer
}

func deviceJobHistory(t *testing.T, running *process, id string) httpapi.DeviceJobPage {
	t.Helper()

	read := running.get(t, "/api/v1/devices/"+id+"/jobs/history")
	require.Equal(t, http.StatusOK, read.status, read.body)

	var page httpapi.DeviceJobPage

	require.NoError(t, json.Unmarshal([]byte(read.body), &page))

	return page
}

// pollUntil polls until done answers true or the time runs out, and reports
// which. It does not fail the case, so the case can say what it saw first.
func pollUntil(limit time.Duration, done func() bool) bool {
	deadline := time.Now().Add(limit)

	for time.Now().Before(deadline) {
		if done() {
			return true
		}

		time.Sleep(100 * time.Millisecond)
	}

	return done()
}

func describeJob(job *httpapi.DeviceJob) string {
	if job == nil {
		return "none"
	}

	ended := "running"
	if job.Ended != nil {
		ended = "ended"
	}

	return job.ID + " " + job.Path + " " + job.State + " " + ended
}

// framesOf is the print frames of one type the bus carried.
func framesOf(frames []event.Envelope, kind string) []event.Envelope {
	var out []event.Envelope

	for _, frame := range frames {
		if frame.Type == kind {
			out = append(out, frame)
		}
	}

	return out
}

// TestALateReportAboutAnEndedPrintChangesNothing is a running reading under the
// number of a print that has just ended, with no idle reading in between. It is
// about the print that ended, so it opens nothing and moves nothing, and the
// idle reading after it does not publish the end a second time.
func TestALateReportAboutAnEndedPrintChangesNothing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		plane, bus, graph := planeBusAndGraph(t)
		id, session := arriveWithSession(t, plane, "SIM-JOB-LATE")

		reportPrint(t, plane, session, map[string]string{
			"/state": `"PRINTING"`, "/job/id": `7`, "/job/path": `"/usb/tangle.bgcode"`,
		})
		reportPrint(t, plane, session, map[string]string{
			"/state": `"FINISHED"`, "/job/state": `"FINISHED"`, "/job/outcome": `"finished"`,
		})
		reportPrint(t, plane, session, map[string]string{
			"/state": `"PRINTING"`, "/job/state": `"PRINTING"`, "/job/path": `"/usb/late.bgcode"`,
		})
		reportPrint(t, plane, session, map[string]string{"/state": `"IDLE"`})

		rows, err := recordsOf(t, graph).DeviceJobs(t.Context(), id, 10)
		require.NoError(t, err)
		require.Len(t, rows, 1, "a late report opened a print of its own")
		require.Equal(t, "/usb/tangle.bgcode", rows[0].Path, "a late report rewrote the print that ended")
		require.Equal(t, "FINISHED", rows[0].State)
		require.False(t, rows[0].Running())

		frames := bus.awaitJobFrames(t, 2)
		require.Len(t, framesOf(frames, event.TypeDeviceJobStarted), 1)
		require.Len(t, framesOf(frames, event.TypeDeviceJobSettled), 1)
	})
}

// TestAPrintPreviewAfterAPrintOpensNothing is an MK4S that ended a print, went
// idle, and was then sent a file whose transfer was stopped. The firmware put up
// its print preview with a file error, which is ATTENTION, and named the file
// under the number of the print before it, because its counter moves only when
// a print starts. Nothing was printed, so nothing is opened (GitHub #701).
func TestAPrintPreviewAfterAPrintOpensNothing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		plane, bus, graph := planeBusAndGraph(t)
		id, session := arriveWithSession(t, plane, "SIM-JOB-PREVIEW")

		reportPrint(t, plane, session, map[string]string{
			"/state": `"PRINTING"`, "/job/id": `7`, "/job/path": `"/usb/tangle.bgcode"`,
		})
		reportPrint(t, plane, session, map[string]string{
			"/state": `"FINISHED"`, "/job/state": `"FINISHED"`, "/job/outcome": `"finished"`,
		})
		reportPrint(t, plane, session, map[string]string{"/state": `"IDLE"`})
		reportPrint(t, plane, session, map[string]string{
			"/state": `"ATTENTION"`, "/dialog/code": `26807`, "/job/path": `"/usb/stopped.gcode"`,
		})
		reportPrint(t, plane, session, map[string]string{"/state": `"IDLE"`})

		rows, err := recordsOf(t, graph).DeviceJobs(t.Context(), id, 10)
		require.NoError(t, err)
		require.Len(t, rows, 1, "a print preview opened a print of its own")
		require.Equal(t, "/usb/tangle.bgcode", rows[0].Path)

		frames := bus.awaitJobFrames(t, 2)
		require.Len(t, framesOf(frames, event.TypeDeviceJobStarted), 1)
		require.Len(t, framesOf(frames, event.TypeDeviceJobSettled), 1)
	})
}

// TestAPrintTheMachineReplacedIsEndedAsUnknown is a machine that reports a new
// job number while a print is open, without ever saying how the open one
// ended. The open print gets an end, keeps the machine's last word, and
// settles as unknown; the new print is one of its own.
func TestAPrintTheMachineReplacedIsEndedAsUnknown(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		plane, bus, graph := planeBusAndGraph(t)
		id, session := arriveWithSession(t, plane, "SIM-JOB-REPLACED")

		reportPrint(t, plane, session, map[string]string{
			"/state": `"PRINTING"`, "/job/id": `7`, "/job/path": `"/usb/tangle.bgcode"`,
		})
		lastWord := time.Now()

		// The machine is out of sight for an hour and comes back on another print.
		time.Sleep(time.Hour)
		reportPrint(t, plane, session, map[string]string{
			"/job/id": `8`, "/job/path": `"/usb/benchy.bgcode"`,
		})

		rows, err := recordsOf(t, graph).DeviceJobs(t.Context(), id, 10)
		require.NoError(t, err)
		require.Len(t, rows, 2)

		byNumber := map[string]devicestore.PrinterJob{}
		for _, row := range rows {
			byNumber[row.DeviceJobID] = row
		}

		replaced, current := byNumber["7"], byNumber["8"]
		require.False(t, replaced.Running(), "the replaced print has no end")
		require.WithinDuration(t, lastWord, replaced.Ended, time.Minute,
			"the replaced print ends when the machine last reported it, not when its successor was seen")
		require.Equal(t, "PRINTING", replaced.State, "the replaced print lost the machine's last word")
		require.Equal(t, "/usb/tangle.bgcode", replaced.Path)
		require.True(t, current.Running())
		require.NotEqual(t, replaced.JobID, current.JobID)

		frames := bus.awaitJobFrames(t, 3)

		settled := framesOf(frames, event.TypeDeviceJobSettled)
		require.Len(t, settled, 1)
		require.Equal(t, replaced.JobID, settled[0].CausationID)

		var how event.JobSettled
		require.NoError(t, json.Unmarshal(settled[0].Data, &how))
		require.Equal(t, event.JobUnknown, how.Outcome)
		require.Equal(t, "7", how.DeviceJobID)
		require.Len(t, framesOf(frames, event.TypeDeviceJobStarted), 2)

		// The replaced print settles before its successor starts, so a reader
		// following one device's prints never holds two running at once.
		order := make([]string, 0, len(frames))
		for _, frame := range frames {
			order = append(order, frame.Type+" "+frame.CausationID)
		}

		require.Equal(t, []string{
			event.TypeDeviceJobStarted + " " + replaced.JobID,
			event.TypeDeviceJobSettled + " " + replaced.JobID,
			event.TypeDeviceJobStarted + " " + current.JobID,
		}, order)
	})
}

// TestAPrintSurvivesTheMachineReconnecting is a machine that drops its
// connection mid-print and comes back reporting the same job number. The
// returning machine is filed as a new entry with a tree of its own, and it is
// still the same print.
func TestAPrintSurvivesTheMachineReconnecting(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		plane, bus, graph := planeBusAndGraph(t)
		id, first := arriveWithSession(t, plane, "SIM-JOB-RECONNECT")

		reportPrint(t, plane, first, map[string]string{
			"/state": `"PRINTING"`, "/job/id": `7`, "/job/path": `"/usb/tangle.bgcode"`, "/job/progress": `20`,
		})

		again, second := arriveWithSession(t, plane, "SIM-JOB-RECONNECT")
		require.Equal(t, id, again)

		reportPrint(t, plane, second, map[string]string{
			"/state": `"PRINTING"`, "/job/id": `7`, "/job/path": `"/usb/tangle.bgcode"`, "/job/progress": `50`,
		})

		rows, err := recordsOf(t, graph).DeviceJobs(t.Context(), id, 10)
		require.NoError(t, err)
		require.Len(t, rows, 1, "a reconnect split one print into two")
		require.InDelta(t, 50, rows[0].Progress, 0.0001)
		require.True(t, rows[0].Running())

		frames := bus.awaitJobFrames(t, 1)
		require.Len(t, framesOf(frames, event.TypeDeviceJobStarted), 1)
	})
}

// TestAPrintIsReadByItsID is GET /api/v1/jobs/{job_id}: one print, by the id a
// link to it carries, with nothing else to find it by.
func TestAPrintIsReadByItsID(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")
	dialIn(t, running, "SIM-JOB-BY-ID")

	id := printers(t, running)[0].ID

	sendVerb(t, running, id,
		`{"verb":"core.command.start_print","args":{"path":"usb/tangle.bgcode"}}`)

	current := awaitJob(t, running, id, "a print running", func(found httpapi.DeviceJobs) bool {
		return found.Current != nil && found.Current.ID != ""
	}).Current

	read := running.get(t, "/api/v1/jobs/"+current.ID)
	require.Equal(t, http.StatusOK, read.status, read.body)

	var job httpapi.DeviceJob

	require.NoError(t, json.Unmarshal([]byte(read.body), &job))
	require.Equal(t, current.ID, job.ID)
	require.Equal(t, current.DeviceJobID, job.DeviceJobID)
	require.Equal(t, id, job.DeviceID, "the print does not name the printer it ran on")
	require.Equal(t, "/usb/tangle.bgcode", job.Path)

	unknown := running.get(t, "/api/v1/jobs/0f7a3c1e-2b4d-4e6f-8a9b-1c2d3e4f5a6b")
	require.Equal(t, http.StatusNotFound, unknown.status, unknown.body)
	require.Contains(t, unknown.body, `"code":"core.printer/JOB_NOT_FOUND"`)

	malformed := running.get(t, "/api/v1/jobs/"+id+":7")
	require.Equal(t, http.StatusUnprocessableEntity, malformed.status,
		"an id in the old form reached the store: %s", malformed.body)
}

// TestAPrintOnADeviceTheCallerCannotSeeIsAnUnknownPrint is the refusal half. A
// caller who reaches nothing on the machine gets the answer an id nobody ever
// minted gets, so asking for ids cannot tell anybody which prints exist.
func TestAPrintOnADeviceTheCallerCannotSeeIsAnUnknownPrint(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		plane, _, graph := planeBusAndGraph(t)
		id, session := arriveWithSession(t, plane, "SIM-JOB-HIDDEN")

		reportPrint(t, plane, session, map[string]string{
			"/state": `"PRINTING"`, "/job/id": `7`, "/job/path": `"/usb/tangle.bgcode"`,
		})

		rows, err := recordsOf(t, graph).DeviceJobs(t.Context(), id, 1)
		require.NoError(t, err)
		require.Len(t, rows, 1)

		_, hidden := plane.Job(t.Context(), "user:nobody", rows[0].JobID)
		_, unknown := plane.Job(t.Context(), "user:nobody", "0f7a3c1e-2b4d-4e6f-8a9b-1c2d3e4f5a6b")

		require.Error(t, hidden)
		require.Equal(t, unknown.Error(), hidden.Error(),
			"a print the caller cannot see answers differently from one that does not exist")
	})
}

// TestAPrintRunningAcrossARestartKeepsItsID is the core stopping mid-print and
// starting again on the same database, and the printer coming back to it still
// printing under the same job number. The latest print of every device is read
// back at start, so the returning reading is matched to the print it belongs
// to: it keeps its id and nothing opens a second one.
func TestAPrintRunningAcrossARestartKeepsItsID(t *testing.T) {
	t.Parallel()

	forEachJobEngine(t, func(t *testing.T, e jobEngine) {
		dir := t.TempDir()

		first := start(t, dir, e.Config)
		before := dialInPrinter(t, first, "SIM-JOB-RESTART")

		id := printers(t, first)[0].ID

		before.SetJobID(7)
		sendVerb(t, first, id,
			`{"verb":"core.command.start_print","args":{"path":"usb/tangle.bgcode"}}`)

		running := awaitJob(t, first, id, "a print running", func(found httpapi.DeviceJobs) bool {
			return found.Current != nil && found.Current.ID != ""
		}).Current

		first.stopAndWait(t)

		second := start(t, dir, e.Config)

		// The printer dials the restarted core and is still on job 7.
		after := dialInPrinter(t, second, "SIM-JOB-RESTART")

		// The listing holds the restored device before the printer dials, so
		// the wait is for the session and not for the row.
		require.Eventually(t, func() bool {
			listed := printers(t, second)

			return len(listed) == 1 && listed[0].ID == id && listed[0].Connected
		}, settle, 25*time.Millisecond, "the printer never reconnected to the restarted core")

		after.SetJobID(7)
		sendVerb(t, second, id,
			`{"verb":"core.command.start_print","args":{"path":"usb/tangle.bgcode"}}`)

		// Two flushes, so the returning reading has been matched and written.
		time.Sleep(2 * recordFlushInterval)

		seen := deviceJobs(t, second, id)
		require.NotNil(t, seen.Current, "the print running at the stop is no longer the current one")
		require.Equal(t, running.ID, seen.Current.ID, "the restart gave the running print a new id")
		require.Nil(t, seen.Last, "the restart opened a print of its own beside the running one")
	})
}

// faltering is a record store whose device writes can be refused for a while,
// which is a database that stops answering and then comes back.
type faltering struct {
	records

	refuse atomic.Bool
}

func (f *faltering) SaveDeviceRecords(ctx context.Context, rows ...devicestore.DeviceRecord) error {
	if f.refuse.Load() {
		return errors.New("the record store is not answering")
	}

	return f.records.SaveDeviceRecords(ctx, rows...)
}

// falterOn puts a faltering store in front of the plane's own.
func falterOn(plane *devices) *faltering {
	store := &faltering{records: plane.records}
	plane.records = store

	return store
}

// TestAFailedWriteForgetsNoReplacedPrint is a write that fails in the flush
// where the machine replaced its print. The print it replaced is still ended
// once the store answers again, rather than left open for good.
func TestAFailedWriteForgetsNoReplacedPrint(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		plane, bus, graph := planeBusAndGraph(t)
		store := falterOn(plane)
		id, session := arriveWithSession(t, plane, "SIM-JOB-FALTER")

		reportPrint(t, plane, session, map[string]string{
			"/state": `"PRINTING"`, "/job/id": `7`, "/job/path": `"/usb/tangle.bgcode"`,
		})

		store.refuse.Store(true)
		reportPrint(t, plane, session, map[string]string{
			"/job/id": `8`, "/job/path": `"/usb/benchy.bgcode"`,
		})

		store.refuse.Store(false)
		synctest.Wait()
		plane.flush()
		synctest.Wait()

		rows, err := recordsOf(t, graph).DeviceJobs(t.Context(), id, 10)
		require.NoError(t, err)
		require.Len(t, rows, 2)

		byNumber := map[string]devicestore.PrinterJob{}
		for _, row := range rows {
			byNumber[row.DeviceJobID] = row
		}

		require.False(t, byNumber["7"].Running(), "the replaced print was left open")
		require.True(t, byNumber["8"].Running())

		settled := framesOf(bus.awaitJobFrames(t, 3), event.TypeDeviceJobSettled)
		require.Len(t, settled, 1)
		require.Equal(t, byNumber["7"].JobID, settled[0].CausationID)
	})
}

// TestAFailedWriteKeepsTheIdleMachine is a reset whose idle reading falls in a
// flush that fails. The next print under the same number is still a print of
// its own, because the idle machine is not lost with the write.
func TestAFailedWriteKeepsTheIdleMachine(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		plane, _, graph := planeBusAndGraph(t)
		store := falterOn(plane)
		id, session := arriveWithSession(t, plane, "SIM-JOB-FALTER-IDLE")

		reportPrint(t, plane, session, map[string]string{
			"/state": `"PRINTING"`, "/job/id": `7`, "/job/path": `"/usb/tangle.bgcode"`,
		})
		reportPrint(t, plane, session, map[string]string{"/state": `"FINISHED"`})

		store.refuse.Store(true)
		reportPrint(t, plane, session, map[string]string{"/state": `"IDLE"`})
		store.refuse.Store(false)

		reportPrint(t, plane, session, map[string]string{
			"/state": `"PRINTING"`, "/job/path": `"/usb/benchy.bgcode"`,
		})

		rows, err := recordsOf(t, graph).DeviceJobs(t.Context(), id, 10)
		require.NoError(t, err)
		require.Len(t, rows, 2, "the print after the reset was merged into the one before it")
		require.NotEqual(t, rows[0].JobID, rows[1].JobID)
	})
}

// TestAMachineThatNamesItsJobsByTextKeepsThatText is a machine whose driver
// reports a text id, such as a hash a Moonraker driver derives, rather than a
// number. The id is kept as the machine spelled it, and a new text is a new
// print.
func TestAMachineThatNamesItsJobsByTextKeepsThatText(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		plane, _, graph := planeBusAndGraph(t)
		id, session := arriveWithSession(t, plane, "SIM-JOB-TEXT")

		reportPrint(t, plane, session, map[string]string{
			"/state": `"PRINTING"`, "/job/id": `"8f3a2c91d0e4b7a6"`, "/job/path": `"/gcodes/tangle.gcode"`,
		})
		reportPrint(t, plane, session, map[string]string{
			"/job/id": `"c47d10ab93e5f286"`, "/job/path": `"/gcodes/benchy.gcode"`,
		})

		rows, err := recordsOf(t, graph).DeviceJobs(t.Context(), id, 10)
		require.NoError(t, err)
		require.Len(t, rows, 2)

		numbers := []string{rows[0].DeviceJobID, rows[1].DeviceJobID}
		require.ElementsMatch(t, []string{"8f3a2c91d0e4b7a6", "c47d10ab93e5f286"}, numbers)
		require.NotEqual(t, rows[0].JobID, rows[1].JobID)
	})
}
