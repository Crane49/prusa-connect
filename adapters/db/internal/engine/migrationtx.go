package engine

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

type migrationTx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (l Ledger) begin(ctx context.Context, db *sql.DB) (migrationTx, func(bool) error, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: open a migration connection: %w", l.Store, err)
	}

	closeConn := func() {
		_ = conn.Close() // the transaction result is the error worth returning
	}

	if !l.Postgres {
		if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
			closeConn()

			return nil, nil, fmt.Errorf("%s: begin the migration: %w", l.Store, err)
		}

		return conn, func(commit bool) error {
			defer closeConn()

			statement := "ROLLBACK"
			if commit {
				statement = "COMMIT"
			}

			if _, finishErr := conn.ExecContext(context.WithoutCancel(ctx), statement); finishErr != nil {
				return fmt.Errorf("%s: %s the migration: %w", l.Store, strings.ToLower(statement), finishErr)
			}

			return nil
		}, nil
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		closeConn()

		return nil, nil, fmt.Errorf("%s: begin the migration: %w", l.Store, err)
	}

	key := migrationLockKey(l.Table)
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", key); err != nil {
		_ = tx.Rollback()
		closeConn()

		return nil, nil, fmt.Errorf("%s: take the migration lock: %w", l.Store, err)
	}

	return tx, func(commit bool) error {
		defer closeConn()

		if commit {
			return tx.Commit() //nolint:wrapcheck // Migrate adds the store and operation.
		}

		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			return fmt.Errorf("%s: roll the migration back: %w", l.Store, rollbackErr)
		}

		return nil
	}, nil
}

func migrationLockKey(table string) int64 {
	sum := sha256.Sum256([]byte("prusa-connect migration lock: " + table))

	return int64(binary.BigEndian.Uint64(sum[:8]) & (^uint64(0) >> 1))
}
