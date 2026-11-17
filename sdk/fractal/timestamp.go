package fractal

import (
	"math"
	"time"
)

// Timestamp is a point in time in milliseconds since the unix epoch,
// 1970-01-01 UTC. Every time field in the protocol has this form.
type Timestamp uint64

// TimestampOf converts a time to a Timestamp. A Timestamp cannot express a time
// before the unix epoch, so such a time converts to 0.
func TimestampOf(t time.Time) Timestamp {
	ms := t.UnixMilli()
	if ms < 0 {
		return 0
	}
	return Timestamp(ms)
}

// Time converts the timestamp to a time.Time in UTC. A timestamp above
// math.MaxInt64 milliseconds converts to the time at math.MaxInt64
// milliseconds.
func (ts Timestamp) Time() time.Time {
	ms := min(uint64(ts), math.MaxInt64)
	return time.UnixMilli(int64(ms)).UTC()
}

// String returns the timestamp in RFC 3339 format with milliseconds.
func (ts Timestamp) String() string {
	return ts.Time().Format("2006-01-02T15:04:05.000Z07:00")
}
