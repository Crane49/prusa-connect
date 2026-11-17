package connectorapitest

import (
	"context"
	"fmt"
	"sync"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// Host is a host fake for an inbound driver: it implements
// connectorapi.Arrivals the way a real host does, and records what a driver
// admitted and announced.
//
// It exists because an inbound driver needs no hardware and no loopback to be
// exercised. Its routes are an http.Handler, so a test mounts them, has a
// device dial them, and holds the session that comes out. Declaring that a
// driver cannot be connected to in a unit test is an answer only an outbound
// driver has.
//
// Every method is safe for concurrent use: a driver serving many devices calls
// Admit and Arrived from many request goroutines.
type Host struct {
	mu sync.Mutex

	profile  connectorapi.Set
	refusal  error
	admitted []connectorapi.Identity
	sessions []connectorapi.Session

	// arrived carries each announced session to whoever is waiting for one. It
	// is buffered, so a driver that announces before anything waits does not
	// block inside its own request handler.
	arrived chan connectorapi.Session
}

// hostBacklog bounds how many sessions may arrive before a test collects one.
// A driver that announces more devices than a conformance run asked for is
// answered rather than blocked, and ErrBacklogFull says which happened.
const hostBacklog = 16

// ErrBacklogFull reports a session announced when the fake was already holding
// hostBacklog of them uncollected. It wraps connectorapi.ErrSessionNotTaken, so
// a driver that inspects the error at all sees the answer the contract names.
var ErrBacklogFull = fmt.Errorf(
	"%w: the host fake is holding more sessions than a test collected", connectorapi.ErrSessionNotTaken)

// NewHost returns a host fake that admits every device and answers with
// profile, which is the tier a real host resolves from its data files.
func NewHost(profile connectorapi.Set) *Host {
	return &Host{profile: profile, arrived: make(chan connectorapi.Session, hostBacklog)}
}

// Refusing returns a host fake that refuses every device with err, which is
// what a real host does for a printer it does not know or an operator has not
// approved.
func Refusing(err error) *Host {
	host := NewHost(nil)
	host.refusal = err

	return host
}

// Admit answers with the profile tier, or with the refusal this fake was built
// with.
func (h *Host) Admit(ctx context.Context, identity connectorapi.Identity) (connectorapi.Set, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.refusal != nil {
		return nil, h.refusal
	}

	h.admitted = append(h.admitted, identity)

	return h.profile.Clone(), nil
}

// Arrived takes the session and makes it available to AwaitSession.
func (h *Host) Arrived(ctx context.Context, session connectorapi.Session) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if session == nil {
		return fmt.Errorf("%w: a driver announced a nil session", connectorapi.ErrSessionNotTaken)
	}

	h.mu.Lock()
	h.sessions = append(h.sessions, session)
	h.mu.Unlock()

	select {
	case h.arrived <- session:
		return nil
	default:
		return ErrBacklogFull
	}
}

// AwaitSession blocks until a device has been announced, or until ctx ends.
//
// It waits on a channel rather than polling, so a caller under
// testing/synctest blocks durably and a caller outside it has no deadline to
// tune.
func (h *Host) AwaitSession(ctx context.Context) (connectorapi.Session, error) {
	select {
	case session := <-h.arrived:
		return session, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("waiting for a device to be announced: %w", ctx.Err())
	}
}

// Admissions returns the identities the driver put through Admit, in order.
func (h *Host) Admissions() []connectorapi.Identity {
	h.mu.Lock()
	defer h.mu.Unlock()

	return append([]connectorapi.Identity(nil), h.admitted...)
}

// Sessions returns every session the driver announced, in order.
func (h *Host) Sessions() []connectorapi.Session {
	h.mu.Lock()
	defer h.mu.Unlock()

	return append([]connectorapi.Session(nil), h.sessions...)
}

// Resolve merges the three tiers the way a real host does: the driver's
// baseline is the ceiling, this fake's profile narrows it, and the announced
// session's own capabilities are the probe.
//
// It is here so that a driver dialled into reaches the resolution a driver
// dialled out to reaches, through the same intersection and in the same order.
func (h *Host) Resolve(baseline connectorapi.Set, session connectorapi.Session) connectorapi.Set {
	h.mu.Lock()
	profile := h.profile.Clone()
	h.mu.Unlock()

	resolved := baseline.Clone()
	if resolved == nil {
		resolved = connectorapi.Set{}
	}

	if profile != nil {
		resolved = resolved.Intersect(profile)
	}

	if session != nil {
		probe, _ := session.Capabilities()
		resolved = resolved.Intersect(probe)
	}

	return resolved
}
