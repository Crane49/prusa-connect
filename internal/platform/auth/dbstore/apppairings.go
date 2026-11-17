package dbstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/prusa3d/prusa-connect/internal/platform/auth"
)

// The statements over the app pairing table, written once with '?' for both
// engines.
const (
	appPairingColumns = `device_digest, user_code, app_name, scopes, client_address, created_at, expires_at,
		interval_secs, polled_at, state, decided_by, decided_scopes, decided_generation, decided_root,
		root_generation`

	insertAppPairingSQL = `INSERT INTO auth_app_pairing (` + appPairingColumns + `)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	countAppPairingsSQL = `SELECT COUNT(*) FROM auth_app_pairing WHERE expires_at > ?`

	selectAppPairingByUserCodeSQL = `SELECT ` + appPairingColumns + ` FROM auth_app_pairing WHERE user_code = ?`

	selectAppPairingByDeviceSQL = `SELECT ` + appPairingColumns + ` FROM auth_app_pairing WHERE device_digest = ?`

	// The state and the expiry are in the predicate, so of two processes
	// deciding one pairing exactly one moves it.
	decideAppPairingSQL = `UPDATE auth_app_pairing SET state = ?, decided_by = ?, decided_scopes = ?,
		decided_generation = ?, decided_root = ?, root_generation = ?
		WHERE user_code = ? AND state = 'pending' AND expires_at > ?`

	pollAppPairingSQL = `UPDATE auth_app_pairing SET polled_at = ?, interval_secs = ? WHERE device_digest = ?`

	// The state is in the predicate, so of two polls collecting one
	// credential exactly one deletes the row.
	takeAppPairingSQL = `DELETE FROM auth_app_pairing WHERE device_digest = ? AND state = ?`

	deleteExpiredAppPairingsSQL = `DELETE FROM auth_app_pairing WHERE expires_at <= ?`
)

// AppPairings is auth.AppPairingStore over the host's database, so a pairing
// one process opened can be approved and collected through another.
type AppPairings struct {
	db     *sql.DB
	engine Engine
}

var _ auth.AppPairingStore = (*AppPairings)(nil)

// NewAppPairings returns the app pairing store over an open handle.
func NewAppPairings(db *sql.DB, engine Engine) (*AppPairings, error) {
	if db == nil {
		return nil, errors.New("dbstore: an app pairing store needs an open database handle")
	}

	if err := checkEngine(engine); err != nil {
		return nil, err
	}

	return &AppPairings{db: db, engine: engine}, nil
}

// CreateAppPairing writes a new row, refusing a user code another row holds.
//
// The check is a read before the insert, as Pairings.Save does, because the
// two engines word a constraint violation differently. The unique index still
// holds the rule when two inserts race, and the loser reports a plain error.
func (a *AppPairings) CreateAppPairing(ctx context.Context, pairing auth.AppPairing) error {
	_, err := a.AppPairingByUserCode(ctx, pairing.UserCode)

	switch {
	case err == nil:
		return auth.ErrUserCodeTaken
	case !errors.Is(err, auth.ErrAppPairingUnknown):
		return err
	}

	_, err = a.db.ExecContext(ctx, a.engine.Q(insertAppPairingSQL),
		pairing.DeviceDigest, pairing.UserCode, pairing.AppName, joinScopes(pairing.Scopes),
		pairing.ClientAddress, pairing.CreatedAt.UTC(), pairing.ExpiresAt.UTC(),
		int64(pairing.Interval/time.Second), pairing.PolledAt.UTC(), pairing.State, pairing.DecidedBy,
		joinScopes(pairing.DecidedScopes), pairing.DecidedGeneration, pairing.DecidedRoot, pairing.RootGeneration)
	if err != nil {
		return fmt.Errorf("save the app pairing: %w", err)
	}

	return nil
}

// CountAppPairings counts the rows not yet expired at now.
func (a *AppPairings) CountAppPairings(ctx context.Context, now time.Time) (int, error) {
	var count int

	err := a.db.QueryRowContext(ctx, a.engine.Q(countAppPairingsSQL), now.UTC()).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count the app pairings: %w", err)
	}

	return count, nil
}

// AppPairingByUserCode finds a row by its user code.
func (a *AppPairings) AppPairingByUserCode(ctx context.Context, userCode string) (auth.AppPairing, error) {
	return a.one(ctx, selectAppPairingByUserCodeSQL, userCode)
}

// AppPairingByDevice finds a row by the digest of the code an app polls with.
func (a *AppPairings) AppPairingByDevice(ctx context.Context, deviceDigest string) (auth.AppPairing, error) {
	return a.one(ctx, selectAppPairingByDeviceSQL, deviceDigest)
}

// DecideAppPairing moves a pending, live row to the decision's state.
func (a *AppPairings) DecideAppPairing(ctx context.Context, decision auth.AppPairing, now time.Time) (bool, error) {
	result, err := a.db.ExecContext(ctx, a.engine.Q(decideAppPairingSQL),
		decision.State, decision.DecidedBy, joinScopes(decision.DecidedScopes), decision.DecidedGeneration,
		decision.DecidedRoot, decision.RootGeneration, decision.UserCode, now.UTC())
	if err != nil {
		return false, fmt.Errorf("decide the app pairing: %w", err)
	}

	moved, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count the decided app pairings: %w", err)
	}

	return moved == 1, nil
}

// PolledAppPairing records a poll.
func (a *AppPairings) PolledAppPairing(
	ctx context.Context, deviceDigest string, polled time.Time, interval time.Duration,
) error {
	_, err := a.db.ExecContext(ctx, a.engine.Q(pollAppPairingSQL),
		polled.UTC(), int64(interval/time.Second), deviceDigest)
	if err != nil {
		return fmt.Errorf("record the app pairing poll: %w", err)
	}

	return nil
}

// TakeAppPairing deletes a row in the given state, and reports whether this
// call deleted it.
func (a *AppPairings) TakeAppPairing(ctx context.Context, deviceDigest, state string) (bool, error) {
	result, err := a.db.ExecContext(ctx, a.engine.Q(takeAppPairingSQL), deviceDigest, state)
	if err != nil {
		return false, fmt.Errorf("take the app pairing: %w", err)
	}

	taken, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count the taken app pairings: %w", err)
	}

	return taken == 1, nil
}

// DeleteExpiredAppPairings removes the rows past their expiry.
func (a *AppPairings) DeleteExpiredAppPairings(ctx context.Context, now time.Time) error {
	if _, err := a.db.ExecContext(ctx, a.engine.Q(deleteExpiredAppPairingsSQL), now.UTC()); err != nil {
		return fmt.Errorf("delete the expired app pairings: %w", err)
	}

	return nil
}

// one reads a single row, or reports auth.ErrAppPairingUnknown.
func (a *AppPairings) one(ctx context.Context, query string, key string) (auth.AppPairing, error) {
	var (
		row          auth.AppPairing
		scopes       string
		decided      string
		intervalSecs int64
		created      time.Time
		expires      time.Time
		polled       time.Time
	)

	err := a.db.QueryRowContext(ctx, a.engine.Q(query), key).Scan(
		&row.DeviceDigest, &row.UserCode, &row.AppName, &scopes, &row.ClientAddress, &created, &expires,
		&intervalSecs, &polled, &row.State, &row.DecidedBy, &decided, &row.DecidedGeneration,
		&row.DecidedRoot, &row.RootGeneration)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return auth.AppPairing{}, auth.ErrAppPairingUnknown
	case err != nil:
		return auth.AppPairing{}, fmt.Errorf("read the app pairing: %w", err)
	}

	row.Scopes = splitScopes(scopes)
	row.DecidedScopes = splitScopes(decided)
	row.Interval = time.Duration(intervalSecs) * time.Second
	row.CreatedAt = created.UTC()
	row.ExpiresAt = expires.UTC()
	row.PolledAt = polled.UTC()

	return row, nil
}
