package stock

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"

	"github.com/prusa3d/materials/internal/dbtest"

	"github.com/prusa3d/prusa-connect/sdk/moduledb"
)

func TestTheSchemaAppliesOnEachEngine(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			db := dbtest.Open(t, engine.Grant(t))
			s := New(db, engine.Name)
			now := s.at(time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC))

			for _, q := range []struct {
				sql  string
				args []any
			}{
				{`INSERT INTO brands (uuid, slug, name) VALUES ($1, $2, $3)`, []any{"b1", "prusament", "Prusament"}},
				{`INSERT INTO containers (uuid, slug, brand_slug, name, empty_weight_g) VALUES ($1, $2, $3, $4, $5)`, []any{"c1", "spool", "prusament", "Spool", 230.5}},
				{`INSERT INTO materials (uuid, slug, brand_slug, name, class, type, abbreviation, color_rgba, density, min_print_temp, drying_time_min)
				  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`, []any{"m1", "pla", "prusament", "PLA", "FFF", "PLA", "PLA", "ff0000ff", 1.24, 215, 240}},
				{`INSERT INTO packages (uuid, slug, class, material_slug, container_slug, net_weight_g, diameter_um, length_mm, gtin)
				  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, []any{"p1", "pla-1kg", "FFF", "pla", "spool", 1000.0, 1750, 330000, "0123"}},
				{`INSERT INTO catalogue_meta (id, data_hash, last_modified, brands, materials, packages, containers, skipped, imported_at)
				  VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8)`, []any{"h", "lm", 1, 1, 1, 1, 0, now}},
				{`INSERT INTO custom_materials (uuid, owner, name, created_at, updated_at) VALUES ($1, $2, $3, $4, $4)`, []any{"u1", "alice", "Mine", now}},
				{`INSERT INTO favourites (principal, material_uuid, starred_at) VALUES ($1, $2, $3)`, []any{"alice", "m1", now}},
				{`INSERT INTO spools (material_uuid, material_source, remaining_weight_g, created_at, updated_at) VALUES ($1, $2, $3, $4, $4)`, []any{"m1", "catalogue", 750.5, now}},
			} {
				if _, err := db.Exec(q.sql, q.args...); err != nil {
					t.Fatalf("%s: %v", q.sql, err)
				}
			}

			var id int64
			var remaining float64
			var deleted any
			if err := db.QueryRow(`SELECT id, remaining_weight_g, deleted_at FROM spools`).
				Scan(&id, &remaining, &deleted); err != nil {
				t.Fatal(err)
			}
			if id != 1 || remaining != 750.5 || deleted != nil {
				t.Fatalf("spool = %d, %v, %v", id, remaining, deleted)
			}

			var slug string
			var density float64
			if err := db.QueryRow(`SELECT slug, density FROM materials WHERE uuid = $1`, "m1").
				Scan(&slug, &density); err != nil || slug != "pla" ||
				density != 1.24 {
				t.Fatalf("material = %q, %v, %v", slug, density, err)
			}

			var n int
			for _, table := range []string{"brands", "containers", "packages", "catalogue_meta", "custom_materials", "favourites"} {
				if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil || n != 1 {
					t.Fatalf("%s rows = %d, %v", table, n, err)
				}
			}
		})
	}
}

func TestOnlyAUniqueIndexRefusalIsUnique(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()

			if _, err := s.CreateLocation(ctx, "Box"); err != nil {
				t.Fatal(err)
			}

			_, err := s.db.ExecContext(ctx, `INSERT INTO locations (name, created_at, updated_at) VALUES ($1, $2, $2)`,
				"box", s.at(time.Now()))
			if !unique(err) {
				t.Fatalf("a second box: %v", err)
			}

			if _, err = s.db.ExecContext(ctx, `INSERT INTO no_such_table VALUES (1)`); err == nil || unique(err) {
				t.Fatalf("a missing table: %v", err)
			}
		})
	}
}

func TestMigrationGivesExistingSpoolsAUUID(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			grant := engine.Grant(t)
			all := dbtest.Schema().Migrations

			// The database as the first release left it, with three spools in it.
			first := fstest.MapFS{}

			for _, dir := range []string{"migrations/sqlite", "migrations/postgres"} {
				body, err := fs.ReadFile(all, dir+"/20261028T091554_materials.sql")
				if err != nil {
					t.Fatal(err)
				}

				first[dir+"/20261028T091554_materials.sql"] = &fstest.MapFile{Data: body}
			}

			old, _, err := moduledb.Open(context.Background(), grant, moduledb.Schema{Migrations: first})
			if err != nil {
				t.Fatal(err)
			}

			now := New(old, engine.Name).at(time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC))

			for range 3 {
				if _, err = old.Exec(
					`INSERT INTO spools (material_uuid, material_source, remaining_weight_g, created_at, updated_at)
					VALUES ('m1', 'catalogue', 1, $1, $1)`,
					now,
				); err != nil {
					t.Fatal(err)
				}
			}

			if err = old.Close(); err != nil {
				t.Fatal(err)
			}

			db := dbtest.Open(t, grant)

			rows, err := db.Query(`SELECT uuid, source FROM spools`)
			if err != nil {
				t.Fatal(err)
			}

			defer rows.Close()

			seen := map[string]bool{}

			for rows.Next() {
				var id, source string
				if err = rows.Scan(&id, &source); err != nil {
					t.Fatal(err)
				}

				parsed, perr := uuid.Parse(id)
				if perr != nil || parsed.Version() != 4 || parsed.Variant() != uuid.RFC4122 ||
					id != strings.ToLower(id) ||
					source != "manual" {
					t.Fatalf("spool = %q, %q, %v", id, source, perr)
				}

				seen[id] = true
			}

			if err = rows.Err(); err != nil {
				t.Fatal(err)
			}

			if len(seen) != 3 {
				t.Fatalf("the spools share uuids: %v", seen)
			}

			if _, err = db.Exec(
				`INSERT INTO spools (material_uuid, material_source, remaining_weight_g, created_at, updated_at, uuid)
				SELECT 'm1', 'catalogue', 1, created_at, updated_at, uuid FROM spools LIMIT 1`,
			); err == nil {
				t.Fatal("two spools may share a uuid")
			}
		})
	}
}
