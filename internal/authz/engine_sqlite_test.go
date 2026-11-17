package authz

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"

	// The pure-database/sql store needs no driver; a test does. modernc rather
	// than the cgo driver, because M6 ships CGO_ENABLED=0 and the vocabulary is
	// enforced by a foreign key: foreign keys are off by default in SQLite
	// and turned on through a DSN parameter the two drivers spell differently,
	// so a green run under the driver that does not ship proves nothing
	// (earlier research).
	//
	// No build tag gates this engine any more, because none gates the driver:
	// sqlite.go links it into every build, so a suite that skipped without a
	// tag was skipping over code that was there.
	_ "modernc.org/sqlite"
)

// sqliteDSN mirrors the settings the rest of the tree opens SQLite with: a busy
// timeout that outlives a contended write, NORMAL synchronous, the foreign keys
// authz_tuple's composite constraint is only real with, and the immediate
// transaction mode that keeps a read-then-write from dying on a stale snapshot.
// modernc spells every pragma as _pragma=name(value), where the cgo driver had
// one flag per pragma. Write-ahead logging is not here for the reason
// enableWAL gives.
const sqliteDSN = "?" + sqlitePragmas

// sqliteDSNWithoutForeignKeys is the same handle with the one pragma that makes
// the constraint real left out, for the test that proves the difference is
// observable rather than assumed.
const sqliteDSNWithoutForeignKeys = "?_pragma=busy_timeout(5000)" +
	"&_pragma=synchronous(NORMAL)&_txlock=immediate"

func sqliteEngine() (engine, bool) {
	return engine{
		Name:    "sqlite",
		Dialect: DialectSQLite,
		Open: func(tb testing.TB) *sql.DB {
			tb.Helper()

			return openSQLite(tb, sqliteDSN)
		},
		Shared: func(tb testing.TB, count int) []*sql.DB {
			tb.Helper()

			path := filepath.Join(tb.TempDir(), "authz.db")

			handles := make([]*sql.DB, 0, count)

			for range count {
				handles = append(handles, openSQLiteAt(tb, path, sqliteDSN))
			}

			return handles
		},
	}, true
}

// openUnsafeSQLite opens a handle with foreign key enforcement left off, which
// is SQLite's default and therefore the shape of the mistake worth testing.
func openUnsafeSQLite(tb testing.TB) (*sql.DB, bool) {
	tb.Helper()

	return openSQLite(tb, sqliteDSNWithoutForeignKeys), true
}

func openSQLite(tb testing.TB, dsn string) *sql.DB {
	tb.Helper()

	return openSQLiteAt(tb, filepath.Join(tb.TempDir(), "authz.db"), dsn)
}

func openSQLiteAt(tb testing.TB, path, dsn string) *sql.DB {
	tb.Helper()

	db, err := sql.Open("sqlite", path+dsn)
	require.NoError(tb, err)

	tb.Cleanup(func() { require.NoError(tb, db.Close()) })

	// The same call OpenSQLite makes, so a test runs on the journal mode that
	// ships rather than on SQLite's rollback-journal default.
	require.NoError(tb, sqlstore.EnableWAL(tb.Context(), db, ErrJournalMode))

	return db
}

// The old backlog. Every open bounds the pool, so a burst of requests queues where
// a cancelled one still leaves rather than inside the database.
func TestOpeningTheGraphBoundsThePool(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "authz.db")

	db, err := OpenSQLite(t.Context(), path)
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })

	require.Equal(t, sqlstore.DefaultMaxOpen, db.Stats().MaxOpenConnections,
		"an open that named no pool was left unbounded")

	named, err := OpenSQLite(t.Context(), filepath.Join(t.TempDir(), "named.db"),
		sqlstore.Pool{MaxOpen: 3, MaxIdle: 1, MaxLifetime: time.Minute})
	require.NoError(t, err)

	t.Cleanup(func() { _ = named.Close() })

	require.Equal(t, 3, named.Stats().MaxOpenConnections)
}

// Finding CL-94. The directory the graph goes into often exists already, with
// a mode this server did not choose, so the file itself is what keeps the
// grants and credentials from another user on the host.
func TestTheGraphFileIsItsOwnersAlone(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Windows reports 0666 for any file a process may write")
	}

	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o755))

	path := filepath.Join(dir, "authz.db")

	db, err := OpenSQLite(t.Context(), path)
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(sqlstore.FileMode), info.Mode().Perm())
}

// Every connection the graph's pool opens reads the page cache it was given.
// Two connections are held at once, so the second is a new connection and not
// the first one handed back.
func TestEveryGraphConnectionTakesThePageCache(t *testing.T) {
	t.Parallel()

	db, err := OpenSQLite(t.Context(), filepath.Join(t.TempDir(), "authz.db"),
		sqlstore.Pool{MaxOpen: 2, MaxIdle: 2, MaxLifetime: time.Minute, PageCacheBytes: 512 << 10})
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })

	for range 2 {
		conn, connErr := db.Conn(t.Context())
		require.NoError(t, connErr)

		t.Cleanup(func() { _ = conn.Close() })

		var size int
		require.NoError(t, conn.QueryRowContext(t.Context(), "PRAGMA cache_size").Scan(&size))
		require.Equal(t, -512, size, "a size in KiB is written as a negative number")
	}
}
