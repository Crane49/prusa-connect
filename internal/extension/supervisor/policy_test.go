package supervisor_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

func TestBreakerIsRefusedForADriver(t *testing.T) {
	t.Parallel()

	driver, err := ref.New(ref.KindDriver, "mk4-01")
	require.NoError(t, err)

	// A printer spends much of its time disconnected, it has one caller and no
	// fallback, so a breaker there turns a partial outage into a complete one.
	// A driver gets backoff instead, and the plane says the instance has no
	// breaker rather than pretending it has one.
	_, err = supervisor.New(supervisor.Config[*target]{
		Ref:    driver,
		Type:   "xbuddy",
		Policy: supervisor.DefaultPolicy(),
		Start: func(context.Context, supervisor.Handle) (*target, error) {
			return &target{}, nil
		},
	})

	require.ErrorIs(t, err, supervisor.ErrBreakerNotApplicable)
}

func TestADriverRunsWithoutABreakerCondition(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		driver, err := ref.New(ref.KindDriver, "mk4-01")
		require.NoError(t, err)

		policy := testPolicy()
		policy.Breaker = nil

		h := newHarness(t, supervisor.Config[*target]{
			Ref:    driver,
			Type:   "xbuddy",
			Policy: policy,
			Start:  starts(ok),
		})

		h.run(t)
		synctest.Wait()

		require.Equal(t, "ready", h.state())

		for _, c := range h.inst.Status().Conditions {
			assert.NotEqual(t, condition.TypeBreakerClosed, c.Type,
				"an instance with no breaker reports no breaker condition")
		}

		h.stop()
	})
}

func TestBackoffDelayIsFullJitterBoundedByTheCap(t *testing.T) {
	t.Parallel()

	ladder := supervisor.BackoffPolicy{
		Base: time.Second,
		Cap:  5 * time.Minute,
		Rand: fixedRand(),
	}

	cases := []struct {
		name    string
		attempt int
		want    time.Duration
	}{
		{name: "before the first attempt", attempt: 0, want: time.Second},
		{name: "first rung", attempt: 1, want: time.Second},
		{name: "second rung", attempt: 2, want: 2 * time.Second},
		{name: "fifth rung", attempt: 5, want: 16 * time.Second},
		{name: "at the cap", attempt: 10, want: 5 * time.Minute},
		{name: "past the cap", attempt: 200, want: 5 * time.Minute},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, ladder.Delay(testCase.attempt))
		})
	}
}

func TestBackoffDrawsOverTheWholeInterval(t *testing.T) {
	t.Parallel()

	// Full jitter is uniform over [0, ceiling). Without jitter, every client
	// that failed at the same moment retries together.
	floor := supervisor.BackoffPolicy{Base: time.Second, Cap: time.Minute, Rand: func() float64 { return 0 }}
	assert.Equal(t, time.Duration(0), floor.Delay(4))

	middle := supervisor.BackoffPolicy{Base: time.Second, Cap: time.Minute, Rand: func() float64 { return 0.5 }}
	assert.Equal(t, 4*time.Second, middle.Delay(4))
}

func TestPolicyRejectsSettingsThatCannotWork(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		build func(supervisor.Policy) supervisor.Policy
	}{
		{
			name:  "no call timeout",
			build: func(p supervisor.Policy) supervisor.Policy { p.CallTimeout = 0; return p },
		},
		{
			name:  "a backoff cap under its base",
			build: func(p supervisor.Policy) supervisor.Policy { p.Backoff.Cap = time.Millisecond; return p },
		},
		{
			name: "a breaker that never trips",
			build: func(p supervisor.Policy) supervisor.Policy {
				p.Breaker = &supervisor.BreakerPolicy{
					Window:      time.Second,
					MinRequests: 1,
					OpenFor:     time.Second,
					Probes:      1,
				}

				return p
			},
		},
		{
			name: "a queue with no drop policy",
			build: func(p supervisor.Policy) supervisor.Policy {
				p.Queue = supervisor.QueuePolicy{Depth: 1, Drop: supervisor.DropPolicy(9)}

				return p
			},
		},
		{
			name: "more stream slots than the bulkhead holds",
			build: func(p supervisor.Policy) supervisor.Policy {
				p.StreamConcurrency = p.Concurrency + 1

				return p
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := testCase.build(supervisor.DefaultPolicy()).Validate(ref.KindStorage)
			require.Error(t, err)
		})
	}
}

func TestAQueueWithNoDropPolicyIsRefused(t *testing.T) {
	t.Parallel()

	// The drop policy has to be written down. If the zero value named one,
	// every queue nobody thought about would get it without a word.
	policy := supervisor.DefaultPolicy()
	policy.Queue = supervisor.QueuePolicy{Depth: 16}

	err := policy.Validate(ref.KindStorage)

	require.ErrorIs(t, err, supervisor.ErrInvalidPolicy)
	assert.Contains(t, err.Error(), "no drop policy")

	policy.Queue.Drop = supervisor.DropOldest
	require.NoError(t, policy.Validate(ref.KindStorage))
}

func TestBreakerWithoutAProbeTimeoutIsRefused(t *testing.T) {
	t.Parallel()

	// A breaker whose probe can never time out sticks in half-open the first
	// time a probe does not return.
	policy := supervisor.DefaultPolicy()
	policy.Breaker = &supervisor.BreakerPolicy{
		Window:       time.Second,
		MinRequests:  1,
		FailureRatio: 1,
		OpenFor:      time.Second,
		Probes:       1,
	}

	require.ErrorIs(t, policy.Validate(ref.KindStorage), supervisor.ErrInvalidPolicy)
}
