package stock

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/prusa3d/materials/internal/dbtest"
	"github.com/prusa3d/materials/internal/opt"
)

var zipPath = flag.String("zip", "", "real OpenPrintTag zip for TestImportRealZip")

func snapshot(tag string) opt.Snapshot {
	w := 1.5
	return opt.Snapshot{
		Meta:       opt.Meta{DataHash: "hash-" + tag, LastModified: "lm-" + tag},
		Brands:     []opt.Brand{{UUID: "b-" + tag, Slug: "b-" + tag, Name: "Brand " + tag}},
		Containers: []opt.Container{{UUID: "c-" + tag, Slug: "c-" + tag, EmptyWeightG: &w}},
		Materials: []opt.Material{
			{UUID: "m-" + tag, Slug: "m-" + tag, BrandSlug: "b-" + tag, Name: "PLA", Density: &w},
		},
		Packages: []opt.Package{
			{UUID: "p-" + tag, Slug: "p-" + tag, MaterialSlug: "m-" + tag, ContainerSlug: "c-" + tag},
		},
	}
}

func count(t *testing.T, s *Store, table string) (n int) {
	t.Helper()

	if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}

	return n
}

var t0 = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

func TestReplaceCatalogueSwapsEverythingAndKeepsSpools(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s := New(dbtest.Open(t, engine.Grant(t)), engine.Name)
			ctx := context.Background()

			if _, _, err := s.ReplaceCatalogue(ctx, snapshot("a"), t0); err != nil {
				t.Fatal(err)
			}

			if _, err := s.db.Exec(
				`INSERT INTO spools (material_uuid, material_source, remaining_weight_g, created_at, updated_at)
				VALUES ('m-a', 'catalogue', 500, $1, $1)`,
				s.at(t0),
			); err != nil {
				t.Fatal(err)
			}

			meta, unchanged, err := s.ReplaceCatalogue(ctx, snapshot("b"), t0.Add(time.Hour))
			if err != nil || unchanged {
				t.Fatalf("unchanged=%v err=%v", unchanged, err)
			}

			var uuid string
			if err = s.db.QueryRow(`SELECT uuid FROM materials`).Scan(&uuid); err != nil || uuid != "m-b" {
				t.Fatalf("materials: %q %v", uuid, err)
			}

			for _, table := range []string{"brands", "containers", "materials", "packages", "spools"} {
				if n := count(t, s, table); n != 1 {
					t.Errorf("%s: %d rows", table, n)
				}
			}

			got, ok, err := s.Catalogue(ctx)
			if err != nil || !ok || got != meta || meta.Materials != 1 || !meta.ImportedAt.Equal(t0.Add(time.Hour)) {
				t.Fatalf("meta %+v vs %+v ok=%v err=%v", got, meta, ok, err)
			}
		})
	}
}

// A run that died midway leaves staging rows; the next run starts clean, and
// a finished run leaves none.
func TestReplaceCatalogueClearsTheStagingTables(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s := New(dbtest.Open(t, engine.Grant(t)), engine.Name)

			if _, err := s.db.Exec(`INSERT INTO materials_next (uuid, slug) VALUES ('stale', 'stale')`); err != nil {
				t.Fatal(err)
			}

			if _, _, err := s.ReplaceCatalogue(context.Background(), snapshot("a"), t0); err != nil {
				t.Fatal(err)
			}

			if n := count(t, s, "materials"); n != 1 {
				t.Errorf("materials: %d rows", n)
			}

			for _, table := range []string{"brands_next", "containers_next", "materials_next", "packages_next"} {
				if n := count(t, s, table); n != 0 {
					t.Errorf("%s: %d rows", table, n)
				}
			}
		})
	}
}

func TestReplaceCatalogueWithTheSameHashChangesNothing(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s := New(dbtest.Open(t, engine.Grant(t)), engine.Name)
			ctx := context.Background()

			if _, _, err := s.ReplaceCatalogue(ctx, snapshot("a"), t0); err != nil {
				t.Fatal(err)
			}

			meta, unchanged, err := s.ReplaceCatalogue(ctx, snapshot("a"), t0.Add(time.Hour))
			if err != nil || !unchanged || !meta.ImportedAt.Equal(t0) {
				t.Fatalf("unchanged=%v meta=%+v err=%v", unchanged, meta, err)
			}
		})
	}
}

func TestReplaceCatalogueNeverTrustsAnEmptyHash(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s := New(dbtest.Open(t, engine.Grant(t)), engine.Name)
			snap := snapshot("a")
			snap.Meta.DataHash = ""

			for range 2 {
				if _, unchanged, err := s.ReplaceCatalogue(context.Background(), snap, t0); err != nil || unchanged {
					t.Fatalf("unchanged=%v err=%v", unchanged, err)
				}
			}
		})
	}
}

func TestReplaceCatalogueKeepsTheFirstDuplicateAndCountsTheRest(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s := New(dbtest.Open(t, engine.Grant(t)), engine.Name)
			snap := snapshot("a")
			dupUUID := snap.Materials[0]
			dupUUID.Slug = "other"
			dupSlug := snap.Materials[0]
			dupSlug.UUID = "other"
			snap.Materials = append(snap.Materials, dupUUID, dupSlug)
			snap.Skipped = 1

			meta, _, err := s.ReplaceCatalogue(context.Background(), snap, t0)
			if err != nil || meta.Materials != 1 || meta.Skipped != 3 {
				t.Fatalf("meta=%+v err=%v", meta, err)
			}
		})
	}
}

func TestReplaceCatalogueBatchesBeyondOneStatement(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s := New(dbtest.Open(t, engine.Grant(t)), engine.Name)
			snap := snapshot("a")
			d := 1.25
			temp := 200

			// 1203 materials of 21 columns: three statements, 10500 parameters in the first.
			for i := range 1203 {
				id := fmt.Sprintf("x%d", i)
				snap.Materials = append(snap.Materials, opt.Material{
					UUID: id, Slug: id, Name: id, Density: &d, MinPrintTemp: &temp, DryingTimeMin: &temp,
				})
			}

			meta, _, err := s.ReplaceCatalogue(context.Background(), snap, t0)
			if err != nil || meta.Materials != 1204 || count(t, s, "materials") != 1204 {
				t.Fatalf("meta=%+v err=%v", meta, err)
			}

			var name string
			var minTemp int
			if err = s.db.QueryRow(`SELECT name, min_print_temp FROM materials WHERE uuid = 'x1000'`).
				Scan(&name, &minTemp); err != nil || name != "x1000" ||
				minTemp != 200 {
				t.Fatalf("row 1000: %q %d %v", name, minTemp, err)
			}
		})
	}
}

// A table that cannot be cleared fails the swap midway, and a staging table
// that is missing fails the fill. Either way catalogue A stays.
func TestReplaceCatalogueRollsBackOnFailure(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		// Packages are swapped last, after brands, containers and materials.
		for _, drop := range []string{"packages", "materials_next"} {
			t.Run(string(engine.Name)+"/"+drop, func(t *testing.T) {
				s := New(dbtest.Open(t, engine.Grant(t)), engine.Name)
				ctx := context.Background()

				if _, _, err := s.ReplaceCatalogue(ctx, snapshot("a"), t0); err != nil {
					t.Fatal(err)
				}

				if _, err := s.db.Exec(`DROP TABLE ` + drop); err != nil {
					t.Fatal(err)
				}

				if _, _, err := s.ReplaceCatalogue(ctx, snapshot("b"), t0); err == nil {
					t.Fatal("want an error")
				}

				var slug string
				if err := s.db.QueryRow(`SELECT slug FROM brands`).Scan(&slug); err != nil || slug != "b-a" {
					t.Fatalf("brands after failure: %q %v", slug, err)
				}

				if meta, _, _ := s.Catalogue(ctx); meta.DataHash != "hash-a" {
					t.Fatalf("meta moved: %+v", meta)
				}
			})
		}
	}
}

// Opt-in: go test ./internal/stock/ -run Real -v -args -zip=/path/opt.zip
func TestImportRealZip(t *testing.T) {
	if *zipPath == "" {
		t.Skip("no -zip flag")
	}

	f, err := os.Open(*zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	st, _ := f.Stat()
	snap, err := opt.Read(f, st.Size(), opt.DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}

	db, grant := dbtest.SQLite(t)
	s := New(db, grant.Engine)
	start := time.Now()
	lastBatch := start
	s.betweenBatches = func() { lastBatch = time.Now() }

	meta, _, err := s.ReplaceCatalogue(context.Background(), snap, start)
	if err != nil {
		t.Fatal(err)
	}

	// The swap is the only part that holds the database.
	t.Logf("replace took %s, the swap transaction %s: %+v", time.Since(start), time.Since(lastBatch), meta)
}

// SQLite has one connection: a read must get it while the import is between
// two batches, or every request waits out the whole import.
func TestAReadCompletesWhileAnImportIsBetweenBatches(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s := New(dbtest.Open(t, engine.Grant(t)), engine.Name)
			ctx := context.Background()

			if _, _, err := s.ReplaceCatalogue(ctx, snapshot("a"), t0); err != nil {
				t.Fatal(err)
			}

			var page Page
			read := make(chan error, 1)
			s.betweenBatches = func() {
				s.betweenBatches = nil

				readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				defer cancel()

				var err error
				page, err = s.List(readCtx, "alice", ListQuery{Limit: 10})
				read <- err
			}

			if _, _, err := s.ReplaceCatalogue(ctx, snapshot("b"), t0); err != nil {
				t.Fatal(err)
			}

			if err := <-read; err != nil {
				t.Fatalf("read during the import: %v", err)
			}

			if len(page.Rows) != 1 || page.Rows[0].UUID != "m-a" {
				t.Fatalf("mid-import read saw %+v, want catalogue a", page.Rows)
			}
		})
	}
}
