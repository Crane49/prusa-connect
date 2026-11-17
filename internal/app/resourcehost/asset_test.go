package resourcehost_test

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/extension/wirehost"
	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// assetType is the qualified type the bundle is installed under; the
// namespace its files are served under is derived from it.
const assetType = "com.github.aux.acme.spools/spools"

// aBundle writes a built bundle into a directory of this test's own and
// records what it hashes to, the way an install does.
func aBundle(t *testing.T) (string, string) {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "ui")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "assets"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.js"), []byte("import './assets/chunk.js'\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "assets", "chunk.js"), []byte("export const a = 1\n"), 0o600))

	digest, err := wirehost.DigestDir(dir)
	require.NoError(t, err)

	return dir, string(digest)
}

// declareAsset writes the one block an operator writes for a module that
// runs no process: the directory and the digest recorded over it.
func (w *wired) declareAsset(dir, checksum string) {
	w.t.Helper()

	w.writeFile(fmt.Sprintf(`
resources:
  asset:
    spools:
      type: %s
      enabled: true
      asset:
        path: %s
        checksum: %s
`, assetType, filepath.ToSlash(dir), checksum))

	require.NoError(w.t, w.store.Reload(w.t.Context()))
}

// theAsset is the instance every case declares.
func theAsset(t *testing.T) ref.Ref {
	t.Helper()

	instance, err := ref.New(ref.KindAsset, "spools")
	require.NoError(t, err)

	return instance
}

// bundleOf reads the bundle back the way the composition root does: by kind,
// through the supervised instance, and never through a pointer kept from an
// earlier start.
func bundleOf(w *wired, instance ref.Ref) (string, httpapi.ModuleUI, bool) {
	runner, ok := w.host.Supervising(ref.KindAsset)[instance]
	if !ok {
		return "", httpapi.ModuleUI{}, false
	}

	return resourcehost.UIOf(runner)
}

// TestAnAssetInstanceServesTheDirectoryAnOperatorDeclared is ADR-20260911-4p6 rule 30
// at the plane: the bundle verifies, the instance reaches ready with no
// process behind it, and the files are read back under the namespace the type
// carries.
func TestAnAssetInstanceServesTheDirectoryAnOperatorDeclared(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	instance := theAsset(t)
	dir, checksum := aBundle(t)

	w.declareAsset(dir, checksum)
	w.waitReady(instance)

	status, ok := w.host.Status(instance)
	require.True(t, ok)
	require.True(t, status.Serving)

	namespace, held, ok := bundleOf(w, instance)
	require.True(t, ok, "a ready asset instance holds no bundle")
	require.Equal(t, "com.github.aux.acme.spools", namespace)
	require.Equal(t, checksum, held.Digest)

	file, err := held.Files.Open("assets/chunk.js")
	require.NoError(t, err)

	body, err := io.ReadAll(file)
	require.NoError(t, file.Close())
	require.NoError(t, err)
	require.Equal(t, "export const a = 1\n", string(body))

	// The host mounts no routes for it: the files are the composition root's
	// to mount, and a module with routes would be a process.
	_, claimed := w.host.Module(namespace)
	require.False(t, claimed, "the host offered an asset instance as a module with routes")

	require.NotContains(t, w.logs.String(), "is serving on stdio", "a process was launched for a directory")
}

// TestAnAssetInstanceWhoseDirectoryChangedIsNeverServed is the digest gate
// from the operator's end. A file moved after the checksum was recorded, so
// the instance latches failed with the code a swapped binary carries, nothing
// retries, and no bundle is ever handed to the mount.
func TestAnAssetInstanceWhoseDirectoryChangedIsNeverServed(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	instance := theAsset(t)
	dir, checksum := aBundle(t)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.js"), []byte("alert(1)\n"), 0o600))

	w.declareAsset(dir, checksum)

	status := w.waitState(instance, condition.StateFailed())
	require.True(t, says(status, "RESOURCE_PLUGIN_CHECKSUM"), conditionsOf(&status))
	require.False(t, status.Serving)

	_, _, ok := bundleOf(w, instance)
	require.False(t, ok, "a bundle that failed its digest was handed to the mount")
}

// TestAnAssetInstanceWithNoDirectoryRetriesRatherThanLatching is the one
// refusal waiting can fix: an operator who has not installed the bundle yet
// gets an instance that comes up when they do.
func TestAnAssetInstanceWithNoDirectoryRetriesRatherThanLatching(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	instance := theAsset(t)
	_, checksum := aBundle(t)

	w.declareAsset(filepath.Join(t.TempDir(), "not-installed"), checksum)

	status := w.waitState(instance, condition.StateRetrying())
	require.True(t, says(status, "RESOURCE_PLUGIN_MISSING"), conditionsOf(&status))
}
