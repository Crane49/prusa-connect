package discovery

import (
	"log/slog"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"testing"
	"testing/synctest"
	"time"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/discovery/networktest"
)

// TestANetworkThatComesUpLaterIsAnnouncedOn is a machine whose network comes
// up after the server started. The responder waits with nothing to send, and
// probes and announces on the link once it appears.
func TestANetworkThatComesUpLaterIsAnnouncedOn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		over.links = nil
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		time.Sleep(10 * time.Minute)
		synctest.Wait()
		require.Empty(t, over.all())

		over.setLinks(deskLink)
		settle()

		sent := over.all()
		require.Equal(t, 3, count(sent, isProbe))
		require.Equal(t, 2, count(sent, isAnnouncement))

		for _, one := range sent {
			require.Equal(t, deskIndex, one.index)
		}
	})
}

// TestALinkThatGoesGetsAGoodbye is an interface whose address changes, and
// then one that disappears. The first gets a goodbye for the address that
// went and is announced again. The second gets a goodbye for every record.
func TestALinkThatGoesGetsAGoodbye(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()

		moved := slices.Clone(deskLink)
		moved[0].addresses = []netip.Addr{netip.MustParseAddr("192.168.1.6"), netip.MustParseAddr("fe80::5")}

		mark := over.mark()
		over.setLinks(moved)

		withdrawn := over.since(mark)
		require.Len(t, withdrawn, 1)
		require.True(t, isGoodbye(withdrawn[0]))
		require.Equal(t, []string{"desk.local.\t0\tCLASS32769\tA\t192.168.1.5"}, lines(withdrawn[0].msg.Answer))

		settle()
		require.Equal(t, 2, count(over.since(mark), isAnnouncement))

		mark = over.mark()
		over.setLinks(nil)

		gone := over.since(mark)
		require.Len(t, gone, 1)
		require.True(t, isGoodbye(gone[0]))
		require.Equal(t, deskIndex, gone[0].index)
		require.Len(t, gone[0].msg.Answer, 10)
	})
}

// TestAPacketFullOfOurRecordsReadsNoInterface is the cost of one packet. A
// response that repeats this server's own address record hundreds of times,
// and the server's own announcement heard back, are judged against records
// built once, and no interface of the machine is read for them.
func TestAPacketFullOfOurRecordsReadsNoInterface(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()

		sent := over.all()
		own := sent[slices.IndexFunc(sent, isAnnouncement)].msg

		flood := new(dns.Msg)
		flood.Response = true

		for range 561 {
			flood.Answer = append(flood.Answer, &dns.A{
				Hdr: dns.RR_Header{Name: "desk.local.", Rrtype: dns.TypeA, Class: dns.ClassINET | topBit, Ttl: 120},
				A:   net.ParseIP("192.168.1.5").To4(),
			})
		}

		reads, mark := over.interfaceReads(), over.mark()

		over.deliver(flood, peer, groupIPv4)
		over.deliver(own, netip.MustParseAddrPort("192.168.1.5:5353"), groupIPv4)

		require.Equal(t, reads, over.interfaceReads(), "a packet made the responder read the interfaces")
		require.Empty(t, over.since(mark))
	})
}

// TestAQueryPastTheLimitsIsNotRead holds one query to the first 64 questions
// and the first 256 known answers. A question past the limit gets no answer,
// and a known answer past it does not hold an answer back.
func TestAQueryPastTheLimitsIsNotRead(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()

		late := new(dns.Msg)
		for i := range maxQuestions {
			late.Question = append(late.Question,
				dns.Question{Name: "other-" + strconv.Itoa(i) + ".local.", Qtype: dns.TypeA, Qclass: dns.ClassINET})
		}

		late.Question = append(late.Question,
			dns.Question{Name: "desk.local.", Qtype: dns.TypeA, Qclass: dns.ClassINET})

		mark := over.mark()
		over.deliver(late, peer, groupIPv4)
		require.Empty(t, over.since(mark), "a question past the limit was answered")

		known := question("desk.local.", dns.TypeA)
		for range maxRecords {
			known.Answer = append(known.Answer, &dns.A{
				Hdr: dns.RR_Header{Name: "other.local.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 120},
				A:   net.ParseIP("192.168.1.99").To4(),
			})
		}

		known.Answer = append(known.Answer, &dns.A{
			Hdr: dns.RR_Header{Name: "desk.local.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 120},
			A:   net.ParseIP("192.168.1.5").To4(),
		})

		over.deliver(known, peer, groupIPv4)
		require.Len(t, over.since(mark), 1, "a known answer past the limit held the answer back")
	})
}

// TestConflictsAndRenamesAreBounded is a peer that claims every name the
// responder takes. The responder keeps the last fifteen conflicts and no
// more, and stops renaming at the limit.
func TestConflictsAndRenamesAreBounded(t *testing.T) {
	t.Parallel()

	over := newFakeNetwork()
	config := Config{Services: []string{linkService}, Instance: "Desk", Version: "1.2.3"}
	current := newResponder(config, "desk", 8080, over, newLinkFilter(workshopLink), middle)
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

	current.setLinks(deskLink, now)
	current.show(now)

	for range 1000 {
		now = now.Add(time.Millisecond)
		current.defend(claimOf(current.instance), now)
	}

	require.Len(t, current.conflicts, conflictBurst)
	require.Equal(t, maxRenames, current.nameTries)
	require.Equal(t, "Desk (100)", current.instance)
}

// TestProbesAreAnsweredAtMostEvery250Milliseconds is RFC 6762 section 6 for
// the answer to a probe. It may go out sooner than once a second, but not
// more than once in 250 ms, whatever the asker sends.
func TestProbesAreAnsweredAtMostEvery250Milliseconds(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()

		probe := rivalProbe(&dns.SRV{
			Hdr: dns.RR_Header{
				Name:   "Desk." + linkService + ".local.",
				Rrtype: dns.TypeSRV,
				Class:  dns.ClassINET,
				Ttl:    120,
			},
			Port:   80,
			Target: "other.local.",
		})

		mark := over.mark()

		for range 100 {
			over.deliver(probe, peer, groupIPv4)
			time.Sleep(time.Millisecond)
		}

		require.Len(t, over.since(mark), 1, "a flood of probes got more than one answer")

		time.Sleep(probeAnswerInterval)
		over.deliver(probe, peer, groupIPv4)
		require.Len(t, over.since(mark), 2)
	})
}

// TestAnOffLinkSenderIsNotAnsweredByUnicast is a packet sent to the group
// with the source of a machine elsewhere. The group address lets it in, but
// its source proves nothing. So an answer it asks for by unicast goes to the
// link instead, and a plain DNS client there gets nothing.
func TestAnOffLinkSenderIsNotAnsweredByUnicast(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()

		ask := question("Desk."+linkService+".local.", dns.TypeSRV)
		ask.Question[0].Qclass |= topBit

		mark := over.mark()
		over.deliver(ask, netip.MustParseAddrPort("203.0.113.7:5353"), groupIPv4)
		over.deliver(question("Desk."+linkService+".local.", dns.TypeTXT),
			netip.MustParseAddrPort("203.0.113.7:40000"), groupIPv4)

		answered := over.since(mark)
		require.Len(t, answered, 1)
		require.False(t, answered[0].to.IsValid(), "an off-link sender was answered at its own address")
	})
}

// TestUnicastAnswersAreLimitedToo is the limit on answers sent to one asker.
// A second question within the second gets no second answer.
func TestUnicastAnswersAreLimitedToo(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()

		ask := question("Desk."+linkService+".local.", dns.TypeSRV)
		ask.Question[0].Qclass |= topBit

		mark := over.mark()
		over.deliver(ask, peer, groupIPv4)
		time.Sleep(500 * time.Millisecond)
		over.deliver(ask, peer, groupIPv4)

		answered := over.since(mark)
		require.Len(t, answered, 1)
		require.Equal(t, peer, answered[0].to)
	})
}

// TestASharedAnswerWaits is RFC 6762 section 6. An answer that holds a shared
// record, which other machines may answer too, waits 20 to 120 ms. An answer
// of unique records alone goes at once.
func TestASharedAnswerWaits(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()

		mark := over.mark()
		over.deliver(question("Desk."+linkService+".local.", dns.TypeSRV), peer, groupIPv4)
		require.Len(t, over.since(mark), 1, "a unique answer waited")

		mark = over.mark()
		asked := time.Now()
		over.deliver(question(linkService+".local.", dns.TypePTR), peer, groupIPv4)
		require.Empty(t, over.since(mark), "a shared answer went at once")

		waitOut()

		answered := over.since(mark)
		require.Len(t, answered, 1)
		require.Equal(t, middle(sharedLeast, sharedMost), answered[0].at.Sub(asked))
	})
}

// lines is records as text, one line each.
func lines(records []dns.RR) []string {
	out := make([]string, 0, len(records))
	for _, rr := range records {
		out = append(out, rr.String())
	}

	return out
}

// TestTheAnswerRecordStaysBounded is thousands of askers at one moment, each
// from its own address. The record of recent answers keeps a fixed number of
// entries, forgets the oldest first, and forgets what is older than a second.
func TestTheAnswerRecordStaysBounded(t *testing.T) {
	t.Parallel()

	record := newAnswered()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

	for i := range 5000 {
		record.note("asker "+strconv.Itoa(i), now)
	}

	require.Equal(t, maxSent, record.size())
	require.Equal(t, maxSent, record.at.len())
	require.LessOrEqual(t, len(record.order), 2*maxSent+1)

	_, oldest := record.last("asker 0")
	_, newest := record.last("asker 4999")
	require.False(t, oldest, "the oldest answer was kept")
	require.True(t, newest, "the newest answer was forgotten")

	record.note("later", now.Add(answerInterval))
	require.Equal(t, 1, record.size(), "answers older than a second were kept")
}

// TestOneAskerOnManyPortsIsAnsweredOnce is one machine asking from a new port
// each time. The limit on unicast answers counts the machine, not the port.
func TestOneAskerOnManyPortsIsAnsweredOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()

		mark := over.mark()

		for port := range 50 {
			over.deliver(question("Desk."+linkService+".local.", dns.TypeSRV),
				netip.AddrPortFrom(peer.Addr(), uint16(40000+port)), groupIPv4)
		}

		require.Len(t, over.since(mark), 1)
	})
}

// TestAnAnswerDroppedForAFullListIsNotCounted is a shared answer that finds
// the list of held answers full. It is dropped, and not marked as sent, so
// the next question is answered.
func TestAnAnswerDroppedForAFullListIsNotCounted(t *testing.T) {
	t.Parallel()

	over := newFakeNetwork()
	config := Config{Services: []string{linkService}, Instance: "Desk", Version: "1.2.3"}
	current := newResponder(config, "desk", 8080, over, newLinkFilter(workshopLink), middle)
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

	current.setLinks(deskLink, now)
	current.phase = steady
	current.held = make([]waiting, maxWaiting)

	ask := packet{
		msg: roundTrip(question(linkService+".local.", dns.TypePTR)), from: peer, to: groupIPv4,
		iface: "eth0", index: deskIndex,
	}

	current.receive(ask, now)
	require.Len(t, current.held, maxWaiting)

	current.held = nil
	current.receive(ask, now)
	require.Len(t, current.held, 1, "an answer that was dropped counted against the limit")
}

// TestAPacketOnNoAnnouncedInterfaceIsNotAnswered is a question that arrived
// on an interface this server does not announce on. It is answered with
// nothing, rather than with the addresses of every link.
func TestAPacketOnNoAnnouncedInterfaceIsNotAnswered(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()

		mark := over.mark()
		over.packets <- packet{
			msg: roundTrip(question("desk.local.", dns.TypeA)), from: peer, to: groupIPv4,
			iface: "wlan9", index: 9,
		}
		synctest.Wait()

		require.Empty(t, over.since(mark))
	})
}

// TestTheTieSortsBothSets is compareSets on sets written out of order. The
// order RFC 6762 section 8.2 compares in is the sorted one: here the text
// records decide, and this server's sorts lower.
func TestTheTieSortsBothSets(t *testing.T) {
	t.Parallel()

	name := "Desk." + linkService + ".local."
	srv := &dns.SRV{
		Hdr:    dns.RR_Header{Name: name, Rrtype: dns.TypeSRV, Class: dns.ClassINET},
		Port:   9000,
		Target: "a.local.",
	}
	text := func(value string) dns.RR {
		return &dns.TXT{Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeTXT, Class: dns.ClassINET}, Txt: []string{value}}
	}

	require.Equal(t, -1, compareSets([]dns.RR{srv, text("a")}, []dns.RR{text("b"), srv}))
}

// TestAPacketThatPanicsIsDroppedAndLogged is the second line of defence. A
// packet that makes the responder panic is logged and dropped, and the
// responder answers the next one.
func TestAPacketThatPanicsIsDroppedAndLogged(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		logged := &countingHandler{}
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)
		announcer.logger = slog.New(logged)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()

		over.packets <- packet{from: peer, to: groupIPv4, iface: "eth0", index: deskIndex}
		synctest.Wait()
		require.Equal(t, int64(1), logged.errors.Load())

		mark := over.mark()
		over.deliver(question("Desk."+linkService+".local.", dns.TypeSRV), peer, groupIPv4)
		require.Len(t, over.since(mark), 1, "the responder did not answer after a bad packet")
	})
}

// TestAServerOutOfNamesStopsAndSaysSo is a peer that claims every name the
// responder renames itself to. At the last rename the responder gives up: it
// sends nothing more, logs it once, and reports failed.
func TestAServerOutOfNamesStopsAndSaysSo(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		logged := &countingHandler{}
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)
		announcer.logger = slog.New(logged)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		synctest.Wait()

		for i := range maxRenames + 1 {
			over.deliver(claimOf(renamed("Desk", i, instanceSuffix)), peer, groupIPv4)
		}

		mark, infos := over.mark(), logged.infos.Load()

		for range 5 {
			over.deliver(question(linkService+".local.", dns.TypePTR), peer, groupIPv4)
		}

		time.Sleep(time.Minute)
		synctest.Wait()

		require.Empty(t, over.since(mark), "a server out of names kept probing")
		require.Equal(t, StateFailed, announcer.State())
		require.Equal(t, 1, logged.count())
		require.Equal(t, int64(0), logged.infos.Load()-infos, "a server out of names kept logging")
	})
}

// TestAConflictDropsTheHeldAnswers is a shared answer held back when a claim
// on the name arrives. The held answer carries the name as it was, so it
// never goes out once the responder has started over.
func TestAConflictDropsTheHeldAnswers(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()

		mark := over.mark()
		over.deliver(question(linkService+".local.", dns.TypePTR), peer, groupIPv4)
		over.deliver(claim(), peer, groupIPv4)
		waitOut()

		for _, one := range over.since(mark) {
			require.False(t, one.msg.Response, "a held answer went out after the conflict: %s", one.msg)
		}
	})
}

// TestTheAnswerRecordCostsTheSameWhateverItHolds counts what a note touches.
// With the record full, a note reads and writes a few entries, however many
// it holds. The stamps are walked only through a method that counts, so a
// walk over every entry would show in the count.
func TestTheAnswerRecordCostsTheSameWhateverItHolds(t *testing.T) {
	t.Parallel()

	record := newAnswered()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

	for i := range maxSent {
		record.note("asker "+strconv.Itoa(i), now)
	}

	const notes = 10 * maxSent

	before := record.cost()

	for i := range notes {
		record.note("later "+strconv.Itoa(i), now.Add(time.Duration(i)*time.Microsecond))
	}

	require.LessOrEqual(t, (record.cost()-before)/notes, 5, "a note touched more than a few entries")
}

// TestAServerWhoseHostNamesAreAllTakenStopsAndSaysSo is the host half of
// giving up. A peer that claims every host name the responder renames to
// makes it stop at the last one, and it reports failed.
func TestAServerWhoseHostNamesAreAllTakenStopsAndSaysSo(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		synctest.Wait()

		for i := range maxRenames + 1 {
			taken := new(dns.Msg)
			taken.Response = true
			taken.Answer = []dns.RR{&dns.A{
				Hdr: dns.RR_Header{
					Name: renamed("desk", i, hostSuffix) + ".local.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 120,
				},
				A: net.ParseIP("192.168.1.20").To4(),
			}}
			over.deliver(taken, peer, groupIPv4)
		}

		mark := over.mark()

		time.Sleep(time.Minute)
		synctest.Wait()

		require.Empty(t, over.since(mark), "a server out of host names kept probing")
		require.Equal(t, StateFailed, announcer.State())
		require.Equal(t, "desk-100", announcer.Host())
	})
}

// TestTheRealSocketIsNotJoinedTwice is the link on this machine's socket. A
// second reading of the interfaces joins nothing that joined already, which
// the system would refuse, so it logs no new failure.
//
// It does not run in parallel, because it opens the mDNS socket.
func TestTheRealSocketIsNotJoinedTwice(t *testing.T) {
	networktest.Require(t)

	logged := &countingHandler{}
	link := NewLink(slog.New(logged), nil)

	user, err := link.join()
	if err != nil {
		t.Skipf("this machine opens no mdns socket: %v", err)
	}

	defer user.close()

	if len(user.interfaces()) == 0 {
		t.Skip("this machine has no interface to join the group on")
	}

	first := logged.count()

	link.mu.Lock()
	link.refresh()
	link.mu.Unlock()

	require.Equal(t, first, logged.count(), "a second reading tried to join a group already joined")
}
