package stream

import (
	"maps"
	"sync"
	"sync/atomic"
)

// maxRetainedKeys bounds how many telemetry keys one origin keeps a last value
// for. A driver reports a fixed set of paths, so this only has to sit above
// that set; past it a new key is not kept and the origin's memory stops
// growing.
const maxRetainedKeys = 256

// retained is the newest telemetry value per key on one origin.
//
// It exists because telemetry has no replay window. A reader attached to an
// origin after that origin started publishing would otherwise show nothing
// about the machine until it sampled again, and a machine that reports a value
// once and then goes quiet would never be shown at all.
//
// The publish path takes no lock and allocates nothing for a key already held.
// Each key owns an atomic pointer, and a publication swaps the value behind it.
// The slots themselves are a map replaced wholesale under the mutex, which only
// a key nobody has reported before reaches: a driver's key set is fixed, so that
// is once per path per origin.
type retained struct {
	slots atomic.Pointer[map[string]*atomic.Pointer[Message]]
	mu    sync.Mutex
}

// keep records m as the newest value of its key. It runs on the publish path.
func (r *retained) keep(m *Message) {
	if held := r.slots.Load(); held != nil {
		if slot, known := (*held)[m.key]; known {
			slot.Store(m)

			return
		}

		if len(*held) >= maxRetainedKeys {
			return
		}
	}

	r.add(m)
}

// add makes a slot for a key this origin has not reported before and puts the
// first value in it. It is off the steady-state path: the map is copied so that
// every reader outside the lock holds one nothing writes to.
func (r *retained) add(m *Message) {
	r.mu.Lock()
	defer r.mu.Unlock()

	held := r.slots.Load()

	next := make(map[string]*atomic.Pointer[Message], maxRetainedKeys)
	if held != nil {
		if slot, known := (*held)[m.key]; known {
			slot.Store(m)

			return
		}

		if len(*held) >= maxRetainedKeys {
			return
		}

		maps.Copy(next, *held)
	}

	slot := &atomic.Pointer[Message]{}
	slot.Store(m)
	next[m.key] = slot

	r.slots.Store(&next)
}

// since returns the values a reader authorized at this generation may see: the
// newest value of each key published at or after the generation its grant began
// at.
//
// The floor is the one grantedSince applies to the durable window. A grant is
// permission to read from now, so a reading taken before it was written is not
// handed over, however recent it is.
func (r *retained) since(generation uint64) []*Message {
	held := r.slots.Load()
	if held == nil {
		return nil
	}

	out := make([]*Message, 0, len(*held))

	for _, slot := range *held {
		if m := slot.Load(); m != nil && m.aclEpoch >= generation {
			out = append(out, m)
		}
	}

	return out
}

// count is how many keys this origin holds a value for.
func (r *retained) count() int {
	if held := r.slots.Load(); held != nil {
		return len(*held)
	}

	return 0
}
