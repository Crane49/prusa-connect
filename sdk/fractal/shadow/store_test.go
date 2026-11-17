package shadow_test

import (
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// TestNewCreatesEnumeratedInterfacesEmpty checks the first step of fractal.md,
// Synchronization of the Shadow - Happy path: New creates every interface in
// Selection.Interfaces with StatusEmpty, before any REP arrives.
func TestNewCreatesEnumeratedInterfacesEmpty(t *testing.T) {
	t.Parallel()
	keys := []shadow.Key{
		{Node: "hq/brno/printer-1", Interface: "FDMPrinter1", Instance: "1"},
		{Node: "hq/brno/printer-1", Interface: "FDMPrinter1", Instance: "2"},
		{Node: "hq/brno/printer-2", Interface: "Camera1", Instance: "1"},
	}
	store := newStore(t, shadow.Config{Selection: shadow.Selection{Interfaces: keys}})
	if got := store.Len(); got != len(keys) {
		t.Fatalf("the store holds %d interfaces, want %d", got, len(keys))
	}
	for _, key := range keys {
		status, updatedAt, ok := store.Status(key)
		if !ok {
			t.Fatalf("%s was not created", key)
		}
		if status != shadow.StatusEmpty || updatedAt != 0 {
			t.Errorf("%s is %s at %d, want EMPTY with no timestamp", key, status, updatedAt)
		}
	}
}

// TestNewSelectsNothing checks that New accepts an empty Selection, and that
// such a Store keeps nothing.
func TestNewSelectsNothing(t *testing.T) {
	t.Parallel()
	store := newStore(t, shadow.Config{})
	if got := store.Len(); got != 0 {
		t.Fatalf("the store holds %d interfaces, want none", got)
	}
	key := shadow.Key{Node: "hq/a", Interface: "Camera1", Instance: "1"}
	if res := store.ApplyREP(rep(key, 10, nil)); res.Outcome != shadow.OutcomeNotSelected {
		t.Fatalf("outcome = %s, want not selected", res.Outcome)
	}
	if got := store.Len(); got != 0 {
		t.Fatalf("the store holds %d interfaces, want none", got)
	}
}

// TestNewRefusesABadSelection checks that New accepts a Selection whose keys
// and patterns are valid, and returns ErrBadSelection when one is not valid.
func TestNewRefusesABadSelection(t *testing.T) {
	t.Parallel()
	good := shadow.Key{Node: "hq/a", Interface: "Camera1", Instance: "1"}
	tests := []struct {
		name string
		sel  shadow.Selection
		ok   bool
	}{
		{name: "a valid name and a valid pattern", ok: true, sel: shadow.Selection{
			Interfaces: []shadow.Key{good},
			Patterns:   []fractal.Pattern{"FDM*"},
		}},
		{name: "a node path with an empty segment", sel: shadow.Selection{
			Interfaces: []shadow.Key{{Node: "hq//a", Interface: "Camera1", Instance: "1"}},
		}},
		{name: "an interface name starting with a digit", sel: shadow.Selection{
			Interfaces: []shadow.Key{{Node: "hq/a", Interface: "1Camera", Instance: "1"}},
		}},
		{name: "no instance id, which selects every instance", ok: true, sel: shadow.Selection{
			Interfaces: []shadow.Key{{Node: "hq/a", Interface: "Camera1"}},
		}},
		{name: "no instance id and an interface name starting with a digit", sel: shadow.Selection{
			Interfaces: []shadow.Key{{Node: "hq/a", Interface: "1Camera"}},
		}},
		{name: "an instance id starting with a hyphen", sel: shadow.Selection{
			Interfaces: []shadow.Key{{Node: "hq/a", Interface: "Camera1", Instance: "-1"}},
		}},
		{name: "a pattern with a slash", sel: shadow.Selection{
			Patterns: []fractal.Pattern{"hq/*"},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := shadow.New(shadow.Config{Selection: tc.sel})
			if tc.ok {
				if err != nil {
					t.Fatalf("New = %v, want a store", err)
				}
				return
			}
			if !errors.Is(err, shadow.ErrBadSelection) {
				t.Fatalf("New = %v, want ErrBadSelection", err)
			}
		})
	}
}

// TestSelectionIsCopied checks that the Store has its own copy of the
// Selection. A change to the slices passed to New, or to the slices Selection
// returns, does not change which interfaces the Store selects.
func TestSelectionIsCopied(t *testing.T) {
	t.Parallel()
	key := shadow.Key{Node: "hq/a", Interface: "Camera1", Instance: "1"}
	sel := shadow.Selection{Interfaces: []shadow.Key{key}, Patterns: []fractal.Pattern{"FDM*"}}
	store := newStore(t, shadow.Config{Selection: sel})
	sel.Patterns[0] = "*"
	if store.Selects(shadow.Key{Node: "hq/a", Interface: "Camera1", Instance: "2"}) {
		t.Fatal("a change to the slice passed to New changed the store")
	}
	got := store.Selection()
	got.Patterns[0] = "*"
	if store.Selects(shadow.Key{Node: "hq/a", Interface: "Camera1", Instance: "3"}) {
		t.Fatal("a change to the slice Selection returned changed the store")
	}
}

// TestStoreSelects checks Selects for an interface in Selection.Interfaces,
// for an interface a pattern matches, and for interfaces the Selection does
// not cover.
func TestStoreSelects(t *testing.T) {
	t.Parallel()
	named := shadow.Key{Node: "hq/a", Interface: "Camera1", Instance: "1"}
	store := newStore(t, shadow.Config{Selection: shadow.Selection{
		Interfaces: []shadow.Key{named},
		Patterns:   []fractal.Pattern{"FDM*"},
	}})
	tests := []struct {
		name string
		key  shadow.Key
		want bool
	}{
		{name: "an enumerated interface", key: named, want: true},
		{name: "another instance of an enumerated interface", key: shadow.Key{
			Node: "hq/a", Interface: "Camera1", Instance: "2",
		}},
		{name: "a pattern matches on a node several levels below", key: shadow.Key{
			Node: "hq/b/c/d", Interface: "FDMPrinter1", Instance: "9",
		}, want: true},
		{name: "a name no pattern matches", key: shadow.Key{
			Node: "hq/b", Interface: "Printer1", Instance: "1",
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := store.Selects(tc.key); got != tc.want {
				t.Fatalf("Selects(%s) = %v, want %v", tc.key, got, tc.want)
			}
		})
	}
}

// TestGetCopiesTheProps checks that Snapshot.Props is a copy: a caller that
// changes the map does not change the Store.
func TestGetCopiesTheProps(t *testing.T) {
	t.Parallel()
	key := shadow.Key{Node: "hq/a", Interface: "Camera1", Instance: "1"}
	store := newStore(t, shadow.Config{Selection: shadow.Selection{Interfaces: []shadow.Key{key}}})
	store.ApplyREP(rep(key, 10, fractal.Props{"n": json.RawMessage("1")}))
	snap, ok := store.Get(key)
	if !ok {
		t.Fatal("the interface is gone")
	}
	snap.Props["n"] = json.RawMessage("999")
	delete(snap.Props, "n")
	snap.Props["other"] = json.RawMessage("1")
	value, ok := store.Prop(key, "n")
	if !ok || string(value) != "1" {
		t.Fatalf("n = %q %v, want \"1\", the value the REP reported", value, ok)
	}
	if _, added := store.Prop(key, "other"); added {
		t.Fatal("the store has a property the caller added to the copy")
	}
}

// TestReadsOfAnAbsentInterface checks that Status, Get and Prop return ok
// false for an interface the Store does not have.
func TestReadsOfAnAbsentInterface(t *testing.T) {
	t.Parallel()
	present := shadow.Key{Node: "hq/a", Interface: "Camera1", Instance: "1"}
	store := newStore(t, shadow.Config{Selection: shadow.Selection{Interfaces: []shadow.Key{present}}})
	tests := []struct {
		name string
		key  shadow.Key
	}{
		{name: "an unknown node", key: shadow.Key{Node: "hq/z", Interface: "Camera1", Instance: "1"}},
		{name: "an unknown interface of a known node", key: shadow.Key{
			Node: "hq/a", Interface: "FDMPrinter1", Instance: "1",
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, _, ok := store.Status(tc.key); ok {
				t.Error("Status found it")
			}
			if _, ok := store.Get(tc.key); ok {
				t.Error("Get found it")
			}
			if _, ok := store.Prop(tc.key, "n"); ok {
				t.Error("Prop found it")
			}
		})
	}
	if _, ok := store.Prop(present, "missing"); ok {
		t.Error("Prop found a property the interface does not have")
	}
}

// TestInterfacesAndSubtreeIterate checks the keys Interfaces, Subtree and
// Nodes return, and their order.
func TestInterfacesAndSubtreeIterate(t *testing.T) {
	t.Parallel()
	keys := []shadow.Key{
		{Node: "hq/a", Interface: "Camera1", Instance: "2"},
		{Node: "hq/a", Interface: "Camera1", Instance: "1"},
		{Node: "hq/a", Interface: "FDMPrinter1", Instance: "1"},
		{Node: "hq/a/b", Interface: "Camera1", Instance: "1"},
		{Node: "hq/ab", Interface: "Camera1", Instance: "1"},
	}
	store := newStore(t, shadow.Config{Selection: shadow.Selection{Interfaces: keys}})

	wantNode := []shadow.Key{
		{Node: "hq/a", Interface: "Camera1", Instance: "1"},
		{Node: "hq/a", Interface: "Camera1", Instance: "2"},
		{Node: "hq/a", Interface: "FDMPrinter1", Instance: "1"},
	}
	if got := slices.Collect(store.Interfaces("hq/a")); !slices.Equal(got, wantNode) {
		t.Errorf("Interfaces(hq/a) = %v, want %v", got, wantNode)
	}
	if got := slices.Collect(store.Interfaces("hq/zz")); len(got) != 0 {
		t.Errorf("Interfaces of an unknown node = %v, want nothing", got)
	}

	wantSubtree := append(slices.Clone(wantNode), shadow.Key{
		Node: "hq/a/b", Interface: "Camera1", Instance: "1",
	})
	if got := slices.Collect(store.Subtree("hq/a")); !slices.Equal(got, wantSubtree) {
		t.Errorf("Subtree(hq/a) = %v, want %v", got, wantSubtree)
	}

	wantNodes := []fractal.NodePath{"hq/a", "hq/a/b", "hq/ab"}
	if got := slices.Collect(store.Nodes()); !slices.Equal(got, wantNodes) {
		t.Errorf("Nodes() = %v, want %v", got, wantNodes)
	}
}

// TestRemoveNodeDeletesTheSubtree checks that RemoveNode deletes the
// interfaces of the node at the path and of every node below it. It keeps the
// node "hq/ab", whose path starts with the same characters as "hq/a" and
// which is not below "hq/a".
func TestRemoveNodeDeletesTheSubtree(t *testing.T) {
	t.Parallel()
	keys := []shadow.Key{
		{Node: "hq/a", Interface: "Camera1", Instance: "1"},
		{Node: "hq/a/b", Interface: "Camera1", Instance: "1"},
		{Node: "hq/ab", Interface: "Camera1", Instance: "1"},
	}
	store := newStore(t, shadow.Config{Selection: shadow.Selection{Interfaces: keys}})
	removed := store.RemoveNode("hq/a")
	want := []shadow.Key{keys[0], keys[1]}
	if !slices.Equal(removed, want) {
		t.Fatalf("RemoveNode = %v, want %v", removed, want)
	}
	if got := store.Len(); got != 1 {
		t.Fatalf("the store has %d interfaces, want only the one of hq/ab", got)
	}
	if _, _, ok := store.Status(keys[2]); !ok {
		t.Error("RemoveNode(hq/a) removed the node hq/ab too")
	}
	if got := slices.Collect(store.Nodes()); !slices.Equal(got, []fractal.NodePath{"hq/ab"}) {
		t.Errorf("Nodes() = %v, want only hq/ab", got)
	}
	if got := store.RemoveNode("hq/a"); got != nil {
		t.Errorf("a second RemoveNode(hq/a) returned %v, want nothing", got)
	}
}

// TestRemovedInterfaceIsCreatedAgainAsEmpty checks a REP that arrives after
// RemoveNode for an interface the Selection still covers. The Store creates
// the interface again as EMPTY, so it applies the REP even with a timestamp
// lower than the one the deleted interface had.
func TestRemovedInterfaceIsCreatedAgainAsEmpty(t *testing.T) {
	t.Parallel()
	key := shadow.Key{Node: "hq/a", Interface: "Camera1", Instance: "1"}
	store := newStore(t, shadow.Config{Selection: shadow.Selection{Interfaces: []shadow.Key{key}}})
	store.ApplyREP(rep(key, 100, fractal.Props{"n": json.RawMessage("1")}))
	store.RemoveNode("hq/a")
	res := store.ApplyREP(rep(key, 5, fractal.Props{"n": json.RawMessage("2")}))
	if res.Outcome != shadow.OutcomeApplied || res.Previous != shadow.StatusEmpty || !res.Created {
		t.Fatalf("outcome %s from %s, created %v, want applied from EMPTY with created true",
			res.Outcome, res.Previous, res.Created)
	}
	if _, updatedAt, _ := store.Status(key); updatedAt != 5 {
		t.Fatalf("updatedAt = %d, want 5, the timestamp of the REP", updatedAt)
	}
}

// TestKeyString checks the text Key.String returns. LimitError.Error uses it.
func TestKeyString(t *testing.T) {
	t.Parallel()
	key := shadow.Key{Node: "hq/brno/printer-1", Interface: "FDMPrinter1", Instance: "1"}
	if got := key.String(); got != "hq/brno/printer-1 FDMPrinter1/1" {
		t.Fatalf("String() = %q", got)
	}
}

// contents returns the status of every interface the Store holds.
func contents(store *shadow.Store) map[shadow.Key]shadow.Status {
	out := map[shadow.Key]shadow.Status{}
	for path := range store.Nodes() {
		for key := range store.Interfaces(path) {
			status, _, _ := store.Status(key)
			out[key] = status
		}
	}
	return out
}

// TestAnEntryWithNoInstanceSelectsEveryInstance checks the rule of fractal.md,
// Shadow Selection: an entry that names an interface with no instance id
// selects every instance of it on that node, as a pattern does. New creates
// nothing for it, and the first report for an instance creates that instance
// as EMPTY. The cases share one Store, so they run in order.
func TestAnEntryWithNoInstanceSelectsEveryInstance(t *testing.T) {
	entry := shadow.Key{Node: "hq", Interface: "Site1"}
	store := newStore(t, shadow.Config{Selection: shadow.Selection{Interfaces: []shadow.Key{entry}}})
	if got := store.Len(); got != 0 {
		t.Fatalf("New created %d interfaces, want none for an entry with no instance id", got)
	}
	first := shadow.Key{Node: "hq", Interface: "Site1", Instance: "1"}
	second := shadow.Key{Node: "hq", Interface: "Site1", Instance: "2"}
	otherInterface := shadow.Key{Node: "hq", Interface: "Camera1", Instance: "1"}
	otherNode := shadow.Key{Node: "hq/a", Interface: "Site1", Instance: "1"}
	tests := []struct {
		name string
		key  shadow.Key
		want shadow.Result
	}{
		{name: "the first instance", key: first, want: shadow.Result{
			Key: first, Outcome: shadow.OutcomeApplied, Previous: shadow.StatusEmpty,
			Status: shadow.StatusOK, UpdatedAt: 10, Created: true,
		}},
		{name: "a second instance", key: second, want: shadow.Result{
			Key: second, Outcome: shadow.OutcomeApplied, Previous: shadow.StatusEmpty,
			Status: shadow.StatusOK, UpdatedAt: 10, Created: true,
		}},
		{name: "another interface of that node", key: otherInterface, want: shadow.Result{
			Key: otherInterface, Outcome: shadow.OutcomeNotSelected,
		}},
		{name: "that interface on another node", key: otherNode, want: shadow.Result{
			Key: otherNode, Outcome: shadow.OutcomeNotSelected,
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := store.ApplyREP(rep(tc.key, 10, nil)); got != tc.want {
				t.Fatalf("ApplyREP = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestRebaseKeepsOnlyTheAncestors checks Rebase, which a node calls on its
// ancestor shadow each time it takes a Node Path (fractal.md, Synchronization
// of the Ancestor Shadow - Happy path, and After the Join Exchange). It deletes
// every interface and publishes its removal, creates again as EMPTY only the
// named interfaces of the nodes above the new path, and then refuses to create
// any node that is not above that path.
func TestRebaseKeepsOnlyTheAncestors(t *testing.T) {
	t.Parallel()
	hq := shadow.Key{Node: "hq", Interface: "Site1", Instance: "1"}
	brno := shadow.Key{Node: "hq/brno", Interface: "Site1", Instance: "1"}
	praha := shadow.Key{Node: "hq/praha", Interface: "Site1", Instance: "1"}
	store := newStore(t, shadow.Config{Selection: shadow.Selection{
		Interfaces: []shadow.Key{hq, brno, praha},
		Patterns:   []fractal.Pattern{"Content*"},
	}})
	for _, key := range []shadow.Key{hq, brno, praha} {
		store.ApplyREP(rep(key, 100, fractal.Props{"n": json.RawMessage("1")}))
	}
	sub := store.Subscribe(16)
	defer sub.Close()

	removed := store.Rebase("hq/praha/printer-1")

	if want := []shadow.Key{hq, brno, praha}; !slices.Equal(removed, want) {
		t.Fatalf("Rebase removed %v, want %v", removed, want)
	}
	wantEvents := []shadow.Event{
		{Key: hq, Status: shadow.StatusOK, UpdatedAt: 100, Removed: true},
		{Key: brno, Status: shadow.StatusOK, UpdatedAt: 100, Removed: true},
		{Key: praha, Status: shadow.StatusOK, UpdatedAt: 100, Removed: true},
	}
	if got := drain(sub); !slices.Equal(got, wantEvents) {
		t.Fatalf("Rebase published %v, want %v", got, wantEvents)
	}
	want := map[shadow.Key]shadow.Status{hq: shadow.StatusEmpty, praha: shadow.StatusEmpty}
	if got := contents(store); !maps.Equal(got, want) {
		t.Fatalf("after Rebase the store holds %v, want %v", got, want)
	}

	later := []struct {
		key  shadow.Key
		want shadow.Outcome
	}{
		{key: brno, want: shadow.OutcomeNotSelected},
		{key: shadow.Key{Node: "hq/brno", Interface: "ContentStore1", Instance: "1"}, want: shadow.OutcomeNotSelected},
		{key: shadow.Key{Node: "hq/praha", Interface: "ContentStore1", Instance: "1"}, want: shadow.OutcomeApplied},
		{
			key:  shadow.Key{Node: "hq/praha/printer-1", Interface: "ContentStore1", Instance: "1"},
			want: shadow.OutcomeNotSelected,
		},
	}
	for _, l := range later {
		if got := store.ApplyREP(rep(l.key, 200, nil)).Outcome; got != l.want {
			t.Errorf("ApplyREP for %s after Rebase = %s, want %s", l.key, got, l.want)
		}
	}
}

// TestRebaseWithNoPathDropsEverything checks Rebase with an empty path: the
// Store deletes every interface and creates none, then or on a later report. A
// node whose admission was revoked can discard its ancestor shadow in full
// this way (fractal.md, Admission and Revocation).
func TestRebaseWithNoPathDropsEverything(t *testing.T) {
	t.Parallel()
	key := shadow.Key{Node: "hq", Interface: "Site1", Instance: "1"}
	store := newStore(t, shadow.Config{Selection: shadow.Selection{
		Interfaces: []shadow.Key{key},
		Patterns:   []fractal.Pattern{"*"},
	}})
	store.ApplyREP(rep(key, 100, nil))

	if removed := store.Rebase(""); !slices.Equal(removed, []shadow.Key{key}) {
		t.Fatalf("Rebase removed %v, want %v", removed, []shadow.Key{key})
	}
	if res := store.ApplyREP(rep(key, 200, nil)); res.Outcome != shadow.OutcomeNotSelected {
		t.Fatalf("ApplyREP after Rebase with no path = %s, want not selected", res.Outcome)
	}
	if got := store.Len(); got != 0 {
		t.Fatalf("the store holds %d interfaces, want none", got)
	}
}
