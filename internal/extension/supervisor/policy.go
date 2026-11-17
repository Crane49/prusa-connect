package supervisor

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// Errors reported when a policy is rejected. Callers inspect them with
// errors.Is.
var (
	// ErrBreakerNotApplicable reports a breaker configured for a kind whose
	// disconnection is its steady state.
	ErrBreakerNotApplicable = errors.New("supervisor: this kind must not have a circuit breaker")
	// ErrInvalidPolicy reports a policy value outside its permitted range.
	ErrInvalidPolicy = errors.New("supervisor: invalid policy")
)

// Policy defaults. Each one is host policy. They reload without building the
// resource again, and none of them is the resource's business.
const (
	// DefaultCallTimeout bounds one call into a resource.
	DefaultCallTimeout = 10 * time.Second
	// DefaultStartTimeout bounds construction and Start.
	DefaultStartTimeout = 30 * time.Second
	// DefaultStopTimeout bounds Close before the instance is declared stopped
	// anyway. A resource that will not close must not hold up a shutdown.
	DefaultStopTimeout = 5 * time.Second
	// DefaultConcurrency is the bulkhead width: how many calls one instance may
	// have in flight before callers wait.
	DefaultConcurrency = 8
	// DefaultQueueDepth is the async queue depth. It bounds memory by item
	// count, so a stalled resource costs a fixed amount and not a growing one.
	DefaultQueueDepth = 1024

	// DefaultBackoffBase is the first rung of the restart ladder.
	DefaultBackoffBase = time.Second
	// DefaultBackoffCap is the last rung. It is longer than the one printers
	// get, because a dead endpoint does not need a handshake every minute for a
	// week.
	DefaultBackoffCap = 5 * time.Minute
	// DefaultStartSpread is the window the first attempt is spread over, so the
	// instances in a deployment do not all dial at the same instant.
	DefaultStartSpread = 2 * time.Second
	// DefaultReadyFor is how long an instance must serve before its restart
	// ladder resets. Resetting when Start returns nil spins against a remote
	// that accepts a connection and drops it at once.
	DefaultReadyFor = time.Minute

	// DefaultGiveUpAfter is how long an optional instance may be continuously
	// not-ready before it stops being retried.
	DefaultGiveUpAfter = time.Hour
	// DefaultMinAttempts is the floor under giving up. Time alone would latch an
	// instance that has been retried twice.
	DefaultMinAttempts = 5

	// DefaultBreakerWindow is the rolling window failures are counted over.
	DefaultBreakerWindow = 10 * time.Second
	// DefaultBreakerMinRequests is how many calls the window must hold before a
	// ratio means anything.
	DefaultBreakerMinRequests = 10
	// DefaultBreakerFailureRatio trips on a partial outage, which a count of
	// consecutive failures never sees.
	DefaultBreakerFailureRatio = 0.5
	// DefaultBreakerOpenFor is how long the breaker stays open before it probes.
	DefaultBreakerOpenFor = 30 * time.Second
	// DefaultBreakerProbes is how many probes half-open admits, and how many
	// must succeed to close.
	DefaultBreakerProbes = 1
	// DefaultBreakerProbeTimeout is how long a probe may be outstanding before
	// the breaker stops waiting for it. Without it, one call that never returns
	// holds the breaker half-open forever and every later call is refused.
	DefaultBreakerProbeTimeout = 30 * time.Second

	// DefaultStreamTimeout is how long a streaming span may stay open before
	// the host takes back its bulkhead slot. A caller that leaks a reader would
	// otherwise leak a slot, and enough leaked slots close the instance to
	// traffic with no failure anywhere to point at.
	DefaultStreamTimeout = time.Hour
)

// DropPolicy says which item a full queue loses. The zero value names nothing,
// so a queue whose author chose no policy is refused and not given a default.
// The drop policy has to be written down.
type DropPolicy uint8

const (
	// DropOldest loses the item that has waited longest and keeps the newest.
	// It suits state, where the newest fact is the one worth having.
	DropOldest DropPolicy = iota + 1
	// DropNewest refuses the offered item and keeps the backlog in order.
	DropNewest
)

// Valid reports whether d is one of the declared policies.
func (d DropPolicy) Valid() bool { return d == DropOldest || d == DropNewest }

// String returns the policy as it is rendered in logs and metric labels.
func (d DropPolicy) String() string {
	switch d {
	case DropOldest:
		return "drop-oldest"
	case DropNewest:
		return "drop-newest"
	}

	return "unset"
}

// QueuePolicy bounds an async instance's backlog.
type QueuePolicy struct {
	// Depth is the number of items held. Zero means the instance takes no
	// offered work and only serves synchronous calls.
	Depth int
	// Drop says which item is lost when the queue is full. A queue with no drop
	// policy is refused.
	Drop DropPolicy
}

// BackoffPolicy is the restart ladder: full jitter, bounded, and spread at the
// start so a deployment does not restart in step.
type BackoffPolicy struct {
	// Base is the first rung.
	Base time.Duration
	// Cap is the ceiling the ladder never exceeds.
	Cap time.Duration
	// Spread is the window the first attempt is spread over.
	Spread time.Duration
	// Rand returns a value in [0,1). It is a field so a test can pin the ladder
	// to its bounds instead of asserting a range.
	Rand func() float64
}

// Delay returns the wait before attempt n, counting the first retry as one. It
// uses full jitter, uniform over the whole interval up to the ceiling. Without
// jitter, every client that failed at the same moment retries together.
func (b BackoffPolicy) Delay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	ceiling := b.Base
	for range attempt - 1 {
		if ceiling >= b.Cap || ceiling > math.MaxInt64/2 {
			break
		}

		ceiling *= 2
	}

	ceiling = min(ceiling, b.Cap)
	if ceiling <= 0 {
		return 0
	}

	return time.Duration(b.random() * float64(ceiling))
}

// Scatter returns the wait before the first attempt.
func (b BackoffPolicy) Scatter() time.Duration {
	if b.Spread <= 0 {
		return 0
	}

	return time.Duration(b.random() * float64(b.Spread))
}

func (b BackoffPolicy) random() float64 {
	if b.Rand == nil {
		return rand.Float64() //nolint:gosec // jitter, not a secret.
	}

	return b.Rand()
}

// BreakerPolicy configures the circuit breaker. A nil *BreakerPolicy means the
// instance has none, which is right for a connection that spends much of its
// time disconnected.
type BreakerPolicy struct {
	// Window is the rolling window failures are counted over.
	Window time.Duration
	// MinRequests is how many calls the window must hold before the ratio is
	// consulted.
	MinRequests int
	// FailureRatio is the fraction of failures that trips the breaker.
	FailureRatio float64
	// OpenFor is how long open lasts before one probe is admitted.
	OpenFor time.Duration
	// Probes is how many calls half-open admits, and how many consecutive
	// successes close the breaker.
	Probes int
	// ProbeTimeout is how long a probe may be outstanding before the breaker
	// gives up waiting for it and reopens.
	ProbeTimeout time.Duration
}

// DefaultBreakerPolicy returns the breaker settings for a remote dependency
// many callers share: object storage, an identity provider, a webhook sink, a
// relay.
func DefaultBreakerPolicy() *BreakerPolicy {
	return &BreakerPolicy{
		Window:       DefaultBreakerWindow,
		MinRequests:  DefaultBreakerMinRequests,
		FailureRatio: DefaultBreakerFailureRatio,
		OpenFor:      DefaultBreakerOpenFor,
		Probes:       DefaultBreakerProbes,
		ProbeTimeout: DefaultBreakerProbeTimeout,
	}
}

// Validate reports whether the settings can produce a breaker that ever trips
// and ever recovers.
func (b *BreakerPolicy) Validate() error {
	switch {
	case b == nil:
		return nil
	case b.Window <= 0:
		return fmt.Errorf("%w: breaker window is %s", ErrInvalidPolicy, b.Window)
	case b.MinRequests < 1:
		return fmt.Errorf("%w: breaker min requests is %d", ErrInvalidPolicy, b.MinRequests)
	case b.FailureRatio <= 0 || b.FailureRatio > 1:
		return fmt.Errorf("%w: breaker failure ratio is %v", ErrInvalidPolicy, b.FailureRatio)
	case b.OpenFor <= 0:
		return fmt.Errorf("%w: breaker open period is %s", ErrInvalidPolicy, b.OpenFor)
	case b.Probes < 1:
		return fmt.Errorf("%w: breaker probes is %d", ErrInvalidPolicy, b.Probes)
	case b.ProbeTimeout <= 0:
		return fmt.Errorf("%w: breaker probe timeout is %s", ErrInvalidPolicy, b.ProbeTimeout)
	}

	return nil
}

// Policy is everything cross-cutting the host decides for an instance. None of
// it is a contract method, so no resource author implements any of it, and all
// of it changes without building the resource again.
type Policy struct {
	// CallTimeout bounds one call. It is applied only when the caller does not
	// already hold a tighter deadline.
	CallTimeout time.Duration
	// StartTimeout bounds construction and Start.
	StartTimeout time.Duration
	// StopTimeout bounds Close, and bounds how long a shutdown waits for a
	// resource that is not coming back.
	StopTimeout time.Duration
	// StreamTimeout bounds a streaming span whose caller never released it.
	StreamTimeout time.Duration
	// Concurrency is the bulkhead width. Zero is unbounded.
	Concurrency int
	// StreamConcurrency is how many streaming spans may be open at once. A
	// stream holds its slot for as long as a client takes, so a width below
	// Concurrency keeps slots that only calls can take. Zero sets no limit
	// beyond Concurrency.
	StreamConcurrency int
	// Queue bounds offered work for an async instance.
	Queue QueuePolicy
	// Breaker is nil for an instance that must not have one.
	Breaker *BreakerPolicy
	// Backoff is the restart ladder.
	Backoff BackoffPolicy
	// Required says whether the deployment is expected to work without this
	// instance. A required instance never gives up. An optional one does, so a
	// dashboard shows something an operator can act on.
	Required bool
	// GiveUpAfter is how long an optional instance may be continuously not-ready
	// before retries stop.
	GiveUpAfter time.Duration
	// MinAttempts is the floor under giving up.
	MinAttempts int
	// ReadyFor is how long an instance must serve before its ladder resets.
	ReadyFor time.Duration
	// FailureLimit is how many failures inside FailureWindow switch the
	// instance off until an operator acts. Zero never switches it off.
	FailureLimit int
	// FailureWindow is how far back failures count against FailureLimit, and
	// how far back the status reports them. Zero keeps none.
	FailureWindow time.Duration
}

// DefaultPolicy returns the host policy for a supervised remote dependency,
// with a breaker. Validate refuses it for ref.KindDriver, because this default
// does not suit every kind.
func DefaultPolicy() Policy {
	return Policy{
		CallTimeout:   DefaultCallTimeout,
		StartTimeout:  DefaultStartTimeout,
		StopTimeout:   DefaultStopTimeout,
		StreamTimeout: DefaultStreamTimeout,
		Concurrency:   DefaultConcurrency,
		Queue:         QueuePolicy{},
		Breaker:       DefaultBreakerPolicy(),
		Backoff: BackoffPolicy{
			Base:   DefaultBackoffBase,
			Cap:    DefaultBackoffCap,
			Spread: DefaultStartSpread,
		},
		Required:    false,
		GiveUpAfter: DefaultGiveUpAfter,
		MinAttempts: DefaultMinAttempts,
		ReadyFor:    DefaultReadyFor,
	}
}

// Validate reports whether the policy is coherent for kind.
//
// A breaker on a driver is refused and not dropped without a word. A driver has
// one caller, no fallback, and spends much of its time disconnected. A breaker
// there turns a partial outage into a complete one.
func (p Policy) Validate(kind ref.Kind) error {
	if p.Breaker != nil && kind == ref.KindDriver {
		return fmt.Errorf("%w: %s", ErrBreakerNotApplicable, kind)
	}

	if err := p.Breaker.Validate(); err != nil {
		return err
	}

	switch {
	case p.CallTimeout <= 0:
		return fmt.Errorf("%w: call timeout is %s", ErrInvalidPolicy, p.CallTimeout)
	case p.StartTimeout <= 0:
		return fmt.Errorf("%w: start timeout is %s", ErrInvalidPolicy, p.StartTimeout)
	case p.StopTimeout <= 0:
		return fmt.Errorf("%w: stop timeout is %s", ErrInvalidPolicy, p.StopTimeout)
	case p.StreamTimeout <= 0:
		return fmt.Errorf("%w: stream timeout is %s", ErrInvalidPolicy, p.StreamTimeout)
	case p.Concurrency < 0:
		return fmt.Errorf("%w: concurrency is %d", ErrInvalidPolicy, p.Concurrency)
	case p.StreamConcurrency < 0 || (p.Concurrency > 0 && p.StreamConcurrency > p.Concurrency):
		return fmt.Errorf("%w: stream concurrency is %d of %d", ErrInvalidPolicy, p.StreamConcurrency, p.Concurrency)
	case p.Queue.Depth < 0:
		return fmt.Errorf("%w: queue depth is %d", ErrInvalidPolicy, p.Queue.Depth)
	case p.Queue.Depth > 0 && !p.Queue.Drop.Valid():
		return fmt.Errorf("%w: a queue %d deep has no drop policy", ErrInvalidPolicy, p.Queue.Depth)
	case p.Backoff.Base <= 0 || p.Backoff.Cap < p.Backoff.Base:
		return fmt.Errorf("%w: backoff runs %s to %s", ErrInvalidPolicy, p.Backoff.Base, p.Backoff.Cap)
	case p.MinAttempts < 1:
		return fmt.Errorf("%w: min attempts is %d", ErrInvalidPolicy, p.MinAttempts)
	case p.FailureLimit < 0 || (p.FailureLimit > 0 && p.FailureWindow <= 0):
		return fmt.Errorf("%w: failure limit %d in %s", ErrInvalidPolicy, p.FailureLimit, p.FailureWindow)
	}

	return nil
}
