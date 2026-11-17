package engine

import (
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

// The environment the store suites read, internal/authz's names: one
// invocation arms every suite.
const (
	requiredEnginesEnv = "AUTHZ_REQUIRED_ENGINES"
	postgresDSNEnv     = "AUTHZ_TEST_POSTGRES_DSN"
)

// testEngine is one SQL backend these cases run against.
type testEngine struct {
	name     string
	postgres bool
	open     func(tb testing.TB) *sql.DB
}

// testEngines is every engine this run reaches. An engine the environment
// requires and the run cannot reach fails the test, because a run that
// skipped it would still report ok.
func testEngines(t *testing.T) []testEngine {
	t.Helper()

	reached := []testEngine{{name: "sqlite", open: func(tb testing.TB) *sql.DB {
		tb.Helper()

		db, err := OpenSQLite(tb.Context(), filepath.Join(tb.TempDir(), "store.db"), 0)
		require.NoError(tb, err)
		tb.Cleanup(func() { require.NoError(tb, db.Close()) })

		return db
	}}}

	if e, ok := postgresTestEngine(); ok {
		reached = append(reached, e)
	}

	for name := range strings.SplitSeq(os.Getenv(requiredEnginesEnv), ",") {
		name = strings.TrimSpace(name)
		if name != "" && !slices.ContainsFunc(reached, func(e testEngine) bool { return e.name == name }) {
			t.Fatalf("%s requires the %s engine and this run does not reach it", requiredEnginesEnv, name)
		}
	}

	return reached
}

// forEachTestEngine runs body as a parallel subtest per reachable engine.
func forEachTestEngine(t *testing.T, body func(t *testing.T, e testEngine)) {
	t.Helper()

	for _, e := range testEngines(t) {
		t.Run(e.name, func(t *testing.T) {
			t.Parallel()
			body(t, e)
		})
	}
}

// datedLedger is a store whose files say the same on both engines.
func datedLedger(e testEngine, files map[string]string) Ledger {
	out := fstest.MapFS{}
	for name, content := range files {
		out["sqlite/"+name] = &fstest.MapFile{Data: []byte(content)}
		out["postgres/"+name] = &fstest.MapFile{Data: []byte(content)}
	}

	return Ledger{Store: "test", Table: "test_migration", Files: out, Postgres: e.postgres}
}

func ledgerRows(t *testing.T, db *sql.DB) []string {
	t.Helper()

	rows, err := db.QueryContext(t.Context(), `SELECT name FROM test_migration ORDER BY name`)
	require.NoError(t, err)

	defer func() { _ = rows.Close() }()

	var names []string

	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))

		names = append(names, name)
	}

	require.NoError(t, rows.Err())

	return names
}

//nolint:gochecknoglobals // the files the cases below share, never written.
var (
	numbered = map[string]string{"0001_first.sql": "CREATE TABLE first_table (id INTEGER PRIMARY KEY)"}

	laterBranch = map[string]string{
		"0001_first.sql":                 numbered["0001_first.sql"],
		"20261028T120000_second_tbl.sql": "CREATE TABLE second_table (id INTEGER PRIMARY KEY)",
	}

	merged = map[string]string{
		"0001_first.sql":                 numbered["0001_first.sql"],
		"20261027T090000_first_note.sql": "ALTER TABLE first_table ADD COLUMN note TEXT",
		"20261028T120000_second_tbl.sql": laterBranch["20261028T120000_second_tbl.sql"],
	}
)

// A database a branch build migrated with the later of two dated migrations
// takes the earlier one when the release that merged both starts.
func TestADatedMigrationFromAParallelBranchIsApplied(t *testing.T) {
	t.Parallel()

	forEachTestEngine(t, func(t *testing.T, e testEngine) {
		db := e.open(t)
		require.NoError(t, Migrate(t.Context(), db, datedLedger(e, laterBranch)))
		require.NoError(t, Migrate(t.Context(), db, datedLedger(e, merged)))

		require.Equal(t, []string{"0001_first.sql", "20261027T090000_first_note.sql",
			"20261028T120000_second_tbl.sql"}, ledgerRows(t, db))

		_, err := db.ExecContext(t.Context(), `INSERT INTO first_table (id, note) VALUES (1, 'kept')`)
		require.NoError(t, err)
	})
}

// A database a newer build migrated holds a dated migration this build does
// not ship and that sorts after all it does. It opens.
func TestADatabaseANewerBuildMigratedOpens(t *testing.T) {
	t.Parallel()

	forEachTestEngine(t, func(t *testing.T, e testEngine) {
		db := e.open(t)
		require.NoError(t, Migrate(t.Context(), db, datedLedger(e, merged)))
		require.NoError(t, Migrate(t.Context(), db, datedLedger(e, numbered)))
	})
}

// A ledger row this build does not ship that sorts before one it ships was
// renamed or deleted, and so was a numbered one, because a new migration is
// dated.
func TestARenamedOrDeletedMigrationIsRefused(t *testing.T) {
	t.Parallel()

	for name, renamed := range map[string]string{
		"a dated migration renamed": "20261027T090000_renamed.sql",
		"a dated migration deleted": "20261027T080000_gone.sql",
		"a numbered one after all":  "0002_late.sql",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			forEachTestEngine(t, func(t *testing.T, e testEngine) {
				db := e.open(t)
				require.NoError(t, Migrate(t.Context(), db, datedLedger(e, merged)))

				_, err := db.ExecContext(t.Context(), render(e, `INSERT INTO test_migration (name, checksum, applied_at)
					VALUES (?, 'x', '2026-10-27 09:00:00')`), renamed)
				require.NoError(t, err)

				err = Migrate(t.Context(), db, datedLedger(e, merged))
				require.ErrorIs(t, err, ErrMigrationMissing)
				require.ErrorContains(t, err, renamed)
			})
		})
	}
}

// A floor migration refuses a build that does not ship it, and a floor table
// a build before the floor migration made gains the column.
func TestAFloorMigrationRefusesABuildWithoutIt(t *testing.T) {
	t.Parallel()

	forEachTestEngine(t, func(t *testing.T, e testEngine) {
		db := e.open(t)

		// The floor as the build before the floor migration left it.
		_, err := db.ExecContext(t.Context(),
			`CREATE TABLE test_schema (id INTEGER PRIMARY KEY, min_schema_version INTEGER NOT NULL)`)
		require.NoError(t, err)

		breaking := datedLedger(e, merged)
		breaking.Floor = "20261027T090000_first_note.sql"
		require.NoError(t, Migrate(t.Context(), db, breaking))

		require.ErrorIs(t, Migrate(t.Context(), db, datedLedger(e, numbered)), ErrSchemaTooNew)
		require.NoError(t, Migrate(t.Context(), db, datedLedger(e, merged)))

		var floor string
		require.NoError(t, db.QueryRowContext(t.Context(),
			`SELECT min_migration FROM test_schema WHERE id = 1`).Scan(&floor))
		require.Equal(t, breaking.Floor, floor)
	})
}

func TestAMigrationThatCommitsIsRefusedBeforeAnythingRuns(t *testing.T) {
	t.Parallel()

	forEachTestEngine(t, func(t *testing.T, e testEngine) {
		db := e.open(t)
		ledger := datedLedger(e, map[string]string{
			"0001_first.sql":               numbered["0001_first.sql"],
			"20261027T090000_escaping.sql": "CREATE TABLE escaped (id INTEGER);\nCOMMIT;",
		})

		require.ErrorIs(t, Migrate(t.Context(), db, ledger), ErrOutsideTransaction)
		require.ErrorIs(t, Rehearse(t.Context(), db, ledger), ErrOutsideTransaction)

		var tables int
		require.NoError(t, db.QueryRowContext(t.Context(), render(e, tableExistsQuery(e)), "first_table").
			Scan(&tables))
		require.Zero(t, tables)
	})
}

func render(e testEngine, query string) string {
	if e.postgres {
		return Numbered(query)
	}

	return query
}

func tableExistsQuery(e testEngine) string {
	if e.postgres {
		return `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = ?`
	}

	return `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`
}
