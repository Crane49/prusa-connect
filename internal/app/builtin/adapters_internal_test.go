package builtin

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/adapters/storage/local"
	"github.com/prusa3d/prusa-connect/internal/extension/host"
	"github.com/prusa3d/prusa-connect/internal/extension/registry"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// proofStore is the name the storage registration below is declared under. It
// is not "local", so this case reads the bridge rather than the shipping
// adoption of the disk adapter next to it.
const proofStore = "proof-store"

// TestTheStorageBridgeGivesTheAdapterTheOperatorsKeys is the storage half of
// the bridge, against the real adapter: the mapping from an instance's config
// block onto storageapi.ProviderSpec.
func TestTheStorageBridgeGivesTheAdapterTheOperatorsKeys(t *testing.T) {
	t.Parallel()

	dir := filepath.ToSlash(t.TempDir())
	types := storeRegistry(t)

	instance, err := ref.New(ref.KindStorage, "files")
	require.NoError(t, err)

	provider, err := registry.Construct[storageapi.Provider](t.Context(), types, ref.KindStorage, host.Discard(),
		registry.Spec{Ref: instance, Type: proofStore, Config: json.RawMessage(
			`{"path":"` + dir + `/store","name":"Bench disk"}`)})
	require.NoError(t, err)

	require.Equal(t, "files", provider.Name(), "the instance name the operator chose is the provider's id")
	require.Equal(t, "Bench disk", provider.DisplayName())
	require.True(t, provider.Capabilities().Browse)

	info, err := os.Stat(filepath.Join(dir, "store"))
	require.NoError(t, err)
	require.True(t, info.IsDir(), "the adapter was given the path and opened it")
}

// TestAStorageSettingsBlockIsRefusedByTheHost is the other side of the split.
// storageapi.ProviderSpec names the root, the label and the allowlist itself,
// so those keys are the host's and a typo in one is refused here rather than
// reaching the adapter as an absent value.
func TestAStorageSettingsBlockIsRefusedByTheHost(t *testing.T) {
	t.Parallel()

	types := storeRegistry(t)

	cases := map[string]struct {
		config  string
		refused string
	}{
		"a misspelled key is refused":         {`{"paht":"/tmp"}`, "paht"},
		"a path of the wrong type is refused": {`{"path":7}`, "cannot unmarshal"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			instance, refErr := ref.New(ref.KindStorage, "files")
			require.NoError(t, refErr)

			_, buildErr := registry.Construct[storageapi.Provider](t.Context(), types, ref.KindStorage,
				host.Discard(), registry.Spec{Ref: instance, Type: proofStore, Config: json.RawMessage(tc.config)})
			require.ErrorIs(t, buildErr, ErrAdapterConfig)
			require.Contains(t, buildErr.Error(), tc.refused)
		})
	}
}

// TestAnAdapterThisPlaneCannotNameIsRefused is ADR-20260825-rre rule 4 at the seam this
// bridge opens. registry.Declare mints a descriptor for the package that calls
// it, so an adapter registered from another module would be republished as this
// one's. The refusal names both spellings, because the operator's answer is to
// stop expecting the foreign one.
func TestAnAdapterThisPlaneCannotNameIsRefused(t *testing.T) {
	t.Parallel()

	registered := storeRegistration(t)

	_, err := adopt(ref.KindStorage, "com.github.-.acme.disks/"+proofStore, registered.Descriptor(),
		func(_ context.Context, _ host.Handle, _ registry.Spec) (storageapi.Provider, error) {
			return nil, nil //nolint:nilnil // never called: the declaration is refused before construction.
		})

	require.ErrorIs(t, err, ErrForeignNamespace)
	require.Contains(t, err.Error(), "com.github.-.acme.disks/"+proofStore)
	require.Contains(t, err.Error(), "core/"+proofStore)
}

// storeRegistry is a one-type registry over the disk adapter, adopted through
// the bridge under test.
func storeRegistry(t *testing.T) *registry.Registry {
	t.Helper()

	declared, err := adoptStore(storeRegistration(t))
	require.NoError(t, err)

	types, err := registry.NewFrom(declared)
	require.NoError(t, err)

	return types
}

// storeRegistration declares the disk adapter without registering it, so the
// process-wide catalogue is untouched and this test can run beside any other.
func storeRegistration(t *testing.T) storageapi.Registration {
	t.Helper()

	registered, err := storageapi.Declare(proofStore, local.Doc(proofStore, "Proof disk"), local.New)
	require.NoError(t, err)
	require.Equal(t, extension.CoreNamespace, registered.Descriptor().Namespace())

	return registered
}

// A storage that keeps no directory is admitted with no path at all, and a
// disk-backed one still refuses one. The host stopped insisting on a path when
// the storage contract stopped assuming a filesystem: whether a store needs a
// directory is the store's own business, and a host that demanded one would
// admit no object store and no memory store.
//
// The pathless store here is pathlessStore below and not a shipped adapter. The
// shipped one is examples/adapters/storage-memory, outside the core module and
// admitted to a binary by a build configuration rather than by this package
// (ADR-20260901-7t4). That is the right outcome for this case regardless: the subject
// is the bridge from an
// instance's config block onto storageapi.ProviderSpec, and a bridge test that
// can only be written while some shipped adapter happens to be pathless is a
// test of the adapter inventory.
func TestAStorageWithNoDirectoryNeedsNoPath(t *testing.T) {
	t.Parallel()

	pathless, err := storageapi.Declare("proof-memory", pathlessDoc(), newPathlessStore)
	require.NoError(t, err)

	disk, err := adoptStore(storeRegistration(t))
	require.NoError(t, err)

	scratch, err := adoptStore(pathless)
	require.NoError(t, err)

	types, err := registry.NewFrom(disk, scratch)
	require.NoError(t, err)

	instance, err := ref.New(ref.KindStorage, "scratch")
	require.NoError(t, err)

	// The disk-backed store refuses it, and the refusal is the adapter's own
	// rather than the bridge's.
	_, err = registry.Construct[storageapi.Provider](t.Context(), types, ref.KindStorage,
		host.Discard(), registry.Spec{Ref: instance, Type: proofStore, Config: json.RawMessage(`{}`)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "spec.Path is required")

	// A store that keeps its objects somewhere other than a disk is built.
	built, err := registry.Construct[storageapi.Provider](t.Context(), types, ref.KindStorage,
		host.Discard(), registry.Spec{Ref: instance, Type: "proof-memory", Config: json.RawMessage(`{}`)})
	require.NoError(t, err)
	require.Equal(t, "scratch", built.Name())

	caps := storageapi.Probe(built)
	require.True(t, caps.Browse)
	require.True(t, caps.Write)
	require.False(t, caps.LocalFile, "a store with no filesystem offered a file handle")

	t.Logf("observed: %s built with no path, serving %+v", built.Name(), caps)
}

// pathlessDoc is the descriptor the pathless store registers under. Every field
// sdk/extension makes mandatory is filled, because Declare refuses a descriptor
// that is missing one and this file would then fail for the wrong reason.
func pathlessDoc() extension.Doc {
	return extension.Doc{
		Summary: "A storage provider with nowhere on disk to be, for the bridge cases in this package.",
		Description: "It stores nothing and serves nothing. Its whole contribution is to be a " +
			"storageapi.Provider whose factory does not refuse an empty spec.Path, which is what the " +
			"host's storage bridge is being held to here.",
		Version:   "1.0.0",
		Contract:  storageapi.Contract,
		Stability: extension.StabilityStable,
	}
}

// newPathlessStore is the storageapi.ProviderFactory for the pathless store. It
// looks at spec.Path and does not care what is in it, which is the whole point.
func newPathlessStore(spec storageapi.ProviderSpec) (storageapi.Provider, error) {
	return pathlessStore{id: spec.ID}, nil
}

// pathlessStore is a storage provider that needs no directory. It answers every
// browse and write call with ErrInvalidPath: nothing in this package's cases
// reads or writes through it, and a store that pretended to hold objects would
// invite a case to be written against the pretence.
type pathlessStore struct{ id string }

var _ storageapi.Provider = pathlessStore{}

func (p pathlessStore) Name() string        { return p.id }
func (p pathlessStore) DisplayName() string { return "Pathless" }

// Capabilities declares both halves, so storageapi.Probe reports a store the
// host would route to. The absent optional interfaces are the assertion the
// case makes: no LocalFiles, because there is no local file.
func (p pathlessStore) Capabilities() storageapi.Caps {
	return storageapi.Caps{Browse: true, Write: true}
}

func (p pathlessStore) List(context.Context, string) ([]storageapi.FileInfo, error) {
	return nil, storageapi.ErrInvalidPath
}

func (p pathlessStore) Stat(context.Context, string) (storageapi.FileInfo, error) {
	return storageapi.FileInfo{}, storageapi.ErrInvalidPath
}

func (p pathlessStore) Open(context.Context, string) (io.ReadCloser, error) {
	return nil, storageapi.ErrInvalidPath
}

func (p pathlessStore) Create(context.Context, string) (storageapi.Writer, error) {
	return nil, storageapi.ErrInvalidPath
}

func (p pathlessStore) Delete(context.Context, string) error { return storageapi.ErrInvalidPath }

func (p pathlessStore) DeleteMany(context.Context, []string) error {
	return storageapi.ErrInvalidPath
}

func (p pathlessStore) Mkdir(context.Context, string) error { return storageapi.ErrInvalidPath }
