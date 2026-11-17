package manifest_test

import (
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// The cases follow Composer's own (composer/semver, VersionParserTest and the
// constraint examples of getcomposer.org/doc/articles/versions.md): each
// constraint, the releases it holds and the ones it does not.
func TestAConstraintReadsAsComposerReadsIt(t *testing.T) {
	t.Parallel()

	cases := []struct {
		constraint string
		holds      []string
		refuses    []string
	}{
		{"1.0.2", []string{"1.0.2"}, []string{"1.0.3", "1.0.2-beta1"}},
		{"=1.0.2", []string{"1.0.2"}, []string{"1.0.1"}},
		{"1.2", []string{"1.2.0"}, []string{"1.2.1"}},
		{">=1.0", []string{"1.0.0", "2.5.0", "1.0.0-beta1"}, []string{"0.9.9"}},
		{">=1.0 <2.0", []string{"1.0.0", "1.9.9"}, []string{"2.0.0", "2.0.0-beta1", "0.9.0"}},
		{">=1.0,<2.0", []string{"1.5.0"}, []string{"2.0.0"}},
		{">1.0", []string{"1.0.1"}, []string{"1.0.0"}},
		{"<=1.2.3", []string{"1.2.3"}, []string{"1.2.4"}},
		{"!=1.2.5", []string{"1.2.4", "1.2.6"}, []string{"1.2.5"}},
		{">=1.0 <1.1 || >=1.2", []string{"1.0.5", "1.2.0", "3.0.0"}, []string{"1.1.0"}},
		{"^1 || ^2", []string{"1.4.0", "2.0.0"}, []string{"3.0.0"}},
		{"1.0.*", []string{"1.0.0", "1.0.9"}, []string{"1.1.0"}},
		{"1.*", []string{"1.0.0", "1.9.0"}, []string{"2.0.0"}},
		{"1.x", []string{"1.3.0"}, []string{"2.0.0"}},
		{"*", []string{"0.0.1", "9.0.0"}, nil},
		{"1.0 - 2.0", []string{"1.0.0", "2.0.9"}, []string{"2.1.0", "0.9.0"}},
		{"1.0.0 - 2.1.0", []string{"2.1.0"}, []string{"2.1.1"}},
		{"~1.2", []string{"1.2.0", "1.9.9"}, []string{"2.0.0", "1.1.9"}},
		{"~1.2.3", []string{"1.2.3", "1.2.9"}, []string{"1.3.0", "1.2.2"}},
		{"~1", []string{"1.0.0", "1.9.0"}, []string{"2.0.0"}},
		{"^1.2.3", []string{"1.2.3", "1.9.0"}, []string{"2.0.0", "1.2.2"}},
		{"^0.3", []string{"0.3.0", "0.3.9"}, []string{"0.4.0"}},
		{"^0.0.3", []string{"0.0.3"}, []string{"0.0.4"}},
		{"^0.9", []string{"0.9.5"}, []string{"0.10.0"}},
		{"^1.2", []string{"1.2.0-beta1", "1.9.0"}, []string{"2.0.0-beta1"}},
		{"^1.2@beta", []string{"1.3.0-beta2"}, nil},
		{">=1.0.0-beta2", []string{"1.0.0-beta2", "1.0.0-RC1", "1.0.0"}, []string{"1.0.0-beta1"}},
		{"v1.2.3", []string{"1.2.3"}, nil},
	}

	for _, testCase := range cases {
		t.Run(testCase.constraint, func(t *testing.T) {
			t.Parallel()

			held, err := manifest.ParseConstraint(testCase.constraint)
			if err != nil {
				t.Fatalf("%q was refused: %v", testCase.constraint, err)
			}

			for _, version := range testCase.holds {
				if !held.HoldsText(version) {
					t.Errorf("%q does not hold %s", testCase.constraint, version)
				}
			}

			for _, version := range testCase.refuses {
				if held.HoldsText(version) {
					t.Errorf("%q holds %s", testCase.constraint, version)
				}
			}
		})
	}
}

func TestAConstraintNamesItsStability(t *testing.T) {
	t.Parallel()

	for constraint, want := range map[string]manifest.Stability{
		"^1.2@beta":      manifest.StabilityBeta,
		"@dev":           manifest.StabilityDev,
		">=1.0.0-alpha3": manifest.StabilityAlpha,
		"^2.0@RC":        manifest.StabilityRC,
	} {
		held, err := manifest.ParseConstraint(constraint)
		if err != nil {
			t.Fatal(err)
		}

		flag, named := held.Stability()
		if !named || flag != want {
			t.Errorf("%q names %s, want %s", constraint, flag, want)
		}
	}

	held, err := manifest.ParseConstraint("^1.2")
	if err != nil {
		t.Fatal(err)
	}

	if _, named := held.Stability(); named {
		t.Error("^1.2 names a stability")
	}
}

func TestAReleaseCarriesItsStability(t *testing.T) {
	t.Parallel()

	for version, want := range map[string]manifest.Stability{
		"1.0.0": manifest.StabilityStable, "1.0.0-beta.2": manifest.StabilityBeta, "1.0.0-RC1": manifest.StabilityRC,
		"1.0.0-alpha": manifest.StabilityAlpha, "1.0.0-dev": manifest.StabilityDev, "2.0.0-b3": manifest.StabilityBeta,
	} {
		held, err := manifest.ParseRelease(version)
		if err != nil {
			t.Fatal(err)
		}

		if held.Stability != want {
			t.Errorf("%s reads as %s", version, held.Stability)
		}
	}

	older, _ := manifest.ParseRelease("1.0.0-beta2")
	newer, _ := manifest.ParseRelease("1.0.0-RC1")

	if older.Compare(newer) >= 0 {
		t.Error("beta2 does not sort below RC1")
	}
}

func TestAConstraintComposerRefusesIsRefused(t *testing.T) {
	t.Parallel()

	for _, constraint := range []string{"1.2.3.4.5", "^", "1.0 -", "^1@nightly", "dev-main", ">= 1.0 ||"} {
		if _, err := manifest.ParseConstraint(constraint); err == nil {
			t.Errorf("%q was read", constraint)
		}
	}
}

// TestACoreConstraintHoldsAPrereleaseAsItsRelease is the platform package's
// own rule: a beta of the core runs a module that requires its release, and a
// constraint that names a prerelease of those numbers is read as written.
func TestACoreConstraintHoldsAPrereleaseAsItsRelease(t *testing.T) {
	t.Parallel()

	cases := []struct {
		constraint, core string
		held             bool
	}{
		{">=0.1.0", "0.1.0-beta.2", true},
		{"^0.1", "0.1.0-alpha.8", true},
		{">=0.2.0", "0.1.0-beta.2", false},
		{">=0.1.0-beta.3", "0.1.0-beta.2", false},
		{">=0.1.0-beta.1", "0.1.0-beta.2", true},
	}

	for _, one := range cases {
		wanted, err := manifest.ParseConstraint(one.constraint)
		if err != nil {
			t.Fatalf("%s: %v", one.constraint, err)
		}

		core, err := manifest.ParseRelease(one.core)
		if err != nil {
			t.Fatalf("%s: %v", one.core, err)
		}

		if got := wanted.HoldsCore(core); got != one.held {
			t.Errorf("%s holds core %s = %v, want %v", one.constraint, one.core, got, one.held)
		}
	}
}
