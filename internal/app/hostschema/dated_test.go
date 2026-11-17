package hostschema

import (
	"database/sql"
	"maps"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
)

// These cases run the ledger rules of ADR-20261027-kqd over two small owners
// rather than the host's own, so each one can grow a file the way a later
// release would.

// owner is one owner whose files say the same on both engines.
func owner(name string, files map[string]string) sqlstore.Migrations {
	out := fstest.MapFS{}
	for file, content := range files {
		out["sqlite/"+file] = &fstest.MapFile{Data: []byte(content)}
		out["postgres/"+file] = &fstest.MapFile{Data: []byte(content)}
	}

	return sqlstore.Migrations{Owner: name, Files: out}
}

// with is files and one more.
func with(files map[string]string, name, content string) map[string]string {
	out := maps.Clone(files)
	out[name] = content

	return out
}

func migrate(t *testing.T, db *sql.DB, e engine, schema sqlstore.Schema) error {
	t.Helper()

	m, err := sqlstore.NewMigrator(db, e.Engine, schema, "0.0.0-test")
	require.NoError(t, err)

	return m.Migrate(t.Context())
}

//nolint:gochecknoglobals // the files the cases below share, never written.
var (
	firstFiles  = map[string]string{"0001_schema.sql": `CREATE TABLE first_note (id INTEGER PRIMARY KEY)`}
	secondFiles = map[string]string{"0001_schema.sql": `CREATE TABLE second_note (id INTEGER PRIMARY KEY)`}
)

// twoOwners is the schema of a release before dated migrations.
func twoOwners() sqlstore.Schema {
	return sqlstore.Schema{Owners: []sqlstore.Migrations{owner("first", firstFiles), owner("second", secondFiles)}}
}

// A later release adds a migration to the first of two owners. Numbering the
// files across owners moved every file of the owners after it, and every
// database the earlier release made then refused to start.
func TestAnOwnerThatGrowsKeepsEveryDatabaseStarting(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.OpenEmpty(t)
		require.NoError(t, migrate(t, db, e, twoOwners()))

		grown := sqlstore.Schema{Owners: []sqlstore.Migrations{
			owner("first", with(firstFiles, "20261027T090000_first_title.sql",
				`ALTER TABLE first_note ADD COLUMN title TEXT`)),
			owner("second", secondFiles),
		}}
		require.NoError(t, migrate(t, db, e, grown))
		require.NoError(t, migrate(t, db, e, grown))

		_, err := db.ExecContext(t.Context(), `INSERT INTO first_note (id, title) VALUES (1, 'kept')`)
		require.NoError(t, err)
	})
}

// A database a build of the upgrade branch migrated numbered its ledger rows
// in a column of their own. That column is left alone and the database starts.
func TestALedgerWithTheBranchVersionColumnStillStarts(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.OpenEmpty(t)

		_, err := db.ExecContext(t.Context(), `CREATE TABLE schema_migration (
			name TEXT NOT NULL PRIMARY KEY, checksum TEXT NOT NULL, binary_version TEXT NOT NULL DEFAULT '',
			applied_at TIMESTAMP NOT NULL, version INTEGER)`)
		require.NoError(t, err)
		require.NoError(t, migrate(t, db, e, twoOwners()))

		_, err = db.ExecContext(t.Context(),
			`UPDATE schema_migration SET version = 2 WHERE name = 'second/0001_schema.sql'`)
		require.NoError(t, err)

		grown := sqlstore.Schema{Owners: []sqlstore.Migrations{
			owner("first", with(firstFiles, "20261027T090000_first_title.sql",
				`ALTER TABLE first_note ADD COLUMN title TEXT`)),
			owner("second", secondFiles),
		}}
		require.NoError(t, migrate(t, db, e, grown))
	})
}

// Two pull requests each add a dated migration. A build of the later one
// migrated the database first, and the build that merges both applies the
// earlier ones too, of the same owner and of another.
func TestAMigrationFromAParallelPullRequestIsApplied(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.OpenEmpty(t)

		laterFirst := with(firstFiles, "20261028T120000_first_title.sql",
			`ALTER TABLE first_note ADD COLUMN title TEXT`)
		require.NoError(t, migrate(t, db, e, sqlstore.Schema{Owners: []sqlstore.Migrations{
			owner("first", laterFirst), owner("second", secondFiles),
		}}))

		merged := sqlstore.Schema{Owners: []sqlstore.Migrations{
			owner("first", with(laterFirst, "20261027T090000_first_pin.sql",
				`ALTER TABLE first_note ADD COLUMN pinned INTEGER`)),
			owner("second", with(secondFiles, "20261026T090000_second_title.sql",
				`ALTER TABLE second_note ADD COLUMN title TEXT`)),
		}}
		require.NoError(t, migrate(t, db, e, merged))

		_, err := db.ExecContext(t.Context(), `INSERT INTO first_note (id, title, pinned) VALUES (1, 'a', 1)`)
		require.NoError(t, err)
		_, err = db.ExecContext(t.Context(), `INSERT INTO second_note (id, title) VALUES (1, 'b')`)
		require.NoError(t, err)

		m, err := sqlstore.NewMigrator(db, e.Engine, merged, "")
		require.NoError(t, err)
		require.NoError(t, m.RequireSchemaOfThisBuild(t.Context()))
	})
}

// newerSchema is a later release: a dated file in the first owner and an
// owner the earlier release does not have.
func newerSchema() sqlstore.Schema {
	return sqlstore.Schema{Owners: []sqlstore.Migrations{
		owner("first", with(firstFiles, "20261027T090000_first_title.sql",
			`ALTER TABLE first_note ADD COLUMN title TEXT`)),
		owner("second", secondFiles),
		owner("third", map[string]string{
			"20261027T100000_third.sql": `CREATE TABLE third_note (id INTEGER PRIMARY KEY)`,
		}),
	}}
}

// A database a newer release migrated holds dated rows this build does not
// ship, each after every file of its owner. The build opens it, and the reset
// command, which must not migrate under a running server, calls it newer.
func TestADatabaseANewerReleaseMigratedIsTolerated(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.OpenEmpty(t)
		require.NoError(t, migrate(t, db, e, newerSchema()))
		require.NoError(t, migrate(t, db, e, twoOwners()))

		m, err := sqlstore.NewMigrator(db, e.Engine, twoOwners(), "")
		require.NoError(t, err)

		err = m.RequireSchemaOfThisBuild(t.Context())
		require.ErrorIs(t, err, sqlstore.ErrSchemaNewer)
	})
}

// A ledger row this build does not ship and that sorts before a file of its
// owner the build does ship was renamed or deleted. So was a numbered one,
// because no release adds a numbered file.
func TestARenamedOrRemovedMigrationIsRefused(t *testing.T) {
	t.Parallel()

	shipped := newerSchema()

	for name, row := range map[string]string{
		"a dated file removed":                "first/20261026T090000_first_gone.sql",
		"a numbered file":                     "second/0002_more.sql",
		"a numbered file of an unknown owner": "fourth/0001_schema.sql",
		"a row with no owner":                 "20261027T090000_loose.sql",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			forEachEngine(t, func(t *testing.T, e engine) {
				db := e.OpenEmpty(t)
				require.NoError(t, migrate(t, db, e, shipped))

				_, err := db.ExecContext(t.Context(), e.Engine.Q(
					`INSERT INTO schema_migration (name, checksum, binary_version, applied_at) VALUES (?, 'x', '', ?)`),
					row, "2026-10-27 09:00:00")
				require.NoError(t, err)

				err = migrate(t, db, e, shipped)
				require.ErrorIs(t, err, sqlstore.ErrMigrationMissing)
				require.ErrorContains(t, err, row)
			})
		})
	}
}

// A shipped file renamed is refused. Renamed to a later date, the name the
// ledger holds sorts before the file and is refused by name. Renamed to an
// earlier date, the ledger's name looks like a newer release's, and the file
// runs a second time and fails, which rolls the whole migration back.
func TestARenamedMigrationIsRefused(t *testing.T) {
	t.Parallel()

	for name, applied := range map[string]string{
		"to a later date":    "first/20261027T085959_first_title.sql",
		"to an earlier date": "first/20261027T090001_first_title.sql",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			forEachEngine(t, func(t *testing.T, e engine) {
				db := e.OpenEmpty(t)
				require.NoError(t, migrate(t, db, e, newerSchema()))

				_, err := db.ExecContext(t.Context(), e.Engine.Q(`UPDATE schema_migration SET name = ? WHERE name = ?`),
					applied, "first/20261027T090000_first_title.sql")
				require.NoError(t, err)

				var before int
				require.NoError(t, db.QueryRowContext(t.Context(),
					`SELECT COUNT(*) FROM schema_migration`).Scan(&before))

				err = migrate(t, db, e, newerSchema())
				require.Error(t, err)

				if name == "to a later date" {
					require.ErrorIs(t, err, sqlstore.ErrMigrationMissing)
				}

				var after int
				require.NoError(t, db.QueryRowContext(t.Context(),
					`SELECT COUNT(*) FROM schema_migration`).Scan(&after))
				require.Equal(t, before, after)
			})
		})
	}
}

// A floor migration refuses every build that does not ship it, raises the
// count for the builds before dated names, and stays where it is under a
// later build that names an earlier one or none.
func TestAFloorMigrationRefusesABuildWithoutIt(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.OpenEmpty(t)

		breaking := newerSchema()
		breaking.Floor = "first/20261027T090000_first_title.sql"
		require.NoError(t, migrate(t, db, e, breaking))

		var (
			count    int
			recorded string
		)

		require.NoError(t, db.QueryRowContext(t.Context(),
			`SELECT min_schema_version, min_migration FROM schema_meta WHERE id = 1`).Scan(&count, &recorded))
		require.Equal(t, 4, count, "a build before dated names reads only the count")
		require.Equal(t, breaking.Floor, recorded)

		err := migrate(t, db, e, twoOwners())
		require.ErrorIs(t, err, sqlstore.ErrSchemaTooNew)
		require.ErrorContains(t, err, breaking.Floor)

		// The breaking release itself without its floor still opens it, and
		// the floor stays.
		require.NoError(t, migrate(t, db, e, newerSchema()))
		require.NoError(t, db.QueryRowContext(t.Context(),
			`SELECT min_migration FROM schema_meta WHERE id = 1`).Scan(&recorded))
		require.Equal(t, breaking.Floor, recorded)
	})
}

// A floor table a release before the floor migration made gains the column,
// and the count it holds still refuses a build with fewer files.
func TestAFloorFromBeforeDatedNamesIsKept(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.OpenEmpty(t)

		_, err := db.ExecContext(t.Context(),
			`CREATE TABLE schema_meta (id INTEGER PRIMARY KEY, min_schema_version INTEGER NOT NULL)`)
		require.NoError(t, err)
		_, err = db.ExecContext(t.Context(), `INSERT INTO schema_meta (id, min_schema_version) VALUES (1, 3)`)
		require.NoError(t, err)

		require.ErrorIs(t, migrate(t, db, e, twoOwners()), sqlstore.ErrSchemaTooNew)
		require.NoError(t, migrate(t, db, e, newerSchema()))
	})
}

func TestAFloorThisBuildDoesNotShipIsRefused(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		schema := twoOwners()
		schema.Floor = "first/20261027T090000_first_title.sql"

		_, err := schema.Version(e.Engine)
		require.ErrorIs(t, err, sqlstore.ErrMigrationName)
	})
}

// A migration that commits on its own would keep what ran before the COMMIT
// when the rest fails or a rehearsal rolls back. It is refused before anything
// runs.
func TestAMigrationThatCommitsIsRefusedBeforeAnythingRuns(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.OpenEmpty(t)
		schema := sqlstore.Schema{Owners: []sqlstore.Migrations{
			owner("first", with(firstFiles, "20261027T090000_escape.sql",
				"CREATE TABLE escaped (id INTEGER);\nCOMMIT;\nCREATE TABLE after_commit (id INTEGER);")),
		}}

		m, err := sqlstore.NewMigrator(db, e.Engine, schema, "")
		require.NoError(t, err)
		require.ErrorIs(t, m.Rehearse(t.Context()), sqlstore.ErrOutsideTransaction)
		require.ErrorIs(t, m.Migrate(t.Context()), sqlstore.ErrOutsideTransaction)

		for _, table := range []string{"first_note", "escaped", "schema_migration"} {
			var count int
			require.NoError(t, db.QueryRowContext(t.Context(), e.Engine.Q(tableCountSQL(e)), table).Scan(&count))
			require.Zero(t, count, "%s exists after a refused migration", table)
		}
	})
}

// A file whose name is neither dated nor numbered is refused.
func TestAnUndatedMigrationIsRefused(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		schema := sqlstore.Schema{Owners: []sqlstore.Migrations{
			owner("first", with(firstFiles, "add_title.sql", `ALTER TABLE first_note ADD COLUMN title TEXT`)),
		}}

		_, err := schema.Version(e.Engine)
		require.ErrorIs(t, err, sqlstore.ErrMigrationName)
	})
}
