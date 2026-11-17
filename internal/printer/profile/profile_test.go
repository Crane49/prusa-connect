package profile_test

import (
	"errors"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

const verbHome = connectorapi.Capability("core.command.home")

func label(s string) connectorapi.FirmwareLabel { return connectorapi.NewFirmwareLabel(s) }

func one(model string, firmware profile.Range, set connectorapi.Set) profile.Registry {
	return profile.Registry{
		Schema:   profile.CurrentSchema,
		Profiles: []profile.Profile{{Model: model, Firmware: firmware, Capabilities: set}},
	}
}

// A firmware lineup is not a totally ordered set, so the range is the only
// place a comparison happens. These pin what it means.
func TestRangeContains(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		bounds   profile.Range
		firmware string
		want     bool
	}{
		{name: "at the lower bound", bounds: profile.Range{From: "6.4.0"}, firmware: "6.4.0", want: true},
		{name: "below the lower bound", bounds: profile.Range{From: "6.4.0"}, firmware: "6.3.9"},
		{name: "far above an open range", bounds: profile.Range{From: "6.4.0"}, firmware: "9.9.9", want: true},
		{
			name:   "at the upper bound, which is exclusive",
			bounds: profile.Range{From: "6.4.0", To: "7.0.0"}, firmware: "7.0.0",
		},
		{
			name:   "inside a closed range",
			bounds: profile.Range{From: "6.4.0", To: "7.0.0"}, firmware: "6.9.9", want: true,
		},
		{
			name:   "a two-part version pads rather than sorting as text",
			bounds: profile.Range{From: "6.4.0"}, firmware: "6.4", want: true,
		},
		{
			name:   "a release candidate is below the release it is a candidate for",
			bounds: profile.Range{From: "6.4.0"}, firmware: "6.4.0-RC",
		},
		{
			name:   "a release candidate stays in the band that ends at its release",
			bounds: profile.Range{From: "6.4.0", To: "6.5.0"}, firmware: "6.5.0-RC", want: true,
		},
		{
			name:   "a candidate for the floor is in a band that starts at the first candidate",
			bounds: profile.Range{From: "6.4.0-0"}, firmware: "6.4.0-RC", want: true,
		},
		{
			name:   "a candidate is still above the release below it",
			bounds: profile.Range{From: "6.4.0"}, firmware: "6.5.0-RC", want: true,
		},
		{
			name:   "an alphanumeric tag is above a numeric one",
			bounds: profile.Range{From: "6.4.0-1"}, firmware: "6.4.0-RC", want: true,
		},
		{
			name:   "a longer tag is above the one it extends",
			bounds: profile.Range{From: "6.4.0-RC.2"}, firmware: "6.4.0-RC",
		},
		{
			name:   "a dotted alpha is below the release it is an alpha of",
			bounds: profile.Range{From: "6.4.0"}, firmware: "6.4.0.alpha",
		},
		{
			name:   "a dotted alpha stays in the band that ends at its release",
			bounds: profile.Range{From: "6.4.0", To: "6.5.0"}, firmware: "6.5.0.alpha", want: true,
		},
		{
			name:   "a dotted alpha is not in the band its release opens",
			bounds: profile.Range{From: "6.5.0"}, firmware: "6.5.0.alpha",
		},
		{
			name:   "a dotted alpha is above the first candidate for the floor",
			bounds: profile.Range{From: "6.4.0-0"}, firmware: "6.4.0.alpha", want: true,
		},
		{
			name:   "a dotted tag and a hyphenated one order the same way",
			bounds: profile.Range{From: "6.4.0", To: "6.5.0"}, firmware: "6.5.0-alpha", want: true,
		},
		{
			name:   "a fourth number counts builds and is not a candidate for anything",
			bounds: profile.Range{From: "6.4.0"}, firmware: "6.4.0.1", want: true,
		},
		{
			name:   "ten sorts above nine rather than below it",
			bounds: profile.Range{From: "6.9.0"}, firmware: "6.10.0", want: true,
		},
		{
			name:   "a build suffix orders on the version and not on the suffix",
			bounds: profile.Range{From: "4.7.5", To: "4.8.0"}, firmware: "4.7.10+8628", want: true,
		},
		{
			name:   "a build suffix below the floor is still below it",
			bounds: profile.Range{From: "4.7.5"}, firmware: "4.7.1+9999",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.bounds.Contains(label(tc.firmware)); got != tc.want {
				t.Errorf("Contains(%q) = %v, want %v", tc.firmware, got, tc.want)
			}
		})
	}
}

// Every ASCII letter is above every digit, so any text fallback puts an
// unreadable label above every numeric version. With every shipped range
// open-ended, the only question asked of a version is "is this at least the
// floor", and a text fallback answers yes to "MK4", "unknown" and "v5.0.0"
// alike: a hundred capabilities the device never claimed. That is a phantom
// where the whole model promises a missing feature, so an unreadable label is
// refused and takes the loud unmatched path.
func TestAnUnreadableFirmwareLabelMatchesNothing(t *testing.T) {
	t.Parallel()

	registry := one("MK4", profile.Range{From: "6.4.0"}, connectorapi.Set{verbHome: {Flag: true}})

	for _, reported := range []string{
		"v5.0.0",
		"MK4",
		"abc",
		"unknown",
		"MK4-6.3.0",
		"",
		// Both spellings of a padded label. Trimming them was considered and
		// refused: see parseVersion, which says why.
		" 6.4.0",
		"6.4.0 ",
		"6.4.x",
		"+6.4.0",
		"-6.4.0",
		"6..0",
		"99999999999999999999.0.0",
		// A prerelease tag semver's grammar refuses: an empty identifier, and
		// one carrying a character an identifier may not.
		"6.4.0-",
		"6.4.0-RC..1",
		"6.4.0-RC 1",
		// A dotted tag reaches the same grammar, so a trailing dot and a space
		// in the tag are refused whichever separator opened it.
		"6.4.0.",
		"6.4.0.alpha 1",
	} {
		if _, ok := registry.Match("MK4", label(reported)); ok {
			t.Errorf("firmware %q selected a profile, and it is not a version", reported)
		}
	}

	// A candidate for the floor is below the floor, so it takes the unmatched
	// path against a band that starts at the release. The shipped bands start at
	// the first candidate instead, which is what TestTheFirmwareFloorMatchesThe
	// ShippingBuilder holds them to.
	if _, ok := registry.Match("MK4", label("6.4.0-RC")); ok {
		t.Error("a candidate for the floor matched a band that starts at the release")
	}

	// The labels a device really reports still match, or the refusal has taken
	// the working path with it.
	for _, reported := range []string{"6.4.0", "6.4", "6.5.0-RC", "6.4.0+8628", "7.0.0"} {
		if _, ok := registry.Match("MK4", label(reported)); !ok {
			t.Errorf("firmware %q matched nothing, and it is a version at or above the floor", reported)
		}
	}
}

// The same refusal against the profiles this binary really ships, which is where
// the hundred phantom capabilities came from.
func TestTheBuiltinProfilesRefuseAnUnreadableFirmwareLabel(t *testing.T) {
	t.Parallel()

	registry, err := profile.Builtin()
	if err != nil {
		t.Fatalf("loading the built-in profiles: %v", err)
	}

	for _, reported := range []string{"v5.0.0", "abc", "unknown", "MK4-6.3.0"} {
		matched, ok := registry.Match("MK4", label(reported))
		if ok {
			t.Errorf("firmware %q selected a profile of %d capabilities",
				reported, len(matched.Capabilities))
		}
	}
}

// A profile whose own bound is not a version covers no printer and would say so
// to nobody, so the file is refused rather than shipped inert.
func TestValidateRefusesABoundThatIsNotAVersion(t *testing.T) {
	t.Parallel()

	for _, bounds := range []profile.Range{
		{From: "latest"},
		{From: "v6.4.0"},
		{From: "6.4.0", To: "next"},
	} {
		registry := one("MK4", bounds, connectorapi.Set{verbHome: {Flag: true}})
		if err := profile.Validate(registry); err == nil {
			t.Errorf("a profile covering %+v was accepted", bounds)
		}
	}
}

func TestMatchIgnoresTheCaseOfAReportedModel(t *testing.T) {
	t.Parallel()

	registry := one("CoreOne", profile.Range{From: "6.4.0"}, connectorapi.Set{verbHome: {Flag: true}})

	for _, reported := range []string{"CoreOne", "COREONE", "coreone"} {
		if _, ok := registry.Match(reported, label("6.4.0")); !ok {
			t.Errorf("a printer reporting %q matched no profile", reported)
		}
	}
}

// No match is the loud-but-working path: the caller falls back to the baseline.
// It must never look like a match.
func TestMatchReportsNoProfileRatherThanAnEmptyOne(t *testing.T) {
	t.Parallel()

	registry := one("CoreOne", profile.Range{From: "6.4.0"}, connectorapi.Set{verbHome: {Flag: true}})

	if _, ok := registry.Match("CoreOne", label("6.3.0")); ok {
		t.Error("a firmware below the range matched a profile")
	}

	if _, ok := registry.Match("Voron", label("6.4.0")); ok {
		t.Error("an unknown model matched a profile")
	}
}

func TestValidateRefusesAProfileThatCannotBeActedOn(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		registry profile.Registry
		want     error
	}{
		{
			name:     "a schema this build does not read",
			registry: profile.Registry{Schema: profile.CurrentSchema + 1},
			want:     profile.ErrSchema,
		},
		{
			name:     "no lower firmware bound",
			registry: one("MK4", profile.Range{}, connectorapi.Set{verbHome: {Flag: true}}),
			want:     profile.ErrInvalidProfile,
		},
		{
			name:     "a range that covers nothing",
			registry: one("MK4", profile.Range{From: "7.0.0", To: "6.4.0"}, connectorapi.Set{verbHome: {Flag: true}}),
			want:     profile.ErrInvalidProfile,
		},
		{
			name: "a key the vocabulary's syntax rejects",
			registry: one("MK4", profile.Range{From: "6.4.0"},
				connectorapi.Set{"core.command.HOME": {Flag: true}}),
			want: profile.ErrInvalidProfile,
		},
		{
			name: "a key present with its flag off",
			registry: one("MK4", profile.Range{From: "6.4.0"},
				connectorapi.Set{verbHome: {Flag: false}}),
			want: profile.ErrInvalidProfile,
		},
		{
			name: "an argument whose verb the profile does not declare",
			registry: one("MK4", profile.Range{From: "6.4.0"},
				connectorapi.Set{"core.command.home#axis": {Flag: true, Kind: connectorapi.KindString}}),
			want: profile.ErrInvalidProfile,
		},
		{
			name: "a default outside its own supported set",
			registry: one("MK4", profile.Range{From: "6.4.0"}, connectorapi.Set{
				verbHome: {Flag: true},
				"core.command.home#axis": {
					Flag: true, Kind: connectorapi.KindString, Supported: []string{"x"}, Default: "y",
				},
			}),
			want: profile.ErrInvalidProfile,
		},
		{
			name: "two profiles for one model whose ranges overlap",
			registry: profile.Registry{
				Schema: profile.CurrentSchema,
				Profiles: []profile.Profile{
					{Model: "MK4", Firmware: profile.Range{From: "6.4.0", To: "7.0.0"},
						Capabilities: connectorapi.Set{verbHome: {Flag: true}}},
					{Model: "mk4", Firmware: profile.Range{From: "6.9.0"},
						Capabilities: connectorapi.Set{verbHome: {Flag: true}}},
				},
			},
			want: profile.ErrOverlappingRange,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := profile.Validate(tc.registry)
			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestValidateAcceptsAdjacentRanges(t *testing.T) {
	t.Parallel()

	registry := profile.Registry{
		Schema: profile.CurrentSchema,
		Profiles: []profile.Profile{
			{Model: "MK4", Firmware: profile.Range{From: "6.4.0", To: "7.0.0"},
				Capabilities: connectorapi.Set{verbHome: {Flag: true}}},
			{Model: "MK4", Firmware: profile.Range{From: "7.0.0"},
				Capabilities: connectorapi.Set{verbHome: {Flag: true}}},
		},
	}

	if err := profile.Validate(registry); err != nil {
		t.Errorf("two bands that meet at a boundary were refused: %v", err)
	}
}

// A retired key is refused forever. Reissuing one is how a reader silently
// acquires a new meaning under an old name, which no schema language catches.
func TestValidateRefusesARetiredKey(t *testing.T) {
	t.Parallel()

	registry := one("MK4", profile.Range{From: "6.4.0"}, connectorapi.Set{verbHome: {Flag: true}})

	if err := profile.ValidateWithReserved(registry, []connectorapi.Capability{verbHome}); !errors.Is(
		err, profile.ErrReservedKey,
	) {
		t.Errorf("err = %v, want a retired key to be refused", err)
	}

	if err := profile.ValidateWithReserved(registry, nil); err != nil {
		t.Errorf("a key that was never retired was refused: %v", err)
	}
}

// Every key in the committed profiles is checked against the retirement list at
// load, so a name that comes back has one place to be caught.
func TestTheBuiltinProfilesUseNoRetiredKey(t *testing.T) {
	t.Parallel()

	registry, err := profile.Builtin()
	if err != nil {
		t.Fatalf("loading the built-in profiles: %v", err)
	}

	if err = profile.ValidateWithReserved(registry, profile.Reserved()); err != nil {
		t.Errorf("the committed profiles use a retired key: %v", err)
	}
}

func TestLoadRefusesAFieldTheFormatDoesNotDefine(t *testing.T) {
	t.Parallel()

	_, err := profile.Load([]byte(`{"schema":1,"profiles":[],"extra":true}`))
	if err == nil {
		t.Error("a profile file carrying an undefined field loaded without complaint")
	}
}
