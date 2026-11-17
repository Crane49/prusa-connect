//go:build postgres

package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
)

// The PostgreSQL half of the app-level history suite, compiled only where the
// driver is: the adapter is behind the postgres build tag, which a
// configuration naming that engine derives through adapters/tags.json.

// databaseNameLen is enough entropy that two runs against one server, parallel
// ones included, do not collide.
const databaseNameLen = 12

// postgresPoolLimit is how many connections one case's own handle may open. The
// core opens a pool of its own beside it, and the fixture allows a hundred for
// the whole suite.
const postgresPoolLimit = 4

// dsnFor remembers the database created for one case's directory, so the
// configuration the core is started with and the handle the case reads back
// with name the same one, however many times either is asked for.
//
//nolint:gochecknoglobals // one map per test binary, keyed by the case's own temporary directory.
var (
	postgresDSNs   = map[string]string{}
	postgresDSNsMu sync.Mutex
)

// postgresHistoryEngine is reachable when the DSN is set, and this file is
// compiled.
func postgresHistoryEngine() (historyEngine, bool) {
	if os.Getenv(postgresDSNEnv) == "" {
		return historyEngine{}, false
	}

	return historyEngine{
		Name: "postgres",
		Config: func(t *testing.T, dir string) string {
			t.Helper()

			return lines(
				"core:",
				"  log:",
				"    level: error",
				"resources:",
				"  history:",
				"    past:",
				"      type: postgres",
				"      config:",
				"        dsn: "+freshPostgres(t, dir),
				"        retention:",
				`          raw: "0"`,
			)
		},
		Open: func(t *testing.T, dir string) *sql.DB {
			t.Helper()

			db, err := authz.OpenPostgres(freshPostgres(t, dir))
			require.NoError(t, err)

			db.SetMaxOpenConns(postgresPoolLimit)

			t.Cleanup(func() { _ = db.Close() })

			return db
		},
		JSONText: func(column, key string) string {
			return column + `::jsonb ->> '` + key + `'`
		},
		True: "TRUE",
	}, true
}

// freshPostgres is the DSN of a database of this case's own, created on first
// use and dropped when the case ends.
//
// A database per case rather than a schema per case, because the core opens the
// store the way a deployment does and a deployment names a database. Two cases
// sharing one would see each other's rows through the same migrated tables,
// which is exactly the independence a restart case cannot do without: it counts
// what one process left behind.
func freshPostgres(t *testing.T, dir string) string {
	t.Helper()

	postgresDSNsMu.Lock()
	defer postgresDSNsMu.Unlock()

	if held, made := postgresDSNs[dir]; made {
		return held
	}

	admin, err := sql.Open("postgres", os.Getenv(postgresDSNEnv))
	require.NoError(t, err)

	name := "app_history_test_" + strings.ToLower(rand.Text())[:databaseNameLen]

	// The identifier is generated here and quoted; no caller value reaches the
	// statement, and CREATE DATABASE takes no parameter.
	_, err = admin.ExecContext(t.Context(), "CREATE DATABASE "+pq.QuoteIdentifier(name))
	require.NoError(t, err)

	parsed, err := url.Parse(os.Getenv(postgresDSNEnv))
	require.NoErrorf(t, err, "%s must be a postgres:// URL", postgresDSNEnv)

	parsed.Path = "/" + name

	postgresDSNs[dir] = parsed.String()

	t.Cleanup(func() {
		postgresDSNsMu.Lock()
		delete(postgresDSNs, dir)
		postgresDSNsMu.Unlock()

		// The case's context is already cancelled by cleanup time.
		_, dropErr := admin.ExecContext(context.Background(),
			"DROP DATABASE IF EXISTS "+pq.QuoteIdentifier(name)+" WITH (FORCE)")
		require.NoError(t, dropErr)
		require.NoError(t, admin.Close())
	})

	return postgresDSNs[dir]
}
