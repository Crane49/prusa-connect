package betaportal

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLocateKeepsAPairingWhereAnEarlierReleaseKeptIt(t *testing.T) {
	t.Parallel()

	etc := t.TempDir()
	overlay := filepath.Join(t.TempDir(), "resources.json")
	configPath := filepath.Join(etc, "config.yaml")

	require.Equal(t, Path(overlay), Locate(configPath, overlay))

	_, err := Open(filepath.Join(etc, "config.d", pairingFile))
	require.NoError(t, err)
	require.Equal(t, filepath.Join(etc, "config.d", pairingFile), Locate(configPath, overlay))
}

func TestResolvePassesOverAnEarlierPairingItCannotUse(t *testing.T) {
	t.Parallel()

	etc := t.TempDir()
	overlay := filepath.Join(t.TempDir(), "resources.json")
	configPath := filepath.Join(etc, "config.yaml")
	earlier := EarlierPath(configPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(earlier), 0o700))
	require.NoError(t, os.WriteFile(earlier, []byte("not a pairing"), 0o644))
	require.NoError(t, os.Chmod(earlier, 0o644))

	path, err := Resolve(configPath, overlay)
	require.Equal(t, Path(overlay), path)
	require.ErrorContains(t, err, "decode the Beta Portal pairing")
	require.Equal(t, Path(overlay), Locate(configPath, overlay))

	content, err := os.ReadFile(earlier)
	require.NoError(t, err)
	require.Equal(t, "not a pairing", string(content), "the earlier pairing was changed")
	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(earlier)
		require.NoError(t, statErr)
		require.Equal(t, os.FileMode(fileMode), info.Mode().Perm(), "a damaged pairing was left readable")
	}
}

// TestResolveLeavesOneFileToOpen is the folder install, whose overlay sits in
// config.d, so the earlier file and the one beside the overlay are one file.
// A damaged file there is not passed over, because there is no other file.
func TestResolveLeavesOneFileToOpen(t *testing.T) {
	t.Parallel()

	etc := t.TempDir()
	configPath := filepath.Join(etc, "config.yaml")
	overlay := filepath.Join(etc, "config.d", "resources.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(overlay), 0o700))
	require.NoError(t, os.WriteFile(Path(overlay), []byte("not a pairing"), fileMode))

	path, err := Resolve(configPath, overlay)
	require.NoError(t, err)
	require.Equal(t, Path(overlay), path)
}

// TestResolveLeavesTheModeOfALinkedFile is an earlier pairing that is a link to
// another file, which does not decode. The other file keeps its mode.
func TestResolveLeavesTheModeOfALinkedFile(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("a file mode does not apply here")
	}

	etc, other := t.TempDir(), t.TempDir()
	configPath := filepath.Join(etc, "config.yaml")
	script := filepath.Join(other, "script.sh")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.Chmod(script, 0o755))
	earlier := EarlierPath(configPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(earlier), 0o700))
	if err := os.Symlink(script, earlier); err != nil {
		t.Skipf("no symbolic link here: %v", err)
	}

	_, err := Resolve(configPath, filepath.Join(t.TempDir(), "resources.json"))
	require.Error(t, err)
	info, err := os.Stat(script)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o755), info.Mode().Perm(), "the linked file was changed")
}

// TestResolveOnAReadOnlyFileSystem stands for a read-only file system, where
// every chmod fails. A protected earlier file is used without one, and an
// unprotected one is passed over rather than stopping the start.
func TestResolveOnAReadOnlyFileSystem(t *testing.T) {
	refuse := func(string, os.FileMode) error { return syscall.EROFS }
	chmod = refuse
	t.Cleanup(func() { chmod = os.Chmod })

	for name, mode := range map[string]os.FileMode{"protected": fileMode, "unprotected": 0o644} {
		etc := t.TempDir()
		configPath := filepath.Join(etc, "config.yaml")
		overlay := filepath.Join(t.TempDir(), "resources.json")
		earlier := EarlierPath(configPath)
		chmod = os.Chmod
		_, err := Open(earlier)
		require.NoError(t, err, name)
		require.NoError(t, os.Chmod(earlier, mode), name)
		chmod = refuse

		path, err := Resolve(configPath, overlay)
		if mode == fileMode && runtime.GOOS != "windows" {
			require.NoError(t, err, name)
			require.Equal(t, earlier, path, name)
			_, err = Open(path)
			require.NoError(t, err, "%s: a protected pairing did not open without a chmod", name)
		} else {
			require.ErrorIs(t, err, syscall.EROFS, name)
			require.ErrorIs(t, err, ErrNotProtected, name)
			require.Equal(t, Path(overlay), path, name)
		}
	}
}

// TestResolveFindsOneFolderSpelledTwoWays is the folder install, whose overlay
// sits in config.d, with the overlay named by a relative path and the
// configuration file by an absolute one.
func TestResolveFindsOneFolderSpelledTwoWays(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)

	configPath := filepath.Join(work, "config", "config.yaml")
	overlay := filepath.Join("config", "config.d", "resources.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(EarlierPath(configPath)), 0o700))
	require.NoError(t, os.WriteFile(EarlierPath(configPath), []byte("not a pairing"), fileMode))

	path, err := Resolve(configPath, overlay)
	require.NoError(t, err)
	require.Equal(t, Path(overlay), path)
}
