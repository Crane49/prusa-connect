package app

import (
	"context"
	"log/slog"
	"math"
)

// applyMemoryLimit hands core.memory_limit to the Go runtime and logs it. Go
// then collects garbage more often to keep its memory under the limit.
//
// A zero limit changes nothing, so the runtime keeps the limit it read from
// GOMEMLIMIT at start, or none. set is debug.SetMemoryLimit outside a test.
func applyMemoryLimit(ctx context.Context, limit int64, logger *slog.Logger, set func(int64) int64) {
	if limit == 0 {
		return
	}

	previous := set(limit)

	attrs := []any{"bytes", limit, "key", "core.memory_limit"}

	// MaxInt64 is how the runtime says that no limit was set. Any other value
	// came from GOMEMLIMIT, and the operator should see that the key won.
	if previous != math.MaxInt64 {
		attrs = append(attrs, "replaced", previous)
	}

	logger.InfoContext(ctx, "memory limit set", attrs...)
}
