package supervisor

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	queueRacers = 8
	queueOffers = 200
	queueRounds = 200
)

func noJob(context.Context, int) error { return nil }

// TestOfferNeverPanicsWhileTheQueueIsClosing is the reason the item channel is
// never closed.
//
// A sink is offered work from the hub's fan-out goroutine while a teardown runs
// on another. A send racing a close is a panic no recover in this package
// catches, so it ends the process. Only the shape of the queue can prevent it,
// not the shape of the calling code.
func TestOfferNeverPanicsWhileTheQueueIsClosing(t *testing.T) {
	t.Parallel()

	for range queueRounds {
		q := newQueue[int](QueuePolicy{Depth: 2, Drop: DropOldest})

		var (
			wg    sync.WaitGroup
			start = make(chan struct{})
		)

		for range queueRacers {
			wg.Go(func() {
				<-start

				for range queueOffers {
					q.offer(job[int]{op: "deliver", fn: noJob})
				}
			})
		}

		wg.Go(func() {
			<-start

			q.close()
		})

		close(start)
		wg.Wait()
	}
}

func TestOfferAfterCloseIsRefusedAndCounted(t *testing.T) {
	t.Parallel()

	q := newQueue[int](QueuePolicy{Depth: 4, Drop: DropOldest})

	require.True(t, q.offer(job[int]{op: "deliver", fn: noJob}))

	q.close()

	assert.False(t, q.offer(job[int]{op: "deliver", fn: noJob}),
		"a closed queue accepts nothing, so a caller is never told work was taken that never runs")
	assert.Equal(t, int64(1), q.dropCount())

	_, open := q.next()
	assert.False(t, open, "the drain stops on close rather than on an empty channel")
}

func TestCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	// A stop can come from an operator action and from shutdown, and the
	// second one must not close a channel that is already closed.
	q := newQueue[int](QueuePolicy{Depth: 1, Drop: DropNewest})

	q.close()
	q.close()
}
