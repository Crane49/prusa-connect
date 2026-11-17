package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// dialType is the dialect these cases dial with. It is registered globally, in
// the init below, because the headline case drives Run: the composition root
// reads the process registry and a declaration handed to one call would prove
// the wiring of that call and not of the server.
const dialType connectorapi.TypeName = "listening-printer"

//nolint:gochecknoinits // the registry is read at start-up, so a test dialect has to be there before one.
func init() {
	connectorapi.Register(dialType, dialDoc(), func(connectorapi.Spec) (connectorapi.Driver, error) {
		return dialsOverTCP{baselineDriver{baseline: connectorapi.Set{
			statusVerb: {Flag: true, Kind: connectorapi.KindFlag},
		}}}, nil
	})
}

func dialDoc() extension.Doc {
	return extension.Doc{
		Summary: "A dialect the host reaches: the printer listens and waits, and the server opens the connection.",
		Description: "It is the shape of a Moonraker host, a Bambu printer in LAN mode and a serial port. " +
			"The device answers one line of JSON saying what it is, and the connection staying open is the " +
			"session. It exists so the dial path can be driven against a real socket.",
		Version:   "1.0.0",
		Contract:  connectorapi.Contract,
		Stability: extension.StabilityExperimental,
	}
}

// TestAnEndpointInTheOverlayIsDialledAndThePrinterArrives is the whole path,
// through the composition root the binary runs.
//
// A printer that listens and waits is the machine the outbound direction exists
// for, and a server that cannot open a connection cannot reach one at all. The
// address is in the machine-owned overlay and not in the operator's file, which
// is the owner's answer in earlier research.
func TestAnEndpointInTheOverlayIsDialledAndThePrinterArrives(t *testing.T) {
	t.Parallel()

	device := newListeningPrinter(t, "DIAL-ARRIVES")
	dir := t.TempDir()

	writeEndpoint(t, dir, device.address())

	running := start(t, dir, dialConfig())

	require.Eventually(t, func() bool {
		listed := printers(t, running)

		return len(listed) == 1 && listed[0].Connected
	}, settle, 25*time.Millisecond, "the server never dialled the printer that was listening for it")

	require.Positive(t, device.accepted(), "the printer was listed and nothing ever opened a connection")

	listed := printers(t, running)
	require.Equal(t, "MK4", listed[0].Model)
	require.Equal(t, "DIAL-ARRIVES", listed[0].Serial)

	running.stopAndWait(t)
}

// TestAnEndpointAddedOverTheApiIsDialledWithNoRestart is the demo's own story.
//
// Nothing is declared when the server starts. One HTTP call adds a printer that
// listens, and the machine arrives on the fleet without anybody restarting the
// process, which is what makes the interface a client of the API rather than a
// second way to edit a file.
func TestAnEndpointAddedOverTheApiIsDialledWithNoRestart(t *testing.T) {
	t.Parallel()

	device := newListeningPrinter(t, "DIAL-OVER-HTTP")
	dir := t.TempDir()

	running := start(t, dir, dialConfig())

	require.Empty(t, printers(t, running), "the server started holding a printer nobody declared")

	created := running.put(t, "/api/v1/connector-endpoints/bench",
		`{"connector":"far","address":"`+device.address()+`","enabled":true}`)
	require.Equal(t, http.StatusCreated, created.status, created.body)
	require.Contains(t, created.body, device.address())

	require.Eventually(t, func() bool {
		listed := printers(t, running)

		return len(listed) == 1 && listed[0].Connected
	}, settle, 25*time.Millisecond, "an address added over the API never reached the dial loop")

	require.Positive(t, device.accepted(), "the printer was listed and nothing opened a connection")
	require.Equal(t, "DIAL-OVER-HTTP", printers(t, running)[0].Serial)

	// The row is readable back through the same surface, under the token the
	// next write has to quote.
	listed := running.get(t, "/api/v1/connector-endpoints")
	require.Equal(t, http.StatusOK, listed.status, listed.body)
	require.Contains(t, listed.body, device.address())

	running.stopAndWait(t)
}

// TestADialledPrinterWhoseSessionEndsIsDialledAgain is the redial. A printer
// that listens is switched off and on again, and nothing asks the server to
// look: the loop is what notices, and the entry comes back on its own.
func TestADialledPrinterWhoseSessionEndsIsDialledAgain(t *testing.T) {
	t.Parallel()

	device := newListeningPrinter(t, "DIAL-AGAIN")
	dir := t.TempDir()

	writeEndpoint(t, dir, device.address())

	running := start(t, dir, dialConfig())

	require.Eventually(t, func() bool {
		listed := printers(t, running)

		return len(listed) == 1 && listed[0].Connected
	}, settle, 25*time.Millisecond, "the printer never arrived in the first place")

	first := device.accepted()

	// The cable out of the machine, and not the server stopping.
	device.hangUp()

	require.Eventually(t, func() bool {
		return device.accepted() > first
	}, settle, 25*time.Millisecond, "the session ended and the address was never dialled again")

	require.Eventually(t, func() bool {
		listed := printers(t, running)

		return len(listed) == 1 && listed[0].Connected
	}, settle, 25*time.Millisecond, "the printer was dialled again and never came back to the listing")

	running.stopAndWait(t)
}

// TestAnAddressThatAnswersNothingIsDialledAgainOnTheLadder is a design requirement
// and an earlier design decision's other half: an endpoint nothing is listening on is retried
// for ever, with full jitter and no ceiling on the attempts.
//
// The ladder is a fast one here. What is under test is that the loop climbs it
// rather than stopping at the first refusal, and supervisor's own suite is
// where the rungs themselves are measured.
func TestAnAddressThatAnswersNothingIsDialledAgainOnTheLadder(t *testing.T) {
	t.Parallel()

	closed := freeAddr(t)
	connector := dialConnectorFor(t, newPlane(t))
	tried := &countingOutbound{Outbound: connector.driver}
	connector.driver = tried

	loops := newDials(discard(), []dialledConnector{connector}, fastLadder())
	loops.start(t.Context())

	t.Cleanup(loops.stop)

	loops.SyncEndpoints(t.Context(), []resource.Endpoint{{
		Name: "bench", Connector: "far", Address: closed, Enabled: true,
	}})

	require.Eventually(t, func() bool {
		return tried.count() >= 3
	}, settle, 5*time.Millisecond, "an address nothing answers on was dialled %d times", tried.count())
}

// TestAnEndpointNamingAConnectorThisBuildCannotDialSaysSo is the row an
// operator has to be able to find. The endpoint list survives a configuration
// change, so an address left behind by a connector that is no longer declared
// must be a log line and not a server that refuses to serve.
func TestAnEndpointNamingAConnectorThisBuildCannotDialSaysSo(t *testing.T) {
	t.Parallel()

	said := &spokenTo{}

	loops := newDials(slog.New(slog.NewTextHandler(said, &slog.HandlerOptions{Level: slog.LevelWarn})),
		nil, fastLadder())
	loops.start(t.Context())

	t.Cleanup(loops.stop)

	loops.SyncEndpoints(t.Context(), []resource.Endpoint{{
		Name: "bench", Connector: "gone", Address: "127.0.0.1:1", Enabled: true,
	}})

	require.Eventually(t, func() bool {
		return said.contains("does not dial with")
	}, settle, 5*time.Millisecond, "an endpoint nothing can dial was dropped in silence")

	require.True(t, said.contains("connector=gone"), "the warning does not name the connector")
}

// TestAnEndpointThatIsSwitchedOffIsNotDialled is the operator's switch. A
// declaration is still readable when it is off, and nothing opens a connection
// for it.
func TestAnEndpointThatIsSwitchedOffIsNotDialled(t *testing.T) {
	t.Parallel()

	device := newListeningPrinter(t, "DIAL-OFF")

	loops := newDials(discard(), []dialledConnector{dialConnectorFor(t, newPlane(t))}, fastLadder())
	loops.start(t.Context())

	t.Cleanup(loops.stop)

	loops.SyncEndpoints(t.Context(), []resource.Endpoint{{
		Name: "bench", Connector: "far", Address: device.address(), Enabled: false,
	}})

	// A dial takes microseconds against a listener in this process, so a wait
	// that finds nothing is a wait long enough to have found one.
	time.Sleep(50 * time.Millisecond)
	require.Zero(t, device.accepted(), "an endpoint that is switched off was dialled")

	loops.SyncEndpoints(t.Context(), []resource.Endpoint{{
		Name: "bench", Connector: "far", Address: device.address(), Enabled: true,
	}})

	require.Eventually(t, func() bool {
		return device.accepted() > 0
	}, settle, 5*time.Millisecond, "switching an endpoint on did not start dialling it")
}

// dialConfig is the operator's file for the cases above. It names the dialect
// and no address at all: core.connectors carries no such key, which is what
// earlier research measured and what the endpoint list replaces.
func dialConfig() string {
	return `
core:
  log:
    level: error
  connectors:
    far:
      type: listening-printer
`
}

// writeEndpoint puts one address into the machine-owned overlay before the
// server starts, which is what a restart reads back after somebody added a
// printer through the API.
func writeEndpoint(t *testing.T, dir, address string) {
	t.Helper()

	overlay := filepath.Join(dir, "config.d", "resources.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(overlay), 0o700))

	document := map[string]any{
		"version":   1,
		"instances": []any{},
		"endpoints": []resource.Endpoint{{
			Name: "bench", Connector: "far", Address: address, Enabled: true,
		}},
	}

	encoded, err := json.Marshal(document)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(overlay, encoded, 0o600))
}

func fastLadder() supervisor.BackoffPolicy {
	return supervisor.BackoffPolicy{
		Base:   time.Millisecond,
		Cap:    5 * time.Millisecond,
		Rand:   func() float64 { return 1 },
		Spread: 0,
	}
}

// listeningPrinter is a machine that waits to be reached: it accepts a
// connection, says what it is in one line of JSON, and holds the connection
// open until one side drops it.
type listeningPrinter struct {
	listener net.Listener
	accepts  atomic.Int64

	mu   sync.Mutex
	open []net.Conn
}

func newListeningPrinter(t *testing.T, serial string) *listeningPrinter {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	device := &listeningPrinter{listener: listener}

	t.Cleanup(func() {
		_ = listener.Close()
		device.hangUp()
	})

	go device.serve(serial)

	return device
}

func (p *listeningPrinter) address() string { return p.listener.Addr().String() }

func (p *listeningPrinter) accepted() int64 { return p.accepts.Load() }

func (p *listeningPrinter) serve(serial string) {
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			return
		}

		p.mu.Lock()
		p.open = append(p.open, conn)
		p.mu.Unlock()

		announced, err := json.Marshal(wireIdentity{
			Vendor: "Prusa", Model: "MK4", Serial: serial, Firmware: "6.4.0",
		})
		if err != nil {
			return
		}

		if _, err = conn.Write(append(announced, '\n')); err != nil {
			continue
		}

		p.accepts.Add(1)
	}
}

// hangUp drops every open connection, which is the cable coming out of the
// machine rather than the server stopping.
func (p *listeningPrinter) hangUp() {
	p.mu.Lock()
	open := p.open
	p.open = nil
	p.mu.Unlock()

	for _, conn := range open {
		_ = conn.Close()
	}
}

// wireIdentity is what the device announces.
type wireIdentity struct {
	Vendor   string `json:"vendor"`
	Model    string `json:"model"`
	Serial   string `json:"serial"`
	Firmware string `json:"firmware"`
}

// dialsOverTCP is the dialling half of a dialect. It is the smallest thing that
// is still real: open the connection, read the identity the device announces,
// and hold the socket for as long as the session lasts.
type dialsOverTCP struct{ baselineDriver }

// Connect opens the connection and performs the identity exchange, which is
// what connectorapi.Outbound obliges it to do inside this call.
func (d dialsOverTCP) Connect(
	ctx context.Context, endpoint connectorapi.Endpoint, profile connectorapi.Set,
) (connectorapi.Session, error) {
	var dialer net.Dialer

	conn, err := dialer.DialContext(ctx, "tcp", endpoint.Address)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", endpoint.Address, err)
	}

	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		_ = conn.Close()

		return nil, fmt.Errorf("read the identity from %s: %w", endpoint.Address, err)
	}

	var announced wireIdentity
	if err = json.Unmarshal(line, &announced); err != nil {
		_ = conn.Close()

		return nil, fmt.Errorf("decode the identity from %s: %w", endpoint.Address, err)
	}

	session := &tcpSession{
		conn: conn,
		identity: connectorapi.Identity{
			Vendor:   announced.Vendor,
			Model:    announced.Model,
			Serial:   announced.Serial,
			Firmware: connectorapi.NewFirmwareLabel(announced.Firmware),
		},
		capabilities: d.baseline.Clone().Intersect(profile),
		patches:      make(chan connectorapi.StatePatch),
	}

	go session.read()

	return session, nil
}

// tcpSession is one connection to a listening printer. The state channel closes
// when the connection drops, whichever end dropped it, which is the signal the
// host retires the entry on and the dial loop redials on.
type tcpSession struct {
	conn         net.Conn
	identity     connectorapi.Identity
	capabilities connectorapi.Set
	patches      chan connectorapi.StatePatch
	closing      sync.Once
}

func (s *tcpSession) Identity() connectorapi.Identity { return s.identity }

func (s *tcpSession) Capabilities() (connectorapi.Set, uint64) { return s.capabilities.Clone(), 1 }

func (s *tcpSession) Do(context.Context, connectorapi.Command) (connectorapi.Result, error) {
	return connectorapi.Result{OK: true}, nil
}

func (s *tcpSession) State() <-chan connectorapi.StatePatch { return s.patches }

func (s *tcpSession) Close() error {
	s.closing.Do(func() { _ = s.conn.Close() })

	return nil
}

// read holds the connection until it drops and then closes the state channel,
// which is the whole of this dialect's session lifetime.
func (s *tcpSession) read() {
	defer close(s.patches)

	buffer := make([]byte, 64)

	for {
		if _, err := s.conn.Read(buffer); err != nil {
			return
		}
	}
}

// countingOutbound counts the dials made through it, for the case that has to
// read how many times an address nothing answers on was tried.
type countingOutbound struct {
	connectorapi.Outbound

	dials atomic.Int64
}

func (c *countingOutbound) Connect(
	ctx context.Context, endpoint connectorapi.Endpoint, profile connectorapi.Set,
) (connectorapi.Session, error) {
	c.dials.Add(1)

	return c.Outbound.Connect(ctx, endpoint, profile)
}

func (c *countingOutbound) count() int64 { return c.dials.Load() }

// spokenTo is a log sink a case reads while the goroutine writing it is still
// running, which a strings.Builder is not safe for.
type spokenTo struct {
	mu    sync.Mutex
	lines strings.Builder
}

func (s *spokenTo) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.lines.Write(p)
}

func (s *spokenTo) contains(text string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return strings.Contains(s.lines.String(), text)
}

// dialConnectorFor is the connector a case builds through the real mountOne, so
// what is dialled is what the composition root would hand the loop.
func dialConnectorFor(t *testing.T, plane *devices) dialledConnector {
	t.Helper()

	types, err := connectorapi.NewRegistry()
	require.NoError(t, err)

	built, err := mountOne(t.Context(), types, discard(), plane, config.NamedConnector{
		Name:      "far",
		Connector: config.Connector{Type: string(dialType)},
	}, nil, nil, "", "")
	require.NoError(t, err, "a declared outbound dialect is not a configuration error")
	require.Empty(t, built.mounts, "an outbound dialect mounted a route, and a route is what a device dials")
	require.NotNil(t, built.dialled, "an outbound dialect was not handed to the dial loop")

	return *built.dialled
}
