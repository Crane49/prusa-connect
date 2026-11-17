package fractal_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// pathOf255 builds a valid path of exactly MaxNodePathLength characters.
func pathOf255() string {
	p := "hq/" + strings.Repeat("a", 60)
	for len(p) < 255 {
		p += "/" + strings.Repeat("b", 60)
	}
	return p[:255]
}

// TestNodePathGrammar tests the Node Path rule of fractal.md: valid Node IDs
// separated by a slash, and at most 255 characters.
func TestNodePathGrammar(t *testing.T) {
	t.Parallel()
	cases := []nameCase{
		{name: "root", in: "hq", ok: true},
		{name: "three segments", in: "hq/brno/printer-1", ok: true},
		{name: "two hundred and fifty five characters", in: pathOf255(), ok: true},
		{name: "two hundred and fifty six characters", in: pathOf255() + "c", is: fractal.ErrTooLong},
		{name: "empty", in: "", is: fractal.ErrEmpty},
		{name: "leading slash", in: "/hq", is: fractal.ErrEmptySegment},
		{name: "trailing slash", in: "hq/", is: fractal.ErrEmptySegment},
		{name: "empty segment", in: "hq//brno", is: fractal.ErrEmptySegment},
		{name: "only a slash", in: "/", is: fractal.ErrEmptySegment},
		{name: "segment with a leading hyphen", in: "hq/-brno", is: fractal.ErrBadFirstCharacter},
		{name: "segment of sixty four characters", in: "hq/" + strings.Repeat("n", 64), ok: true},
		{name: "segment over sixty four characters", in: "hq/" + strings.Repeat("n", 65), is: fractal.ErrTooLong},
		{name: "dot in a segment", in: "hq/br.no", is: fractal.ErrBadCharacter},
		{name: "non ascii", in: "hq/brnō", is: fractal.ErrBadCharacter},
	}
	checkNameCases(t, cases, func(s string) error { return fractal.NodePath(s).Validate() })
}

// TestNodePathParts tests IsRoot, Parent, Last, Depth and Segments on paths of
// one, two and three segments.
func TestNodePathParts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		path     fractal.NodePath
		isRoot   bool
		parent   fractal.NodePath
		hasPar   bool
		last     fractal.NodeID
		depth    int
		segments []fractal.NodeID
	}{
		{
			name: "root", path: "hq", isRoot: true, parent: "", hasPar: false,
			last: "hq", depth: 1, segments: []fractal.NodeID{"hq"},
		},
		{
			name: "two segments", path: "hq/brno", parent: "hq", hasPar: true,
			last: "brno", depth: 2, segments: []fractal.NodeID{"hq", "brno"},
		},
		{
			name: "three segments", path: "hq/brno/printer-1", parent: "hq/brno", hasPar: true,
			last: "printer-1", depth: 3,
			segments: []fractal.NodeID{"hq", "brno", "printer-1"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.path.IsRoot(); got != tc.isRoot {
				t.Errorf("IsRoot = %v, want %v", got, tc.isRoot)
			}
			parent, ok := tc.path.Parent()
			if ok != tc.hasPar || parent != tc.parent {
				t.Errorf("Parent = %q %v, want %q %v", parent, ok, tc.parent, tc.hasPar)
			}
			if got := tc.path.Last(); got != tc.last {
				t.Errorf("Last = %q, want %q", got, tc.last)
			}
			if got := tc.path.Depth(); got != tc.depth {
				t.Errorf("Depth = %d, want %d", got, tc.depth)
			}
			got := slices.Collect(tc.path.Segments())
			if !slices.Equal(got, tc.segments) {
				t.Errorf("Segments = %v, want %v", got, tc.segments)
			}
		})
	}
}

// TestNodePathChild tests the rule of fractal.md for the path of a node that
// joins: the path of the node it joins, a slash and the requested Node ID. The
// result must not be over MaxNodePathLength.
func TestNodePathChild(t *testing.T) {
	t.Parallel()
	got, err := fractal.NodePath("hq/brno").Child("printer-1")
	if err != nil || got != "hq/brno/printer-1" {
		t.Fatalf("Child = %q %v", got, err)
	}
	if _, badID := fractal.NodePath("hq").Child("-bad"); badID == nil {
		t.Error("Child accepted an invalid Node ID")
	}
	if _, badParent := fractal.NodePath("hq/").Child("ok"); badParent == nil {
		t.Error("Child accepted the invalid receiver path \"hq/\"")
	}
	long := fractal.NodePath(pathOf255()[:250])
	if _, tooLong := long.Child("abcdefghij"); tooLong == nil {
		t.Error("Child returned a path over MaxNodePathLength")
	}
}

// TestNodePathIsAncestorOf tests the routing rule of fractal.md: the receiver is
// below a node when the path of the node followed by a slash is a prefix of the
// receiver.
func TestNodePathIsAncestorOf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		p     fractal.NodePath
		other fractal.NodePath
		want  bool
	}{
		{name: "one level below", p: "hq/a", other: "hq/a/b", want: true},
		{name: "two levels below", p: "hq", other: "hq/a/b", want: true},
		{name: "same path", p: "hq/a", other: "hq/a", want: false},
		{name: "shared prefix without a slash", p: "hq/a", other: "hq/ab", want: false},
		{name: "another node at the same level", p: "hq/a", other: "hq/b", want: false},
		{name: "ancestor the other way", p: "hq/a/b", other: "hq/a", want: false},
		{name: "slash but no segment", p: "hq/a", other: "hq/a/", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.IsAncestorOf(tc.other); got != tc.want {
				t.Fatalf("%q.IsAncestorOf(%q) = %v, want %v", tc.p, tc.other, got, tc.want)
			}
		})
	}
}

// TestNodePathCovers tests Sender Validation of fractal.md. In a message that
// arrived from a node directly below, the sender is the path of that node, or
// that path followed by more segments. The field child in the table is the path
// of that node.
func TestNodePathCovers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		child  fractal.NodePath
		sender fractal.NodePath
		want   bool
	}{
		{name: "the same node", child: "hq/brno", sender: "hq/brno", want: true},
		{name: "one level below that node", child: "hq/brno", sender: "hq/brno/printer-1", want: true},
		{name: "two levels below that node", child: "hq/b", sender: "hq/b/c/d", want: true},
		{name: "below another node", child: "hq/brno", sender: "hq/praha/printer-1", want: false},
		{name: "the node above", child: "hq/brno", sender: "hq", want: false},
		{name: "a name that starts the same", child: "hq/a", sender: "hq/ab", want: false},
		{name: "empty sender", child: "hq/brno", sender: "", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.child.Covers(tc.sender); got != tc.want {
				t.Fatalf("%q.Covers(%q) = %v, want %v", tc.child, tc.sender, got, tc.want)
			}
		})
	}
}

// TestNodePathNextHop tests the downward routing rule of fractal.md: a node
// forwards the message to the node directly below it whose Node ID is the next
// segment of the receiver.
func TestNodePathNextHop(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		p        fractal.NodePath
		receiver fractal.NodePath
		want     fractal.NodeID
		ok       bool
	}{
		{name: "receiver one level below", p: "hq/brno", receiver: "hq/brno/printer-1", want: "printer-1", ok: true},
		{name: "receiver two levels below", p: "hq", receiver: "hq/brno/printer-1", want: "brno", ok: true},
		{name: "the node itself", p: "hq/brno", receiver: "hq/brno"},
		{name: "below another node", p: "hq/brno", receiver: "hq/praha/printer-1"},
		{name: "a name that starts the same", p: "hq/a", receiver: "hq/ab"},
		{name: "upward", p: "hq/brno", receiver: "hq"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := tc.p.NextHop(tc.receiver)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("%q.NextHop(%q) = %q %v, want %q %v", tc.p, tc.receiver, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestParseNodePathReturnsThePath tests that ParseNodePath returns a valid
// string unchanged, and returns an error for a path with an empty segment.
func TestParseNodePathReturnsThePath(t *testing.T) {
	t.Parallel()
	got, err := fractal.ParseNodePath("hq/brno")
	if err != nil || got.String() != "hq/brno" {
		t.Fatalf("ParseNodePath = %q %v", got, err)
	}
	if _, bad := fractal.ParseNodePath("hq//brno"); bad == nil {
		t.Fatal("ParseNodePath accepted an empty segment")
	}
}

// TestNodePathSegmentsStopsEarly tests that the iterator of Segments stops when
// the caller breaks out of the range loop.
func TestNodePathSegmentsStopsEarly(t *testing.T) {
	t.Parallel()
	var seen []fractal.NodeID
	for id := range fractal.NodePath("hq/brno/printer-1").Segments() {
		seen = append(seen, id)
		if len(seen) == 2 {
			break
		}
	}
	if !slices.Equal(seen, []fractal.NodeID{"hq", "brno"}) {
		t.Fatalf("seen = %v", seen)
	}
}
