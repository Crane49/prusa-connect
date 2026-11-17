package resource_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTheTimeServerSwitchIsAbsentUntilAPersonUsesIt leaves
// core.time_server.enabled in charge on a server nobody has touched.
func TestTheTimeServerSwitchIsAbsentUntilAPersonUsesIt(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	enabled, recorded, err := h.store.TimeServerEnabled()
	require.NoError(t, err)
	require.False(t, recorded)
	require.False(t, enabled)
}

// TestTheTimeServerSwitchIsWrittenAndReadBack stores false too, because a
// person who switched off a server the file turns on has made a choice.
func TestTheTimeServerSwitchIsWrittenAndReadBack(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	require.NoError(t, h.store.RecordDiscoveryHidden(true))
	require.NoError(t, h.store.RecordTimeServerEnabled(true))

	enabled, recorded, err := h.store.TimeServerEnabled()
	require.NoError(t, err)
	require.True(t, recorded)
	require.True(t, enabled)

	require.NoError(t, h.store.RecordTimeServerEnabled(false))

	enabled, recorded, err = h.store.TimeServerEnabled()
	require.NoError(t, err)
	require.True(t, recorded)
	require.False(t, enabled)

	hidden, _, err := h.store.DiscoveryHidden()
	require.NoError(t, err)
	require.True(t, hidden, "the write changed another member of the file")

	var onDisk map[string]json.RawMessage

	content, err := os.ReadFile(h.paths.Overlay)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(content, &onDisk))
	require.JSONEq(t, `{"enabled": false}`, string(onDisk["time_server"]))
}
