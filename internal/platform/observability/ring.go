package observability

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// ModuleStderrKey marks a record that is a line a module wrote to standard
// error. A diagnostics bundle leaves those out unless the operator asks.
const ModuleStderrKey = "module_stderr"

// LogEntry is one record as the ring keeps it. Attrs are flattened, so a group
// key is dotted, and a value of kind Any is already a string: the ring holds
// no reference into the code that logged it.
type LogEntry struct {
	Time    time.Time
	Level   slog.Level
	Message string
	Attrs   []slog.Attr
}

// LogRing keeps the newest records in memory for a diagnostics bundle. It is
// built once by the composition root and handed to whatever reads it.
type LogRing struct {
	mu       sync.Mutex
	entries  []LogEntry
	bytes    int
	size     int
	maxBytes int
}

// NewLogRing keeps the newest size records, and fewer when their text passes
// maxBytes: a module that writes long lines would otherwise hold size times
// its line length.
func NewLogRing(size, maxBytes int) *LogRing {
	return &LogRing{size: size, maxBytes: maxBytes}
}

// Handler wraps inner so every record inner handles is also kept here.
func (r *LogRing) Handler(inner slog.Handler) slog.Handler {
	return &ringHandler{inner: inner, ring: r}
}

// Entries returns the kept records, oldest first.
func (r *LogRing) Entries() []LogEntry {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]LogEntry(nil), r.entries...)
}

func (r *LogRing) add(entry LogEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.entries = append(r.entries, entry)
	r.bytes += entry.bytes()

	for len(r.entries) > r.size || (r.bytes > r.maxBytes && len(r.entries) > 1) {
		r.bytes -= r.entries[0].bytes()
		r.entries[0] = LogEntry{}
		r.entries = r.entries[1:]
	}
}

// Flag reports whether the entry carries the boolean attribute, set true.
func (e LogEntry) Flag(key string) bool {
	for _, a := range e.Attrs {
		if a.Key == key && a.Value.Kind() == slog.KindBool && a.Value.Bool() {
			return true
		}
	}

	return false
}

// bytes is the size of the text an entry holds.
func (e LogEntry) bytes() int {
	n := len(e.Message)
	for _, a := range e.Attrs {
		n += len(a.Key) + len(a.Value.String())
	}

	return n
}

type ringHandler struct {
	inner  slog.Handler
	ring   *LogRing
	attrs  []slog.Attr
	prefix string
}

func (h *ringHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *ringHandler) Handle(ctx context.Context, record slog.Record) error {
	attrs := append([]slog.Attr(nil), h.attrs...)
	record.Attrs(func(a slog.Attr) bool {
		attrs = flatten(h.prefix, a, attrs)

		return true
	})

	h.ring.add(LogEntry{Time: record.Time, Level: record.Level, Message: record.Message, Attrs: attrs})

	return h.inner.Handle(ctx, record)
}

func (h *ringHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	kept := append([]slog.Attr(nil), h.attrs...)
	for _, a := range attrs {
		kept = flatten(h.prefix, a, kept)
	}

	return &ringHandler{inner: h.inner.WithAttrs(attrs), ring: h.ring, attrs: kept, prefix: h.prefix}
}

func (h *ringHandler) WithGroup(name string) slog.Handler {
	return &ringHandler{inner: h.inner.WithGroup(name), ring: h.ring, attrs: h.attrs, prefix: dotted(h.prefix, name)}
}

func flatten(prefix string, a slog.Attr, into []slog.Attr) []slog.Attr {
	value := a.Value.Resolve()

	switch value.Kind() { //nolint:exhaustive // every other kind keeps its value as it is
	case slog.KindGroup:
		inner := prefix
		if a.Key != "" {
			inner = dotted(prefix, a.Key)
		}

		for _, member := range value.Group() {
			into = flatten(inner, member, into)
		}

		return into
	case slog.KindAny:
		return append(into, slog.String(dotted(prefix, a.Key), fmt.Sprint(value.Any())))
	default:
		return append(into, slog.Attr{Key: dotted(prefix, a.Key), Value: value})
	}
}

func dotted(prefix, key string) string {
	if prefix == "" {
		return key
	}

	return prefix + "." + key
}
