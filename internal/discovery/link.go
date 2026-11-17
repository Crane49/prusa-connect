package discovery

import (
	"cmp"
	"context"
	"log/slog"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// linkWatch is how often the link reads this machine's interfaces again. A
// network that comes up after start is joined and announced on within that
// time, and a join that failed is tried again.
const linkWatch = 10 * time.Second

// backlog is how many packets one user of the link may leave unread. A packet
// that finds no room is dropped for that user alone and counted, so a slow
// listener never holds the responder back.
const backlog = 64

// counter is the one method the link needs of a metric.
type counter interface {
	Add(delta float64)
}

// sockets is the socket as the link uses it. The real one is socket, and a
// test hands in one of its own.
type sockets interface {
	read(deliver func(packet))
	families() []family
	join(iface *net.Interface, which family) error
	leave(iface *net.Interface, which family)
	multicast(msg *dns.Msg, iface *net.Interface) error
	unicast(msg *dns.Msg, to netip.AddrPort)
	close()
}

// Link is the mDNS socket of this process, shared by the announcer and the
// listener. The first user opens it and the last one closes it. One socket
// for each address family matters: two sockets bound to port 5353 both get
// every multicast packet, but a unicast packet reaches only one of them.
//
// While it is open, the link reads the interfaces every linkWatch. It joins
// the mDNS group on an interface that appeared, tries again where a join
// failed, leaves the group on one that went, and tells every user that the
// list changed.
type Link struct {
	logger  *slog.Logger
	dropped counter
	// open opens the socket, and list reads the interfaces. A test replaces
	// both.
	open func() (sockets, error)
	list func() []announcedInterface

	// life keeps one join or leave at a time, so the socket of one user
	// generation is never closed under the next.
	life sync.Mutex

	mu      sync.Mutex
	socket  sockets
	current []announcedInterface
	// joined holds each interface and family the group is joined on, and
	// warned the interfaces whose failed join was already logged.
	joined map[membership]bool
	warned map[int]bool
	// sent says a multicast packet went out since the socket opened, and
	// refused is a write that failed before one did. mute says the refresh
	// found nothing sent and a write refused, and logged it.
	sent    bool
	refused error
	mute    bool
	users   map[*linkUser]struct{}
	stop    chan struct{}
	watched chan struct{}
}

// NewLink returns a link that opens no socket until its first user joins.
// dropped counts the packets a user had no room for.
func NewLink(logger *slog.Logger, dropped counter) *Link {
	if dropped == nil {
		dropped = uncounted{}
	}

	return &Link{
		logger:  logger,
		dropped: dropped,
		open:    func() (sockets, error) { return openSocket() },
		list:    announcedInterfaces,
		users:   make(map[*linkUser]struct{}),
	}
}

// join adds one user. The first one opens the socket, joins the group on
// every interface and starts the readers and the watch.
func (l *Link) join() (*linkUser, error) {
	l.life.Lock()
	defer l.life.Unlock()

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.users) == 0 {
		opened, err := l.open()
		if err != nil {
			return nil, err
		}

		stop, watched := make(chan struct{}), make(chan struct{})
		l.socket, l.stop, l.watched = opened, stop, watched
		l.current, l.joined, l.warned = nil, make(map[membership]bool), make(map[int]bool)
		l.sent, l.refused, l.mute = false, nil, false
		l.refresh()
		opened.read(l.deliver)

		go func() {
			defer close(watched)

			l.watch(stop)
		}()
	}

	user := &linkUser{link: l, packets: make(chan packet, backlog), changed: make(chan struct{}, 1)}
	l.users[user] = struct{}{}

	return user, nil
}

// leave removes one user. The last one stops the watch and waits for it,
// and only then closes the socket and waits for its readers.
func (l *Link) leave(user *linkUser) {
	l.life.Lock()
	defer l.life.Unlock()

	l.mu.Lock()

	if _, ok := l.users[user]; !ok || len(l.users) > 1 {
		delete(l.users, user)
		l.mu.Unlock()

		return
	}

	delete(l.users, user)
	stop, watched := l.stop, l.watched
	l.mu.Unlock()

	close(stop)
	<-watched

	l.mu.Lock()
	opened := l.socket
	l.socket, l.stop, l.watched, l.current = nil, nil, nil, nil
	l.mu.Unlock()

	opened.close()
}

// watch reads the interfaces again every linkWatch until stop closes.
func (l *Link) watch(stop <-chan struct{}) {
	ticker := time.NewTicker(linkWatch)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			l.mu.Lock()
			l.refresh()
			l.mu.Unlock()
		}
	}
}

// refresh reads the interfaces, joins and leaves the group to match, and
// tells every user when the list changed. A join that fails is tried again
// on the next refresh. The interfaces whose join failed for the first time
// share one warning, so a host with many interfaces logs one line and not
// one for each. The caller holds mu.
func (l *Link) refresh() {
	if l.socket == nil {
		return
	}

	fresh := l.list()

	var failed []string

	var firstErr error

	for _, one := range fresh {
		for _, which := range l.socket.families() {
			if err := l.joinOne(one, which); err != nil {
				failed = append(failed, one.name)
				firstErr = cmp.Or(firstErr, err)
			}
		}
	}

	if len(failed) > 0 {
		l.logger.WarnContext(context.Background(), "this server could not join the mdns group on "+
			"some interfaces, and tries again", "interfaces", len(failed), "first", failed[0],
			"error", firstErr.Error())
	}

	present := func(index int) bool {
		return slices.ContainsFunc(fresh, func(one announcedInterface) bool { return one.index == index })
	}

	// Each family that joined is left on its own, so an interface where only
	// one family joined still gets its leave.
	for member := range l.joined {
		if !present(member.index) {
			l.socket.leave(&net.Interface{Index: member.index}, member.family)
			delete(l.joined, member)
		}
	}

	for index := range l.warned {
		if !present(index) {
			delete(l.warned, index)
		}
	}

	if !l.sent && l.refused != nil && !l.mute {
		l.mute = true
		l.logger.WarnContext(context.Background(), "this machine refuses every mdns packet this server sends, "+
			"so no slicer finds it and printers nearby may stay empty. On macOS, allow the app that started "+
			"it under Privacy & Security, Local Network", "error", l.refused.Error())
	}

	if slices.EqualFunc(l.current, fresh, sameInterface) {
		return
	}

	l.current = fresh

	for user := range l.users {
		select {
		case user.changed <- struct{}{}:
		default:
		}
	}
}

// joinOne joins the group for one family on one interface, unless it has
// joined already. It returns the error of the first failure on the
// interface, which it also logs at debug level with the interface's name, and
// nil for a join that held or a failure already returned. The caller holds mu.
func (l *Link) joinOne(one announcedInterface, which family) error {
	member := membership{index: one.index, family: which}
	if l.joined[member] {
		return nil
	}

	err := l.socket.join(&net.Interface{Index: one.index, Name: one.name}, which)
	if err == nil {
		l.joined[member] = true

		return nil
	}

	if l.warned[one.index] {
		return nil
	}

	l.warned[one.index] = true
	l.logger.DebugContext(context.Background(), "this server could not join the mdns group on an interface",
		"interface", one.name, "error", err.Error())

	return err
}

// deliver hands one packet to every user. It names the interface the packet
// arrived on first, and drops a packet that no interface of this machine
// matches, because nothing could be answered on its link.
func (l *Link) deliver(one packet) {
	l.mu.Lock()
	placed := l.place(&one)
	users := make([]*linkUser, 0, len(l.users))

	for user := range l.users {
		users = append(users, user)
	}
	l.mu.Unlock()

	if !placed {
		return
	}

	for _, user := range users {
		select {
		case user.packets <- one:
		default:
			l.dropped.Add(1)
		}
	}
}

// place fills in the interface of a packet and reports whether one matched.
// It takes the index the system reported. Where there is none, or it names
// an interface that is not announced on, such as the loopback a machine's
// packet to itself arrives on, it takes the interface whose prefix holds the
// source, or the one an IPv6 zone names. The caller holds mu.
func (l *Link) place(one *packet) bool {
	from := one.from.Addr()
	zone := from.Zone()
	from = from.WithZone("").Unmap()

	for _, byIndex := range []bool{true, false} {
		for _, iface := range l.current {
			switch {
			case byIndex && iface.index == one.index:
			case !byIndex && zone != "" && (zone == iface.name || zone == strconv.Itoa(iface.index)):
			case !byIndex && slices.ContainsFunc(iface.prefixes, func(p netip.Prefix) bool { return p.Contains(from) }):
			default:
				continue
			}

			one.index, one.iface = iface.index, iface.name

			return true
		}
	}

	return false
}

// targets is the interfaces a multicast on one index leaves on. It is that
// index even once the interface has left the list, so a goodbye still goes
// out where the system still has the interface.
func (l *Link) targets(index int) []*net.Interface {
	if index <= 0 {
		return nil
	}

	target := &net.Interface{Index: index}

	for _, iface := range l.current {
		if iface.index == index {
			target.Name = iface.name

			if iface.loopback {
				target.Flags = net.FlagLoopback
			}
		}
	}

	return []*net.Interface{target}
}

// interfaces is the list as the link last read it.
func (l *Link) interfaces() []announcedInterface {
	l.mu.Lock()
	defer l.mu.Unlock()

	return slices.Clone(l.current)
}

// linkUser is one user of the link: the announcer or the listener.
type linkUser struct {
	link    *Link
	packets chan packet
	changed chan struct{}
}

func (u *linkUser) receive() <-chan packet             { return u.packets }
func (u *linkUser) interfacesChanged() <-chan struct{} { return u.changed }
func (u *linkUser) interfaces() []announcedInterface   { return u.link.interfaces() }
func (u *linkUser) close()                             { u.link.leave(u) }

// multicast sends one message on one interface.
func (u *linkUser) multicast(msg *dns.Msg, index int) {
	u.link.mu.Lock()
	opened := u.link.socket
	targets := u.link.targets(index)
	u.link.mu.Unlock()

	if opened == nil {
		return
	}

	for _, iface := range targets {
		err := opened.multicast(msg, iface)

		// A packet on loopback reaches only this machine, and macOS lets one
		// out where it refuses every other, so it does not count as sent.
		if iface.Flags&net.FlagLoopback == 0 {
			u.link.wrote(err)
		}
	}
}

// wrote records how one multicast write went. Only the writes before the
// first that goes out count: after it, a failed one is one packet short.
func (l *Link) wrote(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	switch {
	case l.sent:
	case err != nil:
		l.refused = err
	default:
		l.sent = true

		if l.mute {
			l.mute = false
			l.logger.InfoContext(context.Background(), "this server's mdns packets leave this machine now")
		}
	}
}

// muted reports whether the link found that nothing it sends leaves this
// machine.
func (l *Link) muted() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.mute
}

// unicast sends one message to one address.
func (u *linkUser) unicast(msg *dns.Msg, to netip.AddrPort) {
	u.link.mu.Lock()
	opened := u.link.socket
	u.link.mu.Unlock()

	if opened != nil {
		opened.unicast(msg, to)
	}
}

// family is an address family of the socket: IPv4 or IPv6.
type family int

const (
	ipv4Family family = 4
	ipv6Family family = 6
)

// membership is one interface's membership of one family's group.
type membership struct {
	index  int
	family family
}

// sameInterface reports whether two readings of an interface agree.
func sameInterface(a, b announcedInterface) bool {
	return a.index == b.index && a.name == b.name && slices.Equal(a.addresses, b.addresses)
}

// uncounted is a counter that counts nothing, for a link built without one.
type uncounted struct{}

func (uncounted) Add(float64) {}
