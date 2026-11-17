package supervisor_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
)

var errZombie = errors.New("the session this goroutine holds was revoked")

func TestReportFromAnOlderGenerationIsDiscarded(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		sink := newMetricSink()

		h := newHarness(t, supervisor.Config[*target]{
			Ref:     testRef(t, "zombie"),
			Type:    "fake",
			Policy:  testPolicy(),
			Metrics: sink.binder(),
			Start:   starts(ok),
		})

		h.run(t)
		synctest.Wait()
		require.Equal(t, "ready", h.state())

		// A driver goroutine that outlived its own Close still has the
		// reporter it was given. Everything it says from here on describes a
		// target that no longer exists.
		zombie := h.inst.Reporter()

		h.inst.Restart()
		synctest.Wait()
		require.Equal(t, "ready", h.state())
		require.Equal(t, int64(2), h.inst.Status().Generation)

		before := h.seen()

		zombie.Permanent(errZombie)
		zombie.Recoverable(errZombie)
		zombie.OK()

		synctest.Wait()

		assert.Equal(t, "ready", h.state(),
			"a dead generation must not drive the state of its healthy successor")
		assert.Equal(t, before, h.seen(), "and must not publish anything either")
		assert.Equal(t, float64(3), sink.count(supervisor.MetricStale+"{resource=zombie}"),
			"what was discarded is counted, so a driver leaking goroutines is visible")

		h.stop()
	})
}

func TestReportFromTheLiveGenerationIsApplied(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, supervisor.Config[*target]{
			Ref:    testRef(t, "live-report"),
			Type:   "fake",
			Policy: testPolicy(),
			Start:  starts(ok),
		})

		h.run(t)
		synctest.Wait()

		// The fence must not be so tight that the reporter a running resource
		// has becomes useless.
		h.inst.Reporter().Permanent(errZombie)
		synctest.Wait()

		assert.Equal(t, "failed", h.state())
		assert.Equal(t, supervisor.ResultStartError, h.inst.Status().Result)

		h.stop()
	})
}

func TestPermanentReportDuringCloseDoesNotFlapThroughFailed(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		closing := make(chan struct{})
		release := make(chan struct{})

		var inst *supervisor.Instance[*target]

		h := newHarness(t, supervisor.Config[*target]{
			Ref:    testRef(t, "closing-report"),
			Type:   "fake",
			Policy: testPolicy(),
			Start:  starts(ok),
			Stop: func(context.Context, *target) error {
				// Anything holding a session reports its loss while it is being
				// closed. That is the ordinary shape, not an exotic one.
				select {
				case closing <- struct{}{}:
				default:
				}

				<-release
				inst.Reporter().Permanent(errZombie)

				return nil
			},
		})

		inst = h.inst

		h.run(t)
		synctest.Wait()
		require.Equal(t, "ready", h.state())

		h.inst.Restart()
		<-closing

		close(release)
		synctest.Wait()

		require.Equal(t, "ready", h.state())
		assert.NotContains(t, h.seen(), "failed",
			"an operator restart must not publish failed on the way past")

		// A status is published per condition change, so one state can appear
		// twice. This case asserts on the order.
		assert.Equal(t,
			[]string{"pending", "starting", "ready", "stopping", "stopped", "starting", "ready"},
			slices.Compact(h.seen()))

		h.stop()
	})
}

func TestRepeatedHealthyReportsPublishOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, supervisor.Config[*target]{
			Ref:    testRef(t, "chatty"),
			Type:   "fake",
			Policy: testPolicy(),
			Start:  starts(ok),
		})

		h.run(t)
		synctest.Wait()

		reporter := h.inst.Reporter()
		reporter.OK()
		synctest.Wait()

		published := len(h.seen())

		// A resource that reports on a timer publishes the same healthy
		// status again and again. Dropping the repeats here is a few lines in
		// one place, instead of a rule in every driver's documentation.
		for range 100 {
			reporter.OK()
		}

		synctest.Wait()

		assert.Len(t, h.seen(), published)
		assert.Equal(t, condition.StatusTrue, statusOf(h.inst.Status(), condition.TypeReachable))

		h.stop()
	})
}

func TestShutdownIsBoundedWhenTheResourceDoesNotReturn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		wedged := make(chan struct{})
		defer close(wedged)

		policy := testPolicy()
		policy.StopTimeout = 5 * time.Second

		h := newHarness(t, supervisor.Config[*target]{
			Ref:    testRef(t, "wedged-close"),
			Type:   "fake",
			Policy: policy,
			Start:  starts(ok),
			Stop: func(context.Context, *target) error {
				// A Close that ignores its context. On a Pi with no console, a
				// process that cannot be shut down because of one of these is a
				// reflash.
				<-wedged

				return nil
			},
		})

		h.run(t)
		synctest.Wait()
		require.Equal(t, "ready", h.state())

		started := time.Now()
		h.stop()

		assert.Equal(t, policy.StopTimeout, time.Since(started),
			"supervision ends on the budget, not on the resource")
		assert.Equal(t, supervisor.ResultTimeout, h.inst.Status().Result)
		assert.Contains(t, reasonsOfAll(h.inst.Status()), "Wedged")
	})
}

func TestDrainStopsDeliveringWhenItsTargetIsTornDown(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		const queued = 2

		entered := make(chan struct{}, 1)
		release := make(chan struct{})
		delivered := make(chan struct{}, 16)

		policy := testPolicy()
		policy.Queue = supervisor.QueuePolicy{Depth: 8, Drop: supervisor.DropOldest}

		h := newHarness(t, supervisor.Config[*target]{
			Ref:    testRef(t, "torn-down"),
			Type:   "fake",
			Policy: policy,
			Start:  starts(ok),
		})

		h.run(t)
		synctest.Wait()

		deliver := func(ctx context.Context, _ *target) error {
			select {
			case entered <- struct{}{}:
			default:
			}

			select {
			case <-release:
			case <-ctx.Done():
			}

			delivered <- struct{}{}

			return nil
		}

		require.NoError(t, supervisor.Offer(h.inst, "deliver", deliver))
		<-entered

		for range queued {
			require.NoError(t, supervisor.Offer(h.inst, "deliver", deliver))
		}

		// A runtime failure tears the target down while the instance is still
		// serving, so the state gate alone would let the backlog through to a
		// target that is being closed.
		h.inst.Reporter().Recoverable(errZombie)

		// Wait for the teardown to reach the point where it is blocked on the
		// drain. Releasing before that races the supervision loop, and a test
		// that wins a race today loses it another day.
		synctest.Wait()

		close(release)
		synctest.Wait()

		assert.Len(t, delivered, 1, "only the item already in flight was delivered")
		assert.Equal(t, int64(queued), h.inst.Dropped(),
			"the backlog a teardown abandons is counted, not silently swallowed")

		h.stop()
	})
}

func reasonsOfAll(s supervisor.Status) []string {
	out := make([]string, 0, len(s.Conditions))
	for _, c := range s.Conditions {
		out = append(out, c.Reason)
	}

	slices.Sort(out)

	return out
}
