package natsbind

import (
	"log/slog"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// TestEveryStateHasAWrittenForm checks that every UplinkState and every
// Admission has a text form, and that no two states share one. A page can then
// show the text directly.
func TestEveryStateHasAWrittenForm(t *testing.T) {
	t.Parallel()
	states := []UplinkState{
		UplinkNotConfigured, UplinkJoining, UplinkConnected, UplinkLost, UplinkDenied,
	}
	seen := map[string]bool{}
	for _, state := range states {
		text := state.String()
		if text == "" {
			t.Errorf("the state %v has an empty String", state)
		}
		if seen[text] {
			t.Errorf("two states have the String %q", text)
		}
		seen[text] = true
	}
	for _, admission := range []Admission{AdmissionPending, AdmissionAdmitted, AdmissionRevoked} {
		if admission.String() == "" {
			t.Errorf("the admission %v has an empty String", admission)
		}
	}
}

// TestTheNodesOfAReadingAreOrdered checks that Status.Nodes is sorted by Node
// ID, so a page shows its rows in the same order every time.
func TestTheNodesOfAReadingAreOrdered(t *testing.T) {
	t.Parallel()
	b := &Binding{
		cfg:    Config{NodeID: "hq"},
		links:  map[fractal.NodeID]*link{},
		uplink: Uplink{State: UplinkNotConfigured},
	}
	for _, id := range []fractal.NodeID{"printer-2", "printer-1", "printer-10"} {
		b.links[id] = &link{id: id, admission: AdmissionAdmitted}
	}
	nodes := b.Status().Nodes
	if len(nodes) != 3 {
		t.Fatalf("Status.Nodes has %d nodes, want 3", len(nodes))
	}
	for i := 1; i < len(nodes); i++ {
		if compareNodeID(nodes[i-1].ID, nodes[i].ID) >= 0 {
			t.Errorf("Status.Nodes has %q before %q", nodes[i-1].ID, nodes[i].ID)
		}
	}
	taken := 0
	for range b.Children() {
		taken++
		break
	}
	if taken != 1 {
		t.Errorf("a loop over Children that breaks after the first Node ID ran %d times", taken)
	}
}

// TestADifferentNodePathIsReported checks that relocated sends the new Node
// Path to the Relocated channel, and that a second call does not block when
// no goroutine reads the channel.
func TestADifferentNodePathIsReported(t *testing.T) {
	t.Parallel()
	b := &Binding{relocate: make(chan fractal.NodePath, 1)}
	b.relocated("hq/praha/printer-1")
	b.relocated("hq/praha/printer-2")
	select {
	case got := <-b.Relocated():
		if got != "hq/praha/printer-1" {
			t.Errorf("Relocated delivered %q, want hq/praha/printer-1", got)
		}
	default:
		t.Fatal("the Relocated channel is empty")
	}
}

// TestWhatTheServerLogsReachesTheNodesLogger calls every log method of
// serverLogger once. It checks only that none of them panics.
func TestWhatTheServerLogsReachesTheNodesLogger(t *testing.T) {
	t.Parallel()
	logger := newServerLogger(slog.New(slog.DiscardHandler))
	logger.Noticef("a notice %d", 1)
	logger.Warnf("a warning %d", 2)
	logger.Errorf("an error %d", 3)
	logger.Fatalf("what the server calls fatal %d", 4)
	logger.Debugf("a detail %d", 5)
	logger.Tracef("a trace %d", 6)
}
