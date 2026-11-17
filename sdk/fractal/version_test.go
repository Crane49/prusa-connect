package fractal_test

import (
	"slices"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// TestVersionIsOne tests the Protocol Version section of fractal.md: the
// current version of the protocol is 1, and no other version is supported.
func TestVersionIsOne(t *testing.T) {
	t.Parallel()
	if fractal.Version != 1 {
		t.Fatalf("Version = %d", fractal.Version)
	}
	if !fractal.SupportedVersion(fractal.Version) {
		t.Fatal("the current version is not supported")
	}
	for _, v := range []uint{0, 2, 7, 1 << 20} {
		if fractal.SupportedVersion(v) {
			t.Errorf("version %d is supported", v)
		}
	}
	got := fractal.SupportedVersions()
	if !slices.Equal(got, []uint{1}) {
		t.Fatalf("SupportedVersions = %v", got)
	}
	got[0] = 99
	if !slices.Equal(fractal.SupportedVersions(), []uint{1}) {
		t.Fatal("a write to the slice SupportedVersions returned changed the versions the package supports")
	}
}

// TestTimestampConversion tests that a Timestamp is milliseconds since the unix
// epoch, in both directions, and that a time before the epoch converts to 0.
func TestTimestampConversion(t *testing.T) {
	t.Parallel()
	const ms = 1751400000000
	when := time.UnixMilli(ms).UTC()
	if got := fractal.TimestampOf(when); got != ms {
		t.Fatalf("TimestampOf = %d, want %d", got, ms)
	}
	if got := fractal.Timestamp(ms).Time(); !got.Equal(when) {
		t.Fatalf("Time = %v, want %v", got, when)
	}
	if got := fractal.TimestampOf(time.UnixMilli(-1)); got != 0 {
		t.Fatalf("a time before the epoch gave %d", got)
	}
	if got := fractal.Timestamp(ms).String(); got != "2025-07-01T20:00:00.000Z" {
		t.Fatalf("String = %q", got)
	}
}
