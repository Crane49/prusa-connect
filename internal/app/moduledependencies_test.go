package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/moduledeps"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The dependencies of ADR-20261027-e6k on the paths an install takes: the
// plan, the command and the API.

// readerManifest is a module that only draws, which other modules require.
func readerManifest(version string) string {
	return strings.Join([]string{
		"name: reader", "kind: asset", "license: MIT", "version: " + version,
		"ui:", "  entry: ui/index.js", `  library: ">=0.1.0 <1.0.0"`, "",
	}, "\n")
}

// viewerManifest requires the reader at a range.
func viewerManifest(reader string) string {
	return strings.Join([]string{
		"name: viewer", "kind: asset", "license: MIT", "version: 1.0.0",
		"require:", `  com.github.aux.acme.reader: "` + reader + `"`,
		"ui:", "  entry: ui/index.js", `  library: ">=0.1.0 <1.0.0"`, "",
	}, "\n")
}

const (
	readerPath = "github.com/acme/reader"
	viewerPath = "github.com/acme/viewer"
)

// offerReader writes an index file in the modules directory that offers the
// reader from a release directory on this server.
func offerReader(t *testing.T, modules string, versions ...string) {
	t.Helper()

	rows := make([]moduleIndexRow, 0, len(versions))
	for _, version := range versions {
		rows = append(rows, moduleIndexRow{
			Module: readerPath, Name: "reader", Version: version, Kind: "asset",
			Source: aRelease(t, readerManifest(version), false),
		})
	}

	document, err := json.Marshal(moduleIndexFile{Modules: rows})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(modules, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(modules, moduleIndexName), document, 0o600))
}

// moduleRun runs one module command and answers what it printed and the error.
func moduleRun(t *testing.T, args ...string) (string, error) {
	t.Helper()

	var out bytes.Buffer

	err := Module(t.Context(), &out, args)

	return out.String(), err
}

func TestAnInstallAddsTheModulesItRequires(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	offerReader(t, core.Modules, "1.2.0")

	said, err := moduleRun(t, addVerb, "-config", configPath, "-from", aRelease(t, viewerManifest("^1.0"), false),
		"-approve", viewerPath+"@1.0.0")
	require.NoError(t, err)
	require.Contains(t, said, "added       "+readerPath+"@1.2.0, which it requires")

	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
	require.NoError(t, err)

	reader, found := doc.Find(assetNamed(t, "reader"))
	require.True(t, found, "the reader was not added")
	require.True(t, reader.Spec.Enabled)
	require.True(t, reader.Spec.Module.Pulled)
	require.Equal(t, readerPath, reader.Spec.Module.Path)

	viewer, found := doc.Find(assetNamed(t, "viewer"))
	require.True(t, found)
	require.False(t, viewer.Spec.Module.Pulled)

	// The reader is required, so it cannot go first.
	_, err = moduleRun(t, removeVerb, "-config", configPath, readerPath)
	require.True(t, fault.HasCode(err, httpfault.CodeModuleRequiredBy()), "want MODULE_REQUIRED_BY, got %v", err)
}

func TestAnInstallANewerRangeNoReleaseMeetsIsRefused(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	offerReader(t, core.Modules, "1.2.0")

	_, err := moduleRun(t, addVerb, "-config", configPath, "-from", aRelease(t, viewerManifest("^2.0"), false),
		"-approve", viewerPath+"@1.0.0")
	require.True(t, fault.HasCode(err, httpfault.CodeModuleDependencyConflict()),
		"want MODULE_DEPENDENCY_CONFLICT, got %v", err)

	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
	require.NoError(t, err)
	require.Empty(t, doc.Instances, "a refused install wrote something")
}

func TestUnusedModulesGoOnlyWhenAsked(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	offerReader(t, core.Modules, "1.2.0")

	_, err := moduleRun(t, addVerb, "-config", configPath, "-from", aRelease(t, viewerManifest("^1.0"), false),
		"-approve", viewerPath+"@1.0.0")
	require.NoError(t, err)

	said, err := moduleRun(t, removeVerb, "-config", configPath, viewerPath)
	require.NoError(t, err)
	require.Contains(t, said, "asset:reader were installed for it and nothing else requires them")

	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
	require.NoError(t, err)

	_, found := doc.Find(assetNamed(t, "reader"))
	require.True(t, found, "an unused module went without being asked")

	// Installed again and removed with the ask, the reader goes with it.
	_, err = moduleRun(t, addVerb, "-config", configPath, "-from", aRelease(t, viewerManifest("^1.0"), false),
		"-approve", viewerPath+"@1.0.0")
	require.NoError(t, err)

	said, err = moduleRun(t, removeVerb, "-config", configPath, "-with-unused", viewerPath)
	require.NoError(t, err)
	require.Contains(t, said, "removed asset:reader, which was installed for it and nothing else requires")

	doc, err = resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
	require.NoError(t, err)
	require.Empty(t, doc.Instances)
}

func TestAPlanThatMovedIsRefused(t *testing.T) {
	t.Parallel()

	installer, _ := theInstaller(t, true)
	offerReader(t, installer.core.Modules, "1.2.0")

	viewer := aRelease(t, viewerManifest("^1.0"), false)

	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: viewerPath, Version: "1.0.0", From: viewer, Approve: true,
	})
	require.True(t, fault.HasCode(err, httpfault.CodeModulePlanChanged()), "want MODULE_PLAN_CHANGED, got %v", err)
	require.Contains(t, err.Error(), readerPath+"@1.2.0")

	installed, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: viewerPath, Version: "1.0.0", From: viewer, Approve: true,
		Dependencies: []httpapi.ModuleReleaseRef{
			{Module: readerPath, Version: "1.2.0", Expect: &httpapi.ModuleRequests{}},
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{readerPath + "@1.2.0"}, installed.Dependencies)

	removal, err := installer.Removal(t.Context(), "user:operator", "com.github.aux.acme.reader")
	require.NoError(t, err)
	require.Len(t, removal.RequiredBy, 1)

	removal, err = installer.Removal(t.Context(), "user:operator", "com.github.aux.acme.viewer")
	require.NoError(t, err)
	require.Len(t, removal.Unused, 1)
	require.Equal(t, "asset:reader", removal.Unused[0].Instance)
}

func assetNamed(t *testing.T, name string) ref.Ref {
	t.Helper()

	instance, err := ref.New(ref.KindAsset, name)
	require.NoError(t, err)

	return instance
}

func TestTheLockReproducesTheInstalledModules(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	offerReader(t, core.Modules, "1.2.0", "1.3.0")

	_, err := moduleRun(t, addVerb, "-config", configPath, "-from", aRelease(t, viewerManifest("^1.0"), false),
		"-approve", viewerPath+"@^1.0")
	require.NoError(t, err)

	lock, err := moduledeps.ReadLock(moduledeps.LockPath(core.Overlay))
	require.NoError(t, err)

	reader, found := lock.Locked("com.github.aux.acme.reader")
	require.True(t, found, "the add wrote no lock")
	require.Equal(t, "1.3.0", reader.Version)
	require.True(t, reader.Pulled)

	viewer, _ := lock.Locked("com.github.aux.acme.viewer")
	require.Equal(t, "^1.0", viewer.Constraint)

	// Installed from the lock on a server with nothing, the same releases
	// come back.
	other, otherCore := installConfig(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(otherCore.Overlay), 0o750))
	require.NoError(t, moduledeps.WriteLock(moduledeps.LockPath(otherCore.Overlay), lock))

	said, err := moduleRun(t, installVerb, "-config", other, "-approve")
	require.NoError(t, err)
	require.Contains(t, said, "installed com.github.aux.acme.reader 1.3.0 from the lock")

	doc, err := resource.Load(resource.Paths{File: other, Overlay: otherCore.Overlay})
	require.NoError(t, err)
	require.Len(t, doc.Instances, 2)

	// why names the viewer, and outdated reads the index.
	said, err = moduleRun(t, whyVerb, "-config", configPath, readerPath)
	require.NoError(t, err)
	require.Contains(t, said, "com.github.aux.acme.viewer 1.0.0 requires com.github.aux.acme.reader ^1.0")

	said, err = moduleRun(t, whyNotVerb, "-config", configPath, readerPath, "1.2.0")
	require.NoError(t, err)
	require.Contains(t, said, "nothing refuses")
}

func TestAnUpdateTakesTheNewestReleaseAndWritesTheLock(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	offerReader(t, core.Modules, "1.2.0")

	_, err := moduleRun(t, addVerb, "-config", configPath, "-from", aRelease(t, viewerManifest("^1.0"), false),
		"-approve", viewerPath+"@^1.0")
	require.NoError(t, err)

	offerReader(t, core.Modules, "1.2.0", "1.4.0", "2.0.0")

	said, err := moduleRun(t, outdatedVerb, "-config", configPath)
	require.NoError(t, err)
	require.Contains(t, said, "com.github.aux.acme.reader\t1.2.0\tallowed 1.4.0\tlatest 2.0.0\tupdate-possible")

	said, err = moduleRun(t, updateVerb, "-config", configPath, "-approve", readerPath)
	require.NoError(t, err)
	require.Contains(t, said, "updated com.github.aux.acme.reader from 1.2.0 to 1.4.0")

	lock, err := moduledeps.ReadLock(moduledeps.LockPath(core.Overlay))
	require.NoError(t, err)

	reader, _ := lock.Locked("com.github.aux.acme.reader")
	require.Equal(t, "1.4.0", reader.Version)
}
