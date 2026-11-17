package discovery

import (
	"net"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTheStartupLineNamesEveryAnnouncedAddress fixes the words of the first
// line of ADR-20260916-b3x rule 12. An operator whose client finds nothing greps for
// this line, so the words are part of the record rather than a thing a reader
// of the code chooses.
func TestTheStartupLineNamesEveryAnnouncedAddress(t *testing.T) {
	t.Parallel()

	line := announcementLine(
		"Prusa Connect",
		addresses(t, "192.168.1.20", "fe80::1"),
		[]string{"_prusalink._tcp", "_prusa-connect._tcp"})

	require.Equal(t,
		"announcing Prusa Connect on 192.168.1.20, fe80::1 as _prusalink._tcp, _prusa-connect._tcp",
		line)
}

// TestTheStartupLineSaysSoWhenNothingWentOut keeps the line readable on a
// machine with no interface to announce on. "on no address" is a sentence; an
// empty gap is a line somebody reads twice.
func TestTheStartupLineSaysSoWhenNothingWentOut(t *testing.T) {
	t.Parallel()

	require.Equal(t,
		"announcing Prusa Connect on no address as _prusalink._tcp",
		announcementLine("Prusa Connect", nil, []string{"_prusalink._tcp"}))
}

// TestTheContainerLineNamesTheBridgeAddressAndBothSettings fixes the words of
// the second line of ADR-20260916-b3x rule 12. It is the line that answers the support
// question the shipped container produces: the server is up, the announcement
// is on, and nothing on the network finds it.
func TestTheContainerLineNamesTheBridgeAddressAndBothSettings(t *testing.T) {
	t.Parallel()

	warning := bridgeWarning(true, []announcedInterface{
		{name: "eth0", addresses: addresses(t, "172.17.0.2", "fe80::42:acff:fe11:2")},
	})

	require.Equal(t,
		"172.17.0.2 is a bridge address that no machine on the network reaches; "+
			"set network_mode: host in compose or hostNetwork: true in Kubernetes",
		warning)
}

// TestTheContainerLineIsWrittenOnlyWhenItIsTrue holds the warning to the one
// shape it describes. A line that appeared on a Raspberry Pi would teach every
// reader to ignore it.
func TestTheContainerLineIsWrittenOnlyWhenItIsTrue(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		containerised bool
		interfaces    []announcedInterface
	}{
		"a binary on a machine of its own": {
			containerised: false,
			interfaces: []announcedInterface{
				{name: "eth0", addresses: addresses(t, "192.168.1.20")},
			},
		},
		"a container sharing the host's interfaces": {
			containerised: true,
			interfaces: []announcedInterface{
				{name: "eth0", addresses: addresses(t, "192.168.1.20")},
				{name: "wlan0", addresses: addresses(t, "192.168.1.21")},
			},
		},
		"an address the whole internet reaches": {
			containerised: true,
			interfaces: []announcedInterface{
				{name: "eth0", addresses: addresses(t, "203.0.113.7")},
			},
		},
		"no interface to announce on": {
			containerised: true,
			interfaces:    nil,
		},
	}

	for name, one := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Empty(t, bridgeWarning(one.containerised, one.interfaces))
		})
	}
}

// TestTheAddressesOfEveryInterfaceAreNamed proves the first line reads every
// interface rather than the first one. A machine with wired and wireless
// networks announces on both, and an operator reading one address would look
// for the server on the wrong network.
func TestTheAddressesOfEveryInterfaceAreNamed(t *testing.T) {
	t.Parallel()

	got := addressesOf([]announcedInterface{
		{name: "eth0", addresses: addresses(t, "192.168.1.20")},
		{name: "wlan0", addresses: addresses(t, "10.0.0.5")},
	})

	require.Equal(t, addresses(t, "192.168.1.20", "10.0.0.5"), got)
}

// addresses parses a list of literals into what the interface reader produces.
func addresses(t *testing.T, literals ...string) []netip.Addr {
	t.Helper()

	out := make([]netip.Addr, 0, len(literals))

	for _, literal := range literals {
		address, err := netip.ParseAddr(literal)
		require.NoError(t, err)

		out = append(out, address)
	}

	return out
}

// An address goes out only when another machine can open it. A VPN gives this
// machine an address on a tunnel, and some virtual bridges carry the network
// address of their prefix; a link to either opens nothing.
func TestOnlyAnAddressAnotherMachineOpensIsAnnounced(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		address string
		bits    int
		want    bool
	}{
		{name: "a host on a /24", address: "192.168.10.123", bits: 24, want: true},
		{name: "the network address of a /24", address: "192.168.107.0", bits: 24, want: false},
		{name: "a .0 host on a /23", address: "192.168.107.0", bits: 23, want: true},
		{name: "a .0 host inside a /22", address: "192.168.1.0", bits: 22, want: true},
		{name: "a /31 link", address: "10.0.0.0", bits: 31, want: true},
		{name: "a /32 address", address: "172.31.240.43", bits: 32, want: true},
		{name: "loopback", address: "127.0.0.1", bits: 8, want: false},
		{name: "an IPv6 address", address: "fd07:b51a:cc66::1", bits: 64, want: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, hostAddress(netip.MustParseAddr(testCase.address), testCase.bits))
		})
	}
}

// A tunnel has one machine at the far end and no link to announce to.
func TestATunnelIsNotALink(t *testing.T) {
	t.Parallel()

	require.True(t, onALink(net.FlagUp|net.FlagMulticast|net.FlagBroadcast))
	require.False(t, onALink(net.FlagUp|net.FlagMulticast|net.FlagPointToPoint), "a VPN tunnel is announced to")
	require.False(t, onALink(net.FlagMulticast), "an interface that is down is announced to")
	require.False(t, onALink(net.FlagUp), "an interface without multicast is announced to")
}

// Docker's own bridges and container cables reach no machine on the network,
// and a bridge a person set up for the network is kept.
func TestDockerPlumbingIsNotALink(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"veth3c1a9f2", "docker0", "br-0123456789ab"} {
		require.True(t, dockerPlumbing(name), name)
	}

	for _, name := range []string{"br0", "br-lan", "br-0123456789abc", "eth0", "enp3s0", "wlan0"} {
		require.False(t, dockerPlumbing(name), name)
	}
}
