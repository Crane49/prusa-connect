package simulator

import (
	"testing"
)

func frameAt(t *testing.T, model string, printed int) map[string]any {
	t.Helper()

	p, err := New(Config{Model: model, BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("new %s: %v", model, err)
	}

	p.state = statePrinting
	p.job.frames = 100
	p.job.printed = printed

	p.mu.Lock()
	defer p.mu.Unlock()

	return p.telemetryFields()
}

// The XL reports five toolheads and moves the active one as the print runs.
func TestAnXLFrameCarriesTheSlotBlockAndTheActiveToolMoves(t *testing.T) {
	t.Parallel()

	early, _ := frameAt(t, "XL", 1)["slot"].(map[string]any)
	late, _ := frameAt(t, "XL", 95)["slot"].(map[string]any)

	if early == nil || late == nil {
		t.Fatal("an XL frame carries no slot block")
	}

	for _, key := range []string{"1", "2", "3", "4", "5"} {
		if _, ok := early[key].(map[string]any); !ok {
			t.Errorf("the slot block has no entry %q", key)
		}
	}

	first, _ := early["active"].(int)
	last, _ := late["active"].(int)

	if first < 1 || first > 5 || last < 1 || last > 5 {
		t.Fatalf("active tools %v and %v are outside 1..5", early["active"], late["active"])
	}

	if first == last {
		t.Errorf("the active tool stayed %d from the start to the end of the print", first)
	}
}

func TestAFrameFromAnotherModelCarriesNoSlotBlock(t *testing.T) {
	t.Parallel()

	if _, ok := frameAt(t, "MK4S", 1)["slot"]; ok {
		t.Error("a single-tool model reported a slot block")
	}
}

// Tool 3 is T2 on the wire. Only its slot reads the target.
func TestAnM104NamingAToolHeatsOnlyThatXLHead(t *testing.T) {
	t.Parallel()

	p, err := New(Config{Model: "XL", BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("new XL: %v", err)
	}

	p.noteToolTarget([]byte("M104 T2 S215"))

	p.mu.Lock()
	slot, _ := p.telemetryFields()["slot"].(map[string]any)
	p.mu.Unlock()

	for key, want := range map[string]float64{"1": ambientTemp, "3": 215} {
		tool, _ := slot[key].(map[string]any)
		if tool["temp"] != want {
			t.Errorf("slot %s reads %v, want %v", key, tool["temp"], want)
		}
	}
}
