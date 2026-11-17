package histstore

import (
	"context"
	"fmt"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

// The statements the sweep runs. The adapter expires its own rows. The host
// deletes nothing.
const (
	deleteSamplesSQL = `DELETE FROM history_sample WHERE at_us < ?`
	deleteFactsSQL   = `DELETE FROM history_fact WHERE at_us < ?`
	deleteGapsSQL    = `DELETE FROM history_gap WHERE at_us < ?`
)

// Retention is what this store keeps: raw rows for the configured time. An
// empty list means it keeps everything.
func (s *Store) Retention() []historyapi.Resolution {
	if s.keepRaw <= 0 {
		return nil
	}

	return []historyapi.Resolution{{Step: 0, Keeps: s.keepRaw}}
}

// horizon is the earliest instant a row can still have, or the zero time when
// nothing expires.
func (s *Store) horizon(now time.Time) time.Time {
	if s.keepRaw <= 0 {
		return time.Time{}
	}

	return now.Add(-s.keepRaw)
}

// Prune deletes samples, facts and gaps older than the retention. It does
// nothing when no retention is set. The three deletes are not one transaction,
// so the write lock is not kept across all of them.
func (s *Store) Prune(ctx context.Context) error {
	horizon := s.horizon(time.Now().UTC())
	if horizon.IsZero() {
		return nil
	}

	before := horizon.UnixMicro()

	for _, statement := range []string{deleteSamplesSQL, deleteFactsSQL, deleteGapsSQL} {
		if _, err := s.db.ExecContext(ctx, s.dialect.render(statement), before); err != nil {
			return fmt.Errorf("histstore: expire rows before %s: %w", horizon.Format(time.RFC3339), err)
		}
	}

	return nil
}

// refuseExpiredWindow refuses a query that starts before the horizon. A short
// page of raw rows would look like a printer that was switched off. A query
// with no lower bound is not refused: it starts at the horizon and the result
// is marked incomplete.
func (s *Store) refuseExpiredWindow(from time.Time) (time.Time, bool, error) {
	horizon := s.horizon(time.Now().UTC())
	if horizon.IsZero() {
		return from, false, nil
	}

	if from.IsZero() {
		return horizon, true, nil
	}

	if from.Before(horizon) {
		return time.Time{}, false, historyapi.RefuseQuery(
			fmt.Sprintf("this provider keeps raw rows for %s, so nothing before %s is stored",
				s.keepRaw, horizon.Format(time.RFC3339)),
			s.coarsestStep(), horizon)
	}

	return from, false, nil
}

// coarsestStep is the step at which the expired window can still be answered.
// It is zero, because this store keeps raw rows only.
func (s *Store) coarsestStep() time.Duration {
	for _, resolution := range s.Retention() {
		if resolution.Step > 0 {
			return resolution.Step
		}
	}

	return 0
}
