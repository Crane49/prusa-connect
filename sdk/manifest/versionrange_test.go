package manifest_test

import (
	"errors"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// A range is what a module author writes to say which versions it runs on, and
// the grammar is npm's, because that is the one they already know. These cases
// are the forms, the shape each one is read into, and what is refused.

// TestARangeIsReadIntoTheComparatorsItMeans holds every form of the grammar to
// the comparators npm reads it as. The rendering is the check rather than the
// members, so a case says what an author wrote and what the host understood.
func TestARangeIsReadIntoTheComparatorsItMeans(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		// Exact pins.
		"1.2.3":  "=1.2.3",
		"=1.2.3": "=1.2.3",
		"v1.2.3": "=1.2.3",

		// The four comparators, written out.
		">=1.2.3": ">=1.2.3",
		">1.2.3":  ">1.2.3",
		"<=1.2.3": "<=1.2.3",
		"<1.2.3":  "<1.2.3",

		// Two comparators in one alternative, which is the form this project
		// wrote before the grammar widened.
		">=0.3.0 <1.0.0": ">=0.3.0 <1.0.0",
		">1.2.3 <2.0.0":  ">1.2.3 <2.0.0",

		// Caret: up to the next change that may break.
		"^1.2.3": ">=1.2.3 <2.0.0",
		"^0.2.3": ">=0.2.3 <0.3.0",
		"^0.0.3": ">=0.0.3 <0.0.4",
		"^1.2.x": ">=1.2.0 <2.0.0",
		"^0.0.x": ">=0.0.0 <0.1.0",
		"^1.x":   ">=1.0.0 <2.0.0",
		"^0.x":   ">=0.0.0 <1.0.0",
		"^1":     ">=1.0.0 <2.0.0",

		// Tilde: patches of the minor that was named.
		"~1.2.3":  ">=1.2.3 <1.3.0",
		"~1.2":    ">=1.2.0 <1.3.0",
		"~1":      ">=1.0.0 <2.0.0",
		"~0.2.3":  ">=0.2.3 <0.3.0",
		"~>1.2.3": ">=1.2.3 <1.3.0",

		// Partial versions and wildcards.
		"1.2":   ">=1.2.0 <1.3.0",
		"1.2.x": ">=1.2.0 <1.3.0",
		"1.x":   ">=1.0.0 <2.0.0",
		"1":     ">=1.0.0 <2.0.0",
		"*":     ">=0.0.0",
		"x":     ">=0.0.0",
		"":      ">=0.0.0",

		// Partial versions after a comparator, filled in the way npm fills
		// them: a floor takes zeros and an open upper bound takes the next
		// line.
		">=1.2": ">=1.2.0",
		"<1.2":  "<1.2.0",
		">1.2":  ">=1.3.0",
		">1":    ">=2.0.0",
		"<=1.2": "<1.3.0",
		"=1.2":  ">=1.2.0 <1.3.0",

		// Hyphen ranges, with a partial on either side.
		"1.2.3 - 2.3.4": ">=1.2.3 <=2.3.4",
		"1.2.3 - 2.3":   ">=1.2.3 <2.4.0",
		"1.2.3 - 2":     ">=1.2.3 <3.0.0",
		"1.2 - 2.3.4":   ">=1.2.0 <=2.3.4",

		// Alternatives.
		"^1 || ^2":          ">=1.0.0 <2.0.0 || >=2.0.0 <3.0.0",
		"1.2.3 || >=2.0.0":  "=1.2.3 || >=2.0.0",
		"<1.0.0 || >=2.0.0": "<1.0.0 || >=2.0.0",

		// Prereleases are versions like any other, and they carry into the
		// comparator they were written on.
		">=1.0.0-rc.1":       ">=1.0.0-rc.1",
		"1.0.0-rc.1":         "=1.0.0-rc.1",
		"^1.0.0-rc.1":        ">=1.0.0-rc.1 <2.0.0",
		">=1.0.0-alpha.1+b7": ">=1.0.0-alpha.1+b7",
	}

	for text, want := range cases {
		t.Run(text, func(t *testing.T) {
			t.Parallel()

			held, err := manifest.ParseRange(text)
			if err != nil {
				t.Fatalf("%q was refused: %v", text, err)
			}

			if got := held.Describe(); got != want {
				t.Errorf("%q reads as %q, expected %q", text, got, want)
			}
		})
	}
}

// TestARangeThisGrammarDoesNotNameIsRefused is the other half. Each of these
// is a shape an author writes by mistake, and reading half of one would let a
// bundle load against a library that broke it.
func TestARangeThisGrammarDoesNotNameIsRefused(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"a version with four fields":        "1.2.3.4",
		"a version with a letter in it":     "1.2.beta",
		"a zero-padded field":               "1.02.0",
		"an empty field":                    "1..3",
		"a comparator with no version":      ">=",
		"an operator on a wildcard":         ">=*",
		"a wildcard before a number":        "1.x.3",
		"a prerelease on a wildcard":        "1.x-rc.1",
		"a hyphen with nothing after it":    "1.2.3 -",
		"a hyphen with nothing before":      "- 2.0.0",
		"an empty prerelease":               "1.2.3-",
		"an empty build":                    "1.2.3+",
		"a prerelease with an empty part":   "1.2.3-rc..1",
		"a prerelease outside the alphabet": "1.2.3-rc/1",
		"a zero-padded prerelease number":   "1.2.3-01",
		"a comparator nobody writes":        "=>1.2.3",
		"a negative field":                  "1.-2.3",
	}

	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			held, err := manifest.ParseRange(text)
			if err == nil {
				t.Fatalf("%q was accepted and reads as %q", text, held.Describe())
			}

			if !errors.Is(err, manifest.ErrInvalid) {
				t.Errorf("the refusal of %q does not wrap ErrInvalid: %v", text, err)
			}
		})
	}
}

// TestWhichVersionsARangeHolds drives the answer a caller actually reads: is
// this version in this range.
func TestWhichVersionsARangeHolds(t *testing.T) {
	t.Parallel()

	type check struct {
		text    string
		version string
		inside  bool
	}

	cases := []check{
		{"^1.2.3", "1.2.3", true},
		{"^1.2.3", "1.9.9", true},
		{"^1.2.3", "2.0.0", false},
		{"^1.2.3", "1.2.2", false},
		{"~1.2.3", "1.2.9", true},
		{"~1.2.3", "1.3.0", false},
		{"^0.2.3", "0.2.9", true},
		{"^0.2.3", "0.3.0", false},
		{"1.2.3", "1.2.3", true},
		{"1.2.3", "1.2.4", false},
		{"*", "0.0.1", true},
		{"*", "99.0.0", true},
		{">=0.3.0 <1.0.0", "0.3.0", true},
		{">=0.3.0 <1.0.0", "1.0.0", false},
		{"1.2.3 - 2.3.4", "2.3.4", true},
		{"1.2.3 - 2.3.4", "2.3.5", false},
		{"^1 || ^3", "2.0.0", false},
		{"^1 || ^3", "3.4.5", true},

		// The prerelease rule. A candidate is in a range only where the author
		// named a candidate of the same three numbers.
		{">=1.0.0", "2.0.0-rc.1", false},
		{">=1.0.0-rc.1", "1.0.0-rc.2", true},
		{">=1.0.0-rc.1", "1.0.0-rc.0", false},
		{">=1.0.0-rc.1 <2.0.0", "1.5.0", true},
		{"^1.0.0-rc.1", "2.0.0-rc.1", false},
		{"1.2.3-rc.1", "1.2.3-rc.1", true},
		{"1.2.3-rc.1", "1.2.3", false},

		// Semver's own ordering of prereleases, read through a range.
		{">1.0.0-alpha", "1.0.0-alpha.1", true},
		{">1.0.0-alpha.1", "1.0.0-alpha.beta", true},
		{">1.0.0-beta.2", "1.0.0-beta.11", true},
		{">1.0.0-rc.1", "1.0.0-beta.11", false},

		// Build metadata takes no part in ordering.
		{"1.2.3", "1.2.3+build.7", true},
	}

	for _, held := range cases {
		t.Run(held.text+" holds "+held.version, func(t *testing.T) {
			t.Parallel()

			parsed, err := manifest.ParseRange(held.text)
			if err != nil {
				t.Fatalf("%q was refused: %v", held.text, err)
			}

			version, err := manifest.ParseVersion(held.version)
			if err != nil {
				t.Fatalf("%q was refused: %v", held.version, err)
			}

			if got := parsed.Holds(version); got != held.inside {
				t.Errorf("%q holds %q: %t, expected %t", held.text, held.version, got, held.inside)
			}
		})
	}
}

// TestTwoVersionsAreOrderedAsSemverSaysThey covers the ordering on its own,
// because a range is only as right as the comparison under it.
func TestTwoVersionsAreOrderedAsSemverSaysThey(t *testing.T) {
	t.Parallel()

	// Every pair is left below right, which is the example order in semver
	// section 11.
	pairs := [][2]string{
		{"1.0.0", "2.0.0"},
		{"2.0.0", "2.1.0"},
		{"2.1.0", "2.1.1"},
		{"1.0.0-alpha", "1.0.0"},
		{"1.0.0-alpha", "1.0.0-alpha.1"},
		{"1.0.0-alpha.1", "1.0.0-alpha.beta"},
		{"1.0.0-alpha.beta", "1.0.0-beta"},
		{"1.0.0-beta", "1.0.0-beta.2"},
		{"1.0.0-beta.2", "1.0.0-beta.11"},
		{"1.0.0-beta.11", "1.0.0-rc.1"},
		{"1.0.0-rc.1", "1.0.0"},
	}

	for _, pair := range pairs {
		t.Run(pair[0]+" is below "+pair[1], func(t *testing.T) {
			t.Parallel()

			left, err := manifest.ParseVersion(pair[0])
			if err != nil {
				t.Fatalf("%q was refused: %v", pair[0], err)
			}

			right, err := manifest.ParseVersion(pair[1])
			if err != nil {
				t.Fatalf("%q was refused: %v", pair[1], err)
			}

			if left.Compare(right) >= 0 {
				t.Errorf("%s is not below %s", pair[0], pair[1])
			}

			if right.Compare(left) <= 0 {
				t.Errorf("%s is not above %s", pair[1], pair[0])
			}
		})
	}

	// Build metadata is ignored, which is semver rule 10.
	plain, err := manifest.ParseVersion("1.0.0")
	if err != nil {
		t.Fatalf("1.0.0 was refused: %v", err)
	}

	built, err := manifest.ParseVersion("1.0.0+build.7")
	if err != nil {
		t.Fatalf("1.0.0+build.7 was refused: %v", err)
	}

	if plain.Compare(built) != 0 {
		t.Error("build metadata changed the order of two versions")
	}
}

// TestAnExactRangeAnswersTheVersionItPins is what `module add` reads: an
// operator who named one release gets that release, and one who named a range
// gets a set.
func TestAnExactRangeAnswersTheVersionItPins(t *testing.T) {
	t.Parallel()

	pinned, err := manifest.ParseRange("1.2.3")
	if err != nil {
		t.Fatalf("1.2.3 was refused: %v", err)
	}

	version, exact := pinned.Exact()
	if !exact || version.String() != "1.2.3" {
		t.Errorf("1.2.3 pins %s, %t", version, exact)
	}

	loose, err := manifest.ParseRange("^1.2.3")
	if err != nil {
		t.Fatalf("^1.2.3 was refused: %v", err)
	}

	if _, exact = loose.Exact(); exact {
		t.Error("^1.2.3 was read as pinning one version")
	}
}

// A core range is held against the core's three numbers, unless it names a
// prerelease of those three numbers, so a module can require the prerelease
// that first read a key it uses.
func TestACoreRangeTellsPrereleasesOfOneReleaseApartOnlyWhenItNamesOne(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		rng, core string
		want      bool
	}{
		{">=0.0.1", "0.0.1-alpha.6", true},
		{">=0.0.1-alpha.7", "0.0.1-alpha.6", false},
		{">=0.0.1-alpha.7", "0.0.1-alpha.7", true},
		{">=0.0.1-alpha.7", "0.0.1-alpha.10", true},
		{">=0.0.1-alpha.7", "0.0.1", true},
		{">=0.0.1-alpha.7", "0.0.2-beta.1", true},
		{"^0.1.0", "0.2.0-rc.1", false},
		{">=0.1.0 <0.2.0", "0.1.5-beta.1", true},
	} {
		rng, err := manifest.ParseRange(tc.rng)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.rng, err)
		}

		core, err := manifest.ParseVersion(tc.core)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.core, err)
		}

		if got := rng.HoldsCore(core); got != tc.want {
			t.Errorf("%q holds the core %s = %v, want %v", tc.rng, tc.core, got, tc.want)
		}
	}
}
