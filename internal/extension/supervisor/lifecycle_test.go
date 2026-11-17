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

func TestInstanceReachesReadyAndCarriesItsConditions(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, supervisor.Config[*target]{
			Ref:    testRef(t, "ready-store"),
			Type:   "fake",
			Policy: testPolicy(),
			Start:  starts(ok),
		})

		require.Equal(t, "pending", h.state(), "an instance is pending before it is run")

		h.run(t)
		synctest.Wait()

		require.Equal(t, "ready", h.state())
		assert.Equal(t, []string{"pending", "starting", "ready"}, h.seen())

		status := h.inst.Status()
		assert.Equal(t, supervisor.ResultSuccess, status.Result)
		assert.Equal(t, int64(1), status.Generation)
		assert.Equal(t, condition.StatusTrue, statusOf(status, condition.TypeStarted))
		assert.Equal(t, condition.StatusTrue, statusOf(status, condition.TypeConfigured))
		assert.Equal(t, condition.StatusTrue, statusOf(status, condition.TypeBreakerClosed))

		h.stop()

		assert.Equal(t, "stopped", h.state(), "shutdown announces itself")
	})
}

func TestRestartDoesNotFlapThroughFailed(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, supervisor.Config[*target]{
			Ref:    testRef(t, "restart-store"),
			Type:   "fake",
			Policy: testPolicy(),
			Start:  starts(ok),
		})

		h.run(t)
		synctest.Wait()
		require.Equal(t, "ready", h.state())

		h.inst.Restart()
		synctest.Wait()

		require.Equal(t, "ready", h.state())
		assert.Equal(t,
			[]string{"pending", "starting", "ready", "stopping", "stopped", "starting", "ready"},
			h.seen(),
			"a restart is not a failure and must not be reported as one",
		)
		assert.NotContains(t, h.seen(), "failed")
		assert.Equal(t, int64(2), h.inst.Status().Generation, "a restart rebuilds the instance")

		h.stop()
	})
}

func TestUnrecognisedErrorIsRecoverableAndRetries(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		// An error this host does not recognise. Treating it as permanent
		// would leave an integration dead after one temporary failure.
		unrecognised := errors.New("something no classifier has an opinion about")

		var attempts atomic.Int64

		h := newHarness(t, supervisor.Config[*target]{
			Ref:    testRef(t, "retry-store"),
			Type:   "fake",
			Policy: testPolicy(),
			Start: func(context.Context, supervisor.Handle) (*target, error) {
				if attempts.Add(1) == 1 {
					return nil, unrecognised
				}

				return &target{do: func(context.Context) error { return nil }}, nil
			},
		})

		require.Equal(t, supervisor.ClassRecoverable, supervisor.Classify(unrecognised))

		h.run(t)
		synctest.Wait()

		require.Equal(t, "retrying", h.state(), "a failure nobody recognises is retried, not latched")

		status := h.inst.Status()
		assert.Equal(t, 1, status.Attempt)
		assert.Equal(t, condition.StatusFalse, statusOf(status, condition.TypeStarted))
		assert.Equal(t, time.Now().Add(supervisor.DefaultBackoffBase), status.NextRetryAt)

		time.Sleep(supervisor.DefaultBackoffBase)
		synctest.Wait()

		assert.Equal(t, "ready", h.state())
		assert.Equal(t, int64(2), attempts.Load())

		h.stop()
	})
}

func TestPermanentErrorLatchesUntilRestart(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var attempts atomic.Int64

		h := newHarness(t, supervisor.Config[*target]{
			Ref:    testRef(t, "failed-store"),
			Type:   "fake",
			Policy: testPolicy(),
			Start: func(context.Context, supervisor.Handle) (*target, error) {
				if attempts.Add(1) == 1 {
					return nil, credentials.New("401 from the endpoint")
				}

				return &target{do: func(context.Context) error { return nil }}, nil
			},
		})

		h.run(t)
		synctest.Wait()

		require.Equal(t, "failed", h.state())
		assert.Equal(t, supervisor.ResultStartError, h.inst.Status().Result)

		time.Sleep(time.Hour)
		synctest.Wait()

		assert.Equal(t, "failed", h.state(), "a latched instance does not retry on its own")
		assert.Equal(t, int64(1), attempts.Load())

		// Restart resets the failure and starts again in one call, so an
		// operator does not have to know a two-step recipe.
		h.inst.Restart()
		synctest.Wait()

		assert.Equal(t, "ready", h.state())

		h.stop()
	})
}

func TestBackoffLadderIsBoundedAndJittered(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		policy := testPolicy()
		policy.Required = true // never give up, so the ladder can be read to its cap

		var at recorder[time.Duration]

		origin := time.Now()
		h := newHarness(t, supervisor.Config[*target]{
			Ref:    testRef(t, "ladder-store"),
			Type:   "fake",
			Policy: policy,
			Start: func(context.Context, supervisor.Handle) (*target, error) {
				at.add(time.Since(origin))

				return nil, errors.New("still down")
			},
		})

		h.run(t)

		time.Sleep(20 * time.Minute)
		synctest.Wait()

		// Full jitter pinned to its upper bound, so the ladder doubles from
		// the base up to the cap.
		want := []time.Duration{0, time.Second, 3 * time.Second, 7 * time.Second, 15 * time.Second}
		attemptedAt := at.all()
		require.GreaterOrEqual(t, len(attemptedAt), len(want))
		assert.Equal(t, want, attemptedAt[:len(want)])

		last := attemptedAt[len(attemptedAt)-1] - attemptedAt[len(attemptedAt)-2]
		assert.Equal(t, supervisor.DefaultBackoffCap, last, "the ladder is bounded at its cap")

		h.stop()
	})
}

func TestOptionalInstanceGivesUpAndRequiredOneDoesNot(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		required bool
		want     string
		result   supervisor.Result
	}{
		{name: "optional gives up", required: false, want: "failed", result: supervisor.ResultGiveUp},
		{name: "required keeps trying", required: true, want: "retrying", result: supervisor.ResultStartError},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				policy := testPolicy()
				policy.Required = testCase.required

				h := newHarness(t, supervisor.Config[*target]{
					Ref:    testRef(t, "giveup-store"),
					Type:   "fake",
					Policy: policy,
					Start: func(context.Context, supervisor.Handle) (*target, error) {
						return nil, errors.New("endpoint is gone")
					},
				})

				h.run(t)

				time.Sleep(2 * supervisor.DefaultGiveUpAfter)
				synctest.Wait()

				assert.Equal(t, testCase.want, h.state())
				assert.Equal(t, testCase.result, h.inst.Status().Result)

				h.stop()
			})
		})
	}
}

// credentials is registered at package level, not in a test body. Registration
// is process-wide and panics on a duplicate, so a test that registers cannot be
// run twice in one process and -count=2 fails before it starts.
//
//nolint:gochecknoglobals // registration happens at init time.
var credentials = fault.Register(
	must(fault.NewDomain("test.supervisor")), "BAD_CREDENTIAL", 403, fault.Document(
		"The remote rejected the credential",
		"Fix the credential and restart the resource.",
		fault.RetryNever,
		fault.ShowDetail,
	))

func statusOf(s supervisor.Status, t condition.Type) condition.Status {
	for _, c := range s.Conditions {
		if c.Type == t {
			return c.Status
		}
	}

	return condition.StatusUnknown
}

// windowPolicy switches an instance off at the third failure inside an hour.
func windowPolicy() supervisor.Policy {
	policy := testPolicy()
	policy.FailureLimit = 3
	policy.FailureWindow = time.Hour

	return policy
}

// failAndRecover reports a runtime failure, then waits out the retry so the
// instance serves again before the next one.
func failAndRecover(t *testing.T, h *harness) {
	t.Helper()

	h.inst.Reporter().Recoverable(errors.New("crashed"))
	synctest.Wait()
	time.Sleep(time.Minute)
	synctest.Wait()
}

func TestARecoverableStopCarriesItsResult(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, supervisor.Config[*target]{
			Ref: testRef(t, "memory-store"), Type: "fake", Policy: windowPolicy(), Start: starts(ok),
		})

		h.run(t)
		synctest.Wait()

		h.inst.Reporter().Recoverable(&supervisor.Stop{
			Result: supervisor.ResultOverMemory, Used: 200 << 20, Budget: 128 << 20,
		})
		synctest.Wait()

		status := h.inst.Status()
		assert.Equal(t, supervisor.ResultOverMemory, status.Result)
		require.Len(t, status.RecentStops, 1)
		assert.Equal(t, int64(200<<20), status.RecentStops[0].Used)
		assert.Equal(t, time.Now(), status.RecentStops[0].At)

		h.stop()
	})
}

func TestTheThirdFailureInTheWindowSwitchesOff(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, supervisor.Config[*target]{
			Ref: testRef(t, "window-store"), Type: "fake", Policy: windowPolicy(), Start: starts(ok),
		})

		h.run(t)
		synctest.Wait()

		failAndRecover(t, h)
		failAndRecover(t, h)
		require.Equal(t, "ready", h.state())

		h.inst.Reporter().Recoverable(errors.New("crashed"))
		synctest.Wait()

		status := h.inst.Status()
		assert.Equal(t, "failed", h.state())
		assert.Equal(t, supervisor.ResultSwitchedOffByHost, status.Result)
		assert.True(t, status.NextRetryAt.IsZero())
		assert.Len(t, status.RecentStops, 3)

		h.stop()
	})
}

// A limit other than three switches off at that count, and not before.
func TestTheFailureLimitIsTheCountThatSwitchesOff(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		policy := windowPolicy()
		policy.FailureLimit = 5
		policy.FailureWindow = 15 * time.Minute

		h := newHarness(t, supervisor.Config[*target]{
			Ref: testRef(t, "five-store"), Type: "fake", Policy: policy, Start: starts(ok),
		})

		h.run(t)
		synctest.Wait()

		for range 4 {
			failAndRecover(t, h)
		}

		require.Equal(t, "ready", h.state())

		h.inst.Reporter().Recoverable(errors.New("crashed"))
		synctest.Wait()

		assert.Equal(t, supervisor.ResultSwitchedOffByHost, h.inst.Status().Result)

		h.stop()
	})
}

// A limit lowered to the failures already counted does not switch off at
// once. The next failure does.
func TestALimitLoweredBelowTheCountSwitchesOffAtTheNextFailure(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		policy := windowPolicy()
		policy.FailureLimit = 5

		h := newHarness(t, supervisor.Config[*target]{
			Ref: testRef(t, "lowered-store"), Type: "fake", Policy: policy, Start: starts(ok),
		})

		h.run(t)
		synctest.Wait()

		failAndRecover(t, h)
		failAndRecover(t, h)

		policy.FailureLimit = 2
		require.NoError(t, h.inst.SetPolicy(policy))
		synctest.Wait()
		require.Equal(t, "ready", h.state())

		h.inst.Reporter().Recoverable(errors.New("crashed"))
		synctest.Wait()

		assert.Equal(t, supervisor.ResultSwitchedOffByHost, h.inst.Status().Result)

		h.stop()
	})
}

// A limit of zero never switches off: every failure is retried, and the
// window still keeps the failures for the status.
func TestNoFailureLimitNeverSwitchesOff(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		policy := windowPolicy()
		policy.FailureLimit = 0

		h := newHarness(t, supervisor.Config[*target]{
			Ref: testRef(t, "never-store"), Type: "fake", Policy: policy, Start: starts(ok),
		})

		h.run(t)
		synctest.Wait()

		for range 10 {
			failAndRecover(t, h)
		}

		assert.NotEqual(t, "failed", h.state())
		assert.NotEqual(t, supervisor.ResultSwitchedOffByHost, h.inst.Status().Result)
		assert.NotEmpty(t, h.inst.Status().RecentStops)

		h.stop()
	})
}

func TestAFailureOutsideTheWindowDoesNotCount(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, supervisor.Config[*target]{
			Ref: testRef(t, "slow-crash-store"), Type: "fake", Policy: windowPolicy(), Start: starts(ok),
		})

		h.run(t)
		synctest.Wait()

		t0 := time.Now()

		failAndRecover(t, h)
		time.Sleep(time.Until(t0.Add(59 * time.Minute)))
		failAndRecover(t, h)
		time.Sleep(time.Until(t0.Add(61 * time.Minute)))

		h.inst.Reporter().Recoverable(errors.New("crashed"))
		synctest.Wait()

		status := h.inst.Status()
		assert.Equal(t, "retrying", h.state())
		assert.Len(t, status.RecentStops, 2)

		h.stop()
	})
}

func TestAFailedStartCounts(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var attempts atomic.Int64

		h := newHarness(t, supervisor.Config[*target]{
			Ref: testRef(t, "start-crash-store"), Type: "fake", Policy: windowPolicy(),
			Start: func(context.Context, supervisor.Handle) (*target, error) {
				if attempts.Add(1) == 3 {
					return nil, errors.New("will not start")
				}

				return &target{do: ok}, nil
			},
		})

		h.run(t)
		synctest.Wait()

		h.inst.Reporter().Recoverable(errors.New("crashed"))
		synctest.Wait()
		time.Sleep(time.Minute)
		synctest.Wait()

		h.inst.Reporter().Recoverable(errors.New("crashed"))
		synctest.Wait()
		time.Sleep(time.Minute)
		synctest.Wait()

		assert.Equal(t, int64(3), attempts.Load())
		assert.Equal(t, supervisor.ResultSwitchedOffByHost, h.inst.Status().Result)

		h.stop()
	})
}

// unserved is the refusal a host returns while a dependency the instance
// declared, such as its record backend, is not serving yet.
type unserved struct{ error }

func (unserved) DependencyReason() string { return "DependencyMissing" }

// TestAStartWaitingOnADependencyDoesNotCount covers a module that starts beside
// its database and loses the race more often than the switch-off limit. It
// keeps retrying and starts once the dependency serves, while a start that
// fails for its own reason is still switched off at the third failure.
func TestAStartWaitingOnADependencyDoesNotCount(t *testing.T) {
	t.Parallel()

	const failedStarts = 5

	cases := []struct {
		name       string
		err        error
		wantStarts int64
		wantState  string
		wantResult supervisor.Result
	}{
		{
			name:       "a dependency that is not serving yet",
			err:        unserved{errors.New("no record backend is serving")},
			wantStarts: failedStarts + 1,
			wantState:  "ready",
			wantResult: supervisor.ResultSuccess,
		},
		{
			name:       "a start that fails",
			err:        errors.New("will not start"),
			wantStarts: 3,
			wantState:  "failed",
			wantResult: supervisor.ResultSwitchedOffByHost,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				var starts atomic.Int64

				h := newHarness(t, supervisor.Config[*target]{
					Ref: testRef(t, "waiting-store"), Type: "fake", Policy: windowPolicy(),
					Start: func(context.Context, supervisor.Handle) (*target, error) {
						if starts.Add(1) <= failedStarts {
							return nil, tc.err
						}

						return &target{do: ok}, nil
					},
				})

				h.run(t)
				time.Sleep(10 * time.Minute)
				synctest.Wait()

				status := h.inst.Status()
				assert.Equal(t, tc.wantStarts, starts.Load())
				assert.Equal(t, tc.wantState, h.state())
				assert.Equal(t, tc.wantResult, status.Result)

				h.stop()
			})
		})
	}
}

// TestAPermanentStartFailureThatFillsTheWindowKeepsItsResult covers a start
// that fails for good as the third failure in the window. It latches either
// way, and it reports the start error, which tells the operator more than the
// switch-off does.
func TestAPermanentStartFailureThatFillsTheWindowKeepsItsResult(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var attempts atomic.Int64

		h := newHarness(t, supervisor.Config[*target]{
			Ref: testRef(t, "permanent-start-store"), Type: "fake", Policy: windowPolicy(),
			Start: func(context.Context, supervisor.Handle) (*target, error) {
				if attempts.Add(1) == 3 {
					return nil, credentials.New("401 from the endpoint")
				}

				return &target{do: ok}, nil
			},
		})

		h.run(t)
		synctest.Wait()

		failAndRecover(t, h)

		h.inst.Reporter().Recoverable(errors.New("crashed"))
		synctest.Wait()
		time.Sleep(time.Minute)
		synctest.Wait()

		status := h.inst.Status()
		assert.Equal(t, int64(3), attempts.Load())
		assert.Equal(t, "failed", h.state())
		assert.Equal(t, supervisor.ResultStartError, status.Result)
		assert.Len(t, status.RecentStops, 3)

		time.Sleep(time.Hour)
		synctest.Wait()

		assert.Equal(t, "failed", h.state(), "a latched instance does not retry on its own")
		assert.Equal(t, int64(3), attempts.Load())

		h.stop()
	})
}

func TestAnOperatorRestartNeitherCountsNorResets(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, supervisor.Config[*target]{
			Ref: testRef(t, "restart-window-store"), Type: "fake", Policy: windowPolicy(), Start: starts(ok),
		})

		h.run(t)
		synctest.Wait()

		failAndRecover(t, h)
		failAndRecover(t, h)

		h.inst.Restart()
		synctest.Wait()
		require.Equal(t, "ready", h.state())
		require.Len(t, h.inst.Status().RecentStops, 2)

		h.inst.Reporter().Recoverable(errors.New("crashed"))
		synctest.Wait()

		assert.Equal(t, supervisor.ResultSwitchedOffByHost, h.inst.Status().Result)

		h.stop()
	})
}

func TestNoWindowWithoutAFailureLimit(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, supervisor.Config[*target]{
			Ref: testRef(t, "no-window-store"), Type: "fake", Policy: testPolicy(), Start: starts(ok),
		})

		h.run(t)
		synctest.Wait()

		for range 10 {
			failAndRecover(t, h)
		}

		h.inst.Reporter().Recoverable(errors.New("crashed"))
		synctest.Wait()

		assert.Equal(t, "retrying", h.state())
		assert.Empty(t, h.inst.Status().RecentStops)

		h.stop()
	})
}
