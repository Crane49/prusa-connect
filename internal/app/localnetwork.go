package app

import (
	"context"
	"fmt"
	"net/netip"
	"sync"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/discovery"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// hiddenStore is the part of the resource store that keeps the hidden setting
// in the machine-owned file.
type hiddenStore interface {
	DiscoveryHidden() (hidden, recorded bool, err error)
	RecordDiscoveryHidden(hidden bool) error
}

// localNetwork is this server on the local network. It holds the announcer
// that tells the others about this server, the listener that hears them, and
// the hidden setting. It is in the composition root because only the root may
// hold the machine-owned file and the network together.
type localNetwork struct {
	announcer *discovery.Announcer
	listener  *discovery.Listener
	store     hiddenStore
	// peers maps the address each connected printer comes from to its id, so
	// an entry the listener heard names the printer it already is. Nil names
	// none.
	peers func() map[netip.Addr]string

	// mu makes a write to the file and the change on the network one step, so
	// two people switching at once leave the file and the network agreeing.
	mu sync.Mutex
}

var _ httpapi.Discovery = (*localNetwork)(nil)

// newLocalNetwork sets the value the hidden setting starts with. A choice a
// person stored in the machine-owned file wins over core.discovery.hidden,
// which only says how a printer starts.
func newLocalNetwork(
	ctx context.Context, core config.Core, announcer *discovery.Announcer, listener *discovery.Listener,
	store hiddenStore,
) (*localNetwork, error) {
	hidden, recorded, err := store.DiscoveryHidden()
	if err != nil {
		return nil, fmt.Errorf("read the hidden setting: %w", err)
	}

	if !recorded {
		hidden = core.Discovery.Hidden
	}

	announcer.SetHidden(ctx, hidden)
	listener.SetQuiet(hidden)

	return &localNetwork{announcer: announcer, listener: listener, store: store}, nil
}

// start begins announcing and listening once the API port is bound.
func (p *localNetwork) start(ctx context.Context, port int) {
	p.announcer.Start(ctx, port)
	p.listener.Start(ctx, port)
}

// stop withdraws the records and closes the listener's socket.
func (p *localNetwork) stop(ctx context.Context) {
	p.announcer.Stop(ctx)
	p.listener.Stop(ctx)
}

// Nearby is every printer the listener hears. A hidden server still lists the
// printers it hears.
func (p *localNetwork) Nearby(_ context.Context) []httpapi.NearbyPrinter {
	heard := p.listener.Nearby()
	out := make([]httpapi.NearbyPrinter, 0, len(heard))

	var here map[netip.Addr]string
	if p.peers != nil && len(heard) > 0 {
		here = p.peers()
	}

	for _, one := range heard {
		out = append(out, httpapi.NearbyPrinter{
			Name:     one.Name,
			Address:  one.Address.String(),
			Port:     one.Port,
			TLS:      one.TLS,
			Services: one.Services,
			URL:      one.URL(),
			DeviceID: connectedFrom(here, one.Addresses),
		})
	}

	return out
}

// connectedFrom is the id of the connected printer that comes from one of the
// addresses an entry answered with, or empty when none does. The addresses
// come in the order the entry shows them, so the first match is the same one
// on every read.
func connectedFrom(here map[netip.Addr]string, addresses []netip.Addr) string {
	for _, addr := range addresses {
		if id, found := here[addr.Unmap().WithZone("")]; found {
			return id
		}
	}

	return ""
}

// Visibility is the hidden setting as the running server applies it.
func (p *localNetwork) Visibility(_ context.Context) (httpapi.Visibility, error) {
	return p.visibility(), nil
}

// SetHidden stores the setting first and then applies it. A write the file
// refused changes nothing on the network, so a restart finds what the network
// showed.
func (p *localNetwork) SetHidden(ctx context.Context, hidden bool) (httpapi.Visibility, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.store.RecordDiscoveryHidden(hidden); err != nil {
		return httpapi.Visibility{}, fmt.Errorf("store the hidden setting: %w", err)
	}

	// The listener goes quiet first, so a hidden server sends no question
	// with its address in it from the moment it is hidden.
	p.listener.SetQuiet(hidden)
	p.announcer.SetHidden(ctx, hidden)

	return p.visibility(), nil
}

func (p *localNetwork) visibility() httpapi.Visibility {
	described := p.announcer.Describe()

	addresses := make([]string, 0, len(described.Addresses))
	for _, addr := range described.Addresses {
		addresses = append(addresses, addr.String())
	}

	return httpapi.Visibility{
		Hidden:       p.announcer.Hidden(),
		Announcement: p.announcer.State(),
		Name:         described.Instance,
		Host:         described.Host,
		Port:         described.Port,
		Services:     described.Services,
		Addresses:    addresses,
		Bridged:      described.Bridged,
	}
}
