package resource_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The two digests an asset declaration carries here. What bytes they cover is
// the host's business: this package reads the record and never the directory.
const (
	installed = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	rebuilt   = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
)

// TestAnAssetBlockLoadsAsAModuleWithNoProcess is ADR-20260911-4p6 rule 30 at the
// merged document: the block reads under its own runtime, carries its record,
// and is a module, so the namespace its files are served under is derived from
// the type the way a process module's is.
func TestAnAssetBlockLoadsAsAModuleWithNoProcess(t *testing.T) {
	t.Parallel()

	doc, err := load(t, `
resources:
  asset:
    spools:
      type: com.github.aux.acme.spools/spools
      asset:
        path: /opt/pcl/plugins/spools/ui
        checksum: sha256:`+installed+`
`)
	require.NoError(t, err)
	require.Empty(t, doc.Failures)

	declared, ok := doc.Find(refOf(t, ref.KindAsset, "spools"))
	require.True(t, ok)
	require.Equal(t, install.RuntimeAsset, declared.Spec.Runtime)
	require.Nil(t, declared.Spec.Service, "an asset carries a service block")
	require.NotNil(t, declared.Spec.Asset)
	require.Equal(t, "/opt/pcl/plugins/spools/ui", declared.Spec.Asset.Path)
	require.Equal(t, "sha256:"+installed, declared.Spec.Asset.Checksum)

	namespace, isModule := declared.ModuleNamespace()
	require.True(t, isModule, "an asset is not a module, so nothing would mount its files")
	require.Equal(t, "com.github.aux.acme.spools", namespace)
	require.Empty(t, declared.ModuleMount(), "an asset has no root mount to claim")
}

// TestTheAssetRecordReachesTheDigest is what tells the supervision plane a
// bundle was reinstalled: a declaration differing only in its checksum or its
// directory is a different bundle to verify and serve.
func TestTheAssetRecordReachesTheDigest(t *testing.T) {
	t.Parallel()

	one := resource.Spec{
		Type: "com.github.aux.acme.spools/spools", Enabled: true, Runtime: install.RuntimeAsset,
		Asset: &install.Asset{Path: "plugins/spools/ui", Checksum: "sha256:" + installed},
	}
	// A second declaration of the same record, behind a pointer of its own, so
	// the comparison is over the record and never over where it is held.
	again := one
	again.Asset = &install.Asset{Path: "plugins/spools/ui", Checksum: "sha256:" + installed}
	rehashed := one
	rehashed.Asset = &install.Asset{Path: "plugins/spools/ui", Checksum: "sha256:" + rebuilt}
	moved := one
	moved.Asset = &install.Asset{Path: "plugins/spools/v2", Checksum: "sha256:" + installed}

	require.NotEqual(t, one.Digest(), rehashed.Digest(), "a new checksum reads as the same bundle")
	require.NotEqual(t, one.Digest(), moved.Digest(), "a new directory reads as the same bundle")
	require.True(t, one.Equal(again))
	require.False(t, one.Equal(rehashed))
}

// TestAnAssetIsNotCreatedThroughTheResourceAPI holds the API to the kinds it
// can build. An asset is a directory an install verified, and this API compiles
// nothing, so a create is refused rather than accepting a block nothing can
// serve.
func TestAnAssetIsNotCreatedThroughTheResourceAPI(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	instance, err := ref.New(ref.KindAsset, "spools")
	require.NoError(t, err)

	_, applyErr := h.store.Apply(t.Context(), owner, instance,
		httpapi.ResourceSpec{Type: "com.github.aux.acme.spools/spools", Enabled: true},
		httpapi.Precondition{MustNotExist: true})
	require.Error(t, applyErr)
	require.True(t, fault.HasCode(applyErr, httpfault.CodeRequestInvalid()), applyErr)
	require.Contains(t, applyErr.Error(), "creates only what this binary compiles in")
	require.Empty(t, h.stray())
}
