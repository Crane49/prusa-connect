// Package store keeps what the printers sent, in files under the module's
// data directory.
//
// Each stream has a folder, and a folder holds segments: append-only files of
// one JSON record per line. A segment is named by its number and the hour its
// records arrived in, such as syslog/000000000042-2026101914.jsonl. The number
// grows by one with each new segment of a stream and never goes back, so it
// orders the segments in the order they were written, even when the clock
// steps back. The hour is there for a reader that skips a segment outside its
// time range. A segment is closed and a new one started when the hour changes
// or when it reaches its size limit, so a reader never holds more than one
// segment in memory. The oldest segment goes first when the stream is older
// than the retention or larger than its share of the room.
//
// Files are the store because the data is a log: it is written once, read by
// time range, and thrown away by age. ADR-20260827-rwq rule 3 lets a module keep a
// store of its own, and ADR-20261019-3ns gives it the directory.
package store

import (
	"bufio"
	"cmp"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Stream is one kind of data a printer sends.
type Stream string

// The two streams.
const (
	Syslog  Stream = "syslog"
	Metrics Stream = "metrics"
)

// Streams is both streams, logs first.
func Streams() []Stream { return []Stream{Syslog, Metrics} }

// Valid reports whether s is one of the two streams.
func (s Stream) Valid() bool { return s == Syslog || s == Metrics }

// MaxSegmentBytes is the largest a segment grows before the next one starts.
// A query reads a whole segment, so this bounds what one query holds.
const MaxSegmentBytes = 4 << 20

// readBuffer is what a segment is read through.
const readBuffer = 64 << 10

// minSegmentBytes keeps a small budget from making a segment per record.
const minSegmentBytes = 64 << 10

// hourLayout is the hour in a segment's name, in UTC.
const hourLayout = "2006010215"

// numberDigits is how wide a segment's number is written. The number orders
// the segments, and it is compared as a number, so a wider one still sorts
// after it.
const numberDigits = 12

// segmentSuffix is the extension of every segment.
const segmentSuffix = ".jsonl"

// ErrClosed reports a write to a store that was closed.
var ErrClosed = errors.New("store: closed")

// Record is one line a printer sent, or one metric point, as it is kept.
type Record struct {
	// At is when the datagram arrived. The printer has no clock it can
	// trust, so this is the time a reader sorts by.
	At time.Time `json:"t"`
	// Device is the device id the datagram was matched to, and empty when it
	// matched none at the time.
	Device string `json:"dev,omitempty"`
	MAC    string `json:"mac"`
	IP     string `json:"ip,omitempty"`
	Seq    uint64 `json:"seq,omitempty"`
	// TmUS is the printer's microseconds since it started.
	TmUS int64 `json:"tm,omitempty"`

	// A syslog line's. Severity is a pointer because 0 is a severity.
	Severity  *int   `json:"sev,omitempty"`
	Component string `json:"comp,omitempty"`
	Text      string `json:"text,omitempty"`

	// A metric point's. PointUS is the point's own time since the printer
	// started: the datagram's tm and the point's offset.
	Line    string            `json:"line,omitempty"`
	Name    string            `json:"name,omitempty"`
	Tags    map[string]string `json:"tags,omitempty"`
	Fields  map[string]any    `json:"fields,omitempty"`
	PointUS int64             `json:"pt,omitempty"`

	// Raw is a datagram that could not be read, kept as it came.
	Raw string `json:"raw,omitempty"`
}

// Limits is how much each stream keeps.
type Limits struct {
	// Hours is how long a record is kept.
	Hours int
	// Bytes is the room each stream may take on disk.
	Bytes map[Stream]int64
}

// Usage is what one stream holds on disk.
type Usage struct {
	Bytes    int64
	Segments int
	// Oldest is the start of the oldest segment's hour, and zero for none.
	Oldest time.Time
}

// Store is the files of every stream.
type Store struct {
	root string
	now  func() time.Time

	mu      sync.Mutex
	limits  Limits
	streams map[Stream]*writer
	closed  bool

	// purging lets one purge run at a time. A purge rewrites files without
	// holding mu, so an append is not held up behind it.
	purging sync.Mutex

	// generation is in every position this store answers. A purge that
	// rewrites a segment moves the records in it, so it changes the
	// generation, and a cursor written before is refused with ErrStale. It
	// starts at a random number, so a cursor from an earlier process is
	// refused too.
	generation atomic.Uint64
}

// writer is the open segment of one stream and the size of every segment.
type writer struct {
	dir   string
	file  *os.File
	name  string
	hour  string
	size  int64
	sizes map[string]int64
	// next is the number the next segment takes: one more than the highest
	// number in the folder.
	next uint64
	// purging is true while a purge rewrites this stream's closed segments,
	// and the retention leaves them alone until it ends.
	purging bool
}

// Open reads what the root already holds and keeps writing beside it.
func Open(root string, now func() time.Time, limits Limits) (*Store, error) {
	s := &Store{root: root, now: now, limits: limits, streams: map[Stream]*writer{}}

	var seed [8]byte

	_, _ = rand.Read(seed[:]) // crypto/rand.Read never fails; it ends the program instead
	s.generation.Store(binary.LittleEndian.Uint64(seed[:]))

	for _, stream := range Streams() {
		dir := filepath.Join(root, string(stream))
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("make the %s folder: %w", stream, err)
		}

		w, err := openWriter(dir)
		if err != nil {
			return nil, fmt.Errorf("read the %s folder: %w", stream, err)
		}

		s.streams[stream] = w
	}

	return s, nil
}

// openWriter counts the segments a stream's folder holds, and numbers the
// next one after the highest of them.
func openWriter(dir string) (*writer, error) {
	if err := renameOldSegments(dir); err != nil {
		return nil, err
	}

	w := &writer{dir: dir, sizes: map[string]int64{}, next: 1}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}

	for _, entry := range entries {
		number, _, ok := parseSegment(entry.Name())
		if entry.IsDir() || !ok {
			continue
		}

		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}

		w.sizes[entry.Name()] = info.Size()
		w.next = max(w.next, number+1)
	}

	return w, nil
}

// renameOldSegments gives a number to each segment an earlier build of the
// module named by its hour and an index alone, such as 2026101914-000.jsonl.
// They are numbered in the order of that name, after every segment named
// the new way, so no rename takes the name of a file that is there.
func renameOldSegments(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("list: %w", err)
	}

	type old struct {
		name, hour string
		index      int
	}

	var (
		found []old
		last  uint64
	)

	for _, entry := range entries {
		if number, _, ok := parseSegment(entry.Name()); ok {
			last = max(last, number)
		}

		if hour, index, ok := parseOldSegment(entry.Name()); ok && !entry.IsDir() {
			found = append(found, old{name: entry.Name(), hour: hour, index: index})
		}
	}

	slices.SortFunc(found, func(a, b old) int {
		if c := strings.Compare(a.hour, b.hour); c != 0 {
			return c
		}

		return a.index - b.index
	})

	for i, segment := range found {
		name := segmentName(last+uint64(i)+1, segment.hour)
		if err = os.Rename(filepath.Join(dir, segment.name), filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("rename %s: %w", segment.name, err)
		}
	}

	return nil
}

// SetLimits changes what each stream keeps. The next Enforce applies it.
func (s *Store) SetLimits(limits Limits) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.limits = limits
}

// Append writes one record to the stream's open segment, starting a new
// segment first when the hour changed or the open one is full.
func (s *Store) Append(stream Stream, record Record) error {
	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode a record: %w", err)
	}

	line = append(line, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrClosed
	}

	w, ok := s.streams[stream]
	if !ok {
		return fmt.Errorf("store: no stream %q", stream)
	}

	hour := record.At.UTC().Format(hourLayout)
	if w.file == nil || w.hour != hour || w.size+int64(len(line)) > s.segmentLimit(stream) {
		if err = s.rotate(stream, w, hour); err != nil {
			return err
		}
	}

	n, err := w.file.Write(line)
	w.size += int64(n)
	w.sizes[w.name] = w.size

	if err != nil {
		return fmt.Errorf("write a %s record: %w", stream, err)
	}

	return nil
}

// segmentsPerRoom is how many segments a stream's room holds at least, so
// retention by size throws away a quarter of it at most.
const segmentsPerRoom = 4

// segmentLimit is the size a segment of stream grows to: a quarter of its
// room, and never more than MaxSegmentBytes.
func (s *Store) segmentLimit(stream Stream) int64 {
	limit := min(int64(MaxSegmentBytes), s.limits.Bytes[stream]/segmentsPerRoom)

	return max(limit, minSegmentBytes)
}

// rotate closes the open segment and opens the next one, numbered one after
// the last. The caller holds s.mu.
func (s *Store) rotate(stream Stream, w *writer, hour string) error {
	if err := w.close(); err != nil {
		return err
	}

	name := segmentName(w.next, hour)
	w.next++

	file, err := os.OpenFile(filepath.Join(w.dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open a %s segment: %w", stream, err)
	}

	w.file, w.name, w.hour, w.size = file, name, hour, 0
	w.sizes[name] = 0

	s.enforceStream(stream, w)

	return nil
}

// close syncs and closes the open segment. On a USB stick or FAT a full disk
// is reported here and nowhere else, so the error is returned.
func (w *writer) close() error {
	if w.file == nil {
		return nil
	}

	file := w.file
	w.file = nil

	syncErr := file.Sync()
	if err := file.Close(); err != nil {
		return fmt.Errorf("close a segment: %w", err)
	}

	if syncErr != nil {
		return fmt.Errorf("sync a segment: %w", syncErr)
	}

	return nil
}

// Sync writes what the open segments hold to the disk. It runs on a timer
// rather than per record, because a printer sends many small records.
func (s *Store) Sync() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var errs []error

	for stream, w := range s.streams {
		if w.file != nil {
			if err := w.file.Sync(); err != nil {
				errs = append(errs, fmt.Errorf("sync %s: %w", stream, err))
			}
		}
	}

	return errors.Join(errs...)
}

// Enforce removes the segments past the retention, oldest first.
func (s *Store) Enforce() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for stream, w := range s.streams {
		s.enforceStream(stream, w)
	}
}

// enforceStream removes a stream's segments that are older than the
// retention, then the oldest ones while the stream is over its room, in the
// order they were written. The open segment stays. The caller holds s.mu.
func (s *Store) enforceStream(stream Stream, w *writer) {
	if w.purging {
		return
	}

	cutoff := s.now().Add(-time.Duration(s.limits.Hours) * time.Hour)
	budget := s.limits.Bytes[stream]

	var total int64
	for _, size := range w.sizes {
		total += size
	}

	for _, name := range sortedSegments(w.sizes) {
		if name == w.name {
			continue
		}

		start, ok := segmentHour(name)
		expired := ok && s.limits.Hours > 0 && !start.Add(time.Hour).After(cutoff)
		over := budget > 0 && total > budget

		if !expired && !over {
			break
		}

		if err := os.Remove(filepath.Join(w.dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			// A segment that cannot be removed now is tried again on the next
			// pass. Removing the next one would leave a gap in the middle.
			break
		}

		total -= w.sizes[name]
		delete(w.sizes, name)
	}
}

// Usage answers what each stream holds.
func (s *Store) Usage() map[Stream]Usage {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := map[Stream]Usage{}

	for stream, w := range s.streams {
		var usage Usage

		for name, size := range w.sizes {
			usage.Bytes += size
			usage.Segments++

			if start, ok := segmentHour(name); ok && (usage.Oldest.IsZero() || start.Before(usage.Oldest)) {
				usage.Oldest = start
			}
		}

		out[stream] = usage
	}

	return out
}

// Close closes every open segment.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closed = true

	var errs []error

	for _, w := range s.streams {
		errs = append(errs, w.close())
	}

	return errors.Join(errs...)
}

// Segments is a stream's segments in the order they were written, oldest
// first.
func (s *Store) Segments(stream Stream) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	w, ok := s.streams[stream]
	if !ok {
		return nil
	}

	return sortedSegments(w.sizes)
}

// sortedSegments is the names of sizes in the order they were written.
func sortedSegments(sizes map[string]int64) []string {
	return slices.SortedFunc(maps.Keys(sizes), compareSegments)
}

// compareSegments orders two segment names by their numbers, which is the
// order they were written in.
func compareSegments(a, b string) int {
	an, _, _ := parseSegment(a)
	bn, _, _ := parseSegment(b)

	if c := cmp.Compare(an, bn); c != 0 {
		return c
	}

	return strings.Compare(a, b)
}

// Entry is one record and where it is.
type Entry struct {
	Position Position
	Record   Record
}

// ReadSegment reads every whole record of one segment, oldest first. A line
// the writer has not finished is skipped.
func (s *Store) ReadSegment(stream Stream, name string) ([]Entry, error) {
	if !stream.Valid() || !validSegment(name) {
		return nil, fmt.Errorf("store: no segment %s/%s", stream, name)
	}

	// Read before the file is opened. A purge changes the generation before
	// and after it rewrites, so a read that overlaps one is never stamped
	// with the generation that follows it.
	generation := s.generation.Load()

	file, err := os.Open(filepath.Join(s.root, string(stream), name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, fmt.Errorf("open a segment: %w", err)
	}
	defer file.Close()

	reader := bufio.NewReaderSize(file, readBuffer)

	var (
		out    []Entry
		offset int64
	)

	for {
		line, readErr := reader.ReadBytes('\n')
		at := offset
		offset += int64(len(line))

		if readErr != nil {
			// A last line with no newline is one the writer is still
			// writing, and is read on the next call.
			break
		}

		var record Record
		if json.Unmarshal(line, &record) != nil {
			continue
		}

		out = append(out, Entry{
			Position: Position{Segment: name, Offset: at, Generation: generation}, Record: record,
		})
	}

	return out, nil
}

// SegmentHour is the hour a segment's records arrived in.
func SegmentHour(name string) (time.Time, bool) { return segmentHour(name) }

func segmentHour(name string) (time.Time, bool) {
	_, hour, ok := parseSegment(name)

	return hour, ok
}

// segmentName is the name of the segment numbered number, of the hour hour.
func segmentName(number uint64, hour string) string {
	return fmt.Sprintf("%0*d-%s%s", numberDigits, number, hour, segmentSuffix)
}

// parseSegment reads a segment's number and hour from its name. It is false
// for a name this package did not write, and for one that climbs a folder.
func parseSegment(name string) (uint64, time.Time, bool) {
	if !strings.HasSuffix(name, segmentSuffix) || strings.ContainsAny(name, `/\`) {
		return 0, time.Time{}, false
	}

	digits, hour, found := strings.Cut(strings.TrimSuffix(name, segmentSuffix), "-")
	if !found || len(digits) < numberDigits {
		return 0, time.Time{}, false
	}

	number, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return 0, time.Time{}, false
	}

	start, err := time.Parse(hourLayout, hour)
	if err != nil {
		return 0, time.Time{}, false
	}

	return number, start, true
}

// parseOldSegment reads a name an earlier build wrote: an hour, a dash, an
// index and the suffix.
func parseOldSegment(name string) (string, int, bool) {
	if !strings.HasSuffix(name, segmentSuffix) || strings.ContainsAny(name, `/\`) {
		return "", 0, false
	}

	hour, index, found := strings.Cut(strings.TrimSuffix(name, segmentSuffix), "-")
	if !found || len(hour) != len(hourLayout) {
		return "", 0, false
	}

	if _, err := time.Parse(hourLayout, hour); err != nil {
		return "", 0, false
	}

	n, err := strconv.Atoi(index)
	if err != nil || n < 0 {
		return "", 0, false
	}

	return hour, n, true
}

// validSegment reports whether name is a segment this package wrote.
func validSegment(name string) bool {
	_, _, ok := parseSegment(name)

	return ok
}
