package diskstore_test

import (
	"bytes"
	"crypto/rand"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// TestARealFileInTheRootIsListedAndReadBack is the plainest claim the adapter
// makes, held against a real directory rather than a fake: a file an operator
// drops into the store is listed with its true size, and its bytes come back
// unchanged.
//
// The contract suite in storageapitest covers the surface exhaustively. This
// one exists because that suite proves conformance and this proves the thing an
// operator actually asks for, in one readable case.
func TestARealFileInTheRootIsListedAndReadBack(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	planted := make([]byte, 4096)
	_, err := rand.Read(planted)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(root, "bench.gcode"), planted, 0o600))

	provider := open(t, root)

	listed, err := provider.List(t.Context(), "")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, "bench.gcode", listed[0].Name)
	assert.Equal(t, "bench.gcode", listed[0].Path)
	assert.False(t, listed[0].IsDir)
	assert.Equal(t, int64(len(planted)), listed[0].Size)

	reader, err := provider.Open(t.Context(), "bench.gcode")
	require.NoError(t, err)

	defer func() { require.NoError(t, reader.Close()) }()

	read, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(planted, read), "the bytes read back differ from the bytes planted")

	// And the other direction: a file the adapter writes is on the disk with
	// the bytes it was given.
	written, err := storageapi.Save(t.Context(), provider, "sub/second.gcode", bytes.NewReader(planted))
	require.NoError(t, err)
	assert.Equal(t, int64(len(planted)), written.Size)

	onDisk, err := os.ReadFile(filepath.Join(root, "sub", "second.gcode"))
	require.NoError(t, err)
	assert.True(t, bytes.Equal(planted, onDisk), "the bytes on disk differ from the bytes saved")
}
