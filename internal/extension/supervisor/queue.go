package supervisor

import (
	"context"
	"sync"
	"sync/atomic"
)

// job is one unit of offered work. The queue stores the closure and not the
// resource. Which target runs it is decided when it is drained, so a reload
// does not leave a backlog pointing at a target that no longer exists.
type job[T any] struct {
	op string
	fn func(context.Context, T) error
}

// queue is the bounded backlog of an async instance. Offering never blocks and
// never allocates past the configured depth. A resource that is too slow loses
// work, and the loss is counted. Otherwise a slow resource would push back on
// the hub that fans out to every other one.
//
// The item channel is never closed. A sink's methods run on the hub's fan-out
// goroutine while an operator restart runs on another, and a send racing a
// close is a panic no recover catches. So shutting down sets a flag under the
// same mutex an offer holds, and wakes the drain through a separate channel.
// The item channel is left to the garbage collector.
type queue[T any] struct {
	items chan job[T]
	drop  DropPolicy
	// done is closed once, by close, to wake the drain goroutine.
	done chan struct{}

	dropped atomic.Int64

	mu     sync.Mutex
	closed bool
}

func newQueue[T any](pol QueuePolicy) *queue[T] {
	return &queue[T]{
		items: make(chan job[T], pol.Depth),
		drop:  pol.Drop,
		done:  make(chan struct{}),
	}
}

// offer enqueues j, reporting false when j was the item dropped. It never
// blocks the caller, whatever the policy, because every channel operation under
// the mutex is non-blocking.
func (q *queue[T]) offer(j job[T]) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		q.dropped.Add(1)

		return false
	}

	select {
	case q.items <- j:
		return true
	default:
	}

	if q.drop == DropNewest {
		q.dropped.Add(1)

		return false
	}

	// Make room by discarding the head. The receive may lose the race to the
	// drain goroutine, which is harmless. Either way one slot came free.
	select {
	case <-q.items:
		q.dropped.Add(1)
	default:
	}

	select {
	case q.items <- j:
		return true
	default:
		q.dropped.Add(1)

		return false
	}
}

// next returns the item at the head, reporting false once the queue is closed.
// A closed queue abandons its backlog instead of delivering it to a target that
// is being torn down. discard counts what is abandoned.
func (q *queue[T]) next() (job[T], bool) {
	select {
	case <-q.done:
		return job[T]{}, false
	default:
	}

	select {
	case work := <-q.items:
		return work, true
	case <-q.done:
		return job[T]{}, false
	}
}

// discard counts everything still queued when the drain stops, so a shutdown
// does not lose work an operator was told had been accepted without saying so.
func (q *queue[T]) discard() {
	for {
		select {
		case <-q.items:
			q.dropped.Add(1)
		default:
			return
		}
	}
}

// dropCount returns how many items this queue has lost.
func (q *queue[T]) dropCount() int64 { return q.dropped.Load() }

// close stops the drain loop and refuses further offers. Calling it twice is
// safe, because a stop can come from an operator action and from shutdown.
func (q *queue[T]) close() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return
	}

	q.closed = true
	close(q.done)
}
