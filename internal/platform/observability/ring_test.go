package observability_test

import (
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/observability"
)

func TestRingKeepsTheNewestRecordsFlattened(t *testing.T) {
	ring := observability.NewLogRing(2, 1<<20)
	logger := slog.New(ring.Handler(enabledDiscard()))

	logger.Info("one")
	logger.With("instance", "cam").WithGroup("req").Info("two", "path", "/x", "err", io.EOF)
	logger.Info("three")

	entries := ring.Entries()
	require.Len(t, entries, 2)
	require.Equal(t, "two", entries[0].Message)
	require.Equal(t, "three", entries[1].Message)

	got := map[string]string{}
	for _, a := range entries[0].Attrs {
		got[a.Key] = a.Value.String()
	}
	require.Equal(t, map[string]string{"instance": "cam", "req.path": "/x", "req.err": "EOF"}, got)
}

func TestRingDropsTheOldestRecordsPastItsByteBound(t *testing.T) {
	ring := observability.NewLogRing(100, 10)
	logger := slog.New(ring.Handler(enabledDiscard()))

	logger.Info("aaaa")
	logger.Info("bbbb")
	logger.Info("cccc")

	entries := ring.Entries()
	require.Len(t, entries, 2)
	require.Equal(t, "bbbb", entries[0].Message)

	logger.Info("a line longer than the whole bound")
	require.Len(t, ring.Entries(), 1, "the newest record is kept whatever its length")
}

func TestRingIsSafeUnderConcurrentLogging(t *testing.T) {
	ring := observability.NewLogRing(16, 1<<20)
	logger := slog.New(ring.Handler(enabledDiscard()))

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 200 {
				logger.Info("tick")
				_ = ring.Entries()
			}
		})
	}
	wg.Wait()
	require.Len(t, ring.Entries(), 16)
}

// enabledDiscard is a handler that accepts every record and writes none.
// slog.DiscardHandler refuses them all, and the ring keeps only what its inner
// handler would write.
func enabledDiscard() slog.Handler {
	return slog.NewTextHandler(io.Discard, nil) //nolint:sloglint // see above
}
