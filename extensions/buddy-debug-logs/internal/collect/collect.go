// Package collect answers diagnostics.collect: the files this module adds to
// the diagnostics bundle an admin downloads (ADR-20261019-nrq).
//
// The first page holds status.json and sources.json. The pages after it hold
// the log lines and then the metric points, newest first, one file per
// printer: syslog/<printer>.log and metrics/<printer>.lp. A printer is named
// by its device id. A MAC matched to no printer is named by its alias, which
// sources.json lists beside it, and a datagram in neither format goes to
// unreadable. A file name never holds a MAC or an address. The host takes at
// most 8 MiB, so the newest lines are the ones a bundle carries, and the log
// lines come before the points because a person reads them first.
//
// A cursor names the stream and the position of the oldest record already
// sent, so a line written while the bundle is built never shifts what is
// sent next.
package collect

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/diagnosticsapi"

	"github.com/prusa3d/buddy-debug-logs/internal/query"
	"github.com/prusa3d/buddy-debug-logs/internal/sources"
	"github.com/prusa3d/buddy-debug-logs/internal/store"
)

// pageBytes is how much text one page holds, below what the host takes so
// the file names and the encoding fit beside it.
const pageBytes = diagnosticsapi.MaxPageBytes - 16<<10

// readBatch is how many records one read of the store answers.
const readBatch = 512

// maxLabelBytes bounds the part of a file name that names the printer. The
// label is ASCII by then, so a cut never splits a character.
const maxLabelBytes = 64

// streamMarks name the streams in a cursor.
//
//nolint:gochecknoglobals // a closed table, read and never written.
var streamMarks = map[store.Stream]string{store.Syslog: "s", store.Metrics: "m"}

// Collector builds the pages.
type Collector struct {
	Book query.Book
	// Status is what GET /status answers, as it is when the bundle is built.
	Status func() any
}

// Page answers the page the cursor names.
func (c Collector) Page(cursor string) (diagnosticsapi.Page, error) {
	if cursor == "" {
		return c.first()
	}

	stream, from, err := parseCursor(cursor)
	if err != nil {
		return diagnosticsapi.Page{}, err
	}

	return c.records(stream, from)
}

// StatusOnly is the one page of a module that keeps nothing: why, in
// status.json.
func StatusOnly(status func() any) (diagnosticsapi.Page, error) {
	encoded, err := json.MarshalIndent(status(), "", "  ")
	if err != nil {
		return diagnosticsapi.Page{}, fmt.Errorf("encode the status: %w", err)
	}

	return diagnosticsapi.Page{Files: []diagnosticsapi.File{{Name: "status.json", Text: string(encoded) + "\n"}}}, nil
}

// first is status.json and sources.json, and the cursor of the newest log
// line.
func (c Collector) first() (diagnosticsapi.Page, error) {
	status, err := json.MarshalIndent(c.Status(), "", "  ")
	if err != nil {
		return diagnosticsapi.Page{}, fmt.Errorf("encode the status: %w", err)
	}

	listed, err := json.MarshalIndent(c.sources(), "", "  ")
	if err != nil {
		return diagnosticsapi.Page{}, fmt.Errorf("encode the sources: %w", err)
	}

	return diagnosticsapi.Page{
		Files: []diagnosticsapi.File{
			{Name: "status.json", Text: string(status) + "\n"},
			{Name: "sources.json", Text: string(listed) + "\n"},
		},
		Cursor: c.start(store.Syslog),
	}, nil
}

// listedSource is one source in sources.json, with the name of its files when
// it is matched to no printer.
type listedSource struct {
	sources.Source

	Alias string `json:"alias,omitempty"`
}

func (c Collector) sources() []listedSource {
	list := c.Book.Sources.List()
	out := make([]listedSource, 0, len(list))

	for _, source := range list {
		held := listedSource{Source: source}
		if source.Device == "" {
			held.Alias = c.Book.Sources.Alias(source.MAC)
		}

		out = append(out, held)
	}

	return out
}

// label names the files of one record's source: its printer, the alias of its
// MAC, or unreadable.
func (c Collector) label(r store.Record) string {
	switch {
	case r.Device != "":
		return r.Device
	case r.MAC != "":
		if device := c.Book.Sources.DeviceOf(r.MAC); device != "" {
			return device
		}

		return c.Book.Sources.Alias(r.MAC)
	default:
		return "unreadable"
	}
}

// start is the cursor that begins a stream, the metrics when the log is
// empty, and no cursor when nothing is left.
func (c Collector) start(stream store.Stream) string {
	if latest := c.Book.Store.Latest(stream); latest != nil {
		// The newest record itself is sent, so the cursor points just past it.
		next := *latest
		next.Offset++

		return streamMarks[stream] + "|" + next.String()
	}

	if stream == store.Syslog {
		return c.start(store.Metrics)
	}

	return ""
}

// records is one page of a stream, newest first, from just before from.
func (c Collector) records(stream store.Stream, from store.Position) (diagnosticsapi.Page, error) {
	var (
		order []string
		files = map[string]*strings.Builder{}
		size  int
	)

	all := func(store.Record) bool { return true }

	for {
		entries, more, err := c.Book.Store.Newest(stream, store.Window{}, all, &from, readBatch)
		if err != nil {
			return diagnosticsapi.Page{}, err
		}

		for _, entry := range entries {
			name := fileName(stream, c.label(entry.Record))
			line := query.Format(entry.Record, "")

			held, seen := files[name]

			// A page ends before it is too large, or before it names more
			// files than the host takes, which a large fleet reaches.
			if size+len(line) > pageBytes || (!seen && len(order) == diagnosticsapi.MaxPageFiles) {
				return pageOf(order, files, streamMarks[stream]+"|"+from.String()), nil
			}

			if !seen {
				held = &strings.Builder{}
				files[name] = held
				order = append(order, name)
			}

			held.WriteString(line)
			size += len(line)
			from = entry.Position
		}

		if !more {
			break
		}
	}

	next := ""
	if stream == store.Syslog {
		next = c.start(store.Metrics)
	}

	return pageOf(order, files, next), nil
}

func pageOf(order []string, files map[string]*strings.Builder, cursor string) diagnosticsapi.Page {
	page := diagnosticsapi.Page{Files: make([]diagnosticsapi.File, 0, len(order)), Cursor: cursor}

	for _, name := range order {
		page.Files = append(page.Files, diagnosticsapi.File{Name: name, Text: files[name].String()})
	}

	return page
}

// fileName is the file one source's records go to. A device id may hold a
// character a file name may not, which becomes a hyphen.
func fileName(stream store.Stream, label string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			return r
		default:
			return '-'
		}
	}, label)

	if safe == "" || strings.Trim(safe, ".") == "" {
		safe = "unknown"
	}

	if len(safe) > maxLabelBytes {
		safe = safe[:maxLabelBytes]
	}

	if stream == store.Metrics {
		return "metrics/" + safe + ".lp"
	}

	return "syslog/" + safe + ".log"
}

// parseCursor reads a cursor this package wrote.
func parseCursor(cursor string) (store.Stream, store.Position, error) {
	mark, at, found := strings.Cut(cursor, "|")
	if !found {
		return "", store.Position{}, fmt.Errorf("%w: %q", store.ErrPosition, cursor)
	}

	for stream, held := range streamMarks {
		if held == mark {
			from, err := store.ParsePosition(at)

			return stream, from, err
		}
	}

	return "", store.Position{}, fmt.Errorf("%w: %q", store.ErrPosition, cursor)
}
