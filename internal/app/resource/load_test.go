package resource_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// twoGoodOneBad declares two instances that load and one that does not, so a
// test cannot pass because the file held a single instance either way.
func twoGoodOneBad(root string) string {
	return `
resources:
  storage:
    local:
      type: filesystem
      enabled: true
      config:
        root: ` + root + `
  sink:
    audit:
      type: webhook
      enabled: true
  history:
    front:
      type: sqlite
      typo: yes
`
}

// storageRoot is a directory of the test's own. A relative root in a fixture is
// resolved against the working directory, which for a test is the source tree
// it is compiled from.
func storageRoot(t *testing.T) string {
	t.Helper()

	return filepath.ToSlash(t.TempDir())
}

// TestOneInstanceThatWillNotLoadFailsOnlyItself is the rule a bad block has to
// obey: a declaration the loader refuses is a failed instance, not a dead
// server. The whole-document form of this took a valid
// storage block down with an unrelated driver block and returned no instances
// at all.
func TestOneInstanceThatWillNotLoadFailsOnlyItself(t *testing.T) {
	t.Parallel()

	doc, err := load(t, twoGoodOneBad(storageRoot(t)))
	require.NoError(t, err, "one bad instance took the whole document with it")

	require.Equal(t, []ref.Ref{
		refOf(t, ref.KindStorage, "local"),
		refOf(t, ref.KindSink, "audit"),
	}, refsOf(doc), "an instance that loads was dropped because another one did not")

	require.Len(t, doc.Failures, 1)
	require.Equal(t, "resources.history.front", doc.Failures[0].Key)
	require.ErrorIs(t, doc.Failures[0].Err, resource.ErrConfigInvalid)
	require.Contains(t, doc.Failures[0].Err.Error(), "typo")
}

// TestAnInstalledServiceInstanceLoads is the regression for the half-landed
// vocabulary. internal/app/config/install reads runtime and service out of the
// same block, and this loader refuses a key it does not know, so a config using
// them parses for the build-tag detector and kills the running binary.
func TestAnInstalledServiceInstanceLoads(t *testing.T) {
	t.Parallel()

	doc, err := load(t, `
resources:
  storage:
    local:
      type: filesystem
      config:
        root: `+storageRoot(t)+`
    front:
      type: s3
      runtime: service
      service:
        path: /opt/pcl/plugins
        checksum: sha256:`+repeated("a", 64)+`
        contract_major: 1
`)
	require.NoError(t, err)

	require.Equal(t, []ref.Ref{
		refOf(t, ref.KindStorage, "front"),
		refOf(t, ref.KindStorage, "local"),
	}, refsOf(doc))
	require.Empty(t, doc.Failures)

	builtin, ok := doc.Find(refOf(t, ref.KindStorage, "local"))
	require.True(t, ok)
	require.Equal(t, install.RuntimeBuiltin, builtin.Spec.Runtime,
		"an instance with no runtime key is not the default runtime")
	require.Nil(t, builtin.Spec.Service)

	service, ok := doc.Find(refOf(t, ref.KindStorage, "front"))
	require.True(t, ok)
	require.Equal(t, install.RuntimeService, service.Spec.Runtime)
	require.NotNil(t, service.Spec.Service)
	require.Equal(t, "/opt/pcl/plugins", service.Spec.Service.Path)
	require.Equal(t, 1, service.Spec.Service.ContractMajor)
}

// TestAServiceBlockTheInstallRulesRefuseFailsThatInstanceAlone keeps one owner
// of the install vocabulary. The rules live in internal/app/config/install and
// this loader consults them, so a checksum the launcher would refuse is refused
// at load rather than at the first launch, and it takes nothing else with it.
func TestAServiceBlockTheInstallRulesRefuseFailsThatInstanceAlone(t *testing.T) {
	t.Parallel()

	doc, err := load(t, `
resources:
  storage:
    local:
      type: filesystem
      config:
        root: `+storageRoot(t)+`
    front:
      type: s3
      runtime: service
      service:
        path: /opt/pcl/plugins
        checksum: not-a-digest
        contract_major: 1
`)
	require.NoError(t, err)

	require.Equal(t, []ref.Ref{refOf(t, ref.KindStorage, "local")}, refsOf(doc))
	require.Len(t, doc.Failures, 1)
	require.Equal(t, "resources.storage.front", doc.Failures[0].Key)
	require.Contains(t, doc.Failures[0].Err.Error(), "checksum")
}

// TestTheRuntimeReachesTheDigest is what tells the supervision plane that an
// instance changed. Two declarations differing only in how the type is reached
// are two different instances to run, and a digest that missed it would report
// them as the same and never restart anything.
func TestTheRuntimeReachesTheDigest(t *testing.T) {
	t.Parallel()

	compiled := resource.Spec{Type: "bambu", Enabled: true, Runtime: install.RuntimeBuiltin}

	launched := compiled
	launched.Runtime = install.RuntimeService
	launched.Service = &install.Service{
		Path: "/opt/pcl/plugins", Checksum: "sha256:" + repeated("a", 64), ContractMajor: 1,
	}

	require.False(t, compiled.Equal(launched))
	require.NotEqual(t, compiled.Digest(), launched.Digest())

	// An absent runtime key is the default one, so a file that never mentioned
	// runtime keeps the digest it had and no instance's generation moves for an
	// upgrade that changed nothing.
	unwritten := resource.Spec{Type: "bambu", Enabled: true}
	require.True(t, unwritten.Equal(compiled))
	require.Equal(t, unwritten.Digest(), compiled.Digest())
}

// load reads one operator file with no overlay beside it.
func load(t *testing.T, content string) (resource.Document, error) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return resource.Load(resource.Paths{File: path, Overlay: filepath.Join(dir, "resources.json")})
}

func refsOf(doc resource.Document) []ref.Ref {
	out := make([]ref.Ref, 0, len(doc.Instances))
	for _, declared := range doc.Instances {
		out = append(out, declared.Ref)
	}

	return out
}

func refOf(t *testing.T, kind ref.Kind, name string) ref.Ref {
	t.Helper()

	instance, err := ref.New(kind, name)
	require.NoError(t, err)

	return instance
}

func repeated(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for range n {
		out = append(out, s...)
	}

	return string(out)
}

func TestAnInstanceMayHideContributionsInTheOperatorsFile(t *testing.T) {
	t.Parallel()

	doc, err := load(t, `
resources:
  storage:
    local:
      type: filesystem
      hidden_contributions: [device_card/fleet-stats]
      config:
        root: `+storageRoot(t)+`
`)
	require.NoError(t, err)
	require.Empty(t, doc.Failures)

	found, ok := doc.Find(refOf(t, ref.KindStorage, "local"))
	require.True(t, ok)
	require.Equal(t, []string{"device_card/fleet-stats"}, found.Spec.HiddenContributions)
}
