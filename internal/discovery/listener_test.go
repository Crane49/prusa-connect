package discovery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/require"
)

// fakeLink is the network as a test sees it. A case writes messages into it and
// reads back the questions the listener asked.
type fakeLink struct {
	refuse error
	// raw hands messages on as they are, without the wire form, so a case
	// can send one no packet could carry.
	raw bool

	mu       sync.Mutex
	messages chan heard
	asked    []time.Duration
	begun    time.Time
}

func newFakeLink() *fakeLink {
	return &fakeLink{messages: make(chan heard)}
}

func (f *fakeLink) listen(ctx context.Context) (<-chan heard, error) {
	if f.refuse != nil {
		return nil, f.refuse
	}

	f.mu.Lock()
	f.begun = time.Now()
	f.mu.Unlock()

	out := make(chan heard)

	go func() {
		defer close(out)

		for {
			select {
			case <-ctx.Done():
				return
			case one := <-f.messages:
				if !f.raw {
					one.msg = roundTrip(one.msg)
				}

				select {
				case out <- one:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return out, nil
}

func (f *fakeLink) ask(_ context.Context, _ string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.asked = append(f.asked, time.Since(f.begun))
}

func (f *fakeLink) questions() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]time.Duration(nil), f.asked...)
}

func listenerOver(over link) *Listener {
	return newListener(ListenerConfig{Enabled: true, Services: []string{linkService}}, quiet(), over,
		newLinkFilter(workshopLink))
}

// TestTheListenerHearsARecordThenItsGoodbye is the gate for listing and
// dropping a printer: it hears one announcement, lists it, and drops it when
// the goodbye arrives.
func TestTheListenerHearsARecordThenItsGoodbye(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeLink()
		listener := listenerOver(over)

		listener.Start(t.Context(), 8080)
		defer listener.Stop(t.Context())

		over.messages <- heard{msg: announcement("Desk MK4", linkService, "desk", 80, "192.168.1.40", 4500), from: netip.AddrPortFrom(sender, mdnsPort), iface: "eth0"}
		synctest.Wait()

		listed := listener.Nearby()
		require.Len(t, listed, 1)
		require.Equal(t, "Desk MK4", listed[0].Name)

		over.messages <- heard{msg: goodbye("Desk MK4", linkService), from: netip.AddrPortFrom(sender, mdnsPort), iface: "eth0"}
		synctest.Wait()

		require.Empty(t, listener.Nearby())
	})
}

func TestTheListenerDropsARecordThatExpires(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeLink()
		listener := listenerOver(over)

		listener.Start(t.Context(), 8080)
		defer listener.Stop(t.Context())

		over.messages <- heard{msg: announcement("Brief", linkService, "brief", 80, "192.168.1.41", 30), from: netip.AddrPortFrom(sender, mdnsPort), iface: "eth0"}
		synctest.Wait()
		require.Len(t, listener.Nearby(), 1)

		time.Sleep(31 * time.Second)

		require.Empty(t, listener.Nearby())
	})
}

// TestTheListenerAsksOnTheSchedule is RFC 6762 section 5.2: a question at once,
// then after one second, two, four and so on.
func TestTheListenerAsksOnTheSchedule(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeLink()
		listener := listenerOver(over)

		listener.Start(t.Context(), 8080)
		defer listener.Stop(t.Context())

		time.Sleep(20 * time.Second)
		synctest.Wait()

		require.Equal(t, []time.Duration{
			0, time.Second, 3 * time.Second, 7 * time.Second, 15 * time.Second,
		}, over.questions())
	})
}

func TestTheListenerAsksBeforeARecordExpires(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeLink()
		listener := listenerOver(over)

		listener.Start(t.Context(), 8080)
		defer listener.Stop(t.Context())

		// A record with five seconds to live is due again after four, which is
		// before the schedule's own question at fifteen.
		time.Sleep(7*time.Second + time.Millisecond)
		over.messages <- heard{msg: announcement("Soon", linkService, "soon", 80, "192.168.1.42", 5), from: netip.AddrPortFrom(sender, mdnsPort), iface: "eth0"}
		synctest.Wait()

		time.Sleep(5 * time.Second)
		synctest.Wait()

		require.Contains(t, over.questions(), 11*time.Second+time.Millisecond)
	})
}

func TestAListenerSwitchedOffOpensNothing(t *testing.T) {
	t.Parallel()

	over := newFakeLink()
	listener := newListener(ListenerConfig{Enabled: false, Services: []string{linkService}}, quiet(), over,
		newLinkFilter(workshopLink))

	listener.Start(t.Context(), 8080)
	listener.Stop(t.Context())

	require.Empty(t, over.questions())
	require.Empty(t, listener.Nearby())
}

// TestASocketThisMachineRefusesLeavesTheServerServing keeps a missing mDNS
// socket a warning. The list is empty and nothing panics.
func TestASocketThisMachineRefusesLeavesTheServerServing(t *testing.T) {
	t.Parallel()

	over := newFakeLink()
	over.refuse = errors.New("address in use")

	listener := listenerOver(over)
	listener.Start(t.Context(), 8080)
	listener.Stop(t.Context())

	require.Empty(t, listener.Nearby())
}

func TestStoppingTheListenerEmptiesTheList(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeLink()
		listener := listenerOver(over)

		listener.Start(t.Context(), 8080)

		over.messages <- heard{msg: announcement("Left", linkService, "left", 80, "192.168.1.43", 4500), from: netip.AddrPortFrom(sender, mdnsPort), iface: "eth0"}
		synctest.Wait()
		require.Len(t, listener.Nearby(), 1)

		listener.Stop(t.Context())

		require.Empty(t, listener.Nearby())
	})
}

func TestANilListenerListsNothing(t *testing.T) {
	t.Parallel()

	var none *Listener

	none.Start(t.Context(), 8080)
	none.Stop(t.Context())
	require.Empty(t, none.Nearby())
}

// TestTheListenerDropsAMessageFromOffTheLink is RFC 6762 section 11 for the
// listener: a record routed in from another network is not listed.
func TestTheListenerDropsAMessageFromOffTheLink(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeLink()
		listener := listenerOver(over)

		listener.Start(t.Context(), 8080)
		defer listener.Stop(t.Context())

		over.messages <- heard{
			msg:   announcement("Far away", linkService, "far", 80, "203.0.113.7", 4500),
			from:  netip.MustParseAddrPort("203.0.113.7:5353"),
			iface: "eth0",
		}
		synctest.Wait()

		require.Empty(t, listener.Nearby())
	})
}

// TestTheListenerTakesRecordsOnlyFromResponsesFromPort5353 is RFC 6762
// section 6 for the listener. A query carries the asker's own records, a
// probe carries records nobody holds yet, and a response from any other port
// is not mDNS, so none of them lists a printer.
func TestTheListenerTakesRecordsOnlyFromResponsesFromPort5353(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeLink()
		listener := listenerOver(over)

		listener.Start(t.Context(), 8080)
		defer listener.Stop(t.Context())

		asking := announcement("Asker", linkService, "asker", 80, "192.168.1.42", 4500)
		asking.Response = false

		probing := announcement("Prober", linkService, "prober", 80, "192.168.1.43", 4500)
		probing.Response = false
		probing.Ns, probing.Answer = probing.Answer, nil

		over.messages <- heard{msg: asking, from: netip.AddrPortFrom(sender, mdnsPort), iface: "eth0"}
		over.messages <- heard{msg: probing, from: netip.AddrPortFrom(sender, mdnsPort), iface: "eth0"}
		over.messages <- heard{
			msg:   announcement("Other port", linkService, "other", 80, "192.168.1.44", 4500),
			from:  netip.AddrPortFrom(sender, 40000),
			iface: "eth0",
		}
		synctest.Wait()

		require.Empty(t, listener.Nearby())
	})
}

// TestAQuietListenerAsksNothing is the hidden server: it keeps its list and
// sends no question with its address in it.
func TestAQuietListenerAsksNothing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeLink()
		listener := listenerOver(over)
		listener.SetQuiet(true)

		listener.Start(t.Context(), 8080)
		defer listener.Stop(t.Context())

		over.messages <- heard{
			msg:  announcement("Desk", linkService, "desk", 80, "192.168.1.40", 4500),
			from: netip.AddrPortFrom(sender, mdnsPort), iface: "eth0",
		}
		time.Sleep(20 * time.Second)
		synctest.Wait()

		require.Empty(t, over.questions())
		require.Len(t, listener.Nearby(), 1, "a quiet listener still lists what it hears")

		listener.SetQuiet(false)
		time.Sleep(20 * time.Second)
		synctest.Wait()

		require.NotEmpty(t, over.questions())
	})
}

// TestAFullListWarnsAgainAfterItHadRoom keeps the warning about a full list
// for each time the list fills, and not only for the first.
func TestAFullListWarnsAgainAfterItHadRoom(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeLink()
		handler := &countingHandler{}
		listener := newListener(ListenerConfig{Enabled: true, Services: []string{linkService}},
			slog.New(handler), over, newLinkFilter(workshopLink))

		listener.Start(t.Context(), 8080)
		defer listener.Stop(t.Context())

		fill := func(round int) {
			for i := range MaxNearby + 1 {
				over.messages <- heard{
					msg: announcement(fmt.Sprintf("r%d-%03d", round, i), linkService,
						fmt.Sprintf("r%d-h%03d", round, i), 80, fmt.Sprintf("192.168.1.%d", i%250+1), 60),
					from:  netip.AddrPortFrom(sender, mdnsPort),
					iface: "eth0",
				}
			}

			synctest.Wait()
		}

		fill(1)
		require.Equal(t, 1, handler.count())

		fill(1)
		require.Equal(t, 1, handler.count(), "a list that stayed full warned twice")

		time.Sleep(2 * time.Minute)
		fill(2)
		require.Equal(t, 2, handler.count(), "a list that filled again said nothing")
	})
}

// TestAGoodbyeLaterInTheMessageDropsTheRecord is one response that points
// at an instance, gives its service record, and then says goodbye to that
// same service record. The record leaves the list, and nothing reads the
// record after it has gone.
func TestAGoodbyeLaterInTheMessageDropsTheRecord(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeLink()
		listener := listenerOver(over)

		listener.Start(t.Context(), 8080)
		defer listener.Stop(t.Context())

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

		over.messages <- heard{msg: msg, from: netip.AddrPortFrom(sender, mdnsPort), iface: "eth0"}
		synctest.Wait()

		require.Empty(t, listener.Nearby())
	})
}

// TestAPacketThatPanicsTheListenerIsDroppedAndLogged is the second line of
// defence in the listener. A packet it cannot read is logged and dropped,
// and the next one is listed.
func TestAPacketThatPanicsTheListenerIsDroppedAndLogged(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		logged := &countingHandler{}
		over := newFakeLink()
		listener := newListener(ListenerConfig{Enabled: true, Services: []string{linkService}}, slog.New(logged),
			over, newLinkFilter(workshopLink))

		listener.Start(t.Context(), 8080)
		defer listener.Stop(t.Context())

		over.messages <- heard{from: netip.AddrPortFrom(sender, mdnsPort), iface: "eth0"}
		over.messages <- heard{
			msg:  announcement("Desk MK4", linkService, "desk", 80, "192.168.1.40", 4500),
			from: netip.AddrPortFrom(sender, mdnsPort), iface: "eth0",
		}
		synctest.Wait()

		require.Equal(t, int64(1), logged.errors.Load())
		require.Len(t, listener.Nearby(), 1)
	})
}

// TestAPanicInsideTheListLeavesTheListenerWorking is a panic while the
// listener holds its lock. The lock is released as the panic unwinds, so the
// list stays readable and Stop returns. A pointer record that is nil is what
// no packet can carry, and it makes the table panic.
//
// It runs on the real clock, because a goroutine waiting on a lock left held
// would keep a synctest bubble from ever settling.
func TestAPanicInsideTheListLeavesTheListenerWorking(t *testing.T) {
	t.Parallel()

	logged := &countingHandler{}
	over := newFakeLink()
	over.raw = true
	listener := newListener(ListenerConfig{Enabled: true, Services: []string{linkService}}, slog.New(logged),
		over, newLinkFilter(workshopLink))

	listener.Start(t.Context(), 8080)

	broken := new(dns.Msg)
	broken.Response = true
	broken.Answer = []dns.RR{(*dns.PTR)(nil)}

	over.messages <- heard{msg: broken, from: netip.AddrPortFrom(sender, mdnsPort), iface: "eth0"}

	require.Eventually(t, func() bool { return logged.errors.Load() == 1 }, 5*time.Second, time.Millisecond,
		"the panic did not happen inside the table")

	// A lock left held would make Nearby wait for ever, so it is tried first
	// and the case fails at once.
	require.True(t, listener.mu.TryLock(), "the panic left the listener's lock held")
	listener.mu.Unlock()

	over.messages <- heard{
		msg:  announcement("Desk MK4", linkService, "desk", 80, "192.168.1.40", 4500),
		from: netip.AddrPortFrom(sender, mdnsPort), iface: "eth0",
	}

	require.Eventually(t, func() bool { return len(listener.Nearby()) == 1 }, 5*time.Second, time.Millisecond)

	listener.Stop(t.Context())
}
