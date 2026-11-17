package store_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/buddy-debug-logs/internal/store"
)

// clock is a time a test moves by hand.
type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }

func start() *clock { return &clock{at: time.Date(2026, time.October, 19, 14, 0, 0, 0, time.UTC)} }

func open(t *testing.T, c *clock, limits store.Limits) (*store.Store, string) {
	t.Helper()

	root := t.TempDir()

	s, err := store.Open(root, c.now, limits)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() { _ = s.Close() })

	return s, root
}

func line(c *clock, text string) store.Record {
	sev := 6

	return store.Record{At: c.at, MAC: "aa:bb:cc:dd:ee:ff", Severity: &sev, Component: "GUI", Text: text}
}

func roomy() store.Limits {
	return store.Limits{Hours: 24, Bytes: map[store.Stream]int64{store.Syslog: 64 << 20, store.Metrics: 64 << 20}}
}

// A record is read back where it was written, and an hour later the store
// starts a new segment.
func TestARecordIsKeptInTheSegmentOfItsHour(t *testing.T) {
	t.Parallel()

	c := start()
	s, _ := open(t, c, roomy())

	for _, text := range []string{"one", "two"} {
		if err := s.Append(store.Syslog, line(c, text)); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	c.at = c.at.Add(time.Hour)

	if err := s.Append(store.Syslog, line(c, "three")); err != nil {
		t.Fatalf("append: %v", err)
	}

	segments := s.Segments(store.Syslog)
	if strings.Join(segments, ",") != "000000000001-2026101914.jsonl,000000000002-2026101915.jsonl" {
		t.Fatalf("segments = %v", segments)
	}

	read, err := s.ReadSegment(store.Syslog, segments[0])
	if err != nil || len(read) != 2 || read[1].Record.Text != "two" {
		t.Fatalf("read = %+v, %v", read, err)
	}
}

// Newest pages back with the cursor of the last record it answered, and After
// answers what came since, oldest first.
func TestTheCursorPagesBackAndForward(t *testing.T) {
	t.Parallel()

	c := start()
	s, _ := open(t, c, roomy())

	for i := range 5 {
		c.at = c.at.Add(30 * time.Minute)

		if err := s.Append(store.Syslog, line(c, strconv.Itoa(i))); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	all := func(store.Record) bool { return true }

	first, more, err := s.Newest(store.Syslog, store.Window{}, all, nil, 2)
	if err != nil || !more || texts(first) != "4,3" {
		t.Fatalf("first page = %s, more %v, %v", texts(first), more, err)
	}

	cursor, err := store.ParsePosition(first[1].Position.String())
	if err != nil {
		t.Fatalf("parse the cursor: %v", err)
	}

	second, more, err := s.Newest(store.Syslog, store.Window{}, all, &cursor, 10)
	if err != nil || more || texts(second) != "2,1,0" {
		t.Fatalf("second page = %s, more %v, %v", texts(second), more, err)
	}

	newer, more, err := s.After(store.Syslog, all, second[1].Position, 2)
	if err != nil || !more || texts(newer) != "2,3" {
		t.Fatalf("after = %s, more %v, %v", texts(newer), more, err)
	}
}

// A window keeps a query to a time range.
func TestAWindowKeepsAQueryToItsRange(t *testing.T) {
	t.Parallel()

	c := start()
	s, _ := open(t, c, roomy())

	base := c.at

	for i := range 6 {
		c.at = base.Add(time.Duration(i) * time.Hour)

		if err := s.Append(store.Syslog, line(c, strconv.Itoa(i))); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	window := store.Window{Since: base.Add(2 * time.Hour), Until: base.Add(4 * time.Hour)}

	got, _, err := s.Newest(store.Syslog, window, func(store.Record) bool { return true }, nil, 10)
	if err != nil || texts(got) != "3,2" {
		t.Fatalf("got %s, %v", texts(got), err)
	}
}

// A segment older than the retention goes, and the open one stays.
func TestAnOldSegmentIsRemoved(t *testing.T) {
	t.Parallel()

	c := start()
	limits := roomy()
	limits.Hours = 2
	s, _ := open(t, c, limits)

	for range 4 {
		if err := s.Append(store.Syslog, line(c, "x")); err != nil {
			t.Fatalf("append: %v", err)
		}

		c.at = c.at.Add(time.Hour)
	}

	s.Enforce()

	want := "000000000003-2026101916.jsonl,000000000004-2026101917.jsonl"
	if got := s2(s.Segments(store.Syslog)); got != want {
		t.Fatalf("segments = %s", got)
	}
}

// A stream over its room loses its oldest segments first.
func TestAStreamOverItsRoomLosesItsOldestSegments(t *testing.T) {
	t.Parallel()

	c := start()
	limits := store.Limits{Hours: 24, Bytes: map[store.Stream]int64{store.Syslog: 512 << 10, store.Metrics: 1 << 20}}
	s, _ := open(t, c, limits)

	big := strings.Repeat("y", 1000)

	for range 2000 {
		if err := s.Append(store.Syslog, line(c, big)); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	s.Enforce()

	usage := s.Usage()[store.Syslog]
	if usage.Bytes > 512<<10+128<<10 {
		t.Fatalf("the stream holds %d bytes over a room of 512 KiB", usage.Bytes)
	}

	newest, _, err := s.Newest(store.Syslog, store.Window{}, func(store.Record) bool { return true }, nil, 1)
	if err != nil || len(newest) != 1 {
		t.Fatalf("the newest record went: %v", err)
	}
}

// A clock that went back gives the open segment an earlier hour than the
// older ones. The retention still removes the older segments and keeps it.
func TestTheRetentionWorksAfterTheClockWentBack(t *testing.T) {
	t.Parallel()

	c := start()
	limits := store.Limits{Hours: 24, Bytes: map[store.Stream]int64{store.Syslog: 50, store.Metrics: 1 << 20}}
	s, _ := open(t, c, limits)

	for _, hours := range []int{0, 1, -2} {
		c.at = start().at.Add(time.Duration(hours) * time.Hour)

		if err := s.Append(store.Syslog, line(c, "x")); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	s.Enforce()

	if got := s2(s.Segments(store.Syslog)); got != "000000000003-2026101912.jsonl" {
		t.Fatalf("segments = %s, want the open one alone", got)
	}
}

// What was there before a restart is read and counted again.
func TestAStoreOpenedAgainReadsWhatItHeld(t *testing.T) {
	t.Parallel()

	c := start()
	root := t.TempDir()

	first, err := store.Open(root, c.now, roomy())
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if err = first.Append(store.Metrics, store.Record{At: c.at, MAC: "m", Name: "temp_bed"}); err != nil {
		t.Fatalf("append: %v", err)
	}

	if err = first.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	second, err := store.Open(root, c.now, roomy())
	if err != nil {
		t.Fatalf("open again: %v", err)
	}

	defer second.Close()

	if usage := second.Usage()[store.Metrics]; usage.Segments != 1 || usage.Bytes == 0 {
		t.Fatalf("usage = %+v", usage)
	}

	if err = second.Append(store.Metrics, store.Record{At: c.at, MAC: "m", Name: "temp_noz"}); err != nil {
		t.Fatalf("append: %v", err)
	}

	if got := s2(second.Segments(store.Metrics)); got != "000000000001-2026101914.jsonl,000000000002-2026101914.jsonl" {
		t.Fatalf("segments = %s", got)
	}
}

// A line the writer has not finished is skipped, and a file that is not a
// segment is not read.
func TestAHalfWrittenLineIsSkipped(t *testing.T) {
	t.Parallel()

	c := start()
	s, root := open(t, c, roomy())

	if err := s.Append(store.Syslog, line(c, "whole")); err != nil {
		t.Fatalf("append: %v", err)
	}

	if err := s.Sync(); err != nil {
		t.Fatalf("sync: %v", err)
	}

	path := filepath.Join(root, "syslog", "000000000001-2026101914.jsonl")

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if _, err = file.WriteString(`{"t":"2026-10-19T14:00:00Z","mac":"half`); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err = file.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	read, err := s.ReadSegment(store.Syslog, "000000000001-2026101914.jsonl")
	if err != nil || len(read) != 1 {
		t.Fatalf("read %d records, %v", len(read), err)
	}

	if _, err = s.ReadSegment(store.Syslog, "../secret.jsonl"); err == nil {
		t.Fatal("a path outside the stream folder was read")
	}

	if _, err = store.ParsePosition("../x:1"); err == nil {
		t.Fatal("a cursor outside the stream folder was read")
	}
}

// A clock that steps back gives the next segment an earlier hour. The
// segments still keep the order they were written in, so the newest record
// is the one written last and a cursor reads on from where it was.
func TestAClockThatStepsBackKeepsTheWriteOrder(t *testing.T) {
	t.Parallel()

	c := start()
	c.at = c.at.Add(time.Hour)
	s, _ := open(t, c, roomy())

	if err := s.Append(store.Syslog, line(c, "first")); err != nil {
		t.Fatalf("append: %v", err)
	}

	c.at = c.at.Add(-90 * time.Minute)

	if err := s.Append(store.Syslog, line(c, "second")); err != nil {
		t.Fatalf("append: %v", err)
	}

	all := func(store.Record) bool { return true }

	newest, more, err := s.Newest(store.Syslog, store.Window{}, all, nil, 1)
	if err != nil || !more || texts(newest) != "second" {
		t.Fatalf("newest = %s, more %v, %v", texts(newest), more, err)
	}

	older, _, err := s.Newest(store.Syslog, store.Window{}, all, &newest[0].Position, 10)
	if err != nil || texts(older) != "first" {
		t.Fatalf("older = %s, %v", texts(older), err)
	}

	after, _, err := s.After(store.Syslog, all, older[0].Position, 10)
	if err != nil || texts(after) != "second" {
		t.Fatalf("after = %s, %v", texts(after), err)
	}

	if latest := s.Latest(store.Syslog); latest == nil || *latest != newest[0].Position {
		t.Fatalf("latest = %v, want %v", latest, newest[0].Position)
	}

	// The newest segment is outside this window, and the older one inside it
	// is still read.
	window := store.Window{Since: start().at.Add(30 * time.Minute)}

	inside, _, err := s.Newest(store.Syslog, window, all, nil, 10)
	if err != nil || texts(inside) != "first" {
		t.Fatalf("in the window = %s, %v", texts(inside), err)
	}
}

// Segments are ordered by their numbers. A number wider than the usual
// twelve digits still sorts after the narrower ones, and the next segment
// takes the number after the highest.
func TestSegmentsAreOrderedByTheirNumbers(t *testing.T) {
	t.Parallel()

	c := start()
	root := t.TempDir()
	dir := filepath.Join(root, "syslog")

	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	for _, name := range []string{"1000000000000-2026101914.jsonl", "999999999999-2026101914.jsonl"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	s, err := store.Open(root, c.now, roomy())
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	defer s.Close()

	c.at = c.at.Add(time.Hour)

	if err = s.Append(store.Syslog, line(c, "x")); err != nil {
		t.Fatalf("append: %v", err)
	}

	want := "999999999999-2026101914.jsonl,1000000000000-2026101914.jsonl,1000000000001-2026101915.jsonl"
	if got := s2(s.Segments(store.Syslog)); got != want {
		t.Fatalf("segments = %s, want %s", got, want)
	}

	read, err := s.ReadSegment(store.Syslog, "1000000000001-2026101915.jsonl")
	if err != nil || len(read) != 1 {
		t.Fatalf("read %d records, %v", len(read), err)
	}

	if !(store.Position{Segment: "999999999999-2026101914.jsonl", Offset: 9}).Before(read[0].Position) {
		t.Error("a position in a lower numbered segment is not before one in a higher numbered one")
	}
}

// A segment an earlier build named by its hour alone is numbered on open, in
// the order of its hour and index, and its records are read.
func TestASegmentOfAnEarlierBuildIsNumbered(t *testing.T) {
	t.Parallel()

	c := start()
	root := t.TempDir()
	dir := filepath.Join(root, "syslog")

	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	for name, text := range map[string]string{
		"2026101914-000.jsonl": "b", "2026101914-001.jsonl": "c", "2026101913-005.jsonl": "a",
	} {
		record := `{"t":"2026-10-19T14:00:00Z","mac":"m","text":"` + text + `"}` + "\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(record), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	s, err := store.Open(root, c.now, roomy())
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	defer s.Close()

	want := "000000000001-2026101913.jsonl,000000000002-2026101914.jsonl,000000000003-2026101914.jsonl"
	if got := s2(s.Segments(store.Syslog)); got != want {
		t.Fatalf("segments = %s, want %s", got, want)
	}

	got, _, err := s.Newest(store.Syslog, store.Window{}, func(store.Record) bool { return true }, nil, 10)
	if err != nil || texts(got) != "c,b,a" {
		t.Fatalf("newest = %s, %v", texts(got), err)
	}
}

func texts(entries []store.Entry) string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Record.Text)
	}

	return strings.Join(out, ",")
}

func s2(names []string) string { return strings.Join(names, ",") }
