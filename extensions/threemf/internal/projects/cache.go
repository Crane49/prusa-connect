package projects

import (
	"context"
	"sync"
	"unsafe"
)

// key is one version of one file, or one object of one version of a file. The
// size and the modification time are part of it, so a file replaced under the
// same name is another key and nothing is ever invalidated on a write.
type key struct {
	storage  string
	path     string
	size     int64
	modified int64
	// object is the object of a mesh, and -1 for a whole project.
	object int
}

// bytes is how much memory one key holds.
func (k key) bytes() int {
	return int(unsafe.Sizeof(k)) + len(k.storage) + len(k.path)
}

// cache holds one answer per key, within a count of entries and a count of
// bytes, and runs one read per key at a time.
type cache[V any] struct {
	mu      sync.Mutex
	entries map[key]V
	order   []key
	bytes   int
	// maxEntries is how many answers are held at once.
	maxEntries int
	// maxBytes is how much memory is held at once, across every entry. The
	// oldest entries are dropped until the total is under it.
	maxBytes int
	// sizeOf is how much memory one answer holds, its key included.
	sizeOf func(key, V) int
	// keep says whether an answer is worth holding.
	keep func(V) bool
	// reading is the read each key is in right now. A second caller asking
	// for the same file waits for that read instead of starting its own.
	reading map[key]*flight[V]
}

// flight is one read in progress. done is closed when the answer is in.
type flight[V any] struct {
	done   chan struct{}
	answer V
}

func newCache[V any](maxEntries, maxBytes int, sizeOf func(key, V) int, keep func(V) bool) *cache[V] {
	return &cache[V]{
		entries:    make(map[key]V),
		reading:    make(map[key]*flight[V]),
		maxEntries: maxEntries,
		maxBytes:   maxBytes,
		sizeOf:     sizeOf,
		keep:       keep,
	}
}

// once runs read for this key unless the answer is held or another caller is
// getting it, and answers it either way. A caller that leaves while another
// caller's read runs answers the zero value, and the read goes on for whoever
// else waits on it.
func (c *cache[V]) once(ctx context.Context, k key, read func() V) V {
	c.mu.Lock()

	if found, ok := c.entries[k]; ok {
		c.mu.Unlock()

		return found
	}

	if waiting, ok := c.reading[k]; ok {
		c.mu.Unlock()

		select {
		case <-waiting.done:
			return waiting.answer
		case <-ctx.Done():
			var zero V

			return zero
		}
	}

	mine := &flight[V]{done: make(chan struct{})}
	c.reading[k] = mine
	c.mu.Unlock()

	mine.answer = read()

	// The answer is filed and the read retired in one turn of the lock. A
	// caller arriving between the two would otherwise find neither an entry
	// nor a read to wait for, and would start a second read.
	c.mu.Lock()
	if c.keep(mine.answer) {
		c.store(k, mine.answer)
	}

	delete(c.reading, k)
	c.mu.Unlock()
	close(mine.done)

	return mine.answer
}

// store files one answer and drops the oldest entries until the cache is
// inside both of its bounds. The lock is held.
func (c *cache[V]) store(k key, answer V) {
	if _, seen := c.entries[k]; seen {
		return
	}

	c.entries[k] = answer
	c.order = append(c.order, k)
	c.bytes += c.sizeOf(k, answer)

	for len(c.order) > c.maxEntries || (c.bytes > c.maxBytes && len(c.order) > 1) {
		oldest := c.order[0]
		c.order = c.order[1:]

		c.bytes -= c.sizeOf(oldest, c.entries[oldest])
		delete(c.entries, oldest)
	}
}
