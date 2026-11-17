package resourcehost

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

func TestAnsweredCountsMethodNotFoundAsAlive(t *testing.T) {
	t.Parallel()

	require.True(t, answered(nil))
	require.True(t, answered(fmt.Errorf("ping: %w", &wirerpc.Error{Code: wirerpc.CodeMethodNotFound})))
	require.False(t, answered(context.DeadlineExceeded))
	require.False(t, answered(&wirerpc.Error{Code: wirerpc.CodeInternal}))
}

// breakerStatus is a status whose breaker went False at since, with hang
// evidence at evidence (zero for none).
func breakerStatus(state condition.Status, since, evidence time.Time) supervisor.Status {
	return supervisor.Status{
		Conditions: []condition.Condition{
			{Type: condition.TypeBreakerClosed, Status: state, LastTransitionTime: since},
		},
		HangEvidenceAt: evidence,
	}
}

func TestDeadlocked(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	policy := defaultHangPolicy()

	cases := []struct {
		name   string
		status supervisor.Status
		want   bool
	}{
		{"open 3 min, evidence 40 s ago", breakerStatus(condition.StatusFalse,
			now.Add(-3*time.Minute), now.Add(-40*time.Second)), true},
		{"open 3 min, evidence 5 min ago", breakerStatus(condition.StatusFalse,
			now.Add(-3*time.Minute), now.Add(-5*time.Minute)), false},
		{"open 3 min, no evidence", breakerStatus(condition.StatusFalse,
			now.Add(-3*time.Minute), time.Time{}), false},
		{"open 1 min, evidence 1 s ago", breakerStatus(condition.StatusFalse,
			now.Add(-time.Minute), now.Add(-time.Second)), false},
		{"closed, evidence 1 s ago", breakerStatus(condition.StatusTrue,
			now.Add(-3*time.Minute), now.Add(-time.Second)), false},
		{"no breaker condition", supervisor.Status{HangEvidenceAt: now.Add(-time.Second)}, false},
	}

	for _, tc := range cases {
		require.Equal(t, tc.want, policy.deadlocked(tc.status, now), tc.name)
	}
}

// pinger is a ping whose answers a case scripts. Past the script it blocks
// until its context ends.
type pinger struct {
	mu      sync.Mutex
	answers []error
	calls   int
}

func (p *pinger) ping(ctx context.Context) error {
	p.mu.Lock()
	n := p.calls
	p.calls++
	p.mu.Unlock()

	if n < len(p.answers) {
		return p.answers[n]
	}

	<-ctx.Done()

	return ctx.Err()
}

func (p *pinger) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.calls
}

// killer records every stop the watch asked for, and when.
type killer struct {
	mu    sync.Mutex
	stops []supervisor.Stop
}

func (k *killer) kill(stop supervisor.Stop) {
	k.mu.Lock()
	defer k.mu.Unlock()

	k.stops = append(k.stops, stop)
}

func (k *killer) taken() []supervisor.Stop {
	k.mu.Lock()
	defer k.mu.Unlock()

	return append([]supervisor.Stop(nil), k.stops...)
}

func newTestWatch(p *pinger, k *killer, status func() supervisor.Status) *hangWatch {
	if status == nil {
		status = func() supervisor.Status { return supervisor.Status{} }
	}

	return &hangWatch{
		policy: defaultHangPolicy(),
		ping:   p.ping,
		status: status,
		kill:   k.kill,
		log:    slog.New(slog.DiscardHandler),
	}
}

func TestThreeMissedPingsKillTheChild(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		k := &killer{}
		start := time.Now()
		exited, stopped := make(chan struct{}), make(chan struct{})

		go newTestWatch(&pinger{}, k, nil).run(context.Background(), exited, stopped)

		time.Sleep(45 * time.Second)
		synctest.Wait()
		require.Empty(t, k.taken(), "killed before the third ping timed out")

		time.Sleep(5 * time.Second)
		synctest.Wait()

		stops := k.taken()
		require.Len(t, stops, 1)
		require.Equal(t, supervisor.ResultNotResponding, stops[0].Result)
		require.LessOrEqual(t, stops[0].At.Sub(start), 50*time.Second)

		close(exited)
	})
}

func TestOneAnsweredPingResetsTheMisses(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		missed := errors.New("missed")
		p := &pinger{answers: []error{missed, missed, nil, missed, missed, nil}}
		k := &killer{}
		exited, stopped := make(chan struct{}), make(chan struct{})

		go newTestWatch(p, k, nil).run(context.Background(), exited, stopped)

		time.Sleep(80 * time.Second)
		synctest.Wait()
		require.Empty(t, k.taken())
		require.Equal(t, 5, p.count())

		close(stopped)
	})
}

func TestAnIdleModuleWithAnOldBreakerFailureIsNotKilled(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		now := time.Now()
		status := breakerStatus(condition.StatusFalse, now.Add(-10*time.Minute), now.Add(-9*time.Minute))
		p := &pinger{answers: make([]error, 100)}
		k := &killer{}
		exited, stopped := make(chan struct{}), make(chan struct{})

		go newTestWatch(p, k, func() supervisor.Status { return status }).run(context.Background(), exited, stopped)

		time.Sleep(5 * time.Minute)
		synctest.Wait()
		require.Empty(t, k.taken())

		close(exited)
	})
}

func TestABreakerOpenWithFreshEvidenceKillsTheChild(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		now := time.Now()
		p := &pinger{answers: make([]error, 100)}
		k := &killer{}
		exited, stopped := make(chan struct{}), make(chan struct{})
		status := func() supervisor.Status {
			return breakerStatus(condition.StatusFalse, now.Add(-3*time.Minute), time.Now().Add(-time.Second))
		}

		go newTestWatch(p, k, status).run(context.Background(), exited, stopped)

		time.Sleep(15 * time.Second)
		synctest.Wait()

		stops := k.taken()
		require.Len(t, stops, 1)
		require.Equal(t, supervisor.ResultNotResponding, stops[0].Result)

		close(exited)
	})
}

func TestTheWatchEndsWithTheChild(t *testing.T) {
	t.Parallel()

	for _, end := range []string{"exited", "stopped"} {
		t.Run(end, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				p := &pinger{answers: make([]error, 100)}
				exited, stopped := make(chan struct{}), make(chan struct{})
				done := make(chan struct{})

				go func() {
					defer close(done)
					newTestWatch(p, &killer{}, nil).run(context.Background(), exited, stopped)
				}()

				time.Sleep(20 * time.Second)

				if end == "exited" {
					close(exited)
				} else {
					close(stopped)
				}

				synctest.Wait()

				select {
				case <-done:
				default:
					t.Fatal("the watch outlived the child")
				}

				pinged := p.count()

				time.Sleep(time.Minute)
				synctest.Wait()
				require.Equal(t, pinged, p.count(), "a ping after the child ended")
			})
		})
	}
}
