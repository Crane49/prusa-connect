package schema_test

import (
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
)

// directions is what an Exposure says about itself.
type directions struct {
	Name string
	Up   bool
	Down bool
}

// TestExposureNamesItsDirections tests the name of each exposure and the
// directions it reports in.
func TestExposureNamesItsDirections(t *testing.T) {
	t.Parallel()
	cases := []struct {
		exposure schema.Exposure
		want     directions
	}{
		{schema.ExposureUpward, directions{"upward", true, false}},
		{schema.ExposureDownward, directions{"downward", false, true}},
		{schema.ExposureBoth, directions{"both", true, true}},
		{0, directions{"unknown", false, false}},
	}
	for _, c := range cases {
		got := directions{c.exposure.String(), c.exposure.Up(), c.exposure.Down()}
		if got != c.want {
			t.Errorf("exposure %d is %+v, want %+v", c.exposure, got, c.want)
		}
	}
}
