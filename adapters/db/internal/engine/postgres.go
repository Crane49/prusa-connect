//go:build postgres

package engine

import (
	"database/sql"
	"fmt"

	// The PostgreSQL driver is behind a build tag, so a SQLite deployment
	// does not link it. adapters/tags.json maps persistence.type to the tag.
	_ "github.com/lib/pq"
)

// postgresDriver is the name lib/pq registers itself under.
const postgresDriver = "postgres"

// OpenPostgres opens the store on the deployment's PostgreSQL database, which
// it may share with the host. The store has its own tables and migration
// ledger. Nothing is dialled here: database/sql connects on the first query,
// which is Migrate.
func OpenPostgres(dsn string) (*sql.DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("%w: the dsn is empty", ErrDatabasePath)
	}

	db, err := sql.Open(postgresDriver, withStatementTimeout(dsn))
	if err != nil {
		return nil, fmt.Errorf("open the store on postgres: %w", err)
	}

	bindPool(db)

	return db, nil
}
