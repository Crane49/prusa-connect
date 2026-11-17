package simulator_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// host is the smallest thing that can drive the simulator: the frozen
// registration endpoints, the websocket upgrade, and a command queue that holds
// commands while a printer is away and drains them when it comes back.
//
// It encodes the nine-byte header itself rather than calling into the package
// under test. Earlier research forbids the two sides sharing a
// codec, because a shared encoder bug makes both ends agree and the test passes
// anyway.
type host struct {
	server   *http.Server
	listener *pipeListener
	upgrader websocket.Upgrader

	// onMessage receives every printer-to-server frame, decoded. onSession
	// receives a fingerprint each time that printer connects.
	onMessage func(fingerprint string, payload map[string]any)
	onSession func(fingerprint string)

	// quiet, once closed, stops the host consuming a session's frames and
	// leaves the socket open. It is how a case puts a printer behind a peer
	// that has stopped reading, which over net.Pipe is what blocks a write.
	// A nil channel never fires, so every other case reads for the whole
	// session.
	quiet chan struct{}

	mu      sync.Mutex
	nextID  uint32
	pending map[string]int
	conns   map[string]*websocket.Conn
	queued  map[string][][]byte

	readers sync.WaitGroup
	serving sync.WaitGroup
}

// newHost starts a host on an in-memory listener. Nothing here touches a real
// socket, which is what keeps a test using it inside a synctest bubble.
func newHost(t *testing.T) *host {
	t.Helper()

	h := &host{
		listener:  newPipeListener(),
		upgrader:  websocket.Upgrader{Subprotocols: []string{"prusa-connect"}},
		onMessage: func(string, map[string]any) {},
		onSession: func(string) {},
		pending:   map[string]int{},
		conns:     map[string]*websocket.Conn{},
		queued:    map[string][][]byte{},
	}

	h.server = &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}

	h.serving.Go(func() { _ = h.server.Serve(h.listener) })

	return h
}

// ServeHTTP routes the two frozen endpoints by hand. A net/http mux would do it
// in one line, and internal/transport/http/httpapi's contract test refuses one
// anywhere under internal/: a route registered on a mux is a route chi.Walk
// cannot enumerate and the OpenAPI document never publishes. Two paths do not
// need a router.
func (h *host) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/p/register":
		h.handleRegister(w, r)
	case "/p/ws":
		h.handleWebsocket(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// baseURL is the address the simulator is pointed at. The host name is
// meaningless: the dialler ignores it and hands back a pipe.
func (h *host) baseURL() string { return "http://simulated-host" }

func (h *host) dial(ctx context.Context, network, address string) (net.Conn, error) {
	return h.listener.dial(ctx, network, address)
}

// holdRegistration makes the next n polls answer 202, so a test can watch the
// printer wait for an operator instead of being admitted at once.
func (h *host) holdRegistration(fingerprint string, polls int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.pending[fingerprint] = polls
}

// send queues a high-level command for a printer and returns the id it will
// answer under. A printer that is not connected gets it on its next session,
// which is the queue-and-drain rule of earlier research.
func (h *host) send(fingerprint, command string, kwargs map[string]any) (uint32, error) {
	body := map[string]any{"command": command}
	if kwargs != nil {
		body["kwargs"] = kwargs
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}

	return h.sendFrame(fingerprint, 'J', encoded)
}

// sendGCode queues a G frame. Nineteen of the fifty-seven verbs leave as gcode
// rather than as a JSON command, and the printer
// answers one without consulting its model, because gcode is not in the
// capability vocabulary.
func (h *host) sendGCode(fingerprint, line string) (uint32, error) {
	return h.sendFrame(fingerprint, 'G', []byte(line))
}

// sendFrame writes one nine-byte-headed frame, or queues it when the printer is
// away.
func (h *host) sendFrame(fingerprint string, kind byte, payload []byte) (uint32, error) {
	h.mu.Lock()

	h.nextID++
	id := h.nextID
	frame := append(fmt.Appendf(nil, "%c%08x", kind, id), payload...)
	conn := h.conns[fingerprint]

	if conn == nil {
		h.queued[fingerprint] = append(h.queued[fingerprint], frame)
	}

	h.mu.Unlock()

	if conn == nil {
		return id, nil
	}

	return id, conn.WriteMessage(websocket.TextMessage, frame)
}

// sendChunk answers a block request with one T frame. It is the one frame whose
// id is not the host's own counter: a chunk is addressed to the file id the
// printer asked under, and a chunk is never queued, because a printer that is
// away asked for nothing.
func (h *host) sendChunk(fingerprint string, fileID uint32, payload []byte) error {
	h.mu.Lock()
	conn := h.conns[fingerprint]
	h.mu.Unlock()

	if conn == nil {
		return fmt.Errorf("no connection for %s", fingerprint)
	}

	frame := append(fmt.Appendf(nil, "T%08x", fileID), payload...)

	return conn.WriteMessage(websocket.BinaryMessage, frame)
}

// queuedFor is how many commands are waiting for a printer that is away.
func (h *host) queuedFor(fingerprint string) int {
	h.mu.Lock()
	defer h.mu.Unlock()

	return len(h.queued[fingerprint])
}

func (h *host) connected(fingerprint string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.conns[fingerprint] != nil
}

func (h *host) stop() {
	h.mu.Lock()

	conns := make([]*websocket.Conn, 0, len(h.conns))
	for _, conn := range h.conns {
		conns = append(conns, conn)
	}

	h.mu.Unlock()

	for _, conn := range conns {
		_ = conn.Close()
	}

	_ = h.server.Close()
	_ = h.listener.Close()

	h.readers.Wait()
	h.serving.Wait()
}

func (h *host) handleRegister(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var body struct {
			SerialNumber string `json:"sn"`
			Fingerprint  string `json:"fingerprint"`
			PrinterType  string `json:"printer_type"`
			Firmware     string `json:"firmware"`
		}

		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Fingerprint == "" {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		// The code carries the fingerprint so the poll can find the row again
		// without a second map.
		w.Header().Set("Code", "CODE-"+body.Fingerprint)
		w.WriteHeader(http.StatusOK)
	case http.MethodGet:
		code := r.Header.Get("Code")
		if code == "" {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		fingerprint := code[len("CODE-"):]

		h.mu.Lock()
		remaining := h.pending[fingerprint]

		if remaining > 0 {
			h.pending[fingerprint] = remaining - 1
		}

		h.mu.Unlock()

		if remaining > 0 {
			w.WriteHeader(http.StatusAccepted)

			return
		}

		w.Header().Set("Token", "TOKEN-"+fingerprint)
		w.WriteHeader(http.StatusOK)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *host) handleWebsocket(w http.ResponseWriter, r *http.Request) {
	fingerprint := r.Header.Get("Fingerprint")
	if fingerprint == "" || r.Header.Get("Token") != "TOKEN-"+fingerprint {
		w.WriteHeader(http.StatusUnauthorized)

		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	h.mu.Lock()
	h.conns[fingerprint] = conn
	drain := h.queued[fingerprint]
	h.queued[fingerprint] = nil
	h.mu.Unlock()

	// Reading starts before anything is written. A server that writes to a
	// connection it is not yet reading from deadlocks against a printer whose
	// first act is to write, which on a reconnection carrying queued commands is
	// every printer with a transfer to resume.
	h.readers.Go(func() { h.read(fingerprint, conn) })

	for _, frame := range drain {
		if writeErr := conn.WriteMessage(websocket.TextMessage, frame); writeErr != nil {
			break
		}
	}

	h.onSession(fingerprint)
}

func (h *host) read(fingerprint string, conn *websocket.Conn) {
	for {
		select {
		case <-h.quiet:
			// The socket stays open and stop() closes it, so the printer's next
			// write has a peer that will never read it.
			return
		default:
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			h.forget(fingerprint, conn)

			_ = conn.Close()

			return
		}

		var payload map[string]any
		if json.Unmarshal(data, &payload) != nil {
			continue
		}

		h.onMessage(fingerprint, payload)
	}
}

// forget drops the session's connection, so a later send queues rather than
// writing into a socket nothing is reading.
func (h *host) forget(fingerprint string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.conns[fingerprint] == conn {
		delete(h.conns, fingerprint)
	}
}

// pipeListener hands out net.Pipe connections. net.Pipe blocks on channels,
// which testing/synctest counts as durably blocked, so a whole session runs in
// virtual time. A localhost socket does not: the package documentation excludes
// network I/O, and earlier research is that this is a property of the
// runtime rather than a budget preference.
type pipeListener struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{conns: make(chan net.Conn), done: make(chan struct{})}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.conns:
		return conn, nil
	case <-l.done:
		return nil, io.EOF
	}
}

func (l *pipeListener) Close() error {
	l.once.Do(func() { close(l.done) })

	return nil
}

func (l *pipeListener) Addr() net.Addr { return pipeAddr{} }

func (l *pipeListener) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	client, server := net.Pipe()

	select {
	case l.conns <- server:
		return client, nil
	case <-l.done:
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "simulated-host" }
