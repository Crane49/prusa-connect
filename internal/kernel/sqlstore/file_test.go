package sqlstore_test

import (
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
)

// The driver created the database and the two files beside it through the
// umask, so another user on the host could read every grant and credential.
func TestOwnFileKeepsTheDatabaseAndItsLogFromOtherUsers(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Windows reports 0666 for any file a process may write")
	}

	path := filepath.Join(t.TempDir(), "store.db")

	require.NoError(t, sqlstore.OwnFile(path))

	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, db.Close()) })

	require.NoError(t, sqlstore.EnableWAL(t.Context(), db, errNotInWAL))

	_, err = db.ExecContext(t.Context(), "CREATE TABLE t (v INTEGER)")
	require.NoError(t, err)

	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		requireMode(t, name, sqlstore.FileMode)
	}
}

// A server upgraded in place finds the files an earlier version left readable.
func TestOwnFileNarrowsWhatAnEarlierProcessLeft(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Windows reports 0666 for any file a process may write")
	}

	path := filepath.Join(t.TempDir(), "store.db")

	for _, name := range []string{path, path + "-wal"} {
		require.NoError(t, os.WriteFile(name, []byte("left"), 0o600))
		require.NoError(t, os.Chmod(name, 0o644))
	}

	require.NoError(t, sqlstore.OwnFile(path))

	requireMode(t, path, sqlstore.FileMode)
	requireMode(t, path+"-wal", sqlstore.FileMode)

	_, err := os.Stat(path + "-shm")
	require.ErrorIs(t, err, fs.ErrNotExist, "a file nobody left is not created")
}

func requireMode(t *testing.T, name string, want fs.FileMode) {
	t.Helper()

	info, err := os.Stat(name)
	require.NoError(t, err)
	require.Equalf(t, want, info.Mode().Perm(), "%s", filepath.Base(name))
}
