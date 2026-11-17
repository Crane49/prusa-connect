//go:build !postgres

package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// postgresEngine reports that Postgres is not reachable: its driver lives
// behind the postgres build tag, which a configuration naming that engine
// derives through adapters/tags.json.
func postgresEngine() (engine, bool) { return engine{}, false }

// TestOpeningPostgresWithoutTheDriverIsRefused is what an operator sees who
// changed persistence.type and did not rebuild. The refusal names the tag and
// the key, and it happens at start-up rather than at the first query, while the
// server is still serving nothing.
func TestOpeningPostgresWithoutTheDriverIsRefused(t *testing.T) {
	t.Parallel()

	db, err := OpenPostgres("postgres://someone@db:5432/connect")
	require.Nil(t, db)
	require.ErrorIs(t, err, ErrPostgresUnlinked)
	require.Contains(t, err.Error(), "-tags postgres")
	require.Contains(t, err.Error(), "persistence.type")
}
