package stream

import (
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// channel is one origin's stream: its minter, sequence, replay window and
// subscribers. Membership is the authorization decision, already made.
//
// The subscriber slice is immutable once published, so the read costs one
// atomic load and takes no lock.
type channel struct {
	origin event.Origin
	// minter is per channel because a ULID is unique per source and the dedup
	// key downstream is source plus id. Per channel it is also uncontended.
	minter ref.Minter
	seq    atomic.Uint64
	hist   history
	// last is the newest telemetry value per key, which the replay window does
	// not hold. It is what a reader attached to this origin after it started
	// publishing is given, so a machine that reports a value and then goes
	// quiet still reaches that reader.
	last    retained
	mu      sync.Mutex
	readers atomic.Pointer[[]*Subscription]
}

func newChannel(origin event.Origin, events, maxBytes int) *channel {
	c := &channel{origin: origin, hist: newHistory(events, maxBytes)}
	c.readers.Store(&[]*Subscription{})

	return c
}

// subscribers returns the current slice. The caller must not modify it.
func (c *channel) subscribers() []*Subscription {
	if s := c.readers.Load(); s != nil {
		return *s
	}

	return nil
}

// attach adds a subscriber. Writers are rare enough that copying the slice is
// cheaper than making every reader synchronise.
func (c *channel) attach(s *Subscription) {
	c.mu.Lock()
	defer c.mu.Unlock()

	current := c.subscribers()
	next := make([]*Subscription, len(current), len(current)+1)
	copy(next, current)
	next = append(next, s)
	c.readers.Store(&next)
}

func (c *channel) detach(s *Subscription) {
	c.mu.Lock()
	defer c.mu.Unlock()

	current := c.subscribers()

	index := slices.Index(current, s)
	if index < 0 {
		return
	}

	next := make([]*Subscription, 0, len(current)-1)
	next = append(next, current[:index]...)
	next = append(next, current[index+1:]...)
	c.readers.Store(&next)
}

// next assigns the identity and sequence of the next event on this stream.
// Sequence is monotonic per source and assigned here, at the point the event
// enters the hub, so a printer that reconnects does not restart the count.
func (c *channel) next(now time.Time) (ref.UID, uint64, error) {
	id, err := c.minter.Mint(now)
	if err != nil {
		return ref.UID{}, 0, fmt.Errorf("mint event id for %s: %w", c.origin, err)
	}

	return id, c.seq.Add(1), nil
}
