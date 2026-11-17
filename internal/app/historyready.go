package app

import (
	"context"
)

// historyReadiness holds the start-up work that must wait for a store.
//
// A history instance reaches ready some time after the boot sequence runs. A
// sweep called from the boot sequence would ask a provider that is not there
// yet and read nothing. The sweep that closes the transfers the last run left
// open is one such piece of work.
//
// The command sweep does not need this, because it runs when the feed
// attaches a provider that folds commands.
type historyReadiness struct {
	// pending is what has not run because no fact-capable provider has served
	// yet. ready says one has, so a caller that registers afterwards runs at
	// once.
	pending []func(context.Context)
	ready   bool
}

// whenStoring registers work to run once a provider that keeps facts is
// serving, and runs it at once if one already is.
//
// Each callback runs on a goroutine of its own, so no caller waits for it. See
// sweep for the context it gets and for how it ends.
func (f *historyFeed) whenStoring(ctx context.Context, run func(context.Context)) {
	f.mu.Lock()

	if !f.arrivals.ready {
		f.arrivals.pending = append(f.arrivals.pending, run)
		f.mu.Unlock()

		return
	}

	f.mu.Unlock()

	f.sweep(ctx, run)
}

// sweep runs one start-up pass on a goroutine of its own.
//
// The context is detached from the caller's, because a pass is about the run
// before this one and must not end when the caller's context ends. It carries
// a deadline of its own, so a store that will not answer cannot hold the pass
// open for as long as the process runs.
//
// The pass is counted while it runs. The feed waits for the count before it
// detaches the providers a pass writes into, so a shutdown does not cut off
// the rows a pass was about to write.
func (f *historyFeed) sweep(ctx context.Context, run func(context.Context)) {
	f.sweeps.Go(func() {
		passing, done := context.WithTimeout(context.WithoutCancel(ctx), sweepBudget)
		defer done()

		run(passing)
	})
}

// storing is what attach calls once it has a provider that keeps facts. It is
// idempotent: the flag is set once and the queue is emptied once, so a second
// provider attaching does not sweep a second time.
func (f *historyFeed) storing(ctx context.Context) {
	f.mu.Lock()

	if f.arrivals.ready {
		f.mu.Unlock()

		return
	}

	f.arrivals.ready = true
	waiting := f.arrivals.pending
	f.arrivals.pending = nil

	f.mu.Unlock()

	for _, run := range waiting {
		f.sweep(ctx, run)
	}
}
