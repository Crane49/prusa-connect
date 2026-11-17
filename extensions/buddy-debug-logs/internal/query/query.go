// Package query answers what the page and the diagnostics bundle ask of the
// kept records: log lines by filter, the metric names a printer sent, one
// metric as a series for a chart, and the records as text for a download.
//
// Every answer holds only the records the Book's Viewer may see. The test is
// part of the filter a read of the store applies, so a page, a cursor and a
// count never show that another printer's records exist.
package query

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/prusa3d/buddy-debug-logs/internal/datagram"
	"github.com/prusa3d/buddy-debug-logs/internal/sources"
	"github.com/prusa3d/buddy-debug-logs/internal/store"
)

// Bounds a caller is held to.
const (
	DefaultLimit = 200
	MaxLimit     = 1000
	// MaxQueryBytes bounds the text a person searches for.
	MaxQueryBytes = 256
	// DefaultMaxPoints is how many points a series answers when the caller
	// names no number. A chart is a few hundred pixels wide.
	DefaultMaxPoints = 300
	MaxPoints        = 2000
	// MaxExportBytes bounds one page of a download, well under the 1 MiB a
	// module answer may hold.
	MaxExportBytes = 512 << 10
	// MaxNames bounds the metric names one answer lists, and MaxNameFields
	// the fields of each. A printer sends a few dozen names of a few fields;
	// a sender that makes up new ones fills neither the answer nor memory.
	MaxNames      = 512
	MaxNameFields = 32
	// namesWindow is how far back the metric names are read when the caller
	// names no start, because reading a day of points for a list of names is
	// most of the work for none of the value.
	namesWindow = time.Hour
)

// ErrInvalid reports a query outside these bounds.
var ErrInvalid = errors.New("query: outside the published bounds")

// Book is the kept records and what tells their sources apart.
type Book struct {
	Store   *store.Store
	Sources *sources.Table
	Now     func() time.Time
	// Viewer is who reads. Its zero value sees nothing.
	Viewer Viewer
}

// Viewer is what one person may read: the records of the printers they may
// see, and the records of no printer when they may see the whole fleet.
type Viewer struct {
	devices map[string]bool
	fleet   bool
}

// Everyone is a viewer that sees every record, for the diagnostics bundle an
// admin builds.
func Everyone() Viewer { return Viewer{fleet: true} }

// ViewerOf is a person who may see the devices named. fleet says they may see
// every printer the module reads, and so the records matched to none.
func ViewerOf(devices map[string]bool, fleet bool) Viewer {
	return Viewer{devices: devices, fleet: fleet}
}

// Sees reports whether the viewer may read the records of device. An empty
// device is a record matched to no printer.
func (v Viewer) Sees(device string) bool {
	return v.fleet || (device != "" && v.devices[device])
}

// Fleet reports whether the viewer sees every record.
func (v Viewer) Fleet() bool { return v.fleet }

// LogFilter is what a person narrows the log to. Every field is optional.
type LogFilter struct {
	// Device is a device id. The lines matched to it, and those kept under a
	// MAC that is matched to it now, both count.
	Device string
	// MAC narrows to one source, for a MAC matched to no device.
	MAC string
	// Severity is the least severe level shown: warning shows warnings,
	// errors and everything worse.
	Severity *datagram.Severity
	// Component is the firmware's log component, such as Connect.
	Component string
	// Text is a piece of the line, in any case.
	Text   string
	Window store.Window
}

// Line is one log line as the page shows it.
type Line struct {
	Cursor    string    `json:"cursor"`
	At        time.Time `json:"at"`
	Device    string    `json:"device_id,omitempty"`
	MAC       string    `json:"mac,omitempty"`
	IP        string    `json:"ip,omitempty"`
	Severity  string    `json:"severity"`
	Component string    `json:"component,omitempty"`
	Text      string    `json:"text"`
	Seq       uint64    `json:"seq,omitempty"`
	// Unreadable is a datagram in neither format, shown as it came.
	Unreadable bool `json:"unreadable,omitempty"`
}

// LogPage is one page of lines, newest first.
type LogPage struct {
	Lines []Line `json:"lines"`
	// Older is the cursor to pass as before for the next page back, and
	// empty when there is none.
	Older string `json:"older,omitempty"`
	// Newest is the cursor to pass as after to ask for lines that come later.
	// It is empty when no line matched, and the caller then asks for the
	// newest page again. A cursor of a line the viewer may not see would tell
	// how much other printers sent.
	Newest string `json:"newest,omitempty"`
}

// Logs answers one page of lines that match, newest first. With after, it
// answers the lines that came after that cursor, newest first, at most limit
// of the oldest of them so a page that polls never skips one.
func (b Book) Logs(filter LogFilter, before, after string, limit int) (LogPage, error) {
	if limit == 0 {
		limit = DefaultLimit
	}

	if limit < 1 || limit > MaxLimit || len(filter.Text) > MaxQueryBytes {
		return LogPage{}, fmt.Errorf(
			"%w: limit is 1 to %d and text at most %d bytes",
			ErrInvalid,
			MaxLimit,
			MaxQueryBytes,
		)
	}

	if before != "" && after != "" {
		return LogPage{}, fmt.Errorf("%w: name before or after, not both", ErrInvalid)
	}

	entries, more, err := b.fetch(filter, before, after, limit)
	if err != nil {
		return LogPage{}, err
	}

	page := LogPage{Lines: make([]Line, 0, len(entries))}

	for _, entry := range entries {
		page.Lines = append(page.Lines, lineOf(entry))
	}

	if more && len(entries) > 0 {
		page.Older = entries[len(entries)-1].Position.String()
	}

	switch {
	case len(entries) > 0:
		page.Newest = entries[0].Position.String()
	case after != "":
		page.Newest = after
	}

	return page, nil
}

// fetch reads the entries of one page: newer than after, oldest of them
// first and then turned newest first, or older than before.
func (b Book) fetch(filter LogFilter, before, after string, limit int) ([]store.Entry, bool, error) {
	match := b.matcher(filter)

	if after != "" {
		from, err := store.ParsePosition(after)
		if err != nil {
			return nil, false, fmt.Errorf("%w: %w", ErrInvalid, err)
		}

		entries, _, err := b.Store.After(store.Syslog, func(r store.Record) bool {
			return filter.Window.Holds(r.At) && match(r)
		}, from, limit)

		slices.Reverse(entries)

		return entries, false, err
	}

	var from *store.Position

	if before != "" {
		at, err := store.ParsePosition(before)
		if err != nil {
			return nil, false, fmt.Errorf("%w: %w", ErrInvalid, err)
		}

		from = &at
	}

	return b.Store.Newest(store.Syslog, filter.Window, match, from, limit)
}

// matcher is the filter, and what the viewer may see, as one test on a
// record. A record's printer is the one it was matched to when it arrived,
// or the one its MAC is matched to now.
func (b Book) matcher(filter LogFilter) func(store.Record) bool {
	matched := b.Sources.Matched()
	text := strings.ToLower(filter.Text)

	return func(r store.Record) bool {
		device := r.Device
		if device == "" {
			device = matched[r.MAC]
		}

		if !b.Viewer.Sees(device) {
			return false
		}

		if filter.Device != "" && r.Device != filter.Device && matched[r.MAC] != filter.Device {
			return false
		}

		if filter.MAC != "" && r.MAC != filter.MAC {
			return false
		}

		if filter.Severity != nil && (r.Severity == nil || datagram.Severity(*r.Severity) > *filter.Severity) {
			return false
		}

		if filter.Component != "" && !strings.EqualFold(r.Component, filter.Component) {
			return false
		}

		if text != "" && !strings.Contains(strings.ToLower(r.Component+" "+r.Text+r.Raw), text) {
			return false
		}

		return true
	}
}

func lineOf(entry store.Entry) Line {
	r := entry.Record

	line := Line{
		Cursor: entry.Position.String(), At: r.At, Device: r.Device, MAC: r.MAC, IP: r.IP,
		Component: r.Component, Text: r.Text, Seq: r.Seq, Severity: "unknown",
	}

	if r.Severity != nil {
		line.Severity = datagram.Severity(*r.Severity).String()
	}

	if r.Raw != "" {
		line.Unreadable = true
		line.Text = r.Raw
	}

	return line
}

// Name is one metric a printer sent.
type Name struct {
	Name string `json:"name"`
	// Fields is every field the metric carried, such as v or value.
	Fields []string `json:"fields"`
	// Last is the newest value of each field.
	Last   map[string]any `json:"last"`
	LastAt time.Time      `json:"last_at"`
	Count  int64          `json:"count"`
}

// Names answers every metric the device sent in the window, by name, at most
// MaxNames of them with at most MaxNameFields fields each. truncated says
// some were left out. With no start the window is the last hour.
func (b Book) Names(device string, window store.Window) (names []Name, truncated bool, err error) {
	if window.Since.IsZero() {
		window.Since = b.Now().Add(-namesWindow)
	}

	match := b.matcher(LogFilter{Device: device})
	held := map[string]*Name{}

	err = b.Store.Scan(store.Metrics, window, nil, func(entry store.Entry) bool {
		r := entry.Record
		if r.Name == "" || !match(r) {
			return true
		}

		name, seen := held[r.Name]
		if !seen {
			if len(held) == MaxNames {
				truncated = true

				return true
			}

			name = &Name{Name: r.Name, Last: map[string]any{}}
			held[r.Name] = name
		}

		name.Count++
		name.LastAt = r.At

		for field, value := range r.Fields {
			if _, known := name.Last[field]; !known && len(name.Last) == MaxNameFields {
				truncated = true

				continue
			}

			name.Last[field] = value
		}

		return true
	})
	if err != nil {
		return nil, false, err
	}

	out := make([]Name, 0, len(held))
	for _, name := range held {
		name.Fields = slices.Sorted(maps.Keys(name.Last))
		out = append(out, *name)
	}

	slices.SortFunc(out, func(a, c Name) int { return strings.Compare(a.Name, c.Name) })

	return out, truncated, nil
}

// Point is one value of a series.
type Point struct {
	At    time.Time `json:"at"`
	Value float64   `json:"value"`
	// Min and Max are the range a downsampled point stands for.
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

// SeriesQuery names one field of one metric of one printer.
type SeriesQuery struct {
	Device string
	MAC    string
	Name   string
	// Field is the field read, v when empty.
	Field     string
	Window    store.Window
	MaxPoints int
}

// Series answers one numeric field over the window, averaged into at most
// MaxPoints buckets of equal time. An event counts as 1 and false as 0, and
// a text value, such as the nan of a failed sensor, is left out. A record
// that arrived after the end of the window is left out too.
func (b Book) Series(q SeriesQuery) ([]Point, error) {
	if q.MaxPoints == 0 {
		q.MaxPoints = DefaultMaxPoints
	}

	if q.Name == "" || q.MaxPoints < 1 || q.MaxPoints > MaxPoints {
		return nil, fmt.Errorf("%w: a series names a metric and 1 to %d points", ErrInvalid, MaxPoints)
	}

	if q.Field == "" {
		q.Field = "v"
	}

	if q.Window.Since.IsZero() {
		q.Window.Since = b.Now().Add(-namesWindow)
	}

	end := q.Window.Until
	if end.IsZero() {
		end = b.Now()
	}

	match := b.matcher(LogFilter{Device: q.Device, MAC: q.MAC})
	span := end.Sub(q.Window.Since)
	bucket := max(span/time.Duration(q.MaxPoints), time.Nanosecond)

	type sum struct {
		total, low, high float64
		n                int
	}

	sums := map[int64]*sum{}

	err := b.Store.Scan(store.Metrics, q.Window, nil, func(entry store.Entry) bool {
		r := entry.Record
		if r.Name != q.Name || !match(r) {
			return true
		}

		value, ok := number(r.Fields[q.Field])
		if !ok || r.At.After(end) {
			return true
		}

		// The bucket is rounded down, so the last one takes what is left of
		// the span and there are never more than MaxPoints of them.
		key := min(int64(r.At.Sub(q.Window.Since)/bucket), int64(q.MaxPoints-1))

		held, seen := sums[key]
		if !seen {
			held = &sum{low: value, high: value}
			sums[key] = held
		}

		held.total += value
		held.n++
		held.low = min(held.low, value)
		held.high = max(held.high, value)

		return true
	})
	if err != nil {
		return nil, err
	}

	keys := make([]int64, 0, len(sums))
	for key := range sums {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	out := make([]Point, 0, len(keys))
	for _, key := range keys {
		held := sums[key]
		out = append(out, Point{
			At:    q.Window.Since.Add(time.Duration(key)*bucket + bucket/2),
			Value: held.total / float64(held.n),
			Min:   held.low,
			Max:   held.high,
		})
	}

	return out, nil
}

// number reads a field value as a number. JSON gives every number back as a
// float64.
func number(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int64:
		return float64(v), true
	case bool:
		if v {
			return 1, true
		}

		return 0, true
	default:
		return 0, false
	}
}
