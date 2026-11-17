// Package authztest opens the host's own database for a test of a store that
// keeps its rows there: empty, and migrated with the schema the host ships, on
// every engine this run can reach.
//
// A store tested against tables built from its own CREATE statements would be
// a test of a schema nothing ships, so a store reaches its tables through
// here. The authorization graph's migrations always run first, as the host
// runs them. A store passes its own after them.
package authztest

import (
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
)

// The two environment variables the graph's own suite reads, so one invocation
// covers every suite: PostgresDSNEnv points at a server, and
// RequiredEnginesEnv turns a missing engine from a skip into a failure.
const (
	PostgresDSNEnv     = "AUTHZ_TEST_POSTGRES_DSN"
	RequiredEnginesEnv = "AUTHZ_REQUIRED_ENGINES"
)

// Engine is one SQL backend a test runs against.
type Engine struct {
	Name    string
	Dialect authz.Dialect
	// Engine is the same backend as a store over the host's database names
	// it.
	Engine sqlstore.Engine
	// Open returns a handle onto an empty database of the test's own,
	// migrated with the graph's schema and the owners ForEachEngine was given,
	// closed when the test ends.
	Open func(tb testing.TB) *sql.DB
	// OpenEmpty returns a handle onto an empty database no migration has
	// touched, closed when the test ends.
	OpenEmpty func(tb testing.TB) *sql.DB
	// OpenShared returns count handles onto one empty database no migration
	// has touched, as several processes starting against one deployment would
	// hold. The handles are separate and not one pool, because a process holds
	// its own pool.
	OpenShared func(tb testing.TB, count int) []*sql.DB
}

// ForEachEngine runs body as a parallel subtest per engine this run reaches.
// owners are migrated after the graph's own schema, in the order given.
// An engine RequiredEnginesEnv names and this build cannot reach fails the
// test. A run that skipped every case would report ok and prove nothing.
func ForEachEngine(t *testing.T, body func(t *testing.T, e Engine), owners ...sqlstore.Migrations) {
	t.Helper()

	for _, e := range Engines(t, owners...) {
		t.Run(e.Name, func(t *testing.T) {
			t.Parallel()
			body(t, e)
		})
	}
}

// Engines is every engine this run reaches, for a test that compares them with
// each other. owners are migrated after the
// graph's own schema, in the order given.
func Engines(t testing.TB, owners ...sqlstore.Migrations) []Engine {
	t.Helper()

	schema := sqlstore.Schema{Owners: append([]sqlstore.Migrations{authz.Migrations()}, owners...)}

	reached := []Engine{sqliteEngine(schema)}
	if e, ok := postgresEngine(schema); ok {
		reached = append(reached, e)
	}

	for name := range strings.SplitSeq(os.Getenv(RequiredEnginesEnv), ",") {
		name = strings.TrimSpace(name)
		if name != "" && !slices.ContainsFunc(reached, func(e Engine) bool { return e.Name == name }) {
			t.Fatalf("%s requires the %s engine, and this build reaches %d engine(s); "+
				"set %s and build with -tags postgres", RequiredEnginesEnv, name, len(reached), PostgresDSNEnv)
		}
	}

	return reached
}

// sqliteEngine is always reached: the driver is linked into every build,
// because the graph runs on it.
func sqliteEngine(schema sqlstore.Schema) Engine {
	shared := func(tb testing.TB, count int) []*sql.DB {
		tb.Helper()

		path := filepath.Join(tb.TempDir(), "host.db")
		handles := make([]*sql.DB, 0, count)

		for range count {
			db, err := authz.OpenSQLite(tb.Context(), path)
			if err != nil {
				tb.Fatalf("open the host database: %v", err)
			}

			tb.Cleanup(func() { _ = db.Close() })

			handles = append(handles, db)
		}

		return handles
	}

	empty := func(tb testing.TB) *sql.DB {
		tb.Helper()

		return shared(tb, 1)[0]
	}

	return Engine{
		Name:       "sqlite",
		Dialect:    authz.DialectSQLite,
		Engine:     sqlstore.EngineSQLite,
		OpenEmpty:  empty,
		OpenShared: shared,
		Open: func(tb testing.TB) *sql.DB {
			tb.Helper()

			return migrated(tb, empty(tb), sqlstore.EngineSQLite, schema)
		},
	}
}

// migrated runs schema over an open handle and hands it back.
func migrated(tb testing.TB, db *sql.DB, engine sqlstore.Engine, schema sqlstore.Schema) *sql.DB {
	tb.Helper()

	migrator, err := sqlstore.NewMigrator(db, engine, schema, "")
	if err == nil {
		err = migrator.Migrate(tb.Context())
	}

	if err != nil {
		tb.Fatalf("migrate the host database: %v", err)
	}

	return db
}
