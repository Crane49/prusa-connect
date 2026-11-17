package histstore_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	sqlengine "github.com/prusa3d/prusa-connect/adapters/db/internal/engine"
	"github.com/prusa3d/prusa-connect/adapters/db/internal/histstore"
)

// The environment this suite reads. They are internal/authz's names on purpose:
// one deployment has one engine, so the invocation that arms the graph's suite
// arms this one, and an operator or a CI lane learns one pair rather than three.
const (
	// requiredEnginesEnv turns a missing engine from a skip into a failure. A
	// skipped test reports ok, and a run that reports ok while every case
	// skipped is indistinguishable from a run that proved something.
	requiredEnginesEnv = "AUTHZ_REQUIRED_ENGINES"

	// postgresDSNEnv points the suite at a PostgreSQL server. Set it with
	// requiredEnginesEnv, always:
	//
	//	AUTHZ_TEST_POSTGRES_DSN=postgres://... AUTHZ_REQUIRED_ENGINES=sqlite,postgres \
	//	  go test -tags "sqlite postgres" ./internal/platform/history/...
	postgresDSNEnv = "AUTHZ_TEST_POSTGRES_DSN"
)

// engine is one SQL backend the suite runs against. Both are first class, so
// every case below runs against each one that is reachable.
type engine struct {
	Name    string
	Dialect histstore.Dialect
	// Open returns an empty database of this engine, torn down with the test.
	Open func(tb testing.TB) *sql.DB
}

func (e engine) tableCount(table string) string {
	if e.Dialect == histstore.DialectPostgres {
		return `SELECT COUNT(*) FROM information_schema.tables
			WHERE table_schema = current_schema() AND table_name = '` + table + `'`
	}

	return `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = '` + table + `'`
}

// sqliteEngine is always there: the driver is linked into every build.
//
// It opens through sqlengine.OpenSQLite rather than through database/sql, so the
// pragmas the store is measured under are the pragmas the cases run on.
func sqliteEngine() engine {
	return engine{
		Name:    "sqlite",
		Dialect: histstore.DialectSQLite,
		Open: func(tb testing.TB) *sql.DB {
			tb.Helper()

			path := filepath.ToSlash(filepath.Join(tb.TempDir(), "history.db"))

			db, err := sqlengine.OpenSQLite(tb.Context(), path, 0)
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

// newStore opens a migrated, empty history store on this engine.
func newStore(tb testing.TB, e engine) *histstore.Store {
	tb.Helper()

	store, err := histstore.New(e.Open(tb), e.Dialect)
	require.NoError(tb, err)
	require.NoError(tb, store.Migrate(tb.Context()))

	return store
}
