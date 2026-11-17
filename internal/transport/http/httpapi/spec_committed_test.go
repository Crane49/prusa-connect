//go:build !nodevtools && !nodashboards && diagnostics

package httpapi

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// update rewrites the committed document instead of comparing against it. It is
// the regenerate half of the generate-and-verify pair, and running it is how a
// contributor answers a failing TestCommittedSpecMatchesTheCode.
var update = flag.Bool("update", false, "rewrite "+committedSpecPath+" from the registered operations")

// The printer build publishes this document without the developer
// operations, and TestThePrinterBuildPublishesTheNormalDocumentWithoutTheDeveloperOperations
// holds it to the committed one. So this comparison runs in every other build.

// TestCommittedSpecMatchesTheCode is the layer neither the router walk nor the
// registration API can reach: a contract that changes shape while its routes
// stay put. Rename a response field and every route still matches; this fails.
func TestCommittedSpecMatchesTheCode(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	generated := indent(t, renderedDocument(t, server))

	if *update {
		require.NoError(t, os.WriteFile(committedSpecPath, generated, 0o600))
		t.Logf("wrote %s", committedSpecPath)

		return
	}

	committed, err := os.ReadFile(committedSpecPath)
	require.NoErrorf(t, err, "run: go test ./%s -run TestCommittedSpecMatchesTheCode -update",
		filepath.ToSlash("internal/transport/http/httpapi"))

	require.Equal(t, string(committed), string(generated),
		"the OpenAPI document this binary serves differs from the committed one.\n"+
			"Review the difference, then regenerate with:\n"+
			"  go test ./internal/transport/http/httpapi -run TestCommittedSpecMatchesTheCode -update")
}

func indent(t *testing.T, raw []byte) []byte {
	t.Helper()

	var value any

	require.NoError(t, json.Unmarshal(raw, &value))

	out, err := json.MarshalIndent(value, "", "  ")
	require.NoError(t, err)

	return append(out, '\n')
}
