package discovery

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// The query schedule of RFC 6762 section 5.2. The listener asks at once, then
// waits twice as long each time, up to an hour. A record that asks to be
// heard again earlier than that is asked for earlier, but never sooner than a
// second after the last question.
const (
	firstQueryInterval = time.Second
	maxQueryInterval   = time.Hour
	minQueryInterval   = time.Second
	queryGrowth        = 2
)

// ListenerConfig is what the listener asks for, with every value decided.
type ListenerConfig struct {
	// Enabled is core.discovery.enabled. The announcer and the listener are
	// switched off together.
	Enabled bool
	// Services are the service names the listener asks for, in order: a
	// machine that announces several is listed under the first.
	Services []string
	// Self is the host name this server's own records carry, which can change
	// when another machine on the network already uses the name. Nil is the
	// machine's own host name.
	Self func() string
}

// heard is one message from the link, the address and port it came from, the
// address it was sent to and the name of the interface it arrived on.
type heard struct {
	msg   *dns.Msg
	from  netip.AddrPort
	to    netip.Addr
	iface string
}

// link is the network side of the listener. The real one is the shared mDNS
// link, and a test hands in messages of its own.
type link interface {
	// listen joins the link and returns every message that arrives on it
	// until the context ends.
	listen(ctx context.Context) (<-chan heard, error)
	// ask puts one question for a service name on the network. The answers
	// arrive on the channel listen returned.
	ask(ctx context.Context, service string)
}

// Listener hears the other printers on the local network and keeps the list of
// them in memory. The list holds a name and an address for each one, and
// being on it grants nothing.
type Listener struct {
	config ListenerConfig
	logger *slog.Logger
	link   link
	filter *linkFilter

	mu     sync.Mutex
	table  *table
	full   bool
	quiet  bool
	port   int
	stop   context.CancelFunc
	done   chan struct{}
	asking sync.WaitGroup
}

// NewListener returns the listener for one configuration. It opens no socket
// until Start.
func NewListener(config ListenerConfig, logger *slog.Logger, link *Link) *Listener {
	return newListener(config, logger, &multicast{link: link}, newLinkFilter(interfacePrefixes))
}

func newListener(config ListenerConfig, logger *slog.Logger, over link, filter *linkFilter) *Listener {
	if config.Self == nil {
		config.Self = hostName
	}

	return &Listener{config: config, logger: logger, link: over, filter: filter, table: newTable(config.Services)}
}

// SetQuiet stops the listener asking, or lets it ask again. A hidden server
// keeps listening, and it sends no question, because a question carries its
// address to every machine on the network.
func (l *Listener) SetQuiet(quiet bool) {
	if l == nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.quiet = quiet
}

// Start joins the link and starts asking. port is this server's own API
// port, so its own announcement is not listed.
//
// A socket this machine refuses is a warning, and the list stays empty. The
// server keeps serving.
func (l *Listener) Start(ctx context.Context, port int) {
	if l == nil || !l.config.Enabled {
		return
	}

	running, stop := context.WithCancel(ctx)

	messages, err := l.link.listen(running)
	if err != nil {
		stop()
		l.logger.WarnContext(ctx, "this server does not hear the other printers on the local network",
			"error", err.Error())

		return
	}

	done := make(chan struct{})

	l.mu.Lock()
	l.port = port
	l.stop, l.done = stop, done
	l.mu.Unlock()

	go func() {
		defer close(done)

		l.run(running, messages)
		l.asking.Wait()

		// The link closes the channel once this listener has left it, so
		// nothing it started outlives Stop.
		for open := true; open; {
			_, open = <-messages
		}
	}()
}

// Stop leaves the link and waits for the listener to end. The list is
// emptied, because nothing keeps it current any more.
func (l *Listener) Stop(_ context.Context) {
	if l == nil {
		return
	}

	l.mu.Lock()
	stop, done := l.stop, l.done
	l.stop, l.done = nil, nil
	l.mu.Unlock()

	if stop == nil {
		return
	}

	stop()
	<-done

	l.mu.Lock()
	defer l.mu.Unlock()

	l.table = newTable(l.config.Services)
}

// Nearby is every printer the listener heard whose record is still alive,
// sorted by name.
func (l *Listener) Nearby() []Nearby {
	if l == nil {
		return []Nearby{}
	}

	host := l.config.Self()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.table.self = self{host: host, port: l.port}

	return l.table.list(time.Now())
}

// run reads the socket and asks on the schedule until the context ends.
func (l *Listener) run(ctx context.Context, messages <-chan heard) {
	interval := firstQueryInterval

	var asked time.Time

	next := time.Now()

	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case one, open := <-messages:
			if !open {
				return
			}

			l.safely(ctx, one)

			// A record that asks to be heard again before the next question
			// brings that question forward.
			if due := l.refreshDue(); !due.IsZero() && due.Before(next) {
				next = latest(due, asked.Add(minQueryInterval))
				timer.Reset(time.Until(next))
			}
		case <-timer.C:
			l.askAll(ctx)

			asked = time.Now()
			next = asked.Add(interval)
			interval = min(interval*queryGrowth, maxQueryInterval)

			if due := l.refreshDue(); !due.IsZero() && due.Before(next) {
				next = latest(due, asked.Add(minQueryInterval))
			}

			timer.Reset(time.Until(next))
		}
	}
}

// safely takes one message from the link, and turns a panic into a logged
// error, so one packet this server cannot read costs that packet and no
// more.
//
// Only a response from port 5353 is mDNS (RFC 6762 section 6). A query
// carries the asker's own records, and a probe carries records that nobody
// holds yet.
func (l *Listener) safely(ctx context.Context, one heard) {
	defer func() {
		if caught := recover(); caught != nil {
			l.logger.ErrorContext(ctx, "the mdns listener dropped a packet it could not read",
				"error", fmt.Sprint(caught))
		}
	}()

	if !one.msg.Response || one.from.Port() != mdnsPort || !l.filter.allows(one.iface, one.from.Addr(), one.to) {
		return
	}

	l.observe(ctx, one)
}

// observe puts one message into the list, and warns once when the list is
// full.
func (l *Listener) observe(ctx context.Context, one heard) {
	if l.take(one) {
		l.logger.WarnContext(ctx, fmt.Sprintf("the list of nearby printers is full at %d, "+
			"so a new printer is listed only after an old record expires", MaxNearby))
	}
}

// take puts one message into the list under the lock, and reports whether
// the list just became full. The lock is released in a defer, so a panic in
// the table leaves the list readable and Stop working.
func (l *Listener) take(one heard) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	refused := l.table.observe(one.msg, one.from.Addr(), time.Now())
	warn := refused > 0 && !l.full
	// The warning comes back once the list has had room again, so a list that
	// fills a second time is reported a second time.
	l.full = refused > 0 || (l.full && len(l.table.records) >= MaxNearby)

	return warn
}

// Quiet reports whether the listener asks nothing, which is what a hidden
// server's listener does.
func (l *Listener) Quiet() bool {
	if l == nil {
		return false
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	return l.quiet
}

// askAll asks for every service name. The questions run beside the loop, so
// the loop keeps reading the answers while a question is still open.
func (l *Listener) askAll(ctx context.Context) {
	l.mu.Lock()
	l.table.asked(time.Now())
	quiet := l.quiet
	l.mu.Unlock()

	if quiet {
		return
	}

	for _, service := range l.config.Services {
		l.asking.Go(func() { l.link.ask(ctx, service) })
	}
}

// refreshDue is the earliest moment a record asks to be heard again.
func (l *Listener) refreshDue() time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.table.refreshDue()
}

// latest is the later of two moments.
func latest(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}

	return b
}

// multicast is the real link: the process's shared mDNS socket, read for the
// answers and written for the questions.
type multicast struct {
	link *Link

	mu   sync.Mutex
	user *linkUser
}

func (m *multicast) listen(ctx context.Context) (<-chan heard, error) {
	user, err := m.link.join()
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.user = user
	m.mu.Unlock()

	out := make(chan heard)

	go func() {
		defer close(out)
		defer user.close()

		for {
			select {
			case <-ctx.Done():
				return
			case one := <-user.receive():
				message := heard{msg: one.msg, from: one.from, to: one.to, iface: one.iface}

				select {
				case out <- message:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return out, nil
}

// ask sends one question for a service name on every interface, from port
// 5353. A question from that port is answered by multicast (RFC 6762 section
// 6), so the answers reach the link.
func (m *multicast) ask(_ context.Context, service string) {
	m.mu.Lock()
	user := m.user
	m.mu.Unlock()

	if user == nil {
		return
	}

	question := new(dns.Msg)
	question.Question = []dns.Question{{Name: service + "." + domain + ".", Qtype: dns.TypePTR, Qclass: dns.ClassINET}}

	for _, iface := range user.interfaces() {
		user.multicast(question, iface.index)
	}
}
