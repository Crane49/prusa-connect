package app

import (
	"context"

	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// marketplaceSwitch is the part of the resource store that keeps whether a
// person switched the marketplace off, and whether they chose to list its
// community extensions, in the machine-owned file.
type marketplaceSwitch interface {
	MarketplaceEnabled() (enabled, recorded bool, err error)
	RecordMarketplaceEnabled(enabled bool) error
	MarketplaceCommunity() (bool, error)
	RecordMarketplaceCommunity(shown bool) error
}

// The installer finds the switch on the store it is handed, so a store that
// stopped answering to it would leave the switch silently unstored.
var _ marketplaceSwitch = (*resource.Store)(nil)

// marketplaceOn says whether this server reads its marketplace. A
// deployment that names none reads nothing, and a switch nobody has used
// leaves a configured marketplace on.
func (m *moduleInstaller) marketplaceOn() (bool, error) {
	if m.market == nil {
		return false, nil
	}

	if m.toggle == nil {
		return true, nil
	}

	enabled, recorded, err := m.toggle.MarketplaceEnabled()
	if err != nil {
		return false, httpfault.Unexpected("read the marketplace switch").Wrap(err)
	}

	return !recorded || enabled, nil
}

// communityShown says whether a person chose to list the community
// extensions. A server with no machine-owned file never stored the choice, so
// it lists none of them.
func (m *moduleInstaller) communityShown() (bool, error) {
	if m.toggle == nil {
		return false, nil
	}

	shown, err := m.toggle.MarketplaceCommunity()
	if err != nil {
		return false, httpfault.Unexpected("read the community extensions switch").Wrap(err)
	}

	return shown, nil
}

// marketplaceStatus is the marketplace object the index and the switch answer.
// A switched-off marketplace reports nothing about its last read, because
// nothing is read from it while it is off.
func (m *moduleInstaller) marketplaceStatus(on, community bool) httpapi.ModuleMarketplace {
	status := *m.market.status()
	status.Enabled = on
	status.Community = community

	if !on {
		status.Reachable, status.ReadAt, status.Problem = false, "", ""
	}

	return status
}

// indexMarketplace is the marketplace object the index answers, and nil for a
// deployment that names no marketplace.
func (m *moduleInstaller) indexMarketplace(on bool) (*httpapi.ModuleMarketplace, error) {
	if m.market == nil {
		return nil, nil //nolint:nilnil // no marketplace is an answer, and no failure
	}

	community, err := m.communityShown()
	if err != nil {
		return nil, err
	}

	status := m.marketplaceStatus(on, community)

	return &status, nil
}

// Marketplace is httpapi.ModuleInstaller.
func (m *moduleInstaller) Marketplace(_ context.Context) (httpapi.ModuleMarketplace, bool, error) {
	if err := modulesRefused(m.core); err != nil {
		return httpapi.ModuleMarketplace{}, true, err
	}

	if m.market == nil {
		return httpapi.ModuleMarketplace{}, false, nil
	}

	on, err := m.marketplaceOn()
	if err != nil {
		return httpapi.ModuleMarketplace{}, true, err
	}

	community, err := m.communityShown()
	if err != nil {
		return httpapi.ModuleMarketplace{}, true, err
	}

	return m.marketplaceStatus(on, community), true, nil
}

// SetMarketplace is httpapi.ModuleInstaller. The switches are stored before
// they are reported, so the answer is what the next start reads too. A nil
// community leaves that switch as it is.
func (m *moduleInstaller) SetMarketplace(
	ctx context.Context, enabled bool, community *bool,
) (httpapi.ModuleMarketplace, bool, error) {
	if err := modulesRefused(m.core); err != nil {
		return httpapi.ModuleMarketplace{}, true, err
	}

	if m.market == nil {
		return httpapi.ModuleMarketplace{}, false, nil
	}

	if m.toggle == nil {
		return httpapi.ModuleMarketplace{}, true,
			httpfault.Unexpected("store the marketplace switch: this server holds no machine-owned file")
	}

	if err := m.toggle.RecordMarketplaceEnabled(enabled); err != nil {
		return httpapi.ModuleMarketplace{}, true, httpfault.Unexpected("store the marketplace switch").Wrap(err)
	}

	if community != nil {
		if err := m.toggle.RecordMarketplaceCommunity(*community); err != nil {
			return httpapi.ModuleMarketplace{}, true,
				httpfault.Unexpected("store the community extensions switch").Wrap(err)
		}
	}

	return m.Marketplace(ctx)
}
