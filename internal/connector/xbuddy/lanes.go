package xbuddy

import (
	"slices"
	"sync"
	"time"
)

// personLull is how long background work waits after a person's command.
// A person's flow is several commands in a row, such as a send's name check,
// offer and landing check, and the next one is queued a moment after the last
// is answered. A listing read taken in that moment holds the printer for up to
// 15 s on a full USB drive, once for every step.
const personLull = 250 * time.Millisecond

// commandQueue is the one queue of commands waiting for the printer, in the
// order they were asked. A command marked lane.Background is taken
// only when no person's command waits, and promoting it clears the mark where
// it stands, so a command is in the queue once and is sent once.
//
// It is bounded per kind: a printer that is away for a long time fails
// commands rather than growing a queue nobody is emptying, and background
// reads never take the room a person's command needs.
type commandQueue struct {
	mu         sync.Mutex
	calls      []*call
	depth      int
	persons    int
	background int
	// wake holds one signal that the queue changed, which is all a pump that
	// reads the queue again needs.
	wake chan struct{}
}

func newCommandQueue(depth int) *commandQueue {
	return &commandQueue{depth: depth, wake: make(chan struct{}, 1)}
}

// push puts a command at the end and reports false when its kind is full.
func (q *commandQueue) push(pending *call) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	count := &q.persons
	if pending.background {
		count = &q.background
	}

	if *count >= q.depth {
		return false
	}

	*count++
	q.calls = append(q.calls, pending)
	q.signal()

	return true
}

// promote makes a waiting background command a person's. A command already
// taken, or a full person's count, leaves it where it is.
func (q *commandQueue) promote(pending *call) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !pending.background || q.persons >= q.depth || !slices.Contains(q.calls, pending) {
		return
	}

	pending.background = false
	q.background--
	q.persons++
	q.signal()
}

// take removes the oldest person's command, or the oldest background one when
// background is true and no person's command waits. person says which it was.
func (q *commandQueue) take(background bool) (pending *call, person bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	at := -1

	for i, c := range q.calls {
		if !c.background {
			at = i

			break
		}

		if background && at < 0 {
			at = i
		}
	}

	if at < 0 {
		return nil, false
	}

	pending = q.calls[at]
	q.calls = append(q.calls[:at], q.calls[at+1:]...)

	if pending.background {
		q.background--
	} else {
		q.persons--
	}

	return pending, !pending.background
}

// drain empties the queue and hands back what was in it.
func (q *commandQueue) drain() []*call {
	q.mu.Lock()
	defer q.mu.Unlock()

	calls := q.calls
	q.calls, q.persons, q.background = nil, 0, 0

	return calls
}

func (q *commandQueue) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	return len(q.calls)
}

func (q *commandQueue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// pump takes one command at a time. Serialising here is what makes "one command
// in flight per printer" true by construction rather than by a lock somebody can
// forget.
//
// Background work is taken only while no person's command waits, no transfer
// is open and the last person's command is personLull behind.
func (s *Session) pump() {
	var lull <-chan time.Time

	for {
		if pending, person := s.queue.take(lull == nil && !s.transferring()); pending != nil {
			s.deliver(pending)

			if person {
				lull = time.After(personLull)
			}

			continue
		}

		select {
		case <-s.done:
			return
		case <-s.queue.wake:
		case <-lull:
			lull = nil
		case <-s.transferMoved:
		}
	}
}

// transferring is whether a transfer is open. A send is a person's, and its
// landing check follows TRANSFER_FINISHED at once, so no background read is
// started while one runs.
func (s *Session) transferring() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.transfer != nil
}

// transferMovedNow wakes the pump. The channel holds one wake, which is all a
// pump that rereads transferring needs.
func (s *Session) transferMovedNow() {
	select {
	case s.transferMoved <- struct{}{}:
	default:
	}
}
