package shadow_test

import (
	"encoding/json"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// TestStatusTransitions checks Allowed for every pair of statuses against the
// status table of fractal.md, Shadow Status. The cases have the allowed
// changes and the ones that are not allowed.
func TestStatusTransitions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		from shadow.Status
		to   shadow.Status
		want bool
	}{
		{name: "empty stays empty on a loss", from: shadow.StatusEmpty, to: shadow.StatusEmpty, want: true},
		{name: "empty to ok when state arrives", from: shadow.StatusEmpty, to: shadow.StatusOK, want: true},
		{name: "empty to stale is forbidden", from: shadow.StatusEmpty, to: shadow.StatusStale},
		{name: "ok to empty is forbidden", from: shadow.StatusOK, to: shadow.StatusEmpty},
		{name: "ok to ok when state arrives", from: shadow.StatusOK, to: shadow.StatusOK, want: true},
		{name: "ok to stale when connectivity is lost", from: shadow.StatusOK, to: shadow.StatusStale, want: true},
		{name: "stale to empty is forbidden", from: shadow.StatusStale, to: shadow.StatusEmpty},
		{name: "stale to ok when state arrives", from: shadow.StatusStale, to: shadow.StatusOK, want: true},
		{name: "stale stays stale", from: shadow.StatusStale, to: shadow.StatusStale, want: true},
		{name: "an unknown source status", from: 0, to: shadow.StatusOK},
		{name: "an unknown target status", from: shadow.StatusOK, to: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := shadow.Allowed(tc.from, tc.to); got != tc.want {
				t.Fatalf("Allowed(%s, %s) = %v, want %v", tc.from, tc.to, got, tc.want)
			}
		})
	}
}

// TestStatusNames checks that String returns the names fractal.md uses.
func TestStatusNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status shadow.Status
		want   string
		valid  bool
	}{
		{status: shadow.StatusEmpty, want: "EMPTY", valid: true},
		{status: shadow.StatusOK, want: "OK", valid: true},
		{status: shadow.StatusStale, want: "STALE", valid: true},
		{status: 0, want: "unknown"},
		{status: 99, want: "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if got := tc.status.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
			if got := tc.status.Valid(); got != tc.valid {
				t.Errorf("Valid() = %v, want %v", got, tc.valid)
			}
		})
	}
	if got := shadow.Statuses(); len(got) != 3 {
		t.Fatalf("Statuses() = %v, want EMPTY, OK and STALE", got)
	}
}

// TestForbiddenTransitionsCannotHappen calls ApplyREP and MarkStale on one
// interface in a fixed order. After each call it checks with Allowed that the
// status table permits the change from the status before the call to the
// status after it.
func TestForbiddenTransitionsCannotHappen(t *testing.T) {
	t.Parallel()
	key := shadow.Key{Node: "hq/brno/printer-1", Interface: "FDMPrinter1", Instance: "1"}
	store, err := shadow.New(shadow.Config{Selection: shadow.Selection{Interfaces: []shadow.Key{key}}})
	if err != nil {
		t.Fatalf("build the store: %v", err)
	}
	rep := fractal.REP{
		Version:   fractal.Version,
		Sender:    key.Node,
		Interface: key.Interface,
		Instance:  key.Instance,
		Props:     fractal.Props{"nozzleTemp": json.RawMessage("25.0")},
	}
	// The steps take the interface from EMPTY to OK, to STALE, back to OK and
	// to STALE again.
	steps := []struct {
		name string
		run  func()
	}{
		{name: "a rep on an empty interface", run: func() { rep.Timestamp = 10; store.ApplyREP(rep) }},
		{name: "loss of the node", run: func() { store.MarkStale(key.Node) }},
		{name: "loss of the node again", run: func() { store.MarkStale(key.Node) }},
		{name: "a rep on a stale interface", run: func() { rep.Timestamp = 20; store.ApplyREP(rep) }},
		{name: "an older rep", run: func() { rep.Timestamp = 5; store.ApplyREP(rep) }},
		{name: "loss of the node above the node", run: func() { store.MarkStale("hq/brno") }},
	}
	before, _, ok := store.Status(key)
	if !ok {
		t.Fatal("New did not create the interface in Selection.Interfaces")
	}
	for _, step := range steps {
		step.run()
		after, _, found := store.Status(key)
		if !found {
			t.Fatalf("%s: the interface disappeared", step.name)
		}
		if !shadow.Allowed(before, after) {
			t.Fatalf("%s: the status changed from %s to %s, which the table does not allow", step.name, before, after)
		}
		before = after
	}
}
