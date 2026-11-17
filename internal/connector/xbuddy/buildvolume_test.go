package xbuddy_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/connector/xbuddy"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
)

// TestEveryBuildVolumeNamesAProfiledModel holds the table to the models this
// build reaches, so a row cannot name a model no printer reports.
func TestEveryBuildVolumeNamesAProfiledModel(t *testing.T) {
	t.Parallel()

	registry, err := profile.Builtin()
	require.NoError(t, err)

	models := registry.Models()
	for _, model := range xbuddy.BuildVolumeModels() {
		require.True(t, slices.ContainsFunc(models, func(m string) bool { return m == model }),
			"%s has a volume and no profile", model)
	}
}
