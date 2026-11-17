package temporary_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/adapters/storage/temporary"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi/storageapitest"
)

// The published suite every storage provider is held to. Staging storage is
// held to the whole of it: a file on its way to a printer is a file, and a
// store an operator can point at a mount is a store a link can be planted in.
func TestProviderContract(t *testing.T) {
	storageapitest.RunProviderContract(t, func(t *testing.T) storageapitest.Subject {
		root := t.TempDir()

		built, err := temporary.New(storageapi.ProviderSpec{
			ID:                "temporary",
			Type:              temporary.Type,
			Path:              root,
			AllowedExtensions: storageapi.DefaultAllowedExtensions,
		})
		require.NoError(t, err)

		return storageapitest.Subject{Provider: built, Root: root}
	})
}

// An earlier design decision, and the one that matters most here: staging storage is where a
// print job lands on its way to a printer, so it is the store a half-gigabyte
// upload actually goes through.
func TestNothingScalesWithFileSize(t *testing.T) {
	storageapitest.RunStreaming(t, func(t *testing.T) storageapitest.Subject {
		root := t.TempDir()

		built, err := temporary.New(storageapi.ProviderSpec{
			ID:                "temporary",
			Type:              temporary.Type,
			Path:              root,
			AllowedExtensions: storageapi.DefaultAllowedExtensions,
		})
		require.NoError(t, err)

		return storageapitest.Subject{Provider: built, Root: root}
	})
}

func TestTheStagingStoreIsAddressedByItsOwnType(t *testing.T) {
	t.Parallel()

	built, err := temporary.New(storageapi.ProviderSpec{
		ID: "temporary", Type: temporary.Type, Path: t.TempDir(),
	})
	require.NoError(t, err)

	assert.Equal(t, "temporary", built.Name())
	assert.Equal(t, temporary.DefaultDisplayName, built.DisplayName())
	assert.Equal(t, storageapi.Caps{Browse: true, Write: true}, built.Capabilities())
	// The optional abilities are the type system's to report, not the
	// provider's, so what a host sees comes from Probe.
	assert.Equal(t,
		storageapi.Caps{Browse: true, Write: true, LocalFile: true, Rename: true},
		storageapi.Probe(built))
}

// Staging and keeping are two names in the reference, and an operator reading
// one has to be told which they are looking at.
func TestTheReferenceSeparatesStagingFromKeeping(t *testing.T) {
	t.Parallel()

	declared, err := storageapi.Declare(temporary.Type, temporary.Doc(), temporary.New)
	require.NoError(t, err)

	assert.Equal(t, temporary.Type, declared.Name())
	assert.Contains(t, declared.Descriptor().Doc().Description, "Staging storage")
}
