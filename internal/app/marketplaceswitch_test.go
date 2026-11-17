package app

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// memorySwitch is the machine-owned file's marketplace key, held in memory.
type memorySwitch struct {
	mu        sync.Mutex
	enabled   bool
	recorded  bool
	community bool
}

func (s *memorySwitch) MarketplaceEnabled() (enabled, recorded bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.enabled, s.recorded, nil
}

func (s *memorySwitch) RecordMarketplaceEnabled(enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.enabled, s.recorded = enabled, true

	return nil
}

func (s *memorySwitch) MarketplaceCommunity() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.community, nil
}

func (s *memorySwitch) RecordMarketplaceCommunity(shown bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.community = shown

	return nil
}

func switchedInstaller(t *testing.T, market string, toggle *memorySwitch) *moduleInstaller {
	t.Helper()

	installer, _ := marketInstaller(t, market)
	installer.toggle = toggle

	return installer
}

func TestAMarketplaceNobodySwitchedIsRead(t *testing.T) {
	t.Parallel()

	market := newFakeMarketplace(t, archiveBytes(t, bothHalvesManifest()), "1.0.0")
	installer := switchedInstaller(t, market.server.URL, &memorySwitch{})

	index, err := installer.Index(t.Context(), "user:operator")
	require.NoError(t, err)
	require.NotNil(t, index.Marketplace)
	require.True(t, index.Marketplace.Enabled)
	require.NotEmpty(t, index.Items, "the marketplace rows were not listed")
	require.Equal(t, 1, market.lists())
}

// Switched off, the marketplace costs no request at all: the index answers
// the local rows and says the marketplace is off, and nothing reached it.
func TestASwitchedOffMarketplaceIsNotRead(t *testing.T) {
	t.Parallel()

	market := newFakeMarketplace(t, archiveBytes(t, bothHalvesManifest()), "1.0.0")
	installer := switchedInstaller(t, market.server.URL, &memorySwitch{enabled: false, recorded: true})

	index, err := installer.Index(t.Context(), "user:operator")
	require.NoError(t, err)
	require.Empty(t, index.Items)
	require.NotNil(t, index.Marketplace)
	require.False(t, index.Marketplace.Enabled)
	require.False(t, index.Marketplace.Reachable)
	require.Equal(t, market.server.URL, index.Marketplace.URL)
	require.Zero(t, market.lists(), "a switched-off marketplace was asked for its catalogue")
}

func TestAnInstallFromASwitchedOffMarketplaceIsRefused(t *testing.T) {
	t.Parallel()

	market := newFakeMarketplace(t, archiveBytes(t, bothHalvesManifest()), "1.0.0")
	installer := switchedInstaller(t, market.server.URL, &memorySwitch{enabled: false, recorded: true})

	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: "github.com/acme/spools", Version: "1.0.0", Approve: true,
	})

	requireCode(t, err, httpfault.CodeMarketplaceDisabled())
	require.Zero(t, market.lists())
}

func TestSwitchingTheMarketplaceIsStoredAndApplied(t *testing.T) {
	t.Parallel()

	market := newFakeMarketplace(t, archiveBytes(t, bothHalvesManifest()), "1.0.0")
	toggle := &memorySwitch{}
	installer := switchedInstaller(t, market.server.URL, toggle)

	off, ok, err := installer.SetMarketplace(t.Context(), false, nil)
	require.NoError(t, err)
	require.True(t, ok)
	require.False(t, off.Enabled)

	enabled, recorded, err := toggle.MarketplaceEnabled()
	require.NoError(t, err)
	require.True(t, recorded, "switching off was not stored, so a restart would read the marketplace again")
	require.False(t, enabled)

	index, err := installer.Index(t.Context(), "user:operator")
	require.NoError(t, err)
	require.Empty(t, index.Items)

	on, ok, err := installer.SetMarketplace(t.Context(), true, nil)
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, on.Enabled)

	index, err = installer.Index(t.Context(), "user:operator")
	require.NoError(t, err)
	require.NotEmpty(t, index.Items, "the marketplace rows were not listed")
}

// The community switch is off until an administrator moves it, and moving it
// leaves the marketplace switch as it was. The index still lists every row,
// because the web app is what hides the community extensions.
func TestTheCommunitySwitchIsStoredApartFromTheMarketplaceSwitch(t *testing.T) {
	t.Parallel()

	market := newFakeMarketplace(t, archiveBytes(t, bothHalvesManifest()), "1.0.0")
	toggle := &memorySwitch{}
	installer := switchedInstaller(t, market.server.URL, toggle)

	before, ok, err := installer.Marketplace(t.Context())
	require.NoError(t, err)
	require.True(t, ok)
	require.False(t, before.Community, "community extensions were listed before anybody chose them")

	shown := true

	after, ok, err := installer.SetMarketplace(t.Context(), true, &shown)
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, after.Community)
	require.True(t, after.Enabled)
	require.True(t, toggle.community)

	kept, _, err := installer.SetMarketplace(t.Context(), false, nil)
	require.NoError(t, err)
	require.True(t, kept.Community, "a write that named no community changed the community switch")

	index, err := installer.Index(t.Context(), "user:operator")
	require.NoError(t, err)
	require.NotNil(t, index.Marketplace)
	require.True(t, index.Marketplace.Community)
}

// A deployment that names no marketplace has no switch to store.
func TestADeploymentWithNoMarketplaceStoresNoSwitch(t *testing.T) {
	t.Parallel()

	toggle := &memorySwitch{}
	installer := switchedInstaller(t, "", toggle)

	_, ok, err := installer.Marketplace(t.Context())
	require.NoError(t, err)
	require.False(t, ok)

	_, ok, err = installer.SetMarketplace(t.Context(), true, nil)
	require.NoError(t, err)
	require.False(t, ok)

	_, recorded, err := toggle.MarketplaceEnabled()
	require.NoError(t, err)
	require.False(t, recorded)
}
