package builtin_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/builtin"
	"github.com/prusa3d/prusa-connect/internal/extension/host"
	"github.com/prusa3d/prusa-connect/internal/extension/registry"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// TestTheBuiltInStorageTypeIsRegistered proves the blank import in the
// composition root reaches this package's init, which is the whole mechanism a
// third-party type arrives by.
func TestTheBuiltInStorageTypeIsRegistered(t *testing.T) {
	t.Parallel()

	types, err := registry.New()
	require.NoError(t, err)

	found, err := types.Lookup(ref.KindStorage, builtin.TypeFilesystem)
	require.NoError(t, err)
	require.Equal(t, builtin.TypeFilesystem, found.Name())
	require.True(t, found.Namespace().IsCore())
}

// TestTheStorageRootIsCreatedOrRefused is the whole contract: a usable root is
// created, and everything else is a construction failure rather than an
// instance that pretends to work.
func TestTheStorageRootIsCreatedOrRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	file := filepath.Join(dir, "a-file")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))

	cases := map[string]struct {
		config  string
		refused string
	}{
		"a root that does not exist yet is created": {config: `{"root":"` + slash(dir) + `/made"}`},
		"an existing directory is taken as it is":   {config: `{"root":"` + slash(dir) + `"}`},
		"a settings block with no root is refused":  {config: `{}`, refused: "root is empty"},
		"a misspelled setting is refused":           {config: `{"rooot":"/tmp"}`, refused: "rooot"},
		// The refusal is matched on the root this code puts in the message and
		// not on the operating system's word for it. Linux says "not a
		// directory" and Windows says the path cannot be found, so asserting
		// either one passes on one platform and fails on the other.
		"a root that is a file is refused": {
			config: `{"root":"` + slash(file) + `"}`, refused: slash(file),
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			instance, err := ref.New(ref.KindStorage, "local")
			require.NoError(t, err)

			types, err := registry.New()
			require.NoError(t, err)

			built, err := registry.Construct[storageapi.Provider](t.Context(), types, ref.KindStorage,
				host.Discard(), registry.Spec{Ref: instance, Type: builtin.TypeFilesystem, Config: []byte(tc.config)})

			if tc.refused != "" {
				require.ErrorIs(t, err, builtin.ErrFilesystemConfig)
				require.Contains(t, err.Error(), tc.refused)

				return
			}

			require.NoError(t, err)

			// The instance is the published provider contract, so the root is
			// checked through it rather than through a method of its own: it
			// lists, which is what a caller does first.
			_, listErr := built.List(t.Context(), "")
			require.NoError(t, listErr)

			// Browse and Write are the provider's own declaration; the local
			// file handle is an interface it either has or has not, so it is
			// read through the probe and not through the declaration.
			require.True(t, built.Capabilities().Browse)
			require.True(t, built.Capabilities().Write)
			require.True(t, storageapi.Probe(built).LocalFile)
		})
	}
}

// TestTheStorageReportsTheAllowlistItEnforces holds the reported list to the
// three states the setting has: unset is the built-in list, an empty list is no
// restriction, and a written list is that list.
func TestTheStorageReportsTheAllowlistItEnforces(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		extra string
		want  []string
	}{
		"unset takes the built-in list": {want: storageapi.DefaultAllowedExtensions},
		"an empty list allows any":      {extra: `,"allowed_extensions":[]`, want: []string{}},
		"a written list is kept":        {extra: `,"allowed_extensions":[".gcode"]`, want: []string{".gcode"}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			instance, err := ref.New(ref.KindStorage, "local")
			require.NoError(t, err)

			types, err := registry.New()
			require.NoError(t, err)

			built, err := registry.Construct[storageapi.Provider](t.Context(), types, ref.KindStorage,
				host.Discard(), registry.Spec{
					Ref: instance, Type: builtin.TypeFilesystem,
					Config: []byte(`{"root":"` + slash(t.TempDir()) + `"` + tc.extra + `}`),
				})
			require.NoError(t, err)

			policy, ok := built.(storageapi.ExtensionPolicy)
			require.True(t, ok)
			require.ElementsMatch(t, tc.want, policy.AllowedExtensions())
		})
	}
}

func slash(path string) string {
	return filepath.ToSlash(path)
}
