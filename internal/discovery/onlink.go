package discovery

import (
	"net"
	"net/netip"
	"sync"
	"time"
)

// prefixesFor is how long the filter keeps the addresses of an interface
// before it reads them again. An address a machine gains or loses is taken
// into account within that time.
const prefixesFor = 30 * time.Second

// linkFilter answers whether a packet came from the link of the interface it
// arrived on. mDNS is link-local, and RFC 6762 section 11 asks a responder to
// drop anything else: the socket listens on every address, so a packet
// routed in from another network would otherwise be read and answered.
type linkFilter struct {
	prefixes func(name string) ([]netip.Prefix, error)

	mu     sync.Mutex
	cached map[string][]netip.Prefix
	read   time.Time
}

func newLinkFilter(prefixes func(name string) ([]netip.Prefix, error)) *linkFilter {
	return &linkFilter{prefixes: prefixes, cached: make(map[string][]netip.Prefix)}
}

// allows reports whether a packet came from the link, as RFC 6762 section 11
// says. A packet sent to the mDNS group is on the link whatever its source,
// because no router forwards it. A packet sent to an address of this machine
// is on the link only when its source is an IPv6 link-local address or inside
// a prefix of the interface it arrived on.
//
// A system that does not report the destination, which is Windows, is judged
// by the second rule, and one that does not report the interface is judged
// against the prefixes of every interface.
func (f *linkFilter) allows(iface string, from, to netip.Addr) bool {
	to = to.WithZone("").Unmap()
	if to == groupIPv4 || to == groupIPv6 {
		return true
	}

	return f.fromLink(iface, from)
}

// fromLink reports whether a source is on the link of an interface: an IPv6
// link-local address, or an address inside a prefix of the interface. An
// answer goes back to the sender alone only when this holds, because the
// source of a packet sent to the group proves nothing.
func (f *linkFilter) fromLink(iface string, from netip.Addr) bool {
	if !from.IsValid() {
		return false
	}

	from = from.WithZone("").Unmap()

	if from.Is6() && from.IsLinkLocalUnicast() {
		return true
	}

	for _, prefix := range f.of(iface) {
		if prefix.Contains(from) {
			return true
		}
	}

	return false
}

// of is the prefixes of one interface, read again once they are older than
// prefixesFor.
func (f *linkFilter) of(iface string) []netip.Prefix {
	f.mu.Lock()
	defer f.mu.Unlock()

	if time.Since(f.read) > prefixesFor {
		clear(f.cached)
		f.read = time.Now()
	}

	if held, ok := f.cached[iface]; ok {
		return held
	}

	prefixes, err := f.prefixes(iface)
	if err != nil {
		prefixes = nil
	}

	f.cached[iface] = prefixes

	return prefixes
}

// interfacePrefixes is the prefixes of one interface of this machine, or of
// every interface that can send multicast when the name is empty. An
// interface without multicast is no link mDNS runs on.
func interfacePrefixes(name string) ([]netip.Prefix, error) {
	var addrs []net.Addr

	for _, iface := range multicastInterfaces() {
		if name != "" && iface.Name != name {
			continue
		}

		held, err := iface.Addrs()
		if err != nil {
			return nil, err
		}

		addrs = append(addrs, held...)
	}

	out := make([]netip.Prefix, 0, len(addrs))

	for _, addr := range addrs {
		if network, ok := addr.(*net.IPNet); ok {
			if prefix, valid := prefixOf(network); valid {
				out = append(out, prefix)
			}
		}
	}

	return out, nil
}

// The lengths of an IPv4 and an IPv6 address, in bits.
const (
	ipv4Bits = 32
	ipv6Bits = 128
)

// prefixOf is one interface address as a prefix. The standard library writes
// an IPv4 address in its 16-byte form with a 4-byte mask on some systems and a
// 16-byte mask on others, and both have to read as the same IPv4 prefix.
func prefixOf(network *net.IPNet) (netip.Prefix, bool) {
	ip, ok := netip.AddrFromSlice(network.IP)
	if !ok {
		return netip.Prefix{}, false
	}

	bits, total := network.Mask.Size()
	if total == 0 {
		return netip.Prefix{}, false
	}

	if ip.Is4In6() {
		ip = ip.Unmap()
	}

	if ip.Is4() && total == ipv6Bits {
		bits -= ipv6Bits - ipv4Bits
	}

	prefix := netip.PrefixFrom(ip, bits)
	if !prefix.IsValid() {
		return netip.Prefix{}, false
	}

	return prefix.Masked(), true
}
