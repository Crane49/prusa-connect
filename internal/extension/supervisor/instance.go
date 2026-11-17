// Package supervisor is one wrapper per running resource instance. It owns the
// queue, the drop policy, the per-call timeout, panic recovery, the circuit
// breaker, the state machine and the observability handles.
//
// The entry points are free functions and not methods, because a Go method may
// not introduce a type parameter, so the result type of a supervised call has
// to come from Call and not from inst.Call.
//
// # What the host cannot do
//
// A timeout is a deadline on a context, and a context only stops work that
// reads it. A resource whose method takes no context, or ignores the one it
// gets, blocks its caller for as long as it likes. The host bounds only what it
// waits for at shutdown, and limits the damage with the bulkhead. A contract
// with no context in its methods, such as eventsapi.Processor, is in this
// group.
//
// A recover only catches a panic on the goroutine that deferred it. A resource
// that panics on a goroutine it started itself takes the process down. Code
// that cannot be trusted that far runs in its own process.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/kernel/machinetext"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// Errors reported when an instance is constructed or used. Callers inspect them
// with errors.Is.
var (
	// ErrNoStart reports a config with no way to build the target.
	ErrNoStart = errors.New("supervisor: instance has no start function")
	// ErrNoRef reports a config with no name.
	ErrNoRef = errors.New("supervisor: instance has no ref")
	// ErrNoQueue reports work offered to an instance whose policy has no queue.
	ErrNoQueue = errors.New("supervisor: instance takes no offered work")
	// ErrAlreadyRunning reports a second Run against one instance.
	ErrAlreadyRunning = errors.New("supervisor: instance is already running")
)

// maxMessageLen caps a condition message. The status crosses the event bus to
// community sinks and to a federated parent, so a driver cannot make a stack
// trace into an event by putting it in an error string.
const maxMessageLen = 200

// The condition reasons this package writes. Every one is a CamelCase token a
// client can branch on.
const (
	reasonConfigAccepted  = "ConfigAccepted"
	reasonStarted         = "Started"
	reasonStartFailed     = "StartFailed"
	reasonStartTimeout    = "StartTimeout"
	reasonPanic           = "Panic"
	reasonStartLimitHit   = "StartLimitHit"
	reasonRuntimeFailure  = "RuntimeFailure"
	reasonReachable       = "Reachable"
	reasonBreakerOpen     = "BreakerOpen"
	reasonBreakerHalfOpen = "BreakerHalfOpen"
	reasonBreakerClosed   = "BreakerClosed"
	reasonWedged          = "Wedged"
)

// live is what a start produces and a stop discards: the target, plus the
// machinery bound to this one construction of it.
type live[T any] struct {
	target T
	// generation is the construction this target belongs to. Everything
	// reported about it is fenced on this value, so a goroutine that outlived
	// its Close cannot drive the state of its successor.
	generation int64
	breaker    *breaker
	sem        chan struct{}
	streams    chan struct{}
	queue      *queue[T]
	drained    chan struct{}
	// closing is what anything already holding this construction checks: the
	// drain between taking an item and running it, and an accessor between
	// loading the pointer and reading through it. Clearing cur stops the
	// pointer being given out. Only this stops it being used.
	closing atomic.Bool
}

// Config describes one supervised instance. Start and Stop are the whole
// lifecycle contract. The host derives every lifecycle state from their return
// values, so a resource cannot disagree with the host about its own lifecycle.
type Config[T any] struct {
	// Ref is the operator-chosen name.
	Ref ref.Ref
	// UID is the host-minted identity.
	UID ref.UID
	// Type is the registered type name this instance was built from.
	Type string
	// Policy is the host's cross-cutting settings.
	Policy Policy
	// Start constructs and starts the target. Every restart calls it again, so
	// an instance is built again and never reused. The Handle it receives
	// carries the logger, the metric handles and the status reporter for the
	// construction being built.
	Start func(ctx context.Context, h Handle) (T, error)
	// Stop closes the target. Nil means there is nothing to close.
	Stop func(ctx context.Context, target T) error
	// Logger is the base logger. The instance binds its Ref to it, so the
	// resource receives a logger that already carries its own identity and
	// imports nothing of ours to log.
	Logger *slog.Logger
	// Metrics binds metric handles to a Ref. Nil records nothing.
	Metrics Binder
	// OnStatus receives every status change. It is how instance state reaches
	// the event bus. Whichever goroutine caused the change calls it, so it must
	// not block.
	OnStatus func(Status)
}

// Instance is one running resource, wrapped. It owns the cross-cutting policy
// and the state machine. The resource inside it owns only its contract.
type Instance[T any] struct {
	instanceRef ref.Ref
	uid         ref.UID
	typeName    string

	start    func(context.Context, Handle) (T, error)
	stop     func(context.Context, T) error
	onStatus func(Status)

	log *slog.Logger
	met *instanceMetrics

	pol   atomic.Pointer[Policy]
	cur   atomic.Pointer[live[T]]
	state atomic.Pointer[condition.State]

	signals chan signal
	running atomic.Bool
	// hangEvidence is the unix nanoseconds of the last call that suggests the
	// target has stopped answering, zero when none has.
	hangEvidence atomic.Int64

	mu            sync.Mutex
	conditions    *condition.Set
	lifecycle     condition.Lifecycle
	generation    int64
	attempt       int
	result        Result
	nextRetryAt   time.Time
	notReadySince time.Time
	dropped       int64
	// stops is the failure window, oldest first.
	stops []Stop
}

// New returns an instance in pending, with its config accepted and nothing
// started. Run drives it from there.
func New[T any](cfg Config[T]) (*Instance[T], error) {
	if cfg.Ref.IsZero() {
		return nil, ErrNoRef
	}

	if cfg.Start == nil {
		return nil, fmt.Errorf("%w: %s", ErrNoStart, cfg.Ref)
	}

	if err := cfg.Policy.Validate(cfg.Ref.Kind()); err != nil {
		return nil, fmt.Errorf("policy for %s: %w", cfg.Ref, err)
	}

	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	metrics := NopMetrics()
	if cfg.Metrics != nil {
		metrics = cfg.Metrics(cfg.Ref)
	}

	i := &Instance[T]{
		instanceRef: cfg.Ref,
		uid:         cfg.UID,
		typeName:    cfg.Type,
		start:       cfg.Start,
		stop:        cfg.Stop,
		onStatus:    cfg.OnStatus,
		// Both keys are stable across every log line the resource and the host
		// write about this instance.
		log:           log.With("kind", cfg.Ref.Kind().String(), "resource", cfg.Ref.ID()),
		met:           newInstanceMetrics(metrics),
		signals:       make(chan signal, signalDepth),
		conditions:    &condition.Set{},
		result:        ResultSuccess,
		notReadySince: time.Now(),
	}

	i.pol.Store(&cfg.Policy)
	i.state.Store(new(condition.StatePending()))

	i.update(func() bool {
		return i.setConditionLocked(condition.TypeConfigured, condition.StatusTrue, reasonConfigAccepted, "")
	})

	return i, nil
}

// Ref returns the operator-chosen name, the only name this instance has on
// every surface.
func (i *Instance[T]) Ref() ref.Ref { return i.instanceRef }

// UID returns the host-minted identity.
func (i *Instance[T]) UID() ref.UID { return i.uid }

// Type returns the registered type name.
func (i *Instance[T]) Type() string { return i.typeName }

// Logger returns the logger bound to this instance's Ref. A resource receives
// it at construction, so it logs with its own identity attached and imports
// nothing of ours.
func (i *Instance[T]) Logger() *slog.Logger { return i.log }

// Metrics returns the metric handles bound to this instance's Ref, for a
// resource that counts something of its own.
func (i *Instance[T]) Metrics() Metrics { return i.met.base }

// State returns the derived state. It is a single atomic load, so the gate on
// every supervised call is free.
func (i *Instance[T]) State() condition.State { return *i.state.Load() }

// Serving reports whether calls may be routed to the instance.
func (i *Instance[T]) Serving() bool { return i.State().Serving() }

// Policy returns the current host policy.
func (i *Instance[T]) Policy() Policy { return *i.pol.Load() }

// SetPolicy replaces the host policy. Timeouts, breaker thresholds, bulkhead
// width and queue depth are host policy, so they change without building the
// resource again. The parts bound to a running target take effect at the next
// start.
func (i *Instance[T]) SetPolicy(p Policy) error {
	if err := p.Validate(i.instanceRef.Kind()); err != nil {
		return fmt.Errorf("policy for %s: %w", i.instanceRef, err)
	}

	i.pol.Store(&p)

	return nil
}

// Status returns the whole published status.
func (i *Instance[T]) Status() Status {
	i.mu.Lock()
	defer i.mu.Unlock()

	return i.statusLocked()
}

// Dropped returns how many offered items this instance's queue has lost,
// counting the queues of generations that have already been torn down.
func (i *Instance[T]) Dropped() int64 {
	i.mu.Lock()
	defer i.mu.Unlock()

	return i.droppedLocked()
}

// Restart asks the supervision loop to stop and start the instance. It also
// resets a failed instance: the attempt counter is cleared and the instance
// tries again, in one call, so an operator does not have to know a two-step
// recipe.
func (i *Instance[T]) Restart() { i.notify(signal{kind: signalRestart}) }

// Reporter returns a status reporter bound to the construction running now. A
// resource receives one at construction. This is for a host that wires its own.
func (i *Instance[T]) Reporter() Reporter {
	i.mu.Lock()
	defer i.mu.Unlock()

	return reporter[T]{inst: i, generation: i.generation}
}

// reporter is the resource-facing half of the status split. It carries the
// generation it was issued for. Without that, the only status API a resource
// has would also be the way around the staleness check.
type reporter[T any] struct {
	inst       *Instance[T]
	generation int64
}

// OK reports that the resource's own work is healthy.
func (r reporter[T]) OK() {
	r.inst.report(r.generation, func() bool {
		return r.inst.setConditionLocked(condition.TypeReachable, condition.StatusTrue, reasonReachable, "")
	}, signal{})
}

// Recoverable reports a failure retrying can fix. A supervised instance
// restarts on the backoff ladder.
func (r reporter[T]) Recoverable(err error) {
	r.inst.report(r.generation, func() bool {
		return r.inst.setConditionLocked(
			condition.TypeReachable, condition.StatusFalse, reasonRuntimeFailure, message(err))
	}, signal{kind: signalRecoverable, err: err, generation: r.generation})
}

// Permanent reports a failure retrying cannot fix. The instance stops serving
// immediately and stays failed until an operator acts, so the state is right
// whether or not a supervision loop is running.
func (r reporter[T]) Permanent(err error) {
	r.inst.report(r.generation, func() bool {
		// The lifecycle position is the host's, not the resource's. A driver
		// that reports a permanent failure while its own Close is running must
		// not overwrite stopping with active, or an operator restart publishes
		// failed on the way past.
		if r.inst.lifecycle == condition.LifecycleActive || r.inst.lifecycle == condition.LifecycleStarting {
			r.inst.lifecycle = condition.LifecycleActive
			r.inst.result = resultOf(err)
		}

		return r.inst.setConditionLocked(
			condition.TypeStarted, condition.StatusFalse, reasonOf(err), message(err))
	}, signal{kind: signalPermanent, err: err, generation: r.generation})
}

// report applies a status write on behalf of one construction, and discards it
// when that construction is gone.
func (i *Instance[T]) report(generation int64, mutate func() bool, notify signal) {
	applied := false

	i.update(func() bool {
		if generation != i.generation {
			return false
		}

		applied = true

		return mutate()
	})

	if !applied {
		i.met.stale.Add(1)
		i.log.Debug("discarded a status report from an older generation",
			"reported_generation", generation, "generation", i.currentGeneration())

		return
	}

	if notify.kind != signalNone {
		i.notify(notify)
	}
}

// currentGeneration returns the generation now running.
func (i *Instance[T]) currentGeneration() int64 {
	i.mu.Lock()
	defer i.mu.Unlock()

	return i.generation
}

// handle is what the resource being constructed receives.
func (i *Instance[T]) handle(generation int64) Handle {
	return Handle{
		Logger:  i.log,
		Metrics: i.met.base,
		Status:  reporter[T]{inst: i, generation: generation},
	}
}

// update runs one mutation under the lock, recomputes the derived state, and
// publishes the status outside the lock. A subscriber must not be able to
// deadlock the instance it is watching.
func (i *Instance[T]) update(mutate func() bool) {
	i.mu.Lock()
	changed := mutate()
	changed = i.recomputeLocked() || changed
	snapshot := i.statusLocked()
	i.mu.Unlock()

	if changed {
		i.emit(snapshot)
	}
}

// setConditionLocked writes one condition stamped with the current generation.
// A write that loses to a newer generation is dropped and logged, never
// returned. A status writer must not be able to fail a call.
func (i *Instance[T]) setConditionLocked(t condition.Type, status condition.Status, reason, text string) bool {
	changed, err := i.conditions.Set(time.Now(), condition.Condition{
		Type:               t,
		Status:             status,
		Reason:             reason,
		Message:            text,
		ObservedGeneration: i.generation,
	})
	if err != nil {
		i.log.Warn("dropped a status write", "condition", t.String(), "error", err.Error())

		return false
	}

	return changed
}

// recomputeLocked derives the state and reports whether it moved.
func (i *Instance[T]) recomputeLocked() bool {
	next := condition.Derive(i.generation, i.lifecycle, i.conditions)
	if previous := i.state.Load(); previous != nil && *previous == next {
		return false
	}

	i.state.Store(&next)
	i.met.states.Add(1)

	return true
}

func (i *Instance[T]) statusLocked() Status {
	return Status{
		Ref:         i.instanceRef,
		UID:         i.uid,
		Type:        i.typeName,
		State:       *i.state.Load(),
		Result:      i.result,
		Conditions:  i.conditions.All(),
		Generation:  i.generation,
		Attempt:     i.attempt,
		NextRetryAt: i.nextRetryAt,
		Dropped:     i.droppedLocked(),
		RecentStops: recentStops(i.stops, time.Now(), i.Policy().FailureWindow),

		HangEvidenceAt: i.hangEvidenceAt(),
	}
}

func (i *Instance[T]) hangEvidenceAt() time.Time {
	if n := i.hangEvidence.Load(); n != 0 {
		return time.Unix(0, n)
	}

	return time.Time{}
}

// droppedLocked totals the drops of every generation, so a restart does not
// reset the count an operator is watching.
func (i *Instance[T]) droppedLocked() int64 {
	total := i.dropped
	if lv := i.cur.Load(); lv != nil && lv.queue != nil {
		total += lv.queue.dropCount()
	}

	return total
}

func (i *Instance[T]) emit(s Status) {
	i.log.Info("resource state", "state", s.State.String(), "result", s.Result.String(), "generation", s.Generation)

	if i.onStatus != nil {
		i.onStatus(s)
	}
}

// Recovered turns a value from recover into a fault and takes the instance out
// of service. It is exported for an adapter that has to recover somewhere the
// host cannot reach, such as a Read on a reader a driver returned.
//
// It only ever sees a panic on a goroutine the host owns. A host in the same
// process cannot recover a panic on a goroutine the resource started itself.
func Recovered[T any](i *Instance[T], op string, rec any) error {
	err := i.recovered(op, rec)

	generation := i.currentGeneration()
	if lv := i.cur.Load(); lv != nil {
		generation = lv.generation
	}

	reporter[T]{inst: i, generation: generation}.Permanent(err)

	return err
}

// recovered turns a recovered panic into a fault. A panicking resource has a
// bug and is not having a network problem, so the fault is registered as never
// retryable and the classifier reads it as permanent.
func (i *Instance[T]) recovered(op string, rec any) error {
	i.met.panics.Add(1)
	i.log.Error("resource panicked",
		"op", op,
		"panic", fmt.Sprint(rec),
		"stack", string(debug.Stack()),
	)

	return panicked.New(fmt.Sprintf("%s panicked in %s", i.instanceRef, op)).
		With(fault.StringParam("resource", i.instanceRef.String()), fault.StringParam("op", op))
}

// message bounds an error string before it becomes a condition message that
// crosses the event bus.
//
// The cut uses machinetext.Clip and not a slice expression. An error string
// reaching here carries whatever a driver put in it, and much of what firmware
// writes is not English, so cutting at a byte offset can land inside a
// character and the JSON encoder then writes U+FFFD. Clip also adds a marker,
// so a subscriber knows the text was cut.
func message(err error) string {
	if err == nil {
		return ""
	}

	return machinetext.Clip(err.Error(), maxMessageLen)
}

// reasonOf maps a failure onto the CamelCase token a client branches on.
func reasonOf(err error) string {
	switch {
	case fault.HasCode(err, CodePanic()):
		return reasonPanic
	case fault.HasCode(err, CodeStartLimitHit()):
		return reasonStartLimitHit
	case errors.Is(err, context.DeadlineExceeded):
		return reasonStartTimeout
	}

	return reasonStartFailed
}

// resultOf maps a failure onto the result axis.
func resultOf(err error) Result {
	if stop := (*Stop)(nil); errors.As(err, &stop) {
		return stop.Result
	}

	switch {
	case fault.HasCode(err, CodePanic()):
		return ResultPanic
	case fault.HasCode(err, CodeStartLimitHit()):
		return ResultGiveUp
	case errors.Is(err, context.DeadlineExceeded):
		return ResultTimeout
	}

	return ResultStartError
}
