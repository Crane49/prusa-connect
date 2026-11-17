// Package discovery announces this server on the local network.
//
// It sends mDNS, which is DNS carried to every machine on the same link, in the
// record shape DNS-SD gives a service. A client asks for a service name and
// gets back this server's host, port and a short list of key=value pairs. That
// is how a slicer finds a running instance without anybody typing an address.
//
// It is the only package that sends or reads mDNS, and a gate in
// internal/arch holds that.
package discovery

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"
)

// The three answers State gives, on the model of the web app's served, off and
// absent. They are separate because an operator who switched the announcer off
// and one whose machine refused the socket need different things done about it.
const (
	// StateAnnouncing is a responder answering for every configured name.
	StateAnnouncing = "announcing"
	// StateOff is core.discovery.enabled set to false.
	StateOff = "off"
	// StateFailed is a responder this machine would not start, one whose
	// every packet this machine refuses to send, or one that gave up because
	// other machines hold every name it may take. The core keeps serving, and
	// the log carries the reason.
	StateFailed = "failed"
	// StateHidden is a printer a person hid. It announces nothing, and it
	// still serves its page and its API at its address.
	StateHidden = "hidden"
)

// domain is the one domain mDNS answers for. Every name here ends in it.
const domain = "local"

// The TXT keys. A TXT record reaches every machine on the link, so it never
// carries a token, a password, a session or an account name.
const (
	// keyPath is where the API sits under the announced port.
	keyPath = "path"
	// keyVersion is the build this process reports about itself.
	keyVersion = "version"
	// keyAPI is the major version of the API served at that path.
	keyAPI = "api"
	// keyTLS says the announced port expects https. A plain listener leaves
	// it out, because every client reads a missing key as "no".
	keyTLS = "tls"
	// keyID is the id this server answers to on every address. It is random
	// and written once, so it tells a reader nothing about the machine, and it
	// lets a client that saw the server at another address know it again.
	keyID = "id"
)

// The fixed values behind three of those keys. Only the version and the TLS key
// vary, and each comes from the process, not from a configuration key.
const (
	// rootPath is the API's mount. The core serves it at the origin root.
	rootPath = "/"
	// apiVersion is the major version this server answers on.
	apiVersion = "v1"
	// tlsOn is the value the TLS key carries when it is there at all.
	tlsOn = "1"
)

// Config is one announcement, with every value already decided. The announcer
// reads no configuration key of its own: the composition root hands over what
// the record carries, so no secret can reach the record.
type Config struct {
	// Enabled is core.discovery.enabled. False announces nothing.
	Enabled bool
	// Services are the service names, one record for each. The loader has
	// already held every one of them to its shape.
	Services []string
	// Instance is the name a client shows a person, which is
	// core.frontend.title.
	Instance string
	// Version is the build this process reports, announced as the version key.
	Version string
	// ID is the id this server answers to, announced as the id key. Empty
	// leaves the key out.
	ID string
	// TLS reports whether the API listener expects https.
	TLS bool
	// SystemHost reports that another responder on this machine answers for
	// its host name, such as the one the operating system runs. The
	// announcement then holds the service records alone, each SRV naming that
	// host, and the host name is neither probed for nor renamed.
	SystemHost bool
}

// text is the TXT record this configuration announces.
func (c Config) text() map[string]string {
	text := map[string]string{
		keyPath:    rootPath,
		keyVersion: c.Version,
		keyAPI:     apiVersion,
	}

	if c.TLS {
		text[keyTLS] = tlsOn
	}

	if c.ID != "" {
		text[keyID] = c.ID
	}

	return text
}

// Announcer is the responder and its lifecycle. It has no lifecycle of its own:
// the composition root starts it once every listener is bound and stops it when
// the listeners drain.
//
// One goroutine owns the responder while the announcer runs. The socket is
// the link's, which the listener shares. Hiding the server sends a goodbye
// for each record and leaves the responder silent, and showing it probes and
// announces again. SetHidden hands that goroutine the setting through a
// channel it always drains.
type Announcer struct {
	config Config
	logger *slog.Logger
	// base is the host name the records start with, before any rename.
	base string
	// open joins the shared link, filter holds packets to the link, and
	// delay picks the random waits of RFC 6762. A test replaces all three.
	open   func() (network, error)
	filter *linkFilter
	delay  func(least, most time.Duration) time.Duration
	// muted says nothing the link sends leaves this machine. Nil reads as no.
	muted func() bool

	mu       sync.Mutex
	state    string
	hidden   bool
	host     string
	instance string
	port     int
	wanted   uint64
	applied  uint64
	// changed is closed and replaced each time the owner applies a change.
	changed chan struct{}
	// poke has one slot. SetHidden fills it, and the owner drains it and
	// reads the setting as it stands, so no change is lost and none waits.
	poke chan struct{}
	stop context.CancelFunc
	done chan struct{}
}

// network is the shared link as the announcer uses it. receive is every
// message from the link, interfacesChanged fires when the interfaces
// change, and close leaves the link.
type network interface {
	wire
	receive() <-chan packet
	interfacesChanged() <-chan struct{}
	interfaces() []announcedInterface
	close()
}

// New returns the announcer for one configuration, on the link it shares with
// the listener. It opens no socket: a failure that would stop the core has to
// happen after the listeners are bound, where it is a warning instead.
func New(config Config, logger *slog.Logger, link *Link) *Announcer {
	return &Announcer{
		config:  config,
		logger:  logger,
		base:    hostName(),
		open:    func() (network, error) { return link.join() },
		filter:  newLinkFilter(interfacePrefixes),
		delay:   randomDelay,
		muted:   link.muted,
		state:   StateOff,
		changed: make(chan struct{}),
		poke:    make(chan struct{}, 1),
	}
}

// State is what the readiness report says about the announcer. A nil receiver is
// a process that built none, which reads the same as the block switched off.
func (a *Announcer) State() string {
	if a == nil {
		return StateOff
	}

	a.mu.Lock()
	state := a.state
	a.mu.Unlock()

	// The link's lock is taken after the announcer's is released, so the two
	// are never held together.
	if state == StateAnnouncing && a.muted != nil && a.muted() {
		return StateFailed
	}

	return state
}

// Host is the host name this server's records carry on the wire. The responder
// renames it when another machine on the network already answers to the name,
// or with SystemHost takes the name the machine's own responder holds.
func (a *Announcer) Host() string {
	if a == nil {
		return hostName()
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.host == "" {
		return a.base
	}

	return a.host
}

// Description is what this server puts on the local network, or would put on
// it once shown again: the names a client reads, the port, the service names
// and the addresses its records carry.
type Description struct {
	// Instance is the name a client shows a person, after any rename.
	Instance string
	// Host is the host name under the .local domain, after any rename.
	Host string
	// Port is the API port the records point at. It is zero until the
	// announcer starts.
	Port int
	// Services are the service names, one record for each.
	Services []string
	// Addresses are this machine's addresses, read from the interfaces as they
	// stand now. The records carry them, or with SystemHost the machine's own
	// responder does.
	Addresses []netip.Addr
	// Bridged is true inside a container whose one network is a bridge, where
	// the records reach no machine on the real network.
	Bridged bool
}

// Describe is what this server announces. A process that built no announcer,
// and one whose announcement is switched off, announce nothing, so a reader
// is never shown a name nobody on the network hears.
func (a *Announcer) Describe() Description {
	if a == nil || !a.config.Enabled {
		return Description{}
	}

	interfaces := announcedInterfaces()

	a.mu.Lock()
	defer a.mu.Unlock()

	host, instance := a.host, a.instance
	if host == "" {
		host = a.base
	}

	if instance == "" {
		instance = a.config.Instance
	}

	return Description{
		Instance:  instance,
		Host:      host + "." + domain,
		Port:      a.port,
		Services:  append([]string(nil), a.config.Services...),
		Addresses: addressesOf(interfaces),
		Bridged:   bridgeWarning(inContainer(), interfaces) != "",
	}
}

// Start joins the link and announces the bound API port under every
// configured service name, unless the server is hidden.
//
// It never returns an error. A machine that will not answer mDNS is a machine
// this server still serves the API on, so a failure is logged and the core
// carries on.
func (a *Announcer) Start(ctx context.Context, port int) {
	if a == nil || !a.config.Enabled {
		return
	}

	over, err := a.open()
	if err != nil {
		a.failed(ctx, err)

		return
	}

	current := newResponder(a.config, a.base, port, over, a.filter, a.delay)
	current.setLinks(over.interfaces(), time.Now())
	running, stop := context.WithCancel(ctx)
	done := make(chan struct{})

	a.mu.Lock()
	a.stop, a.done = stop, done
	a.host, a.instance, a.port = current.host, current.instance, port
	hidden := a.hidden

	a.state = StateAnnouncing
	if hidden {
		a.state = StateHidden
	}
	a.mu.Unlock()

	if !hidden {
		a.announce(ctx, port)
	}

	go func() {
		defer close(done)
		defer over.close()

		a.run(running, port, current, over)
	}()
}

// run owns the responder until the context ends, and then sends a goodbye
// for every record that went out. Nothing in it waits on the network, so a
// change of the hidden setting is applied as soon as it arrives.
func (a *Announcer) run(ctx context.Context, port int, current *responder, over network) {
	timer := time.NewTimer(0)
	defer timer.Stop()

	// Start wrote the startup line for a server that starts visible, so the
	// first change writes none.
	say := false

	for {
		a.apply(ctx, port, current, say)
		say = true

		var wake <-chan time.Time

		if due := current.wake(); !due.IsZero() {
			timer.Reset(time.Until(due))
			wake = timer.C
		}

		select {
		case <-ctx.Done():
			current.hide()

			return
		case one := <-over.receive():
			a.safely(ctx, func() { current.receive(one, time.Now()) })
		case <-over.interfacesChanged():
			current.setLinks(over.interfaces(), time.Now())
		case <-wake:
			current.tick(time.Now())
		case <-a.poke:
		}
	}
}

// apply makes the responder follow the hidden setting as it stands, takes the
// host name it holds now, and marks the change applied.
func (a *Announcer) apply(ctx context.Context, port int, current *responder, say bool) {
	a.mu.Lock()
	hidden, wanted := a.hidden, a.wanted
	a.mu.Unlock()

	switch {
	case hidden && current.phase != silent:
		current.hide()
		a.logger.InfoContext(ctx, "this server is hidden, so it withdrew the local network announcement")
	case !hidden && current.phase == silent && !current.exhausted:
		current.show(time.Now())

		if say {
			a.announce(ctx, port)
		}
	}

	a.publish(ctx, current, hidden)

	a.mu.Lock()
	defer a.mu.Unlock()

	if wanted > a.applied {
		a.applied = wanted
		close(a.changed)
		a.changed = make(chan struct{})
	}
}

// publish takes what the responder holds now: its state, and the host name
// on the wire. A responder out of names has failed, and says so once.
func (a *Announcer) publish(ctx context.Context, current *responder, hidden bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	switch {
	case current.exhausted:
		if a.state != StateFailed {
			a.logger.WarnContext(ctx, "this server stopped announcing itself on the local network, "+
				"because other machines hold every name up to its last rename", "announced_as", current.instance)
		}

		a.state = StateFailed
	case hidden:
		a.state = StateHidden
	default:
		a.state = StateAnnouncing
	}

	if a.config.SystemHost && a.host != "" && a.host != current.host {
		a.logger.InfoContext(ctx, "the services now name the host the system's responder holds",
			"was", a.host+"."+domain, "host", current.host+"."+domain)
	}

	a.host, a.instance = current.host, current.instance
}

// SetHidden hides this server from the local network, or shows it again. A
// running announcer sends a goodbye for every record and stops answering, or
// probes and announces again. SetHidden waits for that while its context
// lasts, and the change is made whether or not the caller is still waiting.
// The wait is short, because the goroutine that makes the change never waits
// on the network. Before Start it sets the value Start begins with.
func (a *Announcer) SetHidden(ctx context.Context, hidden bool) {
	if a == nil {
		return
	}

	a.mu.Lock()
	a.hidden = hidden
	a.wanted++
	wanted, done := a.wanted, a.done
	a.mu.Unlock()

	if done == nil {
		return
	}

	select {
	case a.poke <- struct{}{}:
	default:
	}

	for {
		a.mu.Lock()
		applied, changed := a.applied, a.changed
		a.mu.Unlock()

		if applied >= wanted {
			return
		}

		select {
		case <-changed:
		case <-done:
			return
		case <-ctx.Done():
			return
		}
	}
}

// Hidden reports whether this server is hidden from the local network.
func (a *Announcer) Hidden() bool {
	if a == nil {
		return false
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	return a.hidden
}

// Stop withdraws every record, leaves the link and waits until every
// goroutine the announcer started has ended. The last user of the link closes
// its socket. It is called when the listeners drain, because a record pointing
// at a port nothing answers on is worse than no record.
func (a *Announcer) Stop(ctx context.Context) {
	if a == nil {
		return
	}

	a.mu.Lock()
	stop, done := a.stop, a.done
	a.stop, a.done = nil, nil
	a.mu.Unlock()

	if stop == nil {
		return
	}

	stop()
	<-done

	// The pair of the startup line. It is written before "listeners drained":
	// the records are withdrawn first, while the port is still open.
	a.logger.InfoContext(ctx, "withdrew the local network announcement")
}

// safely runs one step of the loop and turns a panic into a logged error, so
// one packet this server cannot read costs that packet and no more.
func (a *Announcer) safely(ctx context.Context, step func()) {
	defer func() {
		if caught := recover(); caught != nil {
			a.logger.ErrorContext(ctx, "the mdns responder dropped a packet it could not read",
				"error", fmt.Sprint(caught))
		}
	}()

	step()
}

// failed records the reason and leaves the core serving.
func (a *Announcer) failed(ctx context.Context, err error) {
	a.mu.Lock()
	a.state = StateFailed
	a.mu.Unlock()

	a.logger.WarnContext(ctx, "this server is not announcing itself on the local network",
		"error", err.Error(), "services", strings.Join(a.config.Services, ", "))
}

// hostName is the name the record points at: this machine's host name, which a
// client then asks for under the .local domain.
//
// Only the first label is kept. A machine that calls itself workshop.example
// answers for workshop.local on this link, and the responder appends the
// domain itself.
func hostName() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "prusa-connect"
	}

	label, _, _ := strings.Cut(name, ".")

	return label
}
