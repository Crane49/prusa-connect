package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/lib/pq"

	"github.com/prusa3d/prusa-connect/sdk/moduledb"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// sdk/moduledb imports no driver, so its cases over a real database live here,
// in a module that registers both. The Postgres cases read the server of the
// dual-engine fixture that make test-engines starts.
const (
	postgresDSNEnv     = "AUTHZ_TEST_POSTGRES_DSN"
	requiredEnginesEnv = "AUTHZ_REQUIRED_ENGINES"
)

// engine opens a fresh, empty database of one engine for one case.
type engine struct {
	name   wirerpc.DatabaseEngine
	create func(tb testing.TB) wirerpc.DatabaseGrant
}

func engines(t *testing.T) []engine {
	t.Helper()

	out := []engine{{name: wirerpc.DatabaseSQLite, create: sqliteGrant}}

	dsn := os.Getenv(postgresDSNEnv)
	if dsn == "" {
		if strings.Contains(os.Getenv(requiredEnginesEnv), "postgres") {
			t.Fatalf("%s names postgres and %s is empty", requiredEnginesEnv, postgresDSNEnv)
		}

		t.Logf("postgres skipped: %s is empty", postgresDSNEnv)

		return out
	}

	return append(out, engine{name: wirerpc.DatabasePostgres, create: func(tb testing.TB) wirerpc.DatabaseGrant {
		tb.Helper()

		return postgresGrant(tb, dsn)
	}})
}

func sqliteGrant(tb testing.TB) wirerpc.DatabaseGrant {
	tb.Helper()

	return wirerpc.DatabaseGrant{
		Engine: wirerpc.DatabaseSQLite,
		DSN:    filepath.Join(tb.TempDir(), "database.sqlite"),
	}
}

// postgresGrant creates a database of the case's own and drops it afterwards.
func postgresGrant(tb testing.TB, dsn string) wirerpc.DatabaseGrant {
	tb.Helper()

	server, err := url.Parse(dsn)
	if err != nil || server.Scheme == "" {
		tb.Fatalf("%s must be a postgres:// URL", postgresDSNEnv)
	}

	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		tb.Fatalf("reach the postgres server: %v", err)
	}

	name := "moduledbtest_" + strings.ToLower(rand.Text())[:12]

	// The name is made here and quoted; CREATE DATABASE takes no parameter.
	if _, err = admin.ExecContext(tb.Context(), "CREATE DATABASE "+pq.QuoteIdentifier(name)); err != nil {
		tb.Fatalf("create database %s: %v", name, err)
	}

	tb.Cleanup(func() {
		// The case's own context is cancelled by the time cleanup runs.
		_, _ = admin.ExecContext(context.Background(),
			"DROP DATABASE IF EXISTS "+pq.QuoteIdentifier(name)+" WITH (FORCE)")
		_ = admin.Close()
	})

	server.Path = "/" + name

	return wirerpc.DatabaseGrant{Engine: wirerpc.DatabasePostgres, DSN: server.String()}
}

func openOrFail(t *testing.T, grant wirerpc.DatabaseGrant, schema moduledb.Schema) *sql.DB {
	t.Helper()

	db, _, err := moduledb.Open(t.Context(), grant, schema)
	if err != nil {
		t.Fatalf("open %s: %v", grant, err)
	}

	t.Cleanup(func() { _ = db.Close() })

	return db
}

func TestTheModulesMigrationsApplyOnceOnEveryEngine(t *testing.T) {
	t.Parallel()

	for _, one := range engines(t) {
		t.Run(string(one.name), func(t *testing.T) {
			t.Parallel()

			grant := one.create(t)
			db := openOrFail(t, grant, schema)

			kept, err := addNote(t.Context(), db, "the first note")
			if err != nil {
				t.Fatalf("add a note: %v", err)
			}

			// A second open is a restart: nothing is applied again, and the
			// row is still there.
			again := openOrFail(t, grant, schema)

			held, err := listNotes(t.Context(), again)
			if err != nil {
				t.Fatalf("list the notes: %v", err)
			}

			if len(held) != 1 || held[0] != kept {
				t.Errorf("notes = %+v, want [%+v]", held, kept)
			}

			var applied int
			if err = again.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM module_migration`).
				Scan(&applied); err != nil {
				t.Fatalf("read the ledger: %v", err)
			}

			// Three files and the step of version 3.
			if applied != 4 {
				t.Errorf("the ledger records %d migrations, want 4", applied)
			}
		})
	}
}

func TestADatabaseThisReleaseCannotReadIsRefused(t *testing.T) {
	t.Parallel()

	for _, one := range engines(t) {
		t.Run(string(one.name), func(t *testing.T) {
			t.Parallel()

			for name, tc := range map[string]struct {
				// change is what a newer or an edited release did to the database.
				change string
				want   error
			}{
				"a floor above this release": {
					change: `UPDATE module_schema SET min_schema_version = 9 WHERE id = 1`,
					want:   moduledb.ErrSchemaTooNew,
				},
				"a migration this release does not ship": {
					change: `INSERT INTO module_migration (name, version, checksum, applied_at)
						VALUES ('0002_renamed.sql', 2, 'x', CURRENT_TIMESTAMP)`,
					want: moduledb.ErrMigrationMissing,
				},
				"a migration that changed after it was applied": {
					change: `UPDATE module_migration SET checksum = 'x' WHERE name = '0002_note_written.sql'`,
					want:   moduledb.ErrMigrationChanged,
				},
			} {
				t.Run(name, func(t *testing.T) {
					t.Parallel()

					grant := one.create(t)
					db := openOrFail(t, grant, schema)

					if _, err := db.ExecContext(t.Context(), tc.change); err != nil {
						t.Fatalf("change the database: %v", err)
					}

					_, _, err := moduledb.Open(t.Context(), grant, schema)
					if !errors.Is(err, tc.want) {
						t.Errorf("open = %v, want %v", err, tc.want)
					}
				})
			}
		})
	}
}

// A refused floor writes nothing: the migration a release would add is not
// applied to a database it may not touch.
func TestARefusedFloorAppliesNothing(t *testing.T) {
	t.Parallel()

	for _, one := range engines(t) {
		t.Run(string(one.name), func(t *testing.T) {
			t.Parallel()

			grant := one.create(t)
			first := moduledb.Schema{Migrations: onlyFirst(t), Compat: 1}
			db := openOrFail(t, grant, first)

			if _, err := db.ExecContext(t.Context(),
				`UPDATE module_schema SET min_schema_version = 5 WHERE id = 1`); err != nil {
				t.Fatalf("raise the floor: %v", err)
			}

			_, _, err := moduledb.Open(t.Context(), grant, schema)
			if !errors.Is(err, moduledb.ErrSchemaTooNew) {
				t.Fatalf("open = %v, want ErrSchemaTooNew", err)
			}

			var applied int
			if err = db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM module_migration`).
				Scan(&applied); err != nil {
				t.Fatalf("read the ledger: %v", err)
			}

			if applied != 1 {
				t.Errorf("the ledger records %d migrations after a refusal, want 1", applied)
			}
		})
	}
}

// The floor only rises: a release with a lower Compat leaves it where it is.
func TestTheFloorNeverFalls(t *testing.T) {
	t.Parallel()

	for _, one := range engines(t) {
		t.Run(string(one.name), func(t *testing.T) {
			t.Parallel()

			grant := one.create(t)
			_ = openOrFail(t, grant, moduledb.Schema{Migrations: migrations, Steps: steps, Compat: 2})
			db := openOrFail(t, grant, moduledb.Schema{Migrations: migrations, Steps: steps, Compat: 1})

			var floor int
			if err := db.QueryRowContext(t.Context(),
				`SELECT min_schema_version FROM module_schema WHERE id = 1`).Scan(&floor); err != nil {
				t.Fatalf("read the floor: %v", err)
			}

			if floor != 2 {
				t.Errorf("floor = %d, want 2", floor)
			}
		})
	}
}

// onlyFirst is the schema of a release that shipped the first migration only.
func onlyFirst(t *testing.T) fstest.MapFS {
	t.Helper()

	out := fstest.MapFS{}

	for _, dir := range []string{"migrations/sqlite", "migrations/postgres"} {
		content, err := migrations.ReadFile(dir + "/0001_notes.sql")
		if err != nil {
			t.Fatalf("read the first migration: %v", err)
		}

		out[dir+"/0001_notes.sql"] = &fstest.MapFile{Data: content}
	}

	return out
}

// Every pooled connection enforces foreign keys, so a module raising the pool
// gets connections the pragmas reached too.
func TestEverySQLiteConnectionEnforcesForeignKeys(t *testing.T) {
	t.Parallel()

	db := openOrFail(t, sqliteGrant(t), schema)

	const pooled = 3

	db.SetMaxOpenConns(pooled)

	conns := make([]*sql.Conn, 0, pooled)

	t.Cleanup(func() {
		for _, conn := range conns {
			_ = conn.Close()
		}
	})

	for range pooled {
		conn, err := db.Conn(t.Context())
		if err != nil {
			t.Fatalf("open a connection: %v", err)
		}

		conns = append(conns, conn)

		var enabled int
		if err = conn.QueryRowContext(t.Context(), `PRAGMA foreign_keys`).Scan(&enabled); err != nil {
			t.Fatalf("read the pragma: %v", err)
		}

		if enabled != 1 {
			t.Errorf("a pooled connection does not enforce foreign keys")
		}
	}

	// Asked on a connection already held: the pool has none left to hand out.
	var mode string
	if err := conns[0].QueryRowContext(t.Context(), `PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("read the journal mode: %v", err)
	}

	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
}

func TestAMigrationWithABareCarriageReturnIsRefused(t *testing.T) {
	t.Parallel()

	broken := moduledb.Schema{Migrations: fstest.MapFS{
		"migrations/sqlite/0001_a.sql": &fstest.MapFile{Data: []byte("-- a comment\rCREATE TABLE a (id INTEGER);\n")},
	}}

	_, _, err := moduledb.Open(t.Context(), sqliteGrant(t), broken)
	if !errors.Is(err, moduledb.ErrMigrationCarriageReturn) {
		t.Errorf("open = %v, want ErrMigrationCarriageReturn", err)
	}
}

func TestARelativeSQLitePathIsRefused(t *testing.T) {
	t.Parallel()

	_, _, err := moduledb.Open(t.Context(),
		wirerpc.DatabaseGrant{Engine: wirerpc.DatabaseSQLite, DSN: "database.sqlite"}, schema)
	if !errors.Is(err, moduledb.ErrSQLitePath) {
		t.Errorf("open = %v, want ErrSQLitePath", err)
	}
}

// Two releases starting together on one database both succeed: the advisory
// lock on Postgres, and the write lock BEGIN IMMEDIATE takes on SQLite, make
// the second wait and then find nothing to apply.
func TestTwoMigrationsAtOnceBothSucceed(t *testing.T) {
	t.Parallel()

	for _, one := range engines(t) {
		t.Run(string(one.name), func(t *testing.T) {
			t.Parallel()

			grant := one.create(t)

			const starts = 4

			var wg sync.WaitGroup

			errs := make([]error, starts)
			applied := make([]int, starts)

			for i := range starts {
				wg.Go(func() {
					db, report, err := moduledb.Open(t.Context(), grant, schema)
					if err == nil {
						err = db.Close()
					}

					errs[i], applied[i] = err, len(report.Applied)
				})
			}

			wg.Wait()

			if err := errors.Join(errs...); err != nil {
				t.Fatalf("a concurrent migration failed: %v", err)
			}

			total := 0
			for _, count := range applied {
				total += count
			}

			if total != 4 {
				t.Errorf("the starts applied %v migrations between them, want 4 in all", applied)
			}
		})
	}
}

// withMigration is the module's own files with one more file per engine, as a
// later release ships them.
func withMigration(t *testing.T, name, sqlite, postgres string) fstest.MapFS {
	t.Helper()

	out := fstest.MapFS{}

	for _, dir := range []string{"migrations/sqlite", "migrations/postgres"} {
		entries, err := migrations.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}

		for _, entry := range entries {
			content, readErr := migrations.ReadFile(dir + "/" + entry.Name())
			if readErr != nil {
				t.Fatalf("read %s: %v", entry.Name(), readErr)
			}

			out[dir+"/"+entry.Name()] = &fstest.MapFile{Data: content}
		}
	}

	out["migrations/sqlite/"+name] = &fstest.MapFile{Data: []byte(sqlite)}
	out["migrations/postgres/"+name] = &fstest.MapFile{Data: []byte(postgres)}

	return out
}

// An upgrade whose step fails after its file added a column leaves the
// database exactly as it was: no column, no ledger row, the same floor.
func TestAFailingUpgradeLeavesTheDatabaseAsItWas(t *testing.T) {
	t.Parallel()

	for _, one := range engines(t) {
		t.Run(string(one.name), func(t *testing.T) {
			t.Parallel()

			grant := one.create(t)
			db := openOrFail(t, grant, schema)

			if _, err := addNote(t.Context(), db, "a spool of PETG"); err != nil {
				t.Fatalf("add a note: %v", err)
			}

			const add = "ALTER TABLE note ADD COLUMN title TEXT NOT NULL DEFAULT '';\n"

			broken := moduledb.Schema{
				Migrations: withMigration(t, "0004_note_title.sql", add, add),
				Steps: append(slices.Clone(steps), moduledb.Step{Version: 4, Name: "fill_titles", Up: func(
					ctx context.Context, tx moduledb.Tx, _ wirerpc.DatabaseEngine,
				) error {
					if _, err := tx.ExecContext(ctx, `UPDATE note SET title = text`); err != nil {
						return err //nolint:wrapcheck // the case reads the migration's own error.
					}

					return errors.New("a step that fails after it wrote")
				}}),
				Compat: 4,
			}

			_, _, err := moduledb.Open(t.Context(), grant, broken)
			if !errors.Is(err, moduledb.ErrMigrationFailed) {
				t.Fatalf("open = %v, want ErrMigrationFailed", err)
			}

			if _, err = db.ExecContext(t.Context(), `SELECT title FROM note`); err == nil {
				t.Error("the column of the failed upgrade is still there")
			}

			var applied, floor int
			if err = db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM module_migration`).
				Scan(&applied); err != nil {
				t.Fatalf("read the ledger: %v", err)
			}

			if err = db.QueryRowContext(t.Context(),
				`SELECT min_schema_version FROM module_schema WHERE id = 1`).Scan(&floor); err != nil {
				t.Fatalf("read the floor: %v", err)
			}

			if applied != 4 || floor != 0 {
				t.Errorf("after the failed upgrade the ledger holds %d rows and the floor is %d, want 4 and 0",
					applied, floor)
			}

			// The release that keeps working still opens it.
			_ = openOrFail(t, grant, schema)
		})
	}
}

// A release that adds a column and fills it in a step counts the words of
// every note kept before it.
func TestADataStepFillsTheRowsKeptBefore(t *testing.T) {
	t.Parallel()

	for _, one := range engines(t) {
		t.Run(string(one.name), func(t *testing.T) {
			t.Parallel()

			grant := one.create(t)

			// The release before 0003 kept notes with no word count.
			older := openOrFail(t, grant, moduledb.Schema{Migrations: onlyFirstTwo(t)})
			if _, err := older.ExecContext(t.Context(),
				`INSERT INTO note (text, written_at) VALUES ('a spool of PETG', '')`); err != nil {
				t.Fatalf("keep a note: %v", err)
			}

			db, report, err := moduledb.Open(t.Context(), grant, schema)
			if err != nil {
				t.Fatalf("open: %v", err)
			}

			t.Cleanup(func() { _ = db.Close() })

			want := []string{"0003_note_words.sql", "0003_count_words.go"}
			if !slices.Equal(report.Applied, want) || report.Version != 3 {
				t.Errorf("report = %+v, want %v at version 3", report, want)
			}

			held, err := listNotes(t.Context(), db)
			if err != nil {
				t.Fatalf("list: %v", err)
			}

			if len(held) != 1 || held[0].Words != 4 {
				t.Errorf("notes = %+v, want one note of 4 words", held)
			}
		})
	}
}

// onlyFirstTwo is the schema of a release that shipped migrations 1 and 2.
func onlyFirstTwo(t *testing.T) fstest.MapFS {
	t.Helper()

	out := onlyFirst(t)

	for _, dir := range []string{"migrations/sqlite", "migrations/postgres"} {
		content, err := migrations.ReadFile(dir + "/0002_note_written.sql")
		if err != nil {
			t.Fatalf("read the second migration: %v", err)
		}

		out[dir+"/0002_note_written.sql"] = &fstest.MapFile{Data: content}
	}

	return out
}

// The SQLite file and the files SQLite keeps beside it are the module's alone.
func TestTheSQLiteFileIsTheModulesAlone(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Windows keeps no Unix mode on a file")
	}

	grant := sqliteGrant(t)
	db := openOrFail(t, grant, schema)

	if _, err := addNote(t.Context(), db, "a spool of PLA"); err != nil {
		t.Fatalf("add a note: %v", err)
	}

	for _, file := range []string{grant.DSN, grant.DSN + "-wal"} {
		info, err := os.Stat(file)
		if err != nil {
			t.Fatalf("stat %s: %v", file, err)
		}

		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Errorf("%s has mode %o, want 600", filepath.Base(file), mode)
		}
	}
}
