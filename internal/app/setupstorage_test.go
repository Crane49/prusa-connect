package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// TestAFolderIsCheckedWithoutBeingCreated holds rule 35 of ADR-20261015-ejz: a folder
// that is missing is judged by the folder above it, and the check leaves
// nothing behind.
func TestAFolderIsCheckedWithoutBeingCreated(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	there := checkStorage(dir)
	require.True(t, there.Exists)
	require.True(t, there.Writable, there.Detail)

	missing := filepath.Join(dir, "uploads", "prints")
	check := checkStorage(missing)
	require.False(t, check.Exists)
	require.True(t, check.Writable, check.Detail)
	require.Equal(t, missing, check.Root)

	_, err := os.Stat(missing)
	require.ErrorIs(t, err, os.ErrNotExist, "the check created the folder")

	left, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, left, "the write probe was left behind")
}

// TestAFileIsNotAFolder refuses a path that names a file.
func TestAFileIsNotAFolder(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "storage")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))

	check := checkStorage(file)
	require.False(t, check.Writable)
	require.Equal(t, storageNotAFolder, check.Problem)
}

// TestAReadOnlyFolderIsRefused is the case the wizard has to catch before the
// confirm: the folder is there and this process may not write in it, so the
// mode bits alone would not tell.
func TestAReadOnlyFolderIsRefused(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a read-only mode does not stop this process here")
	}

	dir := filepath.Join(t.TempDir(), "locked")
	require.NoError(t, os.Mkdir(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	check := checkStorage(dir)
	require.True(t, check.Exists)
	require.False(t, check.Writable)
	require.Equal(t, storageNotWritable, check.Problem)

	under := checkStorage(filepath.Join(dir, "uploads"))
	require.False(t, under.Writable, "a folder under a read-only one was judged writable")

	_, err := storageRootOf(dir)
	require.Error(t, err, "the confirm took a folder it cannot write")
}

// TestTheBannerNamesWhereToOpenTheWizard is what a person who started a
// downloaded binary reads: the address on this machine first, then this
// machine on the network, and https once the API is encrypted.
func TestTheBannerNamesWhereToOpenTheWizard(t *testing.T) {
	t.Parallel()

	everywhere := browserAddresses(config.Listener{Addr: ":8080"})
	require.NotEmpty(t, everywhere)
	require.Equal(t, "http://localhost:8080", everywhere[0])
	require.LessOrEqual(t, len(everywhere), maxAnnouncedAddresses+1)

	bound := browserAddresses(config.Listener{
		Addr: "192.168.1.10:9090",
		TLS:  config.TLS{Certificate: "/c.pem", Key: "/k.pem"},
	})
	require.Equal(t, []string{"https://192.168.1.10:9090"}, bound)
}
