package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTheDemoNamesEveryReleaseFolderItFinds(t *testing.T) {
	t.Parallel()

	examples := t.TempDir()
	spools := filepath.Join(examples, "spools")
	require.NoError(t, os.MkdirAll(spools, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(spools, manifestName), []byte("name: spools\n"), 0o600))
	extensions := t.TempDir() // fleet-stats not built

	local := demoLocalModules(examples, extensions)

	require.Equal(t, map[string]string{
		"github.com/prusa3d/prusa-connect/examples/spools": filepath.ToSlash(spools),
	}, local)
	require.Contains(t, demoModulesLocal(local),
		"    github.com/prusa3d/prusa-connect/examples/spools: ")
	require.Empty(t, demoModulesLocal(nil))
}

func TestTheDemoOffersEveryPackedExtension(t *testing.T) {
	t.Parallel()

	examples := t.TempDir()
	packed := filepath.Join(examples, demoExtensionsFolder)

	for _, name := range []string{"parts", "fleet-stats"} {
		dir := filepath.Join(packed, name)
		require.NoError(t, os.MkdirAll(dir, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, manifestName), []byte("name: "+name+"\n"), 0o600))
	}

	// A folder with no manifest is a build that failed half way, and is not offered.
	require.NoError(t, os.MkdirAll(filepath.Join(packed, "buddy-debug-logs"), 0o750))

	// The bundle built in place is not offered when a packed release of it is.
	extensions := t.TempDir()
	dist := filepath.Join(extensions, "fleet-stats", "ui", "dist")
	require.NoError(t, os.MkdirAll(dist, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dist, manifestName), []byte("name: fleet-stats\n"), 0o600))

	local := demoLocalModules(examples, extensions)

	require.Equal(t, map[string]string{
		"github.com/prusa3d/parts":       filepath.ToSlash(filepath.Join(packed, "parts")),
		"github.com/prusa3d/fleet-stats": filepath.ToSlash(filepath.Join(packed, "fleet-stats")),
	}, local)
}

func TestTheDemoFallsBackToTheFleetStatsBundleBuiltInPlace(t *testing.T) {
	t.Parallel()

	extensions := t.TempDir()
	dist := filepath.Join(extensions, "fleet-stats", "ui", "dist")
	require.NoError(t, os.MkdirAll(dist, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dist, manifestName), []byte("name: fleet-stats\n"), 0o600))

	local := demoLocalModules(t.TempDir(), extensions)

	require.Equal(t, map[string]string{
		"github.com/prusa3d/fleet-stats": filepath.ToSlash(dist),
	}, local)
}
