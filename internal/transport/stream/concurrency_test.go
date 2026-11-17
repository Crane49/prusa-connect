package stream

import (
	"context"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/event"
)

// TestPublishAndSubscribeConcurrently runs outside a synctest bubble on purpose:
// what it exercises is goroutine interleaving, not time. Readers attach, resume
// and detach while every origin is publishing, which is the case the
// copy-on-write subscriber slice and the replay merge both exist for.
//
// The invariant asserted is the one an out-of-order attach breaks: within one
// origin, every offset a reader is handed is greater than the last it was
// handed, across resumes and across reconnects.
func TestPublishAndSubscribeConcurrently(t *testing.T) {
	const (
		printers    = 4
		churnRounds = 200
		maxEvents   = 20_000
	)

	origins := make([]event.Origin, 0, printers)
	for i := range printers {
		origins = append(origins, originOf(event.KindDevice, "printer-"+strconv.Itoa(i)))
	}

	hub, err := New(Options{
		Site:          "01K2A0SPRAGUEFARM07",
		Authorizer:    staticAuthorizer{visible: map[string][]event.Origin{alice: origins}},
		EventBytes:    64 << 20,
		HistoryEvents: 8192,
		HistoryBytes:  64 << 20,
	})
	require.NoError(t, err)

	defer hub.Close()

	var (
		writers   sync.WaitGroup
		published atomic.Int64
	)

	stop := make(chan struct{})

	// One publisher per printer: the sequence is single-threaded per source,
	// which is the whole of the ordering guarantee.
	for _, origin := range origins {
		writers.Go(func() {
			for i := 0; published.Load() < maxEvents; i++ {
				select {
				case <-stop:
					return
				default:
				}

				if _, publishErr := hub.Publish(stateOf(origin, i)); publishErr != nil {
					t.Error(publishErr)

					return
				}

				published.Add(1)
				runtime.Gosched()
			}
		})
	}

	seen := make(map[event.Origin]uint64, printers)
	cursor := event.Cursor{}
	received, resyncs := 0, 0

	for range churnRounds {
		// A window longer than the test parks the pump, so this goroutine is
		// the only one draining and the assertions see every event the attach
		// queued, in the order it queued them.
		sub, subErr := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Resume:    cursor,
			Interval:  time.Hour,
		})
		require.NoError(t, subErr)

		// A subscription carries what arrives after it attaches, so draining
		// the instant it exists reads an empty queue and the round proves
		// nothing. Wait for the same wake the pump waits on, which is the
		// signal a publisher raises once it has queued something.
		select {
		case <-sub.wake:
		case <-time.After(5 * time.Millisecond):
			// The publishers have reached maxEvents and stopped. Draining
			// anyway is correct: the replay from the cursor is what is being
			// tested, and it does not need a live publisher. The window is
			// short because it is paid on every one of the churn rounds.
		}

		batch, ready := sub.drain()
		sub.Close()

		if !ready {
			continue
		}

		if batch.Resync {
			// Continuity was declared gone, so the reader reloads state and
			// starts again from the head it was handed.
			resyncs++

			clear(seen)

			// Only this hub's generation: an offset another node issued is not
			// a place in this log.
			if generation, held := batch.Cursor.Generation(hub.Epoch()); held {
				for _, position := range generation.Positions {
					seen[position.Origin] = position.Offset
				}
			}

			cursor = batch.Cursor

			continue
		}

		for _, frame := range batch.Events {
			require.Greaterf(t, frame.Position.Offset, seen[frame.Position.Origin],
				"origin %s went backwards or repeated itself", frame.Position.Origin)
			seen[frame.Position.Origin] = frame.Position.Offset
			received++
		}

		cursor = batch.Cursor
	}

	close(stop)
	writers.Wait()

	t.Logf("published %d, received %d in order, %d resyncs", published.Load(), received, resyncs)

	require.Positive(t, published.Load())
	require.Positive(t, received)
	require.Equal(t, 0, hub.Subscribers())
}
