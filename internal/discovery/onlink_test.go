package discovery

import (
	"errors"
	"net"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

// workshopLink is one interface with an IPv4 subnet and an IPv6 prefix.
func workshopLink(name string) ([]netip.Prefix, error) {
	if name != "eth0" {
		return nil, errors.New("no such interface")
	}

	return []netip.Prefix{
		netip.MustParsePrefix("192.168.1.0/24"),
		netip.MustParsePrefix("2001:db8:1::/64"),
	}, nil
}

// TestAPacketFromOffTheLinkIsDropped is RFC 6762 section 11: a responder and a
// listener take a packet sent to the mDNS group from any source, because no
// router forwards it. A packet sent to this machine's own address is taken
// only from a source on the link it arrived on. mDNS is link-local, so
// anything else was routed here on purpose.
func TestAPacketFromOffTheLinkIsDropped(t *testing.T) {
	t.Parallel()

	filter := newLinkFilter(workshopLink)

	own := netip.MustParseAddr("192.168.1.5")
	mappedGroup := netip.MustParseAddr("::ffff:224.0.0.251")

	cases := map[string]struct {
		iface string
		from  string
		to    netip.Addr
		want  bool
	}{
		"an address in the interface's subnet":          {"eth0", "192.168.1.20", own, true},
		"an IPv4 address from another network":          {"eth0", "203.0.113.7", own, false},
		"an IPv6 link-local address":                    {"eth0", "fe80::1", own, true},
		"an IPv6 address in the on-link prefix":         {"eth0", "2001:db8:1::20", own, true},
		"an IPv6 address from another network":          {"eth0", "2001:db8:9::20", own, false},
		"an interface this machine does not have":       {"wlan9", "192.168.1.20", own, false},
		"an IPv4 address written in the IPv6 form":      {"eth0", "::ffff:192.168.1.20", own, true},
		"an IPv4 link-local address sent to this host":  {"eth0", "169.254.3.4", own, false},
		"an IPv4 link-local address sent to the group":  {"eth0", "169.254.3.4", groupIPv4, true},
		"a second subnet on the link sent to the group": {"eth0", "10.9.9.9", groupIPv4, true},
		"another IPv6 network sent to the IPv6 group":   {"eth0", "2001:db8:9::20", groupIPv6, true},
		"the group written in the IPv6 form":            {"eth0", "10.9.9.9", mappedGroup, true},
		"another network, with no destination reported": {"eth0", "203.0.113.7", netip.Addr{}, false},
		"the subnet, with no destination reported":      {"eth0", "192.168.1.20", netip.Addr{}, true},
	}

	for name, one := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, one.want, filter.allows(one.iface, netip.MustParseAddr(one.from), one.to))
		})
	}

	require.False(t, filter.allows("eth0", netip.Addr{}, own), "a packet with no source address")
}

// TestAnInterfaceAddressReadsAsItsPrefix is the address form Linux hands back:
// an IPv4 address in 16 bytes with a 4-byte mask. Another system writes the
// mask in 16 bytes, and both are the same prefix.
func TestAnInterfaceAddressReadsAsItsPrefix(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		network *net.IPNet
		want    string
	}{
		"a 16-byte address with a 4-byte mask": {
			&net.IPNet{IP: net.ParseIP("172.31.0.4"), Mask: net.CIDRMask(16, 32)}, "172.31.0.0/16",
		},
		"a 16-byte address with a 16-byte mask": {
			&net.IPNet{IP: net.ParseIP("172.31.0.4"), Mask: net.CIDRMask(112, 128)}, "172.31.0.0/16",
		},
		"a 4-byte address": {
			&net.IPNet{IP: net.ParseIP("192.168.1.20").To4(), Mask: net.CIDRMask(24, 32)}, "192.168.1.0/24",
		},
		"an IPv6 address": {
			&net.IPNet{IP: net.ParseIP("2001:db8:1::20"), Mask: net.CIDRMask(64, 128)}, "2001:db8:1::/64",
		},
	}

	for name, one := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			prefix, ok := prefixOf(one.network)
			require.True(t, ok)
			require.Equal(t, netip.MustParsePrefix(one.want), prefix)
		})
	}
}
