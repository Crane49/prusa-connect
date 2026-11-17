package xbuddy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildVolumePatchesNameTheModelsVolume(t *testing.T) {
	t.Parallel()

	got := written(buildVolumePatches("coreone"))
	require.Equal(t, map[string]string{
		"/build_volume/x": "250", "/build_volume/y": "220", "/build_volume/z": "270",
	}, got, "the model is matched without case")

	require.Equal(t, map[string]string{
		"/build_volume/x": "250", "/build_volume/y": "205.5", "/build_volume/z": "275",
	}, written(buildVolumePatches("CoreOne_INDX")), "an INDX variant has a bed of its own")

	require.Empty(t, buildVolumePatches("MK2.5"), "a model with no figures reports nothing")
}
