package resource_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/resource"
)

// TestTheFilesCamerasAreReadForTheMove is the upgrade's read half: both
// files' cameras are answered, the operator's wins a name both declare, and
// neither is a declaration any more.
func TestTheFilesCamerasAreReadForTheMove(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.writeFile(`
resources:
  camera:
    door:
      type: mjpeg
      config:
        printer: p1
`)
	h.writeOverlayByHand(`{"version": 1,
  "identities": {"camera/door": {"uid": "01JC8Z00000000000000000001", "generation": 3, "digest": "x"}},
  "instances": [
    {"kind": "camera", "name": "door", "type": "push", "enabled": true, "required": false},
    {"kind": "camera", "name": "bed", "type": "push", "enabled": true, "required": false,
     "config": {"printer": "p2"}}]}`)

	cameras, failures, err := resource.FileCameras(h.paths)
	require.NoError(t, err)
	require.Empty(t, failures)
	require.Len(t, cameras, 2)
	require.Equal(t, resource.FileCamera{
		Name: "bed", Type: "push", Config: []byte(`{"printer": "p2"}`), Source: resource.SourceOverlay,
	}, cameras[0])
	require.Equal(t, "mjpeg", cameras[1].Type, "the operator's file wins")
	require.JSONEq(t, `{"printer":"p1"}`, string(cameras[1].Config))

	require.NoError(t, h.store.Reload(t.Context()))
	require.Empty(t, h.store.Document().Instances)
	require.Empty(t, h.store.Document().Failures)

	require.NoError(t, resource.DropOverlayCameras(h.paths))
	require.NotContains(t, string(h.overlayBytes()), "camera")

	cameras, _, err = resource.FileCameras(h.paths)
	require.NoError(t, err)
	require.Len(t, cameras, 1, "the operator's file is the operator's to edit")
}
