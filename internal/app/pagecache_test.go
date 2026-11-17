package app

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/buildprofile"
)

// The core's database gets the page cache of the default configuration. In a
// normal build that is the size SQLite picks for a connection nobody
// configured, so the setting changes nothing there. The printer build asks
// for 512 KiB.
func TestTheDefaultPageCacheIsSQLitesOwnOutsideThePrinterBuild(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	graph, err := authz.OpenSQLite(t.Context(), filepath.Join(dir, "authz.db"),
		poolOf(config.Default().WithDefaults(filepath.Join(dir, "config.yaml")).Persistence))
	require.NoError(t, err)

	t.Cleanup(func() { _ = graph.Close() })

	bare, err := sql.Open("sqlite", filepath.Join(dir, "bare.db"))
	require.NoError(t, err)

	t.Cleanup(func() { _ = bare.Close() })

	var configured, sqlitesOwn int

	require.NoError(t, graph.QueryRowContext(t.Context(), "PRAGMA cache_size").Scan(&configured))
	require.NoError(t, bare.QueryRowContext(t.Context(), "PRAGMA cache_size").Scan(&sqlitesOwn))

	if buildprofile.Name == "printer" {
		require.Equal(t, -512, configured)

		return
	}

	require.Equal(t, sqlitesOwn, configured)
}
