package resource_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// installedCameraModule is the overlay an install writes for a module that
// offers one camera type, buddy3d.
func installedCameraModule() string {
	return `{
  "version": 1,
  "instances": [
    {"kind": "sink", "name": "cams", "type": "com.github.aux.acme.cams/cams", "runtime": "service",
     "enabled": true, "required": false,
     "service": {"path": "/opt/pcl/plugins", "checksum": "sha256:` + strings.Repeat("a", 64) + `",
                 "contract_major": 1},
     "module": {"version": "0.2.0", "camera_types": [{"name": "buddy3d", "title": "Buddy3D camera"}]}}
  ]
}
`
}

func TestTheOfferedTypesNameTheirModule(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.writeOverlayByHand(installedCameraModule())
	require.NoError(t, h.store.Reload(t.Context()))

	offered := resource.ModuleCameraTypes(h.store.Document().Instances)
	got, ok := offered["com.github.aux.acme.cams/buddy3d"]
	require.True(t, ok, "offered %v", offered)
	require.Equal(t, "Buddy3D camera", got.Title)
	require.Equal(t, "com.github.aux.acme.cams", got.Namespace)
	require.Equal(t, mustRef(t, ref.KindSink, "cams"), got.Module)

	require.Equal(t, resource.GenericCameraType,
		resource.DriverType(ref.KindCamera, "com.github.aux.acme.cams/buddy3d", offered))
	require.Equal(t, "push", resource.DriverType(ref.KindCamera, "push", offered))
	require.Equal(t, "com.github.aux.acme.cams/buddy3d",
		resource.DriverType(ref.KindSink, "com.github.aux.acme.cams/buddy3d", offered))
}
