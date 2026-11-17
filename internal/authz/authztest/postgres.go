//go:build postgres

package authztest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/lib/pq"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
)

// The entropy in a database name, and the connections one test handle may
// hold. The fixture allows a hundred for the whole parallel suite, and a run
// past them answers "too many clients", which reads like the store failing.
const (
	databaseNameLen   = 12
	postgresPoolLimit = 2
)

func postgresEngine(schema sqlstore.Schema) (Engine, bool) {
	dsn := os.Getenv(PostgresDSNEnv)
	if dsn == "" {
		return Engine{}, false
	}

	return Engine{
		Name:    "postgres",
		Dialect: authz.DialectPostgres,
		Engine:  sqlstore.EnginePostgres,
		Open: func(tb testing.TB) *sql.DB {
			tb.Helper()

			return migrated(tb, freshDatabase(tb, dsn), sqlstore.EnginePostgres, schema)
		},
		OpenEmpty: func(tb testing.TB) *sql.DB {
			tb.Helper()

			return freshDatabase(tb, dsn)
		},
		OpenShared: func(tb testing.TB, count int) []*sql.DB {
			tb.Helper()

			return sharedDatabase(tb, dsn, count)
		},
	}, true
}

// freshDatabase creates a database of the test's own on the server the DSN
// names, so cases stay independent and run in parallel, and drops it after.
func freshDatabase(tb testing.TB, dsn string) *sql.DB {
	tb.Helper()

	return sharedDatabase(tb, dsn, 1)[0]
}

// sharedDatabase creates one database of the test's own and returns count
// handles onto it, dropped after the test.
func sharedDatabase(tb testing.TB, dsn string, count int) []*sql.DB {
	tb.Helper()

	server, err := url.Parse(dsn)
	if err != nil || server.Scheme == "" {
		tb.Fatalf("%s must be a postgres:// URL", PostgresDSNEnv)
	}

	name := "authztest_" + strings.ToLower(rand.Text())[:databaseNameLen]

	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		tb.Fatalf("reach the postgres server: %v", err)
	}

	// The name is generated here and quoted; CREATE DATABASE takes no
	// parameter, and nothing a caller wrote reaches it.
	if _, err = admin.ExecContext(tb.Context(), "CREATE DATABASE "+pq.QuoteIdentifier(name)); err != nil {
		tb.Fatalf("create database %s: %v", name, err)
	}

	server.Path = "/" + name

	handles := make([]*sql.DB, 0, count)

	tb.Cleanup(func() {
		for _, db := range handles {
			_ = db.Close()
		}

		// The test's own context is cancelled by the time cleanup runs.
		_, _ = admin.ExecContext(context.Background(),
			"DROP DATABASE IF EXISTS "+pq.QuoteIdentifier(name)+" WITH (FORCE)")
		_ = admin.Close()
	})

	for range count {
		db, openErr := authz.OpenPostgres(server.String())
		if openErr != nil {
			tb.Fatalf("open database %s: %v", name, openErr)
		}

		db.SetMaxOpenConns(postgresPoolLimit)

		handles = append(handles, db)
	}

	return handles
}
