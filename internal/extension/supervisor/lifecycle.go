package supervisor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// signalDepth is the size of the supervision loop's mailbox. It is small,
// because every signal asks for one of the same few transitions and a backlog
// of them says nothing more than one does.
const signalDepth = 4

// signalKind is what a signal asks the supervision loop to do.
type signalKind uint8

const (
	// signalNone is the zero value, and asks for nothing.
	signalNone signalKind = iota
	// signalRestart is an operator action: stop, then start, with the attempt
	// counter cleared.
	signalRestart
	// signalRecoverable is a runtime failure worth retrying.
	signalRecoverable
	// signalPermanent is a runtime failure retrying cannot fix.
	signalPermanent
)

type signal struct {
	kind signalKind
	err  error
	// generation is the construction the signal was raised for. The loop drops
	// a signal from a construction it has already replaced.
	generation int64
}

// notify sends a signal to the supervision loop without blocking. Dropping one
// when the mailbox is full is safe. The conditions already carry the fact, and
// the loop is about to act on the same signal.
func (i *Instance[T]) notify(s signal) {
	select {
	case i.signals <- s:
	default:
	}
}

// Run drives the instance until ctx is cancelled: start, serve, and restart on
// the backoff ladder. It returns nil on cancellation, because a cancelled
// context is how supervision is meant to end.
//
// Everything it does comes from the return values of Start and Close. The
// resource reports runtime status only, so the host's view of the lifecycle and
// the resource's cannot disagree.
func (i *Instance[T]) Run(ctx context.Context) error {
	// Two loops against one instance would each close a target the other built,
	// leave the loser's drain goroutine running forever, and interleave two
	// generations onto one state machine.
	if !i.running.CompareAndSwap(false, true) {
		return fmt.Errorf("%w: %s", ErrAlreadyRunning, i.instanceRef)
	}

	defer i.running.Store(false)
	defer i.shutdown(ctx)

	// Spread the first attempt, so a deployment does not dial every endpoint
	// in the same millisecond.
	if !SleepFor(ctx, i.Policy().Backoff.Scatter()) {
		return nil
	}

	for {
		if err := i.startOnce(ctx); err != nil {
			if !i.afterFailedStart(ctx, err) {
				return nil
			}

			continue
		}

		if !i.serve(ctx) {
			return nil
		}
	}
}

// startOnce constructs and starts the target once.
func (i *Instance[T]) startOnce(ctx context.Context) error {
	pol := i.Policy()

	i.beginAttempt()
	i.met.starts.Add(1)

	startCtx, cancel := context.WithTimeout(ctx, pol.StartTimeout)
	defer cancel()

	generation := i.currentGeneration()

	target, err := i.callStart(startCtx, i.handle(generation))
	if err != nil {
		return err
	}

	i.publish(ctx, pol, generation, target)

	return nil
}

// callStart recovers a panic in the resource's own constructor, which is where
// a driver that cannot parse its config most often dies.
func (i *Instance[T]) callStart(ctx context.Context, h Handle) (target T, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = i.recovered("start", rec)
		}
	}()

	return i.start(ctx, h)
}

// beginAttempt moves the instance into starting and raises the generation, so
// every condition written by the older generation reads as stale from here on.
func (i *Instance[T]) beginAttempt() {
	i.update(func() bool {
		i.generation++
		i.attempt++
		i.lifecycle = condition.LifecycleStarting
		i.nextRetryAt = time.Time{}

		if i.notReadySince.IsZero() {
			i.notReadySince = time.Now()
		}

		return i.setConditionLocked(condition.TypeConfigured, condition.StatusTrue, reasonConfigAccepted, "")
	})
}

// publish makes the started target callable and moves the instance into ready.
func (i *Instance[T]) publish(ctx context.Context, pol Policy, generation int64, target T) {
	lv := &live[T]{target: target, generation: generation}

	if pol.Breaker != nil {
		lv.breaker = newBreaker(*pol.Breaker)
	}

	if pol.Concurrency > 0 {
		lv.sem = make(chan struct{}, pol.Concurrency)
	}

	if pol.StreamConcurrency > 0 {
		lv.streams = make(chan struct{}, pol.StreamConcurrency)
	}

	if pol.Queue.Depth > 0 {
		lv.queue = newQueue[T](pol.Queue)
		lv.drained = make(chan struct{})

		go i.drain(ctx, lv)
	}

	i.cur.Store(lv)

	i.update(func() bool {
		i.lifecycle = condition.LifecycleActive
		i.result = ResultSuccess

		changed := i.setConditionLocked(condition.TypeStarted, condition.StatusTrue, reasonStarted, "")

		// An instance refused for a missing dependency and started once the
		// dependency arrived keeps the condition, now true. Without this the
		// status would still say the dependency is missing while the instance
		// serves.
		changed = i.clearDependencyLocked() || changed

		// The condition is absent entirely on an instance with no breaker, so a
		// reader can tell "closed" from "there is no breaker here".
		if lv.breaker != nil {
			closed := i.setConditionLocked(
				condition.TypeBreakerClosed, condition.StatusTrue, reasonBreakerClosed, "")
			changed = closed || changed
		}

		return changed
	})
}

// serve waits while the instance is running. It returns false when ctx ended.
func (i *Instance[T]) serve(ctx context.Context) bool {
	stability := time.NewTimer(i.Policy().ReadyFor)
	defer stability.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-stability.C:
			// The instance stayed up long enough to count as healthy.
			// Resetting the ladder on a start that merely returned nil spins
			// against a remote that accepts a connection and drops it at once.
			i.clearLadder()
		case s := <-i.signals:
			if s.generation != 0 && s.generation != i.currentGeneration() {
				// Raised by a construction this loop has already replaced.
				i.met.stale.Add(1)

				continue
			}

			return i.act(ctx, s)
		}
	}
}

// act carries out one signal, returning false when ctx ended.
func (i *Instance[T]) act(ctx context.Context, s signal) bool {
	switch s.kind {
	case signalRestart:
		// An operator restart goes through stopping and stopped, never
		// through failed. A restart is not a failure, and reporting it as
		// one would teach operators to ignore real failures.
		i.stopTarget(ctx, true, ResultStoppedByOperator)
		i.clearLadder()

		return true
	case signalPermanent:
		i.stopTarget(ctx, false, resultOf(s.err))

		return i.waitForRestart(ctx)
	case signalRecoverable:
		result := ResultRuntimeError
		if stop := (*Stop)(nil); errors.As(s.err, &stop) {
			result = stop.Result
		}

		i.stopTarget(ctx, false, result)

		if i.recordFailure(s.err, result) {
			return i.switchOff(ctx, s.err)
		}

		return i.scheduleRetry(ctx, s.err, result)
	case signalNone:
	}

	return true
}

// afterFailedStart decides what a failed start means and waits out the
// consequence. It returns false when ctx ended.
func (i *Instance[T]) afterFailedStart(ctx context.Context, err error) bool {
	i.log.WarnContext(ctx, "resource failed to start",
		"attempt", i.Status().Attempt,
		"class", Classify(err).String(),
		"error", err.Error(),
	)

	// An instance waiting for a dependency is not failing. Its database or the
	// module it requires is often still starting beside it, so the refusal
	// climbs the ladder and fills no switch-off window.
	limitReached := !waitingOnDependency(err) && i.recordFailure(err, resultOf(err))

	// A permanent failure latches anyway, and keeps the result that says why.
	if Classify(err) == ClassPermanent {
		i.markFailed(condition.LifecycleActive, err, resultOf(err))

		return i.waitForRestart(ctx)
	}

	if limitReached {
		return i.switchOff(ctx, err)
	}

	if i.shouldGiveUp() {
		limit := startLimitHit.New("retries exhausted").
			With(
				fault.StringParam("resource", i.instanceRef.String()),
				fault.IntParam("attempts", int64(i.Status().Attempt)),
			).
			Wrap(err)

		i.markFailed(condition.LifecycleGaveUp, limit, ResultGiveUp)

		return i.waitForRestart(ctx)
	}

	return i.scheduleRetry(ctx, err, resultOf(err))
}

// switchOff latches an instance that failed too often inside the failure
// window.
func (i *Instance[T]) switchOff(ctx context.Context, err error) bool {
	i.markFailed(condition.LifecycleGaveUp, err, ResultSwitchedOffByHost)

	return i.waitForRestart(ctx)
}

// recordFailure adds a failure to the window and reports whether the window
// has reached the policy's limit. A window with no limit still keeps its
// failures, because the status reports them. An operator restart neither adds
// to the window nor clears it, so restarting by hand cannot hide a crash loop;
// only a new Instance starts with an empty window.
func (i *Instance[T]) recordFailure(err error, result Result) bool {
	pol := i.Policy()
	if pol.FailureWindow <= 0 {
		return false
	}

	stop := Stop{Result: result}
	if s := (*Stop)(nil); errors.As(err, &s) {
		stop = *s
	}

	now := time.Now()
	if stop.At.IsZero() {
		stop.At = now
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	i.stops = append(recentStops(i.stops, now, pol.FailureWindow), stop)

	return pol.FailureLimit > 0 && len(i.stops) >= pol.FailureLimit
}

// recentStops returns the stops no older than window, in a new slice.
func recentStops(stops []Stop, now time.Time, window time.Duration) []Stop {
	var recent []Stop

	for _, s := range stops {
		if now.Sub(s.At) <= window {
			recent = append(recent, s)
		}
	}

	return recent
}

// markFailed latches the instance until an operator acts.
func (i *Instance[T]) markFailed(lc condition.Lifecycle, err error, result Result) {
	i.update(func() bool {
		i.lifecycle = lc
		i.result = result
		i.nextRetryAt = time.Time{}

		started := i.setConditionLocked(condition.TypeStarted, condition.StatusFalse, reasonOf(err), message(err))

		return i.setDependencyLocked(err) || started
	})
}

// scheduleRetry puts the instance into retrying and waits out the ladder rung.
func (i *Instance[T]) scheduleRetry(ctx context.Context, err error, result Result) bool {
	delay := i.Policy().Backoff.Delay(i.Status().Attempt)

	i.update(func() bool {
		i.lifecycle = condition.LifecycleRetrying
		i.result = result
		i.nextRetryAt = time.Now().Add(delay)

		started := i.setConditionLocked(condition.TypeStarted, condition.StatusFalse, reasonOf(err), message(err))

		return i.setDependencyLocked(err) || started
	})

	return i.waitRetry(ctx, delay)
}

// waitRetry waits for the ladder rung, an operator restart, or cancellation.
func (i *Instance[T]) waitRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			return true
		case s := <-i.signals:
			if s.kind == signalRestart {
				i.clearLadder()

				return true
			}
		}
	}
}

// waitForRestart holds a latched instance until somebody asks for it back.
func (i *Instance[T]) waitForRestart(ctx context.Context) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case s := <-i.signals:
			if s.kind == signalRestart {
				i.clearLadder()

				return true
			}
		}
	}
}

// shouldGiveUp reports whether the ladder has run out. A required instance
// never gives up. An optional one latches, so a dead endpoint becomes one
// failed instance an operator can act on.
//
// The bound is a time plus a floor on attempts, and never a count alone. Five
// attempts means something different at every rung of a backoff ladder.
func (i *Instance[T]) shouldGiveUp() bool {
	pol := i.Policy()
	if pol.Required || pol.GiveUpAfter <= 0 {
		return false
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	return i.attempt >= pol.MinAttempts &&
		!i.notReadySince.IsZero() &&
		time.Since(i.notReadySince) >= pol.GiveUpAfter
}

// clearLadder resets the attempt counter and the give-up clock. An operator
// action must not count against the limit that latched the instance.
func (i *Instance[T]) clearLadder() {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.attempt = 0
	i.notReadySince = time.Time{}
}

// stopTarget tears down the running target. announce is false for a failure,
// because a failed instance that then reported stopping would lose the only
// field saying why it went down.
func (i *Instance[T]) stopTarget(ctx context.Context, announce bool, result Result) {
	if announce {
		i.update(func() bool {
			i.lifecycle = condition.LifecycleStopping

			return false
		})
	}

	clean := true

	// Refuse before unpublishing, so the flag covers the whole teardown for
	// anything that already holds this target.
	if held := i.cur.Load(); held != nil {
		held.closing.Store(true)
	}

	lv := i.cur.Swap(nil)
	if lv != nil {
		clean = i.closeQueue(lv)
		clean = i.callStop(ctx, lv.target) && clean
	}

	// A teardown the resource did not return from is not a clean stop,
	// whatever the caller meant to call it.
	if !clean {
		result = ResultTimeout
	}

	i.update(func() bool {
		if announce {
			i.lifecycle = condition.LifecycleStopped
		}

		i.result = result
		i.notReadySince = time.Now()

		return false
	})
}

// closeQueue stops the drain goroutine and folds its drop count into the
// instance's, so a restart does not reset a number an operator is watching.
//
// The wait is bounded. The drain runs third-party code that may not return, and
// an unbounded wait here would make the process impossible to shut down. The
// stuck goroutine is abandoned, counted and named in the log instead.
func (i *Instance[T]) closeQueue(lv *live[T]) bool {
	if lv.queue == nil {
		return true
	}

	lv.queue.close()

	if !i.waitBounded(lv.drained, "drain") {
		return false
	}

	lv.queue.discard()

	i.mu.Lock()
	defer i.mu.Unlock()

	i.dropped += lv.queue.dropCount()

	return true
}

// callStop closes the target within its budget, recovering a panic. The budget
// comes from a context that outlives cancellation, so a shutdown still gets a
// chance to flush.
//
// Close runs on its own goroutine, so the budget is real. A resource that
// ignores its context would otherwise hold the whole process open, and one
// abandoned goroutine is the cheaper failure.
func (i *Instance[T]) callStop(ctx context.Context, target T) bool {
	if i.stop == nil {
		return true
	}

	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), i.Policy().StopTimeout)
	closed := make(chan struct{})

	go func() {
		defer cancel()
		defer close(closed)

		defer func() {
			if rec := recover(); rec != nil {
				_ = i.recovered("stop", rec)
			}
		}()

		if err := i.stop(stopCtx, target); err != nil {
			i.log.ErrorContext(stopCtx, "resource failed to close", "error", err.Error())
		}
	}()

	return i.waitBounded(closed, "close")
}

// waitBounded waits for done, reporting false when the budget expired first and
// the goroutine behind it was abandoned.
func (i *Instance[T]) waitBounded(done <-chan struct{}, what string) bool {
	timer := time.NewTimer(i.Policy().StopTimeout)
	defer timer.Stop()

	select {
	case <-done:
		return true
	case <-timer.C:
		i.wedged(what)

		return false
	}
}

// wedged records a resource that did not return inside a budget the host cannot
// enforce on it. The condition makes the instance read as degraded instead of
// healthy, and the counter is what an operator alerts on.
func (i *Instance[T]) wedged(what string) {
	i.met.wedged.Add(1)
	i.log.Error("resource did not return within its budget and was abandoned",
		"op", what, "budget", i.Policy().StopTimeout.String())

	i.update(func() bool {
		i.result = ResultTimeout

		return i.setConditionLocked(condition.TypeReachable, condition.StatusFalse, reasonWedged,
			what+" did not return within its budget")
	})
}

// shutdown runs when supervision ends.
func (i *Instance[T]) shutdown(ctx context.Context) {
	i.stopTarget(ctx, true, ResultSuccess)
}

// SleepFor waits out d, reporting false when ctx ended first. It is exported
// because every ladder waits the same way. The printer dial loop in the
// composition root walks BackoffPolicy too, and a second copy of this would be
// a second place to get cancellation wrong.
func SleepFor(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
