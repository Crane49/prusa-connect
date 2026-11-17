package shadow_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// newStore builds a store or fails the test.
func newStore(t *testing.T, cfg shadow.Config) *shadow.Store {
	t.Helper()
	store, err := shadow.New(cfg)
	if err != nil {
		t.Fatalf("build the store: %v", err)
	}
	return store
}

// rep builds a REP for one key.
func rep(key shadow.Key, ts fractal.Timestamp, props fractal.Props) fractal.REP {
	return fractal.REP{
		Version:   fractal.Version,
		Sender:    key.Node,
		Interface: key.Interface,
		Instance:  key.Instance,
		Timestamp: ts,
		Props:     props,
	}
}

// TestApplyREPStaleGuard checks the rule of fractal.md, Reported Properties
// Message: ApplyREP discards a REP whose timestamp is lower than the timestamp
// of the interface, and applies a REP with an equal timestamp.
func TestApplyREPStaleGuard(t *testing.T) {
	t.Parallel()
	key := shadow.Key{Node: "hq/brno/printer-1", Interface: "FDMPrinter1", Instance: "1"}
	tests := []struct {
		name    string
		second  fractal.Timestamp
		want    shadow.Outcome
		wantVal string
	}{
		{name: "a newer timestamp is applied", second: 101, want: shadow.OutcomeApplied, wantVal: "2"},
		{name: "an equal timestamp is applied", second: 100, want: shadow.OutcomeApplied, wantVal: "2"},
		{name: "an older timestamp is discarded", second: 99, want: shadow.OutcomeDiscarded, wantVal: "1"},
		{name: "a much older timestamp is discarded", second: 0, want: shadow.OutcomeDiscarded, wantVal: "1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newStore(t, shadow.Config{Selection: shadow.Selection{Interfaces: []shadow.Key{key}}})
			store.ApplyREP(rep(key, 100, fractal.Props{"n": json.RawMessage("1")}))
			res := store.ApplyREP(rep(key, tc.second, fractal.Props{"n": json.RawMessage("2")}))
			if res.Outcome != tc.want {
				t.Fatalf("outcome = %s, want %s", res.Outcome, tc.want)
			}
			value, ok := store.Prop(key, "n")
			if !ok || string(value) != tc.wantVal {
				t.Fatalf("n = %q %v, want %q", value, ok, tc.wantVal)
			}
			_, updatedAt, _ := store.Status(key)
			wantAt := fractal.Timestamp(100)
			if tc.want == shadow.OutcomeApplied {
				wantAt = tc.second
			}
			if updatedAt != wantAt {
				t.Fatalf("updatedAt = %d, want %d", updatedAt, wantAt)
			}
		})
	}
}

// TestApplyREPEmptyInterfaceTakesAnyTimestamp checks that ApplyREP applies a
// REP with any timestamp to an EMPTY interface. An EMPTY interface has no
// timestamp, so no REP is older than it.
func TestApplyREPEmptyInterfaceTakesAnyTimestamp(t *testing.T) {
	t.Parallel()
	key := shadow.Key{Node: "hq/printer-1", Interface: "FDMPrinter1", Instance: "1"}
	store := newStore(t, shadow.Config{Selection: shadow.Selection{Interfaces: []shadow.Key{key}}})
	status, updatedAt, ok := store.Status(key)
	if !ok || status != shadow.StatusEmpty || updatedAt != 0 {
		t.Fatalf("a new interface is %s at %d, want EMPTY with no timestamp", status, updatedAt)
	}
	res := store.ApplyREP(rep(key, 0, fractal.Props{"n": json.RawMessage("1")}))
	if res.Outcome != shadow.OutcomeApplied || res.Previous != shadow.StatusEmpty {
		t.Fatalf("outcome %s from %s, want applied from EMPTY", res.Outcome, res.Previous)
	}
}

// TestApplyREPMergesProps checks the merge rule of fractal.md: a REP replaces
// the property values it contains, and the interface keeps every property the
// REP does not contain.
func TestApplyREPMergesProps(t *testing.T) {
	t.Parallel()
	key := shadow.Key{Node: "hq/printer-1", Interface: "FDMPrinter1", Instance: "1"}
	store := newStore(t, shadow.Config{Selection: shadow.Selection{Interfaces: []shadow.Key{key}}})
	store.ApplyREP(rep(key, 10, fractal.Props{
		"a": json.RawMessage("1"),
		"b": json.RawMessage("2"),
	}))
	store.ApplyREP(rep(key, 20, fractal.Props{
		"b": json.RawMessage("22"),
		"c": json.RawMessage("3"),
	}))
	snap, ok := store.Get(key)
	if !ok {
		t.Fatal("the interface is gone")
	}
	want := map[fractal.PropertyName]string{"a": "1", "b": "22", "c": "3"}
	if len(snap.Props) != len(want) {
		t.Fatalf("props = %v, want %v", snap.Props, want)
	}
	for name, value := range want {
		if got := string(snap.Props[name]); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
}

// TestApplyREPNotSelected checks that a REP for an interface the Selection
// does not cover creates nothing and gets OutcomeNotSelected, which tells the
// node to forward the REP anyway. The cases use the same Store, so they run in
// order and not in parallel.
func TestApplyREPNotSelected(t *testing.T) {
	selected := shadow.Key{Node: "hq/printer-1", Interface: "FDMPrinter1", Instance: "1"}
	store := newStore(t, shadow.Config{Selection: shadow.Selection{Interfaces: []shadow.Key{selected}}})
	tests := []struct {
		name string
		key  shadow.Key
		want shadow.Outcome
	}{
		{name: "another node", key: shadow.Key{
			Node: "hq/printer-2", Interface: "FDMPrinter1", Instance: "1",
		}, want: shadow.OutcomeNotSelected},
		{name: "another interface", key: shadow.Key{
			Node: "hq/printer-1", Interface: "Camera1", Instance: "1",
		}, want: shadow.OutcomeNotSelected},
		{name: "another instance", key: shadow.Key{
			Node: "hq/printer-1", Interface: "FDMPrinter1", Instance: "2",
		}, want: shadow.OutcomeNotSelected},
		{name: "the selected interface", key: selected, want: shadow.OutcomeApplied},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := store.ApplyREP(rep(tc.key, 10, fractal.Props{"n": json.RawMessage("1")}))
			if res.Outcome != tc.want {
				t.Fatalf("outcome = %s, want %s", res.Outcome, tc.want)
			}
			if _, _, ok := store.Status(tc.key); ok != (tc.want == shadow.OutcomeApplied) {
				t.Fatalf("the store has the interface: %v, want %v", ok, !ok)
			}
		})
	}
	if got := store.Len(); got != 1 {
		t.Fatalf("the store holds %d interfaces, want 1", got)
	}
}

// TestApplyREPPatternCreatesInterface checks the rule of fractal.md, Shadow
// Selection: the Store creates an interface that a pattern selects when the
// first REP for it arrives, with StatusEmpty. A pattern is not compared with
// the instance id.
func TestApplyREPPatternCreatesInterface(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pattern fractal.Pattern
		key     shadow.Key
		want    shadow.Outcome
	}{
		{name: "everything", pattern: "*", key: shadow.Key{
			Node: "hq/a/b", Interface: "Camera1", Instance: "7",
		}, want: shadow.OutcomeApplied},
		{name: "a prefix", pattern: "FDM*", key: shadow.Key{
			Node: "hq/a", Interface: "FDMPrinter1", Instance: "1",
		}, want: shadow.OutcomeApplied},
		{name: "any instance of a matching interface", pattern: "FDMPrinter1", key: shadow.Key{
			Node: "hq/a", Interface: "FDMPrinter1", Instance: "99",
		}, want: shadow.OutcomeApplied},
		{name: "a name the pattern misses", pattern: "FDM*", key: shadow.Key{
			Node: "hq/a", Interface: "Camera1", Instance: "1",
		}, want: shadow.OutcomeNotSelected},
		{name: "case matters", pattern: "fdm*", key: shadow.Key{
			Node: "hq/a", Interface: "FDMPrinter1", Instance: "1",
		}, want: shadow.OutcomeNotSelected},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newStore(t, shadow.Config{
				Selection: shadow.Selection{Patterns: []fractal.Pattern{tc.pattern}},
			})
			res := store.ApplyREP(rep(tc.key, 10, fractal.Props{"n": json.RawMessage("1")}))
			if res.Outcome != tc.want {
				t.Fatalf("outcome = %s, want %s", res.Outcome, tc.want)
			}
			if tc.want != shadow.OutcomeApplied {
				return
			}
			if !res.Created {
				t.Error("the result does not say the interface was created")
			}
			if res.Previous != shadow.StatusEmpty {
				t.Errorf("created as %s, want EMPTY before the message is applied", res.Previous)
			}
			if res.Status != shadow.StatusOK {
				t.Errorf("status = %s, want OK", res.Status)
			}
		})
	}
}

// TestApplyREPSecondMessageDoesNotCreateAgain checks that the second REP for
// an interface a pattern selects updates the interface the first REP created.
func TestApplyREPSecondMessageDoesNotCreateAgain(t *testing.T) {
	t.Parallel()
	store := newStore(t, shadow.Config{Selection: shadow.Selection{Patterns: []fractal.Pattern{"*"}}})
	key := shadow.Key{Node: "hq/a", Interface: "Camera1", Instance: "1"}
	store.ApplyREP(rep(key, 10, nil))
	res := store.ApplyREP(rep(key, 20, nil))
	if res.Created {
		t.Error("the second message created the interface again")
	}
	if res.Previous != shadow.StatusOK {
		t.Errorf("previous = %s, want OK", res.Previous)
	}
	if got := store.Len(); got != 1 {
		t.Fatalf("the store holds %d interfaces, want 1", got)
	}
}

// TestApplyREPTakesALargeReport checks that ApplyREP applies a report
// whatever its size. fractal.md says a node MUST update the interface with
// the new state, and the network holds only nodes that report what they own.
func TestApplyREPTakesALargeReport(t *testing.T) {
	t.Parallel()
	key := shadow.Key{Node: "hq/a", Interface: "Camera1", Instance: "1"}
	store := newStore(t, shadow.Config{Selection: shadow.Selection{Patterns: []fractal.Pattern{"*"}}})
	big := json.RawMessage(`"` + strings.Repeat("x", 128<<10) + `"`)
	res := store.ApplyREP(rep(key, 10, fractal.Props{"big": big}))
	got, _ := store.Prop(key, "big")
	if res.Outcome != shadow.OutcomeApplied || !bytes.Equal(got, big) {
		t.Errorf("outcome = %s and the store holds %d bytes, want applied and %d", res.Outcome, len(got), len(big))
	}
}

// TestOutcomeNames checks the text String returns for each Outcome.
func TestOutcomeNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		outcome shadow.Outcome
		want    string
	}{
		{outcome: shadow.OutcomeApplied, want: "applied"},
		{outcome: shadow.OutcomeDiscarded, want: "discarded"},
		{outcome: shadow.OutcomeNotSelected, want: "not selected"},
		{outcome: 0, want: "unknown"},
		{outcome: 99, want: "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if got := tc.outcome.String(); got != tc.want {
				t.Fatalf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}
