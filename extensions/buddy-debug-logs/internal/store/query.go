package store

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ErrPosition reports a cursor this package did not write.
var ErrPosition = errors.New("store: not a cursor this module wrote")

// ErrStale reports a cursor written before a purge moved the records, or by
// an earlier run of the module. The caller reads again from the newest
// record.
var ErrStale = errors.New("store: the records moved since this cursor was written")

// Position is where one record is: its segment, the byte it starts at, and
// the store's generation when it was read. Positions sort in the order the
// records were written.
type Position struct {
	Segment    string
	Offset     int64
	Generation uint64
}

// String is the cursor a caller passes back, such as
// 000000000042-2026101914.jsonl:4096:9f3a61c2d0e4b857.
func (p Position) String() string {
	return p.Segment + ":" + strconv.FormatInt(p.Offset, 10) + ":" + strconv.FormatUint(p.Generation, 16)
}

// ParsePosition reads a cursor back.
func ParsePosition(cursor string) (Position, error) {
	name, rest, found := strings.Cut(cursor, ":")
	if !found || !validSegment(name) {
		return Position{}, fmt.Errorf("%w: %q", ErrPosition, cursor)
	}

	offset, generation, found := strings.Cut(rest, ":")
	if !found {
		return Position{}, fmt.Errorf("%w: %q", ErrPosition, cursor)
	}

	at, err := strconv.ParseInt(offset, 10, 64)
	if err != nil || at < 0 {
		return Position{}, fmt.Errorf("%w: %q", ErrPosition, cursor)
	}

	held, err := strconv.ParseUint(generation, 16, 64)
	if err != nil {
		return Position{}, fmt.Errorf("%w: %q", ErrPosition, cursor)
	}

	return Position{Segment: name, Offset: at, Generation: held}, nil
}

// current refuses a position of another generation.
func (s *Store) current(p *Position) error {
	if p != nil && p.Generation != s.generation.Load() {
		return ErrStale
	}

	return nil
}

// Before reports whether p was written before q. Segments are compared by
// their numbers, not by their names as text.
func (p Position) Before(q Position) bool {
	if c := compareSegments(p.Segment, q.Segment); c != 0 {
		return c < 0
	}

	return p.Offset < q.Offset
}

// Window is the time range a query reads. A zero end is open.
type Window struct {
	Since time.Time
	Until time.Time
}

// Holds reports whether a record that arrived at t is in the window.
func (w Window) Holds(t time.Time) bool {
	if !w.Since.IsZero() && t.Before(w.Since) {
		return false
	}

	return w.Until.IsZero() || t.Before(w.Until)
}

// mayHold reports whether a segment that started at the hour start can hold
// a record in the window. A segment holds the records of its hour.
func (w Window) mayHold(start time.Time) bool {
	if w.past(start) {
		return false
	}

	return w.Until.IsZero() || start.Before(w.Until)
}

// Newest answers up to limit records that match, newest first, from before
// the cursor or from the newest record when before is nil. more says whether
// older matching records may remain.
func (s *Store) Newest(
	stream Stream, window Window, match func(Record) bool, before *Position, limit int,
) ([]Entry, bool, error) {
	if err := s.current(before); err != nil {
		return nil, false, err
	}

	return s.newest(stream, window, match, before, limit)
}

func (s *Store) newest(
	stream Stream, window Window, match func(Record) bool, before *Position, limit int,
) (entries []Entry, more bool, err error) {
	for _, name := range slices.Backward(s.Segments(stream)) {
		if before != nil && compareSegments(name, before.Segment) > 0 {
			continue
		}

		// A clock that stepped back gives a newer segment an earlier hour, so
		// a segment outside the window is skipped and the older ones are
		// still read.
		if start, ok := segmentHour(name); ok && !window.mayHold(start) {
			continue
		}

		read, readErr := s.ReadSegment(stream, name)
		if readErr != nil {
			return nil, false, readErr
		}

		for _, entry := range slices.Backward(read) {
			if !wanted(entry, window, match, before) {
				continue
			}

			if len(entries) == limit {
				return entries, true, nil
			}

			entries = append(entries, entry)
		}
	}

	return entries, false, nil
}

// past reports whether a segment whose records arrived in the hour that
// starts at start ends before the window starts.
func (w Window) past(start time.Time) bool {
	return !w.Since.IsZero() && start.Add(time.Hour).Before(w.Since)
}

// wanted reports whether an entry is older than the cursor, in the window
// and matched.
func wanted(entry Entry, window Window, match func(Record) bool, before *Position) bool {
	if before != nil && !entry.Position.Before(*before) {
		return false
	}

	return window.Holds(entry.Record.At) && match(entry.Record)
}

// After answers up to limit matching records written after the cursor,
// oldest first, so a caller that polls with the newest cursor it holds never
// skips one. more says whether newer matching records remain.
func (s *Store) After(
	stream Stream, match func(Record) bool, after Position, limit int,
) (entries []Entry, more bool, err error) {
	if err = s.current(&after); err != nil {
		return nil, false, err
	}

	for _, name := range s.Segments(stream) {
		if compareSegments(name, after.Segment) < 0 {
			continue
		}

		read, readErr := s.ReadSegment(stream, name)
		if readErr != nil {
			return nil, false, readErr
		}

		for _, entry := range read {
			if !after.Before(entry.Position) || !match(entry.Record) {
				continue
			}

			if len(entries) == limit {
				return entries, true, nil
			}

			entries = append(entries, entry)
		}
	}

	return entries, false, nil
}

// Scan calls visit on every record in the window, oldest first, until visit
// answers false.
func (s *Store) Scan(stream Stream, window Window, from *Position, visit func(Entry) bool) error {
	if err := s.current(from); err != nil {
		return err
	}

	for _, name := range s.Segments(stream) {
		if from != nil && compareSegments(name, from.Segment) < 0 {
			continue
		}

		if start, ok := segmentHour(name); ok && !window.mayHold(start) {
			continue
		}

		read, err := s.ReadSegment(stream, name)
		if err != nil {
			return err
		}

		for _, entry := range read {
			if from != nil && entry.Position.Before(*from) {
				continue
			}

			if window.Holds(entry.Record.At) && !visit(entry) {
				return nil
			}
		}
	}

	return nil
}

// Latest is the position of the newest record of a stream, and nil for an
// empty stream.
func (s *Store) Latest(stream Stream) *Position {
	segments := s.Segments(stream)

	for _, name := range slices.Backward(segments) {
		read, err := s.ReadSegment(stream, name)
		if err != nil || len(read) == 0 {
			continue
		}

		at := read[len(read)-1].Position

		return &at
	}

	return nil
}
