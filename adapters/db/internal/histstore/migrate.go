package histstore

import (
	"context"
	"embed"
	"io/fs"

	"github.com/prusa3d/prusa-connect/adapters/db/internal/engine"
)

// The migrations of the history store. They run in the adapter's own start,
// against its own ledger, and never touch the host's.
//
//go:embed migrations/sqlite/*.sql migrations/postgres/*.sql
var migrationFS embed.FS

// ledgerTable is the table this store records its applied migrations in.
const ledgerTable = "history_migration"

// ErrMigrationChanged reports a file whose bytes are not the bytes that were
// applied to this database.
var ErrMigrationChanged = engine.ErrMigrationChanged

// ErrMigrationMissing reports a ledger row naming a migration this build does
// not ship, which is a database a different build migrated.
var ErrMigrationMissing = engine.ErrMigrationMissing

// ErrUnrecordedTable reports a table of this store that the database already
// holds while its ledger does not record creating it.
var ErrUnrecordedTable = engine.ErrUnrecordedTable

// Migrate brings the history store's schema up to what this binary ships. It
// is safe to call again. It refuses a database a different build migrated or
// made.
func (s *Store) Migrate(ctx context.Context) error {
	files, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return err
	}

	return engine.Migrate(ctx, s.db, historyMigrationLedger(files, s.dialect))
}

// RehearseMigrations runs every pending migration and rolls the transaction
// back.
func (s *Store) RehearseMigrations(ctx context.Context) error {
	files, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return err
	}

	return engine.Rehearse(ctx, s.db, historyMigrationLedger(files, s.dialect))
}

func historyMigrationLedger(files fs.FS, dialect Dialect) engine.Ledger {
	return engine.Ledger{
		Store: "histstore", Table: ledgerTable, Files: files, Postgres: dialect == DialectPostgres, Compat: 1,
	}
}
