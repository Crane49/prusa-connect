package hostschema

import (
	"database/sql"
	"io/fs"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
)

// concurrentStarts is how many processes race into the same empty database. It
// is above the pool's default idle count on purpose, so the run also exercises
// a handle that had to open a connection while another one held the lock.
const concurrentStarts = 8

// migrator is the host schema over db, failing the test when it cannot be built.
func migrator(tb testing.TB, db *sql.DB, e engine) *sqlstore.Migrator {
	tb.Helper()

	m, err := Migrator(db, e.Engine, "0.0.0-test")
	require.NoError(tb, err)

	return m
}

// withOwner is the host schema with one more owner after it.
func withOwner(tb testing.TB, db *sql.DB, e engine, owner sqlstore.Migrations) *sqlstore.Migrator {
	tb.Helper()

	schema := Schema()
	schema.Owners = append(schema.Owners, owner)

	m, err := sqlstore.NewMigrator(db, e.Engine, schema, "")
	require.NoError(tb, err)

	return m
}

// anOwner is a one-file owner whose file says the same on both engines. The
// file is dated, as every file a later build adds is.
func anOwner(statement string) sqlstore.Migrations {
	return sqlstore.Migrations{Owner: "probe", Files: fstest.MapFS{
		"sqlite/20261027T120000_probe.sql":   {Data: []byte(statement)},
		"postgres/20261027T120000_probe.sql": {Data: []byte(statement)},
	}}
}

func ledgerNames(t *testing.T, db *sql.DB) []string {
	t.Helper()

	rows, err := db.QueryContext(t.Context(), `SELECT name FROM schema_migration ORDER BY name`)
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

// TestTheHostSchemaRecordsEveryOwnersBaselineOnce is a fresh database: every
// owner's file is applied once and recorded under the owner's name, and a second
// run changes nothing. The files after a baseline are recorded the same way.
func TestTheHostSchemaRecordsEveryOwnersBaselineOnce(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.OpenEmpty(t)

		require.NoError(t, migrator(t, db, e).Migrate(t.Context()))
		require.NoError(t, migrator(t, db, e).Migrate(t.Context()), "a second run is not a no-op")

		require.Equal(t, []string{
			"authz/0001_schema.sql",
			"dashboards/0001_schema.sql",
			"dbstore/0001_schema.sql",
			"dbstore/0002_oidc_subject.sql",
			"dbstore/0003_scopes_and_app_pairings.sql",
			"devicestore/0001_schema.sql",
			"devicestore/0002_job_objects.sql",
			"devicestore/0003_printer_job_by_start.sql",
			"devicestore/20261030T005519_printer_job_outcome.sql",
			"devicestore/20261116T120000_camera.sql",
			"instanceid/0001_schema.sql",
		}, ledgerNames(t, db))

		require.NoError(t, migrator(t, db, e).RequireSchemaOfThisBuild(t.Context()))
	})
}

// TestTheSchemaVersionCountsTheShippedMigrations holds the floor to the files:
// the floor is compared against the version, and the version is the count.
func TestTheSchemaVersionCountsTheShippedMigrations(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		version, err := migrator(t, e.OpenEmpty(t), e).Version()
		require.NoError(t, err)
		require.Equal(t, 11, version)
		require.LessOrEqual(t, Schema().Compat, version, "the floor is above what this build understands")
	})
}

// TestAnEditedMigrationIsRefused is why the ledger records a checksum: an edit
// to an applied file is otherwise skipped in silence.
func TestAnEditedMigrationIsRefused(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.OpenEmpty(t)

		require.NoError(t, withOwner(t, db, e, anOwner(`CREATE TABLE probe (a TEXT NOT NULL)`)).Migrate(t.Context()))

		err := withOwner(t, db, e, anOwner(`CREATE TABLE probe (a TEXT NOT NULL, b TEXT)`)).Migrate(t.Context())
		require.ErrorIs(t, err, sqlstore.ErrMigrationChanged)
		require.ErrorContains(t, err, "probe/20261027T120000_probe.sql")
	})
}

// TestALaterCompatibleMigrationIsTolerated lets an older binary start while
// the database's recorded compatibility floor still says it can.
func TestALaterCompatibleMigrationIsTolerated(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.OpenEmpty(t)

		require.NoError(t, withOwner(t, db, e, anOwner(`CREATE TABLE probe (a TEXT NOT NULL)`)).Migrate(t.Context()))

		require.NoError(t, migrator(t, db, e).Migrate(t.Context()))

		err := migrator(t, db, e).RequireSchemaOfThisBuild(t.Context())
		require.ErrorIs(t, err, sqlstore.ErrSchemaMismatch)
		require.ErrorIs(t, err, sqlstore.ErrSchemaNewer)
	})
}

func TestARenamedShippedMigrationIsRefused(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.OpenEmpty(t)
		require.NoError(t, migrator(t, db, e).Migrate(t.Context()))

		_, err := db.ExecContext(t.Context(),
			`UPDATE schema_migration SET name = 'authz/0001_renamed.sql' WHERE name = 'authz/0001_schema.sql'`)
		require.NoError(t, err)

		err = migrator(t, db, e).Migrate(t.Context())
		require.ErrorIs(t, err, sqlstore.ErrMigrationMissing)
		require.ErrorContains(t, err, "authz/0001_renamed.sql")
	})
}

func TestAHostMigrationSetRollsBackTogether(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.OpenEmpty(t)
		owner := sqlstore.Migrations{Owner: "probe", Files: fstest.MapFS{
			"sqlite/0001_probe.sql":   {Data: []byte(`CREATE TABLE probe (a TEXT NOT NULL)`)},
			"sqlite/0002_failure.sql": {Data: []byte(`CREATE TABLE`)},
			"postgres/0001_probe.sql": {Data: []byte(`CREATE TABLE probe (a TEXT NOT NULL)`)},
			"postgres/0002_failure.sql": {
				Data: []byte(`CREATE TABLE`),
			},
		}}

		require.Error(t, withOwner(t, db, e, owner).Migrate(t.Context()))

		for _, table := range []string{"probe", "schema_migration", "schema_meta"} {
			var count int
			require.NoError(t, db.QueryRowContext(t.Context(), e.Engine.Q(tableCountSQL(e)), table).Scan(&count))
			require.Zero(t, count, "%s survived the failed migration transaction", table)
		}
	})
}

func TestHostMigrationRehearsalLeavesTheDatabaseUnchanged(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.OpenEmpty(t)
		require.NoError(t, migrator(t, db, e).Rehearse(t.Context()))

		var ledgers int
		require.NoError(t, db.QueryRowContext(t.Context(), e.Engine.Q(tableCountSQL(e)), "schema_migration").
			Scan(&ledgers))
		require.Zero(t, ledgers)

		require.NoError(t, migrator(t, db, e).Migrate(t.Context()))
	})
}

// TestADatabaseAnotherBuildMadeIsRefusedBeforeAnythingIsWritten is a database
// that already holds one of this build's tables and no ledger row creating it:
// a database an earlier build's migrations made, or one that is not this
// server's. Nothing is written to it, the ledger included.
func TestADatabaseAnotherBuildMadeIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.OpenEmpty(t)

		_, err := db.ExecContext(t.Context(), `CREATE TABLE authz_tuple (object_type TEXT NOT NULL)`)
		require.NoError(t, err)

		err = migrator(t, db, e).Migrate(t.Context())
		require.ErrorIs(t, err, sqlstore.ErrUnrecordedTable)
		require.ErrorContains(t, err, "authz_tuple")

		var ledgers int
		require.NoError(t, db.QueryRowContext(t.Context(), e.Engine.Q(tableCountSQL(e)), "schema_migration").
			Scan(&ledgers))
		require.Zero(t, ledgers, "the refusal created the ledger")
	})
}

// TestAnotherStoresTablesInTheSameDatabaseAreNotRefused is a Postgres database
// the history store shares with the host. Its tables are not the host's, and a
// fresh host schema migrates beside them.
func TestAnotherStoresTablesInTheSameDatabaseAreNotRefused(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.OpenEmpty(t)

		_, err := db.ExecContext(t.Context(), `CREATE TABLE history_sample (ref TEXT NOT NULL)`)
		require.NoError(t, err)

		require.NoError(t, migrator(t, db, e).Migrate(t.Context()))
	})
}

// TestTheFloorRefusesABuildOlderThanTheDatabase is the half of the floor that
// makes a restored backup the only way back: a database a newer build raised
// the floor on is refused before anything is applied.
func TestTheFloorRefusesABuildOlderThanTheDatabase(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.OpenEmpty(t)
		require.NoError(t, migrator(t, db, e).Migrate(t.Context()))

		_, err := db.ExecContext(t.Context(), `UPDATE schema_meta SET min_schema_version = 99 WHERE id = 1`)
		require.NoError(t, err)

		err = migrator(t, db, e).Migrate(t.Context())
		require.ErrorIs(t, err, sqlstore.ErrSchemaTooNew)
		require.ErrorContains(t, err, "needs schema version 99")

		var floor int
		require.NoError(t, db.QueryRowContext(t.Context(),
			`SELECT min_schema_version FROM schema_meta WHERE id = 1`).Scan(&floor))
		require.Equal(t, 99, floor, "a refused run lowered the floor")
	})
}

// TestTheFloorOnlyRises is a database a newer build migrated and raised the
// floor on, opened by a build whose floor is lower but whose version still
// covers it. The floor stays where the newer build left it.
func TestTheFloorOnlyRises(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.OpenEmpty(t)
		require.NoError(t, migrator(t, db, e).Migrate(t.Context()))

		lower := Schema()
		lower.Compat = 1

		m, err := sqlstore.NewMigrator(db, e.Engine, lower, "")
		require.NoError(t, err)
		require.NoError(t, m.Migrate(t.Context()))

		var floor int
		require.NoError(t, db.QueryRowContext(t.Context(),
			`SELECT min_schema_version FROM schema_meta WHERE id = 1`).Scan(&floor))
		require.Equal(t, Schema().Compat, floor)
	})
}

// TestADatabaseMissingAMigrationIsOlder is the state after an upgrade and
// before the new server has started once, which the CLI reset refuses.
func TestADatabaseMissingAMigrationIsOlder(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.OpenEmpty(t)
		require.NoError(t, migrator(t, db, e).Migrate(t.Context()))

		later := withOwner(t, db, e, anOwner(`CREATE TABLE probe (a TEXT NOT NULL)`))

		err := later.RequireSchemaOfThisBuild(t.Context())
		require.ErrorIs(t, err, sqlstore.ErrSchemaMismatch)
		require.ErrorIs(t, err, sqlstore.ErrSchemaOlder)
	})
}

// TestConcurrentFirstMigrationsAgainstOneDatabaseAllSurvive is the deployment
// failure the migration lock exists for: a replica set, a unit that restarted
// while the old process was still coming up, or an operator running the binary
// twice. Every one of them comes up, and the schema is migrated exactly once.
func TestConcurrentFirstMigrationsAgainstOneDatabaseAllSurvive(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		handles := e.OpenShared(t, concurrentStarts)

		var (
			start   sync.WaitGroup
			running sync.WaitGroup
			mu      sync.Mutex
			errs    []error
		)

		start.Add(1)

		for _, db := range handles {
			m := migrator(t, db, e)

			running.Go(func() {
				start.Wait()

				if err := m.Migrate(t.Context()); err != nil {
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
				}
			})
		}

		start.Done()
		running.Wait()

		require.Empty(t, errs, "a concurrent migration refused to bring the process up")
		require.Len(t, ledgerNames(t, handles[0]), 11,
			"the ledger does not hold exactly one row per shipped migration")
		require.False(t, migrator(t, handles[0], e).MigrationLockHeld(t.Context()),
			"a finished run left the migration lock held")
	})
}

// TestTheChecksumIgnoresLineEndingsAndRefusesABareCarriageReturn is one file
// checked out with CRLF and with LF, which is one migration; and a bare CR,
// which ends a comment on Postgres and not on SQLite.
func TestTheChecksumIgnoresLineEndingsAndRefusesABareCarriageReturn(t *testing.T) {
	t.Parallel()

	lf, err := sqlstore.Checksum([]byte("-- a\nCREATE TABLE probe (a TEXT);\n"))
	require.NoError(t, err)

	crlf, err := sqlstore.Checksum([]byte("-- a\r\nCREATE TABLE probe (a TEXT);\r\n"))
	require.NoError(t, err)
	require.Equal(t, lf, crlf)

	_, err = sqlstore.Checksum([]byte("-- a\rCREATE TABLE probe (a TEXT);\n"))
	require.ErrorIs(t, err, sqlstore.ErrMigrationCarriageReturn)
}

// TestEveryBaselineCreatesTheTablesTheRefusalLooksFor holds the parser the
// refusal of a foreign database relies on to what the baselines create. A
// parser that found nothing would refuse nothing.
func TestEveryBaselineCreatesTheTablesTheRefusalLooksFor(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		dir := "sqlite"
		if e.Engine == sqlstore.EnginePostgres {
			dir = "postgres"
		}

		var parsed []string

		for _, owner := range Schema().Owners {
			content, err := fs.ReadFile(owner.Files, dir+"/0001_schema.sql")
			require.NoError(t, err)

			tables := sqlstore.CreatedTables(content)
			require.NotEmptyf(t, tables, "%s creates no table the parser reads", owner.Owner)

			parsed = append(parsed, tables...)

			// A table a later file adds is looked for the same way.
			later, err := fs.Glob(owner.Files, dir+"/*.sql")
			require.NoError(t, err)

			for _, name := range later {
				if name == dir+"/0001_schema.sql" {
					continue
				}

				content, err = fs.ReadFile(owner.Files, name)
				require.NoError(t, err)

				parsed = append(parsed, sqlstore.CreatedTables(content)...)
			}
		}

		for _, table := range parsed {
			var count int
			require.NoError(t, db.QueryRowContext(t.Context(), e.Engine.Q(tableCountSQL(e)), table).Scan(&count))
			require.Equalf(t, 1, count, "the parser read %s, which the baseline does not create", table)
		}

		var created []string

		for table := range introspect(t, db, e.Engine).Tables {
			if table != "schema_migration" && table != "schema_meta" {
				created = append(created, table)
			}
		}

		require.ElementsMatch(t, created, parsed, "a table the baselines create is not parsed")
	})
}

func tableCountSQL(e engine) string {
	if e.Engine == sqlstore.EnginePostgres {
		return `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = ?`
	}

	return `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`
}
