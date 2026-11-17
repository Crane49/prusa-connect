package app

import (
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// peerSession is a presence session that says which address its printer
// connects from, the way the xBuddy session does.
type peerSession struct {
	*presenceSession

	peer netip.Addr
}

func (s *peerSession) Peer() netip.Addr { return s.peer }

// TestAConnectedPrinterIsNamedByTheAddressItComesFrom is what lets the list of
// printers nearby tell a printer already on this server from one still to add.
// A printer never seen connected, and a driver that says nothing about
// addresses, name no address.
func TestAConnectedPrinterIsNamedByTheAddressItComesFrom(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)
	host := plane.Arrivals("test", nil, capability.NewResolver(profile.Registry{}, discard()), "")

	arrive := func(session connectorapi.Session) string {
		t.Helper()

		_, err := host.Admit(t.Context(), session.Identity())
		require.NoError(t, err)
		require.NoError(t, host.Arrived(t.Context(), session))

		return plane.idOf(session.Identity())
	}

	presence := func(serial string, connected bool) *presenceSession {
		session := &presenceSession{
			identity: connectorapi.Identity{Vendor: "test", Model: "MK4", Serial: serial, UnitUID: serial},
			patches:  make(chan connectorapi.StatePatch),
			changed:  make(chan struct{}, 1),
		}
		session.setConnected(connected)

		return session
	}

	here := arrive(&peerSession{
		presenceSession: presence("SN-HERE", true),
		peer:            netip.MustParseAddr("::ffff:192.168.1.20"),
	})
	arrive(&peerSession{presenceSession: presence("SN-AWAY", false), peer: netip.MustParseAddr("192.168.1.21")})
	arrive(presence("SN-SILENT", true))

	// A retired id is no longer its machine's, even while its session is open.
	retired := arrive(&peerSession{
		presenceSession: presence("SN-RETIRED", true),
		peer:            netip.MustParseAddr("192.168.1.22"),
	})
	entry, ok := plane.entry(retired)
	require.True(t, ok)
	entry.markRetired("replaced", time.Now())

	// Two printers behind one router come from one address, and the lower id
	// names it on every read.
	first := arrive(
		&peerSession{presenceSession: presence("SN-NAT-1", true), peer: netip.MustParseAddr("192.168.1.30")},
	)
	second := arrive(
		&peerSession{presenceSession: presence("SN-NAT-2", true), peer: netip.MustParseAddr("192.168.1.30")},
	)

	require.Equal(t, map[netip.Addr]string{
		netip.MustParseAddr("192.168.1.20"): here,
		netip.MustParseAddr("192.168.1.30"): min(first, second),
	}, plane.peers(), "the connected printer is not named by its IPv4 address, or a printer that is "+
		"away, says nothing or is retired was named")
}

// TestAPrinterThatIsAwayIsNamedByTheAddressItLastCameFrom holds CL-245. A
// printer announces itself as soon as it is back on the network, seconds
// before its session is open again, and the list of printers nearby must not
// offer it to add in that time, nor after its session has ended.
func TestAPrinterThatIsAwayIsNamedByTheAddressItLastCameFrom(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)
	host := plane.Arrivals("test", nil, capability.NewResolver(profile.Registry{}, discard()), "")
	addr := netip.MustParseAddr("192.168.1.48")

	arrive := func(serial string, connected bool, from netip.Addr) (string, *peerSession) {
		t.Helper()

		session := &peerSession{presenceSession: &presenceSession{
			identity: connectorapi.Identity{Vendor: "test", Model: "COREONE", Serial: serial, UnitUID: serial},
			patches:  make(chan connectorapi.StatePatch),
			changed:  make(chan struct{}, 1),
		}, peer: from}
		session.setConnected(connected)

		_, err := host.Admit(t.Context(), session.Identity())
		require.NoError(t, err)
		require.NoError(t, host.Arrived(t.Context(), session))

		return plane.idOf(session.Identity()), session
	}

	id, session := arrive("SN-REBOOTS", true, addr)

	// The xBuddy session reports no address while the printer is away.
	session.setConnected(false)
	session.peer = netip.Addr{}
	require.Equal(t, map[netip.Addr]string{addr: id}, plane.peers(),
		"a printer that is away was not named by the address it last came from")

	entry, ok := plane.entry(id)
	require.True(t, ok)
	plane.retirePrinter(entry)
	require.Equal(t, map[netip.Addr]string{addr: id}, plane.peers(),
		"a printer whose session ended was not named by the address it last came from")

	// Another printer that now comes from that address is the one there.
	other, _ := arrive("SN-NEW-ON-THE-ADDRESS", true, addr)
	require.Equal(t, map[netip.Addr]string{addr: other}, plane.peers(),
		"a printer that is away held an address a connected printer comes from")
}

// An entry names the connected printer that comes from any of the addresses it
// answered with, and not only from the one the row shows.
func TestAnEntryNamesThePrinterOnAnyOfItsAddresses(t *testing.T) {
	t.Parallel()

	here := map[netip.Addr]string{
		netip.MustParseAddr("192.168.10.123"): "dev-lan",
		netip.MustParseAddr("192.168.1.30"):   "dev-other",
	}

	cases := []struct {
		name      string
		addresses []string
		want      string
	}{
		{name: "the address the row shows", addresses: []string{"192.168.10.123"}, want: "dev-lan"},
		{name: "a second address", addresses: []string{"192.168.1.20", "192.168.10.123"}, want: "dev-lan"},
		{name: "an IPv4 address written as IPv6", addresses: []string{"::ffff:192.168.10.123"}, want: "dev-lan"},
		{
			name:      "the first match in the order shown",
			addresses: []string{"192.168.1.30", "192.168.10.123"},
			want:      "dev-other",
		},
		{name: "no match", addresses: []string{"192.168.1.20"}, want: ""},
		{name: "no address", addresses: nil, want: ""},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			addrs := make([]netip.Addr, 0, len(testCase.addresses))
			for _, literal := range testCase.addresses {
				addrs = append(addrs, netip.MustParseAddr(literal))
			}

			require.Equal(t, testCase.want, connectedFrom(here, addrs))
		})
	}
}
