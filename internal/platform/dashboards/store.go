//go:build !nodashboards

// Package dashboards keeps the dashboards in the database the host already
// opened.
//
// It is one port and no policy. Who may see a dashboard is decided above, by
// the composition root against the authorization graph, and handed down here
// as a level: the highest visibility the caller reaches. Every statement that
// reads or writes a dashboard compares that level in its WHERE clause, so a
// listing's count and a write's refusal never include a dashboard the caller
// may not see.
//
// The tables are the host's own schema. This package owns their migrations
// (Migrations), and the host applies them under its one ledger. The server
// keeps no copy of a row in memory. A request reads the row it needs, and a
// table of at most a few hundred small rows answers that at once.
package dashboards

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/internal/dashboard"
	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
)

const (
	dashboardColumns = `id, name, audience, is_default, grid_columns, density, cards, ` +
		`revision, created_at, created_by, changed_at, changed_by`

	// The listing counts the cards in the query, and the function that does
	// is the engine's own.
	listSQL = `SELECT id, name, audience, is_default, json_array_length(cards), changed_at, changed_by
		FROM dashboard WHERE audience <= ?`
	listPostgresSQL = `SELECT id, name, audience, is_default, jsonb_array_length(cards), changed_at, changed_by
		FROM dashboard WHERE audience <= ?`

	getSQL = `SELECT ` + dashboardColumns + ` FROM dashboard WHERE id = ? AND audience <= ?`

	countSQL = `SELECT COUNT(*) FROM dashboard`

	// takeIDSQL raises the sequence and answers the id it held. The update
	// is also the lock every write that has a limit to keep takes first: the
	// row stays locked to the transaction, so two writers counting at once
	// count one after the other.
	takeIDSQL = `UPDATE dashboard_sequence SET next_id = next_id + 1 WHERE slot = 1 RETURNING next_id - 1`
	// lockSQL takes the same lock without raising the sequence.
	lockSQL    = `UPDATE dashboard_sequence SET next_id = next_id WHERE slot = 1`
	insertSQL  = `INSERT INTO dashboard (` + dashboardColumns + `) VALUES (?, ?, ?, FALSE, ?, ?, ?, 1, ?, ?, ?, ?)`
	replaceSQL = `UPDATE dashboard
		SET name = ?, audience = ?, grid_columns = ?, density = ?, cards = ?,
			revision = revision + 1, changed_at = ?, changed_by = ?
		WHERE id = ? AND revision = ? AND audience <= ? AND (NOT is_default OR ? = 0)`
	revisionSQL = `SELECT revision FROM dashboard WHERE id = ? AND audience <= ?`
	deleteSQL   = `DELETE FROM dashboard WHERE id = ? AND audience <= ? RETURNING audience`

	clearDefaultSQL = `UPDATE dashboard SET is_default = FALSE, revision = revision + 1, changed_at = ?, changed_by = ?
		WHERE is_default`
	setDefaultSQL = `UPDATE dashboard SET is_default = TRUE, revision = revision + 1, changed_at = ?, changed_by = ?
		WHERE id = ? AND audience = 0`
	// heldSQL is the default as a writer that holds the lock sees it.
	heldSQL = `SELECT id FROM dashboard WHERE is_default`
)

// Store keeps dashboards over an open database handle.
//
// The zero value is not usable: New binds the handle, the engine and the
// clock.
type Store struct {
	db     *sql.DB
	engine sqlstore.Engine
	now    func() time.Time
}

// New returns the store over an open handle. The schema is the host's and is
// already migrated by the time anything constructs this. now is the clock a
// write is stamped with, and nil reads the system clock.
func New(db *sql.DB, engine sqlstore.Engine, now func() time.Time) (*Store, error) {
	if db == nil {
		return nil, errors.New("dashboards: the store needs an open database handle")
	}

	if err := engine.Check(); err != nil {
		return nil, err
	}

	if now == nil {
		now = time.Now
	}

	return &Store{db: db, engine: engine, now: now}, nil
}

// List answers every dashboard a caller of the level sees, by name and then
// by id, and the id of the default one when the caller sees it.
//
// The level is compared in the query. The order is written in Go, because the
// two engines fold the case of a non-English name differently, and a listing
// must answer the same order on both.
func (s *Store) List(ctx context.Context, level int) ([]dashboard.Row, *int64, error) {
	query := listSQL
	if s.engine == sqlstore.EnginePostgres {
		query = listPostgresSQL
	}

	rows, err := s.db.QueryContext(ctx, s.engine.Q(query), level)
	if err != nil {
		return nil, nil, fmt.Errorf("list dashboards: %w", err)
	}
	defer rows.Close()

	out := []dashboard.Row{}

	var held *int64

	for rows.Next() {
		var (
			row      dashboard.Row
			audience int
			changed  time.Time
		)

		if err = rows.Scan(&row.ID, &row.Name, &audience, &row.IsDefault, &row.Cards,
			&changed, &row.ChangedBy); err != nil {
			return nil, nil, fmt.Errorf("read a dashboard row: %w", err)
		}

		row.Visibility = dashboard.VisibilityOf(audience)
		row.Changed = changed.UTC()

		if row.IsDefault {
			id := row.ID
			held = &id
		}

		out = append(out, row)
	}

	if err = rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("list dashboards: %w", err)
	}

	// sort.Slice and not slices.SortFunc. The generic sort compiles a copy of
	// itself for this row type, and the printer build has no room in its size
	// budget for one.
	sort.Slice(out, func(i, j int) bool {
		if order := strings.Compare(strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)); order != 0 {
			return order < 0
		}

		return out[i].ID < out[j].ID
	})

	return out, held, nil
}

// Get answers one dashboard a caller of the level sees, and
// dashboard.ErrNotFound for one that is not there or that the caller may not
// see.
func (s *Store) Get(ctx context.Context, id int64, level int) (dashboard.Dashboard, error) {
	return s.read(ctx, s.db, id, level)
}

type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *Store) read(ctx context.Context, q querier, id int64, level int) (dashboard.Dashboard, error) {
	var (
		out      dashboard.Dashboard
		audience int
		density  string
		cards    []byte
		created  time.Time
		changed  time.Time
	)

	err := q.QueryRowContext(ctx, s.engine.Q(getSQL), id, level).Scan(&out.ID, &out.Name,
		&audience, &out.IsDefault, &out.Columns, &density, &cards, &out.Revision, &created, &out.CreatedBy,
		&changed, &out.ChangedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return dashboard.Dashboard{}, dashboard.ErrNotFound
	}

	if err != nil {
		return dashboard.Dashboard{}, fmt.Errorf("read dashboard %d: %w", id, err)
	}

	out.Cards = []dashboard.Card{}
	if err = json.Unmarshal(cards, &out.Cards); err != nil {
		return dashboard.Dashboard{}, fmt.Errorf("read the cards of dashboard %d: %w", id, err)
	}

	out.Visibility = dashboard.VisibilityOf(audience)
	out.Density = dashboard.Density(density)
	out.Created = created.UTC()
	out.Changed = changed.UTC()

	return out, nil
}

// Create writes a new dashboard and answers it as stored. The id comes from
// the sequence in the same transaction, so it is never given out twice.
func (s *Store) Create(ctx context.Context, board dashboard.Dashboard, by string) (dashboard.Dashboard, error) {
	cards, err := json.Marshal(board.Cards)
	if err != nil {
		return dashboard.Dashboard{}, fmt.Errorf("write the cards: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return dashboard.Dashboard{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // a committed transaction answers ErrTxDone, which is the success path.

	// The id is taken first, which locks the sequence, and the count comes
	// after. Two writers then count one after the other. Without the lock
	// both could read a count one below the limit and both insert. An id
	// taken by a write refused here goes back with the rollback.
	var id int64
	if err = tx.QueryRowContext(ctx, s.engine.Q(takeIDSQL)).Scan(&id); err != nil {
		return dashboard.Dashboard{}, fmt.Errorf("take a dashboard id: %w", err)
	}

	var count int
	if err = tx.QueryRowContext(ctx, s.engine.Q(countSQL)).Scan(&count); err != nil {
		return dashboard.Dashboard{}, fmt.Errorf("count dashboards: %w", err)
	}

	if count >= dashboard.MaxDashboards {
		return dashboard.Dashboard{}, dashboard.ErrFull
	}

	at := s.now().UTC()

	if _, err = tx.ExecContext(ctx, s.engine.Q(insertSQL), id, board.Name,
		board.Visibility.Level(), board.Columns, string(board.Density), string(cards), at, by, at, by); err != nil {
		return dashboard.Dashboard{}, fmt.Errorf("write dashboard %d: %w", id, err)
	}

	stored, err := s.read(ctx, tx, id, dashboard.VisibleToAdmins.Level())
	if err != nil {
		return dashboard.Dashboard{}, err
	}

	if err = tx.Commit(); err != nil {
		return dashboard.Dashboard{}, fmt.Errorf("commit dashboard %d: %w", id, err)
	}

	return stored, nil
}

// Replace writes a dashboard over the one stored, against the revision the
// change was made against, for a caller of the level. It answers
// dashboard.ErrNotFound for a dashboard the caller may not see, a
// *dashboard.ChangedError naming the current revision for a stale one, and a
// *dashboard.InvalidError for a default dashboard that not everyone would see
// after the write.
func (s *Store) Replace(ctx context.Context, board dashboard.Dashboard, expected int64, level int,
	by string,
) (dashboard.Dashboard, error) {
	cards, err := json.Marshal(board.Cards)
	if err != nil {
		return dashboard.Dashboard{}, fmt.Errorf("write the cards: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return dashboard.Dashboard{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // a committed transaction answers ErrTxDone, which is the success path.

	result, err := tx.ExecContext(ctx, s.engine.Q(replaceSQL), board.Name,
		board.Visibility.Level(), board.Columns, string(board.Density), string(cards), s.now().UTC(), by,
		board.ID, expected, level, board.Visibility.Level())
	if err != nil {
		return dashboard.Dashboard{}, fmt.Errorf("write dashboard %d: %w", board.ID, err)
	}

	if changed, _ := result.RowsAffected(); changed != 1 { // the drivers in this build report it.
		return dashboard.Dashboard{}, s.whyNot(ctx, tx, board.ID, expected, level)
	}

	stored, err := s.read(ctx, tx, board.ID, dashboard.VisibleToAdmins.Level())
	if err != nil {
		return dashboard.Dashboard{}, err
	}

	if err = tx.Commit(); err != nil {
		return dashboard.Dashboard{}, fmt.Errorf("commit dashboard %d: %w", board.ID, err)
	}

	return stored, nil
}

// whyNot says why a replace changed no row. The caller may not see the
// dashboard, or it was saved since. When the revision still matches, the only
// refusal left is the default dashboard: everyone must see it, and the write
// would narrow its visibility.
func (s *Store) whyNot(ctx context.Context, q querier, id, expected int64, level int) error {
	var revision int64

	err := q.QueryRowContext(ctx, s.engine.Q(revisionSQL), id, level).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return dashboard.ErrNotFound
	}

	if err != nil {
		return fmt.Errorf("read the revision of dashboard %d: %w", id, err)
	}

	if revision == expected {
		return &dashboard.InvalidError{
			Location: "body.visibility",
			Reason:   "is everyone for the default dashboard; choose another default first",
		}
	}

	return &dashboard.ChangedError{Revision: revision}
}

// Delete removes a dashboard a caller of the level sees, and answers who saw
// it, read in the same statement that removed it. A deleted default leaves
// the server with no default.
func (s *Store) Delete(ctx context.Context, id int64, level int) (dashboard.Visibility, error) {
	var audience int

	err := s.db.QueryRowContext(ctx, s.engine.Q(deleteSQL), id, level).Scan(&audience)
	if errors.Is(err, sql.ErrNoRows) {
		return "", dashboard.ErrNotFound
	}

	if err != nil {
		return "", fmt.Errorf("delete dashboard %d: %w", id, err)
	}

	return dashboard.VisibilityOf(audience), nil
}

// SetDefault makes one dashboard the default, or none for a nil id, and
// answers the dashboard that was the default before, read under the same lock.
// Only a dashboard everyone sees may be the default. It answers
// dashboard.ErrNotFound for an id with no such dashboard, and a
// *dashboard.InvalidError for one that is not visible to everyone.
//
// It takes the sequence's lock first. Two writers choosing a default at once
// then choose one after the other. Without the lock both would clear and both
// would set, and the index that allows one default would fail the second.
func (s *Store) SetDefault(ctx context.Context, id *int64, by string) (*int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // a committed transaction answers ErrTxDone, which is the success path.

	if _, err = tx.ExecContext(ctx, s.engine.Q(lockSQL)); err != nil {
		return nil, fmt.Errorf("lock the dashboards: %w", err)
	}

	var was *int64

	var held int64

	err = tx.QueryRowContext(ctx, s.engine.Q(heldSQL)).Scan(&held)

	switch {
	case err == nil:
		was = &held
	case !errors.Is(err, sql.ErrNoRows):
		return nil, fmt.Errorf("read the default dashboard: %w", err)
	}

	if id != nil && was != nil && *id == *was {
		return was, nil
	}

	at := s.now().UTC()

	if id != nil {
		var revision int64

		err = tx.QueryRowContext(ctx, s.engine.Q(revisionSQL), *id, dashboard.VisibleToAdmins.Level()).
			Scan(&revision)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, dashboard.ErrNotFound
		}

		if err != nil {
			return nil, fmt.Errorf("read dashboard %d: %w", *id, err)
		}
	}

	if _, err = tx.ExecContext(ctx, s.engine.Q(clearDefaultSQL), at, by); err != nil {
		return nil, fmt.Errorf("clear the default dashboard: %w", err)
	}

	if id != nil {
		result, execErr := tx.ExecContext(ctx, s.engine.Q(setDefaultSQL), at, by, *id)
		if execErr != nil {
			return nil, fmt.Errorf("set the default dashboard: %w", execErr)
		}

		if set, _ := result.RowsAffected(); set != 1 { // the drivers in this build report it.
			return nil, &dashboard.InvalidError{Location: "body.id", Reason: "names a dashboard everyone sees"}
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit the default dashboard: %w", err)
	}

	return was, nil
}
