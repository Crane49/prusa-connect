package stock

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ReportedSpool is a spool a printer read off its NFC tag.
type ReportedSpool struct {
	// UUID is the uuid of the tag.
	UUID, MaterialUUID, Abbreviation, Type, ColorRGBA string
	// PackageUUID is the catalogue package the tag names, empty for none.
	PackageUUID string
	FullWeightG *float64
	// ConsumedG is what the tool used of the spool, nil when not reported.
	ConsumedG *float64
}

// AddReported adds the spool a printer reported, with source printer and
// the uuid of its tag, and answers it. A tag a spool already carries answers
// that spool, and brings it back to stock when it was deleted or archived.
// Only a deleted one counts against limit, as restoring is never refused. A
// material the catalogue holds gives its fields. Else the spool is labelled
// with what the printer reports, and the list shows it as a copy. ErrFull
// past limit, and ErrInvalid for a tag that names a spool added by hand.
func (s *Store) AddReported(ctx context.Context, principal string, r ReportedSpool, limit int) (Spool, error) {
	if r.UUID == "" || (r.FullWeightG != nil && badWeight(*r.FullWeightG)) {
		return Spool{}, ErrInvalid
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Spool{}, fmt.Errorf("begin: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	var (
		id     int64
		gone   bool
		source string
	)

	err = tx.QueryRowContext(ctx, `SELECT id, deleted_at IS NOT NULL, source FROM spools WHERE uuid = $1`, r.UUID).
		Scan(&id, &gone, &source)

	switch {
	case err == nil && source != SourcePrinter:
		// A manual spool's uuid is the module's own: no tag carries it.
		return Spool{}, ErrInvalid
	case err == nil && !gone:
		var restored bool
		if restored, err = s.restore(ctx, tx, id); err == nil && !restored {
			return s.spool(ctx, tx, id)
		}
	case err == nil:
		err = countLive(ctx, tx, 1, limit)
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE spools SET deleted_at = NULL, archived_at = NULL,
				archive_reason = NULL, archive_reason_note = NULL, updated_at = $2 WHERE id = $1`,
				id, s.at(time.Now().UTC()))
		}
	case errors.Is(err, sql.ErrNoRows):
		id, err = s.insertReported(ctx, tx, principal, r, limit)
	}

	if unique(err) {
		// Another add of the same tag got there first.
		_ = tx.Rollback()

		if err = s.db.QueryRowContext(ctx, `SELECT id FROM spools WHERE uuid = $1`, r.UUID).Scan(&id); err != nil {
			return Spool{}, fmt.Errorf("find the reported spool: %w", err)
		}

		return s.spool(ctx, s.db, id)
	}

	if err != nil {
		return Spool{}, fmt.Errorf("add the reported spool: %w", err)
	}

	sp, err := s.spool(ctx, tx, id)
	if err != nil {
		return Spool{}, err
	}

	if err = tx.Commit(); err != nil {
		return Spool{}, fmt.Errorf("commit: %w", err)
	}

	s.spoolChanged(false, sp.ID)

	return sp, nil
}

// reportedPackage gives a reported spool the catalogue package its tag
// names: the package, its diameter, and its nominal weight when the tag
// carries no full weight. A package the
// catalogue does not hold for the material is left out.
func reportedPackage(ctx context.Context, tx *sql.Tx, r ReportedSpool, sp *Spool) error {
	var (
		net        *float64
		diameterUM *int
	)

	err := tx.QueryRowContext(ctx, `SELECT p.net_weight_g, p.diameter_um FROM packages p
		JOIN materials m ON m.slug = p.material_slug WHERE p.uuid = $1 AND m.uuid = $2`,
		r.PackageUUID, r.MaterialUUID).Scan(&net, &diameterUM)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("find the reported package: %w", err)
	}

	sp.PackageUUID, sp.DiameterUM = &r.PackageUUID, cmp.Or(diameterUM, sp.DiameterUM)
	if sp.FullWeightG == nil {
		sp.FullWeightG = net
	}

	return nil
}

// insertReported writes a new spool of what a printer reported.
func (s *Store) insertReported(ctx context.Context, tx *sql.Tx, principal string, r ReportedSpool, limit int,
) (int64, error) {
	now := time.Now().UTC()
	sp := Spool{
		UUID: r.UUID, Source: SourcePrinter, MaterialUUID: r.MaterialUUID, MaterialSource: "catalogue",
		FullWeightG: r.FullWeightG, Condition: "new", CreatedBy: principal, CreatedAt: now, UpdatedAt: now,
	}

	err := copyMaterial(ctx, tx, principal, SpoolDraft{MaterialUUID: r.MaterialUUID}, &sp)
	if err == nil && r.PackageUUID != "" {
		err = reportedPackage(ctx, tx, r, &sp)
	}

	if sp.FullWeightG != nil {
		sp.RemainingWeightG = *sp.FullWeightG
		if r.ConsumedG != nil {
			sp.RemainingWeightG = max(0, *sp.FullWeightG-max(0, *r.ConsumedG))
		}
	}

	if errors.Is(err, ErrNotFound) {
		sp.MaterialSource, sp.Class, sp.Type = "catalogue", "FFF", cmp.Or(r.Type, r.Abbreviation)
		sp.MaterialName = cmp.Or(r.Abbreviation, r.Type)
		// A spool groups under its material, so one without gets a row of its own.
		sp.MaterialUUID = cmp.Or(r.MaterialUUID, r.UUID)
		err = nil
	}

	if err != nil {
		return 0, err
	}

	sp.ColorRGBA = cmp.Or(r.ColorRGBA, sp.ColorRGBA)

	if err = countLive(ctx, tx, 1, limit); err != nil {
		return 0, err
	}

	if err = s.insertSpool(ctx, tx, &sp); err != nil || r.ConsumedG == nil {
		return sp.ID, err
	}

	// What the tag says was used is where the spool's count starts, so it is
	// not charged again when the printer next reports the tag.
	if _, err = tx.ExecContext(ctx, `UPDATE spools SET consumed_weight_g = $1 WHERE id = $2`,
		max(0, *r.ConsumedG), sp.ID); err != nil {
		return 0, fmt.Errorf("keep the tag's consumed weight: %w", err)
	}

	return sp.ID, nil
}
