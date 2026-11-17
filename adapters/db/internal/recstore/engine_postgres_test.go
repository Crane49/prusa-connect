//go:build postgres

package recstore_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/adapters/db/internal/recstore"
)

// databaseNameLen is enough entropy that two runs against one server, parallel
// ones included, do not collide.
const databaseNameLen = 12

// postgresPoolLimit is how many connections one test handle may open. The
// fixture allows a hundred for the whole parallel suite, and an unbounded
// handle answers "too many clients already", which looks exactly like the thing
// under test failing.
const postgresPoolLimit = 4

// postgresEngine is reachable when the DSN is set, and this file is compiled.
func postgresEngine() (engine, bool) {
	dsn := os.Getenv(postgresDSNEnv)
	if dsn == "" {
		return engine{}, false
	}

	return engine{
		Name:    "postgres",
		Dialect: recstore.DialectPostgres,
		Open: func(tb testing.TB) *sql.DB {
			tb.Helper()

			return openFreshDatabase(tb, dsn)
		},
	}, true
}

// openFreshDatabase creates a database of its own for this test, so cases stay
// independent and run in parallel against one server.
func openFreshDatabase(tb testing.TB, dsn string) *sql.DB {
	tb.Helper()

	name := "state_test_" + strings.ToLower(rand.Text())[:databaseNameLen]

	admin, err := sql.Open("postgres", dsn)
	require.NoError(tb, err)

	// The identifier is generated here and quoted; no caller value reaches the
	// statement, and CREATE DATABASE takes no parameter.
	_, err = admin.ExecContext(tb.Context(), "CREATE DATABASE "+pq.QuoteIdentifier(name))
	require.NoError(tb, err)

	parsed, err := url.Parse(dsn)
	require.NoErrorf(tb, err, "%s must be a postgres:// URL", postgresDSNEnv)

	parsed.Path = "/" + name

	db, err := sql.Open("postgres", parsed.String())
	require.NoError(tb, err)

	db.SetMaxOpenConns(postgresPoolLimit)

	tb.Cleanup(func() {
		require.NoError(tb, db.Close())

		// The test's context is already cancelled by cleanup time.
		_, dropErr := admin.ExecContext(context.Background(),
			"DROP DATABASE IF EXISTS "+pq.QuoteIdentifier(name)+" WITH (FORCE)")
		require.NoError(tb, dropErr)
		require.NoError(tb, admin.Close())
	})

	return db
}
