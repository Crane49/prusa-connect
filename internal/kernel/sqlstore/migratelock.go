package sqlstore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
)

// migrationLockKey is the advisory lock every process takes before it migrates
// the host database. The value is the eight bytes of "pcl_auth" read as a
// big-endian integer, so it is recognisable in pg_locks and does not collide with
// another product that shares the database. It is the contract between two
// binaries pointed at one database and never changes.
const migrationLockKey int64 = 0x70636C5F61757468

// The two halves of the key as Postgres records them. pg_locks splits a bigint
// advisory key across classid and objid.
const (
	migrationLockClass  = migrationLockKey >> 32
	migrationLockObject = migrationLockKey & 0xFFFFFFFF
)

const (
	// pg_advisory_lock waits for the holder. A second process starting a few
	// milliseconds later should wait and not fail. The context bounds the wait.
	takeMigrationLockSQL = `SELECT pg_advisory_lock(?)`

	dropMigrationLockSQL = `SELECT pg_advisory_unlock(?)`

	// objsubid is 1 for the single-argument form of the advisory lock functions.
	// pg_locks covers the whole cluster and an advisory lock belongs to one
	// database, so the database this handle is on is part of the question.
	migrationLockHeldSQL = `SELECT EXISTS (
		SELECT 1 FROM pg_locks
		WHERE locktype = 'advisory'
		  AND database = (SELECT oid FROM pg_database WHERE datname = current_database())
		  AND classid = ? AND objid = ? AND objsubid = 1
	)`
)

// lock serialises the migration run against every other process pointed at
// this database, and returns the release the caller defers.
//
// Postgres needs it because a DDL conflict aborts and does not wait: two
// sessions creating one table leave the second holding "relation already
// exists". SQLite takes no lock. It has one writer, every write transaction
// takes the write lock as it opens, and busy_timeout makes a second process
// wait for the first.
func (m *Migrator) lock(ctx context.Context) (func() error, error) {
	if m.engine != EnginePostgres {
		return func() error { return nil }, nil
	}

	// A dedicated connection, not the pool. An advisory lock belongs to the
	// session that took it, so releasing it from whichever connection the pool
	// hands back releases nothing.
	conn, err := m.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("open the migration lock connection: %w", err)
	}

	if _, err = conn.ExecContext(ctx, m.engine.Q(takeMigrationLockSQL), migrationLockKey); err != nil {
		_ = conn.Close() // the failure being returned is the one worth reporting

		return nil, fmt.Errorf("take the migration lock: %w", err)
	}

	return func() error { return m.unlock(ctx, conn) }, nil
}

// unlock gives the lock back and returns the connection. Closing a *sql.Conn
// returns it to the pool and leaves the session open, so only the unlock ends
// the lock.
func (m *Migrator) unlock(ctx context.Context, conn *sql.Conn) error {
	// The run may have failed because its context was cancelled, and the lock
	// still has to come back.
	release := context.WithoutCancel(ctx)

	_, err := conn.ExecContext(release, m.engine.Q(dropMigrationLockSQL), migrationLockKey)
	if err != nil {
		// A connection returned to the pool still holding the lock blocks every
		// later run for the life of the process. Reporting it as bad ends the
		// session, and Postgres releases a session's advisory locks with it.
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	}

	_ = conn.Close() // a connection going back to the pool reports nothing worth acting on

	if err != nil {
		return fmt.Errorf("release the migration lock: %w", err)
	}

	return nil
}

// MigrationLockHeld reports whether any session holds the migration lock. On
// SQLite there is no lock to hold and it answers false.
func (m *Migrator) MigrationLockHeld(ctx context.Context) bool {
	if m.engine != EnginePostgres {
		return false
	}

	var held bool
	if err := m.db.QueryRowContext(
		ctx, m.engine.Q(migrationLockHeldSQL), migrationLockClass, migrationLockObject,
	).Scan(&held); err != nil {
		return false
	}

	return held
}
