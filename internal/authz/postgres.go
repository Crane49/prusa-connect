//go:build postgres

package authz

import (
	"database/sql"
	"fmt"

	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"

	// The Postgres driver. Unlike SQLite it is behind a tag, because a
	// deployment on SQLite would otherwise carry a client it never opens. This
	// one adds 1,259,520 bytes to the binary. The tag is derived from
	// persistence.type through adapters/tags.json, so a configuration naming
	// Postgres builds with it and one naming SQLite does not.
	//
	// The driver is lib/pq. It is already a direct dependency of this module,
	// and it is pure Go, so CGO_ENABLED=0 still links no cgo. It is in
	// maintenance mode.
	_ "github.com/lib/pq"
)

// postgresDriver is the name lib/pq registers itself under.
const postgresDriver = "postgres"

// PostgresLinked says this build carries the Postgres driver, so the
// first-run wizard offers the engine.
const PostgresLinked = true

// OpenPostgres opens the graph's database from a DSN. It accepts both forms
// lib/pq takes, a postgres:// URL and a key=value string, because it hands the
// value straight to the driver.
//
// Nothing is dialled here: database/sql connects lazily, so a server whose
// database is not up yet fails at the first query rather than at Open, and
// Migrate is that first query.
//
// The caller owns the handle and closes it, which is the same division
// OpenSQLite and Open document.
//
// At most one pool may be named, and naming none takes sqlstore.DefaultPool.
// Its statement timeout rides into the DSN as the connection setting of the
// same name, so every connection the pool opens carries it and a query nobody
// is waiting for any more stops costing the database.
func OpenPostgres(dsn string, pool ...sqlstore.Pool) (*sql.DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("%w: the dsn is empty", ErrDatabasePath)
	}

	bounds := sqlstore.PoolOr(pool)

	db, err := sql.Open(postgresDriver, sqlstore.WithStatementTimeout(dsn, bounds.StatementTimeout))
	if err != nil {
		return nil, fmt.Errorf("open the authorization graph on postgres: %w", err)
	}

	bounds.Apply(db)

	return db, nil
}
