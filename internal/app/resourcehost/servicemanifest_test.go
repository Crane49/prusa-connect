package resourcehost_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
)

// A module's declaration is the record of what was installed. The release
// directory is staged and thrown away, and a module that runs a process leaves
// no manifest on disk, so the type, the module block and the service block are
// where the module's name, release and plugin contract live.
//
// What that record is for is the artefact answering under it. A second artefact
// arriving under the first one's declaration would serve its routes under a
// namespace nothing verified it for, so the child is asked what it is and held
// to the record before anything of it is mounted.

// declareInstalledModule writes the block `module add` writes for a module with
// a process half, including the module block that carries what the manifest
// said.
func (w *wired) declareInstalledModule(typeName, dir, checksum, version string) {
	w.t.Helper()

	w.writeFile(fmt.Sprintf(`
resources:
  sink:
    example:
      type: %s
      runtime: service
      enabled: true
      module:
        version: %s
      service:
        path: %s
        checksum: %s
        contract_major: 1
`, typeName, version, filepath.ToSlash(dir), checksum))

	require.NoError(w.t, w.store.Reload(w.t.Context()))
}

// installAs copies the built example into a directory of its own under another
// artefact name, which is what an install under another module name produces.
func installAs(t *testing.T, moduleName string) *installed {
	t.Helper()

	source, err := builtPlugin()
	require.NoError(t, err)

	body, err := os.ReadFile(source)
	require.NoError(t, err)

	dir := filepath.Join(t.TempDir(), "plugins")
	require.NoError(t, os.MkdirAll(dir, 0o750))

	path := filepath.Join(dir, artefactName(moduleName))
	require.NoError(t, os.WriteFile(path, body, 0o700))

	return &installed{dir: dir, path: path, checksum: digestOf(body)}
}

// TestAModuleThatIsNotTheOneInstalledIsRefused is the check the record exists
// for. The artefact answers plugin.info with a name that is not the one the
// declaration was written for, so the instance latches failed and nothing of
// the module is mounted.
func TestAModuleThatIsNotTheOneInstalledIsRefused(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	module := install(t)
	instance := theServiceSink(t)

	w.declareInstalledModule(mountedModuleType, module.dir, module.checksum, "1.0.0")

	status := w.waitState(instance, condition.StateFailed())
	require.True(t, says(status, "RESOURCE_MANIFEST_DISAGREES"), conditionsOf(&status))
	require.False(t, status.Serving)

	_, claimed := w.host.Module(mountedNamespace)
	require.True(t, claimed, "the namespace stays claimed while the instance is declared")

	surface, _ := w.host.Module(mountedNamespace)
	require.Empty(t, surface.OpenAPI, "a child the host refused published its routes")
}

// TestAModuleAnsweringUnderTheReleaseInstalledRuns is the same path with the
// record and the artefact agreeing, so a correct module is not refused by the
// check above.
func TestAModuleAnsweringUnderTheReleaseInstalledRuns(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	module := installAs(t, pluginName)
	instance := theServiceSink(t)

	w.declareInstalledModule(mountedNamespace+"/"+pluginName, module.dir, module.checksum, "1.0.0")

	w.waitReady(instance)
}

// TestAModuleInstalledFromAnotherReleaseIsRefused is the second half of the
// record: the name agrees and the release does not, which is a module rolled
// back or forward under a declaration nobody rewrote.
func TestAModuleInstalledFromAnotherReleaseIsRefused(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	module := installAs(t, pluginName)
	instance := theServiceSink(t)

	w.declareInstalledModule(mountedNamespace+"/"+pluginName, module.dir, module.checksum, "2.0.0")

	status := w.waitState(instance, condition.StateFailed())
	require.True(t, says(status, "RESOURCE_MANIFEST_DISAGREES"), conditionsOf(&status))
}
