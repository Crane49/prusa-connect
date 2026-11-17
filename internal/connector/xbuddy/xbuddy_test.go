package xbuddy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/prusa3d/prusa-connect/internal/connector/xbuddy"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi/connectorapitest"
)

// The published conformance suite, which every driver in this repository and
// every community driver passes or neither ships (ADR-20260825-n55 rule 4).
func TestContract(t *testing.T) {
	connectorapitest.RunDriverContract(t, func(_ *testing.T) connectorapitest.Subject {
		return connectorapitest.Subject{
			Driver:   xbuddy.NewDriver(xbuddy.ModeNone, 4, 0, "", nil),
			Dial:     dialDevice,
			SafeVerb: xbuddy.VerbSendInfo,
		}
	})
}

func TestTheDriverRegistersItsType(t *testing.T) {
	// Draining seals the package gathering list for this test binary, which is
	// what the host does once at startup.
	registry, err := connectorapi.NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	found, err := registry.Lookup(xbuddy.Type)
	if err != nil {
		t.Fatalf("the driver's init did not put %q in the registry: %v", xbuddy.Type, err)
	}

	if !found.Namespace().IsCore() {
		t.Errorf("a driver in this repository is namespaced %q, and the host derives core for its own module",
			found.Namespace())
	}

	driver, err := registry.Build(connectorapi.Spec{
		Ref:    "printer-1",
		Type:   xbuddy.Type,
		Config: json.RawMessage(`{"mode":"token","auto_register":false,"queue_depth":4,"offline_grace":"15m"}`),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	inbound, outbound, reachable := connectorapi.Directions(driver)
	if reachable != nil {
		t.Fatalf("the registered driver is reachable in no direction: %v", reachable)
	}

	if inbound == nil {
		t.Error("the xBuddy dialect is the inbound half of an earlier design decision and does not implement Inbound")
	}

	if outbound != nil {
		t.Error("nothing dials an xBuddy printer, and the driver claims an outbound half")
	}
}

func TestAnUnknownAdmissionModeIsRefusedAtStartup(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		config string
	}{
		{"an admission mode nothing defines", `{"mode":"password"}`},
		// Approving on arrival is the meaning of mode none and of no other
		// mode, so writing it against the mode is a file that asks for
		// something this connector will not do. Mode token admitted a Core
		// One with nobody deciding that way (BUG-011).
		{"admitting every printer in a mode that takes a credential",
			`{"mode":"token","auto_register":true}`},
		{"admitting every printer in mode fingerprint", `{"mode":"fingerprint","auto_register":true}`},
		{"admitting every printer in mode both", `{"mode":"both","auto_register":true}`},
		{"waiting for an operator in the mode that takes any printer",
			`{"mode":"none","auto_register":false}`},
		{"a queue depth nothing can hold", `{"queue_depth":-1}`},
		{"an unreadable grace", `{"offline_grace":"soon"}`},
		{"a negative grace", `{"offline_grace":"-1m"}`},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if _, err := xbuddy.New(connectorapi.Spec{
				Ref: "printer-1", Type: xbuddy.Type, Config: json.RawMessage(testCase.config),
			}); err == nil {
				t.Errorf("%s was accepted", testCase.config)
			}
		})
	}
}

// The two settings of auto_register that agree with their mode are accepted, so
// a file in the field that says what its mode already does still starts.
func TestAutoRegisterIsAcceptedWhereItAgreesWithTheMode(t *testing.T) {
	t.Parallel()

	for _, config := range []string{
		`{"mode":"none","auto_register":true}`,
		`{"mode":"token","auto_register":false}`,
		`{"mode":"none"}`,
		`{"mode":"token"}`,
	} {
		t.Run(config, func(t *testing.T) {
			t.Parallel()

			if _, err := xbuddy.New(connectorapi.Spec{
				Ref: "printer-1", Type: xbuddy.Type, Config: json.RawMessage(config),
			}); err != nil {
				t.Errorf("%s was refused: %v", config, err)
			}
		})
	}
}

// The three headers the handshake is frozen on. A printer that presents none of
// them in a mode that wants them is refused before anything is upgraded.
func TestAPrinterWithoutItsCredentialsIsRefused(t *testing.T) {
	t.Parallel()

	driver := xbuddy.NewDriver(xbuddy.ModeToken, 4, 0, "", nil)

	handler, err := driver.Handler(connectorapitest.NewHost(driver.Baseline()))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/p/ws", nil)
	if err != nil {
		t.Fatalf("build the request: %v", err)
	}

	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusUnauthorized {
		t.Errorf("a printer with no token was answered %d, want %d", response.StatusCode, http.StatusUnauthorized)
	}
}

// A websocket handshake with none of the three headers is refused in mode none
// too, before the host is asked anything. Mode none checks no credential, and
// the Fingerprint header is not a credential: it is the only identity the
// connection has, and a caller that sends none is a browser or a port scanner
// and not a printer. The request is the one a bench reproduced with curl.
func TestABareWebsocketHandshakeIsNotAPrinterInModeNone(t *testing.T) {
	t.Parallel()

	driver := xbuddy.NewDriver(xbuddy.ModeNone, 4, 0, "", nil)
	host := connectorapitest.NewHost(driver.Baseline())

	handler, err := driver.Handler(host)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/p/ws", nil)
	if err != nil {
		t.Fatalf("build the request: %v", err)
	}

	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Sec-WebSocket-Version", "13")
	request.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")

	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusUnauthorized {
		t.Errorf("a handshake with no fingerprint was answered %d, want %d",
			response.StatusCode, http.StatusUnauthorized)
	}

	if admitted := host.Admissions(); len(admitted) != 0 {
		t.Errorf("the host was asked to admit %d printer(s) for a handshake that named none: %v",
			len(admitted), admitted)
	}
}

// device is one printer for the conformance suite: enough of the frozen wire to
// register, connect and answer, and nothing else.
//
// The simulator is what proves this driver speaks the wire, in simulator_test.go.
// This exists because the suite owns its own server and mounts the driver under
// a prefix, which is a shape the simulator's hard-coded absolute paths cannot
// dial.
type device struct {
	conn *websocket.Conn
	once sync.Once
}

const deviceModel = "MK4"

func dialDevice(t *testing.T, baseURL string) error {
	t.Helper()

	client := &http.Client{Timeout: 10 * time.Second}

	token, err := registerDevice(t, client, baseURL)
	if err != nil {
		return err
	}

	header := http.Header{}
	header.Set("Fingerprint", "CONFORMANCE-1")
	header.Set("Token", token)
	header.Set("User-Agent-Printer", deviceModel)
	header.Set("User-Agent-Version", "6.4.0")

	dialer := websocket.Dialer{
		Subprotocols:     []string{"prusa-connect"},
		HandshakeTimeout: 10 * time.Second,
	}

	conn, response, err := dialer.Dial(websocketURL(baseURL), header)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}

	if err != nil {
		return fmt.Errorf("dial the websocket: %w", err)
	}

	dev := &device{conn: conn}
	t.Cleanup(dev.close)

	go dev.serve()

	return nil
}

// registerDevice runs the frozen registration handshake and returns the token.
func registerDevice(t *testing.T, client *http.Client, baseURL string) (string, error) {
	t.Helper()

	body := strings.NewReader(
		`{"sn":"CONFORMANCE-SN","fingerprint":"CONFORMANCE-1","printer_type":"1.4.0","firmware":"6.4.0"}`)

	begin, err := http.NewRequestWithContext(t.Context(), http.MethodPost, baseURL+"/p/register", body)
	if err != nil {
		return "", fmt.Errorf("build the registration: %w", err)
	}

	begun, err := client.Do(begin)
	if err != nil {
		return "", fmt.Errorf("post the registration: %w", err)
	}

	code := begun.Header.Get("Code")

	_ = begun.Body.Close()

	if begun.StatusCode != http.StatusOK || code == "" {
		return "", fmt.Errorf("registration answered %d with code %q", begun.StatusCode, code)
	}

	poll, err := http.NewRequestWithContext(t.Context(), http.MethodGet, baseURL+"/p/register", nil)
	if err != nil {
		return "", fmt.Errorf("build the poll: %w", err)
	}

	poll.Header.Set("Code", code)

	polled, err := client.Do(poll)
	if err != nil {
		return "", fmt.Errorf("poll the registration: %w", err)
	}

	token := polled.Header.Get("Token")

	_ = polled.Body.Close()

	if polled.StatusCode != http.StatusOK || token == "" {
		return "", fmt.Errorf("the poll answered %d with token %q", polled.StatusCode, token)
	}

	return token, nil
}

// serve answers every frame with one terminal event under the id it arrived
// with. The header is decoded here rather than through the driver's own codec:
// a shared codec makes an encoder bug agree with itself on both sides.
func (d *device) serve() {
	for {
		_, data, err := d.conn.ReadMessage()
		if err != nil {
			return
		}

		if len(data) < 9 {
			continue
		}

		id, parseErr := strconv.ParseUint(string(data[1:9]), 16, 32)
		if parseErr != nil {
			continue
		}

		answer := fmt.Sprintf(`{"event":"FINISHED","command_id":%d,"state":"IDLE"}`, id)
		if writeErr := d.conn.WriteMessage(websocket.TextMessage, []byte(answer)); writeErr != nil {
			return
		}
	}
}

func (d *device) close() {
	d.once.Do(func() { _ = d.conn.Close() })
}

// websocketURL turns the base the suite mounted the driver under into the
// websocket address, keeping the prefix: a driver that only answers at the root
// is one that hard-coded where it was mounted.
func websocketURL(baseURL string) string {
	switch {
	case strings.HasPrefix(baseURL, "https://"):
		return "wss://" + strings.TrimPrefix(baseURL, "https://") + "/p/ws"
	default:
		return "ws://" + strings.TrimPrefix(baseURL, "http://") + "/p/ws"
	}
}

// A printer that dials again while its first socket is still open is one
// printer, not two: the driver puts the new connection under the session that
// is already there and hangs up the old one.
func TestASecondDialReplacesTheSocketAndKeepsOneSession(t *testing.T) {
	t.Parallel()

	driver := xbuddy.NewDriver(xbuddy.ModeNone, 4, 0, "", nil)

	host := connectorapitest.NewHost(driver.Baseline())

	handler, err := driver.Handler(host)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	first := dialSocket(t, server.URL, "PRINTER-A")

	session, err := host.AwaitSession(t.Context())
	if err != nil {
		t.Fatalf("AwaitSession: %v", err)
	}

	// A session is four goroutines, and closing the server and both sockets
	// ends none of them: the session outlives a connection on purpose, which is
	// the very thing this case is about, so only the host releases it.
	t.Cleanup(func() { _ = session.Close() })

	second := dialSocket(t, server.URL, "PRINTER-A")
	t.Cleanup(func() { _ = second.Close() })

	// Read to the end rather than once. The session speaks first on every
	// connection, so the first socket may hold a SEND_INFO frame that was
	// written before the second dial replaced it, and a socket with a frame
	// still buffered is not a socket that is open.
	if err = readToEnd(first); err == nil {
		t.Error("the printer's first socket is still open after it dialled again")
	}

	if got := len(host.Sessions()); got != 1 {
		t.Errorf("the host holds %d sessions for one printer, want 1", got)
	}
}

// readToEnd drains a socket and returns the error that ended it, so a
// connection carrying one last frame is told apart from one still serving.
func readToEnd(conn *websocket.Conn) error {
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return err
		}
	}
}

// dialSocket opens one websocket as a printer with the given fingerprint.
func dialSocket(t *testing.T, baseURL, fingerprint string) *websocket.Conn {
	t.Helper()

	header := http.Header{}
	header.Set("Fingerprint", fingerprint)
	header.Set("User-Agent-Printer", deviceModel)
	header.Set("User-Agent-Version", "6.4.0")

	dialer := websocket.Dialer{Subprotocols: []string{"prusa-connect"}, HandshakeTimeout: 10 * time.Second}

	conn, response, err := dialer.Dial(websocketURL(baseURL), header)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}

	if err != nil {
		t.Fatalf("dial as %s: %v", fingerprint, err)
	}

	return conn
}
