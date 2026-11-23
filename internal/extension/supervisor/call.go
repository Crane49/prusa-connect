package supervisor

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// Call runs one operation against the supervised resource and returns its
// result. It is a free function because a Go method may not introduce a type
// parameter, so R has nowhere else to come from.
//
// Everything cross-cutting happens here: the state gate, the circuit breaker,
// the bulkhead, the per-call deadline, panic recovery, the outcome
// classification and the metrics.
func Call[T, R any](
	ctx context.Context, i *Instance[T], op string, fn func(context.Context, T) (R, error),
) (R, error) {
	return CallWithin(ctx, i, op, 0, fn)
}

// CallWithin is Call with a budget of its own in place of the policy's
// CallTimeout, for a call whose work grows with what it was handed, such as a
// request whose body is a file of a gigabyte. A budget shorter than
// CallTimeout is CallTimeout, so no caller can make a call tighter this way.
func CallWithin[T, R any](
	ctx context.Context, i *Instance[T], op string, budget time.Duration,
	fn func(context.Context, T) (R, error),
) (R, error) {
	var out R

	err := callVoidWithin(ctx, i, op, budget, func(callCtx context.Context, target T) error {
		value, callErr := fn(callCtx, target)
		if callErr != nil {
			return callErr
		}

		out = value

		return nil
	})

	return out, err
}

// CallVoid runs one operation with no result. It exists because R cannot be
// inferred from nothing, and an error-only contract method is the common case.
func CallVoid[T any](ctx context.Context, i *Instance[T], op string, fn func(context.Context, T) error) error {
	return callVoidWithin(ctx, i, op, 0, fn)
}

// callVoidWithin is CallVoid under the longer of budget and CallTimeout.
func callVoidWithin[T any](
	ctx context.Context, i *Instance[T], op string, budget time.Duration, fn func(context.Context, T) error,
) error {
	lv, err := i.acquire()
	if err != nil {
		return err
	}

	sp, callCtx, err := i.begin(ctx, lv, op, max(budget, i.Policy().CallTimeout))
	if err != nil {
		return err
	}

	return sp.finish(i.invoke(callCtx, lv, op, fn))
}

// Stream runs an operation whose result keeps working after the call returns: a
// reader, a writer, an iterator. The returned release function ends the
// supervised span, and the caller must call it exactly once, at Close, Commit
// or the end of the sequence.
//
// Wrapping only the call that returns a reader measures the wrong thing, and it
// releases the bulkhead slot while the transfer is still running.
//
// The host budget bounds the call that returns the value, and nothing after it.
// A large download is not a ten second operation, and a deadline covering the
// transfer would cancel every big one. The caller bounds the transfer, together
// with StreamTimeout, which takes the slot back from a caller who never
// released it.
func Stream[T, R any](
	ctx context.Context, i *Instance[T], op string, fn func(context.Context, T) (R, error),
) (R, func(error), error) {
	var out R

	lv, err := i.acquire()
	if err != nil {
		return out, nil, err
	}

	// The stream slot is taken before the bulkhead slot, so a stream waiting
	// for one holds nothing a call could use.
	if lv.streams != nil {
		select {
		case lv.streams <- struct{}{}:
		case <-ctx.Done():
			i.met.op(op).reject()

			return out, nil, ctx.Err()
		}
	}

	sp, callCtx, err := i.begin(ctx, lv, op, i.Policy().CallTimeout)
	if err != nil {
		if lv.streams != nil {
			<-lv.streams
		}

		return out, nil, err
	}

	sp.stream = true

	err = i.invoke(callCtx, lv, op, func(openCtx context.Context, target T) error {
		value, callErr := fn(openCtx, target)
		if callErr != nil {
			return callErr
		}

		out = value

		return nil
	})
	if err != nil {
		return out, nil, sp.finish(err)
	}

	sp.handOff(i.Policy().StreamTimeout)

	return out, func(transferErr error) { _ = sp.finish(transferErr) }, nil
}

// Offer puts work on the instance's queue without waiting for it. It is the
// entry point for a contract whose caller must not block. The drain goroutine
// runs the same policy stack, so the only difference between a synchronous
// resource and an asynchronous one is where the work waits.
//
// A full queue drops according to the instance's policy and counts what it
// lost. It never blocks the caller, and it is safe to call while another
// goroutine is stopping the instance.
func Offer[T any](i *Instance[T], op string, fn func(context.Context, T) error) error {
	lv, err := i.acquire()
	if err != nil {
		return err
	}

	if lv.queue == nil {
		return fmt.Errorf("%w: %s", ErrNoQueue, i.instanceRef)
	}

	before := lv.queue.dropCount()
	accepted := lv.queue.offer(job[T]{op: op, fn: fn})

	if lost := lv.queue.dropCount() - before; lost > 0 {
		i.met.dropped.Add(float64(lost))
	}

	if !accepted {
		return queueFull.New(fmt.Sprintf("%s dropped %s: queue full", i.instanceRef, op)).
			With(fault.StringParam("resource", i.instanceRef.String()), fault.StringParam("op", op))
	}

	return nil
}

// Peek reads a value straight off the running target, with panic recovery and
// nothing else. It is for the accessors a contract requires to be cheap and to
// do no I/O. Routing those through the policy stack would add cost to a call
// the file browser makes once per row. Peek reports false when the instance is
// not running.
func Peek[T, R any](i *Instance[T], read func(T) R) (value R, ok bool) {
	lv := i.cur.Load()
	if lv == nil || lv.closing.Load() {
		return value, false
	}

	defer func() {
		if rec := recover(); rec != nil {
			// An accessor that panics means a broken driver, so it goes out
			// of service instead of panicking once per row of a file
			// listing.
			_ = Recovered(i, "peek", rec)

			var zero R

			value, ok = zero, false
		}
	}()

	return read(lv.target), true
}

// acquire returns the running target, or the reason there is none. A failed
// instance is never called.
func (i *Instance[T]) acquire() (*live[T], error) {
	lv := i.cur.Load()
	state := i.State()

	if lv == nil || lv.closing.Load() || !state.Serving() {
		return nil, notReady.New(fmt.Sprintf("%s is %s", i.instanceRef, state)).
			With(
				fault.StringParam("resource", i.instanceRef.String()),
				fault.StringParam("state", state.String()),
			)
	}

	return lv, nil
}

// span is one supervised call in flight. It holds what was taken and must be
// given back, and what has to be recorded when the call ends.
type span[T any] struct {
	inst *Instance[T]
	live *live[T]
	op   string

	cancel  context.CancelFunc
	granted admission
	// stream says the span also holds a slot of live.streams.
	stream  bool
	started time.Time
	// budget is the deadline the host imposed, as a duration, for the error
	// that names it.
	budget time.Duration
	// hostDeadline is when the budget the host imposed expires, and zero when
	// it imposed none. Comparing against it tells the host's timeout from the
	// caller's, without keeping the caller's context alive to ask it.
	hostDeadline time.Time

	// watchdog is written after the timer that reads it already exists, so it
	// is the one field on this struct that two goroutines touch.
	watchdog atomic.Pointer[time.Timer]
	finished atomic.Bool
}

// begin takes the breaker's permission, the bulkhead slot and the deadline, and
// returns the context the call runs under.
func (i *Instance[T]) begin(
	ctx context.Context, lv *live[T], op string, budget time.Duration,
) (*span[T], context.Context, error) {
	var granted admission

	if lv.breaker != nil {
		permitted, allowed, moved := lv.breaker.allow()
		i.announce(moved)

		if !allowed {
			i.met.op(op).reject()

			return nil, nil, breakerOpen.New(fmt.Sprintf("%s refused %s: breaker open", i.instanceRef, op)).
				With(fault.StringParam("resource", i.instanceRef.String()), fault.StringParam("op", op))
		}

		granted = permitted
	}

	// The bulkhead is taken after the breaker check, so an open breaker cannot
	// consume a slot that a working call could have had.
	if lv.sem != nil {
		// A free slot is taken before the context is looked at: with both ready,
		// select would pick at random and an expired caller would read as a
		// full bulkhead.
		select {
		case lv.sem <- struct{}{}:
		default:
			select {
			case lv.sem <- struct{}{}:
			case <-ctx.Done():
				i.met.op(op).reject()

				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					i.hangEvidence.Store(time.Now().UnixNano())
				}

				return nil, nil, ctx.Err()
			}
		}
	}

	callCtx, cancel, deadline := deadlineFor(ctx, budget)

	return &span[T]{
		inst:         i,
		live:         lv,
		op:           op,
		cancel:       cancel,
		granted:      granted,
		started:      time.Now(),
		budget:       budget,
		hostDeadline: deadline,
	}, callCtx, nil
}

// handOff ends the deadline phase of a span whose value outlives the call, and
// arms the watchdog that takes the bulkhead slot back if the caller never
// releases it.
func (sp *span[T]) handOff(within time.Duration) {
	sp.cancel()

	sp.cancel = func() {}
	sp.hostDeadline = time.Time{}

	if within <= 0 {
		return
	}

	sp.watchdog.Store(time.AfterFunc(within, func() {
		if sp.finished.Load() {
			return
		}

		sp.inst.met.wedged.Add(1)
		sp.inst.log.Error("a streaming span was never released and its slot was taken back",
			"op", sp.op, "budget", within.String())

		_ = sp.finish(errStreamAbandoned)
	}))
}

// finish records the outcome, releases what begin took, and returns the error
// the caller should see. Calling it twice is safe, because a reader can be
// closed twice and releasing a bulkhead slot twice is worse than not releasing
// it.
func (sp *span[T]) finish(err error) error {
	if !sp.finished.CompareAndSwap(false, true) {
		return err
	}

	if timer := sp.watchdog.Load(); timer != nil {
		timer.Stop()
	}

	elapsed := time.Since(sp.started)

	if sp.timedOut(err) {
		sp.inst.hangEvidence.Store(time.Now().UnixNano())
		err = sp.inst.timedOut(sp.op, sp.budget, err)
	}

	sp.cancel()

	if sp.live.sem != nil {
		<-sp.live.sem
	}

	if sp.stream && sp.live.streams != nil {
		<-sp.live.streams
	}

	class := Classify(err)
	sp.inst.met.op(sp.op).observe(class, elapsed.Seconds())

	if sp.live.breaker != nil {
		sp.inst.announce(sp.live.breaker.record(sp.granted, class.Failure()))
	}

	return err
}

// timedOut reports whether the deadline that expired was the host's. A caller
// with a tighter deadline of its own gets its own error back, unchanged.
func (sp *span[T]) timedOut(err error) bool {
	if sp.hostDeadline.IsZero() || !errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	return !time.Now().Before(sp.hostDeadline)
}

// invoke is the goroutine boundary a panic must not cross. A resource that
// panics takes only itself out of service. The process, the other instances and
// the caller all survive.
func (i *Instance[T]) invoke(
	ctx context.Context, lv *live[T], op string, run func(context.Context, T) error,
) (err error) {
	defer func() {
		rec := recover()
		if rec == nil {
			return
		}

		err = i.recovered(op, rec)
		reporter[T]{inst: i, generation: lv.generation}.Permanent(err)
	}()

	return run(ctx, lv.target)
}

// deadlineFor applies the host's budget only when the caller does not already
// have a tighter one. Deriving a context every time is the most expensive line
// in this stack and usually changes nothing.
//
// The deadline stops the host waiting. It does not stop the resource. A method
// that takes no context, or ignores the one it takes, runs to the end whether
// or not the budget expired. See the package doc.
func deadlineFor(
	ctx context.Context, budget time.Duration,
) (context.Context, context.CancelFunc, time.Time) {
	if budget <= 0 {
		return ctx, func() {}, time.Time{}
	}

	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= budget {
		return ctx, func() {}, time.Time{}
	}

	imposed := time.Now().Add(budget)
	callCtx, cancel := context.WithDeadline(ctx, imposed)

	return callCtx, cancel, imposed
}

// timedOut names the budget the host imposed, so an operator reading the error
// knows whose deadline expired.
func (i *Instance[T]) timedOut(op string, budget time.Duration, cause error) error {
	return callTimeout.New(fmt.Sprintf("%s timed out in %s after %s", i.instanceRef, op, budget)).
		Wrap(cause).
		With(
			fault.StringParam("resource", i.instanceRef.String()),
			fault.StringParam("op", op),
			fault.IntParam("timeout_ms", budget.Milliseconds()),
		)
}

// announce writes a breaker transition into the conditions, so an open breaker
// shows up as degraded instead of as a stream of failures with no explanation.
func (i *Instance[T]) announce(moved transition) {
	if !moved.moved {
		return
	}

	i.met.breaker.Add(1)

	status, reason := condition.StatusFalse, reasonBreakerOpen

	switch moved.to {
	case BreakerClosed:
		status, reason = condition.StatusTrue, reasonBreakerClosed
	case BreakerHalfOpen:
		reason = reasonBreakerHalfOpen
	case BreakerOpen:
	}

	i.update(func() bool {
		return i.setConditionLocked(condition.TypeBreakerClosed, status, reason, "breaker "+moved.to.String())
	})
}

// drain runs offered work on one goroutine per instance. It uses the same
// policy stack as Call, so an asynchronous resource gets the breaker, the
// deadline and the panic recovery a synchronous one gets.
func (i *Instance[T]) drain(ctx context.Context, lv *live[T]) {
	defer close(lv.drained)

	for {
		work, ok := lv.queue.next()
		if !ok {
			return
		}

		if lv.closing.Load() || !i.Serving() {
			lv.queue.dropped.Add(1)

			continue
		}

		sp, callCtx, err := i.begin(ctx, lv, work.op, i.Policy().CallTimeout)
		if err == nil {
			err = sp.finish(i.invoke(callCtx, lv, work.op, work.fn))
		}

		if err != nil {
			i.log.WarnContext(ctx, "offered work failed", "op", work.op, "error", err.Error())
		}
	}
}
