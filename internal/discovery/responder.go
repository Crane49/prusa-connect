package discovery

import (
	"math"
	"math/rand/v2"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// The times to live of RFC 6762 section 10. A record that names a host or
// carries an address lives two minutes, and any other record 75 minutes.
const (
	hostTimeToLive    = 120
	serviceTimeToLive = 4500
	// legacyTimeToLive is the most a reply to a plain DNS client may carry
	// (RFC 6762 section 6.7).
	legacyTimeToLive = 10
)

// The schedule of RFC 6762 sections 6, 8 and 9.
const (
	// probeDelay is the most a responder waits before its first probe, so
	// machines that start together do not probe at the same moment.
	probeDelay = 250 * time.Millisecond
	// probes are sent one probeInterval apart before a name is claimed.
	probes        = 3
	probeInterval = 250 * time.Millisecond
	// announcements are sent one announceInterval apart once it is claimed.
	announcements    = 2
	announceInterval = time.Second
	// answerInterval is the least time between two answers with one record
	// to one place, and probeAnswerInterval is the same for an answer to a
	// probe.
	answerInterval      = time.Second
	probeAnswerInterval = 250 * time.Millisecond
	// A multicast answer that holds a shared record waits between
	// sharedLeast and sharedMost, so the answers of several machines to one
	// question do not collide.
	sharedLeast = 20 * time.Millisecond
	sharedMost  = 120 * time.Millisecond
	// lostTieWait is how long a responder that lost a tie with another
	// prober waits before it probes again.
	lostTieWait = time.Second
	// A responder that meets conflictBurst conflicts in conflictWindow waits
	// conflictPause before it probes again, so a peer that keeps claiming
	// the name cannot keep it probing.
	conflictBurst  = 15
	conflictWindow = 10 * time.Second
	conflictPause  = 5 * time.Second
)

// The limits on one packet. A packet may hold far more than any real
// question needs, and the work it causes has to stay small whatever it holds.
const (
	// maxQuestions is the most questions of one query the responder reads.
	maxQuestions = 64
	// maxRecords is the most records of one packet the responder reads.
	maxRecords = 256
	// maxRenames is the most times a name is renamed. A conflict on the last
	// name makes the responder give up: it stops announcing, logs it once,
	// and the readiness report says failed.
	maxRenames = 99
	// maxWaiting is the most delayed answers the responder holds.
	maxWaiting = 32
	// maxSent is the most recent answers the responder remembers. Past it the
	// oldest is forgotten, so the record costs the same whatever arrives.
	maxSent = 1024
)

// topBit is the cache-flush bit in the class of a unique record (RFC 6762
// section 10.2), and the unicast-response bit in the class of a question
// (section 5.4).
const topBit = 1 << 15

// servicesName lists every service type on the link (RFC 6763 section 9).
const servicesName = "_services._dns-sd._udp." + domain + "."

// phase is where the responder stands with its names.
type phase int

const (
	// silent answers nothing. A responder is silent until it is shown, and
	// a hidden server is silent.
	silent phase = iota
	// probing asks whether another machine already uses the names.
	probing
	// announcing has claimed the names, answers, and sends its announcements.
	announcing
	// steady answers and sends nothing on its own.
	steady
)

// wire is where the responder writes. The real one is the link.
type wire interface {
	multicast(msg *dns.Msg, index int)
	unicast(msg *dns.Msg, to netip.AddrPort)
}

// waiting is one multicast answer held back until due.
type waiting struct {
	due   time.Time
	index int
	msg   *dns.Msg
}

// responder answers for this server's records on the link, as RFC 6762 asks.
// It opens nothing and starts nothing. The announcer's loop owns it, calls it
// from one goroutine, and runs its timer.
type responder struct {
	wire   wire
	filter *linkFilter
	// delay picks a wait between two bounds. A test replaces it.
	delay func(least, most time.Duration) time.Duration

	services []string
	text     []string
	port     uint16
	// systemHost leaves the host name to the machine's own responder: no
	// address record, and the name is not this responder's to claim.
	systemHost bool
	// own holds every address of the links, and reverse their reverse names,
	// read by learn. They are built with the records, so no packet costs a
	// reading of them.
	own     []netip.Addr
	reverse map[string]bool

	// host and instance are the names on the wire. A conflict renames them,
	// and the base names are what a rename counts from.
	host, instance         string
	baseHost, baseInstance string
	hostTries, nameTries   int

	// links are the interfaces the responder announces on. The records are
	// built from them and from the names, and built again only when either
	// changes, so no packet costs a reading of the interfaces.
	links []announcedInterface
	// onLink holds the records of each interface by its index, and every
	// holds the records with the addresses of every interface.
	onLink map[int][]dns.RR
	every  []dns.RR
	// mine holds the key of every record this server announces anywhere.
	// names holds each name it answers for in canonical form, true for a name
	// it holds alone, and unique holds those names as written.
	mine   map[string]bool
	names  map[string]bool
	unique []string

	phase phase
	step  int
	// next is when the next probe or announcement is due. It is zero when
	// none is.
	next time.Time
	// announced reports that a record went out, so a goodbye is owed.
	announced bool
	// conflicts holds when each of the last conflictBurst conflicts was met.
	conflicts []time.Time
	// sent is when each record last went to each place.
	sent answered
	// held are the delayed answers, in the order they are due.
	held []waiting
	// exhausted reports that every name up to the last rename was taken, so
	// the responder gave up and stays silent.
	exhausted bool
}

func newResponder(config Config, host string, port int, over wire, filter *linkFilter,
	delay func(least, most time.Duration) time.Duration,
) *responder {
	text := config.text()
	pairs := make([]string, 0, len(text))

	for key, value := range text {
		pairs = append(pairs, key+"="+value)
	}

	slices.Sort(pairs)

	r := &responder{
		wire:         over,
		filter:       filter,
		delay:        delay,
		services:     config.Services,
		systemHost:   config.SystemHost,
		text:         pairs,
		port:         uint16(min(max(port, 0), math.MaxUint16)),
		host:         renamed(host, 0, hostSuffix),
		instance:     renamed(config.Instance, 0, instanceSuffix),
		baseHost:     host,
		baseInstance: config.Instance,
		sent:         newAnswered(),
	}
	r.build()

	return r
}

// randomDelay is a wait picked evenly between two bounds.
func randomDelay(least, most time.Duration) time.Duration {
	return least + rand.N(most-least+1) //nolint:gosec // a wait between packets needs no secret randomness.
}

// show starts probing a silent responder after a short random wait (RFC 6762
// section 8.1). hide sends a goodbye for every record that went out and makes
// it silent.
func (r *responder) show(now time.Time) {
	if r.phase == silent && !r.exhausted {
		r.phase, r.step, r.next = probing, 0, now.Add(r.delay(0, probeDelay))
	}
}

func (r *responder) hide() {
	r.goodbye()
	r.phase, r.next, r.held = silent, time.Time{}, nil
}

// wake is when the responder next has something to send, or zero.
func (r *responder) wake() time.Time {
	due := r.next

	if len(r.held) > 0 && (due.IsZero() || r.held[0].due.Before(due)) {
		due = r.held[0].due
	}

	return due
}

// tick sends the held answers that are due, then the probe or announcement.
func (r *responder) tick(now time.Time) {
	for len(r.held) > 0 && !now.Before(r.held[0].due) {
		r.wire.multicast(r.held[0].msg, r.held[0].index)
		r.held = r.held[1:]
	}

	if r.next.IsZero() || now.Before(r.next) {
		return
	}

	if r.phase == probing && r.step < probes {
		r.probe(r.step == 0)
		r.step++
		r.next = now.Add(probeInterval)

		return
	}

	if r.phase == probing {
		r.phase, r.step = announcing, 0
	}

	r.announce(now)
	r.step++
	r.next = now.Add(announceInterval)

	if r.step >= announcements {
		r.phase, r.next = steady, time.Time{}
	}
}

// setLinks takes a new reading of the interfaces. A link that went gets a
// goodbye for its records, and an address that went gets one for its record.
// A link that came, or one whose addresses changed, makes the responder probe
// and announce again, because the machines on that link have not heard it.
func (r *responder) setLinks(links []announcedInterface, now time.Time) {
	before, owed := r.onLink, r.announced && r.phase != silent
	changed := false

	for _, old := range r.links {
		fresh := slices.IndexFunc(links, func(one announcedInterface) bool { return one.index == old.index })

		switch {
		case fresh < 0:
			if owed {
				r.farewell(before[old.index], old.index)
			}
		case !slices.Equal(old.addresses, links[fresh].addresses):
			changed = true

			if owed {
				r.farewell(slices.DeleteFunc(slices.Clone(before[old.index]), func(rr dns.RR) bool {
					return !isAddress(rr) || addressIn(rr, links[fresh].addresses)
				}), old.index)
			}
		}
	}

	for _, one := range links {
		if !slices.ContainsFunc(r.links, func(old announcedInterface) bool { return old.index == one.index }) {
			changed = true
		}
	}

	r.links = links
	r.build()

	if changed && r.phase != silent {
		r.phase, r.step, r.next = probing, 0, now.Add(r.delay(0, probeDelay))
	}
}

// receive takes one message from the link.
func (r *responder) receive(one packet, now time.Time) {
	if r.phase == silent || one.msg.Opcode != dns.OpcodeQuery || one.msg.Rcode != dns.RcodeSuccess {
		return
	}

	if !r.filter.allows(one.iface, one.from.Addr(), one.to) {
		return
	}

	switch {
	case one.msg.Response && one.from.Port() != mdnsPort:
		// A response from any port but 5353 is not mDNS, and RFC 6762
		// section 6 asks for it to be ignored.
	case one.msg.Response:
		if r.systemHost {
			r.learn(one, now)
		}

		r.defend(one.msg, now)
	case r.phase == probing:
		r.tie(one, now)
	default:
		r.reply(one, now)
	}
}

// defend reads a response from another machine. One that claims a name of
// this server with other data is a conflict. While probing, the name is
// renamed and probed again. Once the name is claimed it is probed again as
// it stands, so a machine that really holds it answers the probe and the
// name is renamed then (RFC 6762 section 9).
func (r *responder) defend(msg *dns.Msg, now time.Time) {
	host, instance := r.clashes(bounded(msg.Answer, msg.Ns, msg.Extra))
	if !host && !instance {
		return
	}

	if r.phase == probing && ((host && r.hostTries >= maxRenames) || (instance && r.nameTries >= maxRenames)) {
		r.hide()
		r.exhausted = true

		return
	}

	if r.phase == probing {
		if host && r.hostTries < maxRenames {
			r.hostTries++
			r.host = renamed(r.baseHost, r.hostTries, hostSuffix)
		}

		if instance && r.nameTries < maxRenames {
			r.nameTries++
			r.instance = renamed(r.baseInstance, r.nameTries, instanceSuffix)
		}

		r.build()
	}

	r.conflicts = append(r.conflicts, now)
	if len(r.conflicts) > conflictBurst {
		r.conflicts = slices.Delete(r.conflicts, 0, len(r.conflicts)-conflictBurst)
	}

	// A held answer carries the names as they were, so it goes too.
	r.phase, r.step, r.next, r.held = probing, 0, now, nil
	if len(r.conflicts) == conflictBurst && now.Sub(r.conflicts[0]) <= conflictWindow {
		r.next = now.Add(conflictPause)
	}

	r.sent.reset()
}

// tie reads a probe from another machine for a name this server is probing.
// Each side sorts the whole set of records it proposes for the name, and the
// side whose set sorts lower waits a second and probes again. By then the
// other has claimed the name (RFC 6762 section 8.2). A probe whose records are
// all this server's own is its own probe, heard back.
func (r *responder) tie(one packet, now time.Time) {
	proposed := bounded(one.msg.Ns)
	if len(proposed) == 0 {
		return
	}

	ours := r.recordsOn(one.iface, one.index)

	for _, name := range r.unique {
		theirs := withName(proposed, name)
		if len(theirs) == 0 || !slices.ContainsFunc(theirs, func(rr dns.RR) bool { return !r.mine[recordKey(rr)] }) {
			continue
		}

		if compareSets(withName(ours, name), theirs) < 0 {
			r.step, r.next = 0, now.Add(lostTieWait)

			return
		}
	}
}

// reply answers a query from the link. An answer to a plain DNS client, or to
// a question that asks for unicast, goes back to the sender when the sender is
// on the link. Any other answer is multicast on the interface the query came
// in on, after a short random wait when it holds a shared record. A record
// goes to one place at most once a second, or once in 250 ms in answer to a
// probe.
func (r *responder) reply(one packet, now time.Time) {
	questions := one.msg.Question[:min(len(one.msg.Question), maxQuestions)]
	if !slices.ContainsFunc(questions, r.concerns) {
		return
	}

	records := r.recordsOn(one.iface, one.index)
	local := r.filter.fromLink(one.iface, one.from.Addr())
	legacy := one.from.Port() != mdnsPort
	unicast := legacy

	// A plain DNS client off the link can only be answered at its address,
	// and nothing is sent there.
	if legacy && !local {
		return
	}

	held := r.known(one.msg.Answer)

	var answers []dns.RR

	for _, question := range questions {
		unicast = unicast || question.Qclass&topBit != 0
		name := canonical(question.Name)

		for _, rr := range records {
			if matches(question, name, rr) && !slices.Contains(answers, rr) && !covered(held, rr) {
				answers = append(answers, rr)
			}
		}
	}

	// A sender off the link that asks for unicast is answered on the link.
	unicast = unicast && local

	// A unicast answer is limited by the asker's address and not its port,
	// so one machine asking from many ports is answered as one.
	place := "multicast " + strconv.Itoa(one.index)
	if unicast {
		place = one.from.Addr().WithZone("").String()
	}

	interval := answerInterval
	if len(one.msg.Ns) > 0 {
		interval = probeAnswerInterval
	}

	answers = slices.DeleteFunc(answers, func(rr dns.RR) bool {
		last, sent := r.sent.last(place + " " + recordKey(rr))

		return sent && now.Sub(last) < interval
	})

	if len(answers) == 0 {
		return
	}

	msg := response(answers, additional(answers, records))

	// An answer is marked as sent only once it is sent or held, so an answer
	// dropped because the held list is full is not counted against the limit.
	switch {
	case legacy:
		msg.Id = one.msg.Id
		msg.Question = one.msg.Question
		msg.Answer, msg.Extra = legacyCopies(msg.Answer), legacyCopies(msg.Extra)
		r.mark(place, answers, now)
		r.wire.unicast(msg, one.from)
	case unicast:
		r.mark(place, answers, now)
		r.wire.unicast(msg, one.from)
	case len(one.msg.Ns) > 0 || !slices.ContainsFunc(answers, shared):
		r.mark(place, answers, now)
		r.wire.multicast(msg, one.index)
	case len(r.held) < maxWaiting:
		r.mark(place, answers, now)
		r.hold(waiting{due: now.Add(r.delay(sharedLeast, sharedMost)), index: one.index, msg: msg})
	}
}

// hold keeps one delayed answer, in the order the answers are due.
func (r *responder) hold(one waiting) {
	at, _ := slices.BinarySearchFunc(r.held, one.due, func(held waiting, due time.Time) int {
		return held.due.Compare(due)
	})
	r.held = slices.Insert(r.held, at, one)
}

// probe asks every interface whether another machine already uses a name
// this server wants. The records it proposes go in the authority section, so
// another prober can break a tie.
func (r *responder) probe(first bool) {
	if first && r.systemHost {
		r.askHost()
	}

	for _, link := range r.links {
		msg := new(dns.Msg)

		for _, name := range r.unique {
			class := uint16(dns.ClassINET)
			if first {
				class |= topBit
			}

			msg.Question = append(msg.Question, dns.Question{Name: name, Qtype: dns.TypeANY, Qclass: class})
		}

		for _, rr := range r.onLink[link.index] {
			if r.names[canonical(rr.Header().Name)] {
				msg.Ns = append(msg.Ns, rr)
			}
		}

		r.wire.multicast(msg, link.index)
	}
}

// askHost asks the machine's own responder which name holds each address this
// server announces on, by the address's reverse name (RFC 6762 section 4).
// The answer reaches learn like any other response.
func (r *responder) askHost() {
	for _, link := range r.links {
		msg := new(dns.Msg)

		for _, addr := range link.addresses {
			reverse, err := dns.ReverseAddr(addr.String())
			if err != nil {
				continue
			}

			msg.Question = append(msg.Question, dns.Question{Name: reverse, Qtype: dns.TypePTR, Qclass: dns.ClassINET})
		}

		if len(msg.Question) > 0 {
			r.wire.multicast(msg, link.index)
		}
	}
}

// learn takes the host name the machine's own responder holds, from the PTR
// of the reverse name of an address of this machine. Only a packet sent from
// an address of this machine is read, and a name that differs from the
// current one only in case is the same name. The SRV records then name the
// host, and they are announced again so a cache drops the old ones.
func (r *responder) learn(one packet, now time.Time) {
	if !slices.Contains(r.own, one.from.Addr().WithZone("").Unmap()) {
		return
	}

	learned := ""

	for _, rr := range bounded(one.msg.Answer, one.msg.Extra) {
		switch host := hostOf(rr, r.reverse); {
		case host == "":
		case strings.EqualFold(host, r.host):
			return
		case learned == "":
			learned = host
		}
	}

	if learned == "" {
		return
	}

	r.host = learned
	r.build()

	if r.phase == steady || r.phase == announcing {
		r.phase, r.step, r.next = announcing, 0, now
		r.sent.reset()
	}
}

// hostOf is the host label the PTR of a reverse name of this machine points
// at, or empty for any other record. The reverse name of an address names one
// host (RFC 6762 section 4), so an alias that also carries the address never
// gives the host.
func hostOf(rr dns.RR, reverse map[string]bool) string {
	ptr, ok := rr.(*dns.PTR)
	if !ok || ptr.Hdr.Ttl == 0 || !reverse[canonical(ptr.Hdr.Name)] {
		return ""
	}

	labels := dns.SplitDomainName(strings.ToLower(ptr.Ptr))
	if len(labels) != 2 || labels[1] != domain {
		return ""
	}

	return labels[0]
}

// announce sends every record on every interface.
func (r *responder) announce(now time.Time) {
	for _, link := range r.links {
		records := r.onLink[link.index]
		r.mark("multicast "+strconv.Itoa(link.index), records, now)
		r.wire.multicast(response(records, nil), link.index)
	}

	r.announced = true
}

// goodbye sends every record with a time to live of zero, which tells every
// cache on the link to drop it (RFC 6762 section 10.1).
func (r *responder) goodbye() {
	if !r.announced {
		return
	}

	for _, link := range r.links {
		r.farewell(r.onLink[link.index], link.index)
	}

	r.announced = false
}

// farewell sends a goodbye for some records on one interface.
func (r *responder) farewell(records []dns.RR, index int) {
	if len(records) == 0 {
		return
	}

	out := make([]dns.RR, 0, len(records))

	for _, rr := range records {
		gone := dns.Copy(rr)
		gone.Header().Ttl = 0
		out = append(out, gone)
	}

	r.wire.multicast(response(out, nil), index)
}

// mark notes that records went to one place, for the limit on answers.
func (r *responder) mark(place string, records []dns.RR, now time.Time) {
	for _, rr := range records {
		r.sent.note(place+" "+recordKey(rr), now)
	}
}

// hostName and instanceName are the full names on the wire.
func (r *responder) hostName() string {
	return r.host + "." + domain + "."
}

func (r *responder) instanceName(service string) string {
	return escapeLabel(r.instance) + "." + service + "." + domain + "."
}

// build makes the records again from the names and the links.
func (r *responder) build() {
	r.onLink = make(map[int][]dns.RR, len(r.links))

	var every []netip.Addr

	r.reverse = map[string]bool{}

	for _, link := range r.links {
		r.onLink[link.index] = r.records(link.addresses)
		every = append(every, link.addresses...)

		for _, addr := range link.addresses {
			if name, err := dns.ReverseAddr(addr.String()); err == nil {
				r.reverse[canonical(name)] = true
			}
		}
	}

	r.own = every

	r.every = r.records(every)
	r.mine = make(map[string]bool, len(r.every))
	r.names = map[string]bool{}
	r.unique = nil

	if !r.systemHost {
		r.names[canonical(r.hostName())] = true
		r.unique = append(r.unique, r.hostName())
	}

	for _, rr := range r.every {
		r.mine[recordKey(rr)] = true
	}

	for _, service := range r.services {
		r.names[canonical(r.instanceName(service))] = true
		r.names[canonical(service+"."+domain+".")] = false
		r.unique = append(r.unique, r.instanceName(service))
	}

	r.names[canonical(servicesName)] = false
}

// records is every record this server answers with, with the addresses given.
// The address records carry the addresses of one interface, because a
// machine on one link cannot reach an address on another (RFC 6762 section
// 6.2).
func (r *responder) records(addresses []netip.Addr) []dns.RR {
	host := r.hostName()
	shared := func(name string, rrtype uint16) dns.RR_Header {
		return dns.RR_Header{Name: name, Rrtype: rrtype, Class: dns.ClassINET, Ttl: serviceTimeToLive}
	}
	unique := func(name string, rrtype uint16, ttl uint32) dns.RR_Header {
		return dns.RR_Header{Name: name, Rrtype: rrtype, Class: dns.ClassINET | topBit, Ttl: ttl}
	}

	var out []dns.RR

	for _, service := range r.services {
		kind := service + "." + domain + "."
		instance := r.instanceName(service)

		out = append(out,
			&dns.PTR{Hdr: shared(servicesName, dns.TypePTR), Ptr: kind},
			&dns.PTR{Hdr: shared(kind, dns.TypePTR), Ptr: instance},
			&dns.SRV{Hdr: unique(instance, dns.TypeSRV, hostTimeToLive), Port: r.port, Target: host},
			&dns.TXT{Hdr: unique(instance, dns.TypeTXT, serviceTimeToLive), Txt: r.text},
		)
	}

	if r.systemHost {
		return out
	}

	for _, addr := range addresses {
		if addr.Is4() {
			out = append(out, &dns.A{Hdr: unique(host, dns.TypeA, hostTimeToLive), A: addr.AsSlice()})
		} else {
			out = append(out, &dns.AAAA{Hdr: unique(host, dns.TypeAAAA, hostTimeToLive), AAAA: addr.AsSlice()})
		}
	}

	return out
}

// recordsOn is the records of the interface a packet came in on. A packet
// on no interface this server announces on is answered with nothing.
func (r *responder) recordsOn(iface string, index int) []dns.RR {
	if records, ok := r.onLink[index]; ok && index != 0 {
		return records
	}

	for _, link := range r.links {
		if iface != "" && link.name == iface {
			return r.onLink[link.index]
		}
	}

	return nil
}

// clashes reports whether any record claims this server's host name or one of
// its instance names with data this server does not announce. A goodbye
// claims nothing.
func (r *responder) clashes(records []dns.RR) (host, instance bool) {
	for _, rr := range records {
		header := rr.Header()
		if header.Ttl == 0 || !r.names[canonical(header.Name)] {
			continue
		}

		switch header.Rrtype {
		case dns.TypeA, dns.TypeAAAA:
			host = host || !r.mine[recordKey(rr)]
		case dns.TypeSRV, dns.TypeTXT:
			instance = instance || !r.mine[recordKey(rr)]
		}
	}

	return host, instance
}

// concerns reports whether a question asks for a name this server answers
// for. Any other question costs nothing more.
func (r *responder) concerns(question dns.Question) bool {
	_, ours := r.names[canonical(question.Name)]

	return ours
}

// known is the records the asker already holds that this server would
// answer with, by key, with the time to live each has left (RFC 6762 section
// 7.1).
func (r *responder) known(held []dns.RR) map[string]uint32 {
	out := make(map[string]uint32)

	for _, rr := range held[:min(len(held), maxRecords)] {
		if _, ours := r.names[canonical(rr.Header().Name)]; ours {
			out[recordKey(rr)] = rr.Header().Ttl
		}
	}

	return out
}

// covered reports whether the asker holds a record with at least half its
// time to live left, so the answer can leave it out.
func covered(held map[string]uint32, rr dns.RR) bool {
	ttl, ok := held[recordKey(rr)]

	return ok && ttl >= rr.Header().Ttl/2
}

// matches reports whether a record answers a question. name is the name
// the question asks for, in canonical form.
func matches(question dns.Question, name string, rr dns.RR) bool {
	class := question.Qclass &^ topBit
	header := rr.Header()

	return (class == dns.ClassINET || class == dns.ClassANY) &&
		(question.Qtype == dns.TypeANY || question.Qtype == header.Rrtype) &&
		canonical(header.Name) == name
}

// additional is the records an asker needs next: the service and text records
// of every instance a pointer names, and the addresses of every host a service
// record names (RFC 6763 section 12).
func additional(answered, all []dns.RR) []dns.RR {
	var names []string

	for _, rr := range answered {
		switch rr := rr.(type) {
		case *dns.PTR:
			names = append(names, rr.Ptr)
		case *dns.SRV:
			names = append(names, rr.Target)
		}
	}

	for _, rr := range all {
		if srv, ok := rr.(*dns.SRV); ok && slices.Contains(names, srv.Hdr.Name) {
			names = append(names, srv.Target)
		}
	}

	var out []dns.RR

	for _, rr := range all {
		if _, pointer := rr.(*dns.PTR); pointer || slices.Contains(answered, rr) {
			continue
		}

		if slices.ContainsFunc(names, func(name string) bool { return sameName(name, rr.Header().Name) }) {
			out = append(out, rr)
		}
	}

	return out
}

// response is an mDNS answer: marked authoritative, with no question and an
// id of zero (RFC 6762 section 18).
func response(answers, extra []dns.RR) *dns.Msg {
	msg := new(dns.Msg)
	msg.Response = true
	msg.Authoritative = true
	msg.Answer = answers
	msg.Extra = extra
	msg.Compress = true

	return msg
}
