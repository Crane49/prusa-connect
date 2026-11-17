package node

import "time"

// Clock gives this package the current time and its timers. A program uses
// SystemClock. A test can pass a clock that it controls.
type Clock interface {
	// Now returns the current time.
	Now() time.Time
	// After returns a channel that receives once d has passed.
	After(d time.Duration) <-chan time.Time
}

// SystemClock is the Clock that calls time.Now and time.After.
type SystemClock struct{}

// Now returns time.Now().
func (SystemClock) Now() time.Time { return time.Now() }

// After returns time.After(d).
func (SystemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
