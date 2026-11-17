package fleet

import (
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// TestTheMatrixNeverInventsASecondFirmwareOrdering is what its name says, and
// deliberately not more.
//
// Semver rule 11 is pinned in internal/printer/profile, where the code that
// implements it lives, and this gate adds no reach over it: both sides of the
// comparison below read the same call, so inverting the rule in
// internal/printer/profile/version.go moves both and leaves this green while
// four tests in that package go red. What it catches is a second ordering
// reappearing here, in compareVersions, which is the defect this package
// actually had: replacing compareVersions with strings.Compare turns it red on
// three of the six pairs below.
//
// The reach over the resolver's own rule is
// TestAPrereleaseNeverDisplacesTheShippingReleaseInTheFastSet, which names the
// firmware it expects rather than asking for it.
func TestTheMatrixNeverInventsASecondFirmwareOrdering(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		a, b string
	}{
		{name: "a candidate against its own release", a: "6.5.0-RC", b: "6.5.0"},
		{name: "a candidate against the release below it", a: "6.5.0-RC", b: "6.4.9"},
		{name: "the band floor against the release above it", a: "6.4.0-0", b: "6.4.0"},
		{name: "two candidates for one release", a: "6.5.0-RC1", b: "6.5.0-RC2"},
		{name: "a numeric candidate against an alphanumeric one", a: "6.5.0-1", b: "6.5.0-RC"},
		{name: "a release against a later minor", a: "6.9.0", b: "6.10.0"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			want := resolverOrder(testCase.a, testCase.b)

			if got := sign(compareVersions(testCase.a, testCase.b)); got != want {
				t.Errorf("the matrix orders %s against %s as %d, and the resolver orders it %d",
					testCase.a, testCase.b, got, want)
			}

			if got := sign(compareVersions(testCase.b, testCase.a)); got != -want {
				t.Errorf("the matrix orders %s against %s as %d, and the resolver orders it %d",
					testCase.b, testCase.a, got, -want)
			}
		})
	}
}

// resolverOrder is the ordering profile.Range applies, read out of the only
// exported call that applies it. Contains is inclusive at the floor, so
// Range{From: a}.Contains(b) says b is at or above a, and the two directions
// together give the sign.
func resolverOrder(a, b string) int {
	atOrAbove := profile.Range{From: a}.Contains(connectorapi.NewFirmwareLabel(b))
	atOrBelow := profile.Range{From: b}.Contains(connectorapi.NewFirmwareLabel(a))

	switch {
	case atOrAbove && atOrBelow:
		return 0
	case atOrAbove:
		return -1
	case atOrBelow:
		return 1
	}

	// Neither bound reads as a version, so the resolver puts the pair in no
	// range at all and there is no order to agree with.
	return 0
}

// TestEveryFirmwareInTheTableIsReadable keeps the release table out of
// compareVersions' text fallback.
//
// A label the resolver cannot read is in no band, so a cell built from one
// would run on the driver baseline while its name claimed a model and a
// firmware. It is also the only input for which the matrix orders by text
// rather than by the product's own rule.
func TestEveryFirmwareInTheTableIsReadable(t *testing.T) {
	t.Parallel()

	table := loadReleases(t)

	for model, published := range table.Models {
		for _, firmware := range published {
			if !atOrAbove(firmware, firmware) {
				t.Errorf("%s names firmware %q for %s and the resolver cannot read it as a version, so a "+
					"printer reporting it falls in no band", releasesFile, firmware, model)
			}
		}
	}
}

// TestAPrereleaseNeverDisplacesTheShippingReleaseInTheFastSet drives fastCell
// over the case the committed table cannot produce.
//
// The fast set is one cell per model at the newest firmware released for it,
// and the reason recorded on fastCell is that the newest release is what the
// field runs. A candidate for that release is not: it is a build made before it
// existed. Ordering a tag as text puts the candidate above the release, which
// would gate every commit on a firmware nobody runs and leave the shipping one
// to the nightly set.
//
// The candidate has to be named after the newest release for any of that to
// happen, and releases.json names 6.5.0-RC beside 6.5.7: 6.5.0 is below 6.5.7
// on any ordering, so the scan below passes against the exact implementation it
// was written to reject. The published lists here construct the case instead of
// asking the table to carry a candidate that never shipped.
func TestAPrereleaseNeverDisplacesTheShippingReleaseInTheFastSet(t *testing.T) {
	t.Parallel()

	band := profile.Range{From: "6.4.0-0"}

	cases := []struct {
		name      string
		published []string
		want      string
	}{
		{
			name:      "a candidate for the newest release",
			published: []string{"6.5.7-RC", "6.5.7"},
			want:      "6.5.7",
		},
		{
			name:      "a candidate written after the release it is for",
			published: []string{"6.5.7", "6.5.7-RC"},
			want:      "6.5.7",
		},
		{
			name:      "two candidates and the release they are for",
			published: []string{"6.5.7-RC1", "6.5.7-RC2", "6.5.7"},
			want:      "6.5.7",
		},
		{
			name:      "the band floor spelling, which is a candidate for the floor",
			published: []string{"6.4.0-0", "6.4.0"},
			want:      "6.4.0",
		},
		{
			name:      "the committed table's shape, where the candidate is for an older release",
			published: []string{"6.5.0-RC", "6.5.7"},
			want:      "6.5.7",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cell := fastCell("MK4", band, testCase.published)
			if cell.Firmware != testCase.want {
				t.Errorf("the fast set runs MK4 on %s out of %v, and %s is the shipping release. Every "+
					"commit would be gated on a build the field does not run",
					cell.Firmware, testCase.published, testCase.want)
			}

			if cell.Source != sourceReleased {
				t.Errorf("the cell reports source %q and every version here is a published release",
					cell.Source)
			}
		})
	}
}

// TestNoModelInTheTableIsGatedOnACandidate is the scan over the committed
// table. It says nothing about the ordering, which is what the constructed
// cases above are for, and everything about the data.
func TestNoModelInTheTableIsGatedOnACandidate(t *testing.T) {
	t.Parallel()

	table := loadReleases(t)
	tested := 0

	for _, cell := range matrix(t, setFast) {
		published := table.Models[cell.Model]
		if len(published) == 0 {
			continue
		}

		tested++

		if !slices.Contains(published, cell.Firmware) {
			t.Errorf("the fast set runs %s on firmware %s and %s does not name it as a release",
				cell.Model, cell.Firmware, releasesFile)

			continue
		}

		if strings.Contains(cell.Firmware, "-") {
			t.Errorf("the fast set runs %s on %s, which is a candidate for a release rather than one. "+
				"Every commit would then be gated on a build the field does not run", cell.Model, cell.Firmware)
		}
	}

	if tested == 0 {
		t.Fatalf("no model in %s names a released firmware, so this gate compared nothing", releasesFile)
	}

	t.Logf("observed: %d models with a published release, each gated on a shipping build", tested)
}

func sign(order int) int {
	switch {
	case order < 0:
		return -1
	case order > 0:
		return 1
	}

	return 0
}
