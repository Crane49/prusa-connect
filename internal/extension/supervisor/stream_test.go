package supervisor_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

func TestATransferOutlivesTheCallBudget(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		sink := newMetricSink()

		policy := testPolicy()
		policy.CallTimeout = 10 * time.Second

		h := newHarness(t, supervisor.Config[*target]{
			Ref:     testRef(t, "downloads"),
			Type:    "fake",
			Policy:  policy,
			Metrics: sink.binder(),
			Start:   starts(ok),
		})

		h.run(t)
		synctest.Wait()

		ctx, cancel := bubbleContext()
		defer cancel()

		// A large download is not a ten second operation. The budget bounds
		// the call that returns the reader, and nothing after it.
		const transfer = 4 * time.Minute

		opened := make(chan context.Context, 1)

		_, release, err := supervisor.Stream(ctx, h.inst, "open",
			func(openCtx context.Context, _ *target) (int, error) {
				opened <- openCtx

				return 1, nil
			})
		require.NoError(t, err)

		// The open context ends with the call that returned the value. Keeping
		// it alive would put the call budget on the transfer, and a driver
		// that kept using it would lose a large download at that mark.
		openCtx := <-opened
		require.ErrorIs(t, openCtx.Err(), context.Canceled)
		require.NotErrorIs(t, openCtx.Err(), context.DeadlineExceeded)

		time.Sleep(transfer)
		release(nil)

		observed := sink.observations(supervisor.MetricDuration + "{resource=downloads,op=open}")
		require.Len(t, observed, 1)
		assert.InDelta(t, transfer.Seconds(), observed[0], 0.001)
		assert.Equal(t, float64(1),
			sink.count(supervisor.MetricCalls+"{resource=downloads,op=open,outcome=success}"))

		h.stop()
	})
}

func TestAbandonedStreamGivesItsSlotBack(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		sink := newMetricSink()

		policy := testPolicy()
		policy.Concurrency = 1
		policy.StreamTimeout = time.Hour

		h := newHarness(t, supervisor.Config[*target]{
			Ref:     testRef(t, "leaked"),
			Type:    "fake",
			Policy:  policy,
			Metrics: sink.binder(),
			Start:   starts(ok),
		})

		h.run(t)
		synctest.Wait()

		ctx, cancel := bubbleContext()
		defer cancel()

		// The caller opens a reader and never closes it. With a bulkhead one
		// wide that closes the instance to traffic, with no failure anywhere
		// to point at.
		_, _, err := supervisor.Stream(ctx, h.inst, "open",
			func(context.Context, *target) (int, error) { return 1, nil })
		require.NoError(t, err)

		time.Sleep(policy.StreamTimeout + time.Second)
		synctest.Wait()

		assert.Equal(t, float64(1), sink.count(supervisor.MetricWedged+"{resource=leaked}"))
		require.NoError(t, call(ctx, h.inst, "stat"), "the slot came back")

		h.stop()
	})
}

func TestStreamsLeaveCallsTheSlotsTheyMayNotHold(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		policy := testPolicy()
		policy.Concurrency = 3
		policy.StreamConcurrency = 2

		h := newHarness(t, supervisor.Config[*target]{
			Ref:    testRef(t, "slow-writers"),
			Type:   "fake",
			Policy: policy,
			Start:  starts(ok),
		})

		h.run(t)
		synctest.Wait()

		ctx, cancel := bubbleContext()
		defer cancel()

		open := func(ctx context.Context) (func(error), error) {
			_, release, err := supervisor.Stream(ctx, h.inst, "save",
				func(context.Context, *target) (int, error) { return 1, nil })

			return release, err
		}

		first, err := open(ctx)
		require.NoError(t, err)

		_, err = open(ctx)
		require.NoError(t, err)

		// Both stream slots are held. A third stream waits for one, and a call
		// still finds the slot no stream may take.
		waiting, stopWaiting := context.WithTimeout(ctx, time.Second)
		defer stopWaiting()

		_, err = open(waiting)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.NoError(t, call(ctx, h.inst, "list"))

		first(nil)

		_, err = open(ctx)
		require.NoError(t, err, "a released stream gives its stream slot back")

		h.stop()
	})
}

func TestReleasingAStreamTwiceReleasesOneSlot(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		policy := testPolicy()
		policy.Concurrency = 1

		h := newHarness(t, supervisor.Config[*target]{
			Ref:    testRef(t, "double-close"),
			Type:   "fake",
			Policy: policy,
			Start:  starts(ok),
		})

		h.run(t)
		synctest.Wait()

		ctx, cancel := bubbleContext()
		defer cancel()

		_, release, err := supervisor.Stream(ctx, h.inst, "open",
			func(context.Context, *target) (int, error) { return 1, nil })
		require.NoError(t, err)

		// A reader can be closed twice, so the second release must do
		// nothing. The bulkhead is a buffered channel, and giving back a
		// permit that was never taken blocks the closer for good.
		release(nil)
		release(nil)

		require.NoError(t, call(ctx, h.inst, "stat"))

		h.stop()
	})
}

func TestHostTimeoutIsReportedEvenWhenTheCallerAlsoWentAway(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := bubbleContext()
		defer cancel()

		h := newHarness(t, supervisor.Config[*target]{
			Ref:    testRef(t, "late-cancel"),
			Type:   "fake",
			Policy: testPolicy(),
			Start: func(context.Context, supervisor.Handle) (*target, error) {
				return &target{do: func(callCtx context.Context) error {
					<-callCtx.Done()

					// The caller goes away at the moment the host's own budget
					// expires. From here on, asking the caller's context whose
					// deadline it was gets the wrong answer.
					cancel()

					return callCtx.Err()
				}}, nil
			},
		})

		h.run(t)
		synctest.Wait()

		err := call(ctx, h.inst, "stat")

		require.Error(t, err)
		assert.True(t, fault.HasCode(err, supervisor.CodeTimeout()),
			"the host's deadline expired, so the host says so, got %v", err)

		h.stop()
	})
}
