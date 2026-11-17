package resourcehost

import (
	"context"
	"errors"
	"time"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// PolicyOf is the policy the supervisor of a running instance holds.
func PolicyOf(h *Host, instance ref.Ref) (supervisor.Policy, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	running, ok := h.running[instance]
	if !ok {
		return supervisor.Policy{}, false
	}

	return running.runner.Policy(), true
}

// WithHang sets the hang policy every service child of the host is watched
// under.
func WithHang(cfg *Config, every, timeout time.Duration, misses int, falseFor, within time.Duration) {
	cfg.hang = hangPolicy{
		Every: every, Timeout: timeout, Misses: misses, BreakerFalseFor: falseFor, EvidenceWithin: within,
	}
}

// CallChild calls method on a running service instance through its
// supervisor, so the breaker and the bulkhead see the call.
func CallChild(ctx context.Context, h *Host, instance ref.Ref, method string) error {
	return onChild(ctx, h, instance, func(callCtx context.Context, child *serviceChild) error {
		_, err := child.Call(callCtx, method, nil)

		return err
	})
}

// KillChild kills a running service instance once for each result, in order.
func KillChild(ctx context.Context, h *Host, instance ref.Ref, results ...supervisor.Result) error {
	return onChild(ctx, h, instance, func(_ context.Context, child *serviceChild) error {
		for _, result := range results {
			child.killFor(supervisor.Stop{Result: result, At: time.Now()})
		}

		return nil
	})
}

func onChild(ctx context.Context, h *Host, instance ref.Ref, fn func(context.Context, *serviceChild) error) error {
	h.mu.Lock()
	running, ok := h.running[instance]
	h.mu.Unlock()

	if !ok {
		return errors.New("not running: " + instance.String())
	}

	return supervisor.CallVoid(ctx, running.runner, "fixture", func(callCtx context.Context, target any) error {
		child, isChild := target.(*serviceChild)
		if !isChild {
			return errors.New("not a service child: " + instance.String())
		}

		return fn(callCtx, child)
	})
}
