package moduledb_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/prusa3d/prusa-connect/sdk/moduledb"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// engines is both spellings Migrate has: a transaction from database/sql on
// Postgres, and BEGIN IMMEDIATE on the one SQLite connection.
//
//nolint:gochecknoglobals // a closed list read by every case.
var engines = []wirerpc.DatabaseEngine{wirerpc.DatabasePostgres, wirerpc.DatabaseSQLite}

// files is one migration directory for each engine, holding the same files.
func files(names map[string]string) fstest.MapFS {
	out := fstest.MapFS{}

	for name, content := range names {
		out["migrations/sqlite/"+name] = &fstest.MapFile{Data: []byte(content)}
		out["migrations/postgres/"+name] = &fstest.MapFile{Data: []byte(content)}
	}

	return out
}

// stepRunning is a step that runs one statement, so the fake records it in
// order with the files.
func stepRunning(version int, name, statement string) moduledb.Step {
	return moduledb.Step{Version: version, Name: name, Up: func(
		ctx context.Context, tx moduledb.Tx, _ wirerpc.DatabaseEngine,
	) error {
		_, err := tx.ExecContext(ctx, statement)

		return err //nolint:wrapcheck // the fake's own error, read by the case.
	}}
}

// Go steps and SQL files run in version order, a step after the file of its
// version, and the report names each one.
func TestStepsAndFilesRunInVersionOrder(t *testing.T) {
	t.Parallel()

	for _, engine := range engines {
		t.Run(string(engine), func(t *testing.T) {
			t.Parallel()

			db, held := newFake(t)
			schema := moduledb.Schema{
				Migrations: files(map[string]string{
					"0001_notes.sql": "CREATE TABLE note",
					"0003_words.sql": "ALTER TABLE note ADD words",
				}),
				Steps: []moduledb.Step{
					stepRunning(3, "count_words", "UPDATE note SET words"),
					stepRunning(2, "trim_text", "UPDATE note SET text"),
				},
			}

			report, err := moduledb.Migrate(t.Context(), db, engine, schema)
			if err != nil {
				t.Fatalf("migrate: %v", err)
			}

			want := []string{"0001_notes.sql", "0002_trim_text.go", "0003_words.sql", "0003_count_words.go"}
			if !slices.Equal(report.Applied, want) {
				t.Errorf("applied %v, want %v", report.Applied, want)
			}

			ran := held.state().ran
			if !slices.Equal(ran, []string{
				"CREATE TABLE note", "UPDATE note SET text", "ALTER TABLE note ADD words", "UPDATE note SET words",
			}) {
				t.Errorf("the statements ran in the order %v", ran)
			}

			if report.Version != 3 {
				t.Errorf("the database holds version %d, want 3", report.Version)
			}
		})
	}
}

// A step that fails takes back everything the upgrade did: the files and the
// steps before it, and their ledger rows. The database stays at the version
// it held.
func TestAFailingStepRollsBackTheWholeUpgrade(t *testing.T) {
	t.Parallel()

	for _, engine := range engines {
		t.Run(string(engine), func(t *testing.T) {
			t.Parallel()

			db, held := newFake(t)
			first := moduledb.Schema{Migrations: files(map[string]string{"0001_notes.sql": "CREATE TABLE note"})}

			if _, err := moduledb.Migrate(t.Context(), db, engine, first); err != nil {
				t.Fatalf("the first release: %v", err)
			}

			before := held.state()

			upgrade := moduledb.Schema{
				Migrations: files(map[string]string{
					"0001_notes.sql": "CREATE TABLE note",
					"0002_words.sql": "ALTER TABLE note ADD words",
				}),
				Steps: []moduledb.Step{
					stepRunning(2, "count_words", "UPDATE note SET words"),
					stepRunning(3, "broken", "FAIL here"),
				},
			}

			report, err := moduledb.Migrate(t.Context(), db, engine, upgrade)
			if !errors.Is(err, moduledb.ErrMigrationFailed) {
				t.Fatalf("migrate = %v, want ErrMigrationFailed", err)
			}

			if report.Applied != nil {
				t.Errorf("a failed upgrade reported %v as applied", report.Applied)
			}

			after := held.state()
			if !slices.Equal(after.ran, before.ran) || len(after.ledger) != len(before.ledger) {
				t.Errorf("the failed upgrade left %v and ledger %v, want %v and %v",
					after.ran, after.ledger, before.ran, before.ledger)
			}
		})
	}
}

// Running the same release again applies nothing and changes nothing.
func TestARerunAppliesNothing(t *testing.T) {
	t.Parallel()

	for _, engine := range engines {
		t.Run(string(engine), func(t *testing.T) {
			t.Parallel()

			db, held := newFake(t)
			schema := moduledb.Schema{
				Migrations: files(map[string]string{"0001_notes.sql": "CREATE TABLE note"}),
				Steps:      []moduledb.Step{stepRunning(2, "fill", "UPDATE note")},
				Compat:     2,
			}

			if _, err := moduledb.Migrate(t.Context(), db, engine, schema); err != nil {
				t.Fatalf("migrate: %v", err)
			}

			before := held.state()

			report, err := moduledb.Migrate(t.Context(), db, engine, schema)
			if err != nil {
				t.Fatalf("migrate again: %v", err)
			}

			if len(report.Applied) != 0 || report.Version != 2 || report.Compat != 2 {
				t.Errorf("the rerun reported %+v, want nothing applied at version 2, floor 2", report)
			}

			if !slices.Equal(held.state().ran, before.ran) {
				t.Errorf("the rerun ran %v", held.state().ran)
			}
		})
	}
}

// The refusals that need no SQL: a release below the floor, a migration
// renamed after it was applied, and a statement that cannot run in a
// transaction.
func TestARefusedUpgradeChangesNothing(t *testing.T) {
	t.Parallel()

	newer := moduledb.Schema{
		Migrations: files(map[string]string{"0001_a.sql": "CREATE a", "0002_b.sql": "CREATE b"}),
		Compat:     2,
	}

	for name, tc := range map[string]struct {
		release moduledb.Schema
		want    error
	}{
		"a release below the floor": {
			release: moduledb.Schema{Migrations: files(map[string]string{"0001_a.sql": "CREATE a"})},
			want:    moduledb.ErrSchemaTooNew,
		},
		"a migration renamed after it was applied": {
			release: moduledb.Schema{Migrations: files(map[string]string{
				"0001_a.sql": "CREATE a", "0002_renamed.sql": "CREATE b",
			})},
			want: moduledb.ErrMigrationMissing,
		},
		"a migration that changed": {
			release: moduledb.Schema{Migrations: files(map[string]string{
				"0001_a.sql": "CREATE a2", "0002_b.sql": "CREATE b",
			})},
			want: moduledb.ErrMigrationChanged,
		},
		"an index made concurrently": {
			release: moduledb.Schema{Migrations: files(map[string]string{
				"0001_a.sql": "CREATE a", "0002_b.sql": "CREATE b",
				"0003_c.sql": "CREATE INDEX CONCURRENTLY c ON a (x);",
			})},
			want: moduledb.ErrOutsideTransaction,
		},
		"a commit of its own": {
			release: moduledb.Schema{Migrations: files(map[string]string{
				"0001_a.sql": "CREATE a", "0002_b.sql": "CREATE b", "0003_c.sql": "UPDATE a SET x = 1;\nCOMMIT;",
			})},
			want: moduledb.ErrOutsideTransaction,
		},
		"a file with no version": {
			release: moduledb.Schema{Migrations: files(map[string]string{"notes.sql": "CREATE a"})},
			want:    moduledb.ErrMigrationName,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			db, held := newFake(t)

			if _, err := moduledb.Migrate(t.Context(), db, wirerpc.DatabasePostgres, newer); err != nil {
				t.Fatalf("the newer release: %v", err)
			}

			before := held.state()

			_, err := moduledb.Migrate(t.Context(), db, wirerpc.DatabasePostgres, tc.release)
			if !errors.Is(err, tc.want) {
				t.Fatalf("migrate = %v, want %v", err, tc.want)
			}

			if after := held.state(); !slices.Equal(after.ran, before.ran) || len(after.ledger) != len(before.ledger) {
				t.Errorf("a refused upgrade changed the database: %v", after)
			}
		})
	}
}

// A trigger's BEGIN ... END is a statement the upgrade's transaction runs.
func TestATriggerBodyIsNotATransactionStatement(t *testing.T) {
	t.Parallel()

	db, _ := newFake(t)
	schema := moduledb.Schema{Migrations: files(map[string]string{
		"0001_a.sql": "CREATE TRIGGER t AFTER INSERT ON a\nBEGIN\n  UPDATE a SET x = 1;\nEND;",
	})}

	if _, err := moduledb.Migrate(t.Context(), db, wirerpc.DatabaseSQLite, schema); err != nil {
		t.Fatalf("a trigger was refused: %v", err)
	}
}

// A release whose highest version is below the database's, with a floor that
// allows it, opens the database and applies nothing.
func TestAnOlderReleaseTheFloorAllowsOpensTheDatabase(t *testing.T) {
	t.Parallel()

	db, _ := newFake(t)

	newer := moduledb.Schema{Migrations: files(map[string]string{"0001_a.sql": "CREATE a", "0002_b.sql": "CREATE b"})}
	if _, err := moduledb.Migrate(t.Context(), db, wirerpc.DatabaseSQLite, newer); err != nil {
		t.Fatalf("the newer release: %v", err)
	}

	report, err := moduledb.Migrate(t.Context(), db, wirerpc.DatabaseSQLite,
		moduledb.Schema{Migrations: files(map[string]string{"0001_a.sql": "CREATE a"})})
	if err != nil {
		t.Fatalf("the older release: %v", err)
	}

	if report.Version != 2 || len(report.Applied) != 0 {
		t.Errorf("the older release reported %+v, want version 2 and nothing applied", report)
	}
}

// Two migrations at once: the second waits for the first, and the work is
// done once.
func TestTwoMigrationsAtOnceApplyEachFileOnce(t *testing.T) {
	t.Parallel()

	for _, engine := range engines {
		t.Run(string(engine), func(t *testing.T) {
			t.Parallel()

			db, held := newFake(t)
			schema := moduledb.Schema{Migrations: files(map[string]string{
				"0001_a.sql": "CREATE a", "0002_b.sql": "CREATE b",
			})}

			var wait sync.WaitGroup

			errs := make([]error, 2)

			for i := range errs {
				wait.Go(func() { _, errs[i] = moduledb.Migrate(t.Context(), db, engine, schema) })
			}

			wait.Wait()

			for _, err := range errs {
				if err != nil {
					t.Fatalf("migrate: %v", err)
				}
			}

			if ran := held.state().ran; len(ran) != 2 {
				t.Errorf("the two migrations ran %v, want each file once", ran)
			}
		})
	}
}
