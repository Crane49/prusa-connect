package discovery

import (
	"cmp"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// MaxNearby is the most records the listener keeps. A record takes memory on
// the printer's own board, and anything on the network can announce itself, so
// the list has an upper limit. A record that arrives while the list is full is
// dropped until an older one expires.
const MaxNearby = 256

// maxTimeToLive is the longest the listener keeps a record it does not hear
// again. A responder chooses its own time to live, and a printer that loses
// power sends no goodbye, so a very long value would keep it listed for days.
const maxTimeToLive = 2 * time.Hour

// maxAddresses is the most addresses one record keeps. A printer has one or
// two, and a server one for each network it is on.
const maxAddresses = 8

// refreshShare is the part of a record's time to live after which the listener
// asks again. RFC 6762 section 5.2 asks for a query at 80 percent, so a record
// that is still there is heard before it expires.
const (
	refreshShareNumerator   = 4
	refreshShareDenominator = 5
)

// Nearby is one printer or server this one heard on the local network. It is a
// name and an address. Hearing it grants it nothing.
type Nearby struct {
	// Name is the instance name the other side announced.
	Name string
	// Services are the service names it answered under, sorted.
	Services []string
	// Address is where its API answers: the first of Addresses as pick orders
	// them, so the row keeps one address while the machine keeps answering.
	Address netip.Addr
	// Addresses is every address the machine answered with that has not
	// expired, sorted. A connected printer is matched against all of them.
	Addresses []netip.Addr
	// Port is the port of its API.
	Port int
	// TLS reports that the port expects https.
	TLS bool
}

// URL is the address of the other printer's own web page.
func (n Nearby) URL() string {
	scheme := "http"
	if n.TLS {
		scheme = "https"
	}

	return scheme + "://" + net.JoinHostPort(n.Address.String(), strconv.Itoa(n.Port)) + "/"
}

// self is this server's own host and port. The listener hears this server's
// own announcement too, and a server does not list itself.
type self struct {
	host string
	port int
}

// record is one instance under one service name, as far as the listener has
// heard it. It is listed once it has a port and an address.
type record struct {
	service string
	name    string
	host    string
	port    int
	// addresses maps each address the host answered with to when it expires.
	// A machine on several networks answers on each of them with that
	// network's address, so one answer alone never holds them all.
	addresses map[netip.Addr]time.Time
	tls       bool
	expires   time.Time
	refresh   time.Time
}

// table is the list of records the listener heard. It is not safe for use by
// more than one goroutine; the listener holds a lock around every call.
type table struct {
	// watched maps each full service name the listener asks for, in lower
	// case, to the short name a row reports.
	watched map[string]string
	// rank is where each short service name stands in the list the listener
	// asks for. A machine is listed under the first one it announces.
	rank map[string]int
	self self
	// records is keyed by the full instance name in lower case, because DNS
	// names compare without case.
	records map[string]*record
	// sweeps counts the sweeps a full list made, so a test can hold it to one
	// per message.
	sweeps int
}

func newTable(services []string) *table {
	watched := make(map[string]string, len(services))
	rank := make(map[string]int, len(services))

	for i, service := range services {
		watched[strings.ToLower(service)+"."+domain+"."] = service
		rank[service] = i
	}

	return &table{watched: watched, rank: rank, records: make(map[string]*record)}
}

// observe reads one message from the link and returns how many new records it
// refused because the list was full.
//
// Pointer records come first, because they are what says an instance exists.
// A service or text record for an instance nobody pointed at is ignored. A full
// list is swept at most once for the whole message.
func (t *table) observe(msg *dns.Msg, from netip.Addr, now time.Time) int {
	answers := slices.Concat(msg.Answer, msg.Ns, msg.Extra)

	refused := 0
	swept := false

	for _, answer := range answers {
		pointer, ok := answer.(*dns.PTR)
		if !ok {
			continue
		}

		if !swept && t.needsRoom(pointer) {
			t.sweep(now)
			swept = true
		}

		if !t.pointer(pointer, now) {
			refused++
		}
	}

	addresses := hostAddresses(answers)
	served := make(map[string]bool)

	for _, answer := range answers {
		switch rr := answer.(type) {
		case *dns.SRV:
			if t.service(rr) {
				served[strings.ToLower(rr.Hdr.Name)] = true
			}
		case *dns.TXT:
			t.text(rr)
		}
	}

	t.address(served, addresses, from, now)

	return refused
}

// address gives each record a service record in this message named the
// addresses of its host.
//
// An address is taken only from the message whose service record names the
// host, or from its sender. An address record on its own could point a
// listed printer at any machine. The sender is held to the same rule as an
// address record, so the list never holds an address a page cannot open, and
// it is taken only while the record holds no address at all.
func (t *table) address(served map[string]bool, addresses map[string][]heardAddress, from netip.Addr, now time.Time) {
	for key := range served {
		// A goodbye later in the same message may have taken the record
		// out again.
		one, known := t.records[key]
		if !known {
			continue
		}

		heard := addresses[one.host]
		for _, address := range heard {
			if address.ttl == 0 {
				delete(one.addresses, address.addr)

				continue
			}

			// An address lives as long as the record that carried it. A
			// responder sends the address again with each answer, and the
			// record is asked for again long after an address record's two
			// minutes would have run out.
			if _, usable := pick([]netip.Addr{address.addr}); usable {
				one.remember(address.addr, latest(now.Add(timeToLive(address.ttl)), one.expires))
			}
		}

		if len(heard) == 0 && len(one.addresses) == 0 {
			if sent, usable := pick([]netip.Addr{from.WithZone("").Unmap()}); usable {
				one.remember(sent, one.expires)
			}
		}
	}
}

// remember keeps one address until the moment given. A record keeps at most
// maxAddresses, because anything on the network can answer with any number of
// address records; a new one beyond that replaces the one that expires first.
func (r *record) remember(addr netip.Addr, until time.Time) {
	if r.addresses == nil {
		r.addresses = make(map[netip.Addr]time.Time)
	}

	if _, held := r.addresses[addr]; !held && len(r.addresses) >= maxAddresses {
		var soonest netip.Addr

		for held, expires := range r.addresses {
			if !soonest.IsValid() || expires.Before(r.addresses[soonest]) {
				soonest = held
			}
		}

		delete(r.addresses, soonest)
	}

	r.addresses[addr] = until
}

// live is every address of the record that has not expired, sorted the way
// pick orders them.
func (r *record) live(now time.Time) []netip.Addr {
	out := make([]netip.Addr, 0, len(r.addresses))

	for addr, expires := range r.addresses {
		if now.Before(expires) {
			out = append(out, addr)
		}
	}

	slices.SortFunc(out, byPreference)

	return out
}

// needsRoom reports whether a pointer would add a record to a full list.
func (t *table) needsRoom(rr *dns.PTR) bool {
	if len(t.records) < MaxNearby || rr.Hdr.Ttl == 0 {
		return false
	}

	if _, watched := t.watched[strings.ToLower(rr.Hdr.Name)]; !watched {
		return false
	}

	_, known := t.records[strings.ToLower(rr.Ptr)]

	return !known
}

// pointer takes one pointer record. It answers false when the list is full and
// the record is new.
func (t *table) pointer(rr *dns.PTR, now time.Time) bool {
	service, watched := t.watched[strings.ToLower(rr.Hdr.Name)]
	if !watched {
		return true
	}

	key := strings.ToLower(rr.Ptr)

	if rr.Hdr.Ttl == 0 {
		delete(t.records, key)

		return true
	}

	one, known := t.records[key]
	if !known {
		if len(t.records) >= MaxNearby {
			return false
		}

		one = &record{service: service, name: instanceName(rr.Ptr, rr.Hdr.Name)}
		t.records[key] = one
	}

	live := timeToLive(rr.Hdr.Ttl)
	one.expires = now.Add(live)
	one.refresh = now.Add(live * refreshShareNumerator / refreshShareDenominator)

	return true
}

// service takes one service record, which carries the host and the port. A
// goodbye for it withdraws the instance as a goodbye for the pointer does.
//
// It does not move the expiry. The pointer says whether the instance exists,
// and the service record says only where it is.
func (t *table) service(rr *dns.SRV) bool {
	key := strings.ToLower(rr.Hdr.Name)

	one, known := t.records[key]
	if !known {
		return false
	}

	if rr.Hdr.Ttl == 0 {
		delete(t.records, key)

		return false
	}

	host := strings.ToLower(strings.TrimSuffix(rr.Target, "."))
	if host != one.host {
		one.addresses = nil
	}

	one.host = host
	one.port = int(rr.Port)

	return true
}

// text takes one text record. Only the tls key is read, because it says which
// scheme the link to the other printer's page takes.
func (t *table) text(rr *dns.TXT) {
	one, known := t.records[strings.ToLower(rr.Hdr.Name)]
	if !known {
		return
	}

	one.tls = slices.Contains(rr.Txt, keyTLS+"="+tlsOn)
}

// sweep drops every record whose time to live has ended.
func (t *table) sweep(now time.Time) {
	t.sweeps++

	for key, one := range t.records {
		if !now.Before(one.expires) {
			delete(t.records, key)
		}
	}
}

// asked moves every record whose refresh is due to its expiry. The question
// just sent is its refresh. A record that answers gets a new time to live, and
// one that does not is asked for no more and expires.
func (t *table) asked(now time.Time) {
	for _, one := range t.records {
		if !now.Before(one.refresh) {
			one.refresh = one.expires
		}
	}
}

// refreshDue is the earliest moment a record asks to be heard again, or zero
// when none does. A record already asked for waits for its expiry instead.
func (t *table) refreshDue() time.Time {
	var due time.Time

	for _, one := range t.records {
		if !one.refresh.Before(one.expires) {
			continue
		}

		if due.IsZero() || one.refresh.Before(due) {
			due = one.refresh
		}
	}

	return due
}

// list is every record that is still alive and complete, one row per name,
// host and port. A server that answers under both service names is one row.
//
// The row is keyed by the host its service records name and not by the
// address, because a machine on several networks lists its addresses in a
// different order in each answer, and each service would then be a row of its
// own. The row takes every address its records hold, and shows the first of
// them as pick orders them.
func (t *table) list(now time.Time) []Nearby {
	t.sweep(now)

	rows := make(map[place]*Nearby)

	for _, one := range t.records {
		live := one.live(now)
		if one.port == 0 || len(live) == 0 {
			continue
		}

		at := place{name: one.name, host: one.host, port: one.port}

		row, seen := rows[at]
		if !seen {
			row = &Nearby{Name: one.name, Port: one.port}
			rows[at] = row
		}

		for _, addr := range live {
			if !slices.Contains(row.Addresses, addr) {
				row.Addresses = append(row.Addresses, addr)
			}
		}

		row.Services = append(row.Services, one.service)
		row.TLS = row.TLS || one.tls
	}

	best := t.bestRankPerHost(rows)

	out := make([]Nearby, 0, len(rows))
	for at, row := range rows {
		if t.isSelf(at) || t.rankOf(row) > best[strings.ToLower(at.host)] {
			continue
		}

		slices.Sort(row.Services)
		slices.SortFunc(row.Addresses, byPreference)
		row.Address = row.Addresses[0]
		out = append(out, *row)
	}

	slices.SortFunc(out, func(a, b Nearby) int {
		return cmp.Or(strings.Compare(a.Name, b.Name), a.Address.Compare(b.Address), cmp.Compare(a.Port, b.Port))
	})

	return out
}

// byPreference orders addresses the way pick chooses: IPv4 before IPv6, and
// then the lower address, so the same set always puts the same one first.
func byPreference(a, b netip.Addr) int {
	if a.Is4() != b.Is4() {
		if a.Is4() {
			return -1
		}

		return 1
	}

	return a.Compare(b)
}

// timeToLive is a record's time to live, cut to maxTimeToLive.
func timeToLive(seconds uint32) time.Duration {
	return min(time.Duration(seconds)*time.Second, maxTimeToLive)
}

// heardAddress is one address record: the address and its time to live in
// seconds, zero for a goodbye.
type heardAddress struct {
	addr netip.Addr
	ttl  uint32
}

// hostAddresses collects the address records of one message by host name.
func hostAddresses(answers []dns.RR) map[string][]heardAddress {
	out := make(map[string][]heardAddress)

	for _, answer := range answers {
		var (
			host string
			ip   []byte
			ttl  uint32
		)

		switch rr := answer.(type) {
		case *dns.A:
			host, ip, ttl = rr.Hdr.Name, rr.A, rr.Hdr.Ttl
		case *dns.AAAA:
			host, ip, ttl = rr.Hdr.Name, rr.AAAA, rr.Hdr.Ttl
		default:
			continue
		}

		if addr, ok := netip.AddrFromSlice(ip); ok {
			key := strings.ToLower(strings.TrimSuffix(host, "."))
			out[key] = append(out[key], heardAddress{addr: addr.Unmap(), ttl: ttl})
		}
	}

	return out
}

// place is where one row of the list answers: an instance name on a host and
// port.
type place struct {
	name string
	host string
	port int
}

// isSelf reports a record of this server. It still ranks its host, so the
// machine this server runs on is left out whole, the PrusaLink beside it too.
// DNS names compare without case, and a Windows machine reports its host name
// in capitals while the record names it in lower case.
func (t *table) isSelf(at place) bool {
	return strings.EqualFold(at.host, t.self.host+"."+domain) && at.port == t.self.port
}

// rankOf is the best rank among the services a row answers under. A service
// the listener does not ask for ranks last.
func (t *table) rankOf(row *Nearby) int {
	best := len(t.rank)

	for _, service := range row.Services {
		if rank, asked := t.rank[service]; asked {
			best = min(best, rank)
		}
	}

	return best
}

// bestRankPerHost is the best rank each host answers under, keyed in lower
// case. A printer that runs a server of its own announces PrusaLink and the
// server on two ports of one host, and it is listed once, as the server.
func (t *table) bestRankPerHost(rows map[place]*Nearby) map[string]int {
	best := make(map[string]int, len(rows))

	for at, row := range rows {
		host := strings.ToLower(at.host)
		if rank, seen := best[host]; !seen || t.rankOf(row) < rank {
			best[host] = t.rankOf(row)
		}
	}

	return best
}

// pick chooses the address a browser can open. IPv4 comes first. A link-local
// IPv6 address is skipped, because it needs the name of an interface on the
// reader's own machine and a link in a page cannot carry one.
func pick(addresses []netip.Addr) (netip.Addr, bool) {
	for _, addr := range addresses {
		if addr.Is4() {
			return addr, true
		}
	}

	for _, addr := range addresses {
		if !addr.IsLinkLocalUnicast() {
			return addr, true
		}
	}

	return netip.Addr{}, false
}
