package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
)

// manifestPath is the ConfigMap the published Kubernetes manifest set carries.
// The cluster half of that set cannot be exercised without a cluster; the file
// it hands the process can be, and this is where it is read by the same loader
// the server runs.
const manifestPath = "deploy/kubernetes/configmap.yaml"

// manifestKey is the key inside it that becomes the file.
const manifestKey = "config.yaml"

// TestTheKubernetesConfigMapIsAConfigurationThisLoaderAccepts runs the shipped
// manifest's configuration through Load and Validate. The core decodes its own
// block with unknown keys refused, so a key renamed in Go and left behind in the
// manifest stops a tenant at startup, and nothing else in the tree would see it.
func TestTheKubernetesConfigMapIsAConfigurationThisLoaderAccepts(t *testing.T) {
	t.Parallel()

	path := manifestAsFile(t)

	core, err := config.Load(path)
	require.NoError(t, err, "the manifest's %s is not a configuration this loader reads", manifestKey)
	require.NoError(t, core.Validate(), "the manifest's %s does not validate", manifestKey)
}

// TestTheKubernetesConfigMapWritesOnlyWhereAVolumeIsMounted holds the failure
// that stops a tenant under a read-only root filesystem. Both write paths derive
// from the directory holding the configuration file when their keys are unset,
// and in this deployment that directory is the ConfigMap mount, which is
// read-only. The core then stops with `mkdir config/config.d: read-only file
// system`.
func TestTheKubernetesConfigMapWritesOnlyWhereAVolumeIsMounted(t *testing.T) {
	t.Parallel()

	path := manifestAsFile(t)

	core, err := config.Load(path)
	require.NoError(t, err)

	mounted := filepath.ToSlash(filepath.Dir(path))

	for key, written := range map[string]string{
		"core.overlay":        core.Overlay,
		"core.authz.database": core.Authz.Database,
	} {
		slashed := filepath.ToSlash(written)

		require.True(t, strings.HasPrefix(slashed, "/"),
			"%s is %q. A relative path resolves from the container's working directory, "+
				"and a Deployment mounts its volumes at absolute paths", key, written)

		require.NotEqual(t, mounted, filepath.ToSlash(filepath.Dir(slashed)),
			"%s is %q, beside the configuration file. The ConfigMap volume is mounted "+
				"read-only, so the core stops at startup trying to create it", key, written)
	}
}

// TestTheKubernetesConfigMapDeclaresAResourceThatLoads reads the same file
// through the resource loader. A declaration that fails there is not refused at
// startup: it is dropped into Failures, and the instance simply never runs.
func TestTheKubernetesConfigMapDeclaresAResourceThatLoads(t *testing.T) {
	t.Parallel()

	document, err := resource.Load(resource.Paths{File: manifestAsFile(t)})
	require.NoError(t, err)
	require.Empty(t, document.Failures, "the manifest declares a resource that will not load")
	require.NotEmpty(t, document.Instances,
		"the manifest declares no resource instance, so this gate compares nothing")
}

// TestAnUnsetWritePathLandsBesideTheConfigurationFile is the positive control
// for the check above. It proves the assertion measures something: with the two
// keys left out, both paths derive into the directory the file sits in, which in
// the Deployment is the read-only mount.
func TestAnUnsetWritePathLandsBesideTheConfigurationFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	require.NoError(t, os.WriteFile(path, []byte("core:\n  principal: operator\n"), 0o600))

	core, err := config.Load(path)
	require.NoError(t, err)

	for key, written := range map[string]string{
		"core.overlay":        core.Overlay,
		"core.authz.database": core.Authz.Database,
	} {
		require.Equal(t, filepath.ToSlash(dir), filepath.ToSlash(filepath.Dir(filepath.Dir(written))),
			"an unset %s no longer derives from the configuration file's directory, so the "+
				"check that the manifest sets it is measuring nothing", key)
	}
}

// manifestAsFile is the manifest's configuration written to a file, which is the
// only shape the loader reads. It fails the run when the key is absent, so a
// renamed key cannot leave these tests passing over an empty string.
func manifestAsFile(t *testing.T) string {
	t.Helper()

	var manifest struct {
		Data map[string]string `yaml:"data"`
	}

	content, err := os.ReadFile(filepath.Join(repositoryRoot(t), filepath.FromSlash(manifestPath)))
	require.NoError(t, err, "reading %s", manifestPath)
	require.NoError(t, yaml.Unmarshal(content, &manifest), "%s does not parse as yaml", manifestPath)

	body := manifest.Data[manifestKey]
	require.NotEmpty(t, body, "%s carries no %s, so this gate reads nothing", manifestPath, manifestKey)

	path := filepath.Join(t.TempDir(), manifestKey)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	return path
}

// repositoryRoot is the directory holding go.mod, found by walking up from the
// package under test.
func repositoryRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	require.NoError(t, err)

	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "no go.mod above the working directory")

		dir = parent
	}
}
