package authz

import (
	"context"
	"database/sql"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
)

// RequiredEnginesEnv names the environment variable that turns a missing engine
// from a skip into a failure. CI sets it to "sqlite,postgres": a skipped test
// reports ok, and a run that reports ok while every behavioural test skipped is
// indistinguishable from a run that proved something.
const RequiredEnginesEnv = "AUTHZ_REQUIRED_ENGINES"

// PostgresDSNEnv names the environment variable that points the suite at a
// Postgres instance. Set RequiredEnginesEnv with it, always: without the DSN the
// Postgres engine is skipped, and a skip reports ok, so the invocation to run is
//
//	AUTHZ_TEST_POSTGRES_DSN=postgres://... AUTHZ_REQUIRED_ENGINES=sqlite,postgres go test -tags "sqlite postgres" ./internal/authz/...
//
// It lives outside the postgres build tag because the tests that explain how to
// arm the engine have to compile in the builds that do not have it.
const PostgresDSNEnv = "AUTHZ_TEST_POSTGRES_DSN"

// engine is one SQL backend a test runs against. Both engines are first class,
// so every behavioural test runs the same table against each one that is
// compiled in.
type engine struct {
	Name    string
	Dialect Dialect
	// Open returns an empty database of this engine, torn down with the test.
	Open func(tb testing.TB) *sql.DB
	// Shared returns count handles onto one empty database, which is what
	// several processes starting against one deployment look like. Separate
	// handles rather than one pool, because a pool serialises nothing and a
	// process holds its own.
	Shared func(tb testing.TB, count int) []*sql.DB
}

// availableEngines returns the engines this binary can reach. SQLite needs the
// sqlite build tag; Postgres needs the postgres build tag and a DSN in
// AUTHZ_TEST_POSTGRES_DSN.
func availableEngines() []engine {
	var out []engine

	if e, ok := sqliteEngine(); ok {
		out = append(out, e)
	}

	if e, ok := postgresEngine(); ok {
		out = append(out, e)
	}

	return out
}

const noEngineSkip = "no sql engine available: build with -tags sqlite, " +
	"or -tags postgres with AUTHZ_TEST_POSTGRES_DSN set"

// requiredEngines is what the run has to exercise, from the environment.
func requiredEngines() []string {
	var out []string

	for name := range strings.SplitSeq(os.Getenv(RequiredEnginesEnv), ",") {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			out = append(out, trimmed)
		}
	}

	return out
}

// engines returns what this run will exercise, failing rather than skipping
// when the environment says an engine is required and it is not there.
func engines(tb testing.TB) []engine {
	tb.Helper()

	available := availableEngines()

	names := make([]string, 0, len(available))
	for _, e := range available {
		names = append(names, e.Name)
	}

	for _, required := range requiredEngines() {
		if !slices.Contains(names, required) {
			tb.Fatalf("%s requires the %s engine and this build has %v", RequiredEnginesEnv, required, names)
		}
	}

	return available
}

// forEachEngine runs body as a subtest per available engine.
func forEachEngine(t *testing.T, body func(t *testing.T, e engine)) {
	t.Helper()

	engines := engines(t)
	if len(engines) == 0 {
		t.Skip(noEngineSkip)
	}

	for _, e := range engines {
		t.Run(e.Name, func(t *testing.T) {
			t.Parallel()
			body(t, e)
		})
	}
}

// benchEngines returns the engines a benchmark runs against, skipping the
// benchmark when there are none.
func benchEngines(b *testing.B) []engine {
	b.Helper()

	available := engines(b)
	if len(available) == 0 {
		b.Skip(noEngineSkip)
	}

	return available
}

// newStore opens a migrated, empty store on this engine, in the order a server
// starts: migrate, reconcile, then serve. Until Reconcile has run the lookup
// tables are empty and the database refuses every tuple, which is the right way
// round for an authorization graph and the wrong way round for a test that
// forgot the second call.
func newStore(tb testing.TB, e engine, options ...Option) *Store {
	tb.Helper()

	store, err := Open(e.Open(tb), e.Dialect, options...)
	require.NoError(tb, err)
	require.NoError(tb, migrateGraph(tb.Context(), store))
	require.NoError(tb, store.Reconcile(tb.Context()))

	return store
}

// migrateGraph applies the graph's own schema to the store's database, as the
// host's migrator does before it opens the graph.
func migrateGraph(ctx context.Context, store *Store) error {
	engine := sqlstore.EngineSQLite
	if store.dialect == DialectPostgres {
		engine = sqlstore.EnginePostgres
	}

	schema := sqlstore.Schema{Owners: []sqlstore.Migrations{Migrations()}}

	migrator, err := sqlstore.NewMigrator(store.db, engine, schema, "")
	if err != nil {
		return err
	}

	return migrator.Migrate(ctx)
}

// concurrentStarts is how many processes race into the same empty database. It
// is above the pool's default idle count on purpose, so the run also exercises
// a handle that had to open a connection while another one held the lock.
const concurrentStarts = 8
