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

// mode is what a controlled resource does with the next call.
type mode int32

const (
	modeFail mode = iota
	modeSucceed
	// modeWedge ignores the context it is given, the way a resource that
	// never reads its deadline does. Only the test frees it.
	modeWedge
)

var errRefused = errors.New("the endpoint is refusing connections")

// controlled is a resource whose behaviour the test sets call by call, and which
// can hold a call in flight across a breaker state change.
type controlled struct {
	mode    atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func newControlled(m mode) *controlled {
	c := &controlled{entered: make(chan struct{}, 1), release: make(chan struct{})}
	c.mode.Store(int32(m))

	return c
}

func (c *controlled) set(m mode) { c.mode.Store(int32(m)) }

func (c *controlled) do(_ context.Context) error {
	switch mode(c.mode.Load()) {
	case modeSucceed:
		return nil
	case modeWedge:
		select {
		case c.entered <- struct{}{}:
		default:
		}

		<-c.release

		return errRefused
	case modeFail:
	}

	return errRefused
}

func breakerHarness(t *testing.T, name string, pol supervisor.BreakerPolicy, res *controlled) *harness {
	t.Helper()

	policy := testPolicy()
	policy.Breaker = &pol

	return newHarness(t, supervisor.Config[*target]{
		Ref:    testRef(t, name),
		Type:   "fake",
		Policy: policy,
		Start:  starts(res.do),
	})
}

// trip drives the breaker open with two failing calls.
func trip(t *testing.T, ctx context.Context, h *harness) { //nolint:revive // ctx follows t in a test helper.
	t.Helper()

	require.Error(t, call(ctx, h.inst, "stat"))
	require.Error(t, call(ctx, h.inst, "stat"))
	require.Equal(t, "degraded", h.state(), "two failures out of two should have opened the breaker")
}

func TestBreakerRearmsWhenAProbeNeverReturns(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		pol := supervisor.BreakerPolicy{
			Window:       10 * time.Second,
			MinRequests:  2,
			FailureRatio: 0.5,
			OpenFor:      30 * time.Second,
			Probes:       1,
			ProbeTimeout: 20 * time.Second,
		}

		resource := newControlled(modeFail)
		h := breakerHarness(t, "wedged-probe", pol, resource)

		h.run(t)
		synctest.Wait()
		require.Equal(t, "ready", h.state())

		ctx, cancel := bubbleContext()
		defer cancel()

		trip(t, ctx, h)

		// The probe that half-open admits never returns, the way a resource
		// that ignores its deadline behaves.
		resource.set(modeWedge)
		time.Sleep(pol.OpenFor)

		probe := make(chan struct{})

		go func() {
			defer close(probe)

			_ = call(ctx, h.inst, "stat")
		}()

		<-resource.entered
		synctest.Wait()

		assert.Equal(t, "BreakerHalfOpen", reasonOfCondition(h.inst.Status(), condition.TypeBreakerClosed),
			"half-open is announced as itself, not as open with a contradicting message")

		// Without a probe deadline the breaker stays half-open with its only
		// slot taken, and every later call is refused although the resource
		// has recovered.
		resource.set(modeSucceed)
		time.Sleep(pol.ProbeTimeout)

		err := call(ctx, h.inst, "stat")
		require.Error(t, err, "the wedged probe is given up on, which reopens the breaker")
		require.True(t, fault.HasCode(err, supervisor.CodeBreakerOpen()), "got %v", err)

		time.Sleep(pol.OpenFor)

		require.NoError(t, call(ctx, h.inst, "stat"),
			"the breaker re-armed and admitted a fresh probe")
		assert.Equal(t, "ready", h.state())

		close(resource.release)
		<-probe
		h.stop()
	})
}

func TestStragglerFromAnEarlierEpochDoesNotMoveTheBreaker(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		// Two probes, so one success leaves the breaker half-open and the
		// late call arrives in the state that would count it.
		pol := supervisor.BreakerPolicy{
			Window:       time.Minute,
			MinRequests:  2,
			FailureRatio: 0.5,
			OpenFor:      30 * time.Second,
			Probes:       2,
			ProbeTimeout: time.Minute,
		}

		resource := newControlled(modeWedge)
		h := breakerHarness(t, "straggler", pol, resource)

		h.run(t)
		synctest.Wait()

		ctx, cancel := bubbleContext()
		defer cancel()

		// One call is admitted while the breaker is closed and stays in flight.
		straggler := make(chan error, 1)

		go func() { straggler <- call(ctx, h.inst, "stat") }()

		<-resource.entered

		// Two other calls open the breaker underneath it.
		resource.set(modeFail)
		trip(t, ctx, h)

		// The cooldown elapses and the first probe succeeds, leaving the breaker
		// half-open with one success banked.
		resource.set(modeSucceed)
		time.Sleep(pol.OpenFor)
		require.NoError(t, call(ctx, h.inst, "stat"))
		require.Equal(t, "degraded", h.state(), "one of two probes does not close it")

		// Now the late call fails. It was admitted two states ago, so it never
		// took a probe slot, and its outcome describes a breaker state that no
		// longer exists.
		close(resource.release)
		require.Error(t, <-straggler)

		synctest.Wait()

		err := call(ctx, h.inst, "stat")

		require.NoError(t, err, "a straggler must not re-open the breaker")
		assert.Equal(t, "ready", h.state(), "the second probe closed it")

		h.stop()
	})
}

func TestOutcomeArrivingAfterItsWindowStartsAFreshOne(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		pol := supervisor.BreakerPolicy{
			Window:       10 * time.Second,
			MinRequests:  2,
			FailureRatio: 1,
			OpenFor:      30 * time.Second,
			Probes:       1,
			ProbeTimeout: time.Minute,
		}

		resource := newControlled(modeWedge)
		h := breakerHarness(t, "window", pol, resource)

		h.run(t)
		synctest.Wait()

		ctx, cancel := bubbleContext()
		defer cancel()

		slow := make(chan error, 1)

		go func() { slow <- call(ctx, h.inst, "stat") }()

		<-resource.entered

		// One failure inside the window; two are needed to trip.
		resource.set(modeFail)
		require.Error(t, call(ctx, h.inst, "stat"))
		require.Equal(t, "ready", h.state())

		// The window expires while the first call is still out.
		time.Sleep(pol.Window + time.Second)

		close(resource.release)
		require.Error(t, <-slow)

		synctest.Wait()

		assert.Equal(t, "ready", h.state(),
			"an outcome that outlived its window opens a new one rather than being counted "+
				"against a failure eleven seconds older than it")

		h.stop()
	})
}

func TestHalfOpenRequiresEveryProbeToSucceed(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		const probes = 3

		pol := supervisor.BreakerPolicy{
			Window:       10 * time.Second,
			MinRequests:  2,
			FailureRatio: 0.5,
			OpenFor:      30 * time.Second,
			Probes:       probes,
			ProbeTimeout: time.Minute,
		}

		resource := newControlled(modeFail)
		h := breakerHarness(t, "probes", pol, resource)

		h.run(t)
		synctest.Wait()

		ctx, cancel := bubbleContext()
		defer cancel()

		trip(t, ctx, h)

		resource.set(modeSucceed)
		time.Sleep(pol.OpenFor)

		// Every probe but the last leaves the breaker half-open. A breaker
		// that closed on the first success would ignore the rest, and an
		// operator would not get the setting they chose.
		for probe := range probes {
			require.NoError(t, call(ctx, h.inst, "stat"))

			if probe < probes-1 {
				assert.Equal(t, "degraded", h.state(),
					"probe %d of %d closed the breaker early", probe+1, probes)
			}
		}

		assert.Equal(t, "ready", h.state(), "the last probe closes it")

		h.stop()
	})
}

func TestBreakerRefusalRecordsNoLatency(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		sink := newMetricSink()

		pol := supervisor.BreakerPolicy{
			Window:       10 * time.Second,
			MinRequests:  2,
			FailureRatio: 0.5,
			OpenFor:      30 * time.Second,
			Probes:       1,
			ProbeTimeout: time.Minute,
		}

		policy := testPolicy()
		policy.Breaker = &pol

		h := newHarness(t, supervisor.Config[*target]{
			Ref:     testRef(t, "latency"),
			Type:    "fake",
			Policy:  policy,
			Metrics: sink.binder(),
			Start: starts(func(context.Context) error {
				time.Sleep(2 * time.Second)

				return errRefused
			}),
		})

		h.run(t)
		synctest.Wait()

		ctx, cancel := bubbleContext()
		defer cancel()

		trip(t, ctx, h)

		key := supervisor.MetricDuration + "{resource=latency,op=stat}"
		observed := len(sink.observations(key))

		require.Error(t, call(ctx, h.inst, "stat"))
		require.Error(t, call(ctx, h.inst, "stat"))

		assert.Len(t, sink.observations(key), observed,
			"a refusal spent no time in the resource, and recording it as a fast call is how a "+
				"latency panel looks healthiest at the moment it is most wrong")
		assert.Equal(t, float64(2), sink.count(supervisor.MetricRejected+"{resource=latency,op=stat}"))

		h.stop()
	})
}

func reasonOfCondition(s supervisor.Status, t condition.Type) string {
	for _, c := range s.Conditions {
		if c.Type == t {
			return c.Reason
		}
	}

	return ""
}
