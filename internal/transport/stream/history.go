package stream

import (
	"sync"
	"sync/atomic"
)

// history is one origin's replay window, bounded by a count and a byte budget.
// It belongs to a channel, so an offset counts one origin's events and a
// chatty origin evicts nobody else's window.
//
// Telemetry is absent: a resubscribe gets a fresh value within one window.
type history struct {
	mu       sync.Mutex
	ring     []*Message
	head     uint64
	oldest   uint64
	bytes    int
	maxBytes int
	// latest mirrors head for readers that only want the position, so a
	// subscribe does not queue behind a publish.
	latest atomic.Uint64
}

func newHistory(events, maxBytes int) history {
	return history{ring: make([]*Message, events), maxBytes: maxBytes}
}

// append assigns the message its offset and returns it. Offsets start at one,
// so a cursor naming no offset for an origin means "everything you have". The
// stamp happens under the lock a replay reads through, so a message becomes
// visible and gets its place in one step.
func (h *history) append(m *Message) uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	next := h.head + 1
	if h.oldest == 0 {
		h.oldest = next
	}

	for next-h.oldest >= uint64(len(h.ring)) {
		h.evictOldest()
	}

	m.position.Offset = next
	h.ring[next%uint64(len(h.ring))] = m
	h.bytes += m.Size()
	h.head = next
	h.latest.Store(next)

	for h.bytes > h.maxBytes && h.oldest < h.head {
		h.evictOldest()
	}

	return next
}

// since returns the messages after the given offset, and reports whether the
// gap is unfillable. An offset ahead of the log is unfillable too: it belongs
// to a stream this one is not.
func (h *history) since(after uint64) ([]*Message, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if after > h.head {
		return nil, true
	}

	if h.head == 0 || after == h.head {
		return nil, false
	}

	if h.oldest > 1 && after+1 < h.oldest {
		return nil, true
	}

	return h.afterLocked(after), false
}

// grantedFloor reports the offset a reader authorized at this generation starts
// from: the place just before the oldest retained message the grant covers, and
// the head when it covers none. It is the whole authorization bound on a replay,
// and it is what both replay paths take, so a grant made after an origin was
// already publishing means one thing whether the reader was handed it mid-stream
// or arrived carrying a cursor.
//
// One goroutine publishes for one origin, so generations along the window are
// non-decreasing and the bound is a single offset rather than a filter.
func (h *history) grantedFloor(generation uint64) uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.floorLocked(generation)
}

// grantedSince returns the retained messages published at or after the given
// authorization generation, and the offset a reader starts from ahead of them.
// A grant is permission to read from now, so what predates it is neither
// replayed nor counted, and the reader's place is where it stops.
func (h *history) grantedSince(generation uint64) ([]*Message, uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	floor := h.floorLocked(generation)

	return h.afterLocked(floor), floor
}

func (h *history) floorLocked(generation uint64) uint64 {
	for offset := max(h.oldest, 1); offset <= h.head; offset++ {
		if m := h.ring[offset%uint64(len(h.ring))]; m != nil && m.aclEpoch >= generation {
			return m.position.Offset - 1
		}
	}

	return h.head
}

func (h *history) afterLocked(after uint64) []*Message {
	if after >= h.head {
		return nil
	}

	out := make([]*Message, 0, h.head-after)

	for offset := max(after+1, h.oldest); offset <= h.head; offset++ {
		if m := h.ring[offset%uint64(len(h.ring))]; m != nil {
			out = append(out, m)
		}
	}

	return out
}

// position returns the offset of this origin's newest durable event, without
// taking the lock the replay window is guarded by.
func (h *history) position() uint64 { return h.latest.Load() }

func (h *history) evictOldest() {
	index := h.oldest % uint64(len(h.ring))
	if m := h.ring[index]; m != nil {
		h.bytes -= m.Size()
		h.ring[index] = nil
	}

	h.oldest++
}
