//go:build noothermodels

package profile_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printer/profile"
)

// The printer build loads the profile of one model and no other. `make
// test-unit` writes that model's file before it runs this test.
func TestThePrinterBuildLoadsOneProfile(t *testing.T) {
	t.Parallel()

	registry, err := profile.Builtin()
	require.NoError(t, err)

	models := registry.Models()
	require.Len(t, models, 1, "the printer build loaded %v", models)

	matched, ok := registry.Canonical(models[0])
	require.True(t, ok)
	require.Equal(t, models[0], matched)

	for _, bound := range registry.Bounds {
		require.Equal(t, models, bound.Models, "a bound row names another model")
	}
}
