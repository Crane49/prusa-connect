package resourcehost

import (
	"context"
	"log/slog"

	"github.com/prusa3d/prusa-connect/internal/platform/observability"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

const (
	// logTailLines and logTailBytes bound what the host keeps of a module's
	// standard error.
	logTailLines = 200
	logTailBytes = 64 << 10
	// splitAttribute is the attribute wirehost sets on a record that holds
	// part of a longer line.
	splitAttribute = "split"
)

// tail keeps every record in the ring at any log level of the server, and
// passes to inner only what inner enables. A module's standard error is read
// from the ring on request, so a server that logs at error must still keep it.
type tail struct {
	inner slog.Handler
	kept  slog.Handler
}

func newTail(ring *observability.LogRing, inner slog.Handler) slog.Handler {
	return tail{inner: inner, kept: ring.Handler(slog.DiscardHandler)}
}

func (tail) Enabled(context.Context, slog.Level) bool { return true }

func (t tail) Handle(ctx context.Context, record slog.Record) error {
	if err := t.kept.Handle(ctx, record); err != nil {
		return err
	}

	if !t.inner.Enabled(ctx, record.Level) {
		return nil
	}

	return t.inner.Handle(ctx, record)
}

func (t tail) WithAttrs(attrs []slog.Attr) slog.Handler {
	return tail{inner: t.inner.WithAttrs(attrs), kept: t.kept.WithAttrs(attrs)}
}

func (t tail) WithGroup(name string) slog.Handler {
	return tail{inner: t.inner.WithGroup(name), kept: t.kept.WithGroup(name)}
}

// stderrLines is the lines of the module's standard error in the ring, oldest
// first. The ring also holds the host's own records about the module, and
// those are left out.
func stderrLines(ring *observability.LogRing) []httpapi.ResourceLogLine {
	lines := []httpapi.ResourceLogLine{}

	for _, entry := range ring.Entries() {
		if !entry.Flag(observability.ModuleStderrKey) {
			continue
		}

		lines = append(lines, httpapi.ResourceLogLine{
			At: entry.Time, Text: entry.Message, Split: entry.Flag(splitAttribute),
		})
	}

	return lines
}
