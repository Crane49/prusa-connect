package resource_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTheMarketplaceSwitchIsAbsentUntilAPersonUsesIt leaves a configured
// core.marketplace.url in charge on a server nobody has touched.
func TestTheMarketplaceSwitchIsAbsentUntilAPersonUsesIt(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	enabled, recorded, err := h.store.MarketplaceEnabled()
	require.NoError(t, err)
	require.False(t, recorded)
	require.False(t, enabled)
}

// TestTheMarketplaceSwitchIsWrittenAndReadBack is the switch stored in the
// machine-owned file, both ways. False is stored too: switching the
// marketplace off is the choice that has to survive a restart.
func TestTheMarketplaceSwitchIsWrittenAndReadBack(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	require.NoError(t, h.store.RecordMarketplaceEnabled(false))

	enabled, recorded, err := h.store.MarketplaceEnabled()
	require.NoError(t, err)
	require.True(t, recorded)
	require.False(t, enabled)

	require.NoError(t, h.store.RecordMarketplaceEnabled(true))

	enabled, recorded, err = h.store.MarketplaceEnabled()
	require.NoError(t, err)
	require.True(t, recorded)
	require.True(t, enabled)

	var onDisk map[string]json.RawMessage

	content, err := os.ReadFile(h.paths.Overlay)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(content, &onDisk))
	require.JSONEq(t, `{"enabled": true}`, string(onDisk["marketplace"]))
}

// TestTheCommunitySwitchIsWrittenApartFromTheMarketplaceSwitch holds each
// write to its own key. A community choice on a server nobody switched the
// marketplace on leaves the marketplace switch unrecorded, so the configured
// marketplace is still read.
func TestTheCommunitySwitchIsWrittenApartFromTheMarketplaceSwitch(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	shown, err := h.store.MarketplaceCommunity()
	require.NoError(t, err)
	require.False(t, shown)

	require.NoError(t, h.store.RecordMarketplaceCommunity(true))

	_, recorded, err := h.store.MarketplaceEnabled()
	require.NoError(t, err)
	require.False(t, recorded, "the community switch recorded the marketplace switch")

	require.NoError(t, h.store.RecordMarketplaceEnabled(false))

	shown, err = h.store.MarketplaceCommunity()
	require.NoError(t, err)
	require.True(t, shown, "the marketplace switch cleared the community switch")

	var onDisk map[string]json.RawMessage

	content, err := os.ReadFile(h.paths.Overlay)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(content, &onDisk))
	require.JSONEq(t, `{"enabled": false, "community": true}`, string(onDisk["marketplace"]))
}

// TestWritingTheMarketplaceSwitchKeepsTheRestOfTheFile holds the write to the
// one key: the hidden setting beside it stays as it was.
func TestWritingTheMarketplaceSwitchKeepsTheRestOfTheFile(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	require.NoError(t, h.store.RecordDiscoveryHidden(true))
	require.NoError(t, h.store.RecordMarketplaceEnabled(false))

	hidden, recorded, err := h.store.DiscoveryHidden()
	require.NoError(t, err)
	require.True(t, recorded)
	require.True(t, hidden)
}
