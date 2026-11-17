package bridge_test

import (
	"encoding/json"
	"testing"

	"github.com/prusa3d/fractal-link/bridge"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// copyOf builds a shadow that selects one interface of the node hq/workshop,
// and returns the key of that interface.
func copyOf(t *testing.T) (*shadow.Store, shadow.Key) {
	t.Helper()

	key := shadow.Key{Node: "hq/workshop", Interface: "Printer1", Instance: "1"}

	store, err := shadow.New(shadow.Config{Selection: shadow.Selection{Interfaces: []shadow.Key{key}}})
	if err != nil {
		t.Fatalf("build the shadow: %v", err)
	}

	return store, key
}

// TestTheCopyIsReadAsItStands: a ShadowView returns the node, the interface,
// the status, the timestamp and the values of an interface in the shadow. Its
// counts follow the status from EMPTY to OK to STALE.
func TestTheCopyIsReadAsItStands(t *testing.T) {
	t.Parallel()

	store, key := copyOf(t)
	view := bridge.NewShadowView(store)

	if counted := view.Count(); counted.Interfaces != 1 || counted.Empty != 1 {
		t.Fatalf("the counts before the first report are %+v, want one EMPTY interface", counted)
	}

	result := store.ApplyREP(fractal.REP{
		Version:   fractal.Version,
		Sender:    key.Node,
		Interface: key.Interface,
		Instance:  key.Instance,
		Timestamp: fractal.TimestampOf(at()),
		Props:     fractal.Props{"state": json.RawMessage(`"PRINTING"`)},
	})
	if result.Outcome != shadow.OutcomeApplied {
		t.Fatalf("the shadow did not apply the state report: %s", result.Outcome)
	}

	held := view.Subtree("hq")
	if len(held) != 1 {
		t.Fatalf("Subtree returned %d interfaces, want 1", len(held))
	}

	one := held[0]
	if one.Node != key.Node || one.Interface != key.Interface || one.Instance != key.Instance {
		t.Errorf("the interface is %s %s/%s, want %s", one.Node, one.Interface, one.Instance, key)
	}

	if one.Status != shadow.StatusOK.String() {
		t.Errorf("the status is %s, want OK", one.Status)
	}

	if one.UpdatedAt != uint64(fractal.TimestampOf(at())) {
		t.Errorf("UpdatedAt is %d, want the timestamp of the state report", one.UpdatedAt)
	}

	if string(one.Props["state"]) != `"PRINTING"` {
		t.Errorf("the state is %s, want \"PRINTING\"", one.Props["state"])
	}

	if counted := view.Count(); counted.OK != 1 || counted.Empty != 0 {
		t.Fatalf("the counts after one state report are %+v, want one OK interface", counted)
	}

	store.MarkStale(key.Node)

	if counted := view.Count(); counted.Stale != 1 {
		t.Fatalf("the counts after MarkStale are %+v, want one STALE interface", counted)
	}
}

// TestACopyThatIsNotThereReadsAsEmpty: a ShadowView with a nil store returns
// empty results, so its caller does not check for nil.
func TestACopyThatIsNotThereReadsAsEmpty(t *testing.T) {
	t.Parallel()

	view := bridge.NewShadowView(nil)

	if len(view.Subtree("hq")) != 0 {
		t.Error("a ShadowView with a nil store returned an interface")
	}

	if counted := view.Count(); counted.Interfaces != 0 {
		t.Errorf("a ShadowView with a nil store counts %+v, want zero", counted)
	}
}
