package fractal_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// nameCase is one input string for a Validate method. ok says whether Validate
// must accept it. For a string Validate must refuse, is names the error that
// errors.Is must find.
type nameCase struct {
	name string
	in   string
	ok   bool
	is   error
}

// checkNameCases runs validate on every case. For a refused string it also
// checks that the error is a *NameError whose Value is the input.
func checkNameCases(t *testing.T, cases []nameCase, validate func(string) error) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validate(tc.in)
			if tc.ok {
				if err != nil {
					t.Fatalf("Validate refused the valid name %q: %v", tc.in, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate accepted the invalid name %q", tc.in)
			}
			if tc.is != nil && !errors.Is(err, tc.is) {
				t.Fatalf("Validate(%q) = %v, want an error that wraps %v", tc.in, err, tc.is)
			}
			var ne *fractal.NameError
			if !errors.As(err, &ne) {
				t.Fatalf("Validate(%q) returned an error of type %T, want *fractal.NameError", tc.in, err)
			}
			if ne.Value != tc.in {
				t.Fatalf("Validate(%q) returned a NameError with Value %q", tc.in, ne.Value)
			}
		})
	}
}

// TestIdentifierGrammar tests the Identifier rule of fractal.md: letters, digits
// and underscores, and the first character is a letter or an underscore.
func TestIdentifierGrammar(t *testing.T) {
	t.Parallel()
	cases := []nameCase{
		{name: "letter", in: "a", ok: true},
		{name: "underscore first", in: "_x9", ok: true},
		{name: "mixed case and digits", in: "FDMPrinter1", ok: true},
		{name: "long", in: strings.Repeat("a", 4096), ok: true},
		{name: "empty", in: "", is: fractal.ErrEmpty},
		{name: "leading digit", in: "1a", is: fractal.ErrBadFirstCharacter},
		{name: "hyphen", in: "a-b", is: fractal.ErrBadCharacter},
		{name: "slash", in: "a/b", is: fractal.ErrBadCharacter},
		{name: "space", in: "a b", is: fractal.ErrBadCharacter},
		{name: "wildcard", in: "a*", is: fractal.ErrBadCharacter},
		{name: "non ascii", in: "teplých", is: fractal.ErrBadCharacter},
	}
	checkNameCases(t, cases, func(s string) error { return fractal.Identifier(s).Validate() })
	checkNameCases(t, cases, func(s string) error { return fractal.InterfaceName(s).Validate() })
	checkNameCases(t, cases, func(s string) error { return fractal.PropertyName(s).Validate() })
	checkNameCases(t, cases, func(s string) error { return fractal.MethodName(s).Validate() })
}

// TestInstanceIDGrammar tests that an Interface Instance ID follows the
// Identifier rule, and that it may start with a digit.
func TestInstanceIDGrammar(t *testing.T) {
	t.Parallel()
	cases := []nameCase{
		{name: "digit", in: "1", ok: true},
		{name: "digits", in: "42", ok: true},
		{name: "letter", in: "left", ok: true},
		{name: "underscore first", in: "_1", ok: true},
		{name: "empty", in: "", is: fractal.ErrEmpty},
		{name: "hyphen", in: "1-2", is: fractal.ErrBadCharacter},
	}
	checkNameCases(t, cases, func(s string) error { return fractal.InstanceID(s).Validate() })
}

// TestNodeIDGrammar tests the Node ID rule of fractal.md: letters, digits,
// underscores and hyphens, no hyphen as the first character, and at most 64
// characters.
func TestNodeIDGrammar(t *testing.T) {
	t.Parallel()
	cases := []nameCase{
		{name: "hyphenated", in: "printer-001", ok: true},
		{name: "digit first", in: "1printer", ok: true},
		{name: "underscore", in: "a_b", ok: true},
		{name: "sixty four characters", in: strings.Repeat("n", 64), ok: true},
		{name: "sixty five characters", in: strings.Repeat("n", 65), is: fractal.ErrTooLong},
		{name: "leading hyphen", in: "-printer", is: fractal.ErrBadFirstCharacter},
		{name: "empty", in: "", is: fractal.ErrEmpty},
		{name: "slash", in: "hq/brno", is: fractal.ErrBadCharacter},
		{name: "dot", in: "printer.1", is: fractal.ErrBadCharacter},
		{name: "non ascii", in: "přinter", is: fractal.ErrBadCharacter},
	}
	checkNameCases(t, cases, func(s string) error { return fractal.NodeID(s).Validate() })
}

// TestParsersReturnTheValidatedName tests that each Parse function returns a
// valid string unchanged, and that ParseNodeID returns an error for an invalid
// Node ID.
func TestParsersReturnTheValidatedName(t *testing.T) {
	t.Parallel()
	if got, err := fractal.ParseIdentifier("ok"); err != nil || got != "ok" {
		t.Fatalf("identifier: %q %v", got, err)
	}
	if got, err := fractal.ParseInterfaceName("FDMPrinter1"); err != nil || got.String() != "FDMPrinter1" {
		t.Fatalf("interface name: %q %v", got, err)
	}
	if got, err := fractal.ParseInstanceID("1"); err != nil || got.String() != "1" {
		t.Fatalf("instance id: %q %v", got, err)
	}
	if got, err := fractal.ParsePropertyName("nozzleTemp"); err != nil || got.String() != "nozzleTemp" {
		t.Fatalf("property name: %q %v", got, err)
	}
	if got, err := fractal.ParseMethodName("SetNozzleTemp"); err != nil || got.String() != "SetNozzleTemp" {
		t.Fatalf("method name: %q %v", got, err)
	}
	if got, err := fractal.ParseNodeID("printer-1"); err != nil || got.String() != "printer-1" {
		t.Fatalf("node id: %q %v", got, err)
	}
	if _, bad := fractal.ParseNodeID("-bad"); bad == nil {
		t.Fatal("node id: accepted a leading hyphen")
	}
}

// TestNameErrorNamesTheGrammar tests that the message of a NameError has the
// name of the grammar and the invalid value.
func TestNameErrorNamesTheGrammar(t *testing.T) {
	t.Parallel()
	err := fractal.NodeID("-bad").Validate()
	if err == nil {
		t.Fatal("Validate accepted a Node ID that starts with a hyphen")
	}
	if got := err.Error(); !strings.Contains(got, "node id") || !strings.Contains(got, "-bad") {
		t.Fatalf("the message %q must contain both \"node id\" and \"-bad\"", got)
	}
}
