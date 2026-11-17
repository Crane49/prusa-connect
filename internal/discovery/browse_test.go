package discovery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/netip"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/ipv4"

	"github.com/prusa3d/prusa-connect/internal/discovery/networktest"
)

// browseBudget is how long a case waits for an answer. A responder probes the
// link for a name conflict before it announces, which RFC 6762 section 8.1
// gives three queries 250 ms apart, so a shorter budget would time out on a
// healthy machine.
const browseBudget = 15 * time.Second

// TestTheAnnouncedRecordIsBrowsedBack is the gate ADR-20260916-b3x rules 1 to 4 rest
// on. Nothing but a real question proves the record left this process in the
// shape a client reads: the instance name, the host, the port and the four TXT
// keys all come back over the wire here.
//
// Each service name carries a random suffix, so two runs on one network, or a
// run beside a real instance, never read each other's record.
func TestTheAnnouncedRecordIsBrowsedBack(t *testing.T) {
	networktest.Require(t)

	cases := map[string]string{
		"a name in one word": "PrusaConnect-" + token(t),
		"a name with spaces, which is what core.frontend.title holds": "Prusa Connect " + token(t),
		"a name with a dot, which stays in one label":                 "Lab 2.1 " + token(t),
		"a name outside ASCII":                                        "Tiskárna Průša " + token(t),
	}

	// Neither this case nor its halves run in parallel. Each opens a
	// responder on UDP 5353, and two in one process read one another's
	// packets rather than the link.
	for name, instance := range cases {
		t.Run(name, func(t *testing.T) {
			const port = 18080

			service := "_pcl-" + token(t) + "._tcp"

			announcer := New(Config{
				Enabled:  true,
				Services: []string{service},
				Instance: instance,
				Version:  "1.2.3",
				ID:       "8f14e45f-ceea-4e7a-9c2b-3d1f6a5b7c90",
				TLS:      true,
			}, quiet(), NewLink(quiet(), nil))

			ctx, stop := context.WithCancel(t.Context())
			defer stop()

			announcer.Start(ctx, port)
			t.Cleanup(func() { announcer.Stop(context.WithoutCancel(ctx)) })

			require.Equal(t, StateAnnouncing, announcer.State())

			answer := ask(ctx, t, service+"."+domain+".", dns.TypePTR)
			all := slices.Concat(answer.Answer, answer.Extra)

			pointer := findRecord[*dns.PTR](t, all)
			require.Equal(t, instance, instanceName(pointer.Ptr, service+"."+domain+"."))

			// ADR-20260916-b3x rule 3: the record names this machine's host under the
			// .local domain, and the API port.
			srv := findRecord[*dns.SRV](t, all)
			require.Equal(t, port, int(srv.Port))
			require.True(t, strings.EqualFold(hostName()+"."+domain+".", srv.Target),
				"the record points at %s, a host name this machine does not answer for", srv.Target)

			txt := findRecord[*dns.TXT](t, all)
			require.ElementsMatch(t, []string{
				"path=/", "version=1.2.3", "api=v1", "tls=1", "id=8f14e45f-ceea-4e7a-9c2b-3d1f6a5b7c90",
			}, txt.Txt)

			// A client asks next for the instance by the name it read off the
			// wire, escapes and all, and has to get an answer.
			resolved := ask(ctx, t, pointer.Ptr, dns.TypeSRV)
			require.Equal(t, port, int(findRecord[*dns.SRV](t, resolved.Answer).Port))
		})
	}
}

// ask sends one question the way a plain DNS client does, from a port of its
// own to the IPv4 group on every interface, and returns the first answer. The
// responder answers such a client by unicast (RFC 6762 section 6.7), so the
// answer comes back to this socket alone.
func ask(ctx context.Context, t *testing.T, name string, qtype uint16) *dns.Msg {
	t.Helper()

	return askAt(ctx, t, name, qtype, nil)
}

// askAt is ask sent to one address. A nil address is the group on every
// interface.
func askAt(ctx context.Context, t *testing.T, name string, qtype uint16, to *net.UDPAddr) *dns.Msg {
	t.Helper()
	networktest.Require(t)

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	require.NoError(t, err)

	defer conn.Close()

	sender := ipv4.NewPacketConn(conn)
	_ = sender.SetMulticastLoopback(true)

	question := new(dns.Msg)
	question.SetQuestion(name, qtype)
	question.RecursionDesired = false

	packed, err := question.Pack()
	require.NoError(t, err)

	buf := make([]byte, maxPacket)
	deadline := time.Now().Add(browseBudget)

	for time.Now().Before(deadline) && ctx.Err() == nil {
		for _, iface := range multicastInterfaces() {
			if to == nil && sender.SetMulticastInterface(&iface) == nil {
				_, _ = conn.WriteTo(packed, udpAddr(groupIPv4))
			}
		}

		if to != nil {
			_, _ = conn.WriteTo(packed, to)
		}

		_ = conn.SetReadDeadline(time.Now().Add(time.Second))

		for {
			n, _, failed := conn.ReadFrom(buf)
			if failed != nil {
				break
			}

			answer := new(dns.Msg)
			if answer.Unpack(buf[:n]) == nil && answer.Id == question.Id && len(answer.Answer) > 0 {
				return answer
			}
		}
	}

	t.Fatalf("nothing answered %s in %s. The responder started and no answer came back, so either the "+
		"mdns socket was refused or multicast does not reach back here", name, browseBudget)

	return nil
}

// findRecord is the first record of one type.
func findRecord[T dns.RR](t *testing.T, records []dns.RR) T {
	t.Helper()

	for _, rr := range records {
		if found, ok := rr.(T); ok {
			return found
		}
	}

	var none T

	t.Fatalf("the answer carries no %T: %v", none, records)

	return none
}

// token is a short random string, so one run's record cannot be read for
// another's.
func token(t *testing.T) string {
	t.Helper()

	var raw [3]byte

	_, err := rand.Read(raw[:])
	require.NoError(t, err)

	return hex.EncodeToString(raw[:])
}

// TestTheListenerHearsTheAnnouncementThenItsGoodbye is the same proof for the
// listener, over the real network. The announcer announces, the listener lists
// the record, and when the announcer is hidden the goodbye takes the record off
// the list before its time to live ends.
//
// It does not run in parallel with the browse above, for the reason given
// there.
func TestTheListenerHearsTheAnnouncementThenItsGoodbye(t *testing.T) {
	networktest.Require(t)

	const port = 18081

	service := "_pcl-" + token(t) + "._tcp"
	instance := "PrusaConnect-" + token(t)
	link := NewLink(quiet(), nil)

	announcer := New(Config{
		Enabled:  true,
		Services: []string{service},
		Instance: instance,
		Version:  "1.2.3",
	}, quiet(), link)

	// Port zero is no port of this server's, so the listener does not take the
	// announcer's record for its own.
	listener := NewListener(ListenerConfig{Enabled: true, Services: []string{service}}, quiet(), link)

	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	listener.Start(ctx, 0)
	t.Cleanup(func() { listener.Stop(context.WithoutCancel(ctx)) })

	announcer.Start(ctx, port)
	t.Cleanup(func() { announcer.Stop(context.WithoutCancel(ctx)) })

	require.Eventually(
		t,
		func() bool {
			return slices.ContainsFunc(listener.Nearby(), func(one Nearby) bool {
				return one.Name == instance && one.Port == port
			})
		},
		browseBudget,
		100*time.Millisecond,
		"the listener never heard %s. Either the mdns socket was refused or multicast does not reach back here",
		instance,
	)

	announcer.SetHidden(ctx, true)

	require.Eventually(t, func() bool {
		return !slices.ContainsFunc(listener.Nearby(), func(one Nearby) bool { return one.Name == instance })
	}, browseBudget, 100*time.Millisecond, "the goodbye did not take %s off the list", instance)
}

// TestAUnicastQueryReachesTheResponderBesideTheListener is a question sent to
// this machine's own address on port 5353, with the listener running too. A
// unicast packet reaches one socket on the port alone, so the two share one,
// and the responder answers.
//
// It does not run in parallel with the browse above, for the reason given
// there.
func TestAUnicastQueryReachesTheResponderBesideTheListener(t *testing.T) {
	networktest.Require(t)

	const port = 18084

	// The Windows DNS client service holds port 5353 as well, and Windows
	// hands a unicast packet on the port to it.
	if runtime.GOOS == "windows" {
		t.Skip("Windows hands a unicast packet on port 5353 to its own DNS client service")
	}

	service := "_pcl-" + token(t) + "._tcp"
	link := NewLink(quiet(), nil)

	announcer := New(Config{
		Enabled:  true,
		Services: []string{service},
		Instance: "PrusaConnect-" + token(t),
		Version:  "1.2.3",
	}, quiet(), link)
	listener := NewListener(ListenerConfig{Enabled: true, Services: []string{service}}, quiet(), link)

	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	announcer.Start(ctx, port)
	t.Cleanup(func() { announcer.Stop(context.WithoutCancel(ctx)) })

	listener.Start(ctx, 0)
	t.Cleanup(func() { listener.Stop(context.WithoutCancel(ctx)) })

	var own netip.Addr

	for _, one := range addressesOf(announcedInterfaces()) {
		if one.Is4() && !one.IsLinkLocalUnicast() {
			own = one

			break
		}
	}

	if !own.IsValid() {
		t.Skip("this machine has no IPv4 address to send a unicast question to")
	}

	answer := askAt(ctx, t, service+"."+domain+".", dns.TypePTR, udpAddr(own))
	require.NotEmpty(t, answer.Answer)
}

// TestTwoServersOnTheDefaultTitleEndWithTwoNames is two servers that start
// together under the title every server ships with. They see each other's
// claim on the name, and one of them renames itself, so a client lists both.
//
// It does not run in parallel with the browse above, for the reason given
// there.
func TestTwoServersOnTheDefaultTitleEndWithTwoNames(t *testing.T) {
	networktest.Require(t)

	service := "_pcl-" + token(t) + "._tcp"
	title := "Prusa Connect"

	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	for i, host := range []string{"pcl-a-" + token(t), "pcl-b-" + token(t)} {
		announcer := New(Config{Enabled: true, Services: []string{service}, Instance: title, Version: "1.2.3"},
			quiet(), NewLink(quiet(), nil))
		announcer.base = host

		announcer.Start(ctx, 18090+i)
		t.Cleanup(func() { announcer.Stop(context.WithoutCancel(ctx)) })
	}

	listener := NewListener(ListenerConfig{Enabled: true, Services: []string{service}}, quiet(), NewLink(quiet(), nil))
	listener.Start(ctx, 0)
	t.Cleanup(func() { listener.Stop(context.WithoutCancel(ctx)) })

	names := func() []string {
		var out []string
		for _, one := range listener.Nearby() {
			out = append(out, one.Name)
		}

		return out
	}

	require.Eventually(t, func() bool { return len(names()) == 2 }, browseBudget, 100*time.Millisecond,
		"the listener did not hear two servers: %v", names())
	require.ElementsMatch(t, []string{title, title + " (2)"}, names())
}

// TestTwentyHidesLeaveNoGoroutineBehind is the same leak test as the one over
// a fake network, over the real socket. Each show probes the network again,
// and a probe starts nothing.
//
// It does not run in parallel, because it counts the goroutines of the whole
// test binary.
func TestTwentyHidesLeaveNoGoroutineBehind(t *testing.T) {
	networktest.Require(t)

	const port = 18082

	before := runtime.NumGoroutine()

	announcer := New(Config{
		Enabled:  true,
		Services: []string{"_pcl-" + token(t) + "._tcp"},
		Instance: "PrusaConnect-" + token(t),
		Version:  "1.2.3",
	}, quiet(), NewLink(quiet(), nil))

	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	announcer.Start(ctx, port)

	// One cycle first, so the goroutines the announcer keeps for its life are
	// already counted.
	announcer.SetHidden(ctx, true)
	announcer.SetHidden(ctx, false)

	running := runtime.NumGoroutine()

	for range 20 {
		announcer.SetHidden(ctx, true)
		time.Sleep(50 * time.Millisecond)
		announcer.SetHidden(ctx, false)
		time.Sleep(50 * time.Millisecond)
	}

	require.Equal(t, StateAnnouncing, announcer.State())
	goroutinesSettle(t, running, "twenty hides left goroutines behind")

	announcer.Stop(ctx)
	goroutinesSettle(t, before, "the announcer left goroutines behind after Stop")
}

// goroutinesSettle waits for the test binary to hold no more goroutines than
// limit. A goroutine the real socket started a moment ago may still be on its
// way out when the count is first read, and a leak never gets back under the
// limit however long it is given.
//
// It polls on the test's own goroutine. require.Eventually runs its condition
// on a goroutine of its own, which the count it reads would include.
func goroutinesSettle(t *testing.T, limit int, why string) {
	t.Helper()

	deadline := time.Now().Add(goroutineSettle)
	for runtime.NumGoroutine() > limit && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	require.LessOrEqual(t, runtime.NumGoroutine(), limit,
		"%s: %d goroutines above %d", why, runtime.NumGoroutine()-limit, limit)
}

// goroutineSettle is how long a count over the real socket may take to come
// back down.
const goroutineSettle = 5 * time.Second

// TestAConflictingPeerLeavesNoGoroutineBehind is a peer on the real network
// that claims the name the responder probes for, again and again. It needs
// no credential for that. The responder renames itself, and once it stops
// nothing it started is left.
//
// It does not run in parallel, because it counts the goroutines of the whole
// test binary.
func TestAConflictingPeerLeavesNoGoroutineBehind(t *testing.T) {
	networktest.Require(t)

	const port = 18083

	service := "_pcl-" + token(t) + "._tcp"
	instance := "PrusaConnect-" + token(t)

	before := runtime.NumGoroutine()

	announcer := New(
		Config{Enabled: true, Services: []string{service}, Instance: instance, Version: "1.2.3"},
		quiet(),
		NewLink(quiet(), nil),
	)

	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	// The peer sends from port 5353, because a response from any other port
	// is not mDNS and is ignored.
	peerConn, err := net.ListenUDP("udp4", udpAddr(groupIPv4))
	require.NoError(t, err)

	defer peerConn.Close()

	sender := ipv4.NewPacketConn(peerConn)
	_ = sender.SetMulticastLoopback(true)

	claim := new(dns.Msg)
	claim.Response = true
	claim.Answer = []dns.RR{&dns.SRV{
		Hdr: dns.RR_Header{
			Name: instance + "." + service + ".local.", Rrtype: dns.TypeSRV, Class: dns.ClassINET, Ttl: 120,
		},
		Port:   80,
		Target: "somebody-else.local.",
	}}
	packed, err := claim.Pack()
	require.NoError(t, err)

	announcer.Start(ctx, port)

	for range 30 {
		for _, iface := range multicastInterfaces() {
			if sender.SetMulticastInterface(&iface) == nil {
				_, _ = peerConn.WriteTo(packed, udpAddr(groupIPv4))
			}
		}

		time.Sleep(100 * time.Millisecond)
	}

	answer := ask(ctx, t, service+"."+domain+".", dns.TypePTR)
	pointer := findRecord[*dns.PTR](t, answer.Answer)
	require.Equal(t, instance+" (2)", instanceName(pointer.Ptr, service+"."+domain+"."),
		"the responder did not rename itself away from the peer's claim")

	announcer.Stop(ctx)
	require.NoError(t, peerConn.Close())

	goroutinesSettle(t, before, "a conflicting peer left goroutines behind after Stop")
}
