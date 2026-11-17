package app

import (
	"database/sql"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// Running the history cases against both shipped engines, through the real
// composition root.
//
// The store's own suite already runs on both (adapters/db/internal/histstore),
// and that proves the statements. It does not prove the deployment: the config
// block an operator writes, the migration that runs inside the supervised
// start, and the start-up sweep that closes what the last process left open are
// the composition root's and are reached only by starting the core. A case that
// ran on SQLite alone would leave the PostgreSQL deployment of every one of
// those unproven.
//
// The environment is internal/authz's, which is the same pair `make
// test-engines` sets: one deployment has one engine, so an operator or a CI
// lane learns one pair rather than three. postgresDSNEnv is declared in
// graphengine_test.go and requiredEngineNames in devicerecordevents_test.go,
// because this package already reads both.

// historyEngine is one engine an app-level history case runs against: the
// configuration that declares a provider on it, and a handle of the case's own
// to read the store back with.
//
// Reading it back with a handle of the case's own is the property and not a
// convenience: what is under test is what reached a store, and a value read out
// of the process that wrote it proves nothing about the row.
type historyEngine struct {
	Name string
	// Config is the deployment's configuration file for this engine.
	Config func(t *testing.T, dir string) string
	// Open is a handle onto the store the config above declares.
	Open func(t *testing.T, dir string) *sql.DB
	// JSONText renders "read this key out of a JSON column" in this engine's
	// dialect, which is the one place the two disagree in these cases.
	JSONText func(column, key string) string
	// True is how this engine spells a true boolean in a literal statement.
	True string
}

// historyEngines is what this run will exercise, failing rather than skipping
// when the environment says an engine is required and it is not reachable.
func historyEngines(t *testing.T) []historyEngine {
	t.Helper()

	available := []historyEngine{sqliteHistoryEngine()}

	if postgres, ok := postgresHistoryEngine(); ok {
		available = append(available, postgres)
	}

	names := make([]string, 0, len(available))
	for _, one := range available {
		names = append(names, one.Name)
	}

	for _, required := range requiredEngineNames() {
		if !slices.Contains(names, required) {
			t.Fatalf("AUTHZ_REQUIRED_ENGINES requires the %s engine and this build has %v",
				required, names)
		}
	}

	return available
}

// forEachHistoryEngine runs body as a subtest per reachable engine.
//
// The subtests are not parallel with each other. Each one starts a core on a
// listener of its own and drives a simulated printer into it, and two of those
// inside one case is a machine doing twice the work for no more evidence.
func forEachHistoryEngine(t *testing.T, body func(t *testing.T, e historyEngine)) {
	t.Helper()

	for _, one := range historyEngines(t) {
		t.Run(one.Name, func(t *testing.T) {
			body(t, one)
		})
	}
}

// sqliteHistoryEngine is always there: the driver is linked into every build.
func sqliteHistoryEngine() historyEngine {
	return historyEngine{
		Name: "sqlite",
		Config: func(t *testing.T, dir string) string {
			t.Helper()

			return historyConfig(dir)
		},
		Open: func(t *testing.T, dir string) *sql.DB {
			t.Helper()

			db, err := sql.Open("sqlite",
				filepath.ToSlash(historyFile(dir))+"?_pragma=busy_timeout(5000)")
			require.NoError(t, err)

			t.Cleanup(func() { _ = db.Close() })

			return db
		},
		JSONText: func(column, key string) string {
			return `json_extract(` + column + `, '$.` + key + `')`
		},
		True: "1",
	}
}

// count answers one count from this engine's store.
func (e historyEngine) count(t *testing.T, db *sql.DB, query string) int {
	t.Helper()

	var total int

	if err := db.QueryRowContext(t.Context(), query).Scan(&total); err != nil {
		return 0
	}

	return total
}

// text answers one text value, and the empty string where the query answers no
// row.
func (e historyEngine) text(t *testing.T, db *sql.DB, query string) string {
	t.Helper()

	var answer string

	if err := db.QueryRowContext(t.Context(), query).Scan(&answer); err != nil {
		return ""
	}

	return answer
}

// exec runs one statement against the store. It is how a case makes the state a
// process that died would have left, which nothing in the running core can be
// asked to produce on purpose.
func (e historyEngine) exec(t *testing.T, db *sql.DB, statement string) {
	t.Helper()

	_, err := db.ExecContext(t.Context(), statement)
	require.NoError(t, err)
}
