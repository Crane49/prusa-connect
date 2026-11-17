package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/scaffold"
)

// TestThePublishedVersionsAreTheCheckouts. A module written outside a checkout
// asks for these versions, and they are the versions this tree releases. A
// bump of the tree that leaves them behind fails here, not on an author's
// first install.
func TestThePublishedVersionsAreTheCheckouts(t *testing.T) {
	t.Parallel()

	checkout := theCheckout(t)

	goVersion, err := scaffold.GoVersion(checkout)
	require.NoError(t, err)
	require.Equal(t, publishedGo, goVersion, "sdk/go.mod moved and publishedGo did not")

	for _, pkg := range []string{"typescript", "svelte", "standards"} {
		body, readErr := os.ReadFile(filepath.Join(checkout, "packages", pkg, "package.json"))
		require.NoError(t, readErr)

		var held struct {
			Version string `json:"version"`
		}

		require.NoError(t, json.Unmarshal(body, &held))
		require.Equal(t, publishedPackages, held.Version, "packages/%s moved and publishedPackages did not", pkg)
	}

	body, err := os.ReadFile(filepath.Join(checkout, "packages", "svelte", "package.json"))
	require.NoError(t, err)

	var library struct {
		PackageManager string            `json:"packageManager"`
		Engines        map[string]string `json:"engines"`
	}

	require.NoError(t, json.Unmarshal(body, &library))
	require.Equal(t, "pnpm@"+oldestPnpm, library.PackageManager, "the library's pnpm moved and oldestPnpm did not")
	require.Equal(t, ">="+oldestNode, library.Engines["node"], "the library's Node moved and oldestNode did not")
}
