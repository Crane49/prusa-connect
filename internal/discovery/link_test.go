package discovery

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/require"
)

// fakeSocket is a socket with both families that reads nothing and records
// what the link asks of it. A join of a family fails as many times as fail
// says for it, then succeeds. When room is set, a join past that many held
// ones fails, as Linux does past net.ipv4.igmp_max_memberships. When refuse
// is set, every multicast write fails, as macOS fails them for a process
// without Local Network permission.
type fakeSocket struct {
	mu     sync.Mutex
	fail   map[family]int
	room   int
	held   int
	refuse bool
	// through holds the interfaces whose writes go out even when refuse is
	// set, as macOS lets one out on loopback.
	through map[int]bool
	joins   []membership
	leaves  []membership
	sentOn  []int
}

func (s *fakeSocket) read(func(packet))  {}
func (s *fakeSocket) close()             {}
func (s *fakeSocket) families() []family { return []family{ipv4Family, ipv6Family} }

func (s *fakeSocket) join(iface *net.Interface, which family) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.joins = append(s.joins, membership{index: iface.Index, family: which})

	if s.fail[which] > 0 {
		s.fail[which]--

		return errors.New("the interface has no address of this family yet")
	}

	if s.room > 0 {
		if s.held == s.room {
			return errors.New("setsockopt: no buffer space available")
		}

		s.held++
	}

	return nil
}

func (s *fakeSocket) leave(iface *net.Interface, which family) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.leaves = append(s.leaves, membership{index: iface.Index, family: which})
}

func (s *fakeSocket) multicast(_ *dns.Msg, iface *net.Interface) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.refuse && !s.through[iface.Index] {
		return errors.New("sendto: no route to host")
	}

	s.sentOn = append(s.sentOn, iface.Index)

	return nil
}

func (s *fakeSocket) setRefuse(refuse bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.refuse = refuse
}

func (s *fakeSocket) unicast(*dns.Msg, netip.AddrPort) {}

// fakeLinkSource is a link on a fake socket, whose interfaces a case sets.
// It counts how often the socket was opened.
type fakeLinkSource struct {
	mu     sync.Mutex
	list   []announcedInterface
	opens  int
	socket *fakeSocket
}

func (f *fakeLinkSource) link() *Link {
	return f.linkWith(quiet(), nil)
}

func (f *fakeLinkSource) linkWith(logger *slog.Logger, dropped counter) *Link {
	link := NewLink(logger, dropped)
	link.open = func() (sockets, error) {
		f.mu.Lock()
		defer f.mu.Unlock()

		f.opens++

		if f.socket == nil {
			f.socket = &fakeSocket{}
		}

		return f.socket, nil
	}
	link.list = func() []announcedInterface {
		f.mu.Lock()
		defer f.mu.Unlock()

		return f.list
	}

	return link
}

func (f *fakeLinkSource) set(list []announcedInterface) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.list = list
}

// eth0 is one interface with its prefixes, as the link reads it.
var eth0 = announcedInterface{
	index:     deskIndex,
	name:      "eth0",
	addresses: []netip.Addr{netip.MustParseAddr("192.168.1.5"), netip.MustParseAddr("fe80::5")},
	prefixes:  []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24"), netip.MustParsePrefix("fe80::/64")},
}

// TestTheAnnouncerAndTheListenerShareOneSocket is the socket on port 5353.
// Two sockets bound to the port both hear every multicast packet, but a
// unicast packet reaches only one of them. So both users share one socket,
// and every packet reaches both.
func TestTheAnnouncerAndTheListenerShareOneSocket(t *testing.T) {
	t.Parallel()

	source := &fakeLinkSource{list: []announcedInterface{eth0}}
	link := source.link()

	announcer, err := link.join()
	require.NoError(t, err)

	listener, err := link.join()
	require.NoError(t, err)

	link.deliver(packet{
		msg: question("desk.local.", 1), from: netip.MustParseAddrPort("192.168.1.20:5353"),
		to: netip.MustParseAddr("192.168.1.5"), index: deskIndex,
	})

	for _, user := range []*linkUser{announcer, listener} {
		select {
		case one := <-user.receive():
			require.Equal(t, "eth0", one.iface)
		default:
			t.Fatal("a user of the link did not get the unicast packet")
		}
	}

	announcer.close()
	listener.close()
	require.Equal(t, 1, source.opens)
}

// TestTheLinkFollowsTheInterfaces is a network that comes up after start and
// one that goes. The link reads the interfaces again on its schedule, joins
// and leaves the group to match, and tells each user that the list changed.
func TestTheLinkFollowsTheInterfaces(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		source := &fakeLinkSource{}
		link := source.link()

		user, err := link.join()
		require.NoError(t, err)

		defer user.close()

		require.Empty(t, user.interfaces())

		source.set([]announcedInterface{eth0})
		time.Sleep(linkWatch)
		synctest.Wait()

		select {
		case <-user.interfacesChanged():
		default:
			t.Fatal("the link did not say the interfaces changed")
		}

		require.Equal(t, []announcedInterface{eth0}, user.interfaces())
		require.ElementsMatch(t, bothFamilies(deskIndex), source.socket.joins)

		source.set(nil)
		time.Sleep(linkWatch)
		synctest.Wait()

		<-user.interfacesChanged()
		require.Empty(t, user.interfaces())
		require.ElementsMatch(t, bothFamilies(deskIndex), source.socket.leaves)
	})
}

// TestAFailedJoinIsTriedAgainAndLoggedOnce is Windows, where an IPv4 join
// fails until the interface has an address. The link tries the IPv4 join
// again on each reading until it holds, and says so once. The IPv6 family,
// which joined at once, is not joined a second time.
func TestAFailedJoinIsTriedAgainAndLoggedOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		logged := &countingHandler{}
		source := &fakeLinkSource{
			list:   []announcedInterface{eth0},
			socket: &fakeSocket{fail: map[family]int{ipv4Family: 2, ipv6Family: 0}},
		}
		link := source.linkWith(slog.New(logged), nil)

		user, err := link.join()
		require.NoError(t, err)

		defer user.close()

		time.Sleep(3 * linkWatch)
		synctest.Wait()

		v4 := membership{index: deskIndex, family: ipv4Family}
		v6 := membership{index: deskIndex, family: ipv6Family}
		require.Equal(t, []membership{v4, v6, v4, v4}, source.socket.joins,
			"a family was joined again after it held, or a failed one was not tried again")
		require.Equal(t, 1, logged.count())
	})
}

// TestALinkWhosePacketsNeverLeaveSaysSoOnce is a process the operating
// system lets open the socket and send nothing on, as macOS does without
// Local Network permission. The link warns once and reports itself mute, and
// the first packet that goes out ends both.
func TestALinkWhosePacketsNeverLeaveSaysSoOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		logged := &countingHandler{}
		lo0 := announcedInterface{
			index: 1, name: "lo0", addresses: []netip.Addr{netip.MustParseAddr("fe80::1")}, loopback: true,
		}
		source := &fakeLinkSource{
			list:   []announcedInterface{lo0, eth0},
			socket: &fakeSocket{refuse: true, through: map[int]bool{lo0.index: true}},
		}
		link := source.linkWith(slog.New(logged), nil)

		user, err := link.join()
		require.NoError(t, err)

		defer user.close()

		for range 3 {
			user.multicast(new(dns.Msg), lo0.index)
			user.multicast(new(dns.Msg), deskIndex)
			time.Sleep(linkWatch)
			synctest.Wait()
		}

		require.True(t, link.muted(), "a link that sent nothing does not say so")
		require.Equal(t, 1, logged.count(), "the refused packets were not warned of exactly once")

		source.socket.setRefuse(false)
		user.multicast(new(dns.Msg), deskIndex)

		require.False(t, link.muted(), "a packet went out and the link still says none does")
		require.EqualValues(t, 1, logged.infos.Load(), "the first packet that went out was not logged")
	})
}

// TestALinkWhosePacketsGoOutIsNeverMute holds the other side: a write that
// fails after one went out is one packet short, and nothing more.
func TestALinkWhosePacketsGoOutIsNeverMute(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		logged := &countingHandler{}
		source := &fakeLinkSource{list: []announcedInterface{eth0}, socket: &fakeSocket{}}
		link := source.linkWith(slog.New(logged), nil)

		user, err := link.join()
		require.NoError(t, err)

		defer user.close()

		user.multicast(new(dns.Msg), deskIndex)
		source.socket.setRefuse(true)
		user.multicast(new(dns.Msg), deskIndex)

		time.Sleep(2 * linkWatch)
		synctest.Wait()

		require.False(t, link.muted())
		require.Zero(t, logged.count())
	})
}

// TestManyFailedJoinsShareOneWarning is a Docker host with more interfaces
// than the kernel lets the socket join the group on. Every join past the
// limit fails on each reading, and the log gets one warning for all of them.
func TestManyFailedJoinsShareOneWarning(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var many []announcedInterface

		for index := range 30 {
			many = append(many, announcedInterface{index: index + 1, name: "eth" + strconv.Itoa(index)})
		}

		logged := &countingHandler{}
		source := &fakeLinkSource{list: many, socket: &fakeSocket{room: 20}}
		link := source.linkWith(slog.New(logged), nil)

		user, err := link.join()
		require.NoError(t, err)

		defer user.close()

		time.Sleep(3 * linkWatch)
		synctest.Wait()

		require.Equal(t, 1, logged.count())
	})
}

// TestAHalfJoinedInterfaceIsStillLeft is an interface where only one family
// joined the group. When the interface goes, that family is left.
func TestAHalfJoinedInterfaceIsStillLeft(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		source := &fakeLinkSource{
			list:   []announcedInterface{eth0},
			socket: &fakeSocket{fail: map[family]int{ipv4Family: 1000, ipv6Family: 0}},
		}
		link := source.link()

		user, err := link.join()
		require.NoError(t, err)

		defer user.close()

		source.set(nil)
		time.Sleep(linkWatch)
		synctest.Wait()

		require.Equal(t, []membership{{index: deskIndex, family: ipv6Family}}, source.socket.leaves)
	})
}

// bothFamilies is one interface's membership of both groups.
func bothFamilies(index int) []membership {
	return []membership{{index: index, family: ipv4Family}, {index: index, family: ipv6Family}}
}

// TestAGoodbyeStillLeavesOnALinkThatWent is the goodbye for an interface
// that just left the list. It goes out on that interface's index, where the
// system still has the interface.
func TestAGoodbyeStillLeavesOnALinkThatWent(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		source := &fakeLinkSource{list: []announcedInterface{eth0}}
		link := source.link()

		user, err := link.join()
		require.NoError(t, err)

		defer user.close()

		source.set(nil)
		time.Sleep(linkWatch)
		synctest.Wait()

		require.Empty(t, user.interfaces())

		user.multicast(new(dns.Msg), deskIndex)
		require.Equal(t, []int{deskIndex}, source.socket.sentOn)
	})
}

// TestTheWatchNeverOutlivesTheSocket is the last user leaving at the moment
// the watch reads the interfaces. The watch is stopped and waited for before
// the socket goes, and a reading with no socket does nothing.
func TestTheWatchNeverOutlivesTheSocket(t *testing.T) {
	t.Parallel()

	closed := (&fakeLinkSource{list: []announcedInterface{eth0}}).link()
	closed.mu.Lock()
	closed.refresh()
	closed.mu.Unlock()

	synctest.Test(t, func(t *testing.T) {
		link := (&fakeLinkSource{list: []announcedInterface{eth0}}).link()

		for range 200 {
			user, err := link.join()
			require.NoError(t, err)

			time.Sleep(linkWatch)
			user.close()
		}
	})
}

// TestAPacketWithNoInterfaceIsPlacedByItsSource is Windows, which reports no
// interface for a packet, and a machine's packet to itself, which arrives on
// the loopback. The link takes the interface whose prefix holds the source,
// or the one an IPv6 zone names. A packet that matches no interface is
// dropped, because nothing could be answered on its link.
func TestAPacketWithNoInterfaceIsPlacedByItsSource(t *testing.T) {
	t.Parallel()

	link := (&fakeLinkSource{list: []announcedInterface{eth0}}).link()

	user, err := link.join()
	require.NoError(t, err)

	defer user.close()

	for _, from := range []string{"192.168.1.20:5353", "[fe80::9%eth0]:5353", "[fe80::9%2]:5353"} {
		for _, index := range []int{0, 1} {
			link.deliver(packet{msg: new(dns.Msg), from: netip.MustParseAddrPort(from), index: index})

			select {
			case one := <-user.receive():
				require.Equal(t, deskIndex, one.index, from)
				require.Equal(t, "eth0", one.iface, from)
			default:
				t.Fatalf("a packet from %s on index %d was not delivered", from, index)
			}
		}
	}

	link.deliver(packet{msg: new(dns.Msg), from: netip.MustParseAddrPort("203.0.113.7:5353")})

	select {
	case one := <-user.receive():
		t.Fatalf("a packet from no link of this machine was delivered: %v", one.from)
	default:
	}
}

// TestADroppedPacketIsCounted is a user that does not read. Past its backlog
// every packet is dropped for it and counted.
func TestADroppedPacketIsCounted(t *testing.T) {
	t.Parallel()

	dropped := &countingCounter{}
	link := (&fakeLinkSource{list: []announcedInterface{eth0}}).linkWith(quiet(), dropped)

	user, err := link.join()
	require.NoError(t, err)

	defer user.close()

	for range backlog + 5 {
		link.deliver(packet{msg: new(dns.Msg), from: netip.MustParseAddrPort("192.168.1.20:5353"), index: deskIndex})
	}

	require.InDelta(t, 5, dropped.total(), 0)
}

// countingCounter is a metric a test reads back.
type countingCounter struct {
	mu    sync.Mutex
	value float64
}

func (c *countingCounter) Add(delta float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.value += delta
}

func (c *countingCounter) total() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.value
}

// countingHandler is a log handler that counts each level it cares about.
type countingHandler struct {
	infos    atomic.Int64
	warnings atomic.Int64
	errors   atomic.Int64
}

func (h *countingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *countingHandler) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *countingHandler) WithGroup(string) slog.Handler            { return h }

// count is the warnings so far.
func (h *countingHandler) count() int {
	return int(h.warnings.Load())
}

func (h *countingHandler) Handle(_ context.Context, record slog.Record) error {
	switch {
	case record.Level >= slog.LevelError:
		h.errors.Add(1)
	case record.Level >= slog.LevelWarn:
		h.warnings.Add(1)
	case record.Level >= slog.LevelInfo:
		h.infos.Add(1)
	}

	return nil
}
