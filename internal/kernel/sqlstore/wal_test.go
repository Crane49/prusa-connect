package sqlstore_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	// A package over database/sql needs no driver; a test does. modernc is the
	// driver every build links, so this test covers the mode that ships.
	_ "modernc.org/sqlite"

	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
)

// errNotInWAL stands in for a caller's own sentinel. A caller passes one in so
// that the refusal names the caller, so the assertions below are on this value
// and on its text.
var errNotInWAL = errors.New("teststore: the file is not in write-ahead logging")

func openTemp(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.ToSlash(filepath.Join(t.TempDir(), "store.db"))

	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, db.Close()) })

	return db
}

func journalMode(t *testing.T, db *sql.DB) string {
	t.Helper()

	var mode string
	require.NoError(t, db.QueryRowContext(t.Context(), "PRAGMA journal_mode").Scan(&mode))

	return mode
}

// SQLite opens a new file in the rollback journal, where every read queues
// behind the current write.
func TestEnableWALPutsTheFileIntoWriteAheadLogging(t *testing.T) {
	t.Parallel()

	db := openTemp(t)

	require.NotEqual(t, "wal", strings.ToLower(journalMode(t, db)),
		"a fresh file is expected to start outside WAL, or this case proves nothing")

	require.NoError(t, sqlstore.EnableWAL(t.Context(), db, errNotInWAL))

	require.Equal(t, "wal", strings.ToLower(journalMode(t, db)))
}

// The second call on one handle succeeds. It must not report the mode as a
// change it could not make.
func TestEnableWALOnAFileAlreadyInWALTakesNoLock(t *testing.T) {
	t.Parallel()

	db := openTemp(t)

	require.NoError(t, sqlstore.EnableWAL(t.Context(), db, errNotInWAL))
	require.NoError(t, sqlstore.EnableWAL(t.Context(), db, errNotInWAL))

	require.Equal(t, "wal", strings.ToLower(journalMode(t, db)))
}

// The sentinel is in the signature so that a store keeps its own vocabulary.
// An operator reading the refusal is told which store would not take the mode.
func TestEnableWALReportsTheCallersSentinel(t *testing.T) {
	t.Parallel()

	db := openTemp(t)
	require.NoError(t, db.Close())

	err := sqlstore.EnableWAL(t.Context(), db, errNotInWAL)

	require.ErrorIs(t, err, errNotInWAL)
	require.Contains(t, err.Error(), "teststore: the file is not in write-ahead logging")
	require.Contains(t, err.Error(), "after 50 attempts")
}

// The retry loop must not outlive the start-up it runs inside. A cancelled open
// is a shutdown, and a deployment that waits out every pause there does not
// stop.
func TestEnableWALStopsWhenTheContextIsDone(t *testing.T) {
	t.Parallel()

	db := openTemp(t)
	require.NoError(t, db.Close())

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := sqlstore.EnableWAL(ctx, db, errNotInWAL)

	require.ErrorIs(t, err, errNotInWAL)
	require.ErrorIs(t, err, context.Canceled)
}
