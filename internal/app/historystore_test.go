package app

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/event"
)

// The history provider, proved through the real composition root.
//
// Every case here starts the core the binary starts, dials a simulated printer
// into it over the frozen wire, and then reads the history file with a handle of
// its own. Reading it directly is the point: what is under test is what reached
// a disk, and a value read back out of the process that wrote it proves nothing
// about a process that has stopped.

// historyConfig is a deployment that keeps its past: one declared instance of
// the SQLite adapter, which is the only spelling there is (ADR-20260901-rf2 rule 18).
//
// Retention is "0" so that nothing here depends on the wall clock. The default
// is 48 hours of raw rows, and a case that swept while it ran would be a case
// that fails on a machine whose clock moved.
func historyConfig(dir string) string {
	return lines(
		"core:",
		"  log:",
		"    level: error",
		"resources:",
		"  history:",
		"    past:",
		"      type: sqlite",
		"      config:",
		"        path: "+historyFile(dir),
		"        retention:",
		`          raw: "0"`,
	)
}

// storingPast starts the core with a history instance and waits until that
// instance is serving.
//
// The wait is the property and not a convenience. A provider is a supervised
// resource now, and the supervisor scatters an instance's first attempt so a
// deployment does not open every resource on one millisecond, so a provider is
// attached a moment after the listener binds. It stores what it sees from the
// moment it is attached, which is why a printer is dialled in
// after this returns rather than before it.
func storingPast(t *testing.T, dir string) *process {
	t.Helper()

	running := start(t, dir, historyConfig(dir))

	require.Eventually(t, func() bool {
		return running.instanceState(t, "history/past") == "ready"
	}, settle, 100*time.Millisecond, "the declared history provider never reached ready")

	return running
}

// historyFile is where the store goes for a configuration file, which is beside
// the graph and never inside it.
func historyFile(dir string) string {
	return filepath.Join(dir, "config.d", "history.db")
}

// TestAConfiguredProviderStoresSamplesAndFacts is the whole of what this wave
// owes: a measurement stored, an event stored, and both still there after the
// process that wrote them has gone.
func TestAConfiguredProviderStoresSamplesAndFacts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	first := storingPast(t, dir)

	dialIn(t, first, "SIM-HISTORY")

	store := historyFile(dir)

	// The flush is on a two second timer, so this waits for the first write
	// rather than for the frames that fill it.
	var samples, facts int

	require.Eventually(t, func() bool {
		samples = countRows(t, store, `SELECT count(*) FROM history_sample`)
		facts = countRows(t, store, `SELECT count(*) FROM history_fact`)

		return samples > 0 && facts > 0
	}, settle, 250*time.Millisecond,
		"no sample and no fact reached %s while a printer was reporting", store)

	series := countRows(t, store, `SELECT count(*) FROM history_series`)

	t.Logf("observed: while running, %s held %d samples over %d series and %d facts",
		store, samples, series, facts)

	require.Positive(t, series)
	require.Less(t, series, samples,
		"the reference was repeated per row rather than interned")

	// The device record's own change is among them, which is the frame this
	// provider was built to consume: it is class state, so the envelope's class
	// alone sent it to the fact half with no type table in between.
	t.Logf("observed: the fact types stored are %v", factTypes(t, store))

	require.Positive(t, countRows(t, store,
		`SELECT count(*) FROM history_fact WHERE type = '`+event.TypeDeviceRecordChanged+`'`),
		"no device record change reached the fact half")

	// And no telemetry did. A conflated measurement in the fact table would be
	// one row per frame rather than one value per key per window.
	require.Zero(t, countRows(t, store,
		`SELECT count(*) FROM history_fact WHERE type = '`+event.TypeDeviceTelemetrySampled+`'`),
		"a telemetry frame was stored as a fact")

	// The types stored are whatever the bus carried, and nothing in the feed
	// holds a list of them. printer.arrived is stored and no line of the feed
	// names it: the class on the frame is the only thing read, which is what
	// lets the event vocabulary grow without this provider being edited.
	require.Positive(t, countRows(t, store,
		`SELECT count(*) FROM history_fact WHERE type = '`+event.TypeDeviceArrived+`'`),
		"a durable frame of a type the feed does not name reached no row")

	// The samples are the state tree's paths, which is what makes a metric name
	// queryable across driver releases.
	require.Positive(t, countRows(t, store,
		`SELECT count(*) FROM history_series WHERE ref LIKE 'device:%' AND metric LIKE '/%'`),
		"a series was named something other than an origin and a state path")

	first.stopAndWait(t)

	// The second run. The same directory, so the same file, and nothing else
	// crosses: every buffer the first core held is gone.
	second := start(t, dir, historyConfig(dir))

	afterSamples := countRows(t, store, `SELECT count(*) FROM history_sample`)
	afterFacts := countRows(t, store, `SELECT count(*) FROM history_fact`)

	t.Logf("observed: after a restart %s held %d samples and %d facts",
		store, afterSamples, afterFacts)

	require.GreaterOrEqual(t, afterSamples, samples, "a restart forgot the samples")
	require.GreaterOrEqual(t, afterFacts, facts, "a restart forgot the facts")

	// And the second core is writing into the same file rather than starting a
	// new one beside it.
	require.NoError(t, second.wait())
}

// TestEveryArrivalOfAFleetIsStored is the gate the store and the subscription
// each passed on their own while the pair of them lost a row.
//
// Registering a printer moves the authorization graph a generation, so a site
// powering up moves it once per machine, and the feed is one subscriber whose
// visible set widens after all of them have landed. What it is then handed of
// each newly visible printer is bounded by a generation, and a bound taken from
// the graph as it now stands is past every arrival but the last.
//
// The count is the whole assertion: one row per printer, named by the printer,
// so a store holding four of six fails here rather than in a farm six months
// from now.
func TestEveryArrivalOfAFleetIsStored(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := storingPast(t, dir)

	fleet := []string{"SIM-FLEET-1", "SIM-FLEET-2", "SIM-FLEET-3", "SIM-FLEET-4", "SIM-FLEET-5"}
	dialInFleet(t, running, fleet...)

	store := historyFile(dir)
	arrivals := `SELECT count(*) FROM history_fact WHERE type = '` + event.TypeDeviceArrived + `'`

	var stored int

	require.Eventually(t, func() bool {
		stored = countRows(t, store, arrivals)

		return stored >= len(fleet)
	}, settle, 250*time.Millisecond,
		"%s holds %d arrivals for the %d printers that dialled in", store, stored, len(fleet))

	t.Logf("observed: %d printers dialled in and %s holds %d arrival rows",
		len(fleet), store, stored)

	// One row per printer and not five rows for one printer. A count alone would
	// pass on a store that recorded the same machine arriving five times.
	require.Equal(t, len(fleet), countRows(t, store,
		`SELECT count(DISTINCT ref) FROM history_fact WHERE type = '`+
			event.TypeDeviceArrived+`'`),
		"the arrivals stored do not name one printer each")
}

// TestAProviderNobodyConfiguredCostsNothing is the negative. A deployment that
// wants no history opens no file, runs no migration and stores no row, which is
// an earlier design decision: the core emits, and a provider may catch.
func TestAProviderNobodyConfiguredCostsNothing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, "core:\n  log:\n    level: error\n")

	dialIn(t, running, "SIM-NOHISTORY")

	// Long enough that two flush intervals of a configured provider would have
	// passed, so this is the absence of a write rather than a race with one.
	time.Sleep(2 * historyFlushInterval)

	store := historyFile(dir)

	_, err := os.Stat(store)
	require.ErrorIs(t, err, os.ErrNotExist,
		"a deployment that configured no history opened %s", store)

	// The graph is there, so the directory this would have gone in exists and
	// the absence above is the provider's and not the directory's.
	graph := filepath.Join(dir, "config.d", "authz.db")
	_, err = os.Stat(graph)
	require.NoError(t, err)

	t.Logf("observed: with no history configured the graph exists at %s and %s does not",
		graph, store)
}

// TestTheStoreIsNeverTheGraphsFile is an earlier design decision read off a running
// deployment. One SQLite file put a small authorization write's p99 at the five
// second busy timeout, so the two schemas are two files and neither one can take
// the other's write lock.
func TestTheStoreIsNeverTheGraphsFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := storingPast(t, dir)

	dialIn(t, running, "SIM-TWOFILES")

	store := historyFile(dir)
	graph := filepath.Join(dir, "config.d", "authz.db")

	require.Eventually(t, func() bool {
		return countRows(t, store, `SELECT count(*) FROM history_sample`) > 0
	}, settle, 250*time.Millisecond)

	// The graph holds no history table, and the history file holds no graph
	// table. Either one failing is the two schemas having been merged.
	require.Zero(t, countRows(t, graph,
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name LIKE 'history_%'`),
		"the history schema was created inside the authorization graph")
	require.Zero(t, countRows(t, store,
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name LIKE 'authz_%'`),
		"the authorization schema was created inside the history store")

	t.Logf("observed: %s and %s hold no table of each other's", graph, store)
}

// factTypes is every event type the store holds, for the line that says what a
// running deployment actually put there.
func factTypes(t *testing.T, path string) []string {
	t.Helper()

	db, err := sql.Open("sqlite", filepath.ToSlash(path)+"?_pragma=busy_timeout(5000)")
	require.NoError(t, err)

	defer func() { _ = db.Close() }()

	rows, err := db.QueryContext(t.Context(), `SELECT DISTINCT type FROM history_fact ORDER BY type`)
	require.NoError(t, err)

	defer func() { _ = rows.Close() }()

	var types []string

	for rows.Next() {
		var name string

		require.NoError(t, rows.Scan(&name))

		types = append(types, name)
	}

	require.NoError(t, rows.Err())

	return types
}

// countRows answers one count from a SQLite file with a handle of this test's
// own, so nothing under test reports on itself. It opens read-only: the core
// owns the file, and it is in write-ahead logging, which is what lets this read
// while the core writes.
func countRows(t *testing.T, path, query string) int {
	t.Helper()

	if _, err := os.Stat(path); err != nil {
		return 0
	}

	db, err := sql.Open("sqlite", filepath.ToSlash(path)+"?_pragma=busy_timeout(5000)")
	require.NoError(t, err)

	defer func() { _ = db.Close() }()

	var count int
	if err = db.QueryRowContext(t.Context(), query).Scan(&count); err != nil {
		return 0
	}

	return count
}
