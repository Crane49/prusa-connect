package xbuddy_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
)

// coreOneSpellings is what a Core One can put in User-Agent-Printer, or leave
// registration to supply. `7.1.0` is the device type code the registration body
// carries and the driver falls back to when the header is absent, and it is the
// string the bench row on the old backlog recorded as matching nothing.
var coreOneSpellings = []string{"CoreOne", "COREONE", "coreone", "Core One", "core-one", "7.1.0"}

// The whole point of the alias rules, measured against the real driver and the
// real profile file rather than a fixture. Every spelling has to resolve to one
// capability set, because a printer offered a different set depending on how its
// firmware spelled itself is the defect this closes.
func TestACoreOneResolvesTheSameSetUnderEverySpellingItReports(t *testing.T) {
	t.Parallel()

	profiles, err := profile.Builtin()
	if err != nil {
		t.Fatalf("loading the built-in profiles: %v", err)
	}

	baseline := driverBaseline()
	resolver := capability.NewResolver(profiles, slog.New(slog.DiscardHandler))

	canonical := resolver.Resolve(t.Context(), baseline, identityOf("CoreOne"), nil, 1)
	if !canonical.ProfileMatched {
		t.Fatal("the model the file spells CoreOne matched no profile")
	}

	coverage := resolver.Resolve(t.Context(), baseline, identityOf("MK9000"), nil, 1)
	if len(coverage.Capabilities) == len(canonical.Capabilities) {
		t.Fatalf("the coverage tier resolves the same %d keys as the CoreOne profile, so this test "+
			"cannot tell a match from a fallback", len(coverage.Capabilities))
	}

	for _, reported := range coreOneSpellings {
		resolved := resolver.Resolve(t.Context(), baseline, identityOf(reported), nil, 1)

		switch {
		case !resolved.ProfileMatched:
			t.Errorf("a printer reporting %q matched no profile and was held to %d coverage keys",
				reported, len(resolved.Capabilities))
		case resolved.Digest != canonical.Digest:
			t.Errorf("a printer reporting %q resolved digest %s and %d keys, want %s and %d",
				reported, resolved.Digest, len(resolved.Capabilities),
				canonical.Digest, len(canonical.Capabilities))
		}
	}
}

// The reading at the bench, on the two lines a person there has. A spelling that
// resolves says which profile it landed on, and a spelling that does not says
// what the printer sent and what the file carries.
func TestTheLogNamesBothSpellings(t *testing.T) {
	t.Parallel()

	profiles, err := profile.Builtin()
	if err != nil {
		t.Fatalf("loading the built-in profiles: %v", err)
	}

	for _, tc := range []struct {
		name     string
		reported string
		level    string
		want     []string
	}{
		{
			name:     "a device type code resolves and says so",
			reported: "7.1.0",
			level:    "INFO",
			want:     []string{"model=7.1.0", "profile_model=CoreOne"},
		},
		{
			name:     "a spelling nothing reaches names both sides",
			reported: "Core Two",
			level:    "WARN",
			want:     []string{`model="Core Two"`, "CoreOne", "covered="},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			log := slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelInfo}))
			resolver := capability.NewResolver(profiles, log)

			resolver.Resolve(t.Context(), driverBaseline(), identityOf(tc.reported), nil, 1)

			line := out.String()
			t.Logf("observed: %s", strings.TrimSpace(line))

			if !strings.Contains(line, "level="+tc.level) {
				t.Errorf("a printer reporting %q logged no %s line: %s", tc.reported, tc.level, line)
			}

			for _, want := range tc.want {
				if !strings.Contains(line, want) {
					t.Errorf("the line for %q does not carry %s: %s", tc.reported, want, line)
				}
			}
		})
	}
}

// A spelling that reaches nothing stays reaching nothing. The coverage tier and
// the warning are the answer for a machine this build has never met, and a rule
// loose enough to guess would hand it another model's verbs instead.
func TestAPrinterThisBuildHasNeverMetStillMatchesNothing(t *testing.T) {
	t.Parallel()

	profiles, err := profile.Builtin()
	if err != nil {
		t.Fatalf("loading the built-in profiles: %v", err)
	}

	resolver := capability.NewResolver(profiles, slog.New(slog.DiscardHandler))

	for _, reported := range []string{"Voron 2.4", "Bambu X1C", "MK3S", "", "9.9.9"} {
		resolved := resolver.Resolve(t.Context(), driverBaseline(), identityOf(reported), nil, 1)
		if resolved.ProfileMatched {
			t.Errorf("a printer reporting %q was matched to a profile", reported)
		}

		if !resolved.CoverageApplied {
			t.Errorf("a printer reporting %q was not held to the coverage tier", reported)
		}
	}
}
