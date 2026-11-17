//go:build !postgres

package authz

import (
	"database/sql"

	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
)

// PostgresLinked says this build carries no Postgres driver, so the first-run
// wizard does not offer the engine.
const PostgresLinked = false

// OpenPostgres reports that this build carries no Postgres driver.
//
// The refusal is here and not at the first query. A deployment that switched
// engines without rebuilding is told what to do at start-up, while it is
// serving nothing, and does not fail on the first grant.
func OpenPostgres(dsn string, _ ...sqlstore.Pool) (*sql.DB, error) {
	return sqlstore.RefusePostgres(dsn, ErrDatabasePath, ErrPostgresUnlinked)
}
