//go:build postgres

package authz

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
)

// databaseNameLen is enough entropy that two runs against one server, including
// two parallel ones, do not collide.
const databaseNameLen = 12

// postgresPoolLimit is how many connections one test handle may open. See the
// note at the call site.
const postgresPoolLimit = 2

func postgresEngine() (engine, bool) {
	dsn := os.Getenv(PostgresDSNEnv)
	if dsn == "" {
		return engine{}, false
	}

	return engine{
		Name:    "postgres",
		Dialect: DialectPostgres,
		Open: func(tb testing.TB) *sql.DB {
			tb.Helper()

			return openFreshDatabase(tb, dsn)
		},
		Shared: func(tb testing.TB, count int) []*sql.DB {
			tb.Helper()

			return openSharedDatabase(tb, dsn, count)
		},
	}, true
}

// openFreshDatabase creates a database of its own for this test, so cases stay
// independent and can run in parallel against one server.
func openFreshDatabase(tb testing.TB, dsn string) *sql.DB {
	tb.Helper()

	return openSharedDatabase(tb, dsn, 1)[0]
}

// openSharedDatabase creates one database and returns count handles onto it.
// Separate handles rather than one pool: two processes hold two pools, and a
// pool hands out the same session to callers that a second process could never
// share.
func openSharedDatabase(tb testing.TB, dsn string, count int) []*sql.DB {
	tb.Helper()

	name := "authz_test_" + strings.ToLower(rand.Text())[:databaseNameLen]

	admin, err := sql.Open("postgres", dsn)
	require.NoError(tb, err)

	// The admin handle runs one statement now and one at cleanup. Left at the
	// default it keeps its session idle in between, which is a third session
	// per test database for the life of the test, against the same hundred
	// the note below counts.
	admin.SetMaxIdleConns(0)

	// The identifier is generated here and quoted; no caller value reaches the
	// statement, and CREATE DATABASE takes no parameter.
	_, err = admin.ExecContext(tb.Context(), "CREATE DATABASE "+pq.QuoteIdentifier(name))
	require.NoError(tb, err)

	target := withDatabase(tb, dsn, name)

	handles := make([]*sql.DB, 0, count)

	for range count {
		db, openErr := sql.Open("postgres", target)
		require.NoError(tb, openErr)

		// A server holds one bounded pool, and the fixture allows a hundred
		// connections for the whole parallel suite: an unbounded handle here
		// answers "too many clients already", which looks exactly like the
		// thing under test failing. Two is the floor, because the migration
		// lock is held on a connection of its own while the run uses another.
		db.SetMaxOpenConns(postgresPoolLimit)

		handles = append(handles, db)
	}

	tb.Cleanup(func() {
		for _, db := range handles {
			require.NoError(tb, db.Close())
		}

		// The test's context is already cancelled by cleanup time.
		_, dropErr := admin.ExecContext(
			context.Background(),
			"DROP DATABASE IF EXISTS "+pq.QuoteIdentifier(name)+" WITH (FORCE)",
		)
		require.NoError(tb, dropErr)
		require.NoError(tb, admin.Close())
	})

	return handles
}

// withDatabase points a URL DSN at another database on the same server.
func withDatabase(tb testing.TB, dsn, name string) string {
	tb.Helper()

	parsed, err := url.Parse(dsn)
	require.NoErrorf(tb, err, "%s must be a postgres:// URL", PostgresDSNEnv)
	require.NotEmptyf(tb, parsed.Scheme, "%s must be a postgres:// URL", PostgresDSNEnv)

	parsed.Path = "/" + name

	return parsed.String()
}

// TestTheDriverThisBinaryLinksOpensTheGraph is the difference between a suite
// that reaches Postgres and a product that can. The suite opened the database
// itself, through a driver only a test imported; OpenPostgres is what the
// composition root calls, so this exercises the path a deployment takes.
func TestTheDriverThisBinaryLinksOpensTheGraph(t *testing.T) {
	t.Parallel()

	dsn := os.Getenv(PostgresDSNEnv)
	if dsn == "" {
		t.Skipf("%s is not set", PostgresDSNEnv)
	}

	name := "authz_open_" + strings.ToLower(rand.Text())[:databaseNameLen]

	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)

	_, err = admin.ExecContext(t.Context(), "CREATE DATABASE "+pq.QuoteIdentifier(name))
	require.NoError(t, err)

	db, err := OpenPostgres(withDatabase(t, dsn, name))
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, db.Close())

		_, dropErr := admin.ExecContext(
			context.Background(),
			"DROP DATABASE IF EXISTS "+pq.QuoteIdentifier(name)+" WITH (FORCE)",
		)
		require.NoError(t, dropErr)
		require.NoError(t, admin.Close())
	})

	graph, err := Open(db, DialectPostgres)
	require.NoError(t, err)
	require.NoError(t, migrateGraph(t.Context(), graph))
	require.NoError(t, graph.Reconcile(t.Context()))

	// The ledger, read with a statement of its own rather than through the
	// migrator that wrote it.
	var applied int
	require.NoError(t, db.QueryRowContext(t.Context(),
		"SELECT count(*) FROM schema_migration").Scan(&applied))
	require.Positive(t, applied)

	var epoch uint64
	require.NoError(t, db.QueryRowContext(t.Context(),
		"SELECT epoch FROM authz_epoch WHERE id = 1").Scan(&epoch))
	require.Positive(t, epoch)
}

// TestAnEmptyDSNIsRefused keeps the driver from being handed nothing: lib/pq
// takes an empty string and connects to a database named after the process
// owner, which is not this deployment's graph.
func TestAnEmptyDSNIsRefused(t *testing.T) {
	t.Parallel()

	_, err := OpenPostgres("")
	require.ErrorIs(t, err, ErrDatabasePath)
}
