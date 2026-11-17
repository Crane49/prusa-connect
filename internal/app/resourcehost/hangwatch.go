package resourcehost

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// hangPolicy is when the host treats a service module as hung.
//
// EvidenceWithin must stay below BreakerFalseFor. A restarted child's breaker
// starts closed, so evidence recent enough to count is then evidence from the
// new child, never from the one that was killed.
type hangPolicy struct {
	// Every is how often the module is pinged.
	Every time.Duration
	// Timeout is how long one ping may go unanswered.
	Timeout time.Duration
	// Misses is how many unanswered pings in a row mean not responding.
	Misses int
	// BreakerFalseFor is how long the breaker must have been not closed
	// before hang evidence alone kills the module.
	BreakerFalseFor time.Duration
	// EvidenceWithin is how recent that evidence must be.
	EvidenceWithin time.Duration
}

const (
	pingEvery       = 15 * time.Second
	pingTimeout     = 5 * time.Second
	pingMisses      = 3
	breakerFalseFor = 2 * time.Minute
	evidenceWithin  = time.Minute
)

func defaultHangPolicy() hangPolicy {
	return hangPolicy{
		Every:           pingEvery,
		Timeout:         pingTimeout,
		Misses:          pingMisses,
		BreakerFalseFor: breakerFalseFor,
		EvidenceWithin:  evidenceWithin,
	}
}

// deadlocked reports a module whose breaker has been not closed for longer
// than BreakerFalseFor and which saw hang evidence within EvidenceWithin. A
// breaker that opened and saw no call since holds only old evidence, so an
// idle module is never killed on it.
func (p hangPolicy) deadlocked(st supervisor.Status, now time.Time) bool {
	if st.HangEvidenceAt.IsZero() || now.Sub(st.HangEvidenceAt) > p.EvidenceWithin {
		return false
	}

	for _, held := range st.Conditions {
		if held.Type == condition.TypeBreakerClosed {
			return held.Status == condition.StatusFalse && now.Sub(held.LastTransitionTime) > p.BreakerFalseFor
		}
	}

	return false
}

// answered reports a ping the child answered. Method not found is an answer:
// a child built against contract 1.0.0 does not serve plugin.ping, and its
// refusal proves it still reads the connection.
func answered(err error) bool {
	if err == nil {
		return true
	}

	var wireErr *wirerpc.Error

	return errors.As(err, &wireErr) && wireErr.Code == wirerpc.CodeMethodNotFound
}

// hangWatch pings one running child and kills it once it stops answering, or
// once its breaker says its calls hang.
type hangWatch struct {
	policy hangPolicy
	// ping goes over the connection directly, so it takes no bulkhead slot
	// and the breaker never sees it.
	ping   func(context.Context) error
	status func() supervisor.Status
	kill   func(supervisor.Stop)
	log    *slog.Logger
}

// run pings every policy.Every until exited or stopped closes, and calls kill
// at most once. ctx is the parent of each ping and of the log line; it is
// never cancelled, so it does not end the watch. It watches stopped as well
// as exited for the reason the memory watch does: a grandchild holding the
// child's standard error keeps exited open after the child is gone.
func (w *hangWatch) run(ctx context.Context, exited, stopped <-chan struct{}) {
	ticker := time.NewTicker(w.policy.Every)
	defer ticker.Stop()

	misses := 0

	for {
		select {
		case <-exited:
			return
		case <-stopped:
			return
		case <-ticker.C:
		}

		pingCtx, cancel := context.WithTimeout(ctx, w.policy.Timeout)
		err := w.ping(pingCtx)

		cancel()

		if answered(err) {
			misses = 0
		} else {
			misses++
		}

		signal := "breaker"
		if misses >= w.policy.Misses {
			signal = "ping"
		} else if !w.policy.deadlocked(w.status(), time.Now()) {
			continue
		}

		// A ping fails at once on a child that has just ended. That child
		// stopped for another reason and did not hang.
		select {
		case <-exited:
			return
		case <-stopped:
			return
		default:
		}

		w.log.WarnContext(ctx, "the module stopped answering and was killed",
			"missed_pings", misses, "signal", signal)
		w.kill(supervisor.Stop{Result: supervisor.ResultNotResponding, At: time.Now()})

		return
	}
}
