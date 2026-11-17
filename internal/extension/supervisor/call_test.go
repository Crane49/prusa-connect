package supervisor_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

func TestCallRefusedWhileNotServing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, supervisor.Config[*target]{
			Ref:    testRef(t, "pending-store"),
			Type:   "fake",
			Policy: testPolicy(),
			Start:  starts(ok),
		})

		ctx, cancel := bubbleContext()
		defer cancel()

		// Never run, so it never started. A call must not reach a target that
		// does not exist.
		err := call(ctx, h.inst, "stat")

		require.Error(t, err)
		assert.True(t, fault.HasCode(err, supervisor.CodeNotReady()), "got %v", err)
	})
}

func TestPerCallTimeoutIsTheHostsAndNotTheResources(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, supervisor.Config[*target]{
			Ref:    testRef(t, "slow-store"),
			Type:   "fake",
			Policy: testPolicy(),
			Start: func(context.Context, supervisor.Handle) (*target, error) {
				return &target{do: func(ctx context.Context) error {
					<-ctx.Done()

					return ctx.Err()
				}}, nil
			},
		})

		h.run(t)
		synctest.Wait()
		require.Equal(t, "ready", h.state())

		ctx, cancel := bubbleContext()
		defer cancel()

		started := time.Now()
		err := call(ctx, h.inst, "stat")

		require.Error(t, err)
		assert.True(t, fault.HasCode(err, supervisor.CodeTimeout()), "got %v", err)
		assert.Equal(t, supervisor.DefaultCallTimeout, time.Since(started))

		h.stop()
	})
}

func TestCallerDeadlineWinsWhenItIsTighter(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, supervisor.Config[*target]{
			Ref:    testRef(t, "tight-store"),
			Type:   "fake",
			Policy: testPolicy(),
			Start: func(context.Context, supervisor.Handle) (*target, error) {
				return &target{do: func(ctx context.Context) error {
					<-ctx.Done()

					return ctx.Err()
				}}, nil
			},
		})

		h.run(t)
		synctest.Wait()

		parent, cancelParent := bubbleContext()
		defer cancelParent()

		ctx, cancel := context.WithTimeout(parent, time.Second)
		defer cancel()

		started := time.Now()
		err := call(ctx, h.inst, "stat")

		require.Error(t, err)
		assert.Equal(t, time.Second, time.Since(started), "the host must not extend a caller's deadline")
		assert.False(t, fault.HasCode(err, supervisor.CodeTimeout()),
			"the host's budget did not expire, so it must not claim it did")

		h.stop()
	})
}

func TestBreakerOpensHalfOpensAndCloses(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		policy := testPolicy()
		policy.Breaker = &supervisor.BreakerPolicy{
			Window:       10 * time.Second,
			MinRequests:  2,
			FailureRatio: 0.5,
			OpenFor:      30 * time.Second,
			Probes:       1,
			ProbeTimeout: 30 * time.Second,
		}

		var (
			calls   atomic.Int64
			healthy atomic.Bool
		)

		h := newHarness(t, supervisor.Config[*target]{
			Ref:    testRef(t, "breaker-store"),
			Type:   "fake",
			Policy: policy,
			Start: func(context.Context, supervisor.Handle) (*target, error) {
				return &target{do: func(context.Context) error {
					calls.Add(1)
					if healthy.Load() {
						return nil
					}

					return errors.New("the endpoint is refusing connections")
				}}, nil
			},
		})

		h.run(t)
		synctest.Wait()
		require.Equal(t, "ready", h.state())

		ctx, cancel := bubbleContext()
		defer cancel()

		require.Error(t, call(ctx, h.inst, "stat"))
		require.Error(t, call(ctx, h.inst, "list"))

		assert.Equal(t, "degraded", h.state(), "an open breaker is a serving instance with an abnormal condition")
		assert.Equal(t, condition.StatusFalse, statusOf(h.inst.Status(), condition.TypeBreakerClosed))

		reached := calls.Load()
		err := call(ctx, h.inst, "stat")
		require.Error(t, err)
		assert.True(t, fault.HasCode(err, supervisor.CodeBreakerOpen()), "got %v", err)
		assert.Equal(t, reached, calls.Load(),
			"one breaker is shared by every operation, or each caller rediscovers the outage")

		// Still open one tick before the cooldown ends.
		time.Sleep(policy.Breaker.OpenFor - time.Millisecond)
		require.True(t, fault.HasCode(call(ctx, h.inst, "stat"), supervisor.CodeBreakerOpen()))

		healthy.Store(true)
		time.Sleep(policy.Breaker.OpenFor)

		require.NoError(t, call(ctx, h.inst, "stat"), "half-open admits a probe")

		assert.Equal(t, "ready", h.state(), "one successful probe closes the breaker")
		assert.Equal(t, condition.StatusTrue, statusOf(h.inst.Status(), condition.TypeBreakerClosed))

		h.stop()
	})
}

func TestPanicDegradesTheInstanceAndNothingElse(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		bad := newHarness(t, supervisor.Config[*target]{
			Ref:    testRef(t, "panicking-store"),
			Type:   "fake",
			Policy: testPolicy(),
			Start: func(context.Context, supervisor.Handle) (*target, error) {
				return &target{do: func(context.Context) error {
					panic("index out of range in somebody else's driver")
				}}, nil
			},
		})

		good := newHarness(t, supervisor.Config[*target]{
			Ref:    testRef(t, "healthy-store"),
			Type:   "fake",
			Policy: testPolicy(),
			Start:  starts(ok),
		})

		bad.run(t)
		good.run(t)
		synctest.Wait()

		ctx, cancel := bubbleContext()
		defer cancel()

		err := call(ctx, bad.inst, "stat")

		require.Error(t, err, "the panic reaches the caller as an error, not as a crash")
		assert.True(t, fault.HasCode(err, supervisor.CodePanic()), "got %v", err)

		synctest.Wait()

		assert.Equal(t, "failed", bad.state(), "a panicking resource takes itself out of service")
		assert.Equal(t, supervisor.ResultPanic, bad.inst.Status().Result)
		assert.False(t, bad.inst.Serving())

		assert.Equal(t, "ready", good.state(), "and only itself")
		require.NoError(t, call(ctx, good.inst, "stat"))

		bad.stop()
		good.stop()
	})
}

func TestPanicIsRedactedOnTheWayToAClient(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, supervisor.Config[*target]{
			Ref:    testRef(t, "leaky-store"),
			Type:   "fake",
			Policy: testPolicy(),
			Start: func(context.Context, supervisor.Handle) (*target, error) {
				return &target{do: func(context.Context) error {
					panic("connection string postgres://user:hunter2@db/prod")
				}}, nil
			},
		})

		h.run(t)
		synctest.Wait()

		ctx, cancel := bubbleContext()
		defer cancel()

		err := call(ctx, h.inst, "stat")
		require.Error(t, err)

		var carried *fault.Fault
		require.ErrorAs(t, err, &carried)
		assert.True(t, carried.Redacted(), "a panic's detail never reaches a client")

		h.stop()
	})
}
