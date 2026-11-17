package stock

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	// ErrTaken is a location name another location already has, in any case.
	ErrTaken = errors.New("name taken")
	// ErrTooManyLocations is a new location past MaxLocations.
	ErrTooManyLocations = errors.New("too many locations")
)

// MaxLocations is the most locations the module keeps, so GET /locations
// answers all of them, with names of maxText bytes, inside the 1 MiB bound.
const MaxLocations = 100

// Location is one place spools are kept, shared by everyone, and how many
// live spools are kept there.
type Location struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Spools int    `json:"spools"`
}

// locationName trims a name and refuses an empty one or one past maxText.
func locationName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || !fits(name, maxText) || strings.ContainsAny(name, "\n\r\t") {
		return "", fmt.Errorf("a location is named in one line of 1 to %d bytes: %w", maxText, ErrInvalid)
	}

	return name, nil
}

const locationSelect = `SELECT l.id, l.name, COUNT(s.id) FROM locations l
	LEFT JOIN spools s ON s.location_id = l.id AND s.deleted_at IS NULL AND s.archived_at IS NULL`

// Locations lists every location by name, with its spools in stock, archived ones left out.
func (s *Store) Locations(ctx context.Context) ([]Location, error) {
	rows, err := s.db.QueryContext(ctx, locationSelect+` GROUP BY l.id, l.name ORDER BY lower(l.name), l.id`)
	if err != nil {
		return nil, fmt.Errorf("list locations: %w", err)
	}

	defer rows.Close()

	out := []Location{}

	for rows.Next() {
		var l Location
		if err = rows.Scan(&l.ID, &l.Name, &l.Spools); err != nil {
			return nil, fmt.Errorf("read location: %w", err)
		}

		out = append(out, l)
	}

	return out, rows.Err()
}

// location answers one location by id, or by name in any case when id is 0.
func (s *Store) location(ctx context.Context, id int64, name string) (Location, error) {
	var l Location

	err := s.db.QueryRowContext(ctx, locationSelect+` WHERE l.id = $1 OR ($1 = 0 AND lower(l.name) = lower($2))
		GROUP BY l.id, l.name`, id, name).Scan(&l.ID, &l.Name, &l.Spools)
	if errors.Is(err, sql.ErrNoRows) {
		return l, ErrNotFound
	}

	if err != nil {
		return l, fmt.Errorf("read location: %w", err)
	}

	return l, nil
}

// CreateLocation makes a location, or answers the one that already has the
// name in any case, so typing a known place twice picks it.
func (s *Store) CreateLocation(ctx context.Context, name string) (Location, error) {
	name, err := locationName(name)
	if err != nil {
		return Location{}, err
	}

	if l, findErr := s.location(ctx, 0, name); !errors.Is(findErr, ErrNotFound) {
		return l, findErr
	}

	// ponytail: count-then-insert is not locked, two concurrent creates may pass the limit by one.
	var kept int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM locations`).Scan(&kept); err != nil {
		return Location{}, fmt.Errorf("count locations: %w", err)
	}

	if kept >= MaxLocations {
		return Location{}, ErrTooManyLocations
	}

	now := s.at(time.Now())
	if _, err = s.db.ExecContext(ctx, `INSERT INTO locations (name, created_at, updated_at) VALUES ($1, $2, $2)`,
		name, now); err != nil {
		// Someone made the same place a moment ago: the unique index refused this one.
		if l, found := s.location(ctx, 0, name); found == nil {
			return l, nil
		}

		return Location{}, fmt.Errorf("create location: %w", err)
	}

	return s.location(ctx, 0, name)
}

// RenameLocation renames a location for every spool kept there. ErrTaken for
// a name another location has in any case.
func (s *Store) RenameLocation(ctx context.Context, id int64, name string) (Location, error) {
	name, err := locationName(name)
	if err != nil {
		return Location{}, err
	}

	res, err := s.db.ExecContext(ctx, `UPDATE locations SET name = $2, updated_at = $3 WHERE id = $1`,
		id, name, s.at(time.Now()))
	if err != nil {
		// The unique index refused it: another location has the name, or got it a moment ago.
		if unique(err) {
			if other, found := s.location(ctx, 0, name); found == nil && other.ID != id {
				return Location{}, fmt.Errorf("%q is already a location: %w", other.Name, ErrTaken)
			}
		}

		return Location{}, fmt.Errorf("rename location: %w", err)
	}

	if n, countErr := res.RowsAffected(); countErr != nil || n == 0 {
		return Location{}, errors.Join(ErrNotFound, countErr)
	}

	return s.location(ctx, id, "")
}

// DeleteLocation deletes a location, and every spool kept there, live or
// deleted, is left with no location, in one transaction.
func (s *Store) DeleteLocation(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, `UPDATE spools SET location_id = NULL WHERE location_id = $1`, id); err != nil {
		return fmt.Errorf("release location: %w", err)
	}

	res, err := tx.ExecContext(ctx, `DELETE FROM locations WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete location: %w", err)
	}

	if n, countErr := res.RowsAffected(); countErr != nil || n == 0 {
		return errors.Join(ErrNotFound, countErr)
	}

	return tx.Commit()
}
