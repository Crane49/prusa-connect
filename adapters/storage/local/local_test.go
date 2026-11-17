package local_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/adapters/storage/local"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi/storageapitest"
)

// The adapter is the registration and the implementation is diskstore, so what
// is left to test here is that the name resolves to a working store. The store
// itself is held by the conformance suite next door.
func TestTheFactoryIsRegisteredUnderItsType(t *testing.T) {
	t.Parallel()

	registry, err := storageapi.NewRegistryFrom(mustDeclare(t))
	require.NoError(t, err)

	built, err := registry.BuildProvider(storageapi.ProviderSpec{
		ID:   "local",
		Type: local.Type,
		Path: t.TempDir(),
	})
	require.NoError(t, err)
	assert.Equal(t, "local", built.Name())
	assert.Equal(t, local.DefaultDisplayName, built.DisplayName())
}

func mustDeclare(t *testing.T) storageapi.Registration {
	t.Helper()

	declared, err := storageapi.Declare(local.Type, local.Doc(local.Type, local.DefaultDisplayName), local.New)
	require.NoError(t, err)

	return declared
}

// The contract, run through the registration rather than through diskstore
// directly. It is the same store as the one next door, and this is what says
// the name an operator writes in configuration resolves to a store that passes
// the suite: the brief asks for the contract against local, temporary, the disk
// store and the memory store, and this is the local one.
func TestProviderContract(t *testing.T) {
	storageapitest.RunProviderContract(t, func(t *testing.T) storageapitest.Subject {
		root := t.TempDir()

		registry, err := storageapi.NewRegistryFrom(mustDeclare(t))
		require.NoError(t, err)

		built, err := registry.BuildProvider(storageapi.ProviderSpec{
			ID:                "local",
			Type:              local.Type,
			Path:              root,
			AllowedExtensions: storageapi.DefaultAllowedExtensions,
		})
		require.NoError(t, err)

		return storageapitest.Subject{Provider: built, Root: root}
	})
}
