package store_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prusa3d/buddy-debug-logs/internal/store"
)

// fill writes one syslog line at each of the given minutes after the clock's
// start, and leaves the clock at the last.
func fill(t *testing.T, s *store.Store, c *clock, minutes ...int) {
	t.Helper()

	origin := c.at

	for _, minute := range minutes {
		c.at = origin.Add(time.Duration(minute) * time.Minute)
		if err := s.Append(store.Syslog, line(c, time.Duration(minute).String())); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
}

// readAll is every syslog line the store holds, oldest first.
func readAll(t *testing.T, s *store.Store) []store.Entry {
	t.Helper()

	var out []store.Entry

	for _, name := range s.Segments(store.Syslog) {
		entries, err := s.ReadSegment(store.Syslog, name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}

		out = append(out, entries...)
	}

	return out
}

// Removing everything empties the stream, and the next line opens a new
// segment.
func TestAPurgeOfEverythingStartsAgain(t *testing.T) {
	t.Parallel()

	c := start()
	s, _ := open(t, c, roomy())
	fill(t, s, c, 0, 30, 70, 130)

	purged, err := s.Purge(store.Streams(), time.Time{})
	if err != nil {
		t.Fatalf("purge: %v", err)
	}

	if got := purged[store.Syslog]; got.Records != 4 || got.Segments != 3 || got.Bytes == 0 {
		t.Errorf("purged = %+v, want 4 records in 3 segments", got)
	}

	if left := readAll(t, s); len(left) != 0 {
		t.Fatalf("%d lines left", len(left))
	}

	fill(t, s, c, 140)

	if left := readAll(t, s); len(left) != 1 {
		t.Errorf("%d lines after a new one, want 1", len(left))
	}
}

// A cut inside an hour removes the older segments whole and writes the
// segment that holds the cut again with the newer lines only.
func TestAPurgeKeepsTheNewestHours(t *testing.T) {
	t.Parallel()

	c := start()
	s, _ := open(t, c, roomy())
	fill(t, s, c, 0, 30, 70, 100, 130)

	// The clock stands at 16:10, and the cut at 15:20 falls inside the hour
	// that holds 15:10 and 15:40.
	cut := c.at.Add(-50 * time.Minute)

	purged, err := s.Purge([]store.Stream{store.Syslog}, cut)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}

	if got := purged[store.Syslog]; got.Records != 3 || got.Segments != 1 {
		t.Errorf("purged = %+v, want 3 records, 1 segment whole", got)
	}

	left := readAll(t, s)
	if len(left) != 2 {
		t.Fatalf("%d lines left, want the two since 15:20", len(left))
	}

	for _, entry := range left {
		if entry.Record.At.Before(cut) {
			t.Errorf("a line from %s is older than the cut", entry.Record.At)
		}
	}
}

// A purge of one stream leaves the other alone.
func TestAPurgeOfOneStreamLeavesTheOther(t *testing.T) {
	t.Parallel()

	c := start()
	s, _ := open(t, c, roomy())
	fill(t, s, c, 0)

	if err := s.Append(store.Metrics, store.Record{At: c.at, MAC: "aa:bb:cc:dd:ee:ff", Name: "temp_bed"}); err != nil {
		t.Fatalf("append: %v", err)
	}

	if _, err := s.Purge([]store.Stream{store.Metrics}, time.Time{}); err != nil {
		t.Fatalf("purge: %v", err)
	}

	if left := readAll(t, s); len(left) != 1 {
		t.Errorf("%d syslog lines left, want 1", len(left))
	}

	if names := s.Segments(store.Metrics); len(names) != 0 {
		t.Errorf("metrics segments left: %v", names)
	}
}

// A purge that rewrites a segment moves the records in it, so a cursor
// written before is refused with ErrStale on every read, and one written
// after it is read.
func TestACursorWrittenBeforeAPurgeIsStale(t *testing.T) {
	t.Parallel()

	c := start()
	s, _ := open(t, c, roomy())
	fill(t, s, c, 0, 10, 20, 30)

	all := func(store.Record) bool { return true }

	page, _, err := s.Newest(store.Syslog, store.Window{}, all, nil, 2)
	if err != nil || len(page) != 2 {
		t.Fatalf("newest = %d, %v", len(page), err)
	}

	cursor, err := store.ParsePosition(page[1].Position.String())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if _, err = s.Purge([]store.Stream{store.Syslog}, c.at.Add(-15*time.Minute)); err != nil {
		t.Fatalf("purge: %v", err)
	}

	if _, _, err = s.Newest(store.Syslog, store.Window{}, all, &cursor, 10); !errors.Is(err, store.ErrStale) {
		t.Errorf("newest before the cursor: %v, want ErrStale", err)
	}

	if _, _, err = s.After(store.Syslog, all, cursor, 10); !errors.Is(err, store.ErrStale) {
		t.Errorf("after the cursor: %v, want ErrStale", err)
	}

	if err = s.Scan(store.Syslog, store.Window{}, &cursor, func(store.Entry) bool { return true }); !errors.Is(
		err, store.ErrStale,
	) {
		t.Errorf("scan from the cursor: %v, want ErrStale", err)
	}

	again, _, err := s.Newest(store.Syslog, store.Window{}, all, nil, 1)
	if err != nil || len(again) != 1 {
		t.Fatalf("newest after the purge = %d, %v", len(again), err)
	}

	if _, _, err = s.Newest(store.Syslog, store.Window{}, all, &again[0].Position, 10); err != nil {
		t.Errorf("a cursor written after the purge: %v", err)
	}
}

// A store opened again refuses a cursor of the run before, which a purge in
// between may have moved.
func TestACursorOfAnEarlierRunIsStale(t *testing.T) {
	t.Parallel()

	c := start()
	root := t.TempDir()

	first, err := store.Open(root, c.now, roomy())
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	fill(t, first, c, 0, 10)

	all := func(store.Record) bool { return true }

	page, _, err := first.Newest(store.Syslog, store.Window{}, all, nil, 1)
	if err != nil || len(page) != 1 {
		t.Fatalf("newest = %d, %v", len(page), err)
	}

	if err = first.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	second, _ := reopen(t, root, c)

	if _, _, err = second.After(store.Syslog, all, page[0].Position, 10); !errors.Is(err, store.ErrStale) {
		t.Errorf("after a cursor of the run before: %v, want ErrStale", err)
	}
}

func reopen(t *testing.T, root string, c *clock) (*store.Store, string) {
	t.Helper()

	s, err := store.Open(root, c.now, roomy())
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() { _ = s.Close() })

	return s, root
}

// Appends go on while purges rewrite the segments, and afterwards the room the
// store counts is what the files on disk take.
func TestAppendsGoOnWhileAPurgeRuns(t *testing.T) {
	t.Parallel()

	c := start()
	s, root := open(t, c, roomy())
	fill(t, s, c, 0, 10, 20, 30, 40, 50, 70, 90)

	done := make(chan struct{})

	go func() {
		defer close(done)

		for range 20 {
			if _, err := s.Purge([]store.Stream{store.Syslog}, c.at.Add(-30*time.Minute)); err != nil {
				t.Errorf("purge: %v", err)

				return
			}
		}
	}()

	for range 500 {
		if err := s.Append(store.Syslog, line(c, "during")); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	<-done

	var onDisk int64

	for _, name := range s.Segments(store.Syslog) {
		info, err := os.Stat(filepath.Join(root, string(store.Syslog), name))
		if err != nil {
			t.Fatalf("a counted segment is not on disk: %v", err)
		}

		onDisk += info.Size()
	}

	if counted := s.Usage()[store.Syslog].Bytes; counted != onDisk {
		t.Errorf("the store counts %d bytes and the disk holds %d", counted, onDisk)
	}

	if n := len(readAll(t, s)); n < 500 {
		t.Errorf("%d lines left, want at least the 500 appended during the purges", n)
	}
}
