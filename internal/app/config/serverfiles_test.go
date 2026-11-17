package config_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
)

// serverBlocks are the top-level blocks a server configuration is made of. A
// YAML file whose top level names one of them and nothing else is a file this
// server is meant to read, and every other YAML file in the tree is something
// else: a Kubernetes object, a compose build, a module manifest.
//
// There is no marker to look for. The server is handed a path and reads it, so
// the shape of the document is the only thing that tells its own files apart.
//
//nolint:gochecknoglobals // the closed set of blocks, read by the sweep below.
var serverBlocks = []string{"core", "persistence", "resources"}

// skippedTrees are directories the sweep does not enter: dependencies, build
// output and the files a test writes.
//
//nolint:gochecknoglobals // the closed set of directories to skip.
var skippedTrees = []string{".git", "node_modules", "dist", "out", "build", "coverage"}

// deliberatelyInvalid are the files that are meant not to load, each with the
// reason. A file here is still read: the sweep fails when one of them loads,
// because a fixture that stopped being invalid is a test that stopped testing.
//
//nolint:gochecknoglobals // the list of known-bad fixtures, read by the sweep below.
var deliberatelyInvalid = map[string]string{}

// TestEveryServerConfigurationInTheTreeLoads reads every configuration file in
// the repository through the loader the server runs, with unknown keys refused.
//
// A key renamed in Go and left behind in a profile, an example or a container's
// file stops that deployment at startup, and nothing else in the tree would
// notice.
func TestEveryServerConfigurationInTheTreeLoads(t *testing.T) {
	t.Parallel()

	files := serverConfigurations(t)
	require.NotEmpty(t, files, "the sweep found no server configuration, so it compares nothing")

	for _, path := range files {
		t.Run(relative(t, path), func(t *testing.T) {
			t.Parallel()

			reason, invalid := deliberatelyInvalid[relative(t, path)]

			core, err := config.Load(path)
			if invalid {
				require.Error(t, err, "this file is listed as deliberately invalid (%s) and it loads", reason)

				return
			}

			require.NoError(t, err)
			require.NotEmpty(t, core.Principal, "the loader filled no defaults, so nothing was read")
		})
	}
}

// TestEveryServerConfigurationDeclaresResourcesThatLoad reads the same files
// through the resource loader. A declaration that fails there does not stop the
// server: it is dropped, and the instance never runs.
func TestEveryServerConfigurationDeclaresResourcesThatLoad(t *testing.T) {
	t.Parallel()

	for _, path := range serverConfigurations(t) {
		if _, invalid := deliberatelyInvalid[relative(t, path)]; invalid {
			continue
		}

		t.Run(relative(t, path), func(t *testing.T) {
			t.Parallel()

			document, err := resource.Load(resource.Paths{File: path})
			require.NoError(t, err)

			for _, failed := range document.Failures {
				require.NoError(t, failed.Err, "%s will not load", failed.Key)
			}
		})
	}
}

// TestTheShippedConfigurationValidates holds the default file and the profiles
// that name no certificate to what the server refuses to start on. The TLS
// profile is left out: validation opens the certificate files, and those are
// written by `make certificate-all` into a deployment's own directory.
func TestTheShippedConfigurationValidates(t *testing.T) {
	t.Parallel()

	for _, path := range serverConfigurations(t) {
		if _, invalid := deliberatelyInvalid[relative(t, path)]; invalid {
			continue
		}

		t.Run(relative(t, path), func(t *testing.T) {
			t.Parallel()

			core, err := config.Load(path)
			require.NoError(t, err)

			if core.API.TLS.Enabled() {
				t.Skip("this profile names a certificate, which validation opens")
			}

			require.NoError(t, core.Validate())
		})
	}
}

// TestTheSmallestFileStartsAServer is the file the page offers a first-time
// reader. It has to be a configuration the loader accepts and validation passes,
// or the first thing somebody copies does not work.
func TestTheSmallestFileStartsAServer(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(smallestFile), 0o600))

	core, err := config.Load(path)
	require.NoError(t, err)
	require.NoError(t, core.Validate())

	document, err := resource.Load(resource.Paths{File: path})
	require.NoError(t, err)
	require.Empty(t, document.Failures)
}

// smallestFile is the file printed on docs/src/content/docs/config.md as the
// least a deployment needs. Every other key has a working default.
const smallestFile = `core:
  api:
    addr: ":8080"

resources:
  storage:
    local:
      type: filesystem
      config:
        root: ./storage
`

// serverConfigurations is every YAML file in the repository whose top level
// names only the blocks a server configuration is made of.
func serverConfigurations(t *testing.T) []string {
	t.Helper()

	root := repositoryRoot(t)

	var found []string

	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			if slices.Contains(skippedTrees, entry.Name()) {
				return fs.SkipDir
			}

			return nil
		}

		if extension := filepath.Ext(entry.Name()); extension != ".yaml" && extension != ".yml" {
			return nil
		}

		if isServerConfiguration(t, path) {
			found = append(found, path)
		}

		return nil
	}))

	slices.Sort(found)

	return found
}

// isServerConfiguration reports whether a file's top level names the server's
// own blocks and nothing else. A document that names one other key is somebody
// else's format and is left alone.
func isServerConfiguration(t *testing.T, path string) bool {
	t.Helper()

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	var document map[string]yaml.Node
	if yaml.Unmarshal(content, &document) != nil || len(document) == 0 {
		return false
	}

	for name := range document {
		if !slices.Contains(serverBlocks, name) {
			return false
		}
	}

	return true
}

// relative is a path as the repository names it, for a subtest name and for the
// list of deliberately invalid files.
func relative(t *testing.T, path string) string {
	t.Helper()

	rel, err := filepath.Rel(repositoryRoot(t), path)
	require.NoError(t, err)

	return filepath.ToSlash(rel)
}

// TestEveryDeliberatelyInvalidFileIsInTheTree keeps that list honest. A name in
// it that is not in the tree is a fixture somebody deleted.
func TestEveryDeliberatelyInvalidFileIsInTheTree(t *testing.T) {
	t.Parallel()

	found := map[string]struct{}{}
	for _, path := range serverConfigurations(t) {
		found[relative(t, path)] = struct{}{}
	}

	for name, reason := range deliberatelyInvalid {
		_, present := found[name]
		require.True(t, present, "%s is listed as deliberately invalid (%s) and is not in the tree", name, reason)
	}
}
