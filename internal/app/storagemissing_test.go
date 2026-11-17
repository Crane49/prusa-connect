package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// Asking about a name a storage holds nothing under says there is no such
// file, and answers 404.
//
// The store folds a name it cannot resolve and a name that resolves only by
// leaving it onto one error, so a caller cannot read the store's shape out of
// the refusals. That one error is a path this server will not take, which is a
// 400, and a person told their request was wrong looks for a typo instead of
// for the file.
func TestAskingAboutAMissingNameSaysThereIsNoSuchFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "part.gcode"), []byte("G28\n"), 0o600))

	graph := newGraph(t)
	plane := storagePlane{graph: graph, stores: stores{host: stageOne(t, root).host}}
	who := subjectOf(config.Default().Principal)

	_, err := plane.Stat(t.Context(), who, "bench", "nothing.gcode")
	require.Error(t, err)

	var raised *fault.Fault
	require.ErrorAs(t, err, &raised)
	require.Equal(t, fault.StorageFileNotFound.Code(), raised.Code(),
		"a missing name reads as a path this server will not take")
	require.Equal(t, 404, raised.Status())

	// The file that is really there still answers, so the mapping did not turn
	// every read into a refusal.
	entry, err := plane.Stat(t.Context(), who, "bench", "part.gcode")
	require.NoError(t, err)
	require.Equal(t, int64(4), entry.Size)
}
