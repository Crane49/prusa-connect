package app

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/authz"
)

// testHost is the host's own database, opened and migrated the way a
// deployment opens it: one handle for the graph, the accounts, the credentials
// and the registrations.
//
// It is a helper and not a fixture, because most of what reads a hostStore
// wants nothing more than an empty, migrated database on a file of its own.
func testHost(tb testing.TB) hostStore {
	tb.Helper()

	return testHostAt(tb, filepath.Join(tb.TempDir(), "host.db"))
}

// testHostAt is testHost against a named file, so a test can open the same
// database twice and see what the next process sees.
func testHostAt(tb testing.TB, path string) hostStore {
	tb.Helper()

	core := config.Default()
	core.Authz.Database = path

	return openTestHost(tb, core)
}

// openedHosts is the host each test opened through openTestHost, keyed by its
// graph, so a helper handed only the graph reaches the record store over the
// same database. A key is a graph one test opened, so tests share nothing.
var openedHosts sync.Map

// openTestHost opens the host database the way the composition root does and
// remembers it for recordsOf.
func openTestHost(tb testing.TB, core config.Core) hostStore {
	tb.Helper()

	host, closeGraph, err := openGraph(tb.Context(), core, "0.0.0-test", discard())
	require.NoError(tb, err)

	tb.Cleanup(closeGraph)

	openedHosts.Store(host.graph, host)
	tb.Cleanup(func() { openedHosts.Delete(host.graph) })

	return host
}

// recordsOf is the record store over the database graph was opened on.
func recordsOf(tb testing.TB, graph *authz.Store) records {
	tb.Helper()

	host, ok := openedHosts.Load(graph)
	require.True(tb, ok, "the graph was not opened through openTestHost")

	return host.(hostStore).devices
}

// forEachHostEngine runs body against every engine this run can reach: SQLite
// always, and Postgres in a build carrying the driver with a DSN in the
// environment, which is the invocation `make test-engines` uses.
//
// Both engines are first class for everything in the host's database, and the
// tables this session added are no exception: a store proved on one engine is a
// store half proved.
func forEachHostEngine(t *testing.T, body func(t *testing.T, host hostStore)) {
	t.Helper()

	t.Run("sqlite", func(t *testing.T) {
		t.Parallel()
		body(t, testHost(t))
	})

	t.Run("postgres", func(t *testing.T) {
		t.Parallel()

		dsn := os.Getenv(postgresDSNEnv)
		if dsn == "" {
			t.Skipf("%s is not set", postgresDSNEnv)
		}

		if !postgresDriverLinked(t, dsn) {
			t.Skip("this build carries no postgres driver")
		}

		core := config.Default()
		core.Overlay = "overlay.json"
		core.Modules = "modules"
		core.Authz.Database = filepath.Join(t.TempDir(), "unused", "authz.db")
		core.Persistence.Type = config.EnginePostgres
		core.Persistence.Postgres.DSN = scratchDatabase(t, dsn)

		require.NoError(t, core.Validate())

		host, closeGraph, err := openGraph(t.Context(), core, "0.0.0-test", discard())
		require.NoError(t, err)

		t.Cleanup(closeGraph)

		body(t, host)
	})
}
