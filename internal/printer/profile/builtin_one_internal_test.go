//go:build noothermodels

package profile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A profile left behind for another model stops the printer build from
// loading, and so does a binary that does not name its model.
func TestThePrinterBuildRefusesAnotherModelsProfile(t *testing.T) {
	t.Parallel()

	registry, err := parsed()
	require.NoError(t, err)

	model := registry.Models()[0]

	require.NoError(t, oneModel(registry, model))
	require.ErrorIs(t, oneModel(registry, ""), ErrOtherModel)
	require.ErrorIs(t, oneModel(registry, "a model this file does not hold"), ErrOtherModel)
	require.ErrorIs(t, oneModel(Registry{Schema: CurrentSchema}, model), ErrNotOneModel)
}
