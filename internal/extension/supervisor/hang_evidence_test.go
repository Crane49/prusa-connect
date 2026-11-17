package supervisor_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

func blocksUntilCancelled(ctx context.Context) error {
	<-ctx.Done()

	return ctx.Err()
}

func readyHarness(t *testing.T, id string, policy supervisor.Policy, do func(context.Context) error) *harness {
	t.Helper()

	h := newHarness(t, supervisor.Config[*target]{
		Ref:    testRef(t, id),
		Type:   "fake",
		Policy: policy,
		Start:  starts(do),
	})

	h.run(t)
	synctest.Wait()
	require.Equal(t, "ready", h.state())

	return h
}

func TestAHostTimeoutIsHangEvidence(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		policy := testPolicy()
		policy.CallTimeout = 50 * time.Millisecond
		h := readyHarness(t, "hang-timeout", policy, blocksUntilCancelled)

		ctx, cancel := bubbleContext()
		defer cancel()

		err := call(ctx, h.inst, "stat")

		assert.True(t, fault.HasCode(err, supervisor.CodeTimeout()), "got %v", err)
		assert.False(t, h.inst.Status().HangEvidenceAt.IsZero())

		h.stop()
	})
}

func TestACallersOwnDeadlineIsNotHangEvidence(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		policy := testPolicy()
		policy.CallTimeout = time.Second
		h := readyHarness(t, "hang-caller", policy, blocksUntilCancelled)

		parent, cancelParent := bubbleContext()
		defer cancelParent()

		ctx, cancel := context.WithTimeout(parent, 20*time.Millisecond)
		defer cancel()

		require.Error(t, call(ctx, h.inst, "stat"))
		assert.True(t, h.inst.Status().HangEvidenceAt.IsZero())

		h.stop()
	})
}

// fullBulkhead returns a ready instance whose only slot is held by a call that
// outlives the test body.
func fullBulkhead(t *testing.T, id string) (*harness, context.Context) {
	t.Helper()

	policy := testPolicy()
	policy.Concurrency = 1
	policy.CallTimeout = time.Hour
	h := readyHarness(t, id, policy, blocksUntilCancelled)

	held, cancel := bubbleContext()
	t.Cleanup(func() {
		cancel()
		synctest.Wait()
		h.stop()
	})

	go func() { _ = call(held, h.inst, "held") }()
	synctest.Wait()

	return h, held
}

func TestAFullBulkheadIsHangEvidence(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h, held := fullBulkhead(t, "hang-bulkhead")

		ctx, cancel := context.WithTimeout(held, 50*time.Millisecond)
		defer cancel()

		require.ErrorIs(t, call(ctx, h.inst, "stat"), context.DeadlineExceeded)
		assert.False(t, h.inst.Status().HangEvidenceAt.IsZero())
	})
}

func TestACallerThatCancelsWhileWaitingForASlotIsNotHangEvidence(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h, held := fullBulkhead(t, "hang-bulkhead-cancel")

		ctx, cancel := context.WithCancel(held)
		time.AfterFunc(10*time.Millisecond, cancel)

		require.ErrorIs(t, call(ctx, h.inst, "stat"), context.Canceled)
		assert.True(t, h.inst.Status().HangEvidenceAt.IsZero())
	})
}

func TestAnExpiredCallerWithAFreeSlotIsNotHangEvidence(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		policy := testPolicy()
		policy.Concurrency = 1
		h := readyHarness(t, "hang-free-slot", policy, blocksUntilCancelled)

		parent, cancelParent := bubbleContext()
		defer cancelParent()

		ctx, cancel := context.WithDeadline(parent, time.Now().Add(-time.Second))
		defer cancel()

		_ = call(ctx, h.inst, "stat")

		assert.True(t, h.inst.Status().HangEvidenceAt.IsZero())

		h.stop()
	})
}

func TestAnErrorAnsweredPromptlyIsNotHangEvidence(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := readyHarness(t, "hang-prompt", breakerPolicy(), failsAtOnce)

		ctx, cancel := bubbleContext()
		defer cancel()

		for range 20 {
			_ = call(ctx, h.inst, "stat")
		}

		st := h.inst.Status()
		assert.Equal(t, condition.StatusFalse, statusOf(st, condition.TypeBreakerClosed))
		assert.True(t, st.HangEvidenceAt.IsZero())

		h.stop()
	})
}

func TestABreakerRefusalIsNotHangEvidence(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := readyHarness(t, "hang-refusal", breakerPolicy(), failsAtOnce)

		ctx, cancel := bubbleContext()
		defer cancel()

		for range 20 {
			_ = call(ctx, h.inst, "stat")
		}

		for range 5 {
			err := call(ctx, h.inst, "stat")
			assert.True(t, fault.HasCode(err, supervisor.CodeBreakerOpen()), "got %v", err)
		}

		assert.True(t, h.inst.Status().HangEvidenceAt.IsZero())

		h.stop()
	})
}

func breakerPolicy() supervisor.Policy {
	policy := testPolicy()
	policy.Breaker = &supervisor.BreakerPolicy{
		Window:       10 * time.Second,
		MinRequests:  2,
		FailureRatio: 0.5,
		OpenFor:      30 * time.Second,
		Probes:       1,
		ProbeTimeout: 30 * time.Second,
	}

	return policy
}

func failsAtOnce(context.Context) error { return errors.New("the endpoint is refusing connections") }
