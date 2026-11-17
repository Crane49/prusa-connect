package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
)

// The print a machine ran, read back off the shipped binary.
//
// internal/app already drives a print through the composition root in process.
// What that cannot reach is the binary: the migration this table needs is
// applied by the same start-up path a self-hoster runs, and a job that reached
// no table would still answer from the live entry there. Here the answer comes
// out of the record store on the other side of a real HTTP request, and the
// route is the one the OpenAPI document publishes.

// pausePrintVerb and resumePrintVerb move a print that is already running. They
// are beside the two scenarios_test.go already names for the same reason those
// two are named: a verb spelled inline in three cases is three places to fix
// when the vocabulary moves.
const (
	pausePrintVerb  = "core.command.pause_print"
	resumePrintVerb = "core.command.resume_print"
)

// jobView is one print as the jobs route reports it. It carries the members
// this case reads and no more: the route publishes the whole row, and a struct
// here that mirrored all of it would be a second copy of the API to keep in
// step.
type jobView struct {
	ID      string  `json:"id"`
	Path    string  `json:"path"`
	State   string  `json:"state"`
	Started string  `json:"started"`
	Ended   *string `json:"ended"`
}

// jobsView is what GET /api/v1/devices/{id}/jobs answers.
type jobsView struct {
	Current *jobView `json:"current"`
	Last    *jobView `json:"last"`
}

// jobs reads the route.
func (s *server) jobs(t *testing.T, printer string) jobsView {
	t.Helper()

	read := s.get(t, "/api/v1/devices/"+printer+"/jobs")
	if read.status != http.StatusOK {
		t.Fatalf("the jobs route answered %d: %s", read.status, read.body)
	}

	var found jobsView

	if err := json.Unmarshal(read.body, &found); err != nil {
		t.Fatalf("the jobs route answered something this case cannot read: %s: %v", read.body, err)
	}

	return found
}

// TestAPrintIsRecordedFromStartToStop walks one print through the four verbs
// that move it and reads the stored row after each, against the built binary.
//
// The assertion that matters most is the last one: once the print has stopped,
// the machine reports no current job and the row that ended carries the file it
// printed and the moment it ended. That is the question an operator asks about
// a machine they were not watching, and nothing in the state tree can answer it
// after the next print starts.
func TestAPrintIsRecordedFromStartToStop(t *testing.T) {
	t.Parallel()

	server := startServer(t)
	server.awaitReady(t)

	_, listed := server.attachPrinter(t, benchModel)

	// A machine that has never printed carries neither member, which is a real
	// answer and not an absence of one.
	before := server.jobs(t, listed.ID)
	if before.Current != nil || before.Last != nil {
		t.Fatalf("a machine that has never printed already carries a job: %+v", before)
	}

	printable := firstPrintable(t, server, listed.ID, benchStorage, 0)

	sendPrintVerb(t, server, listed.ID, fmt.Sprintf(`{"verb":%q,"args":{"path":%q}}`,
		startPrintVerb, printable))

	var running jobsView

	eventually(t, "the jobs route reported a print running", func() bool {
		running = server.jobs(t, listed.ID)

		return running.Current != nil && running.Current.State == "PRINTING"
	})

	if running.Current.Ended != nil {
		t.Fatalf("a print that is running carries an end: %+v", running.Current)
	}

	// The file is the one fact about a print that telemetry never carries, so a
	// row with an empty path is a JOB_INFO the driver did not read.
	//
	// It is waited for rather than read once. The state reaches PRINTING on a
	// telemetry frame and the file arrives with a JOB_INFO, so on a loaded
	// machine the row is named a moment after the print is running.
	name := printable[strings.LastIndex(printable, "/")+1:]

	eventually(t, "the jobs route named the file the machine is printing", func() bool {
		running = server.jobs(t, listed.ID)

		return running.Current != nil && strings.HasSuffix(running.Current.Path, name)
	})

	t.Logf("observed: %s is printing %s under job %s, started %s",
		listed.ID, running.Current.Path, running.Current.ID, running.Current.Started)

	// PAUSED, PRINTING and then STOPPED, each read off /jobs. That sequence is
	// what BUG-004 lost: real firmware volunteers no JOB_INFO for any of the
	// three, and a host that did not ask stored the whole print as PRINTING.
	sendPrintVerb(t, server, listed.ID, fmt.Sprintf(`{"verb":%q}`, pausePrintVerb))
	eventually(t, "the jobs route reported the print paused", func() bool {
		found := server.jobs(t, listed.ID)

		return found.Current != nil && found.Current.State == "PAUSED"
	})

	t.Logf("observed: %s reports the print paused", listed.ID)

	sendPrintVerb(t, server, listed.ID, fmt.Sprintf(`{"verb":%q}`, resumePrintVerb))
	eventually(t, "the jobs route reported the print running again", func() bool {
		found := server.jobs(t, listed.ID)

		return found.Current != nil && found.Current.State == "PRINTING"
	})

	t.Logf("observed: %s reports the print running again", listed.ID)

	sendPrintVerb(t, server, listed.ID, fmt.Sprintf(`{"verb":%q}`, stopPrintVerb))

	var stopped jobsView

	eventually(t, "the jobs route reported the print as the last one", func() bool {
		stopped = server.jobs(t, listed.ID)

		return stopped.Last != nil
	})

	if stopped.Current != nil {
		t.Fatalf("a machine that stopped printing still reports a current job: %+v", stopped.Current)
	}

	if stopped.Last.ID != running.Current.ID {
		t.Fatalf("the print that stopped is a different row from the one that started: %s against %s",
			stopped.Last.ID, running.Current.ID)
	}

	if stopped.Last.Ended == nil {
		t.Fatalf("the print that stopped carries no end: %+v", stopped.Last)
	}

	if stopped.Last.State != "STOPPED" {
		t.Fatalf("the print that was cancelled reports %q", stopped.Last.State)
	}

	t.Logf("observed: %s ran %s and ended %s at %s",
		listed.ID, stopped.Last.Path, stopped.Last.State, *stopped.Last.Ended)
}

// sendPrintVerb sends one command and fails unless the machine carried it out.
func sendPrintVerb(t *testing.T, s *server, printer, body string) {
	t.Helper()

	sent := s.do(t, http.MethodPost, "/api/v1/devices/"+printer+"/commands", body, nil)
	if sent.status != http.StatusOK {
		t.Fatalf("%s answered %d: %s", body, sent.status, sent.body)
	}

	if sent.decode(t)["ok"] != true {
		t.Fatalf("the printer refused %s: %s", body, sent.body)
	}
}

// jobFramesToTheEnd is how many telemetry frames the print that runs to its own
// end takes. The printing cadence is one frame a second, so this is the length
// of the print in seconds, and it is short enough to sit inside settleBudget
// with the record flush and the refresh delay after it.
const jobFramesToTheEnd = 3

// TestAPrintThatRunsToItsEndIsRecordedAsFinished is the other way a print ends,
// and the one no verb of ours reports.
//
// A print that reaches its own end is a state change like a pause: the machine
// reports FINISHED at /state on a telemetry frame, volunteers nothing about the
// print, and goes back to what it does between prints. Nothing on the host asks
// on its own behalf here, so the row's state is the one the driver wrote as the
// machine's state moved (BUG-004).
func TestAPrintThatRunsToItsEndIsRecordedAsFinished(t *testing.T) {
	t.Parallel()

	server := startServer(t)
	server.awaitReady(t)

	listed := attachFinitePrinter(t, server, jobFramesToTheEnd)

	printable := firstPrintable(t, server, listed.ID, benchStorage, 0)

	sendPrintVerb(t, server, listed.ID, fmt.Sprintf(`{"verb":%q,"args":{"path":%q}}`,
		startPrintVerb, printable))

	var finished jobsView

	eventually(t, "the jobs route reported a print that ended on its own", func() bool {
		finished = server.jobs(t, listed.ID)

		return finished.Last != nil
	})

	if finished.Last.State != "FINISHED" {
		t.Fatalf("a print that ran to its end is stored as %q: %+v",
			finished.Last.State, finished.Last)
	}

	if finished.Current != nil {
		t.Fatalf("a machine whose print ended still reports a current job: %+v", finished.Current)
	}

	if finished.Last.Ended == nil {
		t.Fatalf("the print that finished carries no end: %+v", finished.Last)
	}

	if !strings.HasSuffix(finished.Last.Path, printable[strings.LastIndex(printable, "/")+1:]) {
		t.Fatalf("the row names %q and the print was started on %q", finished.Last.Path, printable)
	}

	t.Logf("observed: %s ran %s to its end and stored it as %s at %s",
		listed.ID, finished.Last.Path, finished.Last.State, *finished.Last.Ended)
}

// attachFinitePrinter registers a printer whose prints end by themselves after
// frames telemetry frames, runs it, and waits for it on the API.
//
// It is server.attachPrinter with one setting changed. The printer that case
// builds prints for ever, which is what every other job case wants: a print
// that ended while a verb was in flight would make the verb's own answer
// ambiguous.
func attachFinitePrinter(t *testing.T, s *server, frames int) printerView {
	t.Helper()

	printer, err := simulator.New(simulator.Config{
		Model: benchModel, BaseURL: s.baseURL, JobFrames: frames,
	})
	if err != nil {
		t.Fatalf("new %s: %v", benchModel, err)
	}

	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))

	if err = printer.Register(ctx); err != nil {
		cancel()
		t.Fatalf("register a %s against the running binary: %v", benchModel, err)
	}

	running := make(chan error, 1)
	go func() { running <- printer.Run(ctx) }()

	t.Cleanup(func() {
		cancel()
		<-running
	})

	var view printerView

	eventually(t, "the printer appeared on /api/v1/devices", func() bool {
		found, ok := s.findPrinter(t, printer.Serial())
		if !ok {
			return false
		}

		view = found

		return view.Connected
	})

	return view
}
