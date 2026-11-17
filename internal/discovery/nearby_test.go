package discovery

import (
	"fmt"
	"net"
	"net/netip"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/require"
)

// start is the moment every case in this file begins at. The table takes the
// time as an argument, so no case waits for a clock.
var start = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

// sender is the address a message arrives from when a case does not care.
var sender = netip.MustParseAddr("192.168.1.50")

// newTestTable asks for both services, a server first, as the core does.
func newTestTable() *table {
	return newTable([]string{connectService, linkService})
}

// announcement is the message a responder sends for one instance: the pointer,
// the service record, the text and the host's address.
func announcement(name, service, host string, port int, address string, ttl uint32, text ...string) *dns.Msg {
	instance := escapeLabel(name) + "." + service + ".local."
	target := host + ".local."

	msg := new(dns.Msg)
	msg.Response = true
	msg.Answer = []dns.RR{
		&dns.PTR{
			Hdr: dns.RR_Header{Name: service + ".local.", Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: ttl},
			Ptr: instance,
		},
		&dns.SRV{
			Hdr:    dns.RR_Header{Name: instance, Rrtype: dns.TypeSRV, Class: dns.ClassINET, Ttl: 120},
			Port:   uint16(port),
			Target: target,
		},
		&dns.TXT{
			Hdr: dns.RR_Header{Name: instance, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: ttl},
			Txt: text,
		},
	}

	if address != "" {
		msg.Extra = []dns.RR{&dns.A{
			Hdr: dns.RR_Header{Name: target, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 120},
			A:   net.ParseIP(address),
		}}
	}

	return msg
}

// goodbye is the message a responder sends when it withdraws an instance: the
// pointer again, with a time to live of zero (RFC 6762 section 10.1).
func goodbye(name, service string) *dns.Msg {
	msg := new(dns.Msg)
	msg.Response = true
	msg.Answer = []dns.RR{&dns.PTR{
		Hdr: dns.RR_Header{Name: service + ".local.", Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: 0},
		Ptr: escapeLabel(name) + "." + service + ".local.",
	}}

	return msg
}

func TestAHeardPrinterIsListedWithItsAddressPortAndTLS(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	nearby.observe(announcement("Workshop MK4", linkService, "mk4", 443, "192.168.1.20", 4500,
		"path=/", "tls=1"), sender, start)

	require.Equal(t, []Nearby{{
		Name:      "Workshop MK4",
		Services:  []string{linkService},
		Address:   netip.MustParseAddr("192.168.1.20"),
		Addresses: []netip.Addr{netip.MustParseAddr("192.168.1.20")},
		Port:      443,
		TLS:       true,
	}}, nearby.list(start))
}

func TestAPrinterWithoutTheTLSKeyIsPlain(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	nearby.observe(announcement("Plain", linkService, "plain", 80, "192.168.1.21", 4500, "path=/"), sender, start)

	listed := nearby.list(start)
	require.Len(t, listed, 1)
	require.False(t, listed[0].TLS)
}

func TestAGoodbyeDropsThePrinterAtOnce(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	nearby.observe(announcement("Leaving", linkService, "leaving", 80, "192.168.1.22", 4500), sender, start)
	require.Len(t, nearby.list(start), 1)

	nearby.observe(goodbye("Leaving", linkService), sender, start.Add(time.Second))

	require.Empty(t, nearby.list(start.Add(time.Second)))
}

func TestARecordIsDroppedWhenItsTimeToLiveEnds(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	nearby.observe(announcement("Short", linkService, "short", 80, "192.168.1.23", 60), sender, start)

	require.Len(t, nearby.list(start.Add(59*time.Second)), 1)
	require.Empty(t, nearby.list(start.Add(61*time.Second)))
}

func TestHearingARecordAgainKeepsIt(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	nearby.observe(announcement("Kept", linkService, "kept", 80, "192.168.1.24", 60), sender, start)
	nearby.observe(announcement("Kept", linkService, "kept", 80, "192.168.1.24", 60), sender, start.Add(50*time.Second))

	require.Len(t, nearby.list(start.Add(100*time.Second)), 1)
}

// TestALongTimeToLiveIsCut keeps a record that claims to live for years from
// staying in the list after its printer has gone without a goodbye.
func TestALongTimeToLiveIsCut(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	nearby.observe(announcement("Forever", linkService, "forever", 80, "192.168.1.25", 1<<31), sender, start)

	require.Len(t, nearby.list(start.Add(maxTimeToLive-time.Second)), 1)
	require.Empty(t, nearby.list(start.Add(maxTimeToLive+time.Second)))
}

func TestTheListHoldsAtMost256Records(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()

	for i := range MaxNearby {
		nearby.observe(announcement(fmt.Sprintf("printer-%03d", i), linkService, fmt.Sprintf("host-%03d", i),
			80, fmt.Sprintf("10.0.%d.%d", i/200, i%200+1), 60), sender, start)
	}

	require.Len(t, nearby.list(start), MaxNearby)

	refused := nearby.observe(announcement("one-too-many", linkService, "extra", 80, "10.0.9.9", 4500), sender, start)
	require.Equal(t, 1, refused)
	require.Len(t, nearby.list(start), MaxNearby)
	require.Len(t, nearby.records, MaxNearby, "a refused record still took memory")

	// Once the first ones have expired, a new printer has room again.
	nearby.observe(
		announcement("after-expiry", linkService, "later", 80, "10.0.9.10", 4500),
		sender,
		start.Add(2*time.Minute),
	)

	listed := nearby.list(start.Add(2 * time.Minute))
	require.Len(t, listed, 1)
	require.Equal(t, "after-expiry", listed[0].Name)
}

func TestAServerUnderBothNamesIsOneRow(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	nearby.observe(announcement("Workshop", linkService, "workshop", 8080, "192.168.1.26", 4500), sender, start)
	nearby.observe(announcement("Workshop", connectService, "workshop", 8080, "192.168.1.26", 4500), sender, start)

	listed := nearby.list(start)
	require.Len(t, listed, 1)
	require.Equal(t, []string{connectService, linkService}, listed[0].Services)
}

// A server on several networks lists its addresses in a different order in
// each answer. Its two services then arrive with two addresses, and it is still
// one row, at the address that sorts first.
func TestAServerWhoseServicesNameTwoAddressesIsOneRow(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	nearby.observe(announcement("Workshop", linkService, "workshop", 8080, "192.168.181.7", 4500), sender, start)
	nearby.observe(announcement("Workshop", connectService, "workshop", 8080, "192.168.1.26", 4500), sender, start)

	listed := nearby.list(start)
	require.Len(t, listed, 1)
	require.Equal(t, []string{connectService, linkService}, listed[0].Services)
	require.Equal(t, netip.MustParseAddr("192.168.1.26"), listed[0].Address)
}

// A printer that runs a server of its own announces PrusaLink from its system
// responder and the server on another port, both naming one host. It is one
// machine, listed under the service the listener asks for first: the server.
func TestAMachineIsListedUnderTheFirstServiceItAnnounces(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	nearby.observe(announcement("Bench", linkService, "bench", 443, "192.168.1.22", 4500), sender, start)
	nearby.observe(announcement("Bench server", connectService, "Bench", 8090, "192.168.1.22", 4500), sender, start)

	require.Equal(t, []Nearby{{
		Name:      "Bench server",
		Services:  []string{connectService},
		Address:   netip.MustParseAddr("192.168.1.22"),
		Addresses: []netip.Addr{netip.MustParseAddr("192.168.1.22")},
		Port:      8090,
	}}, nearby.list(start))
}

// A server on one machine leaves the printers on the others where they are.
func TestAServerHidesNoPrinterOnAnotherMachine(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	nearby.observe(announcement("Bench server", connectService, "bench", 8090, "192.168.1.22", 4500), sender, start)
	nearby.observe(announcement("MK4", linkService, "mk4", 80, "192.168.1.23", 4500), sender, start)

	listed := nearby.list(start)
	require.Len(t, listed, 2)
	require.Equal(t, []string{"Bench server", "MK4"}, []string{listed[0].Name, listed[1].Name})
}

// The machine this server runs on is left out whole: its own server, and the
// PrusaLink its system responder announces beside it on another port.
func TestTheMachineThisServerRunsOnIsNotListed(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	nearby.self = self{host: "bench", port: 8090}
	nearby.observe(announcement("Bench", linkService, "bench", 443, "192.168.1.22", 4500), sender, start)
	nearby.observe(announcement("Bench server", connectService, "bench", 8090, "192.168.1.22", 4500), sender, start)

	require.Empty(t, nearby.list(start))
}

// A server whose system responder answers for the host sends its service
// records without an address. Another server hears that announcement beside
// the PrusaLink of the same machine and lists the machine once, as the server,
// at the address the announcement came from.
func TestAServerBesideASystemResponderIsListedOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := systemHostAnnouncer(over)
		announcer.config.Services = []string{connectService}

		announcer.Start(t.Context(), 8090)
		defer announcer.Stop(t.Context())

		settle()

		sent := over.all()
		server := roundTrip(sent[slices.IndexFunc(sent, isAnnouncement)].msg)
		require.Empty(t, server.Extra, "a system host sent an address")

		desk := netip.MustParseAddr("192.168.1.22")
		nearby := newTestTable()
		nearby.observe(announcement("Desk", linkService, "desk", 443, "192.168.1.22", 4500), desk, start)
		nearby.observe(server, desk, start)

		require.Equal(t, []Nearby{{
			Name:      "Desk",
			Services:  []string{connectService},
			Address:   desk,
			Addresses: []netip.Addr{desk},
			Port:      8090,
		}}, nearby.list(start))
	})
}

// addressRecord is one more address record for a host, to put beside the one
// announcement carries.
func addressRecord(host, address string, ttl uint32) dns.RR {
	return &dns.A{
		Hdr: dns.RR_Header{Name: host + ".local.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttl},
		A:   net.ParseIP(address),
	}
}

// A second answer that lists the same addresses in another order leaves the
// row where it was. A machine on several networks answers like that, and the
// link on the page must not change between two reads.
func TestAnAnswerInAnotherOrderKeepsTheAddress(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	first := announcement("Workshop", linkService, "workshop", 8080, "192.168.1.26", 4500)
	first.Extra = append(first.Extra, addressRecord("workshop", "172.31.240.43", 120))
	nearby.observe(first, sender, start)
	shown := nearby.list(start)[0].Address

	second := announcement("Workshop", linkService, "workshop", 8080, "172.31.240.43", 4500)
	second.Extra = append(second.Extra, addressRecord("workshop", "192.168.1.26", 120))
	nearby.observe(second, sender, start)

	listed := nearby.list(start)
	require.Len(t, listed, 1)
	require.Equal(t, shown, listed[0].Address)
	require.ElementsMatch(t, addresses(t, "192.168.1.26", "172.31.240.43"), listed[0].Addresses)
}

// A machine on several networks answers on each with that network's address
// alone. The row holds every one of them and keeps showing the same one, so a
// connected printer is matched whichever network it dials from.
func TestEachNetworksAnswerAddsItsAddressToOneRow(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	nearby.observe(announcement("Workshop", connectService, "workshop", 8080, "192.168.139.3", 4500), sender, start)
	nearby.observe(announcement("Workshop", connectService, "workshop", 8080, "192.168.10.123", 4500), sender, start)
	shown := nearby.list(start)[0].Address

	nearby.observe(announcement("Workshop", connectService, "workshop", 8080, "192.168.139.3", 4500), sender, start)

	listed := nearby.list(start)
	require.Len(t, listed, 1)
	require.Equal(t, netip.MustParseAddr("192.168.10.123"), shown)
	require.Equal(t, shown, listed[0].Address, "the row moved to the address of the last answer")
	require.Equal(t, addresses(t, "192.168.10.123", "192.168.139.3"), listed[0].Addresses)
}

// An address record sent with a time to live of zero withdraws that address
// alone, and the row shows the next one.
func TestAGoodbyeForAnAddressDropsThatAddress(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	first := announcement("Workshop", linkService, "workshop", 8080, "172.31.240.43", 4500)
	first.Extra = append(first.Extra, addressRecord("workshop", "192.168.1.26", 120))
	nearby.observe(first, sender, start)

	bye := announcement("Workshop", linkService, "workshop", 8080, "192.168.1.26", 4500)
	bye.Extra = append(bye.Extra, addressRecord("workshop", "172.31.240.43", 0))
	nearby.observe(bye, sender, start)

	listed := nearby.list(start)
	require.Equal(t, netip.MustParseAddr("192.168.1.26"), listed[0].Address)
	require.Equal(t, addresses(t, "192.168.1.26"), listed[0].Addresses)
}

// An address lives as long as the record that carried it. The record is asked
// for again only at 80 percent of its time to live, long after the address
// record's two minutes.
func TestAnAddressOutlivesItsOwnTimeToLiveWhileTheRecordLives(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	nearby.observe(announcement("Desk", linkService, "desk", 80, "192.168.1.20", 4500), sender, start)

	require.Len(t, nearby.list(start.Add(time.Hour)), 1, "the printer left the list when its address record ran out")
}

// A record keeps at most maxAddresses, whatever an answer carries.
func TestARecordKeepsABoundedNumberOfAddresses(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	flood := announcement("Desk", linkService, "desk", 80, "192.168.1.1", 4500)

	for i := 2; i < 40; i++ {
		flood.Extra = append(flood.Extra, addressRecord("desk", fmt.Sprintf("192.168.1.%d", i), 120))
	}

	nearby.observe(flood, sender, start)

	require.Len(t, nearby.list(start)[0].Addresses, maxAddresses)
}

// A printer first heard over IPv6 alone moves to its IPv4 address once an
// answer names one, so the link uses IPv4, as pick orders them.
func TestAKeptIPv6AddressYieldsToIPv4(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	first := announcement("Desk", linkService, "desk", 80, "", 4500)
	first.Extra = []dns.RR{&dns.AAAA{
		Hdr:  dns.RR_Header{Name: "desk.local.", Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 120},
		AAAA: net.ParseIP("2001:db8::20"),
	}}
	nearby.observe(first, sender, start)
	require.Equal(t, netip.MustParseAddr("2001:db8::20"), nearby.list(start)[0].Address)

	second := announcement("Desk", linkService, "desk", 80, "192.168.1.20", 4500)
	second.Extra = append(second.Extra, first.Extra[0])
	nearby.observe(second, sender, start)
	require.Equal(t, netip.MustParseAddr("192.168.1.20"), nearby.list(start)[0].Address)
}

// A printer and a server that happen to announce the same name on the same
// port are two machines, so they are two rows.
func TestTwoHostsUnderOneNameAreTwoRows(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	nearby.observe(announcement("Desk", linkService, "desk-a", 80, "192.168.1.20", 4500), sender, start)
	nearby.observe(announcement("Desk", connectService, "desk-b", 80, "192.168.1.21", 4500), sender, start)

	listed := nearby.list(start)
	require.Len(t, listed, 2)
}

func TestAnotherServiceIsNotListed(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	nearby.observe(announcement("Printer", "_ipp._tcp", "office", 631, "192.168.1.27", 4500), sender, start)

	require.Empty(t, nearby.list(start))
	require.Empty(t, nearby.records)
}

func TestThisServerIsNotListed(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	nearby.self = self{host: "myself", port: 8080}

	nearby.observe(announcement("Me", linkService, "myself", 8080, "192.168.1.28", 4500), sender, start)
	nearby.observe(announcement("Neighbour", linkService, "myself", 8081, "192.168.1.28", 4500), sender, start)

	listed := nearby.list(start)
	require.Len(t, listed, 1)
	require.Equal(t, "Neighbour", listed[0].Name)
}

// TestTheSenderIsTheAddressWhenNoAddressRecordCame covers a responder that
// sends the service record without the host's address in the same message.
func TestTheSenderIsTheAddressWhenNoAddressRecordCame(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	nearby.observe(announcement("Bare", linkService, "bare", 80, "", 4500), netip.MustParseAddr("192.168.1.29"), start)

	listed := nearby.list(start)
	require.Len(t, listed, 1)
	require.Equal(t, netip.MustParseAddr("192.168.1.29"), listed[0].Address)
}

// TestAnIPv6LinkLocalSenderIsNotTakenAsTheAddress is the sender of a
// message with no address record, when that sender is an IPv6 link-local
// address. A link in a page cannot carry the interface such an address needs,
// so the printer waits for an address a page can open.
func TestAnIPv6LinkLocalSenderIsNotTakenAsTheAddress(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	nearby.observe(announcement("Bare", linkService, "bare", 80, "", 4500), netip.MustParseAddr("fe80::29%eth0"), start)

	require.Empty(t, nearby.list(start))
	require.Empty(t, nearby.records["bare._prusalink._tcp.local."].addresses)

	nearby.observe(announcement("Bare", linkService, "bare", 80, "192.168.1.29", 4500), sender, start)

	listed := nearby.list(start)
	require.Len(t, listed, 1)
	require.Equal(t, netip.MustParseAddr("192.168.1.29"), listed[0].Address)
}

func TestAPointerWithoutAServiceRecordIsNotListed(t *testing.T) {
	t.Parallel()

	msg := announcement("Half", linkService, "half", 80, "192.168.1.30", 4500)
	msg.Answer = msg.Answer[:1]
	msg.Extra = nil

	nearby := newTestTable()
	nearby.observe(msg, sender, start)

	require.Empty(t, nearby.list(start))
}

func TestTheRefreshIsDueBeforeTheRecordExpires(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	require.True(t, nearby.refreshDue().IsZero())

	nearby.observe(announcement("Due", linkService, "due", 80, "192.168.1.31", 100), sender, start)

	require.Equal(t, start.Add(80*time.Second), nearby.refreshDue())
}

func TestAnEscapedNameIsReadBack(t *testing.T) {
	t.Parallel()

	for escaped, want := range map[string]string{
		`Prusa\ Connect`: "Prusa Connect",
		`a\.b`:           "a.b",
		`caf\195\169`:    "café",
		`plain`:          "plain",
	} {
		require.Equal(t, want, unescapeLabel(escaped), escaped)
	}
}

// TestARecordAskedForIsNotAskedForAgain keeps a printer that left without a
// goodbye from making the listener ask every second until its record expires.
func TestARecordAskedForIsNotAskedForAgain(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	nearby.observe(announcement("Gone", linkService, "gone", 80, "192.168.1.32", 100), sender, start)

	nearby.asked(start.Add(80 * time.Second))

	require.True(t, nearby.refreshDue().IsZero())
}

func TestTheLinkToAPrinterNamesItsScheme(t *testing.T) {
	t.Parallel()

	require.Equal(t, "https://192.168.1.20:443/",
		Nearby{Address: netip.MustParseAddr("192.168.1.20"), Port: 443, TLS: true}.URL())
	require.Equal(t, "http://[2001:db8::1]:8080/",
		Nearby{Address: netip.MustParseAddr("2001:db8::1"), Port: 8080}.URL())
}

// TestAnAddressRecordWithoutItsServiceRecordIsIgnored keeps a stranger from
// pointing a listed printer at another address. An address is taken only from
// the message whose service record names the host, or from that message's
// sender.
func TestAnAddressRecordWithoutItsServiceRecordIsIgnored(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	nearby.observe(announcement("Desk", linkService, "desk", 80, "192.168.1.20", 4500), sender, start)

	spoof := new(dns.Msg)
	spoof.Response = true
	spoof.Answer = []dns.RR{&dns.A{
		Hdr: dns.RR_Header{Name: "desk.local.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 120},
		A:   net.ParseIP("192.168.1.99"),
	}}
	nearby.observe(spoof, netip.MustParseAddr("192.168.1.99"), start)

	listed := nearby.list(start)
	require.Len(t, listed, 1)
	require.Equal(t, netip.MustParseAddr("192.168.1.20"), listed[0].Address)
}

// TestAFullListSweepsOncePerMessage keeps a message full of new pointers from
// walking the whole list once for each of them.
func TestAFullListSweepsOncePerMessage(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()

	for i := range MaxNearby {
		nearby.observe(announcement(fmt.Sprintf("printer-%03d", i), linkService, fmt.Sprintf("host-%03d", i),
			80, fmt.Sprintf("10.0.%d.%d", i/200, i%200+1), 4500), sender, start)
	}

	crowd := new(dns.Msg)
	crowd.Response = true

	for i := range 10 {
		crowd.Answer = append(crowd.Answer, &dns.PTR{
			Hdr: dns.RR_Header{Name: linkService + ".local.", Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: 4500},
			Ptr: fmt.Sprintf("extra-%d.%s.local.", i, linkService),
		})
	}

	nearby.sweeps = 0
	refused := nearby.observe(crowd, sender, start)

	require.Equal(t, 10, refused)
	require.Equal(t, 1, nearby.sweeps)
}

// TestThisServerIsNotListedWhateverTheCaseOfItsName is a Windows host name,
// which the system reports in capitals. The record carries it in lower case,
// and DNS names compare without case.
func TestThisServerIsNotListedWhateverTheCaseOfItsName(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	nearby.self = self{host: "DESKTOP-4F2K", port: 8080}

	nearby.observe(announcement("Me", linkService, "desktop-4f2k", 8080, "192.168.1.28", 4500), sender, start)

	require.Empty(t, nearby.list(start))
}

// TestThisServerIsNotListedAfterARename covers a host name the responder
// changed because another machine on the network already used it.
func TestThisServerIsNotListedAfterARename(t *testing.T) {
	t.Parallel()

	nearby := newTestTable()
	nearby.self = self{host: "desk-2", port: 8080}

	nearby.observe(announcement("Me", linkService, "desk-2", 8080, "192.168.1.28", 4500), sender, start)
	nearby.observe(announcement("Other desk", linkService, "desk", 8080, "192.168.1.29", 4500), sender, start)

	listed := nearby.list(start)
	require.Len(t, listed, 1)
	require.Equal(t, "Other desk", listed[0].Name)
}

// TestAGoodbyeLaterInTheMessageLeavesTheTableWhole is the table under the
// listener, with no recover around it. A response that gives a service
// record and then its goodbye takes the record out and reads nothing after.
func TestAGoodbyeLaterInTheMessageLeavesTheTableWhole(t *testing.T) {
	t.Parallel()

	msg := announcement("Gone", linkService, "gone", 80, "", 4500)
	msg.Answer = append(msg.Answer[:2], &dns.SRV{
		Hdr: dns.RR_Header{
			Name:   "Gone." + linkService + ".local.",
			Rrtype: dns.TypeSRV,
			Class:  dns.ClassINET,
			Ttl:    0,
		},
		Port:   80,
		Target: "gone.local.",
	})

	nearby := newTestTable()
	nearby.observe(roundTrip(msg), sender, start)

	require.Empty(t, nearby.list(start))
}
