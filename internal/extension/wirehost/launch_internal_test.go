package wirehost

import (
	"context"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
)

// One number governs a start. The supervisor bounds a start with the
// instance's start limit and hands that context to the launch, so the
// handshake line and the first call share what the operator configured rather
// than each taking a limit of the host's own that nothing can set.

// TestAStartTakesTheBudgetItsCallerBrought holds the derivation. The caller's
// deadline is the whole of the start, whether it is shorter or longer than the
// host's floor.
func TestAStartTakesTheBudgetItsCallerBrought(t *testing.T) {
	t.Parallel()

	for name, budget := range map[string]time.Duration{
		"shorter than the host's floor": startTimeout / 2,
		"longer than the host's floor":  startTimeout * 3,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), budget)
			defer cancel()

			asked, _ := ctx.Deadline()

			bounded, stop := startBound(ctx)
			defer stop()

			got, set := bounded.Deadline()
			if !set {
				t.Fatal("a start of a bounded caller is unbounded")
			}

			if !got.Equal(asked) {
				t.Fatalf("the start ends at %s and its caller asked for %s", got, asked)
			}
		})
	}
}

// TestAStartOfAnUnboundedCallerTakesTheHostFloor is the other half: a caller
// with no budget of its own still cannot wait for a child for ever.
func TestAStartOfAnUnboundedCallerTakesTheHostFloor(t *testing.T) {
	t.Parallel()

	bounded, stop := startBound(context.Background())
	defer stop()

	got, set := bounded.Deadline()
	if !set {
		t.Fatal("a start with no caller budget is unbounded")
	}

	if left := time.Until(got); left > startTimeout {
		t.Fatalf("the start has %s left and the host's floor is %s", left, startTimeout)
	}
}

// TestAnExitNobodyCouldReadIsNotAPanic is the child another reaper collected:
// the wait failed, there is no exit status, and the stop still says something.
func TestAnExitNobodyCouldReadIsNotAPanic(t *testing.T) {
	t.Parallel()

	exited := make(chan struct{})
	close(exited)

	got := (&Child{exited: exited}).ExitStop()

	want := supervisor.Stop{Result: supervisor.ResultExited, Code: -1}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
