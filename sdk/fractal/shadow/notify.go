package shadow

import (
	"sync"
	"sync/atomic"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// DefaultEventBuffer is the channel size of a Subscription when Subscribe gets
// a buffer of zero or less.
const DefaultEventBuffer = 64

// Event reports one change of one shadow interface: a REP was applied, the
// interface became STALE, or RemoveNode or Rebase deleted it.
type Event struct {
	// Key identifies the shadow interface.
	Key Key
	// Status is the status after the change. For a removed interface it is the
	// status the interface had when it was removed.
	Status Status
	// UpdatedAt is the update time of the interface after the change. It is
	// zero for an interface that is EMPTY.
	UpdatedAt fractal.Timestamp
	// Removed is true when RemoveNode or Rebase deleted the interface from the
	// Store.
	Removed bool
}

// Subscription sends the Events of one Store to one reader, on a buffered
// channel. The Store never waits for a reader. When the channel is full, it
// drops the Event and adds one to Missed. A reader that sees Missed grow must
// read the Store again with Get or Subtree. Only Close closes the channel.
type Subscription struct {
	events chan Event
	missed atomic.Uint64
	store  *Store
	once   sync.Once
}

// Events returns the channel the Events arrive on.
func (sub *Subscription) Events() <-chan Event { return sub.events }

// Missed returns how many Events the Store dropped because the channel was
// full. The number never goes down.
func (sub *Subscription) Missed() uint64 { return sub.missed.Load() }

// Close removes the Subscription from the Store and closes the channel. It is
// safe to call more than once and from any goroutine.
func (sub *Subscription) Close() {
	sub.once.Do(func() {
		sub.store.removeSubscription(sub)
		close(sub.events)
	})
}

// Subscribe returns a new Subscription to the Events of the Store. buffer is
// the channel size, and zero or less means DefaultEventBuffer. Call Close when
// the reader stops. Until then the Store keeps sending to the channel.
func (s *Store) Subscribe(buffer int) *Subscription {
	if buffer <= 0 {
		buffer = DefaultEventBuffer
	}
	sub := &Subscription{events: make(chan Event, buffer), store: s}
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	s.subs = append(s.subs, sub)
	s.subCount.Store(int64(len(s.subs)))
	return sub
}

// removeSubscription removes sub from s.subs. When it returns, no publish is
// sending to sub, and Close can close the channel.
func (s *Store) removeSubscription(sub *Subscription) {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	for i, other := range s.subs {
		if other == sub {
			s.subs = append(s.subs[:i], s.subs[i+1:]...)
			break
		}
	}
	s.subCount.Store(int64(len(s.subs)))
}

// publish sends one Event to every Subscription and never blocks. The caller
// must not hold s.mu or the lock of a node.
func (s *Store) publish(ev Event) {
	if s.subCount.Load() == 0 {
		return
	}
	s.subsMu.RLock()
	defer s.subsMu.RUnlock()
	for _, sub := range s.subs {
		select {
		case sub.events <- ev:
		default:
			sub.missed.Add(1)
		}
	}
}

// publishEvents sends the Events one call collected under s.mu. Call it after
// s.mu is released.
func (s *Store) publishEvents(events []Event) {
	for _, ev := range events {
		s.publish(ev)
	}
}

// watching reports whether the Store has a Subscription.
func (s *Store) watching() bool { return s.subCount.Load() > 0 }
