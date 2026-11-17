package install_test

import (
	"testing"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
)

// TestABetaCoreIsHeldAsItsRelease covers a beta tester on a prerelease tag.
// Under npm's rule a prerelease is outside every range that names no
// prerelease, so every module refused to install on v0.1.0-beta.1. The check
// holds the release numbers instead, in both directions, unless the range
// names a prerelease of the same three numbers.
func TestABetaCoreIsHeldAsItsRelease(t *testing.T) {
	t.Parallel()

	cases := []struct {
		running string
		core    string
		inside  bool
	}{
		{running: "0.1.0-beta.1", core: "0.1.0", inside: true},
		{running: "0.1.0-beta.1", core: ">=0.1.0", inside: true},
		{running: "0.1.0-beta.1", core: "^0.1.0", inside: true},
		{running: "0.1.0+3835f5f6e0a1.dirty", core: "^0.1.0", inside: true},
		{running: "0.1.0-beta.1", core: ">=0.2.0", inside: false},
		{running: "0.1.0-beta.1", core: "^0.2.0", inside: false},
		{running: "0.2.0", core: "^0.1.0", inside: false},
		{running: "0.0.1-alpha.6", core: ">=0.0.1", inside: true},
		{running: "0.0.1-alpha.6", core: ">=0.0.1-alpha.7", inside: false},
		{running: "0.0.1-alpha.7", core: ">=0.0.1-alpha.7", inside: true},
	}

	for _, tc := range cases {
		t.Run(tc.running+" in "+tc.core, func(t *testing.T) {
			t.Parallel()

			in, readable := install.CoreHolds(tc.core, tc.running)
			if !readable {
				t.Fatalf("%q was not read as a version", tc.running)
			}

			if in != tc.inside {
				t.Errorf("%q in %q = %v, want %v", tc.running, tc.core, in, tc.inside)
			}
		})
	}

	if _, readable := install.CoreHolds(">=1.0", "(devel)"); readable {
		t.Errorf("(devel) was read as a version, so the check would not be skipped")
	}
}
