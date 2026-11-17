package shadow_test

import (
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// staleFixture builds a Store with one OK interface on each node of a small
// tree, and returns the sorted keys. The node "hq/ab" is not below "hq/a".
func staleFixture(t *testing.T) (*shadow.Store, []shadow.Key) {
	t.Helper()
	keys := []shadow.Key{
		{Node: "hq", Interface: "Camera1", Instance: "1"},
		{Node: "hq/a", Interface: "Camera1", Instance: "1"},
		{Node: "hq/a/b", Interface: "Camera1", Instance: "1"},
		{Node: "hq/a/b/c", Interface: "Camera1", Instance: "1"},
		{Node: "hq/ab", Interface: "Camera1", Instance: "1"},
	}
	store := newStore(t, shadow.Config{Selection: shadow.Selection{Interfaces: keys}})
	for _, key := range keys {
		store.ApplyREP(rep(key, 100, fractal.Props{"n": json.RawMessage("1")}))
	}
	return store, keys
}

// TestMarkStaleCoversTheSubtree checks the rule of fractal.md, Connection Loss
// Message: the lost node becomes STALE, and so does every node whose path
// starts with the path of the lost node and a slash. A node whose path only
// starts with the same characters, such as "hq/ab" for "hq/a", stays OK.
func TestMarkStaleCoversTheSubtree(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		lost fractal.NodePath
		want []fractal.NodePath
	}{
		{name: "a node with no nodes below it", lost: "hq/a/b/c", want: []fractal.NodePath{"hq/a/b/c"}},
		{name: "a node and the nodes below it", lost: "hq/a", want: []fractal.NodePath{"hq/a", "hq/a/b", "hq/a/b/c"}},
		{name: "the root", lost: "hq", want: []fractal.NodePath{
			"hq", "hq/a", "hq/a/b", "hq/a/b/c", "hq/ab",
		}},
		{
			name: "a node whose path starts like the path of another node",
			lost: "hq/ab", want: []fractal.NodePath{"hq/ab"},
		},
		{name: "a node the store does not have", lost: "hq/zz"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store, keys := staleFixture(t)
			changed := store.MarkStale(tc.lost)
			var got []fractal.NodePath
			for _, key := range changed {
				got = append(got, key.Node)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("MarkStale(%s) marked %v, want %v", tc.lost, got, tc.want)
			}
			for _, key := range keys {
				status, updatedAt, _ := store.Status(key)
				want := shadow.StatusOK
				if slices.Contains(tc.want, key.Node) {
					want = shadow.StatusStale
				}
				if status != want {
					t.Errorf("%s is %s, want %s", key, status, want)
				}
				if updatedAt != 100 {
					t.Errorf("%s: updatedAt = %d, want 100, the timestamp of the last REP", key, updatedAt)
				}
			}
		})
	}
}

// TestMarkStaleLeavesEmptyInterfacesEmpty checks that MarkStale leaves an
// EMPTY interface EMPTY.
func TestMarkStaleLeavesEmptyInterfacesEmpty(t *testing.T) {
	t.Parallel()
	reported := shadow.Key{Node: "hq/a", Interface: "Camera1", Instance: "1"}
	silent := shadow.Key{Node: "hq/a", Interface: "FDMPrinter1", Instance: "1"}
	store := newStore(t, shadow.Config{
		Selection: shadow.Selection{Interfaces: []shadow.Key{reported, silent}},
	})
	store.ApplyREP(rep(reported, 100, nil))
	changed := store.MarkStale("hq/a")
	if !slices.Equal(changed, []shadow.Key{reported}) {
		t.Fatalf("MarkStale marked %v, want only the OK interface", changed)
	}
	status, updatedAt, _ := store.Status(silent)
	if status != shadow.StatusEmpty || updatedAt != 0 {
		t.Fatalf("the empty interface is %s at %d, want EMPTY with no timestamp", status, updatedAt)
	}
}

// TestMarkStaleTwiceChangesNothingTheSecondTime checks that a second MarkStale
// for the same path returns no keys, so a node does not report the same stale
// interfaces to the application twice.
func TestMarkStaleTwiceChangesNothingTheSecondTime(t *testing.T) {
	t.Parallel()
	store, _ := staleFixture(t)
	if first := store.MarkStale("hq/a"); len(first) != 3 {
		t.Fatalf("the first MarkStale marked %d interfaces, want 3", len(first))
	}
	if second := store.MarkStale("hq/a"); second != nil {
		t.Fatalf("the second MarkStale marked %v, want nothing", second)
	}
}

// TestApplyLOS checks that ApplyLOS marks the node at the path of the sender,
// a slash and LOS.Child, and that it returns an error when those do not make a
// valid Node Path.
func TestApplyLOS(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		sender fractal.NodePath
		child  fractal.NodeID
		want   []fractal.NodePath
		ok     bool
	}{
		{name: "a node directly below the sender", sender: "hq", child: "a", ok: true, want: []fractal.NodePath{
			"hq/a", "hq/a/b", "hq/a/b/c",
		}},
		{
			name:   "a node id that starts like another node id",
			sender: "hq", child: "ab", ok: true, want: []fractal.NodePath{"hq/ab"},
		},
		{name: "a node id with a slash in it", sender: "hq", child: "a/b"},
		{name: "a node id that starts with a hyphen", sender: "hq", child: "-a"},
		{name: "an empty sender", sender: "", child: "a"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store, _ := staleFixture(t)
			los := fractal.LOS{Version: fractal.Version, Sender: tc.sender, Child: tc.child}
			changed, err := store.ApplyLOS(los)
			if !tc.ok {
				if err == nil {
					t.Fatalf("ApplyLOS accepted %s and %s", tc.sender, tc.child)
				}
				return
			}
			if err != nil {
				t.Fatalf("ApplyLOS = %v", err)
			}
			var got []fractal.NodePath
			for _, key := range changed {
				got = append(got, key.Node)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("marked %v, want %v", got, tc.want)
			}
		})
	}
}

// TestStaleInterfaceTakesTheNextReport checks what happens after a node
// connects again: a REP changes a STALE interface to OK.
func TestStaleInterfaceTakesTheNextReport(t *testing.T) {
	t.Parallel()
	store, keys := staleFixture(t)
	store.MarkStale("hq/a")
	key := keys[1]
	res := store.ApplyREP(rep(key, 200, fractal.Props{"n": json.RawMessage("2")}))
	if res.Previous != shadow.StatusStale || res.Status != shadow.StatusOK {
		t.Fatalf("transition %s to %s, want STALE to OK", res.Previous, res.Status)
	}
	if res.UpdatedAt != 200 {
		t.Fatalf("updatedAt = %d, want 200", res.UpdatedAt)
	}
}

// TestMarkStaleOnManyNodesTouchesOnlyTheSubtree checks MarkStale on a Store
// with several hundred nodes: it marks the interfaces below the lost node
// "hq/hall-2" and leaves the interfaces below "hq/hall-1" OK.
func TestMarkStaleOnManyNodesTouchesOnlyTheSubtree(t *testing.T) {
	t.Parallel()
	store := newStore(t, shadow.Config{Selection: shadow.Selection{Patterns: []fractal.Pattern{"*"}}})
	for i := range 400 {
		node := fractal.NodePath("hq/hall-" + strconv.Itoa(i%4) + "/printer-" + strconv.Itoa(i))
		key := shadow.Key{Node: node, Interface: "FDMPrinter1", Instance: "1"}
		store.ApplyREP(rep(key, 100, nil))
	}
	changed := store.MarkStale("hq/hall-2")
	if len(changed) != 100 {
		t.Fatalf("marked %d interfaces, want the 100 under hall-2", len(changed))
	}
	for _, key := range changed {
		if status, _, _ := store.Status(key); status != shadow.StatusStale {
			t.Fatalf("%s is %s, want STALE", key, status)
		}
	}
	for key := range store.Subtree("hq/hall-1") {
		if status, _, _ := store.Status(key); status != shadow.StatusOK {
			t.Fatalf("%s is %s, want OK", key, status)
		}
	}
}

// TestMarkStaleAboveSparesTheSender checks the rule of fractal.md, Ancestor
// Connection Loss Message: every node above the sender goes STALE, and the
// sender itself, the nodes below it and the nodes beside the path stay OK.
func TestMarkStaleAboveSparesTheSender(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		from  fractal.NodePath
		stale []fractal.NodePath
	}{
		{name: "a node several levels down", from: "hq/a/b/c", stale: []fractal.NodePath{"hq", "hq/a", "hq/a/b"}},
		{name: "a node directly below the root", from: "hq/a", stale: []fractal.NodePath{"hq"}},
		{name: "a path that starts like the path of another node", from: "hq/ab/x", stale: []fractal.NodePath{
			"hq", "hq/ab",
		}},
		{name: "the root", from: "hq"},
		{name: "a path outside the tree the store holds", from: "other/a"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store, keys := staleFixture(t)
			changed := store.MarkStaleAbove(tc.from)
			want := map[shadow.Key]shadow.Status{}
			var wantChanged []shadow.Key
			for _, key := range keys {
				want[key] = shadow.StatusOK
				if slices.Contains(tc.stale, key.Node) {
					want[key] = shadow.StatusStale
					wantChanged = append(wantChanged, key)
				}
			}
			if !slices.Equal(changed, wantChanged) {
				t.Fatalf("MarkStaleAbove(%s) changed %v, want %v", tc.from, changed, wantChanged)
			}
			if got := contents(store); !maps.Equal(got, want) {
				t.Fatalf("the store holds %v, want %v", got, want)
			}
		})
	}
}

// TestMarkStaleAboveLeavesEmptyInterfacesEmpty checks that an EMPTY interface
// above the sender stays EMPTY and is not reported as changed (fractal.md,
// Ancestor Connection Loss Message).
func TestMarkStaleAboveLeavesEmptyInterfacesEmpty(t *testing.T) {
	t.Parallel()
	empty := shadow.Key{Node: "hq", Interface: "Site1", Instance: "1"}
	held := shadow.Key{Node: "hq", Interface: "Camera1", Instance: "1"}
	store := newStore(t, shadow.Config{Selection: shadow.Selection{Interfaces: []shadow.Key{empty, held}}})
	store.ApplyREP(rep(held, 100, nil))

	if changed := store.MarkStaleAbove("hq/a"); !slices.Equal(changed, []shadow.Key{held}) {
		t.Fatalf("MarkStaleAbove changed %v, want only %s", changed, held)
	}
	want := map[shadow.Key]shadow.Status{empty: shadow.StatusEmpty, held: shadow.StatusStale}
	if got := contents(store); !maps.Equal(got, want) {
		t.Fatalf("the store holds %v, want %v", got, want)
	}
}
