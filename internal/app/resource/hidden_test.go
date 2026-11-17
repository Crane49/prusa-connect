package resource_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/resource"
)

// TestTheHiddenSettingIsAbsentUntilAPersonChoosesIt keeps the file's key in
// charge on a printer nobody has touched: the overlay records nothing, and the
// caller falls back to core.discovery.hidden.
func TestTheHiddenSettingIsAbsentUntilAPersonChoosesIt(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	hidden, recorded, err := h.store.DiscoveryHidden()
	require.NoError(t, err)
	require.False(t, recorded)
	require.False(t, hidden)
}

// TestTheHiddenSettingIsWrittenAndReadBack is the setting stored in the
// machine-owned file, both ways. False is stored too, because a person who
// showed a printer the file hides has made a choice that has to win.
func TestTheHiddenSettingIsWrittenAndReadBack(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	require.NoError(t, h.store.RecordDiscoveryHidden(true))

	hidden, recorded, err := h.store.DiscoveryHidden()
	require.NoError(t, err)
	require.True(t, recorded)
	require.True(t, hidden)

	require.NoError(t, h.store.RecordDiscoveryHidden(false))

	hidden, recorded, err = h.store.DiscoveryHidden()
	require.NoError(t, err)
	require.True(t, recorded)
	require.False(t, hidden)

	var onDisk map[string]json.RawMessage

	content, err := os.ReadFile(h.paths.Overlay)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(content, &onDisk))
	require.JSONEq(t, `{"hidden": false}`, string(onDisk["discovery"]))
}

// TestWritingTheHiddenSettingKeepsTheFirstRunDecision holds the write to the
// one key: every other member of the overlay stays as it was.
func TestWritingTheHiddenSettingKeepsTheFirstRunDecision(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	require.NoError(t, h.store.RecordSetupMode("required"))
	require.NoError(t, h.store.RecordDiscoveryHidden(true))

	mode, err := h.store.SetupMode()
	require.NoError(t, err)
	require.Equal(t, "required", mode)
}

// TestAnOverlayKeyThisBuildDoesNotReadIsNamed is the way back to an older
// build. A newer build wrote a key this one does not read, and the refusal
// names the key so a person can remove it.
func TestAnOverlayKeyThisBuildDoesNotReadIsNamed(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	require.NoError(t, os.MkdirAll(filepath.Dir(h.paths.Overlay), 0o750))
	require.NoError(t, os.WriteFile(h.paths.Overlay,
		[]byte(`{"version": 1, "instances": [], "later": {"on": true}}`), 0o600))

	_, _, err := h.store.DiscoveryHidden()
	require.ErrorIs(t, err, resource.ErrConfigInvalid)
	require.Contains(t, err.Error(), `"later"`)
	require.Contains(t, err.Error(), "remove the key")
}

// TestANestedOverlayKeyIsNamedWithItsParent is a key a newer build wrote under
// a member this build reads. The refusal names the whole path, so a person
// finds the key under discovery rather than looking for it at the top.
func TestANestedOverlayKeyIsNamedWithItsParent(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	require.NoError(t, os.MkdirAll(filepath.Dir(h.paths.Overlay), 0o750))
	require.NoError(t, os.WriteFile(h.paths.Overlay,
		[]byte(`{"version": 1, "instances": [], "discovery": {"hidden": true, "announce_as": "Desk"}}`), 0o600))

	_, _, err := h.store.DiscoveryHidden()
	require.ErrorIs(t, err, resource.ErrConfigInvalid)
	require.Contains(t, err.Error(), `"discovery.announce_as"`)
}

// TestTheOverlayKeyNamedIsTheOneRefused is an unknown key written twice, at
// the top and under discovery. The refusal names the one the decoder met
// first, every time, because the walk reads the file in its own order.
func TestTheOverlayKeyNamedIsTheOneRefused(t *testing.T) {
	t.Parallel()

	for range 20 {
		h := newHarness(t)

		require.NoError(t, os.MkdirAll(filepath.Dir(h.paths.Overlay), 0o750))
		require.NoError(t, os.WriteFile(h.paths.Overlay, []byte(
			`{"version": 1, "instances": [], "zzz": 1, "discovery": {"hidden": true, "zzz": 2}}`), 0o600))

		_, _, err := h.store.DiscoveryHidden()
		require.ErrorIs(t, err, resource.ErrConfigInvalid)
		require.Contains(t, err.Error(), `the key "zzz"`)
	}
}
