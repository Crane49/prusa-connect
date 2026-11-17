//go:build unix

package app

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// A module's listing leaves out a link, whether it points at a file or a
// folder, and a special file. Neither is an object of the store (ADR-20261023-sns
// rule 1). The person's HTTP listing still shows them.
func TestAModuleListingLeavesOutLinks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	changed := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

	require.NoError(t, os.WriteFile(filepath.Join(root, "part.gcode"), gcodeSample, 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(root, "models"), 0o700))

	for _, name := range []string{"part.gcode", "models"} {
		require.NoError(t, os.Chtimes(filepath.Join(root, name), changed, changed))
	}

	if os.Symlink("part.gcode", filepath.Join(root, "link.gcode")) != nil ||
		os.Symlink("models", filepath.Join(root, "link-models")) != nil ||
		syscall.Mkfifo(filepath.Join(root, "pipe.gcode"), 0o600) != nil {
		t.Skip("this file system makes no links or named pipes")
	}

	files, graph := storageOver(t, root, authz.ActionReadStorage)
	grantReading(t, graph)

	listing, err := files.List(t.Context(), storageapi.ListParams{Storage: "bench"})
	require.NoError(t, err)
	require.Equal(t, storageapi.Listing{Entries: []storageapi.ListEntry{
		{Name: "models", Dir: true, MTime: changed.Unix()},
		{Name: "part.gcode", Size: int64(len(gcodeSample)), MTime: changed.Unix()},
	}}, listing)
}
