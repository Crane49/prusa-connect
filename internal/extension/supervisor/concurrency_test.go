package supervisor_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
)

// These tests run outside a synctest bubble. They are about two goroutines
// reaching one instance at the same instant, not about time, and a bubble that
// parks every goroutine but one cannot find a race. They are bounded by an
// iteration count and never by sleeping, and they are meant to be run under
// -race.

const (
	racers     = 8
	restarts   = 40
	teardowns  = 300
	readyLimit = 10 * time.Second
	readyTick  = time.Millisecond
)

// errTeardown stands in for a driver reporting that its connection dropped.
var errTeardown = errors.New("connection lost")

// live starts an instance and waits for it to serve.
func liveInstance(t *testing.T, cfg supervisor.Config[*target]) (*supervisor.Instance[*target], func()) {
	t.Helper()

	inst, err := supervisor.New(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		_ = inst.Run(ctx)
	}()

	require.Eventually(t, inst.Serving, readyLimit, readyTick, "instance never reached a serving state")

	return inst, func() {
		cancel()
		<-done
	}
}

// TestInstanceSurvivesATeardownStorm drives offers from several goroutines
// against an instance that is being torn down and restarted underneath them.
// It gives -race something to work on and does not catch one defect on its own.
// The window for a send on a closed channel is very short through this path,
// and queue_internal_test.go holds the test that closes it every time.
func TestInstanceSurvivesATeardownStorm(t *testing.T) {
	policy := testPolicy()
	policy.Queue = supervisor.QueuePolicy{Depth: 4, Drop: supervisor.DropOldest}
	policy.Backoff.Base = time.Microsecond
	policy.Backoff.Cap = 10 * time.Microsecond
	policy.ReadyFor = time.Microsecond

	inst, stop := liveInstance(t, supervisor.Config[*target]{
		Ref:    testRef(t, "offer-race"),
		Type:   "fake",
		Policy: policy,
		Start:  starts(ok),
		Stop:   func(context.Context, *target) error { return nil },
	})
	defer stop()

	// The teardown runs through a recoverable report as well as a restart,
	// because the two take different paths. A restart moves the instance into
	// stopping before it touches the queue. A runtime failure leaves it
	// serving right up to the moment the queue goes away.
	var (
		wg      sync.WaitGroup
		stopped = make(chan struct{})
	)

	for range racers {
		wg.Go(func() {
			for {
				select {
				case <-stopped:
					return
				default:
				}

				_ = supervisor.Offer(inst, "deliver", func(context.Context, *target) error { return nil })
			}
		})
	}

	for range teardowns {
		inst.Reporter().Recoverable(errTeardown)
		inst.Restart()
	}

	close(stopped)
	wg.Wait()

	require.Eventually(t, inst.Serving, readyLimit, readyTick)
}

func TestCallsRacingARestartKeepTheirBulkheadSlots(t *testing.T) {
	policy := testPolicy()
	policy.Concurrency = 2
	policy.Backoff.Base = time.Microsecond
	policy.Backoff.Cap = time.Millisecond

	inst, stop := liveInstance(t, supervisor.Config[*target]{
		Ref:    testRef(t, "call-race"),
		Type:   "fake",
		Policy: policy,
		Start:  starts(ok),
		Stop:   func(context.Context, *target) error { return nil },
	})
	defer stop()

	var (
		wg      sync.WaitGroup
		stopped = make(chan struct{})
	)

	for range racers {
		wg.Go(func() {
			for {
				select {
				case <-stopped:
					return
				default:
				}

				_ = call(t.Context(), inst, "stat")
			}
		})
	}

	for range restarts {
		inst.Restart()
	}

	close(stopped)
	wg.Wait()

	// A slot that was never given back shows up here. With a bulkhead two
	// wide, a call that cannot get a permit blocks until its own deadline and
	// never succeeds again. The per-call deadline turns that into a failure
	// instead of a hang.
	require.Eventually(t, func() bool {
		callCtx, callCancel := context.WithTimeout(t.Context(), time.Second)
		defer callCancel()

		return call(callCtx, inst, "stat") == nil
	}, readyLimit, readyTick, "calls never started succeeding again")
}

func TestSecondRunIsRefused(t *testing.T) {
	inst, stop := liveInstance(t, supervisor.Config[*target]{
		Ref:    testRef(t, "single-run"),
		Type:   "fake",
		Policy: testPolicy(),
		Start:  starts(ok),
	})
	defer stop()

	// Two loops would each close a target the other built, and the losing
	// loop's drain would keep running.
	err := inst.Run(t.Context())

	require.ErrorIs(t, err, supervisor.ErrAlreadyRunning)
	assert.True(t, inst.Serving(), "the loop that was already running is untouched")
}

func TestConcurrentStatusReadersSeeAConsistentStatus(t *testing.T) {
	inst, stop := liveInstance(t, supervisor.Config[*target]{
		Ref:    testRef(t, "status-race"),
		Type:   "fake",
		Policy: testPolicy(),
		Start:  starts(ok),
	})
	defer stop()

	var (
		wg      sync.WaitGroup
		stopped = make(chan struct{})
	)

	for range racers {
		wg.Go(func() {
			for {
				select {
				case <-stopped:
					return
				default:
				}

				status := inst.Status()
				if status.State.String() != "" {
					_ = status.Conditions
				}
			}
		})
	}

	for range restarts {
		inst.Restart()
		inst.Reporter().OK()
	}

	close(stopped)
	wg.Wait()
}
