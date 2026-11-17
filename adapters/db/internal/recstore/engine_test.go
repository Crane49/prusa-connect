package recstore_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	// The store is pure database/sql; a test needs a driver. modernc rather
	// than the cgo one, because the shipping binary links modernc and a green
	// run under a driver that does not ship proves nothing.
	_ "modernc.org/sqlite"

	"github.com/prusa3d/prusa-connect/adapters/db/internal/recstore"
)

// The environment this suite reads. They are internal/authz's names on purpose:
// one deployment has one engine, so the invocation that arms the graph's suite
// is the invocation that arms this one, and an operator or a CI lane learns one
// pair rather than two.
const (
	// requiredEnginesEnv turns a missing engine from a skip into a failure. A
	// skipped test reports ok, and a run that reports ok while every case
	// skipped is indistinguishable from a run that proved something.
	requiredEnginesEnv = "AUTHZ_REQUIRED_ENGINES"

	// postgresDSNEnv points the suite at a Postgres server. Set it with
	// requiredEnginesEnv, always:
	//
	//	AUTHZ_TEST_POSTGRES_DSN=postgres://... AUTHZ_REQUIRED_ENGINES=sqlite,postgres \
	//	  go test -tags "sqlite postgres" ./internal/extension/state/...
	postgresDSNEnv = "AUTHZ_TEST_POSTGRES_DSN"
)

// engine is one SQL backend the suite runs against. Both are first class, so
// every case below runs against each one that is reachable.
type engine struct {
	Name    string
	Dialect recstore.Dialect
	// Open returns an empty database of this engine, torn down with the test.
	Open func(tb testing.TB) *sql.DB
}

// sqliteEngine is always there: the driver is linked into every build.
func sqliteEngine() engine {
	return engine{
		Name:    "sqlite",
		Dialect: recstore.DialectSQLite,
		Open: func(tb testing.TB) *sql.DB {
			tb.Helper()

			// The same pragmas the graph opens with, so a lock this store takes
			// behaves here as it does in a deployment.
			path := filepath.ToSlash(filepath.Join(tb.TempDir(), "state.db"))

			db, err := sql.Open("sqlite", "file:"+path+
				"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"+
				"&_pragma=synchronous(NORMAL)&_txlock=immediate")
			require.NoError(tb, err)

			tb.Cleanup(func() { require.NoError(tb, db.Close()) })

			return db
		},
	}
}

// engines is what this run will exercise, failing rather than skipping when the
// environment says an engine is required and it is not reachable.
func engines(tb testing.TB) []engine {
	tb.Helper()

	available := []engine{sqliteEngine()}

	if postgres, ok := postgresEngine(); ok {
		available = append(available, postgres)
	}

	names := make([]string, 0, len(available))
	for _, e := range available {
		names = append(names, e.Name)
	}

	for required := range strings.SplitSeq(os.Getenv(requiredEnginesEnv), ",") {
		trimmed := strings.TrimSpace(required)
		if trimmed != "" && !slices.Contains(names, trimmed) {
			tb.Fatalf("%s requires the %s engine and this build has %v",
				requiredEnginesEnv, trimmed, names)
		}
	}

	return available
}

// forEachEngine runs body as a subtest per reachable engine.
func forEachEngine(t *testing.T, body func(t *testing.T, e engine)) {
	t.Helper()

	for _, e := range engines(t) {
		t.Run(e.Name, func(t *testing.T) {
			t.Parallel()
			body(t, e)
		})
	}
}

// newStore opens a migrated, empty record store on this engine.
func newStore(tb testing.TB, e engine) *recstore.Store {
	tb.Helper()

	store, err := recstore.New(e.Open(tb), e.Dialect)
	require.NoError(tb, err)
	require.NoError(tb, store.Migrate(tb.Context()))

	return store
}
