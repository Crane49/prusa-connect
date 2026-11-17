package app

import (
	"bytes"
	"log/slog"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// runtimeLimit stands in for debug.SetMemoryLimit, so a test does not change
// the limit of the whole test binary.
type runtimeLimit struct {
	current int64
	calls   int
}

func (r *runtimeLimit) set(limit int64) int64 {
	previous := r.current

	if limit >= 0 {
		r.current = limit
		r.calls++
	}

	return previous
}

// A configured limit reaches the runtime, and the log line names it, because
// a limit an operator cannot read is one they cannot debug.
func TestAConfiguredMemoryLimitReachesTheRuntimeAndTheLog(t *testing.T) {
	t.Parallel()

	var logged bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&logged, nil))
	runtime := &runtimeLimit{current: math.MaxInt64}

	applyMemoryLimit(t.Context(), 12<<20, logger, runtime.set)

	require.Equal(t, int64(12<<20), runtime.current)
	require.Contains(t, logged.String(), "memory limit set")
	require.Contains(t, logged.String(), "bytes=12582912")
	require.NotContains(t, logged.String(), "replaced", "no limit was set before")
}

// The key wins over GOMEMLIMIT, and the log line says which value it replaced.
func TestAConfiguredMemoryLimitReplacesTheEnvironment(t *testing.T) {
	t.Parallel()

	var logged bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&logged, nil))
	runtime := &runtimeLimit{current: 64 << 20}

	applyMemoryLimit(t.Context(), 12<<20, logger, runtime.set)

	require.Equal(t, int64(12<<20), runtime.current)
	require.Contains(t, logged.String(), "replaced=67108864")
}

// Without the key the runtime keeps the limit it read from GOMEMLIMIT, and
// nothing is logged, so a normal build starts as it does without the key.
func TestNoMemoryLimitLeavesTheRuntimeAlone(t *testing.T) {
	t.Parallel()

	var logged bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&logged, nil))
	runtime := &runtimeLimit{current: math.MaxInt64}

	applyMemoryLimit(t.Context(), 0, logger, runtime.set)

	require.Zero(t, runtime.calls)
	require.Empty(t, logged.String())
}
