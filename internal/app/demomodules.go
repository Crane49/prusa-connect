package app

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// The modules the demo offers on its extensions page.
//
// `make demo` runs scripts/pack-example-modules.sh first, which builds both
// examples into one release folder each under the directory demoExamplesDir
// names. Then it runs scripts/pack-demo-extensions.sh, which does the same for
// the first-party extensions, best effort, under its extensions folder. The
// demo names every folder it finds in core.modules_local, so somebody can
// install a module and watch it appear without a network, a GitHub release or
// a shell.
//
// The same script packs the releases the spools gate installs, so what a
// person installs here is the layout that gate proves.

// demoExamplesDir is where the demo looks for the built example releases. It
// is relative to the directory the demo was started from, which is the
// repository root under `make demo`.
//
// It is not bin/demo, because `make demo` builds its binary there and on Linux,
// where an executable carries no suffix, the binary and the directory would be
// one path.
const demoExamplesDir = "bin/demo-modules"

// demoExample is one example module this repository ships, as the index names
// it.
//
// The module path is here and not in the manifest. The namespace of every
// address of a module comes from the path the operator installed, so a
// manifest that named a path would claim an address it does not own.
// Everything else on the card, the version, the kind, the sentence and the
// author, is read back out of the manifest inside the archive, so a row can
// never say what the release does not.
type demoExample struct {
	// module is the module path an install names.
	module string
	// name is the manifest's name, which is also the name of its release folder.
	name string
}

// demoExamples are the two example modules `make demo` builds and offers.
//
//nolint:gochecknoglobals // a fixed list of what this repository ships, read-only.
var demoExamples = []demoExample{
	{
		module: "github.com/prusa3d/prusa-connect/examples/spools",
		name:   "spools",
	},
	{
		module: "github.com/prusa3d/prusa-connect/examples/nozzle-watch",
		name:   "nozzle-watch",
	},
}

// demoExtensionPrefix is the module path every first-party extension is
// offered under, followed by its folder name. The server image documents the
// same paths, and extensions/fleet-stats/install.sh names the same one.
const demoExtensionPrefix = "github.com/prusa3d/"

// demoExtensionsFolder is the folder under examplesDir where
// scripts/pack-demo-extensions.sh lays out the first-party extensions.
const demoExtensionsFolder = "extensions"

// demoFleetStats is the statistics extension. Its bundle alone, built in
// place under extensionsDir, is still offered when no release of it was
// packed, as `make fleet-stats-ui-build` leaves it.
const demoFleetStats = demoExtensionPrefix + "fleet-stats"

// demoLocalModules is the release folder of every demo module that was built:
// the example folders scripts/pack-example-modules.sh leaves in examplesDir,
// the extension folders scripts/pack-demo-extensions.sh leaves under its
// extensions folder, and fleet-stats' ui/dist under extensionsDir when no
// release of it was packed.
//
// The extensions are found by a walk and not by a list, because they are
// released apart from the core and the core does not name them.
func demoLocalModules(examplesDir, extensionsDir string) map[string]string {
	local := map[string]string{}

	for _, example := range demoExamples {
		addRelease(local, example.module, filepath.Join(examplesDir, example.name))
	}

	packed := filepath.Join(examplesDir, demoExtensionsFolder)
	if entries, err := os.ReadDir(packed); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				addRelease(local, demoExtensionPrefix+entry.Name(), filepath.Join(packed, entry.Name()))
			}
		}
	}

	if _, ok := local[demoFleetStats]; !ok {
		addRelease(local, demoFleetStats, filepath.Join(extensionsDir, "fleet-stats", "ui", "dist"))
	}

	return local
}

// addRelease names dir under module when it holds a manifest. The path is
// absolute because the server's working directory is nobody's to promise.
func addRelease(local map[string]string, module, dir string) {
	if _, err := os.Stat(filepath.Join(dir, manifestName)); err != nil {
		return
	}

	absolute, err := filepath.Abs(dir)
	if err != nil {
		return
	}

	local[module] = filepath.ToSlash(absolute)
}

// demoModulesLocal renders core.modules_local for the demo configuration, or
// nothing when no release was built.
func demoModulesLocal(local map[string]string) string {
	if len(local) == 0 {
		return ""
	}

	lines := []string{"  modules_local:"}
	for _, module := range slices.Sorted(maps.Keys(local)) {
		lines = append(lines, "    "+module+": "+demoPath(local[module]))
	}

	return strings.Join(lines, "\n")
}
