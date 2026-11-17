package echo

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// Session is one connected device.
//
// The host calls Do on one goroutine while the device calls the routes on
// others. Everything between them goes through a channel. The waiting map is
// read and written only under mu, and nothing blocks while it has mu.
type Session struct {
	identity connectorapi.Identity
	caps     connectorapi.Set

	// queued passes a command from Do to the next poll. It is bounded, so a
	// device that stops polling fails commands.
	queued chan *call

	// patches is what the host reads. pump is its only writer and its only
	// closer, so Close cannot race a send.
	inbox   chan connectorapi.StatePatch
	patches chan connectorapi.StatePatch

	done      chan struct{}
	closeOnce sync.Once
	// onClose drops the driver's reference to this session. It is set at
	// construction, before another goroutine can reach the session.
	onClose func()

	nextID atomic.Uint64

	mu      sync.Mutex
	waiting map[uint64]*call
}

// call is one command waiting for the device to answer it.
type call struct {
	id  uint64
	cmd connectorapi.Command
	// result gets one outcome. It is buffered, so delivering it never blocks
	// on a caller that gave up.
	result chan connectorapi.Result
	// once guards result against a device that answers the same id twice.
	once sync.Once
}

const (
	// patchBacklog is how many state patches may wait for the host to read
	// them.
	patchBacklog = 32
)

func newSession(
	identity connectorapi.Identity, caps connectorapi.Set, queueDepth int, onClose func(),
) *Session {
	s := &Session{
		identity: identity,
		caps:     caps,
		onClose:  onClose,
		queued:   make(chan *call, queueDepth),
		inbox:    make(chan connectorapi.StatePatch, patchBacklog),
		patches:  make(chan connectorapi.StatePatch),
		done:     make(chan struct{}),
		waiting:  make(map[uint64]*call),
	}

	go s.pump()

	return s
}

// Identity is what the device said it is when it dialled in.
func (s *Session) Identity() connectorapi.Identity { return s.identity }

// Capabilities is the resolved set and its epoch. The set is a copy, so a
// caller cannot change it within an epoch. The epoch is always 1: this dialect
// does not renegotiate. A dialect that does raises the epoch and swaps the set
// atomically.
func (s *Session) Capabilities() (connectorapi.Set, uint64) { return s.caps.Clone(), 1 }

// State is the channel of updates to the state tree. It closes when the
// session does.
func (s *Session) State() <-chan connectorapi.StatePatch { return s.patches }

// Do sends one command to the device and waits for its answer. The host has
// already validated the arguments against Capabilities.
func (s *Session) Do(ctx context.Context, cmd connectorapi.Command) (connectorapi.Result, error) {
	// The host already refused a verb this printer does not have. This is a
	// second check, so a device never answers for an undeclared verb.
	if !s.caps.Has(cmd.Verb) {
		return connectorapi.Result{}, connectorapi.CapabilityNotSupported.
			New("the printer does not have that capability").
			With(fault.StringParam("verb", cmd.Verb.String()))
	}

	pending := &call{
		id:     s.nextID.Add(1),
		cmd:    cmd,
		result: make(chan connectorapi.Result, 1),
	}

	if err := s.offer(ctx, pending); err != nil {
		return connectorapi.Result{}, err
	}

	select {
	case result := <-pending.result:
		return result, nil
	case <-ctx.Done():
		s.abandon(pending.id)

		return connectorapi.Result{}, fmt.Errorf("echo: waiting for %s: %w", cmd.Verb, ctx.Err())
	case <-s.done:
		s.abandon(pending.id)

		return connectorapi.Result{}, fmt.Errorf("echo: %s: %w", cmd.Verb, ErrSessionClosed)
	}
}

// offer puts a command in the queue. It fails at once when the queue is full,
// so a caller does not wait behind a stalled device.
func (s *Session) offer(ctx context.Context, pending *call) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("echo: %s: %w", pending.cmd.Verb, err)
	}

	select {
	case <-s.done:
		return fmt.Errorf("echo: %s: %w", pending.cmd.Verb, ErrSessionClosed)
	default:
	}

	s.mu.Lock()
	s.waiting[pending.id] = pending
	s.mu.Unlock()

	select {
	case s.queued <- pending:
		return nil
	default:
		s.abandon(pending.id)

		// Return the registered code, with the sentinel as the cause. The
		// host reports an unregistered error as an internal one.
		return connectorapi.CommandQueueFull.
			New("the device is not draining its command queue").
			With(fault.StringParam("verb", pending.cmd.Verb.String())).
			Wrap(ErrQueueFull)
	}
}

// nextCall answers one poll. ok is false when the window passed with nothing
// queued.
func (s *Session) nextCall(ctx context.Context, window time.Duration) (*call, bool) {
	timer := time.NewTimer(window)
	defer timer.Stop()

	select {
	case pending := <-s.queued:
		return pending, true
	case <-timer.C:
		return nil, false
	case <-ctx.Done():
		return nil, false
	case <-s.done:
		return nil, false
	}
}

// complete gives one outcome to the caller waiting for it. An unknown id is
// dropped.
func (s *Session) complete(id uint64, result connectorapi.Result) {
	s.mu.Lock()
	pending, ok := s.waiting[id]
	delete(s.waiting, id)
	s.mu.Unlock()

	if !ok {
		return
	}

	pending.once.Do(func() { pending.result <- result })
}

// abandon forgets a command whose caller has stopped waiting.
func (s *Session) abandon(id uint64) {
	s.mu.Lock()
	delete(s.waiting, id)
	s.mu.Unlock()
}

// report queues one state patch for the host.
func (s *Session) report(ctx context.Context, patch connectorapi.StatePatch) error {
	select {
	case s.inbox <- patch:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("echo: reporting %s: %w", patch.Path, ctx.Err())
	case <-s.done:
		return fmt.Errorf("echo: reporting %s: %w", patch.Path, ErrSessionClosed)
	}
}

// pump is the only writer to patches and the only one that closes it, so a
// handler never sends on a closed channel.
func (s *Session) pump() {
	defer close(s.patches)

	for {
		select {
		case <-s.done:
			return
		case patch := <-s.inbox:
			select {
			case s.patches <- patch:
			case <-s.done:
				return
			}
		}
	}
}

// Close releases the session. It is safe to call more than once.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)

		if s.onClose != nil {
			s.onClose()
		}
	})

	return nil
}
