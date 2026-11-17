package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/betaportal"
)

// TestTheCoreStartsWithItsConfigurationFolderReadOnly is a device whose
// operating system owns the configuration folder, and a cluster that mounts it
// from a ConfigMap. Both point core.overlay and core.authz.database outside
// that folder, because the database defaults to it. The test also points
// core.modules outside, as such a device does. The Kubernetes manifest leaves
// it unset, and the start does not need it. The Beta Portal pairing follows
// core.overlay, and nothing goes beside the configuration file.
func TestTheCoreStartsWithItsConfigurationFolderReadOnly(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a read-only mode does not stop this process here")
	}

	root := t.TempDir()
	etc := filepath.Join(root, "etc")
	state := filepath.Join(root, "state")
	overlay := filepath.Join(state, "resources.json")

	write(t, filepath.Join(etc, "config.yaml"), lines(
		"core:",
		"  log:",
		"    level: error",
		"  overlay: "+slashed(overlay),
		"  authz:",
		"    database: "+slashed(filepath.Join(state, "authz.db")),
		"  modules: "+slashed(filepath.Join(state, "modules")),
	))
	recordOpenSetup(t, etc)

	require.NoError(t, os.Chmod(etc, 0o500))
	t.Cleanup(func() { _ = os.Chmod(etc, 0o700) })

	running := launch(t, etc)
	waitUntilReady(t, running)

	_, err := os.Stat(betaportal.Path(overlay))
	require.NoError(t, err, "the Beta Portal pairing is not beside the overlay")

	_, err = os.Stat(filepath.Join(etc, "config.d"))
	require.ErrorIs(t, err, os.ErrNotExist, "the core wrote beside its configuration file")

	running.stopAndWait(t)
}
