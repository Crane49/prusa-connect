package app

import (
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/devicestore"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// The print, from the state tree to the stored row.
//
// The first case is the derivation on its own, because every rule the flush
// applies is in it and none of them needs a database to be wrong. The second
// drives a real print through the published API and restarts the core over it,
// which is the only way to prove the row outlives the process that wrote it.

// treeOf is a state tree written the way a driver leaves one: paths to encoded
// values, with a value the machine did not report simply absent.
func treeOf(pairs map[string]string) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(pairs))
	for path, value := range pairs {
		out[path] = json.RawMessage(value)
	}

	return out
}

// TestTheJobIsReadOutOfTheStateTree is every rule the flush applies, one case
// each. What it is guarding is the two-way split between the machine's state
// and the print's: a machine that has finished and gone back to idle must keep
// saying how the print ended, and must stop saying it is printing.
func TestTheJobIsReadOutOfTheStateTree(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		tree  map[string]string
		held  bool
		state string
		// current is the id device_record carries, empty when the machine is
		// printing nothing.
		current string
		running bool
		// ended is whether the reading stamps the row's end, which is not the
		// negation of running: see the BUSY case and jobFacts.ended.
		ended bool
		path  string
	}{
		{
			name: "a machine that has never printed",
			tree: map[string]string{"/state": `"IDLE"`},
			held: false,
		},
		{
			name: "a print running",
			tree: map[string]string{
				"/state": `"PRINTING"`, "/job/id": `7`, "/job/state": `"PRINTING"`,
				"/job/path": `"/usb/tangle.bgcode"`, "/job/progress": `41`,
			},
			held: true, state: "PRINTING", current: "p-alpha:7", running: true,
			path: "/usb/tangle.bgcode",
		},
		{
			name: "a print paused",
			tree: map[string]string{
				"/state": `"PAUSED"`, "/job/id": `7`, "/job/state": `"PAUSED"`,
			},
			held: true, state: "PAUSED", current: "p-alpha:7", running: true,
		},
		{
			// A dialog on the screen is not the end of a print. The machine
			// reports ATTENTION instead of PRINTING and the file is still on
			// the bed.
			name: "a print with a dialog up",
			tree: map[string]string{
				"/state": `"ATTENTION"`, "/job/id": `7`, "/job/state": `"PRINTING"`,
			},
			held: true, state: "PRINTING", current: "p-alpha:7", running: true,
		},
		{
			name: "a print stopped",
			tree: map[string]string{
				"/state": `"STOPPED"`, "/job/id": `7`, "/job/state": `"STOPPED"`,
			},
			held: true, state: "STOPPED", running: false, ended: true,
		},
		{
			// The machine has moved on and the print has not. This is the case
			// the separate /job/state path exists for: reading the job's state
			// off /state here would store IDLE as how the print ended.
			name: "a machine idle after a print that finished",
			tree: map[string]string{
				"/state": `"IDLE"`, "/job/id": `7`, "/job/state": `"FINISHED"`,
			},
			held: true, state: "FINISHED", running: false, ended: true,
		},
		{
			// A long move is not the end of a print. A machine reports BUSY
			// while it homes or levels its bed and says nothing about a print
			// by it, so the row stays open: an end stamped here would put a
			// wrong finish time on a print that is still running (BUG-012).
			//
			// It stays the current print for the same reason. A reading that
			// closed neither the row nor called the print current took the
			// job out of device_record's current_job_id for the length of the
			// move and put it back after, which is the row open and the
			// device printing nothing at the same time.
			name: "a print under a long move",
			tree: map[string]string{
				"/state": `"BUSY"`, "/job/id": `7`, "/job/state": `"PRINTING"`,
			},
			held: true, state: "PRINTING", current: "p-alpha:7", running: true, ended: false,
		},
		{
			// And a long move over a print that has ended is not a print
			// running. The print's own last word is the terminal one, and BUSY
			// is the machine doing something else afterwards.
			name: "a long move after a print that stopped",
			tree: map[string]string{
				"/state": `"BUSY"`, "/job/id": `7`, "/job/state": `"STOPPED"`,
			},
			held: true, state: "STOPPED", running: false, ended: false,
		},
		{
			// A machine with no print on the bed gets no row out of a long
			// move either, because no job number has ever been reported.
			name: "a long move on a machine that has never printed",
			tree: map[string]string{"/state": `"BUSY"`},
			held: false,
		},
		{
			// A driver that reports no separate job state still gets one.
			name: "a driver that reports only the machine state",
			tree: map[string]string{"/state": `"PRINTING"`, "/job/id": `7`},
			held: true, state: "PRINTING", current: "p-alpha:7", running: true,
		},
	}

	for _, one := range cases {
		t.Run(one.name, func(t *testing.T) {
			t.Parallel()

			tree := treeOf(one.tree)
			facts := jobFactsOf(tree)

			require.Equal(t, one.held, facts.held())
			require.Equal(t, one.current != "", facts.held() && facts.running(),
				"whether the device is running a print")

			record := devicestore.DeviceRecord{DeviceID: "p-alpha", LastSeen: startedAtInApp, State: tree}

			row, held := jobRowOf(record, facts, "print-1")
			require.Equal(t, one.held, held)

			book := newPrintBook()
			book.put("p-alpha", latestPrint{id: "print-1", number: facts.machineID})

			summary := jobOf(tree, book, "p-alpha")

			if !one.held {
				require.Nil(t, summary, "a machine that has never printed carries a job")

				return
			}

			require.Equal(t, one.state, row.State)
			require.Equal(t, one.path, row.Path)
			require.Equal(t, one.ended, !row.Running())
			require.True(t, startedAtInApp.Equal(row.Started))

			require.NotNil(t, summary)
			require.Equal(t, row.JobID, summary.ID)
			require.Equal(t, one.state, summary.State)
			require.Equal(t, one.running, summary.Running)
		})
	}
}

// TestTheJobFollowsTheMachineWhenTheJobInfoIsStale is the derivation half of
// BUG-004.
//
// Real Buddy firmware volunteers a JOB_INFO when a print starts and never
// again, so /job/state can go on saying PRINTING for the whole of a print that
// was paused and then aborted. The driver now asks for a fresh one on every
// move, and these are the readings the record must take while that answer is in
// flight, or on a firmware that never sends one: the machine's own state is the
// newer of the two, and where the two cannot both be true the machine wins.
func TestTheJobFollowsTheMachineWhenTheJobInfoIsStale(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		tree    map[string]string
		state   string
		running bool
		display string
	}{
		{
			// The reading taken on the bench while the MK4S was paused.
			name: "the machine is paused and the print has not been told",
			tree: map[string]string{
				"/state": `"PAUSED"`, "/job/id": `233`, "/job/state": `"PRINTING"`,
				"/job/path": `"/usb/bench-mk4s.gcode"`,
			},
			state: "PAUSED", running: true, display: "bench-mk4s.gcode",
		},
		{
			// And the one taken after the stop settled, which is the row the
			// history kept as PRINTING.
			name: "the machine is stopped and the print has not been told",
			tree: map[string]string{
				"/state": `"STOPPED"`, "/job/id": `233`, "/job/state": `"PRINTING"`,
				"/job/path": `"/usb/bench-mk4s.gcode"`,
			},
			state: "STOPPED", running: false, display: "bench-mk4s.gcode",
		},
		{
			// A print stopped from the machine's own screen while it was
			// paused. The job's last word is PAUSED and it is a running word,
			// so the machine overrules it too.
			name: "the machine is stopped and the print still says paused",
			tree: map[string]string{
				"/state": `"STOPPED"`, "/job/id": `233`, "/job/state": `"PAUSED"`,
			},
			state: "STOPPED", running: false,
		},
		{
			name: "the machine finished and the print has not been told",
			tree: map[string]string{
				"/state": `"FINISHED"`, "/job/id": `233`, "/job/state": `"PRINTING"`,
			},
			state: "FINISHED", running: false,
		},
		{
			// The machine has gone back to idle and the print's own last word
			// is how it ended. Reading the machine here would replace the
			// answer with the absence of one.
			name: "the machine is idle after a print that finished",
			tree: map[string]string{
				"/state": `"IDLE"`, "/job/id": `233`, "/job/state": `"FINISHED"`,
			},
			state: "FINISHED", running: false,
		},
		{
			// The tree the driver leaves behind: it writes the machine's
			// terminal word into /job/state as the print ends, and the machine
			// then goes back to IDLE. Reading the machine here would report the
			// print as running again.
			name: "the machine is idle after a print that was stopped",
			tree: map[string]string{
				"/state": `"IDLE"`, "/job/id": `233`, "/job/state": `"STOPPED"`,
				"/job/path": `"/usb/bench-mk4s.gcode"`,
			},
			state: "STOPPED", running: false, display: "bench-mk4s.gcode",
		},
		{
			// The same staleness from the other side. /job/state moves at the
			// start of a print and on a reconnect, so a host that reattached
			// between two prints holds IDLE over a machine that is printing,
			// and the machine is the newer of the two either way round.
			name: "the print says idle and the machine is printing",
			tree: map[string]string{
				"/state": `"PRINTING"`, "/job/id": `233`, "/job/state": `"IDLE"`,
			},
			state: "PRINTING", running: true,
		},
		{
			// BUG-013, as the tree stood on all three printers: the firmware
			// renders JOB_INFO's state as PAUSED for any device state that is
			// not exactly Printing, a dialog included, so an answer taken while
			// the print-preview dialog stood said PAUSED about a print nobody
			// paused. The machine says it is printing and it is the truer word.
			name: "the print says paused and the machine is printing",
			tree: map[string]string{
				"/state": `"PRINTING"`, "/job/id": `233`, "/job/state": `"PAUSED"`,
				"/job/path": `"/usb/s07-long.gcode"`,
			},
			state: "PRINTING", running: true, display: "s07-long.gcode",
		},
		{
			// A print cancelled at the printer, read before the driver has
			// written how it ended: the row must not freeze as PRINTING.
			name: "the machine is idle and the print still says printing",
			tree: map[string]string{
				"/state": `"IDLE"`, "/job/id": `233`, "/job/state": `"PRINTING"`,
			},
			state: "IDLE", running: false,
		},
		{
			// A dialog over a print that is still on the bed.
			name: "the machine wants attention during a print",
			tree: map[string]string{
				"/state": `"ATTENTION"`, "/job/id": `233`, "/job/state": `"PRINTING"`,
			},
			state: "PRINTING", running: true,
		},
		{
			// The firmware names a display only for a file whose name it
			// shortened, so most prints arrive without one.
			name: "the firmware named the print and the host keeps its word",
			tree: map[string]string{
				"/state": `"PRINTING"`, "/job/id": `233`, "/job/state": `"PRINTING"`,
				"/job/path": `"/usb/BENCH~1.GCO"`, "/job/display": `"bench-mk4s.gcode"`,
			},
			state: "PRINTING", running: true, display: "bench-mk4s.gcode",
		},
		{
			name: "a print with no file at all is named nothing",
			tree: map[string]string{
				"/state": `"PRINTING"`, "/job/id": `233`, "/job/state": `"PRINTING"`,
			},
			state: "PRINTING", running: true,
		},
	}

	for _, one := range cases {
		t.Run(one.name, func(t *testing.T) {
			t.Parallel()

			tree := treeOf(one.tree)
			record := devicestore.DeviceRecord{DeviceID: "p-alpha", LastSeen: startedAtInApp, State: tree}

			row, held := jobRowOf(record, jobFactsOf(tree), "print-1")
			require.True(t, held)

			require.Equal(t, one.state, row.State,
				"the record took the stale JOB_INFO over what the machine is saying")
			require.Equal(t, one.running, row.Running())
			require.Equal(t, one.display, row.DisplayName)

			summary := jobOf(tree, newPrintBook(), "p-alpha")
			require.NotNil(t, summary)
			require.Equal(t, one.state, summary.State)
			require.Equal(t, one.running, summary.Running)
		})
	}
}

// startedAtInApp is the last_seen every derivation case above is written at.
var startedAtInApp = time.Date(2026, 8, 28, 9, 14, 22, 0, time.UTC)

// deviceJobs is what GET /api/v1/devices/{id}/jobs answered.
func deviceJobs(t *testing.T, running *process, id string) httpapi.DeviceJobs {
	t.Helper()

	read := running.get(t, "/api/v1/devices/"+id+"/jobs")
	require.Equal(t, http.StatusOK, read.status, read.body)

	var found httpapi.DeviceJobs

	require.NoError(t, json.Unmarshal([]byte(read.body), &found))

	return found
}

// sendVerb sends one command and requires the printer to have carried it out.
func sendVerb(t *testing.T, running *process, id, body string) {
	t.Helper()

	sent := running.call(t, http.MethodPost, "/api/v1/devices/"+id+"/commands", body)
	require.Equal(t, http.StatusOK, sent.status, sent.body)

	var result httpapi.PrinterResult

	require.NoError(t, json.Unmarshal([]byte(sent.body), &result))
	require.Truef(t, result.OK, "the printer refused the command: %s", result.MachineReason)
}

// awaitJob waits for the jobs route to answer something the case is about. The
// record flush is on a two second timer, so every step here is a wait and never
// a read straight after the command.
func awaitJob(t *testing.T, running *process, id, what string, ready func(httpapi.DeviceJobs) bool) httpapi.DeviceJobs {
	t.Helper()

	var found httpapi.DeviceJobs

	require.Eventuallyf(t, func() bool {
		found = deviceJobs(t, running, id)

		return ready(found)
	}, settle, 100*time.Millisecond, "the jobs route never reported %s", what)

	return found
}

// jobEngine is one engine the print table runs on, as a whole configuration
// file rather than as a handle.
//
// It is a configuration and not a store because what is under test is the core
// end to end: a print reaches the table through the driver, the plane's flush
// and the HTTP surface, and a case that opened a store itself would prove the
// two statements and none of the path between them. recordEngine, beside it in
// devicerecordevents_test.go, is the handle-shaped one for a plane-level case.
//
// Which engine holds the prints is the deployment's `persistence:` block and
// not a resource declaration: the graph, the device records and the prints
// share one pool and one migration ledger (note 42, recommendation 3).
type jobEngine struct {
	Name string
	// Config is the whole configuration file a case starts on, built once per
	// case so that the restart in the middle of it comes back to the same
	// database.
	Config string
}

// sqliteJobs is the deployment nobody configured, which is the one every other
// case in this package already runs.
func sqliteJobs() jobEngine {
	return jobEngine{Name: "sqlite", Config: "core:\n  log:\n    level: error\n"}
}

// forEachJobEngine runs body as a subtest per engine the prints can be held on.
// A missing engine is a skip unless AUTHZ_REQUIRED_ENGINES names it, which is
// the rule every other engine-parametrised case here follows: a run that
// reports ok while the engine it was invoked for never ran proved nothing.
func forEachJobEngine(t *testing.T, body func(t *testing.T, e jobEngine)) {
	t.Helper()

	engines := []jobEngine{sqliteJobs()}
	required := slices.Contains(requiredEngineNames(), "postgres")

	if postgres, reachable := postgresJobs(t, required); reachable {
		engines = append(engines, postgres)
	}

	for _, e := range engines {
		t.Run(e.Name, func(t *testing.T) {
			t.Parallel()
			body(t, e)
		})
	}
}

// postgresJobs is the deployment that keeps its prints on Postgres, on a
// scratch database of this case's own so that two cases running at once do not
// migrate over each other. It reports false when this run cannot reach one, and
// fails rather than reporting false when the environment said it must.
func postgresJobs(t *testing.T, required bool) (jobEngine, bool) {
	t.Helper()

	dsn := os.Getenv(postgresDSNEnv)
	if dsn == "" {
		if required {
			t.Fatalf("AUTHZ_REQUIRED_ENGINES names postgres and %s is not set", postgresDSNEnv)
		}

		return jobEngine{}, false
	}

	if !postgresDriverLinked(t, dsn) {
		if required {
			t.Fatal("AUTHZ_REQUIRED_ENGINES names postgres and this build carries no postgres driver")
		}

		return jobEngine{}, false
	}

	return jobEngine{Name: "postgres", Config: lines(
		"core:",
		"  log:",
		"    level: error",
		"persistence:",
		"  type: postgres",
		"  postgres:",
		`    dsn: "`+scratchDatabase(t, dsn)+`"`,
	)}, true
}

// TestAPrintIsTrackedThroughItsStatesAndSurvivesARestart is the whole of what
// the table is for, over the real composition root and the published API.
//
// A print is started, paused, resumed and stopped through the command surface,
// and the jobs route is read after each move. Then the core is stopped the way
// a signal stops it and started again on the same configuration, and the print
// is still there: the row is in the record store, so what survives is not the
// session that watched it.
//
// It runs on both engines, because the row it is about is written by one
// statement per engine and read by another, and a restart that came back
// against Postgres would find nothing if either half were spelled for SQLite
// alone.
func TestAPrintIsTrackedThroughItsStatesAndSurvivesARestart(t *testing.T) {
	t.Parallel()

	forEachJobEngine(t, func(t *testing.T, e jobEngine) {
		printLifecycleCase(t, e)
	})
}

// printLifecycleCase is the body above, per engine.
func printLifecycleCase(t *testing.T, e jobEngine) {
	t.Helper()

	dir := t.TempDir()

	first := start(t, dir, e.Config)

	dialIn(t, first, "SIM-JOBS")

	fleet := printers(t, first)
	require.Len(t, fleet, 1)

	id := fleet[0].ID

	// A machine that has never printed answers the route with neither member,
	// which is a real answer and not an absence of one.
	require.Nil(t, deviceJobs(t, first, id).Current)
	require.Nil(t, deviceJobs(t, first, id).Last)

	sendVerb(t, first, id,
		`{"verb":"core.command.start_print","args":{"path":"usb/tangle.bgcode"}}`)

	running := awaitJob(t, first, id, "a print running", func(found httpapi.DeviceJobs) bool {
		return found.Current != nil && found.Current.State == "PRINTING"
	})

	require.Regexp(t, printIDPattern, running.Current.ID, "the print is not named by a version 7 UUID")
	require.NotEmpty(t, running.Current.DeviceJobID, "the machine's job number did not reach the row")
	require.Equal(t, "/usb/tangle.bgcode", running.Current.Path,
		"the file the machine is printing did not reach the row, so JOB_INFO was not read")
	require.Equal(t, "tangle.bgcode", running.Current.DisplayName,
		"the print reached the row with nothing to show it as")
	require.Nil(t, running.Current.Ended, "a print that is running carries an end")
	require.False(t, running.Current.Started.IsZero())
	require.Nil(t, running.Last, "the first print on a machine is reported as a previous one")

	// The device render carries the same print, out of the state tree rather
	// than out of the table, so a fleet page says what every machine is doing
	// without a request per row.
	listed := printers(t, first)
	require.NotNil(t, listed[0].Job, "the device render carries no job")
	require.Equal(t, running.Current.ID, listed[0].Job.ID)
	require.True(t, listed[0].Job.Running)

	// The three moves below are the whole of BUG-004. The simulated printer
	// volunteers nothing after any of them, exactly as the firmware volunteers
	// nothing: it changes its own /state and waits to be asked. Every state
	// this waits for therefore proves that the host sent SEND_JOB_INFO, or that
	// the record read the machine's state over a JOB_INFO that had gone stale.
	sendVerb(t, first, id, `{"verb":"core.command.pause_print","args":{}}`)
	awaitJob(t, first, id, "a print paused", func(found httpapi.DeviceJobs) bool {
		return found.Current != nil && found.Current.State == "PAUSED"
	})

	sendVerb(t, first, id, `{"verb":"core.command.resume_print","args":{}}`)
	awaitJob(t, first, id, "a print resumed", func(found httpapi.DeviceJobs) bool {
		return found.Current != nil && found.Current.State == "PRINTING"
	})

	sendVerb(t, first, id, `{"verb":"core.command.stop_print","args":{}}`)

	stopped := awaitJob(t, first, id, "a print that ended", func(found httpapi.DeviceJobs) bool {
		return found.Last != nil
	})

	require.Nil(t, stopped.Current, "a machine that stopped printing still reports a current job")
	require.Equal(t, "STOPPED", stopped.Last.State)
	require.NotNil(t, stopped.Last.Ended, "a print that ended carries no end")
	require.Equal(t, running.Current.ID, stopped.Last.ID,
		"the print that stopped is a different row from the one that started")
	require.Equal(t, "/usb/tangle.bgcode", stopped.Last.Path)

	endedAt := *stopped.Last.Ended

	t.Logf("observed: %s ran %s and ended %s at %s",
		id, stopped.Last.Path, stopped.Last.State, endedAt.Format(time.RFC3339Nano))

	first.stopAndWait(t)

	// The second run. The same directory, so the same database file, and
	// nothing else crosses.
	second := start(t, dir, e.Config)

	after := deviceJobs(t, second, id)

	require.NotNil(t, after.Last, "the print did not survive the restart")
	require.Equal(t, stopped.Last.ID, after.Last.ID)
	require.Equal(t, "/usb/tangle.bgcode", after.Last.Path)
	require.Equal(t, "STOPPED", after.Last.State)
	require.NotNil(t, after.Last.Ended)
	require.True(t, endedAt.Equal(*after.Last.Ended),
		"the restart moved the end of a print that was already over to %s", after.Last.Ended)
	require.Nil(t, after.Current,
		"a restart reported a print in progress on a machine nothing has reached")
}

// TestTheJobsRouteRefusesADeviceTheCallerCannotSee is the authorization half,
// and it is the same refusal GET /api/v1/devices/{id} gives: a machine the
// caller reaches nothing on and an id this server has never been dialled by are
// one answer, so a refusal never says a printer exists.
func TestTheJobsRouteRefusesADeviceTheCallerCannotSee(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	unknown := running.get(t, "/api/v1/devices/p-nobody/jobs")
	require.Equal(t, http.StatusNotFound, unknown.status, unknown.body)

	// The device read answers the same id the same way, which is what makes the
	// two indistinguishable.
	device := running.get(t, "/api/v1/devices/p-nobody")
	require.Equal(t, device.status, unknown.status,
		"the jobs route and the device read disagree about a device nobody can see")
}

// TestAPrintThatStoppedKeepsItsWordWhenTheMachineIdlesInsideOneFlush is the
// race half of BUG-004.
//
// Rows are derived on the record flush, and the first flush that sees a print
// which is no longer running writes `ended`. The upsert then freezes every
// column of the row at that reading (internal/authz/printerjob.go). A machine that
// passes STOPPED and reaches IDLE inside one flush interval therefore froze the
// row at whatever the tree said with the machine already idle, and on a
// firmware that volunteers no JOB_INFO that was the PRINTING the frame at the
// start of the print left behind.
//
// The two verbs here are sent back to back, so both states are reached in a few
// milliseconds and one flush sees the whole of it. What makes the row right is
// that the driver writes the terminal word into /job/state before the machine
// idles, which is a reading no later frame takes back.
func TestAPrintThatStoppedKeepsItsWordWhenTheMachineIdlesInsideOneFlush(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	dialIn(t, running, "SIM-JOB-IDLE")

	fleet := printers(t, running)
	require.Len(t, fleet, 1)

	id := fleet[0].ID

	sendVerb(t, running, id,
		`{"verb":"core.command.start_print","args":{"path":"usb/tangle.bgcode"}}`)

	awaitJob(t, running, id, "a print running", func(found httpapi.DeviceJobs) bool {
		return found.Current != nil && found.Current.State == "PRINTING"
	})

	// The stop and the machine going back to what it does between prints, with
	// nothing waited for in between.
	sendVerb(t, running, id, `{"verb":"core.command.stop_print","args":{}}`)
	sendVerb(t, running, id, `{"verb":"core.command.set_idle","args":{}}`)

	stopped := awaitJob(t, running, id, "a print that ended", func(found httpapi.DeviceJobs) bool {
		return found.Last != nil
	})

	require.Equal(t, "STOPPED", stopped.Last.State,
		"the row froze at the state the tree held with the machine already idle")
	require.Nil(t, stopped.Current, "a machine that is idle still reports a print in progress")
	require.NotNil(t, stopped.Last.Ended)

	t.Logf("observed: the machine idled inside one flush and the row kept %s", stopped.Last.State)

	// And it stays that way. The device goes on reporting IDLE at 4 Hz, and a
	// derivation that read the machine over the print would move the row back
	// to a running state on the next flush.
	time.Sleep(2 * recordFlushInterval)

	after := deviceJobs(t, running, id)
	require.NotNil(t, after.Last)
	require.Equal(t, "STOPPED", after.Last.State,
		"two flushes after the print ended the row no longer says how it ended")
	require.Nil(t, after.Current)
}
