package xbuddy

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// quick is the shipping liveness shape with the clock turned down: an interval
// well below the deadline, and a deadline long enough that a printer answering
// pings is never mistaken for one that has gone.
func quick() Liveness {
	return Liveness{
		ReadDeadline: 150 * time.Millisecond,
		PingInterval: 25 * time.Millisecond,
		PingTimeout:  100 * time.Millisecond,
	}
}

// severPoll is how long a relay pump waits for bytes before it looks again at
// whether the link has been severed. It bounds how late a sever takes effect,
// not how late a byte is forwarded: a read returns as soon as data arrives.
const severPoll = 2 * time.Millisecond

// severable is a TCP relay that can stop forwarding in both directions while
// leaving both sockets open, which is the difference this whole file turns on.
//
// A peer that hangs up sends a FIN and the read fails at once. A cable pulled
// out of a switch sends nothing: the packets are discarded, both ends still hold
// an ESTABLISHED socket, and a write into one succeeds because it lands in a
// buffer the kernel will retry for minutes. Only the read deadline separates
// that from a printer that is merely quiet, so a test that closes the socket is
// testing the case that already worked.
type severable struct {
	listener net.Listener
	upstream string
	cut      chan struct{}
	once     sync.Once

	mu sync.Mutex
	// held keeps every relayed socket referenced. net's finalizer closes a
	// connection nothing points at, and a severed link is exactly a connection
	// no goroutine is holding any more: without this the sockets are closed by
	// the garbage collector within milliseconds and the test measures a hang-up.
	held []net.Conn
	// pong is when the relay last handed the client's pong to the host. A sever
	// drops whatever is still inside the relay, so a pong that has reached the
	// host is the only pong a measurement may count on.
	pong time.Time
}

// clientPong is the first byte of a pong a client sends: final fragment, opcode
// 0xA. Reading one byte is what keeps a measurement from taking the relay's
// other traffic, the registration requests included, for a pong.
const clientPong = 0x8a

// relay starts a severable link in front of upstream.
func relay(t *testing.T, upstream string) *severable {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening for the severable link: %v", err)
	}

	link := &severable{listener: listener, upstream: upstream, cut: make(chan struct{})}

	t.Cleanup(func() {
		_ = listener.Close()
		link.release()
	})

	go link.serve()

	return link
}

// keep holds a reference to a relayed socket for the test's lifetime.
func (s *severable) keep(conns ...net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.held = append(s.held, conns...)
}

// release closes every socket the relay is holding open, at the end of a test.
func (s *severable) release() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, conn := range s.held {
		_ = conn.Close()
	}

	s.held = nil
}

// addr is where a client dials to reach upstream through this link.
func (s *severable) addr() string { return s.listener.Addr().String() }

// notePong records that the client's pong has reached the host.
func (s *severable) notePong() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pong = time.Now()
}

// pongAfter reports whether a pong reached the host at or after mark.
func (s *severable) pongAfter(mark time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return !s.pong.Before(mark)
}

// sever stops forwarding without closing either socket.
func (s *severable) sever() { s.once.Do(func() { close(s.cut) }) }

func (s *severable) serve() {
	for {
		near, err := s.listener.Accept()
		if err != nil {
			return
		}

		far, dialErr := net.Dial("tcp", s.upstream)
		if dialErr != nil {
			_ = near.Close()

			return
		}

		s.keep(near, far)

		go s.pump(far, near, true)
		go s.pump(near, far, false)
	}
}

// pump forwards one direction until the link is severed or one side fails.
// toHost says this is the direction a pong travels, which is the one a
// measurement of the read deadline has to know has arrived.
//
// A sever returns without closing anything, so both peers keep an open socket
// and learn nothing. Any other end closes both, which is what a peer hanging up
// has to look like.
func (s *severable) pump(dst, src net.Conn, toHost bool) {
	buffer := make([]byte, 32*1024)

	for {
		select {
		case <-s.cut:
			return
		default:
		}

		_ = src.SetReadDeadline(time.Now().Add(severPoll))

		read, err := src.Read(buffer)
		if read > 0 {
			if _, writeErr := dst.Write(buffer[:read]); writeErr != nil {
				s.hangUp(dst, src)

				return
			}

			if toHost && buffer[0] == clientPong {
				s.notePong()
			}
		}

		if err == nil {
			continue
		}

		var timeout net.Error
		if errors.As(err, &timeout) && timeout.Timeout() {
			continue
		}

		s.hangUp(dst, src)

		return
	}
}

// hangUp propagates a real close to both peers, so the relay is transparent to
// everything except a sever.
func (s *severable) hangUp(dst, src net.Conn) {
	select {
	case <-s.cut:
		return
	default:
	}

	_ = dst.Close()
	_ = src.Close()
}

// pings records when the printer last saw a ping, taken on the printer's own
// connection just before it answers one.
//
// The pong that follows is what arms the session's read deadline, so this time
// is never later than the arming. A test that assumed the ping ticker was on
// time instead had no such bound: a tick delayed by load left the last pong
// older than the test believed and put the detection under a floor derived from
// the interval.
type pings struct {
	mu   sync.Mutex
	last time.Time
}

// watch wraps a connection's ping handler so every ping is recorded before it is
// answered. It has to run before anything reads that connection, because gorilla
// calls the handler from inside ReadMessage.
func (p *pings) watch(conn *websocket.Conn) {
	answer := conn.PingHandler()

	conn.SetPingHandler(func(payload string) error {
		p.mu.Lock()
		p.last = time.Now()
		p.mu.Unlock()

		return answer(payload)
	})
}

// after reports the recorded ping when one has been seen at or after mark.
func (p *pings) after(mark time.Time) (time.Time, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.last, !p.last.Before(mark)
}

// awaited polls until condition holds, and ends the test with reason if it never
// does.
func awaited(t *testing.T, reason string, condition func() bool) {
	t.Helper()

	limit := time.Now().Add(2 * time.Second)

	for !condition() {
		if time.Now().After(limit) {
			t.Fatal(reason)
		}

		time.Sleep(severPoll)
	}
}

// severAfterAPong severs the link once a pong has reached the host, and reports
// the moment of the sever with what is left of the read deadline at it.
//
// The client sends nothing but pongs, so the pong is the last thing that armed
// the deadline. The floor under a detection time is the deadline less the age
// of that pong, and the returned floor holds whatever the ping ticker did.
// Severing while a pong is still inside the relay would leave the deadline
// armed by the pong before it and the floor too high by an interval, which is
// why this waits for the crossing.
func severAfterAPong(t *testing.T, link *severable, seen *pings, deadline time.Duration) (time.Time, time.Duration) {
	t.Helper()

	mark := time.Now()

	awaited(t, "the printer answered no ping, so nothing observed says when the read deadline was armed",
		func() bool { _, ok := seen.after(mark); return ok })

	ping, _ := seen.after(mark)

	awaited(t, "the printer answered a ping and no pong reached the host, so the link carries the "+
		"printer's traffic in one direction only", func() bool { return link.pongAfter(ping) })

	silent := time.Now()
	floor := deadline - silent.Sub(ping)

	link.sever()

	return silent, floor
}

// harness is one session behind a real websocket, reached through a link the
// test can sever.
type harness struct {
	session *Session
	client  *websocket.Conn
	link    *severable
	// pings is when the printer last answered a ping, which is what anchors a
	// measurement of the read deadline.
	pings *pings
	// closed fires when the session itself ends, which is the offline grace
	// running out rather than the socket going.
	closed <-chan struct{}
}

// attached stands one session up behind a real websocket and hands back the harness.
//
// A real socket is what this needs. The case is a printer that stops answering
// without hanging up, and a printer that hangs up is the case that already
// worked: internal/printer/simulator closes cleanly, so nothing it can do
// reaches this path.
func attached(t *testing.T, live Liveness, grace time.Duration) *harness {
	t.Helper()

	ended := make(chan struct{})

	session := newSession(settings{
		identity:   connectorapi.Identity{Vendor: Vendor, Model: "MK4", Serial: "SN-1"},
		caps:       baseline(),
		storage:    defaultStorage,
		queueDepth: 1,
		grace:      grace,
		live:       live,
		onClose:    func() { close(ended) },
	})

	t.Cleanup(func() { _ = session.Close() })

	var upgrader websocket.Upgrader

	up := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		if !session.attach(r.Context(), conn) {
			_ = conn.Close()

			return
		}

		close(up)
	}))
	t.Cleanup(server.Close)

	link := relay(t, strings.TrimPrefix(server.URL, "http://"))

	client, _, err := websocket.DefaultDialer.Dial("ws://"+link.addr(), nil)
	if err != nil {
		t.Fatalf("dialling the driver: %v", err)
	}

	t.Cleanup(func() { _ = client.Close() })

	seen := &pings{}
	seen.watch(client)

	select {
	case <-up:
	case <-time.After(2 * time.Second):
		t.Fatal("the connection never attached, so this test is not watching a session")
	}

	// Attaching is itself a presence change and leaves a wake-up waiting. A test
	// that measured from a severed link without taking it would read the arrival
	// as the departure and report a detection time of zero.
	select {
	case <-session.PresenceChanged():
	case <-time.After(2 * time.Second):
		t.Fatal("attaching a connection woke nobody, so presence is not being reported at all")
	}

	return &harness{session: session, client: client, link: link, pings: seen, closed: ended}
}

// A printer that vanishes without hanging up is noticed.
//
// Losing power or falling out of a NAT table produces silence and nothing else:
// the socket stays writable for as long as the operating system keeps retrying
// TCP, so the session holds a link that is gone, the offline grace that ends an
// absence never starts, and every command written to it is lost instead of
// queued for the printer's return.
func TestAPrinterThatStopsAnsweringLosesItsConnection(t *testing.T) {
	t.Parallel()

	// The client is never read from, so gorilla never answers a ping. That is
	// what a printer that is no longer there looks like on the wire.
	stand := attached(t, quick(), 50*time.Millisecond)

	select {
	case <-stand.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("a printer that stopped answering kept a live session: nothing detached the connection, " +
			"so the offline grace never started and a command would be written into a dead socket")
	}
}

// The other direction. A printer that answers must not be dropped for being
// quiet: this dialect's telemetry stops when a print does, and a quiet printer
// re-arms the deadline only with its pong, so a session that pinged nothing
// would end every idle printer's connection on a timer.
func TestAPrinterThatAnswersPingsKeepsItsConnection(t *testing.T) {
	t.Parallel()

	live := quick()
	stand := attached(t, live, 50*time.Millisecond)

	// gorilla answers a ping from inside ReadMessage, which is where a printer
	// that is listening and has nothing to say spends its time.
	go func() {
		for {
			if _, _, err := stand.client.ReadMessage(); err != nil {
				return
			}
		}
	}()

	select {
	case <-stand.closed:
		t.Fatal("a printer answering every ping had its connection ended anyway")
	case <-time.After(4 * live.ReadDeadline):
	}
}

// A printer that is sending must not be dropped for a pong that comes late.
//
// Buddy firmware reads its socket only when its planner lets it, so its pong
// can arrive after ReadDeadline less PingInterval, which is 6s on the shipping
// numbers. A deadline that only a pong re-arms drops a CORE One every minute or
// two while its telemetry arrives every few seconds. The liveness keeps the
// shipping ratio of 54 to 60, and the printer pings the host as the firmware
// does after sending nothing for a while.
func TestAPrinterThatSendsKeepsItsConnectionWhateverItsPong(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		frame func(client *websocket.Conn) error
	}{
		{
			name: "telemetry",
			frame: func(client *websocket.Conn) error {
				return client.WriteMessage(websocket.TextMessage, []byte(`{"state":"IDLE","temp_bed":25}`))
			},
		},
		{
			name: "pings from the printer",
			frame: func(client *websocket.Conn) error {
				return client.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second))
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			live := Liveness{
				ReadDeadline: 300 * time.Millisecond,
				PingInterval: 270 * time.Millisecond,
				PingTimeout:  100 * time.Millisecond,
			}
			stand := attached(t, live, 0)

			// The printer never answers a ping, which is the latest a pong can be.
			// The client is read so that the host's pong to its own ping is taken
			// off the wire, and only one goroutine writes to it.
			stand.client.SetPingHandler(func(string) error { return nil })

			go func() {
				for {
					if _, _, err := stand.client.ReadMessage(); err != nil {
						return
					}
				}
			}()

			stop := make(chan struct{})
			t.Cleanup(func() { close(stop) })

			go func() {
				for {
					select {
					case <-stop:
						return
					case <-time.After(20 * time.Millisecond):
					}

					if err := testCase.frame(stand.client); err != nil {
						return
					}
				}
			}()

			select {
			case <-stand.session.PresenceChanged():
				t.Fatalf("a printer sending %s every 20ms was dropped on a %v read deadline because it did "+
					"not answer a ping", testCase.name, live.ReadDeadline)
			case <-time.After(6 * live.ReadDeadline):
			}
		})
	}
}

// A printer still sending one long answer keeps its connection.
//
// The firmware sends a FILE_INFO as one message in many fragments and renders
// each as it reads the stick, so a folder of 300 files takes longer than the
// read deadline, and nothing else can reach the host in the middle of a message.
// A deadline re-armed only by a whole message cut the connection while the
// listing was still arriving, and the next request for it started again.
func TestAPrinterSendingOneLongAnswerKeepsItsConnection(t *testing.T) {
	t.Parallel()

	live := Liveness{
		ReadDeadline: 300 * time.Millisecond,
		PingInterval: 270 * time.Millisecond,
		PingTimeout:  100 * time.Millisecond,
	}
	stand := attached(t, live, 0)

	// The printer never answers a ping while it sends, as the firmware does not.
	stand.client.SetPingHandler(func(string) error { return nil })

	go func() {
		for {
			if _, _, err := stand.client.ReadMessage(); err != nil {
				return
			}
		}
	}()

	writer, err := stand.client.NextWriter(websocket.TextMessage)
	if err != nil {
		t.Fatalf("starting the answer: %v", err)
	}

	if _, err = io.WriteString(writer, `{"event":"FILE_INFO","data":{"path":"/usb","name":"usb","pad":"`); err != nil {
		t.Fatalf("writing the answer: %v", err)
	}

	// gorilla sends a fragment whenever its 4 KiB buffer fills, so a kilobyte
	// every 30ms is a fragment about every 120ms, and the message lasts four
	// read deadlines.
	chunk := strings.Repeat("x", 1024)
	started := time.Now()

	for time.Since(started) < 4*live.ReadDeadline {
		if _, err = io.WriteString(writer, chunk); err != nil {
			t.Fatalf("the connection ended while the printer was still sending its answer: %v", err)
		}

		time.Sleep(30 * time.Millisecond)
	}

	if _, err = io.WriteString(writer, `"}}`); err != nil {
		t.Fatalf("finishing the answer: %v", err)
	}

	if err = writer.Close(); err != nil {
		t.Fatalf("sending the last fragment: %v", err)
	}

	awaited(t, "the long answer never reached the session", func() bool {
		_, ok := stand.session.recalled("usb", 0)

		return ok
	})

	select {
	case <-stand.session.PresenceChanged():
		t.Fatalf("a printer sending one message for %v was dropped on a %v read deadline, although a "+
			"fragment of it arrived every 120ms", time.Since(started), live.ReadDeadline)
	default:
	}
}

// A link that blackholes rather than resets is still noticed, and the read
// deadline is the only thing that notices it.
//
// This is the case a bench sees when the ethernet cable comes out of a switch:
// no FIN, no RST, packets discarded, both ends still holding an ESTABLISHED
// socket. The ping the host writes lands in a buffer and succeeds, so nothing on
// the write path fails and nothing on the read path returns. The measurement
// this makes is the number that matters, because the shipping deadline is 60s.
func TestABlackholedLinkStillReportsThePrinterUnreachable(t *testing.T) {
	t.Parallel()

	live := quick()
	stand := attached(t, live, 0)

	// The relay is transparent until it is severed, so the printer is answering
	// pings right up to the moment the link goes.
	go func() {
		for {
			if _, _, err := stand.client.ReadMessage(); err != nil {
				return
			}
		}
	}()

	time.Sleep(4 * live.PingInterval)

	if !stand.session.Connected() {
		t.Fatal("the printer was already unreachable before the link was severed, so this test " +
			"measures nothing")
	}

	silent, floor := severAfterAPong(t, stand.link, stand.pings, live.ReadDeadline)

	select {
	case <-stand.session.PresenceChanged():
	case <-time.After(20 * live.ReadDeadline):
		t.Fatalf("a blackholed link never reported the printer unreachable: %v of silence and the "+
			"session still says it has a connection", 20*live.ReadDeadline)
	}

	elapsed := time.Since(silent)

	if stand.session.Connected() {
		t.Fatal("presence woke a reader and the session still says the printer is reachable")
	}

	t.Logf("silence to presence: %v, on a read deadline of %v and a ping interval of %v",
		elapsed, live.ReadDeadline, live.PingInterval)

	// The deadline is armed by the last pong, the only frame this client sends,
	// so silence lasts for whatever is left of it at the sever. The floor is that
	// remainder, measured from the pong the printer was watched answering, and
	// anything under it is a socket that was closed rather than blackholed,
	// which is the case that already worked.
	if elapsed < floor {
		t.Errorf("the printer was reported unreachable after %v, sooner than the %v that a %v deadline "+
			"had left when the link went: something other than the deadline ended the connection, so "+
			"this measures the wrong thing", elapsed, floor, live.ReadDeadline)
	}

	if elapsed > 2*live.ReadDeadline {
		t.Errorf("the printer was reported unreachable after %v, which is more than two %v read "+
			"deadlines", elapsed, live.ReadDeadline)
	}
}

// The link really does blackhole, which is what makes the measurement above a
// measurement of the case that does not already work.
//
// A peer that hangs up is detected by the failing read, on any deadline at all.
// This asserts the printer's own socket learns nothing: a write still succeeds
// and a read times out rather than reporting a close.
func TestASeveredLinkNeverTellsThePrinterAnything(t *testing.T) {
	t.Parallel()

	stand := attached(t, quick(), 0)

	stand.link.sever()

	if err := stand.client.WriteMessage(websocket.TextMessage, []byte(`{"event":"INFO"}`)); err != nil {
		t.Fatalf("a severed link failed the printer's write, so the socket was closed and this is a "+
			"hang-up and not a blackhole: %v", err)
	}

	_ = stand.client.SetReadDeadline(time.Now().Add(200 * time.Millisecond))

	_, _, err := stand.client.ReadMessage()

	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatalf("a severed link gave the printer %v, want a read timeout: anything else means the "+
			"peer was told the connection ended", err)
	}

	if errors.Is(err, io.EOF) || websocket.IsCloseError(err) {
		t.Fatalf("a severed link closed the printer's socket: %v", err)
	}
}

// Both failure modes reach the same detach, so a shortened deadline surfaces as
// presence exactly as a clean close does.
//
// The two arrive on different paths: a close fails the read at once, and a
// blackhole fails it when the deadline expires. Both have to leave the session
// open, drop the connection, and wake whoever is watching reachability, because
// the presence frame is published off that wake-up and nothing else.
func TestEveryWayAConnectionEndsReachesPresence(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		end  func(stand *harness)
	}{
		{
			name: "the printer hangs up",
			end:  func(stand *harness) { _ = stand.client.Close() },
		},
		{
			name: "the link blackholes",
			end:  func(stand *harness) { stand.link.sever() },
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			live := quick()
			stand := attached(t, live, 0)

			testCase.end(stand)

			select {
			case <-stand.session.PresenceChanged():
			case <-time.After(20 * live.ReadDeadline):
				t.Fatal("the connection ended and nothing woke a reader of presence")
			}

			if stand.session.Connected() {
				t.Error("presence woke a reader and the session still says the printer is reachable")
			}

			// The session outliving the connection is what keeps a command queued
			// for the printer's return, and a grace of zero never ends it.
			select {
			case <-stand.closed:
				t.Error("a lost connection closed the session, so a command queued for the printer " +
					"would be failed rather than held for its return")
			default:
			}
		})
	}
}

// The numbers a session runs on are the shipping connector's, and a caller that
// names none gets them. A zero interval is also what would panic a ticker.
func TestTheDefaultLivenessIsTheShippingConnectors(t *testing.T) {
	t.Parallel()

	got := Liveness{}.orShipping()

	want := Liveness{ReadDeadline: readDeadline, PingInterval: pingInterval, PingTimeout: pingTimeout}
	if got != want {
		t.Errorf("liveness = %+v, want the shipping numbers %+v", got, want)
	}

	if got.PingInterval >= got.ReadDeadline {
		t.Errorf("the ping interval %v is not below the read deadline %v, so the deadline expires "+
			"before a pong can re-arm it", got.PingInterval, got.ReadDeadline)
	}
}
