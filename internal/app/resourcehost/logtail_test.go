package resourcehost

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/observability"
)

// enabled is a handler that accepts every level, which slog.DiscardHandler does
// not, and the ring only sees a record its inner handler enables.
func enabled() slog.Handler { return slog.NewTextHandler(&bytes.Buffer{}, nil) }

func logOf(t *testing.T, w *memoryWatch, write func(*slog.Logger)) []string {
	t.Helper()

	write(slog.New(w.log.Handler(enabled())))

	var texts []string
	for _, line := range stderrLines(w.log) {
		texts = append(texts, line.Text)
	}

	return texts
}

func TestTheTailKeepsOnlyModuleStderr(t *testing.T) {
	t.Parallel()

	got := logOf(t, newMemoryWatch(1), func(l *slog.Logger) {
		l.Info("from the module", observability.ModuleStderrKey, true)
		l.Info("from the host")
	})

	require.Equal(t, []string{"from the module"}, got)
}

func TestTheTailKeepsTheNewest200Lines(t *testing.T) {
	t.Parallel()

	w := newMemoryWatch(1)
	got := logOf(t, w, func(l *slog.Logger) {
		for i := range 250 {
			l.Info(fmt.Sprintf("line %d", i), observability.ModuleStderrKey, true)
		}
	})

	require.Len(t, got, 200)
	require.Equal(t, "line 249", got[len(got)-1])

	long := newMemoryWatch(1)
	got = logOf(t, long, func(l *slog.Logger) {
		for range 100 {
			l.Info(strings.Repeat("x", 1024), observability.ModuleStderrKey, true)
		}
	})

	var total int
	for _, text := range got {
		total += len(text)
	}

	require.LessOrEqual(t, total, 64<<10)
}

func TestASplitLineIsMarked(t *testing.T) {
	t.Parallel()

	w := newMemoryWatch(1)
	slog.New(w.log.Handler(enabled())).Info("part", observability.ModuleStderrKey, true, splitAttribute, true)

	lines := stderrLines(w.log)
	require.Len(t, lines, 1)
	require.True(t, lines[0].Split)
}

// A reading the host could not take stays absent on the wire; reading it as
// zero would draw a module that is fine where its memory is unknown.
func TestAnUnreadSampleIsAbsentNotZero(t *testing.T) {
	t.Parallel()

	w := newMemoryWatch(1 << 30)
	t0 := time.Now()
	w.sample(t0.Add(-2*time.Hour), 7, nil)
	w.sample(t0, 1<<20, nil)
	w.sample(t0.Add(2*time.Second), 0, errors.New("no RssAnon"))

	points := w.points(t0.Add(-time.Hour))
	require.Len(t, points, 2, "a reading older than the window was served")
	require.Equal(t, int64(1<<20), *points[0].Value)
	require.Nil(t, points[1].Value)
}
