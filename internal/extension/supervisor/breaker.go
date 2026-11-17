package supervisor

import (
	"sync"
	"time"
)

// BreakerState is the position of one instance's circuit breaker.
type BreakerState uint8

const (
	// BreakerClosed passes calls through and counts outcomes.
	BreakerClosed BreakerState = iota
	// BreakerOpen refuses calls without touching the resource.
	BreakerOpen
	// BreakerHalfOpen admits a bounded number of probes.
	BreakerHalfOpen
)

// String returns the state as it is rendered in conditions, logs and metrics.
func (s BreakerState) String() string {
	switch s {
	case BreakerOpen:
		return "open"
	case BreakerHalfOpen:
		return "half-open"
	case BreakerClosed:
		return "closed"
	}

	return "unknown"
}

// admission is permission to make one call, stamped with the breaker epoch it
// was granted in. An outcome counts only while the breaker has not changed
// state since. A call admitted while the breaker was closed says nothing about
// a breaker that opened while the call was in flight.
type admission struct {
	epoch uint64
	// probe records that this admission took one of the half-open slots, so
	// only the call that took a slot gives one back.
	probe bool
	valid bool
}

// breaker is a rolling-window circuit breaker. It counts by ratio and not by
// consecutive failures, because a remote that fails half its calls is an outage
// a count of consecutive failures never notices.
//
// It reads time.Now and takes no clock. Under testing/synctest the package
// clock is already fake, so a second clock would do the same job twice.
type breaker struct {
	pol BreakerPolicy

	mu    sync.Mutex
	state BreakerState
	// epoch increases on every state change, which is how a late outcome is
	// recognised as stale.
	epoch       uint64
	requests    int
	failures    int
	windowStart time.Time
	openedAt    time.Time
	probedAt    time.Time
	inFlight    int
	successes   int
}

func newBreaker(pol BreakerPolicy) *breaker {
	return &breaker{pol: pol, windowStart: time.Now()}
}

// State returns the current position.
func (b *breaker) State() BreakerState {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.state
}

// allow reports whether a call may proceed, and returns the transition it
// caused so the caller can announce it after releasing the lock.
func (b *breaker) allow() (admission, bool, transition) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	moved := b.rearm(now)

	switch b.state {
	case BreakerClosed:
		b.rollWindow(now)

		return admission{epoch: b.epoch, valid: true}, true, moved
	case BreakerHalfOpen:
		if b.inFlight >= b.pol.Probes {
			return admission{}, false, moved
		}

		b.inFlight++
		b.probedAt = now

		return admission{epoch: b.epoch, probe: true, valid: true}, true, moved
	case BreakerOpen:
		return admission{}, false, moved
	}

	return admission{}, false, moved
}

// record adds one outcome to the breaker. Only a failure the host blames on the
// resource counts. A client error means the remote worked correctly, and
// counting it would trip the breaker on user input.
func (b *breaker) record(granted admission, failed bool) transition {
	b.mu.Lock()
	defer b.mu.Unlock()

	// The breaker moved after this call was admitted, so the outcome describes
	// a state that no longer exists. Counting it would let a late call give
	// back a probe slot it never took, and reopen a breaker its outcome says
	// nothing about.
	if !granted.valid || granted.epoch != b.epoch {
		return transition{}
	}

	now := time.Now()

	switch b.state {
	case BreakerClosed:
		// The window is rolled here as well as in allow, so an outcome
		// arriving after its window expired opens a new window instead of
		// counting against much older calls.
		b.rollWindow(now)

		b.requests++
		if failed {
			b.failures++
		}

		if b.requests >= b.pol.MinRequests && float64(b.failures) >= b.pol.FailureRatio*float64(b.requests) {
			return b.moveTo(BreakerOpen, now)
		}
	case BreakerHalfOpen:
		if granted.probe {
			b.inFlight = max(b.inFlight-1, 0)
		}

		if failed {
			return b.moveTo(BreakerOpen, now)
		}

		b.successes++
		if b.successes >= b.pol.Probes {
			return b.moveTo(BreakerClosed, now)
		}
	case BreakerOpen:
		// Unreachable while the epoch matches, and harmless if it ever is.
	}

	return transition{}
}

// rearm moves the breaker on when a timer has expired. Open becomes half-open
// after the cooldown, and half-open goes back to open when a probe was admitted
// and never returned. Without the second move, one stuck probe would keep the
// breaker half-open, and every later call would be refused although the
// resource may have recovered.
func (b *breaker) rearm(now time.Time) transition {
	switch b.state {
	case BreakerOpen:
		if now.Sub(b.openedAt) >= b.pol.OpenFor {
			return b.moveTo(BreakerHalfOpen, now)
		}
	case BreakerHalfOpen:
		if b.inFlight > 0 && now.Sub(b.probedAt) >= b.pol.ProbeTimeout {
			return b.moveTo(BreakerOpen, now)
		}
	case BreakerClosed:
	}

	return transition{}
}

// rollWindow starts a new counting window once the old one has expired. The
// caller holds the lock.
func (b *breaker) rollWindow(now time.Time) {
	if now.Sub(b.windowStart) >= b.pol.Window {
		b.windowStart, b.requests, b.failures = now, 0, 0
	}
}

// moveTo resets the counters that belong to the state being entered and raises
// the epoch, which invalidates every admission granted before it. The caller
// holds the lock.
func (b *breaker) moveTo(to BreakerState, now time.Time) transition {
	from := b.state
	if from == to {
		return transition{}
	}

	b.state = to
	b.epoch++

	switch to {
	case BreakerOpen:
		b.openedAt = now
		b.inFlight, b.successes = 0, 0
	case BreakerHalfOpen:
		b.probedAt = now
		b.inFlight, b.successes = 0, 0
	case BreakerClosed:
		b.windowStart, b.requests, b.failures = now, 0, 0
		b.inFlight, b.successes = 0, 0
	}

	return transition{from: from, to: to, moved: true}
}

// transition is one breaker state change, returned out of the lock so the
// callback that writes a condition cannot deadlock against the breaker.
type transition struct {
	from  BreakerState
	to    BreakerState
	moved bool
}
