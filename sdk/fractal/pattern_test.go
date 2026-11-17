package fractal_test

import (
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// TestPatternRejectsOtherCharacters tests the Shadow Selection rule of
// fractal.md: a pattern is made of Identifier characters and the wildcard, and
// a node rejects a pattern with any other character.
func TestPatternRejectsOtherCharacters(t *testing.T) {
	t.Parallel()
	cases := []nameCase{
		{name: "wildcard alone", in: "*", ok: true},
		{name: "prefix", in: "FDM*", ok: true},
		{name: "suffix", in: "*Printer1", ok: true},
		{name: "wildcard in the middle", in: "FDM*1", ok: true},
		{name: "several wildcards", in: "*a*b*", ok: true},
		{name: "no wildcard", in: "FDMPrinter1", ok: true},
		{name: "leading digit", in: "1*", ok: true},
		{name: "underscore", in: "_a*", ok: true},
		{name: "empty", in: "", is: fractal.ErrEmpty},
		{name: "question mark", in: "FDM?", is: fractal.ErrBadCharacter},
		{name: "slash", in: "hq/*", is: fractal.ErrBadCharacter},
		{name: "hyphen", in: "FDM-*", is: fractal.ErrBadCharacter},
		{name: "dot", in: "*.*", is: fractal.ErrBadCharacter},
		{name: "bracket", in: "[ab]*", is: fractal.ErrBadCharacter},
		{name: "non ascii", in: "á*", is: fractal.ErrBadCharacter},
	}
	checkNameCases(t, cases, func(s string) error { return fractal.Pattern(s).Validate() })
}

// TestPatternMatch tests the matching rule of fractal.md: a pattern matches the
// whole Interface Name, the comparison is case-sensitive, and the wildcard
// matches zero or more characters anywhere in the name.
func TestPatternMatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pattern fractal.Pattern
		subject fractal.InterfaceName
		want    bool
	}{
		{name: "everything", pattern: "*", subject: "FDMPrinter1", want: true},
		{name: "exact", pattern: "FDMPrinter1", subject: "FDMPrinter1", want: true},
		{name: "exact refuses a longer name", pattern: "FDMPrinter", subject: "FDMPrinter1"},
		{name: "prefix", pattern: "FDM*", subject: "FDMPrinter1", want: true},
		{name: "suffix", pattern: "*1", subject: "FDMPrinter1", want: true},
		{name: "middle", pattern: "FDM*1", subject: "FDMPrinter1", want: true},
		{name: "wildcard matches nothing", pattern: "FDM*Printer1", subject: "FDMPrinter1", want: true},
		{name: "case matters", pattern: "fdm*", subject: "FDMPrinter1"},
		{name: "case matters in a literal", pattern: "FDMPRINTER1", subject: "FDMPrinter1"},
		{name: "no partial match", pattern: "Printer", subject: "FDMPrinter1"},
		{name: "several wildcards", pattern: "*Print*1", subject: "FDMPrinter1", want: true},
		{name: "wildcard against an empty name", pattern: "*", subject: "", want: true},
		{name: "literal against an empty name", pattern: "a", subject: ""},
		{name: "trailing wildcards", pattern: "FDM***", subject: "FDM", want: true},
		{name: "a name that contains the wildcard character", pattern: "*", subject: "*0", want: true},
		{name: "a literal against the wildcard character", pattern: "a", subject: "*", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.pattern.Match(tc.subject); got != tc.want {
				t.Fatalf("%q.Match(%q) = %v, want %v", tc.pattern, tc.subject, got, tc.want)
			}
		})
	}
}

// TestPatternMatchDoesNotExplode tests that Match finishes on a pattern with
// many wildcards and a long name. A matcher that goes back to every earlier
// wildcard needs exponential time for this pattern, and the test would time
// out.
func TestPatternMatchDoesNotExplode(t *testing.T) {
	t.Parallel()
	pattern := fractal.Pattern("a*a*a*a*a*a*a*a*b")
	subject := fractal.InterfaceName(strings.Repeat("a", 20000))
	if pattern.Match(subject) {
		t.Fatal("the pattern ends in b and matched a name with no b in it")
	}
	if !pattern.Match(subject + "b") {
		t.Fatal("the pattern did not match a name that ends in b")
	}
}

// TestParsePatternReturnsThePattern tests that ParsePattern returns a valid
// string unchanged, and returns an error for a string with a question mark.
func TestParsePatternReturnsThePattern(t *testing.T) {
	t.Parallel()
	got, err := fractal.ParsePattern("FDM*")
	if err != nil || got.String() != "FDM*" {
		t.Fatalf("ParsePattern = %q %v", got, err)
	}
	if _, bad := fractal.ParsePattern("FDM?"); bad == nil {
		t.Fatal("ParsePattern accepted a question mark")
	}
}
