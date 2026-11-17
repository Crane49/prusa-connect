package query

import (
	"fmt"
	"strings"

	"github.com/prusa3d/buddy-debug-logs/internal/datagram"
	"github.com/prusa3d/buddy-debug-logs/internal/store"
)

// timeLayout is how a line is dated in a download: UTC, to the millisecond,
// so lines from two printers sort together.
const timeLayout = "2006-01-02T15:04:05.000Z"

// Format writes one record as a line of text: the time, the source, then
// for a log line its severity, component and text, and for a metric point the
// line the printer sent. label names the source and is left out when empty.
// Every text goes through datagram.Clean, so a download holds one line per
// record whatever the files hold.
func Format(r store.Record, label string) string {
	var out strings.Builder

	out.WriteString(r.At.UTC().Format(timeLayout))

	if label != "" {
		out.WriteString(" ")
		out.WriteString(label)
	}

	switch {
	case r.Raw != "":
		out.WriteString(" UNREADABLE from ")
		out.WriteString(r.IP)
		out.WriteString(": ")
		out.WriteString(datagram.Clean(r.Raw))
	case r.Line != "":
		out.WriteString(" ")
		out.WriteString(datagram.Clean(r.Line))
	default:
		severity := "UNKNOWN"
		if r.Severity != nil {
			severity = strings.ToUpper(datagram.Severity(*r.Severity).String())
		}

		out.WriteString(" ")
		out.WriteString(severity)
		out.WriteString(" ")

		if r.Component != "" {
			out.WriteString(datagram.Clean(r.Component))
			out.WriteString(": ")
		}

		out.WriteString(datagram.Clean(r.Text))
	}

	out.WriteString("\n")

	return out.String()
}

// Label is how a download names a record's source: its device id, the
// device its MAC is matched to now, or the MAC.
func (b Book) Label(r store.Record) string {
	switch {
	case r.Device != "":
		return r.Device
	case r.MAC != "":
		if device := b.Sources.DeviceOf(r.MAC); device != "" {
			return device
		}

		return r.MAC
	default:
		return r.IP
	}
}

// ExportPage is one page of a download.
type ExportPage struct {
	Text string `json:"text"`
	// Next is the cursor of the next page, and empty on the last one.
	Next string `json:"next,omitempty"`
}

// Export answers one page of a stream as text, oldest first, for a browser
// that puts the pages together into one file.
func (b Book) Export(stream store.Stream, device string, window store.Window, cursor string) (ExportPage, error) {
	if !stream.Valid() {
		return ExportPage{}, fmt.Errorf("%w: stream is syslog or metrics", ErrInvalid)
	}

	var from *store.Position

	if cursor != "" {
		at, err := store.ParsePosition(cursor)
		if err != nil {
			return ExportPage{}, fmt.Errorf("%w: %w", ErrInvalid, err)
		}

		from = &at
	}

	match := b.matcher(LogFilter{Device: device})

	var (
		out  strings.Builder
		next string
	)

	err := b.Store.Scan(stream, window, from, func(entry store.Entry) bool {
		if !match(entry.Record) {
			return true
		}

		line := Format(entry.Record, b.Label(entry.Record))
		if out.Len()+len(line) > MaxExportBytes {
			next = entry.Position.String()

			return false
		}

		out.WriteString(line)

		return true
	})
	if err != nil {
		return ExportPage{}, err
	}

	return ExportPage{Text: out.String(), Next: next}, nil
}
