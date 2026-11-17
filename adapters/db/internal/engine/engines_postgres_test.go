//go:build postgres

package engine

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

// postgresTestEngine is reachable when the DSN is set and this file is
// compiled. Each case gets a database of its own on that server.
func postgresTestEngine() (testEngine, bool) {
	dsn := os.Getenv(postgresDSNEnv)
	if dsn == "" {
		return testEngine{}, false
	}

	return testEngine{name: "postgres", postgres: true, open: func(tb testing.TB) *sql.DB {
		tb.Helper()

		name := "engine_test_" + strings.ToLower(rand.Text())[:12]

		admin, err := sql.Open("postgres", dsn)
		require.NoError(tb, err)

		// The name is generated here and quoted. CREATE DATABASE takes no
		// parameter, and nothing a caller wrote reaches it.
		_, err = admin.ExecContext(tb.Context(), "CREATE DATABASE "+pq.QuoteIdentifier(name))
		require.NoError(tb, err)

		parsed, err := url.Parse(dsn)
		require.NoError(tb, err)

		parsed.Path = "/" + name

		db, err := OpenPostgres(parsed.String())
		require.NoError(tb, err)

		tb.Cleanup(func() {
			_ = db.Close()
			// The test's own context is cancelled by the time cleanup runs.
			_, _ = admin.ExecContext(context.Background(),
				"DROP DATABASE IF EXISTS "+pq.QuoteIdentifier(name)+" WITH (FORCE)")
			_ = admin.Close()
		})

		return db
	}}, true
}
