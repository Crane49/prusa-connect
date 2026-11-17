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

const testQueueDepth = 2

// stalledSink returns a harness whose resource blocks inside its first call
// until release is closed, and a channel that reports when it got there.
func stalledSink(t *testing.T, drop supervisor.DropPolicy, release <-chan struct{}) (*harness, chan struct{}) {
	t.Helper()

	entered := make(chan struct{}, 1)

	policy := testPolicy()
	policy.Queue = supervisor.QueuePolicy{Depth: testQueueDepth, Drop: drop}

	h := newHarness(t, supervisor.Config[*target]{
		Ref:    testRef(t, "slow-sink"),
		Type:   "fake",
		Policy: policy,
		Start: func(context.Context, supervisor.Handle) (*target, error) {
			return &target{do: func(ctx context.Context) error {
				select {
				case entered <- struct{}{}:
				default:
				}

				select {
				case <-release:
				case <-ctx.Done():
				}

				return nil
			}}, nil
		},
	})

	return h, entered
}

func TestSlowResourceDropsFromItsQueueAndCountsWhatItLost(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		h, entered := stalledSink(t, supervisor.DropOldest, release)

		h.run(t)
		synctest.Wait()
		require.Equal(t, "ready", h.state())

		noop := func(context.Context, *target) error { return nil }
		block := func(ctx context.Context, tgt *target) error { return tgt.do(ctx) }

		require.NoError(t, supervisor.Offer(h.inst, "deliver", block))
		<-entered // the drain goroutine is now stuck inside the resource

		const offered = 10

		started := time.Now()

		for range offered {
			require.NoError(t, supervisor.Offer(h.inst, "deliver", noop),
				"drop-oldest accepts the newest item")
		}

		assert.Zero(t, time.Since(started), "offering never waits on the resource")
		assert.Equal(t, int64(offered-testQueueDepth), h.inst.Dropped(),
			"everything past the queue depth is dropped and counted")

		close(release)
		h.stop()
	})
}

func TestDropNewestRefusesTheOfferedItem(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		h, entered := stalledSink(t, supervisor.DropNewest, release)

		h.run(t)
		synctest.Wait()

		noop := func(context.Context, *target) error { return nil }
		block := func(ctx context.Context, tgt *target) error { return tgt.do(ctx) }

		require.NoError(t, supervisor.Offer(h.inst, "deliver", block))
		<-entered

		for range testQueueDepth {
			require.NoError(t, supervisor.Offer(h.inst, "deliver", noop))
		}

		err := supervisor.Offer(h.inst, "deliver", noop)
		require.Error(t, err)
		assert.True(t, fault.HasCode(err, supervisor.CodeQueueFull()), "got %v", err)
		assert.Equal(t, int64(1), h.inst.Dropped())

		close(release)
		h.stop()
	})
}

func TestOfferWithoutAQueueIsRefused(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, supervisor.Config[*target]{
			Ref:    testRef(t, "sync-store"),
			Type:   "fake",
			Policy: testPolicy(), // no queue: the default is synchronous
			Start:  starts(ok),
		})

		h.run(t)
		synctest.Wait()

		err := supervisor.Offer(h.inst, "deliver", func(context.Context, *target) error { return nil })
		require.ErrorIs(t, err, supervisor.ErrNoQueue)

		h.stop()
	})
}
