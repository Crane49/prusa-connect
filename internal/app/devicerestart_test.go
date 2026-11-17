package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// A restart, proved by restarting.
//
// The old backlog's conditions walk carries "a restart does not forget the fleet" as
// condition 5, open, and earlier research's inventory says the consequence in one
// line: nothing persists. Both cases below are that condition, and both are
// written as a restart rather than as a write followed by a read: a test that
// stores a value and reads it back inside one process proves nothing about a
// process that has stopped, because every structure it read from is still in
// memory.
//
// The first stops the core the way a signal does and starts it again. The
// second kills a separate operating-system process, which is the case the clean
// path cannot reach: nothing ran on the way out, so what the store holds is
// whatever the last flush left, and presence is still marked up in it.

// restartConfig is what both cases run, with the address filled in. It is the
// default configuration and one log level: what is being proved is that a
// deployment nobody configured for it remembers its fleet. The announcement on
// the local network is off as well, because the binary started from it is a
// new build on every run.
func restartConfig(addr string) string {
	return lines(
		"core:",
		"  log:",
		"    level: error",
		"  discovery:",
		"    enabled: false",
		"  api:",
		`    addr: "`+addr+`"`,
	)
}

// TestARestartRemembersTheFleet is the whole condition, over the real
// composition root: a printer dials in over the frozen wire, the core is
// stopped the way a signal stops it, and a core started again on the same
// directory lists the machine through the published API.
//
// Every assertion is on the bytes GET /api/v1/devices answered with, because
// that is what a client reads and it is where the fleet was being lost.
func TestARestartRemembersTheFleet(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	first := start(t, dir, "core:\n  log:\n    level: error\n")

	dialIn(t, first, "SIM-RESTART")

	before := printers(t, first)
	require.Len(t, before, 1)
	require.True(t, before[0].Connected)
	require.NotEmpty(t, before[0].State)

	// The flush is on a two second timer and the telemetry that filled the
	// state tree above has to reach the store before the process ends. A clean
	// stop writes one last time on its way out, so what this waits for is only
	// that the tree is not empty when it does.
	require.Eventually(t, func() bool {
		return telemetered(printers(t, first)[0].State)
	}, settle, 25*time.Millisecond)

	first.stopAndWait(t)

	// The second run. The same directory, so the same database file, and
	// nothing else crosses: every map, every session and every handle the first
	// core held is gone.
	second := start(t, dir, "core:\n  log:\n    level: error\n")

	after := printers(t, second)
	require.Len(t, after, 1, "the fleet was forgotten by a restart")

	found := after[0]

	t.Logf("observed: after a restart the listing answered %s, %s %s, connected=%v, state paths=%d",
		found.ID, found.Model, found.Serial, found.Connected, len(found.State))

	require.Equal(t, before[0].ID, found.ID, "the machine came back under a different id")
	require.Equal(t, "MK4", found.Model)
	require.Equal(t, "Prusa", found.Vendor)
	require.Equal(t, "SIM-RESTART", found.Serial)
	require.Equal(t, "xbuddy", found.Driver)
	require.Contains(t, found.State, "/state", "the state tree did not survive")

	// The rule earlier research states as a rule rather than as a column. Nothing
	// has reached this machine since the process began, so it is away, and the
	// wire says why and when it was last heard from.
	require.False(t, found.Connected,
		"a restart reported a machine as reachable that nothing has reached")
	require.NotNil(t, found.Departed, "a restored machine reports no departure at all")
	require.Contains(t, []string{departureSessionEnded, departureServerStopping}, found.Departed.Reason,
		"a restored machine reports a reason no client can read")
	require.False(t, found.Departed.At.IsZero(), "a restored machine has no last-seen time")

	// A capability set is resolved when a machine arrives and an earlier design decision moves
	// its epoch on every arrival, so a persisted one would be a claim about a
	// machine this process has not spoken to.
	require.Empty(t, found.Capabilities)
	require.Zero(t, found.CapabilityEpoch)

	// A machine that is not there takes no command, and the refusal is the one
	// a departed printer already gets rather than a panic on a session nothing
	// holds.
	sent := second.call(t, http.MethodPost, "/api/v1/devices/"+found.ID+"/commands",
		`{"verb":"`+nozzleVerb+`","args":{"nozzle_temperature":"215"}}`)
	require.Equal(t, http.StatusConflict, sent.status, sent.body)

	// And it is one device and not two: the record follows the directory, so a
	// restored machine that dials in again is the same row.
	single := second.get(t, "/api/v1/devices/"+found.ID)
	require.Equal(t, http.StatusOK, single.status, single.body)
}

// TestAKilledServerComesBackWithItsFleetAndNoPresence is the restart nothing
// prepared for, and it is a second operating-system process rather than this
// one: the binary is built, run, dialled into, and killed outright.
//
// Two things only this case can prove. The core's shutdown path never runs, so
// the store holds whatever the last two-second flush left rather than a tidy
// final write. And that store says the machine was reachable, because the
// process that wrote it was holding a socket to it: the start that follows has
// to put every device down before it serves, and this is where that is watched.
func TestAKilledServerComesBackWithItsFleetAndNoPresence(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binary := buildTheCore(t)

	addr := freeAddr(t)
	write(t, filepath.Join(dir, "config.yaml"), restartConfig(addr))

	killed := runTheCore(t, binary, dir, addr)

	printer, err := simulator.New(simulator.Config{
		Model: "MK4", Serial: "SIM-KILLED", BaseURL: "http://" + addr,
	})
	require.NoError(t, err)

	printing, stop := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan error, 1)

	go func() { done <- printer.RegisterAndRun(printing) }()

	// The store is read directly here rather than through the API, because what
	// this case is about is what is on the disk when the process dies. The
	// handle is this test's own and the file is in write-ahead logging, so
	// reading it while the core writes it is what WAL is for.
	graph := config.DefaultGraph(filepath.Join(dir, "config.yaml"))

	var stored deviceRow

	require.Eventually(t, func() bool {
		row, ok := readOneRecord(t, graph)
		stored = row

		return ok && row.departure == "" && len(row.document) > 2
	}, settle, 100*time.Millisecond,
		"no device record with a state tree reached %s before the kill", graph)

	t.Logf("observed: before the kill the store held %s, %s, departure=%q, %d bytes of state",
		stored.deviceID, stored.model, stored.departure, len(stored.document))

	// Nothing runs on the way out. No session is closed, no departure is
	// recorded and no last flush happens, which is a power cut and a kill -9
	// and an OOM kill.
	require.NoError(t, killed.Process.Kill())
	_, _ = killed.Process.Wait()

	stop()
	<-done

	// What was left on the disk is a row whose session never ended, which is
	// what makes this a crash and not a stop. The row cannot claim the machine
	// is reachable, because earlier research left no column that could: an empty
	// departure is as much as a dead process can assert.
	crashed, ok := readOneRecord(t, graph)
	require.True(t, ok)
	require.Empty(t, crashed.departure,
		"the killed process wrote itself down, so this case is not about a crash")

	// The start that follows.
	next := freeAddr(t)
	write(t, filepath.Join(dir, "config.yaml"), restartConfig(next))

	runTheCore(t, binary, dir, next)

	listed := readFleet(t, next)
	require.Len(t, listed, 1, "the killed process's fleet was forgotten")

	found := listed[0]

	t.Logf("observed: after the kill the listing answered %s, %s %s, connected=%v, state paths=%d",
		found.ID, found.Model, found.Serial, found.Connected, len(found.State))

	require.Equal(t, stored.deviceID, found.ID)
	require.Equal(t, "MK4", found.Model)
	require.Equal(t, "SIM-KILLED", found.Serial)
	require.Contains(t, found.State, "/state")
	require.False(t, found.Connected,
		"a start after a crash reported a machine as reachable because the dead process had been holding a socket")

	// And the answer is the schema's rather than a correction the start made.
	// Nothing in device_record can say a machine is reachable, so a second
	// reader of that table reaches the same answer this plane did without
	// running anything first, and a start that skipped the correction step
	// could not get it wrong.
	after, ok := readOneRecord(t, graph)
	require.True(t, ok)
	require.Empty(t, after.departure,
		"the start invented a departure for a session it never watched end")

	var presence bool

	require.Error(t, readColumn(t, graph, `SELECT connected FROM device_record`, &presence),
		"device_record holds a presence column again, so a restart can restore one")
}

// deviceRow is one device_record row, read with a handle of this test's own so
// that nothing under test reports on itself.
type deviceRow struct {
	deviceID string
	model    string
	// departure is why the last session ended and is empty while it was open,
	// so an empty one on a row nobody is writing is the mark of a crash. It is
	// read instead of a presence column because there is none: earlier research
	// refused one, on the ground that a stored presence can only ever be read
	// back as false.
	departure string
	document  string
}

// readOneRecord reads the single device record out of a graph file, and reports
// false while there is none. It opens read-only: the core owns the file.
func readOneRecord(t *testing.T, path string) (deviceRow, bool) {
	t.Helper()

	if _, err := os.Stat(path); err != nil {
		return deviceRow{}, false
	}

	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	require.NoError(t, err)

	defer func() { _ = db.Close() }()

	var row deviceRow

	err = db.QueryRowContext(t.Context(),
		`SELECT device_id, model, departure, state_document FROM device_record`).
		Scan(&row.deviceID, &row.model, &row.departure, &row.document)
	if err != nil {
		return deviceRow{}, false
	}

	return row, true
}

// readColumn runs one query against a graph file and returns whatever the
// engine said. It is here so a case can assert that a column is absent, which
// is a property of the schema and not of any value in it.
func readColumn(t *testing.T, path, query string, into any) error {
	t.Helper()

	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	require.NoError(t, err)

	defer func() { _ = db.Close() }()

	return db.QueryRowContext(t.Context(), query).Scan(into)
}

// readFleet reads the listing off a core running at addr.
func readFleet(t *testing.T, addr string) []httpapi.Device {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		"http://"+addr+"/api/v1/devices", nil)
	require.NoError(t, err)

	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)

	defer func() { _ = response.Body.Close() }()

	require.Equal(t, http.StatusOK, response.StatusCode)

	var listing struct {
		Items []httpapi.Device `json:"items"`
	}

	require.NoError(t, json.NewDecoder(response.Body).Decode(&listing))

	return listing.Items
}

// buildTheCore builds the shipping binary, which is what the second case runs.
// It is the command cmd/prusa-connect ships and not a harness: a restart
// is about a process, and a process is what this produces.
func buildTheCore(t *testing.T) string {
	t.Helper()

	toolchain, err := exec.LookPath("go")
	require.NoError(t, err)

	name := "prusa-connect"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	out := filepath.Join(t.TempDir(), name)

	build := exec.CommandContext(t.Context(), toolchain, "build", "-o", out,
		"github.com/prusa3d/prusa-connect/cmd/prusa-connect")
	build.Dir = repositoryRoot(t)

	said, err := build.CombinedOutput()
	require.NoError(t, err, string(said))

	return out
}

// runTheCore starts the built binary against a configuration directory and
// waits until it answers, and kills it with the test whatever else happened.
func runTheCore(t *testing.T, binary, dir, addr string) *exec.Cmd {
	t.Helper()

	// A core that has recorded no first-run decision serves the probes, the page
	// and the first run and refuses every other path. This case is about what a
	// server that has been set up keeps across a crash, so the decision is
	// written the way an operator's own call writes it.
	recordOpenSetup(t, dir)

	// `core` names this composition root. It is the binary's default and is
	// written out anyway, so the case says which server it started.
	core := exec.Command(binary, "core", "--config", filepath.Join(dir, "config.yaml"))
	core.Stdout = os.Stderr
	core.Stderr = os.Stderr

	require.NoError(t, core.Start())

	t.Cleanup(func() {
		_ = core.Process.Kill()
		_, _ = core.Process.Wait()
	})

	require.Eventually(t, func() bool {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
			"http://"+addr+"/api/v1/health", nil)
		if err != nil {
			return false
		}

		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return false
		}

		_ = response.Body.Close()

		return response.StatusCode == http.StatusOK
	}, settle, 50*time.Millisecond, "the core never answered on %s", addr)

	return core
}

// repositoryRoot is where the module lives, two directories above this package.
func repositoryRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(root, "go.mod"))
	require.NoError(t, err, "the repository root is not where this test looked")

	return root
}

// defaultGraphPathIsUnderTheConfigDirectory is what makes both cases restart
// against one database: config.Load derives core.authz.database from the
// configuration file's own directory, so a second run over the same directory
// opens the same file. It is asserted rather than assumed, because the whole of
// both cases rests on it.
func TestTheGraphPathFollowsTheConfigurationFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	write(t, path, restartConfig("127.0.0.1:0"))

	core, err := config.Load(path)
	require.NoError(t, err)

	within, err := filepath.Rel(dir, core.Authz.Database)
	require.NoError(t, err)
	require.NotContains(t, within, "..",
		"the graph is outside the configuration's own directory, so a restart there is a restart "+
			"against a different database")
}
