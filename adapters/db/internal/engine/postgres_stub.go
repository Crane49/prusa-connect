//go:build !postgres

package engine

import (
	"database/sql"
	"fmt"
)

// OpenPostgres reports that this build has no PostgreSQL driver. It fails at
// start-up, not at the first query.
func OpenPostgres(dsn string) (*sql.DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("%w: the dsn is empty", ErrDatabasePath)
	}

	return nil, fmt.Errorf("%w: this adapter declares the postgres build tag in "+
		"adapters/tags.json, and a build that carries it links the driver", ErrPostgresUnlinked)
}
