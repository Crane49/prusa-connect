package engine

import (
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func TestAFailedMigrationRollsBackTheWholeStore(t *testing.T) {
	t.Parallel()

	db, err := OpenSQLite(t.Context(), filepath.Join(t.TempDir(), "store.db"), 0)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	ledger := Ledger{
		Store: "test",
		Table: "test_migration",
		Files: fstest.MapFS{
			"sqlite/0001_first.sql":   {Data: []byte("CREATE TABLE first_table (id INTEGER PRIMARY KEY)")},
			"sqlite/0002_failure.sql": {Data: []byte("CREATE TABLE")},
			"postgres/0001_first.sql": {Data: []byte("CREATE TABLE first_table (id INTEGER PRIMARY KEY)")},
			"postgres/0002_failure.sql": {
				Data: []byte("CREATE TABLE"),
			},
		},
	}

	require.Error(t, Migrate(t.Context(), db, ledger))

	for _, table := range []string{"first_table", "test_migration"} {
		var found int
		require.NoError(t, db.QueryRowContext(t.Context(),
			"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&found))
		require.Zero(t, found, "%s survived a failed migration transaction", table)
	}
}

func TestRehearsalRollsBackASuccessfulStoreMigration(t *testing.T) {
	t.Parallel()

	db, err := OpenSQLite(t.Context(), filepath.Join(t.TempDir(), "store.db"), 0)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	ledger := Ledger{
		Store: "test",
		Table: "test_migration",
		Files: fstest.MapFS{
			"sqlite/0001_first.sql":   {Data: []byte("CREATE TABLE first_table (id INTEGER PRIMARY KEY)")},
			"postgres/0001_first.sql": {Data: []byte("CREATE TABLE first_table (id INTEGER PRIMARY KEY)")},
		},
	}

	require.NoError(t, Rehearse(t.Context(), db, ledger))

	var found int
	require.NoError(t, db.QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'first_table'").Scan(&found))
	require.Zero(t, found)

	require.NoError(t, Migrate(t.Context(), db, ledger))
	require.NoError(t, db.QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM first_table").Scan(&found))
}

func TestNewerCompatibleMigrationIsTolerated(t *testing.T) {
	t.Parallel()

	db, err := OpenSQLite(t.Context(), filepath.Join(t.TempDir(), "store.db"), 0)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	ledger := testLedger()
	require.NoError(t, Migrate(t.Context(), db, ledger))

	_, err = db.ExecContext(t.Context(),
		`INSERT INTO test_migration (name, checksum, applied_at)
		 VALUES ('20991231T235959_future.sql', 'future', '2026-01-01T00:00:00Z')`)
	require.NoError(t, err)
	require.NoError(t, Migrate(t.Context(), db, ledger))
}

func TestCompatibilityFloorRejectsAnOlderBuild(t *testing.T) {
	t.Parallel()

	db, err := OpenSQLite(t.Context(), filepath.Join(t.TempDir(), "store.db"), 0)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	ledger := testLedger()
	require.NoError(t, Migrate(t.Context(), db, ledger))

	_, err = db.ExecContext(t.Context(), "UPDATE test_schema SET min_schema_version = 2 WHERE id = 1")
	require.NoError(t, err)
	require.ErrorIs(t, Migrate(t.Context(), db, ledger), ErrSchemaTooNew)
}

func TestRenamedShippedMigrationIsRejected(t *testing.T) {
	t.Parallel()

	db, err := OpenSQLite(t.Context(), filepath.Join(t.TempDir(), "store.db"), 0)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	ledger := testLedger()
	require.NoError(t, Migrate(t.Context(), db, ledger))

	_, err = db.ExecContext(t.Context(),
		"UPDATE test_migration SET name = '0001_renamed.sql'")
	require.NoError(t, err)

	err = Migrate(t.Context(), db, ledger)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrMigrationMissing)
}

func testLedger() Ledger {
	return Ledger{
		Store:  "test",
		Table:  "test_migration",
		Compat: 1,
		Files: fstest.MapFS{
			"sqlite/0001_first.sql":   {Data: []byte("CREATE TABLE first_table (id INTEGER PRIMARY KEY)")},
			"postgres/0001_first.sql": {Data: []byte("CREATE TABLE first_table (id INTEGER PRIMARY KEY)")},
		},
	}
}

// Two processes starting against one fresh file, such as a restart overlap or
// a second replica, both migrate it. Each must return nil, and each migration
// must be recorded once.
func TestTwoHandlesMigratingOneFreshFileAtOnce(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"sqlite/0001_a.sql": {Data: []byte("CREATE TABLE a (id INTEGER PRIMARY KEY);")},
		"sqlite/0002_b.sql": {Data: []byte("CREATE TABLE b (id INTEGER PRIMARY KEY);")},
		"sqlite/0003_c.sql": {Data: []byte("CREATE TABLE c (id INTEGER PRIMARY KEY);")},
	}
	ledger := Ledger{Store: "racestore", Table: "race_migration", Files: files}

	// One round failed every time before the run held a lock, so a few rounds
	// are enough.
	const rounds = 10

	for range rounds {
		path := filepath.Join(t.TempDir(), "race.db")

		one, err := OpenSQLite(t.Context(), path, 0)
		require.NoError(t, err)

		two, err := OpenSQLite(t.Context(), path, 0)
		require.NoError(t, err)

		var (
			wg   sync.WaitGroup
			errs [2]error
		)

		for i, db := range []*sql.DB{one, two} {
			wg.Go(func() { errs[i] = Migrate(t.Context(), db, ledger) })
		}

		wg.Wait()

		require.NoError(t, errs[0])
		require.NoError(t, errs[1])

		var recorded int
		require.NoError(t, one.QueryRowContext(t.Context(),
			`SELECT COUNT(*) FROM race_migration`).Scan(&recorded))
		require.Equal(t, len(files), recorded)

		require.NoError(t, one.Close())
		require.NoError(t, two.Close())
	}
}
