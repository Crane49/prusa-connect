package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type migrationTx interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (m *Migrator) begin(ctx context.Context) (migrationTx, func(bool) error, error) {
	conn, err := m.db.Conn(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("open a migration connection: %w", err)
	}

	closeConn := func() { _ = conn.Close() }

	if m.engine == EngineSQLite {
		if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
			closeConn()

			return nil, nil, fmt.Errorf("begin the migration: %w", err)
		}

		return conn, func(commit bool) error {
			defer closeConn()

			statement := "ROLLBACK"
			if commit {
				statement = "COMMIT"
			}

			if _, finishErr := conn.ExecContext(context.WithoutCancel(ctx), statement); finishErr != nil {
				return fmt.Errorf("%s the migration: %w", strings.ToLower(statement), finishErr)
			}

			return nil
		}, nil
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		closeConn()

		return nil, nil, fmt.Errorf("begin the migration: %w", err)
	}

	return tx, func(commit bool) error {
		defer closeConn()

		if commit {
			return tx.Commit() //nolint:wrapcheck // caller adds the migration operation.
		}

		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			return fmt.Errorf("roll back the migration: %w", rollbackErr)
		}

		return nil
	}, nil
}
