package fractal_test

import (
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// FuzzParseNodePath tests every path ParseNodePath accepts. The path must
// follow the rules of the Node Path section of fractal.md. Depth, Last,
// Segments, Parent, Child, IsAncestorOf, Covers and NextHop must give results
// that agree with each other.
func FuzzParseNodePath(f *testing.F) {
	seeds := []string{
		"", "/", "//", "hq", "hq/brno", "hq/brno/printer-1", "hq//brno", "hq/",
		"/hq", "-hq", "hq/-brno", "a", "a/b/c/d/e", strings.Repeat("n", 64),
		strings.Repeat("n", 65), pathOf255(), pathOf255() + "c", "hq/ab", "hq/a",
		"\x00", "hq/br.no", "hq/brnō", "HQ/Brno/Printer_1",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		path, err := fractal.ParseNodePath(s)
		if err != nil {
			return
		}
		if len(path) == 0 || len(path) > fractal.MaxNodePathLength {
			t.Fatalf("accepted a path of %d characters", len(path))
		}
		if path.Depth() != strings.Count(s, "/")+1 {
			t.Fatalf("Depth of %q is %d, want the number of slashes plus one", s, path.Depth())
		}
		if bad := path.Last().Validate(); bad != nil {
			t.Fatalf("the last segment of %q is not a Node ID: %v", s, bad)
		}
		var rebuilt []string
		for id := range path.Segments() {
			if bad := id.Validate(); bad != nil {
				t.Fatalf("segment %q of %q is not a Node ID: %v", id, s, bad)
			}
			rebuilt = append(rebuilt, string(id))
		}
		if strings.Join(rebuilt, "/") != s {
			t.Fatalf("the segments of %q joined by a slash give %q", s, strings.Join(rebuilt, "/"))
		}
		parent, ok := path.Parent()
		if ok != !path.IsRoot() {
			t.Fatalf("for %q Parent returned ok %v and IsRoot returned %v", s, ok, path.IsRoot())
		}
		if !ok {
			if path.Depth() != 1 {
				t.Fatalf("a root path has one segment, %q has %d", s, path.Depth())
			}
			return
		}
		child, rebuildErr := parent.Child(path.Last())
		if rebuildErr != nil || child != path {
			t.Fatalf("%q.Child(%q) = %q, %v, want %q", parent, path.Last(), child, rebuildErr, s)
		}
		if !parent.IsAncestorOf(path) || !parent.Covers(path) {
			t.Fatalf("IsAncestorOf or Covers is false for %q and the path below it %q", parent, s)
		}
		hop, routable := parent.NextHop(path)
		if !routable || hop != path.Last() {
			t.Fatalf("NextHop from %q toward %q gave %q %v", parent, s, hop, routable)
		}
		if path.IsAncestorOf(path) {
			t.Fatalf("IsAncestorOf is true for %q and itself", s)
		}
	})
}

// referenceMatch implements the wildcard rule by plain recursion. It is slow
// and easy to check by reading. FuzzPattern compares Match with it.
func referenceMatch(pattern, subject string) bool {
	if pattern == "" {
		return subject == ""
	}
	if pattern[0] == '*' {
		if referenceMatch(pattern[1:], subject) {
			return true
		}
		return subject != "" && referenceMatch(pattern, subject[1:])
	}
	return subject != "" && pattern[0] == subject[0] && referenceMatch(pattern[1:], subject[1:])
}

// FuzzPattern tests that a pattern ParsePattern accepts has only the characters
// the Shadow Selection section of fractal.md allows, and that Match returns
// the same result as referenceMatch.
func FuzzPattern(f *testing.F) {
	seeds := []struct{ pattern, subject string }{
		{"*", "FDMPrinter1"},
		{"FDM*", "FDMPrinter1"},
		{"*1", "FDMPrinter1"},
		{"FDM*1", "FDMPrinter1"},
		{"a*a*a*a*b", "aaaaaaaaaa"},
		{"***", "abc"},
		{"", ""},
		{"FDM?", "FDM"},
		{"fdm*", "FDMPrinter1"},
		{"_a*", "_ab"},
		{"1*", "1a"},
		{"a", ""},
	}
	for _, s := range seeds {
		f.Add(s.pattern, s.subject)
	}
	f.Fuzz(func(t *testing.T, pattern, subject string) {
		p, err := fractal.ParsePattern(pattern)
		if err != nil {
			return
		}
		for i := range len(pattern) {
			c := pattern[i]
			ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
				c == '_' || c == '*'
			if !ok {
				t.Fatalf("accepted %q, which has the character %q", pattern, c)
			}
		}
		got := p.Match(fractal.InterfaceName(subject))
		if !strings.Contains(pattern, "*") && got != (pattern == subject) {
			t.Fatalf("Match(%q, %q) = %v, and a pattern with no wildcard matches only an equal name",
				pattern, subject, got)
		}
		if len(pattern) <= 16 && len(subject) <= 16 {
			if want := referenceMatch(pattern, subject); got != want {
				t.Fatalf("Match(%q, %q) = %v, referenceMatch gives %v", pattern, subject, got, want)
			}
		}
	})
}
