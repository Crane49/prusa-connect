package xbuddy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// takeOne is an Arrivals that admits every printer and hands its session to the
// test.
type takeOne struct {
	baseline connectorapi.Set
	sessions chan *Session
}

func (h *takeOne) Admit(context.Context, connectorapi.Identity) (connectorapi.Set, error) {
	return h.baseline.Clone(), nil
}

func (h *takeOne) Arrived(_ context.Context, session connectorapi.Session) error {
	h.sessions <- session.(*Session) //nolint:forcetypeassert // this driver builds no other session type.

	return nil
}

// configured stands the whole driver up from a config block, the way a host
// building one from an operator's file does, and puts a printer on it through a
// link the test can sever. The returned pings are when that printer last
// answered one, which is what anchors a measurement of the read deadline.
func configured(t *testing.T, block string) (*Session, *severable, *pings) {
	t.Helper()

	built, err := New(connectorapi.Spec{Ref: "printer-1", Type: Type, Config: json.RawMessage(block)})
	if err != nil {
		t.Fatalf("building the driver from %s: %v", block, err)
	}

	driver, ok := built.(*Driver)
	if !ok {
		t.Fatalf("New returned a %T", built)
	}

	host := &takeOne{baseline: driver.Baseline(), sessions: make(chan *Session, 1)}

	handler, err := driver.Handler(host)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	link := relay(t, strings.TrimPrefix(server.URL, "http://"))
	base := "http://" + link.addr()

	const fingerprint = "FINGERPRINT-1"

	token := registered(t, driver, base, fingerprint, "MK4")

	header := http.Header{}
	header.Set(fingerprintHeader, fingerprint)
	header.Set(tokenHeader, token)
	header.Set(modelHeader, "MK4")
	header.Set(firmwareHeader, "6.4.0")

	dialer := websocket.Dialer{Subprotocols: []string{subprotocol}}

	client, resp, err := dialer.Dial("ws://"+link.addr()+websocketPath, header)
	if err != nil {
		t.Fatalf("dialling %s: %v", websocketPath, err)
	}

	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}

	t.Cleanup(func() { _ = client.Close() })

	seen := &pings{}
	seen.watch(client)

	// A printer answering pings, which is where one with nothing to say spends
	// its time.
	go func() {
		for {
			if _, _, readErr := client.ReadMessage(); readErr != nil {
				return
			}
		}
	}()

	select {
	case session := <-host.sessions:
		t.Cleanup(func() { _ = session.Close() })

		// Arriving is itself a presence change and leaves a wake-up waiting.
		<-session.PresenceChanged()

		return session, link, seen
	case <-time.After(5 * time.Second):
		t.Fatal("no printer was announced, so this test is not watching a session")

		return nil, nil, nil
	}
}

// registered performs the frozen handshake and returns the token. printerType
// is what the registration body carries, which is the model a printer that
// sends no header is filed under.
// registered walks one printer's whole registration and returns the token it
// walks away with: it posts the body, has the code approved as an operator
// would, and polls for the credential.
//
// The approval is here because every mode but none waits for one, and these
// cases are about what happens after a printer is let in. A driver in mode none
// approves on arrival and the call is then a second decision that changes
// nothing.
func registered(t *testing.T, driver *Driver, base, fingerprint, printerType string) string {
	t.Helper()

	body := strings.NewReader(`{"sn":"SN-1","fingerprint":"` + fingerprint +
		`","printer_type":"` + printerType + `","firmware":"6.4.0"}`)

	begin, err := http.Post(base+registerPath, "application/json", body)
	if err != nil {
		t.Fatalf("POST %s: %v", registerPath, err)
	}

	defer func() { _ = begin.Body.Close() }()

	code := begin.Header.Get(codeHeader)
	if code == "" {
		t.Fatalf("POST %s answered %d with no code", registerPath, begin.StatusCode)
	}

	if _, approved := driver.Approve(t.Context(), code, "user:tester"); approved != nil {
		t.Fatalf("approve the code %s: %v", code, approved)
	}

	poll, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+registerPath, nil)
	if err != nil {
		t.Fatalf("building the poll: %v", err)
	}

	poll.Header.Set(codeHeader, code)

	answer, err := http.DefaultClient.Do(poll)
	if err != nil {
		t.Fatalf("GET %s: %v", registerPath, err)
	}

	defer func() { _ = answer.Body.Close() }()

	token := answer.Header.Get(tokenHeader)
	if token == "" {
		t.Fatalf("GET %s answered %d with no token", registerPath, answer.StatusCode)
	}

	return token
}

// The three durations an operator writes reach the session that serves their
// printer.
//
// They are the whole of how fast a link that blackholes is noticed, and a
// deployment that cannot reach them takes the shipping 60s whatever it writes.
// This measures the shortened deadline end to end rather than reading a field:
// a value that stopped at the driver would be a key that changes nothing.
func TestTheConfiguredTimeoutsReachThePrinterSession(t *testing.T) {
	t.Parallel()

	const read = 150 * time.Millisecond

	started := time.Now()

	session, link, seen := configured(t,
		`{"read_deadline":"150ms","ping_interval":"25ms","ping_timeout":"100ms"}`)

	// Held on the thing itself and not on a number of intervals. A sleep long
	// enough on this machine is a sleep too short on a loaded runner, and what
	// this needs is a printer that has answered at least one ping: below that
	// there is no armed deadline to measure.
	awaited(t, "the printer answered no ping, so the session had exchanged nothing to measure",
		func() bool { _, ok := seen.after(started); return ok })

	if !session.Connected() {
		t.Fatal("the printer was unreachable before the link was severed, so this test measures nothing")
	}

	silent, floor := severAfterAPong(t, link, seen, read)

	select {
	case <-session.PresenceChanged():
	case <-time.After(readDeadline):
		t.Fatalf("a driver configured with a %v read deadline had not reported the printer "+
			"unreachable after %v, so the configured value did not reach the session and the "+
			"shipping %v is still what a deployment gets", read, readDeadline, readDeadline)
	}

	elapsed := time.Since(silent)

	if session.Connected() {
		t.Fatal("presence woke a reader and the session still says the printer is reachable")
	}

	t.Logf("silence to presence: %v, on a configured read deadline of %v", elapsed, read)

	// The deadline was armed by the last pong, so what is left of it at the sever
	// is the deadline less the age of the pong the printer was watched answering.
	// Anything under that floor is a socket that was closed, which is the case
	// that already worked.
	if elapsed < floor {
		t.Errorf("the printer was reported unreachable after %v, sooner than the %v a %v deadline had "+
			"left when the link went", elapsed, floor, read)
	}

	if elapsed > 2*read {
		t.Errorf("the printer was reported unreachable after %v, more than two %v deadlines",
			elapsed, read)
	}
}

// A config block that names no liveness key is the shipping connector, which is
// what a deployment that never heard of these keys has to keep getting.
func TestATimeoutsBlockThatNamesNothingIsTheShippingConnector(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		block Config
		want  Liveness
	}{
		{
			name:  "no block at all",
			block: Config{},
			want:  Liveness{ReadDeadline: readDeadline, PingInterval: pingInterval, PingTimeout: pingTimeout},
		},
		{
			name:  "a longer deadline and the rest absent",
			block: Config{ReadDeadline: "90s"},
			want:  Liveness{ReadDeadline: 90 * time.Second, PingInterval: pingInterval, PingTimeout: pingTimeout},
		},
		{
			name:  "a ping timeout and the rest absent",
			block: Config{PingTimeout: "2s"},
			want: Liveness{
				ReadDeadline: readDeadline, PingInterval: pingInterval, PingTimeout: 2 * time.Second,
			},
		},
		{
			name:  "all three named",
			block: Config{ReadDeadline: "10s", PingInterval: "4s", PingTimeout: "2s"},
			want: Liveness{
				ReadDeadline: 10 * time.Second, PingInterval: 4 * time.Second, PingTimeout: 2 * time.Second,
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseTimeouts(testCase.block)
			if err != nil {
				t.Fatalf("parseTimeouts(%+v): %v", testCase.block, err)
			}

			if got != testCase.want {
				t.Errorf("parseTimeouts(%+v) = %+v, want %+v", testCase.block, got, testCase.want)
			}
		})
	}
}

// A liveness setting the connector cannot run on is refused at startup, where an
// operator is reading the error, rather than at the first printer.
func TestAnUnusableTimeoutsBlockIsRefusedAtStartup(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		config string
	}{
		{"a read deadline nothing can parse", `{"read_deadline":"soon"}`},
		{"a ping interval nothing can parse", `{"ping_interval":"often"}`},
		{"a ping timeout nothing can parse", `{"ping_timeout":"a while"}`},
		{"a read deadline of zero", `{"read_deadline":"0s"}`},
		{"a negative read deadline", `{"read_deadline":"-10s"}`},
		{"a ping timeout of zero", `{"ping_timeout":"0s"}`},
		{
			name:   "an interval at the deadline, which drops every printer on a timer",
			config: `{"read_deadline":"10s","ping_interval":"10s"}`,
		},
		{
			// The likeliest thing an operator writes: shorten the deadline and
			// leave the shipping interval, which then sits above it.
			name:   "a shortened deadline with the shipping interval left under it",
			config: `{"read_deadline":"12s"}`,
		},
		{
			name:   "an interval above the deadline",
			config: `{"read_deadline":"10s","ping_interval":"20s"}`,
		},
		{
			name:   "an interval above what the firmware expects",
			config: `{"read_deadline":"5m","ping_interval":"60s"}`,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			built, err := New(connectorapi.Spec{
				Ref: "printer-1", Type: Type, Config: json.RawMessage(testCase.config),
			})
			if err == nil {
				t.Fatalf("%s was accepted, and %+v is what every printer would run on",
					testCase.config, built.(*Driver).live) //nolint:forcetypeassert // New builds one type.
			}

			named := false
			for _, key := range []string{"read_deadline", "ping_interval", "ping_timeout"} {
				named = named || strings.Contains(err.Error(), key)
			}

			if !named {
				t.Errorf("the refusal is %q and names no key an operator can correct", err)
			}
		})
	}
}
