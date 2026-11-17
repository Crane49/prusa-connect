package main

import (
	"errors"
	"maps"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/prusa3d/prusa-connect/sdk/moduledb"
)

// plus is files with one more file per engine.
func plus(files fstest.MapFS, name, sqlite, postgres string) fstest.MapFS {
	out := maps.Clone(files)

	out["migrations/sqlite/"+name] = &fstest.MapFile{Data: []byte(sqlite)}
	out["migrations/postgres/"+name] = &fstest.MapFile{Data: []byte(postgres)}

	return out
}

// The module's numbered files shipped, and two branches each added a dated
// one. A build of the later branch migrated the database, and the release
// that merges both applies the earlier one, on both engines. A floor table a
// release before floor migrations made gains the column.
func TestADatedMigrationFromAParallelBranchIsApplied(t *testing.T) {
	t.Parallel()

	for _, one := range engines(t) {
		t.Run(string(one.name), func(t *testing.T) {
			t.Parallel()

			grant := one.create(t)

			first := openOrFail(t, grant, moduledb.Schema{Migrations: migrations, Steps: steps})

			// The floor as a release before floor migrations left it.
			_, err := first.ExecContext(t.Context(), `ALTER TABLE module_schema DROP COLUMN min_migration`)
			if err != nil {
				t.Fatalf("take the floor back: %v", err)
			}

			later := withMigration(t, "20261028T120000_note_tags.sql",
				`ALTER TABLE note ADD COLUMN tags TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE note ADD COLUMN tags TEXT NOT NULL DEFAULT ''`)

			_ = openOrFail(t, grant, moduledb.Schema{Migrations: later, Steps: steps})

			merged := plus(later, "20261027T090000_note_pins.sql",
				`ALTER TABLE note ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0`,
				`ALTER TABLE note ADD COLUMN pinned BOOLEAN NOT NULL DEFAULT FALSE`)

			db, report, err := moduledb.Open(t.Context(), grant, moduledb.Schema{
				Migrations: merged, Steps: steps, Floor: "20261028T120000_note_tags.sql",
			})
			if err != nil {
				t.Fatalf("the merged release: %v", err)
			}

			t.Cleanup(func() { _ = db.Close() })

			if !slices.Equal(report.Applied, []string{"20261027T090000_note_pins.sql"}) || report.Version != 3 {
				t.Errorf("the merged release reported %+v", report)
			}

			if report.Floor != "20261028T120000_note_tags.sql" {
				t.Errorf("the floor is %q", report.Floor)
			}

			if _, err = db.ExecContext(t.Context(), `SELECT pinned, tags FROM note`); err != nil {
				t.Errorf("the merged columns: %v", err)
			}

			// The release before the floor migration cannot read it any more.
			_, _, err = moduledb.Open(t.Context(), grant, moduledb.Schema{Migrations: migrations, Steps: steps})
			if !errors.Is(err, moduledb.ErrSchemaTooNew) {
				t.Errorf("the release without the floor migration = %v, want ErrSchemaTooNew", err)
			}
		})
	}
}
