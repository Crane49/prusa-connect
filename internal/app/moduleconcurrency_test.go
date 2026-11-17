package app

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// Two installs of one module at once take turns: neither copies a release into
// a folder the other is replacing, and the module ends up whole.
func TestTwoInstallsOfOneModuleAtOnceTakeTurns(t *testing.T) {
	t.Parallel()

	market := newFakeMarketplace(t, archiveBytes(t, bothHalvesManifest()), "1.0.0")
	installer, _ := marketInstaller(t, market.server.URL)

	var group sync.WaitGroup
	errs := make([]error, 2)

	for i := range errs {
		group.Go(func() {
			_, errs[i] = installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
				Module: theModulePath, Version: "1.0.0", Approve: true,
			})
		})
	}

	group.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}

	index, err := installer.Index(t.Context(), "user:operator")
	require.NoError(t, err)
	require.Equal(t, "1.0.0", index.Items[0].InstalledVersion)

	entries, err := os.ReadDir(filepath.Join(installer.core.Modules, "spools"))
	require.NoError(t, err)
	require.NotEmpty(t, entries, "the module's folder was left empty by the second install")
}
