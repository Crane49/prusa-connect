package install_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
)

// TestAServiceMountIsReadAsTheOperatorWroteIt is the key ADR-20260827-wmv rule 12 names
// for a module whose clients expect a fixed address: the operator writes the
// root prefix, and the loader carries it through untouched for the host to hold
// to the router's rules.
func TestAServiceMountIsReadAsTheOperatorWroteIt(t *testing.T) {
	t.Parallel()

	digest := "sha256:" + strings.Repeat("1f", 32)

	doc, err := install.Parse("config.yaml", []byte(`
resources:
  sink:
    graphql:
      type: com.github.aux.acme.graphql/graphql
      runtime: service
      service:
        path: plugins/graphql
        checksum: `+digest+`
        contract_major: 1
        mount: /graphql
    planner:
      type: com.github.aux.acme.planner/planner
      runtime: service
      service:
        path: plugins/planner
        checksum: `+digest+`
        contract_major: 1
`))
	require.NoError(t, err)
	require.Empty(t, doc.Failures)
	require.Len(t, doc.Instances, 2)

	byName := map[string]install.Instance{}
	for _, declared := range doc.Instances {
		byName[declared.Ref.ID()] = declared
	}

	require.Equal(t, "/graphql", byName["graphql"].Service.Mount)
	require.Empty(t, byName["planner"].Service.Mount, "a module with no mount key reads as mounted somewhere")
}

// TestAServiceMountIsRefusedWhereTheFileAloneCanTell covers the two refusals
// this package owns. Everything about the prefix's shape and the paths the
// router keeps is the host's, because the reserved set is the router's own.
func TestAServiceMountIsRefusedWhereTheFileAloneCanTell(t *testing.T) {
	t.Parallel()

	digest := "sha256:" + strings.Repeat("1f", 32)

	cases := map[string]struct {
		typeName string
		mount    string
		says     string
	}{
		"a mount on a bare type, which is not a module": {
			typeName: "bambu",
			mount:    "/graphql",
			says:     "is not a module",
		},
		"a mount that says nothing": {
			typeName: "com.github.aux.acme.graphql/graphql",
			mount:    `"  "`,
			says:     "service.mount is set and empty",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			doc, err := install.Parse("config.yaml", []byte(`
resources:
  sink:
    graphql:
      type: `+testCase.typeName+`
      runtime: service
      service:
        path: plugins/graphql
        checksum: `+digest+`
        contract_major: 1
        mount: `+testCase.mount+`
`))
			require.NoError(t, err)
			require.Empty(t, doc.Instances)
			require.Len(t, doc.Failures, 1)
			require.ErrorIs(t, doc.Failures[0].Err, install.ErrInvalid)
			require.Contains(t, doc.Failures[0].Err.Error(), testCase.says)
		})
	}
}
