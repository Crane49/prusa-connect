package app

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

// A start-up pass asks the history store one question about the run before this
// one: which commands and which transfers were left open. It runs on a
// goroutine, off the path the attach is on, and these are the two ends it has.
//
// It does not end with its caller. The pass is triggered by whichever request
// happened to attach a provider that keeps facts, and a client that stopped
// waiting for that request must not leave the rows of the last run open.
//
// It does end. The store is a supervised instance that may be slow or wedged,
// and the feed waits for every running pass before it detaches the providers
// the passes write into, so a pass with no end would be a shutdown with no end.
func TestAStartUpPassOutlivesItsCallerAndStillEnds(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		feed := newHistoryFeed(nil, nil, discard())

		running := make(chan struct{})

		var ended atomic.Pointer[error]

		ctx, cancel := context.WithCancel(context.Background())

		feed.sweep(ctx, func(passing context.Context) {
			close(running)

			<-passing.Done()

			err := passing.Err()
			ended.Store(&err)
		})

		<-running

		cancel()
		synctest.Wait()

		require.Nil(t, ended.Load(), "the pass ended with the caller that started it")

		time.Sleep(sweepBudget)
		synctest.Wait()

		require.NotNil(t, ended.Load(), "the pass outlived the budget that bounds it")
		require.ErrorIs(t, *ended.Load(), context.DeadlineExceeded)

		feed.sweeps.Wait()
	})
}

// TestTheFeedWaitsForAStartUpPassBeforeItDetaches is the other half: the count
// the feed waits on is taken before the goroutine starts, so a pass registered
// and not yet running is still one the feed waits for.
func TestTheFeedWaitsForAStartUpPassBeforeItDetaches(t *testing.T) {
	t.Parallel()

	feed := newHistoryFeed(nil, nil, discard())

	release := make(chan struct{})
	waited := make(chan struct{})

	feed.sweep(context.Background(), func(context.Context) { <-release })

	go func() {
		defer close(waited)

		feed.sweeps.Wait()
	}()

	select {
	case <-waited:
		t.Fatal("the feed stopped waiting while a start-up pass was still writing")
	case <-time.After(10 * time.Millisecond):
	}

	close(release)

	<-waited
}
