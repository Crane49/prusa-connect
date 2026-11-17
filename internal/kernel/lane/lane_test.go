package lane_test

import (
	"testing"

	"github.com/prusa3d/prusa-connect/internal/kernel/lane"
)

func TestABackgroundIsPromotedOnce(t *testing.T) {
	t.Parallel()

	b := lane.NewBackground()

	if got := lane.BackgroundOf(t.Context()); got != nil {
		t.Fatalf("a plain context carried %p, and nil is what says a person is waiting", got)
	}

	if got := lane.BackgroundOf(lane.WithBackground(t.Context(), b)); got != b {
		t.Fatalf("WithBackground carried %p, want %p", got, b)
	}

	b.Promote()
	b.Promote()

	select {
	case <-b.Promoted():
	default:
		t.Fatal("a promoted command's channel is still open")
	}

	var none *lane.Background

	none.Promote()

	if none.Promoted() != nil {
		t.Fatal("a nil Background handed back a channel, and a select needs nil to block")
	}
}
