package discovery

import (
	"context"
	"log/slog"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
	"unicode/utf8"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/require"
)

// The two service names the shipped configuration announces under.
const (
	linkService    = "_prusalink._tcp"
	connectService = "_prusa-connect._tcp"
)

// deskIndex is the index of the one interface the fake network has.
const deskIndex = 2

// deskLink is that interface. Its addresses are inside workshopLink's
// prefixes, so the filter reads it as the link the packets arrive on.
var deskLink = []announcedInterface{{
	index: deskIndex,
	name:  "eth0",
	addresses: []netip.Addr{
		netip.MustParseAddr("192.168.1.5"),
		netip.MustParseAddr("fe80::5"),
	},
}}

// peer is another machine on the link.
var peer = netip.MustParseAddrPort("192.168.1.20:5353")

// written is one message the responder sent.
type written struct {
	msg *dns.Msg
	// to is the address of a unicast message. It is not valid for a
	// multicast one.
	to    netip.AddrPort
	index int
	at    time.Time
}

// fakeNetwork is the link as a test sees it. A case writes packets into it,
// changes its interfaces, and reads back what the responder sent.
type fakeNetwork struct {
	packets chan packet
	changes chan struct{}

	mu     sync.Mutex
	links  []announcedInterface
	reads  int
	sent   []written
	opens  int
	closed int
}

func newFakeNetwork() *fakeNetwork {
	return &fakeNetwork{packets: make(chan packet), changes: make(chan struct{}, 1), links: deskLink}
}

func (f *fakeNetwork) interfaces() []announcedInterface {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.reads++

	return slices.Clone(f.links)
}

func (f *fakeNetwork) interfacesChanged() <-chan struct{} { return f.changes }

// setLinks changes the interfaces, tells the responder and waits until it
// has read them.
func (f *fakeNetwork) setLinks(links []announcedInterface) {
	f.mu.Lock()
	f.links = links
	f.mu.Unlock()

	f.changes <- struct{}{}

	synctest.Wait()
}

// interfaceReads is how often the responder asked for the interfaces.
func (f *fakeNetwork) interfaceReads() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.reads
}

func (f *fakeNetwork) multicast(msg *dns.Msg, index int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.sent = append(f.sent, written{msg: roundTrip(msg), index: index, at: time.Now()})
}

func (f *fakeNetwork) unicast(msg *dns.Msg, to netip.AddrPort) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.sent = append(f.sent, written{msg: roundTrip(msg), to: to, at: time.Now()})
}

func (f *fakeNetwork) receive() <-chan packet { return f.packets }

func (f *fakeNetwork) close() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.closed++
}

func (f *fakeNetwork) open() (network, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.opens++

	return f, nil
}

// all is every message sent so far, mark is how many that is, and since is
// every message sent after a mark.
func (f *fakeNetwork) all() []written {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.sent)
}

func (f *fakeNetwork) mark() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.sent)
}

func (f *fakeNetwork) since(mark int) []written {
	return f.all()[mark:]
}

// deliver hands the responder one message from the link and waits until it
// has been read. The message goes through the wire form first, as a real one
// does, so every name arrives written the way the DNS library writes it.
func (f *fakeNetwork) deliver(msg *dns.Msg, from netip.AddrPort, to netip.Addr) {
	f.packets <- packet{msg: roundTrip(msg), from: from, to: to, iface: "eth0", index: deskIndex}

	synctest.Wait()
}

// fakeAnnouncer is an announcer on the fake network, for the host desk and
// the instance Desk.
func fakeAnnouncer(over *fakeNetwork) *Announcer {
	announcer := New(Config{
		Enabled:  true,
		Services: []string{linkService, connectService},
		Instance: "Desk",
		Version:  "1.2.3",
	}, quiet(), NewLink(quiet(), nil))
	announcer.base = "desk"
	announcer.open = over.open
	announcer.filter = newLinkFilter(workshopLink)
	announcer.delay = middle

	return announcer
}

// roundTrip is a message packed to the wire form and read back, which is
// what the link hands on and what a machine on the link reads. A nil message
// stays nil.
func roundTrip(msg *dns.Msg) *dns.Msg {
	if msg == nil {
		return nil
	}

	packed, err := msg.Pack()
	if err != nil {
		panic("a test message does not pack: " + err.Error())
	}

	back := new(dns.Msg)
	if unpacked := back.Unpack(packed); unpacked != nil {
		panic("a test message does not read back: " + unpacked.Error())
	}

	return back
}

// middle is the random wait a test takes: halfway between its bounds, so a
// case can tell a wait from none.
func middle(least, most time.Duration) time.Duration {
	return (least + most) / 2
}

// waitOut lets a held answer go out.
func waitOut() {
	time.Sleep(sharedMost)
	synctest.Wait()
}

// settle lets the probes and both announcements go out.
func settle() {
	time.Sleep(3 * time.Second)
	synctest.Wait()
}

// The kinds of message a responder sends.
func isProbe(one written) bool {
	return !one.msg.Response && len(one.msg.Ns) > 0
}

func isAnnouncement(one written) bool {
	return one.msg.Response && !one.to.IsValid() && len(one.msg.Answer) > 0 && one.msg.Answer[0].Header().Ttl > 0
}

func isGoodbye(one written) bool {
	return one.msg.Response && len(one.msg.Answer) > 0 &&
		!slices.ContainsFunc(one.msg.Answer, func(rr dns.RR) bool { return rr.Header().Ttl > 0 })
}

func count(sent []written, kind func(written) bool) int {
	return len(slices.DeleteFunc(slices.Clone(sent), func(one written) bool { return !kind(one) }))
}

// question is a query for one name, from a sender.
func question(name string, qtype uint16) *dns.Msg {
	msg := new(dns.Msg)
	msg.Question = []dns.Question{{Name: name, Qtype: qtype, Qclass: dns.ClassINET}}

	return msg
}

// claim is a response from another machine that holds the instance Desk under
// the link service, on another host. claimOf is the same for another name.
func claim() *dns.Msg {
	return claimOf("Desk")
}

func claimOf(instance string) *dns.Msg {
	msg := new(dns.Msg)
	msg.Response = true
	msg.Answer = []dns.RR{&dns.SRV{
		Hdr: dns.RR_Header{
			Name:   escapeLabel(instance) + "." + linkService + ".local.",
			Rrtype: dns.TypeSRV, Class: dns.ClassINET, Ttl: 120,
		},
		Port:   80,
		Target: "other.local.",
	}}

	return msg
}

// TestTheResponderProbesThenAnnouncesTwice is the start of RFC 6762 sections
// 8.1 and 8.3: a random wait of up to 250 ms, three probes 250 ms apart, then
// two announcements a second apart, and nothing more. The wait here is the
// middle of its range.
func TestTheResponderProbesThenAnnouncesTwice(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)
		begun := time.Now()

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		time.Sleep(10 * time.Second)
		synctest.Wait()

		var moments []time.Duration

		for _, one := range over.all() {
			require.Equal(t, deskIndex, one.index)
			moments = append(moments, one.at.Sub(begun))
		}

		require.Equal(t, []time.Duration{
			125 * time.Millisecond, 375 * time.Millisecond, 625 * time.Millisecond,
			875 * time.Millisecond, 1875 * time.Millisecond,
		}, moments)
		require.Equal(t, 3, count(over.all(), isProbe))
		require.Equal(t, 2, count(over.all(), isAnnouncement))
	})
}

// TestTheAnnouncementCarriesEveryRecord is the record set of ADR-20260916-b3x rules 1
// to 4: a pointer and the service list for each name, the service and text
// records of the instance, and the host's addresses on that interface. A
// unique record carries the cache-flush bit, and each carries the time to
// live RFC 6762 section 10 gives it.
func TestTheAnnouncementCarriesEveryRecord(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()

		sent := over.all()
		first := sent[slices.IndexFunc(sent, isAnnouncement)].msg

		var lines []string
		for _, rr := range first.Answer {
			lines = append(lines, rr.String())
		}

		require.ElementsMatch(t, []string{
			"_services._dns-sd._udp.local.\t4500\tIN\tPTR\t_prusalink._tcp.local.",
			"_prusalink._tcp.local.\t4500\tIN\tPTR\tDesk._prusalink._tcp.local.",
			"Desk._prusalink._tcp.local.\t120\tCLASS32769\tSRV\t0 0 8080 desk.local.",
			"Desk._prusalink._tcp.local.\t4500\tCLASS32769\tTXT\t\"api=v1\" \"path=/\" \"version=1.2.3\"",
			"_services._dns-sd._udp.local.\t4500\tIN\tPTR\t_prusa-connect._tcp.local.",
			"_prusa-connect._tcp.local.\t4500\tIN\tPTR\tDesk._prusa-connect._tcp.local.",
			"Desk._prusa-connect._tcp.local.\t120\tCLASS32769\tSRV\t0 0 8080 desk.local.",
			"Desk._prusa-connect._tcp.local.\t4500\tCLASS32769\tTXT\t\"api=v1\" \"path=/\" \"version=1.2.3\"",
			"desk.local.\t120\tCLASS32769\tA\t192.168.1.5",
			"desk.local.\t120\tCLASS32769\tAAAA\tfe80::5",
		}, lines)
	})
}

// TestStoppingSaysGoodbye is RFC 6762 section 10.1 when the listeners drain.
// Every record goes out once more with a time to live of zero, and the socket
// is closed.
func TestStoppingSaysGoodbye(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		settle()
		announcer.Stop(t.Context())

		require.Equal(t, 1, count(over.all(), isGoodbye))
		require.Equal(t, 1, over.closed)
	})
}

// TestAQueryIsAnsweredWithWhatTheAskerNeedsNext is RFC 6763 section 12: an
// answer to a pointer question carries the service record, the text and the
// addresses, so the asker needs no second question.
func TestAQueryIsAnsweredWithWhatTheAskerNeedsNext(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()

		asked := over.mark()
		over.deliver(question(linkService+".local.", dns.TypePTR), peer, groupIPv4)
		waitOut()

		answered := over.since(asked)
		require.Len(t, answered, 1)
		require.False(t, answered[0].to.IsValid(), "a question from port 5353 is answered by multicast")
		require.Len(t, answered[0].msg.Answer, 1)
		require.Equal(t, "Desk._prusalink._tcp.local.", answered[0].msg.Answer[0].(*dns.PTR).Ptr)

		var extra []uint16
		for _, rr := range answered[0].msg.Extra {
			extra = append(extra, rr.Header().Rrtype)
		}

		require.ElementsMatch(t, []uint16{dns.TypeSRV, dns.TypeTXT, dns.TypeA, dns.TypeAAAA}, extra)
	})
}

// TestARecordIsMulticastAtMostOnceASecond is RFC 6762 section 6. A second
// question within the second is not answered on the link again, and one after
// it is.
func TestARecordIsMulticastAtMostOnceASecond(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()

		asked := over.mark()
		over.deliver(question(linkService+".local.", dns.TypePTR), peer, groupIPv4)
		time.Sleep(500 * time.Millisecond)
		over.deliver(question(linkService+".local.", dns.TypePTR), peer, groupIPv4)
		waitOut()
		require.Len(t, over.since(asked), 1, "the record went out twice in one second")

		time.Sleep(600 * time.Millisecond)
		over.deliver(question(linkService+".local.", dns.TypePTR), peer, groupIPv4)
		waitOut()
		require.Len(t, over.since(asked), 2, "the record was not answered again after the second")
	})
}

// TestAPlainDNSClientGetsAUnicastAnswer is RFC 6762 section 6.7. A question
// from a port other than 5353 comes from a client that does not speak mDNS.
// It gets the answer back to that port, with its own id and question, a time
// to live of ten seconds at most and no cache-flush bit.
func TestAPlainDNSClientGetsAUnicastAnswer(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()

		client := netip.MustParseAddrPort("192.168.1.20:40000")
		asked := question("Desk."+linkService+".local.", dns.TypeSRV)
		asked.Id = 4242

		begun := over.mark()
		over.deliver(asked, client, groupIPv4)

		answered := over.since(begun)
		require.Len(t, answered, 1)
		require.Equal(t, client, answered[0].to)
		require.Equal(t, uint16(4242), answered[0].msg.Id)
		require.Equal(t, asked.Question, answered[0].msg.Question)

		for _, rr := range slices.Concat(answered[0].msg.Answer, answered[0].msg.Extra) {
			require.LessOrEqual(t, rr.Header().Ttl, uint32(legacyTimeToLive), rr.String())
			require.Equal(t, uint16(dns.ClassINET), rr.Header().Class, rr.String())
		}
	})
}

// TestTheResponderDropsAPacketFromOffTheLink is RFC 6762 section 11 on the
// responder's side. A packet sent to the group is on the link whatever its
// source, so a link-local address and a second subnet on the link are
// answered. A packet sent to this machine's own address is answered only from
// a prefix of the interface or an IPv6 link-local address.
func TestTheResponderDropsAPacketFromOffTheLink(t *testing.T) {
	t.Parallel()

	own, ownV6 := netip.MustParseAddr("192.168.1.5"), netip.MustParseAddr("fe80::5")

	cases := map[string]struct {
		from string
		to   netip.Addr
		want bool
	}{
		"a unicast packet from the interface's subnet":  {"192.168.1.20:5353", own, true},
		"a unicast packet from another network":         {"203.0.113.7:5353", own, false},
		"a unicast packet from an IPv6 link-local":      {"[fe80::9]:5353", ownV6, true},
		"a unicast packet from another IPv6 network":    {"[2001:db8:9::1]:5353", ownV6, false},
		"a unicast packet from an IPv4 link-local":      {"169.254.3.4:5353", own, false},
		"a packet to the group from an IPv4 link-local": {"169.254.3.4:5353", groupIPv4, true},
		"a packet to the group from a second subnet":    {"10.9.9.9:5353", groupIPv4, true},
		"a packet to the IPv6 group from anywhere":      {"[2001:db8:9::1]:5353", groupIPv6, true},
		"a packet whose destination is not reported":    {"203.0.113.7:5353", netip.Addr{}, false},
	}

	for name, one := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				over := newFakeNetwork()
				announcer := fakeAnnouncer(over)

				announcer.Start(t.Context(), 8080)
				defer announcer.Stop(t.Context())

				settle()

				begun := over.mark()
				over.deliver(question(linkService+".local.", dns.TypePTR), netip.MustParseAddrPort(one.from), one.to)
				waitOut()

				require.Equal(t, one.want, len(over.since(begun)) == 1)
			})
		})
	}
}

// TestAConflictWhileProbingRenamesTheInstance is RFC 6762 section 9 and
// ADR-20260916-b3x rule 2. Another machine that holds the instance name makes this
// server probe again as "Desk (2)", and the host name stays as it was.
func TestAConflictWhileProbingRenamesTheInstance(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		synctest.Wait()
		over.deliver(claim(), peer, groupIPv4)
		settle()

		sent := over.all()
		first := sent[slices.IndexFunc(sent, isAnnouncement)].msg
		require.Contains(t, first.String(), `Desk\ \(2\)._prusalink._tcp.local.`)
		require.Equal(t, "desk", announcer.Host())
	})
}

// TestAResponseFromAnotherPortClaimsNothing is RFC 6762 section 6. A
// response that did not come from port 5353 is not mDNS, and it renames
// nothing.
func TestAResponseFromAnotherPortClaimsNothing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		synctest.Wait()
		over.deliver(claim(), netip.MustParseAddrPort("192.168.1.20:40000"), groupIPv4)
		settle()

		sent := over.all()
		require.Equal(t, 3, count(sent, isProbe))
		require.Contains(t, sent[slices.IndexFunc(sent, isAnnouncement)].msg.String(), "Desk._prusalink._tcp.local.")
	})
}

// TestTheHostNameFollowsARename is the host name the responder settles on
// when another machine already answers to this one's. Host reports the name
// on the wire, because the listener compares its records against it and this
// server must still not list itself.
func TestTheHostNameFollowsARename(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		taken := new(dns.Msg)
		taken.Response = true
		taken.Answer = []dns.RR{&dns.A{
			Hdr: dns.RR_Header{Name: "desk.local.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 120},
			A:   net.ParseIP("192.168.1.20"),
		}}

		synctest.Wait()
		over.deliver(taken, peer, groupIPv4)
		settle()

		require.Equal(t, "desk-2", announcer.Host())

		sent := over.all()
		last := sent[len(sent)-1].msg
		require.Contains(t, last.String(), "0 0 8080 desk-2.local.")
	})
}

// TestTheResponderHearsItsOwnAnnouncementWithoutAConflict is the packet a
// machine hears back from itself. Its own records, from any of its
// interfaces, claim nothing.
func TestTheResponderHearsItsOwnAnnouncementWithoutAConflict(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()

		sent := over.all()
		own := sent[slices.IndexFunc(sent, isAnnouncement)].msg
		begun := over.mark()

		over.deliver(own, netip.MustParseAddrPort("192.168.1.5:5353"), groupIPv4)
		time.Sleep(5 * time.Second)
		synctest.Wait()

		require.Empty(t, over.since(begun))
		require.Equal(t, "desk", announcer.Host())
	})
}

// TestAConflictAfterTheProbeProbesAgainUnderTheSameName is the second half of
// RFC 6762 section 9. A claim heard once the name is taken makes the server
// probe again as it stands, and it keeps the name when nothing answers the
// probe.
func TestAConflictAfterTheProbeProbesAgainUnderTheSameName(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()

		begun := over.mark()
		over.deliver(claim(), peer, groupIPv4)
		settle()

		again := over.since(begun)
		require.Equal(t, 3, count(again, isProbe))
		require.Equal(t, 2, count(again, isAnnouncement))
		require.Contains(t, again[len(again)-1].msg.String(), "Desk._prusalink._tcp.local.")
	})
}

// TestAProberWhoseRecordsSortHigherWinsTheTie is RFC 6762 section 8.2. Two
// machines probing for one name at once compare their records, and the one
// whose records sort lower waits a second before it probes again.
func TestAProberWhoseRecordsSortHigherWinsTheTie(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		time.Sleep(130 * time.Millisecond)
		synctest.Wait()

		rival := rivalProbe(&dns.SRV{
			Hdr: dns.RR_Header{
				Name:   "Desk." + linkService + ".local.",
				Rrtype: dns.TypeSRV,
				Class:  dns.ClassINET,
				Ttl:    120,
			},
			Port:   9999,
			Target: "zzz.local.",
		})
		lost := time.Now()
		over.deliver(rival, peer, groupIPv4)

		time.Sleep(time.Second - time.Millisecond)
		synctest.Wait()
		require.Equal(t, 1, count(over.all(), isProbe), "the loser probed again before its second")

		time.Sleep(time.Millisecond)
		synctest.Wait()

		probes := slices.DeleteFunc(over.all(), func(one written) bool { return !isProbe(one) })
		require.Len(t, probes, 2)
		require.Equal(t, time.Second, probes[1].at.Sub(lost))
	})
}

// TestATieComparesBothWholeSets is the tie of RFC 6762 section 8.2 when the
// other prober shares one record with this server. Both sides compare every
// record they propose, the shared one too. Here this server's service record
// sorts higher, so it wins and probes on without a wait. A side that left the
// shared record out of one set only would lose here, and so would the other
// side, and neither would claim the name.
func TestATieComparesBothWholeSets(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)
		begun := time.Now()

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		time.Sleep(130 * time.Millisecond)
		synctest.Wait()

		name := "Desk." + linkService + ".local."
		over.deliver(rivalProbe(
			&dns.TXT{
				Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 4500},
				Txt: []string{"api=v1", "path=/", "version=1.2.3"},
			},
			&dns.SRV{
				Hdr:  dns.RR_Header{Name: name, Rrtype: dns.TypeSRV, Class: dns.ClassINET, Ttl: 120},
				Port: 80, Target: "desk.local.",
			},
		), peer, groupIPv4)

		time.Sleep(300 * time.Millisecond)
		synctest.Wait()

		probes := slices.DeleteFunc(over.all(), func(one written) bool { return !isProbe(one) })
		require.Len(t, probes, 2, "the winner of the tie waited")
		require.Equal(t, 375*time.Millisecond, probes[1].at.Sub(begun))
	})
}

// rivalProbe is a probe from another machine for the instance Desk, with the
// records it proposes.
func rivalProbe(proposed ...dns.RR) *dns.Msg {
	msg := question("Desk."+linkService+".local.", dns.TypeANY)
	msg.Ns = proposed

	return msg
}

// TestAPeerThatKeepsClaimingTheNameIsMadeToWait is the limit of RFC 6762
// section 8.1. Fifteen conflicts in ten seconds hold the next probe back five
// seconds, so a machine that claims every name it hears cannot keep this one
// busy.
func TestAPeerThatKeepsClaimingTheNameIsMadeToWait(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		synctest.Wait()

		for i := range conflictBurst {
			over.deliver(claimOf(renamed("Desk", i, instanceSuffix)), peer, groupIPv4)
		}

		paused := over.mark()

		time.Sleep(conflictPause - time.Millisecond)
		synctest.Wait()
		require.Empty(t, over.since(paused))

		time.Sleep(time.Millisecond)
		synctest.Wait()
		require.Equal(t, 1, count(over.since(paused), isProbe))
	})
}

// TestTheNamesFitInOneLabel keeps a long title on the wire. A DNS label holds
// 63 bytes, and a record whose name is longer cannot be sent at all.
func TestTheNamesFitInOneLabel(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("é", 40)

	for _, name := range []string{
		renamed(long, 0, instanceSuffix),
		renamed(long, 1, instanceSuffix),
		renamed(strings.Repeat("h", 70), 12, hostSuffix),
	} {
		require.LessOrEqual(t, len(name), maxLabel, name)
		require.True(t, utf8.ValidString(name), name)
	}

	require.Equal(t, "Desk (2)", renamed("Desk", 1, instanceSuffix))
	require.Equal(t, "desk-3", renamed("desk", 2, hostSuffix))
}

// TestATitleWithADotIsOneLabel is a title the way a person writes it. The
// dot and the backslash stay in the instance name, and the name reads back as
// the title.
func TestATitleWithADotIsOneLabel(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)
		announcer.config.Instance = `Lab 2.1 \ MK4`

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()

		sent := over.all()
		packed, err := sent[slices.IndexFunc(sent, isAnnouncement)].msg.Pack()
		require.NoError(t, err)

		back := new(dns.Msg)
		require.NoError(t, back.Unpack(packed))

		pointer := findRecord[*dns.PTR](t, withName(back.Answer, linkService+".local."))
		require.Equal(t, `Lab 2.1 \ MK4`, instanceName(pointer.Ptr, linkService+".local."))
	})
}

// TestAConflictingPeerLeavesNoGoroutineAfterStop is a peer that claims the
// name again and again, so the responder probes again each time. A probe
// starts nothing, and the bubble ends with no goroutine left.
func TestAConflictingPeerLeavesNoGoroutineAfterStop(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		announcer.Start(ctx, 8080)

		for range 40 {
			over.deliver(claim(), peer, groupIPv4)
			time.Sleep(100 * time.Millisecond)
		}

		announcer.Stop(ctx)
		synctest.Wait()

		require.Equal(t, 1, over.opens)
		require.Equal(t, 1, over.closed)
	})
}

// systemHostAnnouncer is fakeAnnouncer on a machine whose own responder
// answers for the host name (ADR-20261103-qag).
func systemHostAnnouncer(over *fakeNetwork) *Announcer {
	announcer := fakeAnnouncer(over)
	announcer.config.SystemHost = true

	return announcer
}

// TestASystemHostAnnouncesOnlyTheServices leaves the host name to the
// machine's own responder: no address record goes out, and each SRV names the
// host that responder answers for.
func TestASystemHostAnnouncesOnlyTheServices(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := systemHostAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()

		sent := over.all()
		first := sent[slices.IndexFunc(sent, isAnnouncement)].msg

		var lines []string
		for _, rr := range first.Answer {
			lines = append(lines, rr.String())
		}

		require.ElementsMatch(t, []string{
			"_services._dns-sd._udp.local.\t4500\tIN\tPTR\t_prusalink._tcp.local.",
			"_prusalink._tcp.local.\t4500\tIN\tPTR\tDesk._prusalink._tcp.local.",
			"Desk._prusalink._tcp.local.\t120\tCLASS32769\tSRV\t0 0 8080 desk.local.",
			"Desk._prusalink._tcp.local.\t4500\tCLASS32769\tTXT\t\"api=v1\" \"path=/\" \"version=1.2.3\"",
			"_services._dns-sd._udp.local.\t4500\tIN\tPTR\t_prusa-connect._tcp.local.",
			"_prusa-connect._tcp.local.\t4500\tIN\tPTR\tDesk._prusa-connect._tcp.local.",
			"Desk._prusa-connect._tcp.local.\t120\tCLASS32769\tSRV\t0 0 8080 desk.local.",
			"Desk._prusa-connect._tcp.local.\t4500\tCLASS32769\tTXT\t\"api=v1\" \"path=/\" \"version=1.2.3\"",
		}, lines)

		require.Positive(t, count(sent, isProbe), "a system host sent no probe for its instance names")

		for _, one := range sent {
			if !isProbe(one) {
				continue
			}

			for _, asked := range one.msg.Question {
				require.NotEqual(t, "desk.local.", asked.Name, "a system host's name was probed for")
			}
		}
	})
}

// TestASystemHostKeepsItsNameWhenAnotherAnswersForIt is the machine's own
// responder answering for the host name. That is not a conflict, so nothing
// is renamed and the SRV still names the host.
func TestASystemHostKeepsItsNameWhenAnotherAnswersForIt(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := systemHostAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		system := new(dns.Msg)
		system.Response = true
		system.Answer = []dns.RR{&dns.A{
			Hdr: dns.RR_Header{Name: "desk.local.", Rrtype: dns.TypeA, Class: dns.ClassINET | topBit, Ttl: 120},
			A:   net.ParseIP("192.168.1.5"),
		}}

		synctest.Wait()
		over.deliver(system, thisMachine, groupIPv4)
		settle()

		require.Equal(t, "desk", announcer.Host())
	})
}

// TestASystemHostLeavesTheAddressQuestionsToTheSystem is a client asking for
// the host's address. The machine's own responder answers it, and this one
// stays quiet.
func TestASystemHostLeavesTheAddressQuestionsToTheSystem(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := systemHostAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()
		// Past the once-a-second limit, so a quiet answer is not a limited one.
		time.Sleep(2 * time.Second)
		synctest.Wait()

		for _, qtype := range []uint16{dns.TypeA, dns.TypeAAAA, dns.TypeANY} {
			mark := over.mark()
			over.deliver(question("desk.local.", qtype), peer, groupIPv4)
			waitOut()
			require.Empty(t, over.since(mark), "a system host answered %s for its host name", dns.TypeToString[qtype])
		}
	})
}

// thisMachine is this machine's own responder on the link: it sends from the
// machine's address, as Avahi does.
var thisMachine = netip.MustParseAddrPort("192.168.1.5:5353")

// reverseOf is the PTR the machine's own responder holds for one of its
// addresses: the reverse name of the address, pointing at a host name.
func reverseOf(addr, host string, ttl uint32) dns.RR {
	name, err := dns.ReverseAddr(addr)
	if err != nil {
		panic(err)
	}

	return &dns.PTR{
		Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypePTR, Class: dns.ClassINET | topBit, Ttl: ttl},
		Ptr: host,
	}
}

// systemAnswer is a response holding one record about a name and an address.
func systemAnswer(rr dns.RR) *dns.Msg {
	msg := new(dns.Msg)
	msg.Response = true
	msg.Answer = []dns.RR{rr}

	return msg
}

// TestASystemHostAsksWhichNameHoldsItsAddresses is how a system host learns
// the name the machine's responder holds: with its first probe it asks for the
// reverse name of each address it announces on.
func TestASystemHostAsksWhichNameHoldsItsAddresses(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := systemHostAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()

		var asked []string

		for _, one := range over.all() {
			if one.msg.Response {
				continue
			}

			for _, question := range one.msg.Question {
				if question.Qtype == dns.TypePTR {
					asked = append(asked, question.Name)
				}
			}
		}

		require.ElementsMatch(t, []string{
			"5.1.168.192.in-addr.arpa.",
			"5.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.8.e.f.ip6.arpa.",
		}, asked)
	})
}

// TestASystemHostFollowsTheNameTheSystemHolds is the machine's responder
// holding another name than the one this process read, because it renamed the
// machine or read the name before it changed. The SRV records follow it, so a
// client that resolves the service reaches this machine.
func TestASystemHostFollowsTheNameTheSystemHolds(t *testing.T) {
	t.Parallel()

	for name, answer := range map[string]dns.RR{
		"the reverse name of its IPv4 address": reverseOf("192.168.1.5", "desk-2.local.", 120),
		"the reverse name of its IPv6 address": reverseOf("fe80::5", "desk-2.local.", 120),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				over := newFakeNetwork()
				announcer := systemHostAnnouncer(over)

				announcer.Start(t.Context(), 8080)
				defer announcer.Stop(t.Context())

				settle()
				over.deliver(systemAnswer(answer), thisMachine, groupIPv4)
				settle()

				require.Equal(t, "desk-2", announcer.Host())

				sent := over.all()
				last := sent[len(sent)-1-slices.IndexFunc(reversed(sent), isAnnouncement)].msg

				var lines []string
				for _, rr := range last.Answer {
					lines = append(lines, rr.String())
				}

				require.ElementsMatch(t, []string{
					"_services._dns-sd._udp.local.\t4500\tIN\tPTR\t_prusalink._tcp.local.",
					"_prusalink._tcp.local.\t4500\tIN\tPTR\tDesk._prusalink._tcp.local.",
					"Desk._prusalink._tcp.local.\t120\tCLASS32769\tSRV\t0 0 8080 desk-2.local.",
					"Desk._prusalink._tcp.local.\t4500\tCLASS32769\tTXT\t\"api=v1\" \"path=/\" \"version=1.2.3\"",
					"_services._dns-sd._udp.local.\t4500\tIN\tPTR\t_prusa-connect._tcp.local.",
					"_prusa-connect._tcp.local.\t4500\tIN\tPTR\tDesk._prusa-connect._tcp.local.",
					"Desk._prusa-connect._tcp.local.\t120\tCLASS32769\tSRV\t0 0 8080 desk-2.local.",
					"Desk._prusa-connect._tcp.local.\t4500\tCLASS32769\tTXT\t\"api=v1\" \"path=/\" \"version=1.2.3\"",
				}, lines)
			})
		})
	}
}

// TestASystemHostIgnoresTheNameOfAnotherMachine keeps a name that holds an
// address of another machine, which says nothing about this one.
func TestASystemHostIgnoresTheNameOfAnotherMachine(t *testing.T) {
	t.Parallel()

	ours := func(name string, ttl uint32) dns.RR { return reverseOf("192.168.1.5", name, ttl) }

	for name, tc := range map[string]struct {
		answer dns.RR
		from   netip.AddrPort
	}{
		"an address of another machine": {
			answer: &dns.A{
				Hdr: dns.RR_Header{Name: "bench.local.", Rrtype: dns.TypeA, Class: dns.ClassINET | topBit, Ttl: 120},
				A:   net.ParseIP("192.168.1.20"),
			},
			from: thisMachine,
		},
		"the reverse name of another machine": {
			answer: &dns.PTR{
				Hdr: dns.RR_Header{
					Name: "20.1.168.192.in-addr.arpa.", Rrtype: dns.TypePTR, Class: dns.ClassINET | topBit, Ttl: 120,
				},
				Ptr: "bench.local.",
			},
			from: thisMachine,
		},
		"another machine naming this one": {answer: ours("bench.local.", 120), from: peer},
		"a goodbye for an old name":       {answer: ours("bench.local.", 0), from: thisMachine},
		"a name with more than one label": {answer: ours("bench.lab.local.", 120), from: thisMachine},
		"an alias of this machine": {
			answer: &dns.A{
				Hdr: dns.RR_Header{Name: "alias.local.", Rrtype: dns.TypeA, Class: dns.ClassINET | topBit, Ttl: 120},
				A:   net.ParseIP("192.168.1.5"),
			},
			from: thisMachine,
		},
		"another machine on its link-local address": {
			answer: ours("bench.local.", 120), from: netip.MustParseAddrPort("[fe80::20%eth0]:5353"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				over := newFakeNetwork()
				announcer := systemHostAnnouncer(over)

				announcer.Start(t.Context(), 8080)
				defer announcer.Stop(t.Context())

				settle()
				over.deliver(systemAnswer(tc.answer), tc.from, groupIPv4)
				settle()

				require.Equal(t, "desk", announcer.Host())
			})
		})
	}
}

// reversed is the messages newest first.
func reversed(sent []written) []written {
	out := slices.Clone(sent)
	slices.Reverse(out)

	return out
}

// TestASystemHostLearnsFromItsLinkLocalAddress is the machine's own responder
// answering over IPv6, from the interface's link-local address with its zone.
func TestASystemHostLearnsFromItsLinkLocalAddress(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := systemHostAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()
		over.deliver(systemAnswer(reverseOf("fe80::5", "desk-2.local.", 120)),
			netip.MustParseAddrPort("[fe80::5%eth0]:5353"), groupIPv6)
		settle()

		require.Equal(t, "desk-2", announcer.Host())
	})
}

// TestASystemHostDoesNotSwingToAnAlias is a machine whose responder holds an
// alias beside its host name, both for one address. Packets that carry one
// name or the other do not move the host, so the records do not swing between
// them. Only the reverse name of an address names the host, and it is unique.
func TestASystemHostDoesNotSwingToAnAlias(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := systemHostAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()

		named := func(name string) *dns.Msg {
			return systemAnswer(&dns.A{
				Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeA, Class: dns.ClassINET | topBit, Ttl: 120},
				A:   net.ParseIP("192.168.1.5"),
			})
		}

		mark := over.mark()
		over.deliver(named("alias.local."), thisMachine, groupIPv4)
		settle()
		over.deliver(named("desk.local."), thisMachine, groupIPv4)
		settle()

		require.Equal(t, "desk", announcer.Host())
		require.Zero(t, count(over.since(mark), isAnnouncement), "an alias moved the host")
	})
}

// TestASystemHostReadsItsNameWithoutCase is a machine whose name is written
// with capitals. The responder answers in lower case, which is the same name.
func TestASystemHostReadsItsNameWithoutCase(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := systemHostAnnouncer(over)
		announcer.base = "Desk"

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()

		mark := over.mark()
		over.deliver(systemAnswer(reverseOf("192.168.1.5", "desk.local.", 120)), thisMachine, groupIPv4)
		settle()

		require.Equal(t, "Desk", announcer.Host())
		require.Zero(t, count(over.since(mark), isAnnouncement), "the same name in lower case was announced again")
	})
}

// TestASystemHostAsksAgainWhenTheInterfacesChange is a new address, whose name
// the machine's responder may hold under another name.
func TestASystemHostAsksAgainWhenTheInterfacesChange(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := systemHostAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()

		mark := over.mark()
		over.setLinks([]announcedInterface{{
			index:     deskIndex,
			name:      "eth0",
			addresses: []netip.Addr{netip.MustParseAddr("192.168.1.6")},
		}})
		settle()

		var asked []string

		for _, one := range over.since(mark) {
			for _, question := range one.msg.Question {
				if !one.msg.Response && question.Qtype == dns.TypePTR {
					asked = append(asked, question.Name)
				}
			}
		}

		require.Equal(t, []string{"6.1.168.192.in-addr.arpa."}, asked)
	})
}

// linesHandler keeps every log line as its message and its attributes.
type linesHandler struct {
	mu    sync.Mutex
	lines []string
}

func (h *linesHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *linesHandler) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *linesHandler) WithGroup(string) slog.Handler            { return h }

func (h *linesHandler) Handle(_ context.Context, record slog.Record) error {
	line := record.Message

	record.Attrs(func(attr slog.Attr) bool {
		line += " " + attr.String()

		return true
	})

	h.mu.Lock()
	defer h.mu.Unlock()

	h.lines = append(h.lines, line)

	return nil
}

func (h *linesHandler) all() []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	return slices.Clone(h.lines)
}

// TestASystemHostSaysWhenItTakesANewName is the line an operator reads when
// a slicer finds the server under another host than the one it started with.
func TestASystemHostSaysWhenItTakesANewName(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := systemHostAnnouncer(over)
		logged := &linesHandler{}
		announcer.logger = slog.New(logged)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()
		over.deliver(systemAnswer(reverseOf("192.168.1.5", "desk-2.local.", 120)), thisMachine, groupIPv4)
		settle()

		require.Contains(t, logged.all(),
			"the services now name the host the system's responder holds was=desk.local host=desk-2.local")
	})
}
