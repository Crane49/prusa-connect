package stream

import (
	"sync"

	"github.com/prusa3d/prusa-connect/sdk/event"
)

// conflationKey is what a telemetry value supersedes: the same metric, for the
// same origin, published again.
type conflationKey struct {
	origin event.Origin
	key    string
}

// telemetryQueue holds the newest value per key. A superseded value releases
// its bytes, so the bound is the number of distinct keys and not the lag.
//
// A slow reader is never disconnected over telemetry. A job result is not here.
type telemetryQueue struct {
	mu     sync.Mutex
	latest map[conflationKey]*Message
	order  []conflationKey
	bytes  int
	// maxBytes is set once, at construction, so a reader outside the lock is
	// reading a value nothing writes.
	maxBytes   int
	superseded uint64
	dropped    uint64
}

func newTelemetryQueue(maxBytes int) telemetryQueue {
	return telemetryQueue{latest: make(map[conflationKey]*Message), maxBytes: maxBytes}
}

// put supersedes any queued value for the same key. It never blocks and never
// disconnects.
func (q *telemetryQueue) put(m *Message) {
	k := conflationKey{origin: m.position.Origin, key: m.key}

	q.mu.Lock()
	defer q.mu.Unlock()

	if old, held := q.latest[k]; held {
		q.bytes -= old.Size()
		q.superseded++
	} else {
		q.order = append(q.order, k)
	}

	q.latest[k] = m
	q.bytes += m.Size()

	// A backstop, not the bound: conflation already caps the queue at the
	// distinct keys a subscriber can see.
	for q.bytes > q.maxBytes && len(q.order) > 1 {
		q.evictOldest()
	}
}

// seed puts a value the reader has never been given, and keeps whatever the
// queue already holds for that key.
//
// It is for an origin the reader has just been attached to: the live path may
// already have queued a newer value for the same key while the attach ran, and
// that one is the current reading. Keeping it stops an older retained value
// from overwriting it.
func (q *telemetryQueue) seed(messages []*Message) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for _, m := range messages {
		k := conflationKey{origin: m.position.Origin, key: m.key}
		if _, held := q.latest[k]; held {
			continue
		}

		q.order = append(q.order, k)
		q.latest[k] = m
		q.bytes += m.Size()
	}

	for q.bytes > q.maxBytes && len(q.order) > 1 {
		q.evictOldest()
	}
}

// drain empties the queue in the order the keys first changed since the last
// drain.
func (q *telemetryQueue) drain(out []*Message) []*Message {
	q.mu.Lock()
	defer q.mu.Unlock()

	for _, k := range q.order {
		out = append(out, q.latest[k])
	}

	clear(q.latest)
	q.order = q.order[:0]
	q.bytes = 0

	return out
}

func (q *telemetryQueue) stats() (keys, bytes int, superseded, dropped uint64) {
	q.mu.Lock()
	defer q.mu.Unlock()

	return len(q.order), q.bytes, q.superseded, q.dropped
}

func (q *telemetryQueue) evictOldest() {
	k := q.order[0]
	q.order = q.order[1:]

	if m, held := q.latest[k]; held {
		q.bytes -= m.Size()
		delete(q.latest, k)
		q.dropped++
	}
}

// lane is the part eventQueue and seriesQueue share: an ordered run of
// messages against a byte bound, and the flag that tells the reader the run
// was emptied. The owner holds the lock.
type lane struct {
	items    []*Message
	bytes    int
	maxBytes int
	resync   bool
	dropped  uint64
}

// drain empties the lane and reports whether the reader must resync before
// trusting what it gets next.
func (l *lane) drain(out []*Message) ([]*Message, bool) {
	out = append(out, l.items...)
	clear(l.items)
	l.items = l.items[:0]
	l.bytes = 0

	resync := l.resync
	l.resync = false

	return out, resync
}

// overflow empties the lane and raises the flag.
func (l *lane) overflow() {
	l.dropped += uint64(len(l.items))
	clear(l.items)
	l.items = l.items[:0]
	l.bytes = 0
	l.resync = true
}

// eventQueue is FIFO and lossless up to its bound. Past the bound it does not
// drop quietly. An event stream with a part dropped leaves a client wrong
// without knowing it, so the queue is emptied and the reader is told to
// resync.
type eventQueue struct {
	lane

	mu sync.Mutex
	// accepted is the highest offset handed on per origin. It keeps the
	// offsets in the lane increasing. A publisher that assigned an offset
	// before an attach can enqueue it after the replay that already covered
	// it, and put drops that copy. A resumed reader seeds it from its cursor
	// through expect, because the connection before this one already handed
	// on the offsets the cursor names.
	accepted map[event.Origin]uint64
}

func newEventQueue(maxBytes int) eventQueue {
	return eventQueue{lane: lane{maxBytes: maxBytes}, accepted: make(map[event.Origin]uint64)}
}

// put appends unless the queue is over its bound, in which case everything
// queued is discarded and the reader is flagged for resync. Discarding loses no
// information: a resync replaces the queue's whole contents with authoritative
// state, so keeping the head would only delay the reader learning that.
func (q *eventQueue) put(m *Message) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Already delivered, by a replay that ran while this event was in flight.
	// The offset identifies the event, so this is the same one.
	if m.position.Offset <= q.accepted[m.position.Origin] {
		return
	}

	q.accepted[m.position.Origin] = m.position.Offset
	q.items = append(q.items, m)
	q.bytes += m.Size()

	if q.bytes <= q.maxBytes {
		return
	}

	q.overflow()
}

// expect records an offset the reader already holds, so the live path cannot
// queue an event behind it. Publish assigns an offset before it reads the
// subscriber list, so a publisher inside that fan-out can reach a subscription
// that attached after the offset was minted. A reader whose resume cursor
// names that offset would then get the event a second time.
func (q *eventQueue) expect(origin event.Origin, offset uint64) {
	if offset == 0 {
		return
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	q.accepted[origin] = max(q.accepted[origin], offset)
}

// adopt puts replayed events ahead of everything the live path queued while the
// subscription was being attached, and drops the live copies the replay covers.
// Without it a publish landing mid-attach is queued before older events and a
// reader's offsets go backwards.
func (q *eventQueue) adopt(replayed []*Message) {
	if len(replayed) == 0 {
		return
	}

	covered := make(map[event.Origin]uint64, len(replayed))
	for _, m := range replayed {
		covered[m.position.Origin] = max(covered[m.position.Origin], m.position.Offset)
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	merged := make([]*Message, 0, len(replayed)+len(q.items))
	merged = append(merged, replayed...)

	for _, m := range q.items {
		if offset, seen := covered[m.position.Origin]; seen && m.position.Offset <= offset {
			continue
		}

		merged = append(merged, m)
	}

	q.items = merged
	q.bytes = 0

	for _, m := range merged {
		q.bytes += m.Size()
		q.accepted[m.position.Origin] = max(q.accepted[m.position.Origin], m.position.Offset)
	}

	if q.bytes > q.maxBytes {
		q.overflow()
	}
}

func (q *eventQueue) drain(out []*Message) ([]*Message, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	return q.lane.drain(out)
}

// requireResync flags the reader without queueing anything, for a resume the
// hub cannot honour.
func (q *eventQueue) requireResync() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.resync = true
}

func (q *eventQueue) dropCount() uint64 {
	q.mu.Lock()
	defer q.mu.Unlock()

	return q.dropped
}

func (q *eventQueue) stats() (count, bytes int, dropped uint64, resync bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	return len(q.items), q.bytes, q.dropped, q.resync
}

// dropOrigins forgets every conflated value for an origin the subscriber may no
// longer see. The queue holds the newest value per key with no expiry, so a
// value taken at the moment a grant was withdrawn would otherwise be delivered
// on the next window.
func (q *telemetryQueue) dropOrigins(lost map[event.Origin]struct{}) {
	q.mu.Lock()
	defer q.mu.Unlock()

	kept := q.order[:0]

	for _, k := range q.order {
		if _, withdrawn := lost[k.origin]; !withdrawn {
			kept = append(kept, k)

			continue
		}

		if m, held := q.latest[k]; held {
			q.bytes -= m.Size()
			delete(q.latest, k)
		}
	}

	q.order = kept
}

// dropOrigins removes queued events for an origin the subscriber may no longer
// see. It raises no resync flag: the streams the reader still follows are
// intact.
func (q *eventQueue) dropOrigins(lost map[event.Origin]struct{}) {
	q.mu.Lock()
	defer q.mu.Unlock()

	kept := q.items[:0]
	bytes := 0

	for _, m := range q.items {
		if _, withdrawn := lost[m.position.Origin]; withdrawn {
			continue
		}

		kept = append(kept, m)
		bytes += m.Size()
	}

	// The tail still points at dropped messages, which would keep their
	// payloads alive for as long as the queue's backing array lives.
	clear(q.items[len(kept):])

	q.items = kept
	q.bytes = bytes

	for origin := range lost {
		delete(q.accepted, origin)
	}
}

// seriesQueue keeps every telemetry frame, in arrival order. It is for a
// history store, which is asked later what the series was and can only answer
// with what it was handed. For a store a superseded value is a lost
// measurement. A browser draws only the newest value, so conflation suits a
// browser and not a store.
//
// The cost is the bound. telemetryQueue is bounded by the number of distinct
// keys a subscriber can see, which no slow reader exceeds. This queue is
// bounded by the lag, so a reader that stops reading does exceed it. Past the
// bound it does what eventQueue does: the queue is emptied and the reader is
// told to resync, because nothing downstream can mark a window that silently
// misses readings as incomplete.
type seriesQueue struct {
	lane

	mu sync.Mutex
}

func newSeriesQueue(maxBytes int) seriesQueue {
	return seriesQueue{lane: lane{maxBytes: maxBytes}}
}

// put appends. Nothing is superseded here, so a value leaves unread only when
// the bound is passed.
func (q *seriesQueue) put(m *Message) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.items = append(q.items, m)
	q.bytes += m.Size()

	if q.bytes <= q.maxBytes {
		return
	}

	q.overflow()
}

// drain empties the queue and reports whether readings were discarded since
// the last window. The reader records that as a gap in the series.
func (q *seriesQueue) drain(out []*Message) ([]*Message, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	return q.lane.drain(out)
}

func (q *seriesQueue) stats() (count, bytes int, dropped uint64, resync bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	return len(q.items), q.bytes, q.dropped, q.resync
}

// dropOrigins forgets readings for an origin the subscriber may no longer see.
// Like eventQueue's, it raises no resync flag: the series the reader still
// follows are whole.
func (q *seriesQueue) dropOrigins(lost map[event.Origin]struct{}) {
	q.mu.Lock()
	defer q.mu.Unlock()

	kept := q.items[:0]
	bytes := 0

	for _, m := range q.items {
		if _, withdrawn := lost[m.position.Origin]; withdrawn {
			continue
		}

		kept = append(kept, m)
		bytes += m.Size()
	}

	// The tail still points at dropped messages, which would keep their
	// payloads alive for as long as the queue's backing array lives.
	clear(q.items[len(kept):])

	q.items = kept
	q.bytes = bytes
}
