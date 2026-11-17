package shadow_test

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// The Examples section of fractal.md uses a network of the nodes hq, brno
// below hq, and printer-1 below brno. Both hq and brno keep a shadow of the
// FDMPrinter1/1 interface of printer-1.
const (
	printerPath = fractal.NodePath("hq/brno/printer-1")
	brnoPath    = fractal.NodePath("hq/brno")
)

// printerKey is the key of that interface.
var printerKey = shadow.Key{Node: printerPath, Interface: "FDMPrinter1", Instance: "1"}

// renderShadow returns a shadow interface as text, in the format the Examples
// section of fractal.md shows after each step. The test compares this text
// with the text copied from fractal.md.
func renderShadow(t *testing.T, store *shadow.Store, key shadow.Key) string {
	t.Helper()
	snap, ok := store.Get(key)
	if !ok {
		t.Fatalf("%s: the store does not have this interface", key)
	}
	var b strings.Builder
	b.WriteString("status: " + snap.Status.String() + "\n")
	b.WriteString("updatedAt: ")
	if snap.Status == shadow.StatusEmpty {
		b.WriteString("null\n")
	} else {
		b.WriteString(strconv.FormatUint(uint64(snap.UpdatedAt), 10) + "\n")
	}
	if len(snap.Props) == 0 {
		return b.String()
	}
	b.WriteString("props:\n")
	names := make([]fractal.PropertyName, 0, len(snap.Props))
	for name := range snap.Props {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		b.WriteString("  " + string(name) + ": " + string(snap.Props[name]) + "\n")
	}
	return b.String()
}

// newExampleStore builds the Store that hq and brno each have in the examples.
// Its Selection names the interface of printer-1 in Selection.Interfaces.
func newExampleStore(t *testing.T) *shadow.Store {
	t.Helper()
	store, err := shadow.New(shadow.Config{
		Selection: shadow.Selection{Interfaces: []shadow.Key{printerKey}},
	})
	if err != nil {
		t.Fatalf("build the store: %v", err)
	}
	return store
}

// printerREP builds the REP printer-1 sends in the examples.
func printerREP(ts fractal.Timestamp, props fractal.Props) fractal.REP {
	return fractal.REP{
		Version:   fractal.Version,
		Sender:    printerPath,
		Interface: printerKey.Interface,
		Instance:  printerKey.Instance,
		Timestamp: ts,
		Props:     props,
	}
}

// applyAtBoth applies one REP to the Store of brno and then to the Store of
// hq. In the examples brno applies the REP and forwards it unchanged to hq.
func applyAtBoth(t *testing.T, brno, hq *shadow.Store, m fractal.REP) (brnoRes, hqRes shadow.Result) {
	t.Helper()
	brnoRes = brno.ApplyREP(m)
	hqRes = hq.ApplyREP(m)
	return brnoRes, hqRes
}

// wantShadowAtBoth checks that both Stores render as want. fractal.md shows
// the shadow once per step and says it is the same at hq and at brno.
func wantShadowAtBoth(t *testing.T, brno, hq *shadow.Store, want string) {
	t.Helper()
	if got := renderShadow(t, brno, printerKey); got != want {
		t.Errorf("brno shadow:\n%s\nwant:\n%s", got, want)
	}
	if got := renderShadow(t, hq, printerKey); got != want {
		t.Errorf("hq shadow:\n%s\nwant:\n%s", got, want)
	}
}

// TestSpecExamples runs the examples of fractal.md in order on a Store for hq
// and a Store for brno. After each step it compares each Store with the shadow
// fractal.md shows for that step. Each example continues from the state of
// the one before, so the steps do not run in parallel.
func TestSpecExamples(t *testing.T) {
	brno, hq := newExampleStore(t), newExampleStore(t)

	t.Run("example 1 startup", func(t *testing.T) {
		wantShadowAtBoth(t, brno, hq, "status: EMPTY\nupdatedAt: null\n")
		brnoRes, hqRes := applyAtBoth(t, brno, hq, printerREP(1751400000000, fractal.Props{
			"nozzleTemp":       json.RawMessage("25.0"),
			"nozzleTempTarget": json.RawMessage("null"),
		}))
		for _, res := range []shadow.Result{brnoRes, hqRes} {
			if res.Outcome != shadow.OutcomeApplied {
				t.Fatalf("outcome = %s, want applied", res.Outcome)
			}
			if res.Previous != shadow.StatusEmpty || res.Status != shadow.StatusOK {
				t.Fatalf("transition %s to %s, want EMPTY to OK", res.Previous, res.Status)
			}
		}
		wantShadowAtBoth(t, brno, hq, "status: OK\nupdatedAt: 1751400000000\nprops:\n"+
			"  nozzleTemp: 25.0\n  nozzleTempTarget: null\n")
	})

	t.Run("example 2 state change", func(t *testing.T) {
		applyAtBoth(t, brno, hq, printerREP(1751400010000, fractal.Props{
			"nozzleTemp": json.RawMessage("200.0"),
		}))
		wantShadowAtBoth(t, brno, hq, "status: OK\nupdatedAt: 1751400010000\nprops:\n"+
			"  nozzleTemp: 200.0\n  nozzleTempTarget: null\n")
	})

	t.Run("example 3 desired properties", func(t *testing.T) {
		applyAtBoth(t, brno, hq, printerREP(1751400020000, fractal.Props{
			"nozzleTempTarget": json.RawMessage("220.0"),
		}))
		wantShadowAtBoth(t, brno, hq, "status: OK\nupdatedAt: 1751400020000\nprops:\n"+
			"  nozzleTemp: 200.0\n  nozzleTempTarget: 220.0\n")
	})

	t.Run("example 4 method invocation", func(t *testing.T) {
		applyAtBoth(t, brno, hq, printerREP(1751400025000, fractal.Props{
			"nozzleTempTarget": json.RawMessage("250.0"),
		}))
		wantShadowAtBoth(t, brno, hq, "status: OK\nupdatedAt: 1751400025000\nprops:\n"+
			"  nozzleTemp: 200.0\n  nozzleTempTarget: 250.0\n")
	})

	t.Run("example 5 connection loss", func(t *testing.T) {
		changed := brno.MarkStale(printerPath)
		if len(changed) != 1 || changed[0] != printerKey {
			t.Fatalf("brno marked %v, want the one interface of printer-1", changed)
		}
		los := fractal.LOS{Version: fractal.Version, Sender: brnoPath, Child: "printer-1"}
		atHQ, err := hq.ApplyLOS(los)
		if err != nil {
			t.Fatalf("hq applies the LOS: %v", err)
		}
		if len(atHQ) != 1 || atHQ[0] != printerKey {
			t.Fatalf("hq marked %v, want the one interface of printer-1", atHQ)
		}
		wantShadowAtBoth(t, brno, hq, "status: STALE\nupdatedAt: 1751400025000\nprops:\n"+
			"  nozzleTemp: 200.0\n  nozzleTempTarget: 250.0\n")
	})

	t.Run("example 6 reconnection", func(t *testing.T) {
		brnoRes, hqRes := applyAtBoth(t, brno, hq, printerREP(1751400120000, fractal.Props{
			"nozzleTemp":       json.RawMessage("230.0"),
			"nozzleTempTarget": json.RawMessage("250.0"),
		}))
		for _, res := range []shadow.Result{brnoRes, hqRes} {
			if res.Previous != shadow.StatusStale || res.Status != shadow.StatusOK {
				t.Fatalf("transition %s to %s, want STALE to OK", res.Previous, res.Status)
			}
		}
		wantShadowAtBoth(t, brno, hq, "status: OK\nupdatedAt: 1751400120000\nprops:\n"+
			"  nozzleTemp: 230.0\n  nozzleTempTarget: 250.0\n")
	})
}
