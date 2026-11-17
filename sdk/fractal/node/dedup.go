package node

import (
	"context"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// The values NewMemoryDedup uses when an argument is zero or less.
const (
	// DefaultDedupWindow is how long a MemoryDedup keeps an id. A deployment
	// should set the window to the longest time a request can take to expire
	// plus the longest outage it expects.
	DefaultDedupWindow = 5 * time.Minute
	// DefaultMaxDedupEntries is the highest number of ids a MemoryDedup keeps
	// for executed requests, and the same number again for processed answers.
	DefaultMaxDedupEntries = 10000
)

// ExecutedStore stores the encoded answer to each method invocation this node
// ran, by the id of the request. For an id in the store, the engine sends the
// stored answer and does not run the method again.
type ExecutedStore interface {
	// Executed returns the encoded RES stored for id. ok is false when the
	// store has no record for id.
	Executed(ctx context.Context, id fractal.RPCID) (res []byte, ok bool, err error)
	// RecordExecuted stores the encoded RES for id. A store on disk must
	// return only when a restart does not lose the record.
	RecordExecuted(ctx context.Context, id fractal.RPCID, res []byte) error
}

// ProcessedStore stores the id of each RES this node accepted for a call it
// made. The engine drops a RES whose id is in the store.
type ProcessedStore interface {
	// Processed reports whether id is in the store.
	Processed(ctx context.Context, id fractal.RPCID) (bool, error)
	// RecordProcessed stores id.
	RecordProcessed(ctx context.Context, id fractal.RPCID) error
}

// Resetter is an optional interface of an ExecutedStore and a ProcessedStore.
// The engine calls Reset when SetPath changes the Node Path of this node,
// because the records belong to messages addressed to the old path.
type Resetter interface {
	// Reset deletes every record.
	Reset()
}

// dedupEntry is the record of one id.
type dedupEntry struct {
	res     []byte
	written time.Time
}

// idSet keeps ids for the length of window and at most limit of them. When
// full it removes the oldest id. order lists the ids from oldest to newest.
type idSet struct {
	mu      sync.Mutex
	entries map[fractal.RPCID]dedupEntry
	order   []fractal.RPCID
	window  time.Duration
	limit   int
}

// newIDSet builds an empty set with the given window and limit.
func newIDSet(window time.Duration, limit int) *idSet {
	return &idSet{entries: make(map[fractal.RPCID]dedupEntry), window: window, limit: limit}
}

// get returns the record of one id. ok is false when the set has no record, or
// the record is older than the window.
func (s *idSet) get(now time.Time, id fractal.RPCID) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok || now.Sub(e.written) > s.window {
		return nil, false
	}
	return e.res, true
}

// put stores one id. It then removes every record older than the window, and
// the oldest records while the set has more than limit of them.
func (s *idSet) put(now time.Time, id fractal.RPCID, res []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[id]; !ok {
		s.order = append(s.order, id)
	}
	s.entries[id] = dedupEntry{res: res, written: now}
	s.expire(now)
	for len(s.order) > s.limit {
		s.evictOldest()
	}
}

// expire removes every record older than the window.
func (s *idSet) expire(now time.Time) {
	for len(s.order) > 0 {
		e, ok := s.entries[s.order[0]]
		if ok && now.Sub(e.written) <= s.window {
			return
		}
		s.evictOldest()
	}
}

// evictOldest removes the first id in order, which is the oldest.
func (s *idSet) evictOldest() {
	oldest := s.order[0]
	s.order = s.order[1:]
	delete(s.entries, oldest)
}

// reset deletes every record.
func (s *idSet) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = make(map[fractal.RPCID]dedupEntry)
	s.order = nil
}

// len returns the number of ids in the set. It includes ids older than the
// window that put has not removed yet.
func (s *idSet) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.order)
}

// MemoryDedup is an ExecutedStore, a ProcessedStore and a Resetter that keeps
// its records in memory. It is safe for concurrent use. A restart loses the
// records, and a request that arrives again then runs its method a second
// time. Supply durable stores in Config.Executed and Config.Processed to
// avoid that.
type MemoryDedup struct {
	clock     Clock
	executed  *idSet
	processed *idSet
}

// NewMemoryDedup builds an empty store. window is how long it keeps an id, and
// zero or less means DefaultDedupWindow. maxEntries is the highest number of
// ids it keeps of each kind, and zero or less means DefaultMaxDedupEntries. A
// nil clock means SystemClock.
func NewMemoryDedup(clock Clock, window time.Duration, maxEntries int) *MemoryDedup {
	if clock == nil {
		clock = SystemClock{}
	}
	if window <= 0 {
		window = DefaultDedupWindow
	}
	if maxEntries <= 0 {
		maxEntries = DefaultMaxDedupEntries
	}
	return &MemoryDedup{
		clock:     clock,
		executed:  newIDSet(window, maxEntries),
		processed: newIDSet(window, maxEntries),
	}
}

// Executed returns the encoded RES stored for id. ok is false when there is no
// record or it is older than the window. The error is always nil.
func (d *MemoryDedup) Executed(_ context.Context, id fractal.RPCID) ([]byte, bool, error) {
	res, ok := d.executed.get(d.clock.Now(), id)
	return res, ok, nil
}

// RecordExecuted stores the encoded RES for id. The error is always nil.
func (d *MemoryDedup) RecordExecuted(_ context.Context, id fractal.RPCID, res []byte) error {
	d.executed.put(d.clock.Now(), id, res)
	return nil
}

// Processed reports whether id is in the store and inside the window. The
// error is always nil.
func (d *MemoryDedup) Processed(_ context.Context, id fractal.RPCID) (bool, error) {
	_, ok := d.processed.get(d.clock.Now(), id)
	return ok, nil
}

// RecordProcessed stores id. The error is always nil.
func (d *MemoryDedup) RecordProcessed(_ context.Context, id fractal.RPCID) error {
	d.processed.put(d.clock.Now(), id, nil)
	return nil
}

// Reset deletes every executed record and every processed id.
func (d *MemoryDedup) Reset() {
	d.executed.reset()
	d.processed.reset()
}

// Len returns the number of executed records and the number of processed ids
// in the store.
func (d *MemoryDedup) Len() (executed, processed int) {
	return d.executed.len(), d.processed.len()
}
