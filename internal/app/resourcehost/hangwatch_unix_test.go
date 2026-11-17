//go:build !windows

package resourcehost_test

import (
	"context"
	"errors"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// These cases stop a real child with SIGSTOP, which Windows does not have.

// newHangWired is a host whose hang watch and breaker run in milliseconds, so
// a hang is found well inside a test. A call that outlasts callTimeout is hang
// evidence, so a case that must not see any sets it well above a stall.
func newHangWired(t *testing.T, callTimeout time.Duration) *wired {
	t.Helper()

	return newWiredWith(t, nil, func(cfg *resourcehost.Config) {
		resourcehost.WithHang(cfg, 300*time.Millisecond, 200*time.Millisecond, 3, 2*time.Second, time.Second)
		cfg.Policy = func(kind ref.Kind, declared resource.Declaration) supervisor.Policy {
			policy := promptPolicy(kind, declared)
			policy.CallTimeout = callTimeout
			policy.Breaker = supervisor.DefaultBreakerPolicy()
			policy.Breaker.Window = time.Second
			policy.Breaker.MinRequests = 3
			policy.Breaker.OpenFor = 300 * time.Millisecond

			return policy
		}
	})
}

// hogPids is every pid the hog child announced, in order.
func hogPids(t *testing.T, logged string) []int {
	t.Helper()

	var pids []int

	for _, found := range hogBanner.FindAllStringSubmatch(logged, -1) {
		pid, err := strconv.Atoi(found[1])
		require.NoError(t, err)

		pids = append(pids, pid)
	}

	return pids
}

// keepCalling calls method on the instance every 50 ms until the returned
// stop is called or the test ends.
func (w *wired) keepCalling(instance ref.Ref, method string) (stop func()) {
	ctx, cancel := context.WithCancel(w.t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = resourcehost.CallChild(ctx, w.host, instance, method)
			}
		}
	}()

	stop = func() {
		cancel()
		<-done
	}
	w.t.Cleanup(stop)

	return stop
}

// TestAFrozenModuleIsRestartedAndCounted is the ping path: a stopped process
// misses its pings, is killed as not-responding, starts again, and the stop
// counts toward its switch-off. The calls beside it write far less than the
// pipe holds, so this is a child that does not read, not one whose standard
// input is full.
func TestAFrozenModuleIsRestartedAndCounted(t *testing.T) {
	t.Parallel()

	w := newHangWired(t, 100*time.Millisecond)
	w.declareHog(installHog(t), "idle", 64)

	instance := theServiceSink(t)
	w.waitReady(instance)
	w.waitLog(hogBanner.MatchString, "the child never announced itself")

	frozen := hogPids(t, w.logs.String())[0]
	// Only SIGCONT: by then the pid may name another process, and SIGCONT
	// does nothing to one that runs.
	t.Cleanup(func() { _ = syscall.Kill(frozen, syscall.SIGCONT) })

	stopCalling := w.keepCalling(instance, "fixture.fail")

	started := time.Now()
	require.NoError(t, syscall.Kill(frozen, syscall.SIGSTOP))

	var status httpapi.ResourceStatus

	w.await(func() bool {
		status, _ = w.host.Status(instance)

		return status.State == condition.StateRetrying().String() && status.Result == "not-responding"
	}, "the frozen child was never stopped as not responding", func() string { return w.describe(instance) })

	require.Less(t, time.Since(started), 3*time.Second, "the frozen child was found too late")
	require.NotNil(t, status.Memory)
	require.NotNil(t, status.Memory.LastStop)
	require.Equal(t, "not-responding", status.Memory.LastStop.Result)

	stopCalling()

	restarting := time.Now()

	w.waitLog(func(logged string) bool { return len(hogBanner.FindAllString(logged, -1)) > 1 },
		"the killed child was never replaced")
	w.waitReady(instance)
	require.Less(t, time.Since(restarting), supervisor.DefaultBackoffBase+2*time.Second,
		"the replacement took longer than the backoff")

	require.NotEqual(t, frozen, hogPids(t, w.logs.String())[1])

	status, _ = w.host.Status(instance)
	require.NotNil(t, status.Memory)
	require.Equal(t, 1, status.Memory.FailuresInWindow)
}

// TestADeadlockedHandlerIsRestarted is the breaker path: the child answers
// every ping, and every call runs into the host's deadline.
func TestADeadlockedHandlerIsRestarted(t *testing.T) {
	t.Parallel()

	w := newHangWired(t, 100*time.Millisecond)
	w.declareHog(installHog(t), "idle", 64)

	instance := theServiceSink(t)
	w.waitReady(instance)

	w.keepCalling(instance, "fixture.sleep")

	started := time.Now()

	w.await(func() bool {
		status, _ := w.host.Status(instance)

		return status.Result == "not-responding"
	}, "the deadlocked child was never stopped as not responding", func() string { return w.describe(instance) })

	require.Less(t, time.Since(started), 5*time.Second, "the deadlocked child was found too late")
}

// TestAModuleAnsweringErrorsIsNotKilled is the owner's line between failing
// and hung: errors answered at once open the breaker and are no evidence.
func TestAModuleAnsweringErrorsIsNotKilled(t *testing.T) {
	t.Parallel()

	w := newHangWired(t, time.Second)
	w.declareHog(installHog(t), "idle", 64)

	instance := theServiceSink(t)
	w.waitReady(instance)
	w.waitLog(hogBanner.MatchString, "the child never announced itself")

	stopCalling := w.keepCalling(instance, "fixture.fail")

	opened := false
	deadline := time.After(5 * time.Second)

	for waiting := true; waiting; {
		select {
		case <-deadline:
			waiting = false
		case <-time.After(50 * time.Millisecond):
			status, _ := w.host.Status(instance)
			require.NotEqual(t, "not-responding", status.Result, w.describe(instance))

			for _, held := range status.Conditions {
				if held.Type == string(condition.TypeBreakerClosed) && held.Status == string(condition.StatusFalse) {
					opened = true
				}
			}
		}
	}

	stopCalling()

	require.True(t, opened, "the errors never opened the breaker")
	require.Len(t, hogPids(t, w.logs.String()), 1, "the child was replaced")
}

// TestKillChildrenKillsAModuleWithoutWaitingForItsStop is what a wedged
// shutdown leans on before a restart replaces the process: the module's
// process is gone at once, not after the stop budget its supervisor holds.
func TestKillChildrenKillsAModuleWithoutWaitingForItsStop(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	w.declareHog(installHog(t), "idle", 64)

	w.waitReady(theServiceSink(t))
	w.waitLog(hogBanner.MatchString, "the child never announced itself")

	pid := hogPids(t, w.logs.String())[0]

	w.host.KillChildren()

	w.await(func() bool { return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) },
		"the module process is gone", func() string { return strconv.Itoa(pid) })
}
