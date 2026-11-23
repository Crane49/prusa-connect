package app

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// countingTable counts the sweeps it is asked for.
type countingTable struct{ swept atomic.Int32 }

func (c *countingTable) DeleteExpired(context.Context, time.Time) (int, error) {
	c.swept.Add(1)

	return 0, nil
}

// The credential table is swept on every tick while the server runs, not
// only when it starts, and the sweep ends with its context (ADR-20261112-nay).
func TestTheCredentialsAreSweptWhileTheServerRuns(t *testing.T) {
	t.Parallel()

	table := &countingTable{}
	ctx, stop := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		sweepCredentials(ctx, table, 5*time.Millisecond, slog.New(slog.DiscardHandler))
	}()

	require.Eventually(t, func() bool { return table.swept.Load() >= 3 }, 5*time.Second, time.Millisecond)

	stop()
	<-done
}
