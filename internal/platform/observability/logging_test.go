package observability_test

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/observability"
)

// counted is a counter handle that remembers what it was told, so a test can
// assert that something writes the series the catalog declares.
type counted struct {
	mu     sync.Mutex
	totals map[string]float64
}

func newCounted() *counted { return &counted{totals: map[string]float64{}} }

func (c *counted) at(level string) observability.Counter { return handle{owner: c, level: level} }

func (c *counted) total(level string) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.totals[level]
}

type handle struct {
	owner *counted
	level string
}

func (h handle) Add(delta float64) {
	h.owner.mu.Lock()
	defer h.owner.mu.Unlock()

	h.owner.totals[h.level] += delta
}

// The catalog declares pcl_log_lines_total, and this handler is what writes it.
// A declared series nothing writes is a name an operator can point a dashboard
// at and never see.
func TestTheLogHandlerCountsEveryLineItWrites(t *testing.T) {
	t.Parallel()

	emitted, dropped := newCounted(), newCounted()
	out := &bytes.Buffer{}

	logger := slog.New(observability.NewLogHandler(
		slog.NewJSONHandler(out, &slog.HandlerOptions{Level: slog.LevelDebug}),
		observability.LogConfig{
			Emitted:  emitted.at,
			Dropped:  dropped.at,
			Sampling: observability.LogSampling{First: 1, Thereafter: 1},
		}))

	logger.Info("one")
	logger.Warn("two")
	logger.Warn("three")
	logger.Error("four")
	logger.Debug("five")

	require.Equal(t, float64(1), emitted.total("info"))
	require.Equal(t, float64(2), emitted.total("warn"))
	require.Equal(t, float64(1), emitted.total("error"))
	require.Equal(t, float64(1), emitted.total("debug"))
	require.Zero(t, dropped.total("debug"), "nothing was sampled away at a divisor of one")

	require.Equal(t, 5, strings.Count(out.String(), "\n"), "a counted line was not written")

	// A logger built with attributes counts into the same handles. A wrapper
	// that rebuilt them per WithAttrs would count a request-scoped logger's
	// lines nowhere.
	logger.With("printer", "p1").WithGroup("link").Info("six")
	require.Equal(t, float64(2), emitted.total("info"))
}

// Both series exist together. An operator reading a debug volume that suddenly
// fell cannot otherwise tell a quiet process from a sampled one.
func TestSamplingIsCountedRatherThanSilent(t *testing.T) {
	t.Parallel()

	emitted, dropped := newCounted(), newCounted()
	out := &bytes.Buffer{}

	// One clock reading for the whole run, so every line falls in one tick and
	// the result does not depend on how fast the test ran.
	frozen := time.Now()

	logger := slog.New(observability.NewLogHandler(
		slog.NewJSONHandler(out, &slog.HandlerOptions{Level: slog.LevelDebug}),
		observability.LogConfig{
			Emitted:  emitted.at,
			Dropped:  dropped.at,
			Sampling: observability.LogSampling{First: 2, Thereafter: 5, Tick: time.Minute},
			Now:      func() time.Time { return frozen },
		}))

	const lines = 22

	for range lines {
		logger.Debug("a message in a loop over four hundred printers")
	}

	// The first two pass, then one in five: lines 7, 12, 17 and 22.
	require.Equal(t, float64(6), emitted.total("debug"))
	require.Equal(t, float64(lines-6), dropped.total("debug"))
	require.Equal(t, 6, strings.Count(out.String(), "\n"))

	// A warning in the same loop is never sampled, whatever the debug line did.
	for range lines {
		logger.Warn("the same loop, one level up")
	}

	require.Equal(t, float64(lines), emitted.total("warn"))
	require.Zero(t, dropped.total("warn"))
}

// Two log sites do not share an allowance, and a process that logged a burst a
// minute ago is not still sampled for it.
func TestSamplingKeysOnTheMessageAndResetsPerTick(t *testing.T) {
	t.Parallel()

	emitted := newCounted()
	now := time.Now()

	logger := slog.New(observability.NewLogHandler(
		slog.NewJSONHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelDebug}),
		observability.LogConfig{
			Emitted:  emitted.at,
			Sampling: observability.LogSampling{First: 1, Thereafter: 1000, Tick: time.Second},
			Now:      func() time.Time { return now },
		}))

	logger.Debug("first site")
	logger.Debug("first site")
	logger.Debug("second site")

	require.Equal(t, float64(2), emitted.total("debug"),
		"one message's allowance was spent by another message")

	now = now.Add(2 * time.Second)

	logger.Debug("first site")
	require.Equal(t, float64(3), emitted.total("debug"), "the tick did not reset the count")
}

// The level label stays a closed set. A custom level is reported as the
// standard level at or below it, because a series per numeric level would be
// unbounded cardinality on the metric that measures how noisy the process is.
func TestALevelBetweenTwoStandardOnesDoesNotOpenASeries(t *testing.T) {
	t.Parallel()

	for level, want := range map[slog.Level]string{
		slog.LevelDebug:     "debug",
		slog.LevelDebug + 1: "debug",
		slog.LevelInfo:      "info",
		slog.LevelInfo + 3:  "info",
		slog.LevelWarn:      "warn",
		slog.LevelError:     "error",
		slog.LevelError + 9: "error",
	} {
		require.Equal(t, want, observability.LogLevelName(level), "level %s", level)
	}
}

// In a build with no observability tag the handles are nil. Every line is still
// written and nothing panics.
func TestTheHandlerRecordsNothingWithoutARegistry(t *testing.T) {
	t.Parallel()

	out := &bytes.Buffer{}
	logger := slog.New(observability.NewLogHandler(
		slog.NewJSONHandler(out, &slog.HandlerOptions{Level: slog.LevelDebug}),
		observability.LogConfig{}))

	logger.Info("one")
	logger.Debug("two")

	require.Equal(t, 2, strings.Count(out.String(), "\n"))
}
