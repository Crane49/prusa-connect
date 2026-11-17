package atomicfile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/atomicfile"
)

func TestWritePublishesTheWholeFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, atomicfile.Write(path, []byte("first\n"), 0o600))

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "first\n", string(content))

	require.NoError(t, atomicfile.Write(path, []byte("second\n"), 0o600))

	content, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "second\n", string(content))
}

// TestACrashBetweenStageAndCommitLeavesThePreviousBytes is the proof for
// an earlier design decision. A crash between the write and the rename is exactly a Stage with
// no Commit: the data is on disk under another name and the destination has
// never been opened.
func TestACrashBetweenStageAndCommitLeavesThePreviousBytes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	require.NoError(t, atomicfile.Write(path, []byte(`{"version":1}`), 0o600))

	staged, err := atomicfile.Stage(path, []byte("torn"), 0o600)
	require.NoError(t, err)
	require.NotEqual(t, path, staged.Path())

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, `{"version":1}`, string(content),
		"the destination must still hold the previous bytes while a write is staged")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 2, "a staged write is the destination plus one temporary file")

	require.NoError(t, staged.Commit())

	content, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "torn", string(content))
}

func TestDiscardLeavesNothingBehind(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	staged, err := atomicfile.Stage(path, []byte("abandoned"), 0o600)
	require.NoError(t, err)
	require.NoError(t, staged.Discard())

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries)

	require.NoError(t, staged.Discard(), "discarding twice is not an error")
}

func TestCommitTwiceIsRefused(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.json")

	staged, err := atomicfile.Stage(path, []byte("once"), 0o600)
	require.NoError(t, err)
	require.NoError(t, staged.Commit())
	require.ErrorIs(t, staged.Commit(), atomicfile.ErrCommitted)
}

func TestStageFailsWhenTheDirectoryIsMissing(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "absent", "config.json")

	_, err := atomicfile.Stage(path, []byte("nowhere"), 0o600)
	require.Error(t, err)
	require.Contains(t, err.Error(), "create a temporary file")
}

// TestTheTemporaryFileIsInTheDestinationDirectory is step 1 of an earlier design decision. A
// temporary under the system temp directory is a different mount in most
// containers, and the rename that publishes it fails there rather than here.
func TestTheTemporaryFileIsInTheDestinationDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	staged, err := atomicfile.Stage(path, []byte("x"), 0o600)
	require.NoError(t, err)

	t.Cleanup(func() { _ = staged.Discard() })

	require.Equal(t, dir, filepath.Dir(staged.Path()))
	require.True(t, strings.HasPrefix(filepath.Base(staged.Path()), "config.json.tmp"))
}
