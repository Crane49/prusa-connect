package stock

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/prusa3d/materials/internal/colour"
	"github.com/prusa3d/materials/internal/opt"
)

// maxRows is how many rows one INSERT carries.
const maxRows = 500

// CatalogueMeta is the state of the last import.
type CatalogueMeta struct {
	DataHash     string    `json:"data_hash"`
	LastModified string    `json:"last_modified"`
	Brands       int       `json:"brands"`
	Materials    int       `json:"materials"`
	Packages     int       `json:"packages"`
	Containers   int       `json:"containers"`
	Skipped      int       `json:"skipped"`
	ImportedAt   time.Time `json:"imported_at"`
}

const metaColumns = `data_hash, last_modified, brands, materials, packages, containers, skipped, imported_at`

// Catalogue answers the state of the last import, false when none is loaded.
func (s *Store) Catalogue(ctx context.Context) (CatalogueMeta, bool, error) {
	return s.catalogue(ctx, s.db)
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *Store) catalogue(ctx context.Context, q queryer) (CatalogueMeta, bool, error) {
	var (
		m   CatalogueMeta
		at  any
		err = q.QueryRowContext(ctx, `SELECT `+metaColumns+` FROM catalogue_meta WHERE id = 1`).Scan(
			&m.DataHash, &m.LastModified, &m.Brands, &m.Materials, &m.Packages, &m.Containers, &m.Skipped, &at)
	)
	if errors.Is(err, sql.ErrNoRows) {
		return m, false, nil
	}

	if err != nil {
		return m, false, fmt.Errorf("read catalogue meta: %w", err)
	}

	m.ImportedAt, err = parseTime(at)

	return m, err == nil, err
}

// catalogueTables are the tables an import replaces. Each has a twin named
// <table>_next that the import fills first.
var catalogueTables = []string{"brands", "containers", "materials", "packages"}

// ReplaceCatalogue swaps the four catalogue tables for the snapshot. It fills
// the staging tables in batches, each its own statement, so a request gets the
// database between two of them; SQLite has a single connection. Only the swap
// is one transaction, so a failure leaves the old catalogue. A snapshot whose
// hash the stored one already has changes nothing. Spools, custom materials
// and favourites are never touched. Imports must not run concurrently; the
// importer runs one at a time.
func (s *Store) ReplaceCatalogue(ctx context.Context, snap opt.Snapshot, now time.Time) (CatalogueMeta, bool, error) {
	// An empty hash is a corrupt manifest: it never counts as unchanged.
	old, ok, err := s.catalogue(ctx, s.db)
	if err != nil {
		return old, false, err
	}

	if ok && snap.Meta.DataHash != "" && old.DataHash == snap.Meta.DataHash {
		return old, true, nil
	}

	// A run that died midway leaves rows behind.
	if err = clearStaging(ctx, s.db); err != nil {
		return CatalogueMeta{}, false, err
	}

	// The first row per uuid and per slug wins. The rest count as skipped.
	skipped := snap.Skipped

	brands := dedupe(&skipped, snap.Brands, func(b opt.Brand) (string, string, []any) {
		return b.UUID, b.Slug, []any{b.UUID, b.Slug, b.Name}
	})
	containers := dedupe(&skipped, snap.Containers, func(c opt.Container) (string, string, []any) {
		return c.UUID, c.Slug, []any{c.UUID, c.Slug, c.BrandSlug, c.Name, c.EmptyWeightG}
	})
	materials := dedupe(&skipped, snap.Materials, func(m opt.Material) (string, string, []any) {
		return m.UUID, m.Slug, []any{
			m.UUID,
			m.Slug,
			m.BrandSlug,
			m.Name,
			m.Class,
			m.Type,
			m.Abbreviation,
			m.ColorRGBA,
			m.Density,
			m.HardnessShoreD,
			m.MinPrintTemp,
			m.MaxPrintTemp,
			m.PreheatTemp,
			m.MinBedTemp,
			m.MaxBedTemp,
			m.ChamberTemp,
			m.MinChamberTemp,
			m.MaxChamberTemp,
			m.DryingTemp,
			m.DryingTimeMin,
			m.TransmissionDistance,
			colour.Family(m.ColorRGBA),
			strings.Join(m.Tags, ","),
		}
	})
	packages := dedupe(&skipped, snap.Packages, func(p opt.Package) (string, string, []any) {
		return p.UUID, p.Slug, []any{p.UUID, p.Slug, p.Class, p.MaterialSlug, nilIfEmpty(p.ContainerSlug),
			p.NetWeightG, p.DiameterUM, p.LengthMM, nilIfEmpty(p.GTIN)}
	})

	jobs := []struct {
		table, columns string
		n              int
		rows           [][]any
	}{
		{"brands", "uuid, slug, name", 3, brands},
		{"containers", "uuid, slug, brand_slug, name, empty_weight_g", 5, containers},
		{
			"materials",
			"uuid, slug, brand_slug, name, class, type, abbreviation, color_rgba, density, hardness_shore_d, " +
				"min_print_temp, max_print_temp, preheat_temp, min_bed_temp, max_bed_temp, chamber_temp, min_chamber_temp, " +
				"max_chamber_temp, drying_temp, drying_time_min, transmission_distance, color_family, tags",
			23,
			materials,
		},
		{
			"packages",
			"uuid, slug, class, material_slug, container_slug, net_weight_g, diameter_um, length_mm, gtin",
			9,
			packages,
		},
	}

	for _, job := range jobs {
		if err = s.insertRows(ctx, job.table+"_next", job.columns, job.n, job.rows); err != nil {
			return CatalogueMeta{}, false, err
		}
	}

	meta := CatalogueMeta{
		DataHash: snap.Meta.DataHash, LastModified: snap.Meta.LastModified,
		Brands: len(brands), Materials: len(materials), Packages: len(packages), Containers: len(containers),
		Skipped: skipped, ImportedAt: now.UTC(),
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CatalogueMeta{}, false, fmt.Errorf("begin: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	for _, job := range jobs {
		//nolint:gosec // table and columns are the constants above.
		if _, err = tx.ExecContext(ctx, `DELETE FROM `+job.table); err != nil {
			return CatalogueMeta{}, false, fmt.Errorf("clear %s: %w", job.table, err)
		}

		//nolint:gosec // table and columns are the constants above.
		if _, err = tx.ExecContext(ctx, `INSERT INTO `+job.table+` (`+job.columns+`) SELECT `+job.columns+
			` FROM `+job.table+`_next`); err != nil {
			return CatalogueMeta{}, false, fmt.Errorf("swap %s: %w", job.table, err)
		}
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO catalogue_meta (id, `+metaColumns+`)
		VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE SET data_hash = excluded.data_hash, last_modified = excluded.last_modified,
		brands = excluded.brands, materials = excluded.materials, packages = excluded.packages,
		containers = excluded.containers, skipped = excluded.skipped, imported_at = excluded.imported_at`,
		meta.DataHash,
		meta.LastModified,
		meta.Brands,
		meta.Materials,
		meta.Packages,
		meta.Containers,
		meta.Skipped,
		s.at(now),
	)
	if err != nil {
		return CatalogueMeta{}, false, fmt.Errorf("write catalogue meta: %w", err)
	}

	if err = clearStaging(ctx, tx); err != nil {
		return CatalogueMeta{}, false, err
	}

	if err = tx.Commit(); err != nil {
		return CatalogueMeta{}, false, fmt.Errorf("commit: %w", err)
	}

	return meta, false, nil
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func clearStaging(ctx context.Context, e execer) error {
	for _, table := range catalogueTables {
		if _, err := e.ExecContext(ctx, `DELETE FROM `+table+`_next`); err != nil {
			return fmt.Errorf("clear %s_next: %w", table, err)
		}
	}

	return nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}

	return s
}

// dedupe keeps the first of each uuid and slug and counts the others.
func dedupe[T any](skipped *int, in []T, row func(T) (uuid, slug string, vals []any)) [][]any {
	uuids, slugs := map[string]bool{}, map[string]bool{}
	out := make([][]any, 0, len(in))

	for _, v := range in {
		u, sl, vals := row(v)
		if uuids[u] || slugs[sl] {
			*skipped++

			continue
		}

		uuids[u], slugs[sl] = true, true
		out = append(out, vals)
	}

	return out
}

// insertRows writes rows with multi-row INSERTs of at most maxRows, each its
// own statement, so the connection is free between two of them.
func (s *Store) insertRows(ctx context.Context, table, columns string, n int, rows [][]any) error {
	for start := 0; start < len(rows); start += maxRows {
		chunk := rows[start:min(start+maxRows, len(rows))]

		var q strings.Builder

		args := make([]any, 0, len(chunk)*n)

		fmt.Fprintf(&q, "INSERT INTO %s (%s) VALUES ", table, columns)

		for i, r := range chunk {
			if i > 0 {
				q.WriteByte(',')
			}

			q.WriteByte('(')

			for j := range n {
				if j > 0 {
					q.WriteByte(',')
				}

				fmt.Fprintf(&q, "$%d", len(args)+1)

				args = append(args, r[j])
			}

			q.WriteByte(')')
		}

		if _, err := s.db.ExecContext(ctx, q.String(), args...); err != nil {
			return fmt.Errorf("insert %s: %w", table, err)
		}

		if s.betweenBatches != nil {
			s.betweenBatches()
		}
	}

	return nil
}
