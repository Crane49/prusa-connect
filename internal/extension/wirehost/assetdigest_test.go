package wirehost_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/extension/wirehost"
)

// aBundle writes a small tree of files the way a build leaves one: an entry
// file, and a chunk in a subdirectory.
func aBundle(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "assets"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.js"), []byte("import './assets/chunk.js'\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "assets", "chunk.js"), []byte("export const a = 1\n"), 0o600))

	return dir
}

// TestADirectoryDigestRoundTrips covers the install side: what an installer
// records is what the host verifies, and the record has the same shape as a
// file digest.
func TestADirectoryDigestRoundTrips(t *testing.T) {
	t.Parallel()

	dir := aBundle(t)

	digest, err := wirehost.DigestDir(dir)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(digest), "sha256:"), string(digest))
	require.Len(t, string(digest), len("sha256:")+64)

	require.NoError(t, digest.VerifyDir(dir))

	// The same bytes under another directory name hash the same. The record
	// covers the files and not where the operator put them.
	again := aBundle(t)
	require.NoError(t, digest.VerifyDir(again))
}

// TestADirectoryThatMovedFromItsDigestIsRefused covers the ways a tree can
// change after an install. After any of them the host must not serve it.
func TestADirectoryThatMovedFromItsDigestIsRefused(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		change func(t *testing.T, dir string)
	}{
		{
			name: "a file rewritten",
			change: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "index.js"), []byte("alert(1)\n"), 0o600))
			},
		},
		{
			name: "a file added",
			change: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "extra.js"), []byte("x"), 0o600))
			},
		},
		{
			name: "a file removed",
			change: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.Remove(filepath.Join(dir, "assets", "chunk.js")))
			},
		},
		{
			name: "a file renamed",
			change: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.Rename(filepath.Join(dir, "index.js"), filepath.Join(dir, "main.js")))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := aBundle(t)

			digest, err := wirehost.DigestDir(dir)
			require.NoError(t, err)

			tc.change(t, dir)

			err = digest.VerifyDir(dir)
			require.ErrorIs(t, err, wirehost.ErrDigestMismatch)
		})
	}
}

// TestADirectoryDigestRefusesWhatItCannotVouchFor covers the refusals that are
// not a mismatch: a record in the wrong form, a tree with nothing in it, and a
// link that could name a file outside the bundle.
func TestADirectoryDigestRefusesWhatItCannotVouchFor(t *testing.T) {
	t.Parallel()

	t.Run("a digest that is not sha256", func(t *testing.T) {
		t.Parallel()

		err := wirehost.Digest("md5:abc").VerifyDir(aBundle(t))
		require.ErrorIs(t, err, wirehost.ErrDigestFormat)
	})

	t.Run("an empty directory", func(t *testing.T) {
		t.Parallel()

		_, err := wirehost.DigestDir(t.TempDir())
		require.ErrorIs(t, err, wirehost.ErrBundleEmpty)
	})

	t.Run("a directory that does not exist", func(t *testing.T) {
		t.Parallel()

		_, err := wirehost.DigestDir(filepath.Join(t.TempDir(), "missing"))
		require.Error(t, err)
		require.NotErrorIs(t, err, wirehost.ErrDigestMismatch,
			"a missing directory reads as a mismatch, which an operator would answer by re-recording the digest")
	})

	t.Run("a symbolic link inside the tree", func(t *testing.T) {
		t.Parallel()

		if runtime.GOOS == "windows" {
			t.Skip("creating a symbolic link needs a privilege an ordinary account does not hold")
		}

		dir := aBundle(t)
		outside := filepath.Join(t.TempDir(), "secret")
		require.NoError(t, os.WriteFile(outside, []byte("s"), 0o600))
		require.NoError(t, os.Symlink(outside, filepath.Join(dir, "link.js")))

		_, err := wirehost.DigestDir(dir)
		require.ErrorIs(t, err, wirehost.ErrBundleEntry)
	})
}
