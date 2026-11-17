package sqlstore

import (
	"database/sql"
	"fmt"
)

// RefusePostgres is what a store answers when the build carries no PostgreSQL
// driver. Each caller passes its own sentinels, so the refusal names the store
// an operator configured and not this package.
//
// The refusal comes at the open and not at the first query, so a deployment
// configured for PostgreSQL without a matching build is told what to do at
// start-up, while it is still serving nothing.
//
// The message names a build tag and the configuration key the tag comes from.
// Keeping one copy of that sentence means a renamed key is corrected in one
// place.
func RefusePostgres(dsn string, unusablePath, unlinked error) (*sql.DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("%w: the dsn is empty", unusablePath)
	}

	return nil, fmt.Errorf(
		"%w: build with -tags postgres, which `make build` derives from persistence.type",
		unlinked)
}
