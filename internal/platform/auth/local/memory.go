package local

import (
	"sync"
	"time"
)

// ReturnMemoryEvery is the shortest time between two returns of memory to the
// operating system after password hashes.
//
// A return is a full garbage collection, so it is bounded however many logins
// arrive. It runs on its own goroutine, and a person logging in does not wait
// for it.
const ReturnMemoryEvery = time.Second

// memoryReturner gives the memory of finished argon2id passes back to the
// operating system.
//
// One pass fills a block of the configured argon2id memory, 19 MiB in a normal
// build. When the pass ends the block is garbage. Go keeps the freed pages and
// gives them back to the operating system only after a minute or two, so every
// login would leave the server a block larger for that long.
//
// request asks for a return after a pass. The return runs at once, or when
// ReturnMemoryEvery has passed since the last one. A burst of logins then costs
// one return at its start and one after its end.
type memoryReturner struct {
	every time.Duration
	// give returns the memory. It is debug.FreeOSMemory outside a test.
	give func()

	mu      sync.Mutex
	last    time.Time
	pending bool
}

func newMemoryReturner(every time.Duration, give func()) *memoryReturner {
	return &memoryReturner{every: every, give: give}
}

// request asks for one return after a pass has ended. A request made while a
// return is already waiting is covered by that return.
func (r *memoryReturner) request() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.pending {
		return
	}

	r.pending = true

	wait := max(r.every-time.Since(r.last), 0)

	time.AfterFunc(wait, r.run)
}

// run clears the waiting mark before it returns the memory. A pass that ends
// during the collection may still hold its block, so its request has to
// schedule the next return and not be dropped.
func (r *memoryReturner) run() {
	r.mu.Lock()
	r.pending = false
	r.last = time.Now()
	r.mu.Unlock()

	r.give()
}
