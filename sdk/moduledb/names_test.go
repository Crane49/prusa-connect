package moduledb_test

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/moduledb"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

func TestAMigrationNameIsDatedOrNumbered(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]moduledb.MigrationName{
		"0003_note_words.sql":           {Name: "0003_note_words.sql", Number: 3},
		"12_Old Style.sql":              {Name: "12_Old Style.sql", Number: 12},
		"0004_fill_titles.go":           {Name: "0004_fill_titles.go", Number: 4, Step: true},
		"20261027T093015_note_tags.sql": {Name: "20261027T093015_note_tags.sql", Stamp: "20261027T093015"},
		"20261027T093015_count_tags.go": {
			Name: "20261027T093015_count_tags.go", Stamp: "20261027T093015", Step: true,
		},
		"20261231T235959_a_b_c.sql":         {Name: "20261231T235959_a_b_c.sql", Stamp: "20261231T235959"},
		"20261027T093015_with_digits_2.sql": {Name: "20261027T093015_with_digits_2.sql", Stamp: "20261027T093015"},
	} {
		got, err := moduledb.ParseMigrationName(name)
		if err != nil || got != want {
			t.Errorf("ParseMigrationName(%q) = %+v, %v, want %+v", name, got, err, want)
		}
	}

	for _, name := range []string{
		"notes.sql",
		"0000_zero.sql",
		"20261027_no_time.sql",
		"20261027T0930_minutes_only.sql",
		"20261327T093015_month_13.sql",
		"20261027T093015_Upper.sql",
		"20261027T093015_.sql",
		"20261027T093015_two__underscores.sql",
		"20261027T093015_note.txt",
	} {
		if _, err := moduledb.ParseMigrationName(name); !errors.Is(err, moduledb.ErrMigrationName) {
			t.Errorf("ParseMigrationName(%q) = %v, want ErrMigrationName", name, err)
		}
	}
}

// Every numbered migration comes first, by number rather than by spelling,
// then every dated one by its date. At one date the file runs before the step.
func TestMigrationsApplyNumberedFirstThenByDate(t *testing.T) {
	t.Parallel()

	names := []string{
		"20261028T080000_b.sql", "10_ten.sql", "20261027T093015_count.go", "2_two.sql",
		"20261027T093015_tags.sql", "0002_two.go",
	}

	parsed := make([]moduledb.MigrationName, 0, len(names))
	for _, name := range names {
		one, err := moduledb.ParseMigrationName(name)
		if err != nil {
			t.Fatal(err)
		}

		parsed = append(parsed, one)
	}

	slices.SortFunc(parsed, moduledb.MigrationName.Compare)

	got := make([]string, 0, len(parsed))
	for _, one := range parsed {
		got = append(got, one.Name)
	}

	want := []string{
		"2_two.sql", "0002_two.go", "10_ten.sql", "20261027T093015_tags.sql", "20261027T093015_count.go",
		"20261028T080000_b.sql",
	}
	if !slices.Equal(got, want) {
		t.Errorf("the order is %v, want %v", got, want)
	}
}

// datedStep is a step at a date that runs one statement.
func datedStep(stamp, name, statement string) moduledb.Step {
	step := stepRunning(0, name, statement)
	step.Stamp = stamp

	return step
}

// A module whose numbered migrations shipped goes on with dated ones, and a
// dated step runs after the file of its date.
func TestDatedMigrationsFollowTheNumberedOnes(t *testing.T) {
	t.Parallel()

	for _, engine := range engines {
		t.Run(string(engine), func(t *testing.T) {
			t.Parallel()

			db, held := newFake(t)
			numbered := moduledb.Schema{Migrations: files(map[string]string{
				"0001_notes.sql": "CREATE TABLE note", "0002_words.sql": "ALTER TABLE note ADD words",
			})}

			if _, err := moduledb.Migrate(t.Context(), db, engine, numbered); err != nil {
				t.Fatalf("the numbered release: %v", err)
			}

			dated := moduledb.Schema{
				Migrations: files(map[string]string{
					"0001_notes.sql":                "CREATE TABLE note",
					"0002_words.sql":                "ALTER TABLE note ADD words",
					"20261027T093015_note_tags.sql": "ALTER TABLE note ADD tags",
				}),
				Steps: []moduledb.Step{datedStep("20261027T093015", "fill_tags", "UPDATE note SET tags")},
			}

			report, err := moduledb.Migrate(t.Context(), db, engine, dated)
			if err != nil {
				t.Fatalf("the dated release: %v", err)
			}

			want := []string{"20261027T093015_note_tags.sql", "20261027T093015_fill_tags.go"}
			if !slices.Equal(report.Applied, want) || report.Version != 2 {
				t.Errorf("the dated release reported %+v, want %v at version 2", report, want)
			}

			if ran := held.state().ran; !slices.Equal(ran[len(ran)-2:],
				[]string{"ALTER TABLE note ADD tags", "UPDATE note SET tags"}) {
				t.Errorf("the statements ran in the order %v", ran)
			}
		})
	}
}

// Two branches each add a dated migration. The database took the later one
// from a build of its branch, and the release that merges both applies the
// earlier one too, although it sorts before a migration the database holds.
func TestAMigrationDatedBeforeAnAppliedOneIsApplied(t *testing.T) {
	t.Parallel()

	for _, engine := range engines {
		t.Run(string(engine), func(t *testing.T) {
			t.Parallel()

			db, held := newFake(t)
			later := map[string]string{
				"0001_notes.sql":           "CREATE TABLE note",
				"20261028T120000_tags.sql": "ALTER TABLE note ADD tags",
			}

			_, err := moduledb.Migrate(t.Context(), db, engine, moduledb.Schema{Migrations: files(later)})
			if err != nil {
				t.Fatalf("the branch build: %v", err)
			}

			merged := map[string]string{"20261027T090000_pins.sql": "ALTER TABLE note ADD pinned"}
			maps.Copy(merged, later)

			report, err := moduledb.Migrate(t.Context(), db, engine, moduledb.Schema{Migrations: files(merged)})
			if err != nil {
				t.Fatalf("the merged release: %v", err)
			}

			if !slices.Equal(report.Applied, []string{"20261027T090000_pins.sql"}) {
				t.Errorf("the merged release applied %v", report.Applied)
			}

			if ran := held.state().ran; ran[len(ran)-1] != "ALTER TABLE note ADD pinned" {
				t.Errorf("the statements ran in the order %v", ran)
			}
		})
	}
}

// What a ledger row this release does not ship means depends on where it
// sorts: after every shipped migration it is a newer release's, and before
// one it is a migration renamed or deleted.
func TestALedgerRowThisReleaseDoesNotShip(t *testing.T) {
	t.Parallel()

	newer := map[string]string{
		"0001_a.sql":            "CREATE a",
		"20261027T090000_b.sql": "CREATE b",
		"20261028T090000_c.sql": "CREATE c",
	}

	for name, tc := range map[string]struct {
		release map[string]string
		want    error
	}{
		"a newer release's migration": {
			release: map[string]string{"0001_a.sql": "CREATE a", "20261027T090000_b.sql": "CREATE b"},
		},
		"a dated migration renamed": {
			release: map[string]string{
				"0001_a.sql":                  "CREATE a",
				"20261027T090000_renamed.sql": "CREATE b",
				"20261028T090000_c.sql":       "CREATE c",
			},
			want: moduledb.ErrMigrationMissing,
		},
		"a dated migration deleted": {
			release: map[string]string{"0001_a.sql": "CREATE a", "20261028T090000_c.sql": "CREATE c"},
			want:    moduledb.ErrMigrationMissing,
		},
		"a dated migration given an earlier date": {
			release: map[string]string{
				"0001_a.sql": "CREATE a", "20261027T080000_b.sql": "CREATE b", "20261028T090000_c.sql": "CREATE c",
			},
			want: moduledb.ErrMigrationMissing,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			db, held := newFake(t)
			if _, err := moduledb.Migrate(t.Context(), db, wirerpc.DatabaseSQLite,
				moduledb.Schema{Migrations: files(newer)}); err != nil {
				t.Fatalf("the newer release: %v", err)
			}

			before := held.state()

			_, err := moduledb.Migrate(t.Context(), db, wirerpc.DatabaseSQLite,
				moduledb.Schema{Migrations: files(tc.release)})

			switch {
			case tc.want == nil && err != nil:
				t.Fatalf("migrate: %v", err)
			case tc.want != nil && !errors.Is(err, tc.want):
				t.Fatalf("migrate = %v, want %v", err, tc.want)
			}

			if after := held.state(); !slices.Equal(after.ran, before.ran) {
				t.Errorf("the release ran %v", after.ran[len(before.ran):])
			}
		})
	}
}

// A floor migration refuses every release that does not ship it, whatever its
// numbered version, and only moves to a later migration.
func TestAFloorMigrationRefusesAReleaseWithoutIt(t *testing.T) {
	t.Parallel()

	first := map[string]string{"0001_a.sql": "CREATE a", "20261027T090000_b.sql": "CREATE b"}
	breaking := map[string]string{
		"0001_a.sql": "CREATE a", "20261027T090000_b.sql": "CREATE b", "20261028T090000_drop.sql": "DROP x",
	}

	db, held := newFake(t)

	report, err := moduledb.Migrate(t.Context(), db, wirerpc.DatabaseSQLite,
		moduledb.Schema{Migrations: files(breaking), Floor: "20261028T090000_drop.sql"})
	if err != nil || report.Floor != "20261028T090000_drop.sql" {
		t.Fatalf("the breaking release: %+v, %v", report, err)
	}

	_, err = moduledb.Migrate(t.Context(), db, wirerpc.DatabaseSQLite, moduledb.Schema{Migrations: files(first)})
	if !errors.Is(err, moduledb.ErrSchemaTooNew) {
		t.Fatalf("a release without the floor migration = %v, want ErrSchemaTooNew", err)
	}

	// A later release with an earlier floor leaves the recorded one.
	later := map[string]string{"20261029T090000_d.sql": "CREATE d"}
	maps.Copy(later, breaking)

	report, err = moduledb.Migrate(t.Context(), db, wirerpc.DatabaseSQLite,
		moduledb.Schema{Migrations: files(later), Floor: "20261027T090000_b.sql"})
	if err != nil || report.Floor != "20261028T090000_drop.sql" || held.state().floorName != report.Floor {
		t.Fatalf("the later release: %+v, %v, recorded %q", report, err, held.state().floorName)
	}
}

func TestAFloorThisReleaseDoesNotShipIsRefused(t *testing.T) {
	t.Parallel()

	db, _ := newFake(t)

	_, err := moduledb.Migrate(t.Context(), db, wirerpc.DatabaseSQLite, moduledb.Schema{
		Migrations: files(map[string]string{"0001_a.sql": "CREATE a"}), Floor: "20261027T090000_b.sql",
	})
	if !errors.Is(err, moduledb.ErrMigrationName) {
		t.Fatalf("migrate = %v, want ErrMigrationName", err)
	}
}

func TestAStepTakesAStampOrAVersion(t *testing.T) {
	t.Parallel()

	up := func(context.Context, moduledb.Tx, wirerpc.DatabaseEngine) error { return nil }

	for name, step := range map[string]moduledb.Step{
		"neither":    {Name: "fill", Up: up},
		"both":       {Name: "fill", Version: 1, Stamp: "20261027T090000", Up: up},
		"a bad date": {Name: "fill", Stamp: "20261027T0900", Up: up},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			db, _ := newFake(t)

			_, err := moduledb.Migrate(t.Context(), db, wirerpc.DatabaseSQLite, moduledb.Schema{
				Migrations: files(map[string]string{"0001_a.sql": "CREATE a"}), Steps: []moduledb.Step{step},
			})
			if !errors.Is(err, moduledb.ErrMigrationName) {
				t.Fatalf("migrate = %v, want ErrMigrationName", err)
			}
		})
	}
}

func TestATransactionStatementIsRefused(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]string{
		"commit":                  "UPDATE a SET x = 1;\nCOMMIT;",
		"commit at the end":       "UPDATE a SET x = 1;\nCOMMIT",
		"commit in lower case":    "update a set x = 1; commit;",
		"end":                     "UPDATE a SET x = 1;\nEND TRANSACTION;",
		"begin":                   "BEGIN;\nUPDATE a SET x = 1;",
		"begin immediate":         "BEGIN IMMEDIATE;\nUPDATE a SET x = 1;",
		"start transaction":       "START TRANSACTION;\nUPDATE a SET x = 1;",
		"rollback":                "UPDATE a SET x = 1;\nROLLBACK;",
		"abort":                   "UPDATE a SET x = 1;\nABORT;",
		"prepare transaction":     "PREPARE TRANSACTION 'x';",
		"an index made at once":   "CREATE INDEX CONCURRENTLY c ON a (x);",
		"a commit after triggers": "CREATE TRIGGER t AFTER INSERT ON a BEGIN UPDATE a SET x = 1; END;\nCOMMIT;",
		"vacuum":                  "VACUUM;",
	} {
		if err := moduledb.RequireTransactional([]byte(content)); !errors.Is(err, moduledb.ErrOutsideTransaction) {
			t.Errorf("%s: RequireTransactional = %v, want ErrOutsideTransaction", name, err)
		}
	}

	for name, content := range map[string]string{
		"a trigger body":      "CREATE TRIGGER t AFTER INSERT ON a\nBEGIN\n  UPDATE a SET x = 1;\nEND;",
		"a word in a comment": "-- COMMIT; CONCURRENTLY\n/* BEGIN; */\nUPDATE a SET x = 1;",
		"a word in a string":  "INSERT INTO a (x) VALUES ('COMMIT; it''s CONCURRENTLY');",
		"a quoted identifier": `CREATE TABLE "commit" (x INTEGER);`,
		"a function body": "CREATE FUNCTION f() RETURNS trigger AS $$\nBEGIN\n  COMMIT;\nEND;\n" +
			"$$ LANGUAGE plpgsql;",
		"a tagged function body":    "CREATE FUNCTION f() RETURNS void AS $body$ BEGIN END; $body$ LANGUAGE plpgsql;",
		"a case expression":         "UPDATE a SET x = CASE WHEN y THEN 1 ELSE 0 END;",
		"a column named for an end": "ALTER TABLE a ADD COLUMN ended_at TEXT;",
		"a begin atomic body":       "CREATE FUNCTION f() RETURNS int LANGUAGE sql BEGIN ATOMIC SELECT 1; END;",
	} {
		if err := moduledb.RequireTransactional([]byte(content)); err != nil {
			t.Errorf("%s: RequireTransactional = %v, want nil", name, err)
		}
	}
}
