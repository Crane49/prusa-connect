package engine

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// The old backlog. Every open bounds the pool, so a sample flood queues where a
// cancelled write still leaves rather than inside the database.
func TestOpeningTheStoreBoundsThePool(t *testing.T) {
	t.Parallel()

	db, err := OpenSQLite(t.Context(), filepath.Join(t.TempDir(), "history.db"), 0)
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })

	require.Equal(t, DefaultMaxOpen, db.Stats().MaxOpenConnections,
		"the handle was left with no bound on how many connections it opens")
}

// The store's directory often exists already, with a mode this store did not
// choose, so the file and the log beside it are what keep the fleet's history
// from another user on the host.
func TestTheStoreFileIsItsOwnersAlone(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Windows reports 0666 for any file a process may write")
	}

	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o755))

	path := filepath.Join(dir, "history.db")

	db, err := OpenSQLite(t.Context(), path, 0)
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })

	_, err = db.ExecContext(t.Context(), "CREATE TABLE t (v INTEGER)")
	require.NoError(t, err)

	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		info, statErr := os.Stat(name)
		require.NoError(t, statErr)
		require.Equalf(t, os.FileMode(storeFilePerm), info.Mode().Perm(), "%s", filepath.Base(name))
	}
}

// The statement timeout reaches the server as a connection setting in the DSN,
// because that is the only place every connection the pool opens reads.
func TestTheStatementTimeoutReachesBothDSNForms(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		dsn  string
		want string
	}{
		"key and value": {
			"host=db dbname=history", "host=db dbname=history statement_timeout=30000",
		},
		"a url with no query": {
			"postgres://db:5432/history", "postgres://db:5432/history?statement_timeout=30000",
		},
		"a url that already has a query": {
			"postgres://db:5432/history?sslmode=disable",
			"postgres://db:5432/history?sslmode=disable&statement_timeout=30000",
		},
		// The operator wrote that number on purpose, in either form.
		"a dsn that already names it": {
			"postgres://db:5432/history?statement_timeout=1000",
			"postgres://db:5432/history?statement_timeout=1000",
		},
		"nothing at all": {"", ""},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.want, withStatementTimeout(tc.dsn))
		})
	}
}

// Every connection the store opens reads the page cache it was given. Zero
// keeps the size SQLite picks for a connection nobody configured.
func TestEveryStoreConnectionTakesThePageCache(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	bare, err := sql.Open(sqliteDriver, filepath.Join(dir, "bare.db"))
	require.NoError(t, err)

	t.Cleanup(func() { _ = bare.Close() })

	var sqlitesOwn int
	require.NoError(t, bare.QueryRowContext(t.Context(), "PRAGMA cache_size").Scan(&sqlitesOwn))

	cases := map[string]struct {
		bytes int
		want  int
	}{
		"none asked for": {0, sqlitesOwn},
		"half a MiB":     {512 << 10, -512},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			db, openErr := OpenSQLite(t.Context(), filepath.Join(t.TempDir(), "history.db"), tc.bytes)
			require.NoError(t, openErr)

			t.Cleanup(func() { _ = db.Close() })

			// Two connections are held at once, so the second is a new
			// connection and not the first one handed back.
			for range 2 {
				conn, connErr := db.Conn(t.Context())
				require.NoError(t, connErr)

				t.Cleanup(func() { _ = conn.Close() })

				var size int
				require.NoError(t, conn.QueryRowContext(t.Context(), "PRAGMA cache_size").Scan(&size))
				require.Equal(t, tc.want, size)
			}
		})
	}
}

// A page cache that is not a size is refused with the instance it names.
func TestAPageCacheThatIsNotASizeIsRefused(t *testing.T) {
	t.Parallel()

	for _, bytes := range []int{-1, 512} {
		err := CheckPageCache("history/main", bytes)
		require.ErrorIs(t, err, ErrSettings)
		require.ErrorContains(t, err, "history/main")
	}

	require.NoError(t, CheckPageCache("history/main", 0))
	require.NoError(t, CheckPageCache("history/main", 1024))
}

// A Postgres store has no page cache, so page_cache_bytes there is refused
// rather than ignored. An absent key is fine.
func TestAPageCacheOnAPostgresStoreIsRefused(t *testing.T) {
	t.Parallel()

	err := RefusePageCacheOnPostgres("history/main", 524288)
	require.ErrorIs(t, err, ErrSettings)
	require.ErrorContains(t, err, "history/main")
	require.ErrorContains(t, err, "page_cache_bytes")

	require.NoError(t, RefusePageCacheOnPostgres("history/main", 0))
}
