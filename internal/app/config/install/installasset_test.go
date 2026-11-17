package install_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// assetDigest is a well-formed digest for a block the loader accepts; the
// bytes it covers are the host's business, not this package's.
const assetDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// TestAnAssetDeclarationReadsItsRecord is ADR-20260911-4p6 rule 30 at the operator's
// file: a module that runs no process is declared under the asset kind with
// its directory and the digest recorded over it, and no runtime key, because
// the kind offers one runtime and nothing to choose.
func TestAnAssetDeclarationReadsItsRecord(t *testing.T) {
	t.Parallel()

	doc, err := install.Parse("config.yaml", []byte(`
resources:
  asset:
    spools:
      type: com.github.aux.acme.spools/spools
      asset:
        path: plugins/spools/ui
        checksum: `+assetDigest+`
    planner:
      type: com.github.aux.acme.planner/planner
      runtime: asset
      enabled: false
      asset:
        path: plugins/planner/ui
        checksum: `+assetDigest+`
`))
	require.NoError(t, err)
	require.Empty(t, doc.Failures)
	require.Len(t, doc.Instances, 2)

	byName := map[string]install.Instance{}
	for _, declared := range doc.Instances {
		byName[declared.Ref.ID()] = declared
	}

	spools := byName["spools"]
	require.Equal(t, ref.KindAsset, spools.Ref.Kind())
	require.Equal(t, install.RuntimeAsset, spools.Runtime,
		"an asset kind with no runtime key reads as something other than an asset")
	require.Equal(t, install.StateRuntime, spools.State)
	require.Equal(t, install.Asset{Path: "plugins/spools/ui", Checksum: assetDigest}, spools.Asset)
	require.Empty(t, spools.Service.Path, "an asset carries a service record")

	require.Equal(t, install.RuntimeAsset, byName["planner"].Runtime)
	require.Equal(t, install.StateDisabled, byName["planner"].State)
}

// TestAnAssetDeclarationIsRefusedForWhatTheFileAloneCanTell covers every
// refusal this package owns for the block. Each case is one edit to the file,
// and the message names the key to change.
func TestAnAssetDeclarationIsRefusedForWhatTheFileAloneCanTell(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		block string
		says  string
	}{
		{
			name: "no asset block",
			block: `
resources:
  asset:
    spools:
      type: com.github.aux.acme.spools/spools
`,
			says: "asset is absent",
		},
		{
			name: "an empty path",
			block: `
resources:
  asset:
    spools:
      type: com.github.aux.acme.spools/spools
      asset:
        path: "  "
        checksum: ` + assetDigest + `
`,
			says: "asset.path is empty",
		},
		{
			name: "a checksum that is not sha256",
			block: `
resources:
  asset:
    spools:
      type: com.github.aux.acme.spools/spools
      asset:
        path: plugins/spools/ui
        checksum: md5:abc
`,
			says: "asset.checksum is",
		},
		{
			name: "a bare type name, which carries no namespace to serve under",
			block: `
resources:
  asset:
    spools:
      type: spools
      asset:
        path: plugins/spools/ui
        checksum: ` + assetDigest + `
`,
			says: "bare name",
		},
		{
			name: "the service runtime, which would launch a directory",
			block: `
resources:
  asset:
    spools:
      type: com.github.aux.acme.spools/spools
      runtime: service
      service:
        path: plugins/spools
        checksum: ` + assetDigest + `
        contract_major: 1
`,
			says: "asset offers asset",
		},
		{
			name: "a service block, which carries a contract and a process an asset has not got",
			block: `
resources:
  asset:
    spools:
      type: com.github.aux.acme.spools/spools
      asset:
        path: plugins/spools/ui
        checksum: ` + assetDigest + `
      service:
        path: plugins/spools
        checksum: ` + assetDigest + `
        contract_major: 1
`,
			says: `service is set and runtime is "asset"`,
		},
		{
			name: "required, which only a type the core ships may hold",
			block: `
resources:
  asset:
    spools:
      type: com.github.aux.acme.spools/spools
      required: true
      asset:
        path: plugins/spools/ui
        checksum: ` + assetDigest + `
`,
			says: "required is true",
		},
		{
			name: "an asset block on a sink, whose runtimes execute something",
			block: `
resources:
  sink:
    spools:
      type: com.github.aux.acme.spools/spools
      runtime: service
      service:
        path: plugins/spools
        checksum: ` + assetDigest + `
        contract_major: 1
      asset:
        path: plugins/spools/ui
        checksum: ` + assetDigest + `
`,
			says: `asset is set and runtime is "service"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc, err := install.Parse("config.yaml", []byte(tc.block))
			require.NoError(t, err)
			require.Empty(t, doc.Instances, "the declaration loaded")
			require.Len(t, doc.Failures, 1)
			require.ErrorIs(t, doc.Failures[0].Err, install.ErrInvalid)
			require.Contains(t, doc.Failures[0].Err.Error(), tc.says)
			require.True(t, strings.HasPrefix(doc.Failures[0].Key, "resources."), doc.Failures[0].Key)
		})
	}
}
