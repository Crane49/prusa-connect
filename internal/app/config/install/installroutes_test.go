package install_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// routesService is one module declaration whose service block carries the
// route grants in block, indented under service.
func routesService(block string) []byte {
	return []byte(`
resources:
  sink:
    uploader:
      type: com.github.aux.acme.uploader/uploader
      runtime: service
      service:
        path: plugins/uploader
        checksum: sha256:` + strings.Repeat("1f", 32) + `
        contract_major: 1
` + block)
}

// The route grants an operator approved load as they were written.
func TestTheRouteGrantsLoadAsWritten(t *testing.T) {
	t.Parallel()

	doc, err := install.Parse("config.yaml", routesService(`        public_routes:
          - POST /hooks/{device}/announce
        staged_routes:
          - {method: PUT, path: "/files/{name}", max_mib: 1024}
          - {method: POST, path: "/files/{name}", max_mib: 10}
`))
	require.NoError(t, err)
	require.Empty(t, doc.Failures)
	require.Len(t, doc.Instances, 1)

	service := doc.Instances[0].Service
	require.Equal(t, []manifest.Route{{Method: "POST", Path: "/hooks/{device}/announce"}}, service.PublicRoutes)
	require.Equal(t, []install.StagedRoute{
		{Method: "PUT", Path: "/files/{name}", MaxMiB: 1024},
		{Method: "POST", Path: "/files/{name}", MaxMiB: 10},
	}, service.StagedRoutes)
}

// A hand edit of the service block is held to the rules the manifest holds a
// module to. The host matches the first route a request reaches, so a block
// it would read differently from every page is refused (ADR-20261120-xys).
func TestARouteGrantIsRefusedAtLoad(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		block string
		says  string
	}{
		"a public route that is no route": {
			block: "        public_routes:\n          - hooks/announce\n",
			says:  "service.public_routes names",
		},
		"a staged route that is no route": {
			block: "        staged_routes:\n          - {method: put, path: /files/raw, max_mib: 10}\n",
			says:  "service.staged_routes[0]",
		},
		"a staged limit of nothing": {
			block: "        staged_routes:\n          - {method: PUT, path: /files/raw, max_mib: 0}\n",
			says:  "expected 1 to 4096",
		},
		"a staged limit over the host's": {
			block: "        staged_routes:\n          - {method: PUT, path: /files/raw, max_mib: 5000}\n",
			says:  "expected 1 to 4096",
		},
		"a staged route twice": {
			block: "        staged_routes:\n" +
				"          - {method: PUT, path: /files/raw, max_mib: 10}\n" +
				"          - {method: PUT, path: /files/raw, max_mib: 20}\n",
			says: "one request can reach both",
		},
		"two staged routes one request reaches": {
			block: "        staged_routes:\n" +
				"          - {method: PUT, path: \"/files/{name}\", max_mib: 10}\n" +
				"          - {method: PUT, path: /files/big, max_mib: 4096}\n",
			says: "one request can reach both",
		},
		"a route both public and staged": {
			block: "        public_routes:\n          - PUT /files/{name}\n" +
				"        staged_routes:\n          - {method: PUT, path: /files/raw, max_mib: 10}\n",
			says: "both reach",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			doc, err := install.Parse("config.yaml", routesService(testCase.block))
			require.NoError(t, err)
			require.Empty(t, doc.Instances)
			require.Len(t, doc.Failures, 1)
			require.ErrorIs(t, doc.Failures[0].Err, install.ErrInvalid)
			require.Contains(t, doc.Failures[0].Err.Error(), testCase.says)
		})
	}
}
