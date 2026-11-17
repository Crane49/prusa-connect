package fleet

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/internal/connector/xbuddy"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// handshakeBudget bounds the header read on the in-memory listener. It is
// virtual time, so its only job is to stop a wedged handshake hanging a bubble.
const handshakeBudget = 5 * time.Second

// queueDepth is how many commands the driver holds for a printer that is away.
// The offline scenario queues one, so anything above one proves the same thing.
const queueDepth = 8

// rig is one cell under test: the xbuddy driver, the host half that resolves
// capabilities, and a simulated printer of that model and firmware, joined by a
// net.Pipe.
//
// It is the shape internal/connector/xbuddy's own suite uses, driven from data
// instead of from a hand-picked model, and with one deliberate difference: a
// firmware no profile covers is admitted on the driver baseline rather than
// refused. That is what a host must do, and refusing
// would make the unmatched cell untestable.
type rig struct {
	cell     Cell
	driver   *xbuddy.Driver
	host     *arrivals
	printer  *simulator.Printer
	ctx      context.Context //nolint:containedctx // the rig is one test's scope and dies with it.
	cancel   context.CancelFunc
	server   *http.Server
	listener *pipeListener
	sessions []*capability.Session
	spy      *driverSpy
	running  chan error
}

// rigOption varies the printer or the driver for one scenario.
type rigOption func(*rigSettings)

type rigSettings struct {
	grace time.Duration
	files []simulator.File
}

// withGrace closes a session whose printer has been away that long.
func withGrace(grace time.Duration) rigOption {
	return func(s *rigSettings) { s.grace = grace }
}

// withFiles puts these files on the printer in place of its default tree.
func withFiles(files ...simulator.File) rigOption {
	return func(s *rigSettings) { s.files = files }
}

// newRig builds everything one cell needs and leaves it unconnected.
func newRig(t *testing.T, cell Cell, opts ...rigOption) *rig {
	t.Helper()

	settings := rigSettings{}
	for _, apply := range opts {
		apply(&settings)
	}

	profiles, err := profile.Builtin()
	if err != nil {
		t.Fatalf("load the profiles: %v", err)
	}

	quiet := slog.New(slog.DiscardHandler)

	host := &arrivals{
		profiles: profiles,
		coverage: profiles.Coverage(),
		resolver: capability.NewResolver(profiles, quiet),
		sessions: make(chan *capability.Session, 4),
		spies:    make(chan *driverSpy, 4),
	}

	// ModeToken is the strictest mode a simulated printer can satisfy: it
	// presents the token registration issued and nothing else.
	driver := xbuddy.NewDriver(xbuddy.ModeToken, queueDepth, settings.grace, "", quiet)
	host.baseline = driver.Baseline()

	handler, err := driver.Handler(host)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	listener := newPipeListener()
	server := &http.Server{Handler: handler, ReadHeaderTimeout: handshakeBudget}

	go func() { _ = server.Serve(listener) }()

	printer, err := simulator.New(simulator.Config{
		Model:    cell.Model,
		Firmware: cell.Firmware,
		BaseURL:  "http://xbuddy-host",
		Dial:     listener.dial,
		Files:    settings.files,
	})
	if err != nil {
		t.Fatalf("new printer for %s: %v", cell, err)
	}

	ctx, cancel := context.WithCancel(t.Context())

	return &rig{
		cell: cell, driver: driver, host: host, printer: printer,
		ctx: ctx, cancel: cancel, server: server, listener: listener,
	}
}

// connect registers the printer, starts its loop and takes the session the host
// was handed.
func (r *rig) connect(t *testing.T) *capability.Session {
	t.Helper()

	// Mode token waits for an operator on every registration since the owner
	// decision of 2026-09-04, so the operator's half runs here while the
	// printer polls on its own goroutine.
	registered := make(chan error, 1)
	go func() { registered <- r.printer.Register(r.ctx) }()

	r.approve(t)

	if err := <-registered; err != nil {
		t.Fatalf("%s: register: %v", r.cell, err)
	}

	r.run()

	return r.await(t)
}

// approve lets through the registration the printer is polling on, which is
// what an operator does at /api/v1/registrations.
func (r *rig) approve(t *testing.T) {
	t.Helper()

	const attempts = 500

	for range attempts {
		if waiting := r.driver.Pending(); len(waiting) > 0 {
			if _, err := r.driver.Approve(r.ctx, waiting[0].Code, "user:tester"); err != nil {
				t.Fatalf("%s: approve: %v", r.cell, err)
			}

			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("%s: no registration was waiting for an operator", r.cell)
}

// run starts the printer's own loop.
func (r *rig) run() {
	r.running = make(chan error, 1)

	go func() { r.running <- r.printer.Run(r.ctx) }()
}

// await takes the session the host was handed.
func (r *rig) await(t *testing.T) *capability.Session {
	t.Helper()

	select {
	case session := <-r.host.sessions:
		r.sessions = append(r.sessions, session)
		r.spy = <-r.host.spies

		return session
	case <-r.ctx.Done():
		t.Fatalf("%s: no printer was announced", r.cell)

		return nil
	}
}

// stop ends everything in the order that lets each goroutine finish: the
// printer first, then the sessions holding websockets, then the server.
func (r *rig) stop(t *testing.T) {
	t.Helper()

	r.cancel()

	if r.running != nil {
		<-r.running
	}

	for _, session := range r.sessions {
		if err := session.Close(); err != nil {
			t.Errorf("%s: close a session: %v", r.cell, err)
		}
	}

	_ = r.server.Close()
	_ = r.listener.Close()
}

// arrivals is the host's half of an inbound dialect: capability.Resolve on
// Admit and capability.Wrap on Arrived.
type arrivals struct {
	profiles profile.Registry
	coverage connectorapi.Set
	resolver *capability.Resolver
	baseline connectorapi.Set

	sessions chan *capability.Session
	spies    chan *driverSpy

	mu       sync.Mutex
	admitted []connectorapi.Identity
	matched  []bool
}

// Admit takes every printer this driver can speak to. A model and firmware no
// profile covers is admitted on the driver baseline narrowed to what the
// profiles describe between them, which is the reduced capability set decision
// C1 calls for rather than a refusal to connect.
func (a *arrivals) Admit(_ context.Context, identity connectorapi.Identity) (connectorapi.Set, error) {
	matched, ok := a.profiles.Match(identity.Model, identity.Firmware)

	a.mu.Lock()
	a.admitted = append(a.admitted, identity)
	a.matched = append(a.matched, ok)
	a.mu.Unlock()

	if !ok {
		return a.coverage, nil
	}

	return matched.Capabilities, nil
}

func (a *arrivals) Arrived(ctx context.Context, session connectorapi.Session) error {
	probe, _ := session.Capabilities()
	watched := &driverSpy{inner: session}
	resolution := a.resolver.Arrive(ctx, a.baseline, session.Identity(), probe)

	a.spies <- watched
	a.sessions <- capability.Wrap(watched, resolution)

	return nil
}

// admissions is what the host was told about each printer that arrived, and
// whether a profile covered it.
func (a *arrivals) admissions() ([]connectorapi.Identity, []bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	return append([]connectorapi.Identity(nil), a.admitted...), append([]bool(nil), a.matched...)
}

// driverSpy counts what reaches the driver, so "the host refused it first" is an
// observation rather than an inference.
type driverSpy struct {
	inner     connectorapi.Session
	calls     atomic.Int64
	transfers atomic.Int64
}

func (s *driverSpy) Identity() connectorapi.Identity { return s.inner.Identity() }

func (s *driverSpy) Capabilities() (connectorapi.Set, uint64) { return s.inner.Capabilities() }

func (s *driverSpy) State() <-chan connectorapi.StatePatch { return s.inner.State() }

func (s *driverSpy) Close() error { return s.inner.Close() }

// Send forwards the transfer half and counts it.
//
// It has to be here rather than left off: a spy that answers only Session hides
// the half connectorapi.Uploads asks for, and every cell would report a printer
// that cannot be sent a file whatever its profile declares.
func (s *driverSpy) Send(ctx context.Context, up connectorapi.Upload) error {
	s.transfers.Add(1)

	transfers, ok := connectorapi.Uploads(s.inner)
	if !ok {
		return connectorapi.ErrNoTransfers
	}

	if err := transfers.Send(ctx, up); err != nil {
		return fmt.Errorf("driver: %w", err)
	}

	return nil
}

func (s *driverSpy) Do(ctx context.Context, cmd connectorapi.Command) (connectorapi.Result, error) {
	s.calls.Add(1)

	result, err := s.inner.Do(ctx, cmd)
	if err != nil {
		return result, fmt.Errorf("driver: %w", err)
	}

	return result, nil
}

// pipeListener hands out net.Pipe connections. net.Pipe blocks on channels,
// which testing/synctest counts as durably blocked, so a whole session runs in
// virtual time. A localhost socket does not, which is earlier research.
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
		return nil, fmt.Errorf("dial the in-memory host: %w", ctx.Err())
	}
}

type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "xbuddy-host" }
