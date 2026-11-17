package main

import (
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// TestANodeThatHandledNothingReportsNothing: a link that has handled no
// message reports no counters at all, so a page draws no table of zeroes.
func TestANodeThatHandledNothingReportsNothing(t *testing.T) {
	t.Parallel()

	if held := newCounters().reading(); held != nil {
		t.Fatalf("a node that handled nothing reported %+v, want nothing", held)
	}
}

// TestEveryKindAndOutcomeIsCountedUnderItsOwnName checks both halves: a pair
// that counted something has a row with the count, and a pair that counted
// nothing has no row.
func TestEveryKindAndOutcomeIsCountedUnderItsOwnName(t *testing.T) {
	t.Parallel()

	count := newCounters()

	count.Message(fractal.KindRPC, fractalnode.OutcomeProcessed)
	count.Message(fractal.KindRPC, fractalnode.OutcomeProcessed)
	count.Message(fractal.KindREP, fractalnode.OutcomeForwarded)
	count.Drop(fractal.KindREP, fractalnode.DropMalformed)

	held := count.reading()
	if held == nil {
		t.Fatal("a node that handled messages reported nothing")
	}

	if err := held.Validate(); err != nil {
		t.Fatalf("the contract refused the counters: %v", err)
	}

	if len(held.Messages) != 2 {
		t.Fatalf("the reading has %d message rows, want 2: %+v", len(held.Messages), held.Messages)
	}

	for _, row := range held.Messages {
		switch {
		case row.Kind == fractal.KindRPC.String() && row.Outcome == fractalnode.OutcomeProcessed.String():
			if row.Count != 2 {
				t.Errorf("the processed RPC count is %d, want 2", row.Count)
			}
		case row.Kind == fractal.KindREP.String() && row.Outcome == fractalnode.OutcomeForwarded.String():
			if row.Count != 1 {
				t.Errorf("the forwarded REP count is %d, want 1", row.Count)
			}
		default:
			t.Errorf("the reading has a row nothing counted: %+v", row)
		}
	}

	if len(held.Drops) != 1 {
		t.Fatalf("the reading has %d drop rows, want 1: %+v", len(held.Drops), held.Drops)
	}

	if held.Drops[0].Kind != fractal.KindREP.String() ||
		held.Drops[0].Reason != fractalnode.DropMalformed.String() ||
		held.Drops[0].Count != 1 {
		t.Errorf("the drop row is %+v, want one malformed REP", held.Drops[0])
	}
}

// TestAValueOutsideTheSetsIsCountedNowhere: a build of this module against a
// later SDK must not write past the end of a slice, so a kind, an outcome or a
// reason this build does not know is counted nowhere.
func TestAValueOutsideTheSetsIsCountedNowhere(t *testing.T) {
	t.Parallel()

	count := newCounters()

	count.Message(fractal.Kind(0), fractalnode.OutcomeProcessed)
	count.Message(fractal.Kind(200), fractalnode.OutcomeProcessed)
	count.Message(fractal.KindRPC, fractalnode.Outcome(200))
	count.Drop(fractal.KindRPC, fractalnode.DropReason(200))
	count.Drop(fractal.Kind(200), fractalnode.DropMalformed)

	if held := count.reading(); held != nil {
		t.Fatalf("a value outside the sets was counted as %+v", held)
	}
}

// BenchmarkCountOneMessage measures what counting costs on the path every
// message takes. The engine calls it from several goroutines, so the number
// that matters is the one with more than one.
func BenchmarkCountOneMessage(b *testing.B) {
	count := newCounters()

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			count.Message(fractal.KindREP, fractalnode.OutcomeProcessed)
		}
	})
}
