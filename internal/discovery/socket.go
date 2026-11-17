package discovery

import (
	"cmp"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// The mDNS groups and port of RFC 6762 section 3.
var (
	groupIPv4 = netip.MustParseAddr("224.0.0.251")
	groupIPv6 = netip.MustParseAddr("ff02::fb")
)

const (
	mdnsPort = 5353
	// mdnsHops is the hop limit RFC 6762 section 11 asks a sender to set.
	mdnsHops = 255
	// writeFor is how long one write may wait on one interface.
	writeFor = time.Second
	// maxPacket is the largest mDNS message RFC 6762 section 17 allows.
	maxPacket = 9000
)

// packet is one message read from the link.
type packet struct {
	msg  *dns.Msg
	from netip.AddrPort
	// to is the address the packet was sent to. It is not valid on a system
	// that does not report it, which is Windows.
	to netip.Addr
	// iface and index name the interface the packet arrived on. They are
	// empty and zero when no interface of this machine matches the packet.
	iface string
	index int
}

// socket is the two UDP sockets on port 5353, one for each address family.
// The link joins them to the mDNS group on each interface. Two goroutines
// read them, one for each family, and close waits for both.
type socket struct {
	v4 *ipv4.PacketConn
	v6 *ipv6.PacketConn

	// mu keeps one write or join at a time, because a multicast write first
	// picks the interface the packet leaves on.
	mu      sync.Mutex
	closed  sync.Once
	readers sync.WaitGroup
}

// openSocket opens both sockets. One family that this machine refuses is
// left out. Both refused is an error.
func openSocket() (*socket, error) {
	s := &socket{}

	var errs []error

	// A socket bound to a group address listens on every address of the
	// port, so unicast packets to port 5353 arrive here too.
	if conn, err := net.ListenUDP("udp4", udpAddr(groupIPv4)); err != nil {
		errs = append(errs, err)
	} else {
		s.v4 = ipv4.NewPacketConn(conn)
		// Windows reports neither the destination nor the interface, and the
		// link then finds the interface from the source address.
		_ = s.v4.SetControlMessage(ipv4.FlagDst|ipv4.FlagInterface, true)
		_ = s.v4.SetMulticastTTL(mdnsHops)
		_ = s.v4.SetTTL(mdnsHops)
		_ = s.v4.SetMulticastLoopback(true)
	}

	if conn, err := net.ListenUDP("udp6", udpAddr(groupIPv6)); err != nil {
		errs = append(errs, err)
	} else {
		s.v6 = ipv6.NewPacketConn(conn)
		_ = s.v6.SetControlMessage(ipv6.FlagDst|ipv6.FlagInterface, true)
		_ = s.v6.SetMulticastHopLimit(mdnsHops)
		_ = s.v6.SetHopLimit(mdnsHops)
		_ = s.v6.SetMulticastLoopback(true)
	}

	if s.v4 == nil && s.v6 == nil {
		return nil, fmt.Errorf("open the mdns socket: %w", errors.Join(errs...))
	}

	return s, nil
}

// read starts the two readers. Each hands every message that parses to
// deliver, until the socket is closed.
func (s *socket) read(deliver func(packet)) {
	if s.v4 != nil {
		s.readers.Go(func() { s.readIPv4(deliver) })
	}

	if s.v6 != nil {
		s.readers.Go(func() { s.readIPv6(deliver) })
	}
}

// families is the address families this socket has.
func (s *socket) families() []family {
	var out []family

	if s.v4 != nil {
		out = append(out, ipv4Family)
	}

	if s.v6 != nil {
		out = append(out, ipv6Family)
	}

	return out
}

// join adds one interface to the group of one family. The link keeps track
// of which families joined, because joining twice is an error.
func (s *socket) join(iface *net.Interface, which family) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case which == ipv4Family && s.v4 != nil:
		return s.v4.JoinGroup(iface, udpAddr(groupIPv4))
	case which == ipv6Family && s.v6 != nil:
		return s.v6.JoinGroup(iface, udpAddr(groupIPv6))
	default:
		return nil
	}
}

// leave takes one interface out of the group of one family. The interface
// may be gone already, so a failure is expected and not reported.
func (s *socket) leave(iface *net.Interface, which family) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case which == ipv4Family && s.v4 != nil:
		_ = s.v4.LeaveGroup(iface, udpAddr(groupIPv4))
	case which == ipv6Family && s.v6 != nil:
		_ = s.v6.LeaveGroup(iface, udpAddr(groupIPv6))
	}
}

// close closes both sockets and waits for the two readers to end.
func (s *socket) close() {
	s.closed.Do(func() {
		if s.v4 != nil {
			_ = s.v4.Close()
		}

		if s.v6 != nil {
			_ = s.v6.Close()
		}
	})

	s.readers.Wait()
}

func (s *socket) readIPv4(deliver func(packet)) {
	buf := make([]byte, maxPacket)

	for {
		n, cm, from, err := s.v4.ReadFrom(buf)
		if errors.Is(err, net.ErrClosed) {
			return
		}

		if err != nil {
			continue
		}

		one := packet{from: addrPortOf(from)}
		if cm != nil {
			one.to, _ = netip.AddrFromSlice(cm.Dst)
			one.index = cm.IfIndex
		}

		parse(buf[:n], one, deliver)
	}
}

func (s *socket) readIPv6(deliver func(packet)) {
	buf := make([]byte, maxPacket)

	for {
		n, cm, from, err := s.v6.ReadFrom(buf)
		if errors.Is(err, net.ErrClosed) {
			return
		}

		if err != nil {
			continue
		}

		one := packet{from: addrPortOf(from)}
		if cm != nil {
			one.to, _ = netip.AddrFromSlice(cm.Dst)
			one.index = cm.IfIndex
		}

		parse(buf[:n], one, deliver)
	}
}

// parse hands one message on. A message that does not parse is dropped.
func parse(raw []byte, one packet, deliver func(packet)) {
	msg := new(dns.Msg)
	if err := msg.Unpack(raw); err != nil {
		return
	}

	one.msg = msg
	one.to = one.to.Unmap()

	deliver(one)
}

// multicast sends one message to both groups on one interface. A family whose
// socket cannot pick that interface sends nothing, so no packet goes out on
// the wrong link.
//
// The answer is nil when a write went out or none was tried, and the first
// failed write otherwise. One failed write leaves the link one packet short,
// and the schedule sends the next one anyway. The link judges the answers
// over time, because a machine that refuses every write is a different thing.
func (s *socket) multicast(msg *dns.Msg, iface *net.Interface) error {
	packed, err := msg.Pack()
	if err != nil {
		return fmt.Errorf("pack an mdns message: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	sent := false

	var refused error

	note := func(_ int, err error) {
		if err == nil {
			sent = true
		}

		refused = cmp.Or(refused, err)
	}

	if s.v4 != nil && s.v4.SetMulticastInterface(iface) == nil {
		_ = s.v4.SetWriteDeadline(time.Now().Add(writeFor))
		note(s.v4.WriteTo(packed, nil, udpAddr(groupIPv4)))
	}

	if s.v6 != nil && s.v6.SetMulticastInterface(iface) == nil {
		_ = s.v6.SetWriteDeadline(time.Now().Add(writeFor))
		note(s.v6.WriteTo(packed, nil, udpAddr(groupIPv6)))
	}

	if sent {
		return nil
	}

	return refused
}

// unicast sends one message to one address.
func (s *socket) unicast(msg *dns.Msg, to netip.AddrPort) {
	packed, err := msg.Pack()
	if err != nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	target := net.UDPAddrFromAddrPort(to)

	switch {
	case to.Addr().Unmap().Is4() && s.v4 != nil:
		_ = s.v4.SetWriteDeadline(time.Now().Add(writeFor))
		_, _ = s.v4.WriteTo(packed, nil, target)
	case to.Addr().Is6() && s.v6 != nil:
		_ = s.v6.SetWriteDeadline(time.Now().Add(writeFor))
		_, _ = s.v6.WriteTo(packed, nil, target)
	}
}

// multicastInterfaces is every interface that is up, can send multicast, is
// not a point-to-point tunnel or Docker plumbing, and has an address.
func multicastInterfaces() []net.Interface {
	all, err := net.Interfaces()
	if err != nil {
		return nil
	}

	var out []net.Interface

	for _, iface := range all {
		if !onALink(iface.Flags) || dockerPlumbing(iface.Name) {
			continue
		}

		if addrs, failed := iface.Addrs(); failed == nil && len(addrs) > 0 {
			out = append(out, iface)
		}
	}

	return out
}

// onALink reports whether an interface with these flags is one mDNS reaches
// other machines on. A point-to-point interface is a tunnel, such as a VPN:
// it has one machine at the far end and no link to announce to, and the
// address a VPN gives this machine is one nobody on the local network opens.
func onALink(flags net.Flags) bool {
	return flags&net.FlagUp != 0 && flags&net.FlagMulticast != 0 && flags&net.FlagPointToPoint == 0
}

// dockerPlumbing reports whether Docker made this interface to connect
// containers: the host end of a container's cable (veth...), the default
// bridge docker0, and the bridge of a user network, br- and the first 12 hex
// digits of the network id. No machine on the network is on these links, and
// a host with many containers has more of them than the kernel lets one
// socket join a group on (net.ipv4.igmp_max_memberships, 20 by default). A
// bridge a person named, such as br0, is kept.
func dockerPlumbing(name string) bool {
	if strings.HasPrefix(name, "veth") || name == "docker0" {
		return true
	}

	id, bridge := strings.CutPrefix(name, "br-")

	return bridge && len(id) == dockerNetworkID && strings.Trim(id, "0123456789abcdef") == ""
}

// dockerNetworkID is how many hex digits of a network id Docker puts in the
// name of the network's bridge.
const dockerNetworkID = 12

// udpAddr is the mDNS port on one address.
func udpAddr(addr netip.Addr) *net.UDPAddr {
	return net.UDPAddrFromAddrPort(netip.AddrPortFrom(addr, mdnsPort))
}

// addrPortOf is the address and port a message came from. The IPv6 zone is
// kept, because a reply to a link-local address needs it.
func addrPortOf(from net.Addr) netip.AddrPort {
	udp, ok := from.(*net.UDPAddr)
	if !ok {
		return netip.AddrPort{}
	}

	addr := udp.AddrPort()

	return netip.AddrPortFrom(addr.Addr().Unmap(), addr.Port())
}
