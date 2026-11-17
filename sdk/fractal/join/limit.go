package join

import (
	"sync"
	"time"
)

// limiter limits how many join requests a Parent evaluates at the same time,
// and how many it accepts per second. A limit of zero or below is off.
type limiter struct {
	mu          sync.Mutex
	maxInFlight int
	inFlight    int
	rate        float64
	burst       float64
	tokens      float64
	last        time.Time
}

// newLimiter builds a limiter with a full bucket. A burst of zero or below
// becomes one when rate is set.
func newLimiter(maxInFlight int, rate float64, burst int, now time.Time) *limiter {
	b := float64(burst)
	if rate > 0 && b <= 0 {
		b = 1
	}
	return &limiter{
		maxInFlight: maxInFlight,
		rate:        rate,
		burst:       b,
		tokens:      b,
		last:        now,
	}
}

// acquire takes one slot and one token. The caller runs release when the
// evaluation ends. ok is false when either limit is reached.
func (l *limiter) acquire(now time.Time) (release func(), ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.maxInFlight > 0 && l.inFlight >= l.maxInFlight {
		return nil, false
	}
	if !l.takeToken(now) {
		return nil, false
	}
	l.inFlight++
	return l.done, true
}

// takeToken refills the bucket for the time since the last call, then takes
// one token. The caller must hold the lock.
func (l *limiter) takeToken(now time.Time) bool {
	if l.rate <= 0 {
		return true
	}
	if elapsed := now.Sub(l.last); elapsed > 0 {
		l.tokens = min(l.burst, l.tokens+elapsed.Seconds()*l.rate)
		l.last = now
	}
	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}

// done counts one request fewer in evaluation.
func (l *limiter) done() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.inFlight--
}
