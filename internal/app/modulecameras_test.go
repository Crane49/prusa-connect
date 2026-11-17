package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/builtin"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/platform/devicestore"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/cameraapi"
)

// framingModule is the instance the cases here hand frames as.
const framingModule = "sink:buddy3d"

// A module's own frame is held to its ceiling and then to the graph, and one
// inside both is the frame the snapshot path serves. Both directions of the
// same rule, on the real camera plane and the real graph.
func TestAModuleHandsAFrameOnlyToACameraItWasGranted(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	media := pushPlaneOn(t, graph, pushedCameraConfig(pushedCamera, boundPrinter))

	require.NoError(t, graph.UpsertDevice(t.Context(), boundPrinter, "Bound"))
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, boundPrinter, authz.RelOperator, vera)))
	requirePushable(t, media)

	params := cameraapi.AcceptParams{Camera: pushedCamera, Size: int64(len(jpeg(0x01)))}

	// Outside the ceiling: a grant on the camera does not help.
	outside := moduleCamera{cameras: media, standing: moduleStanding{
		principal: framingModule, ceiling: []authz.Action{authz.ActionObserve},
	}}
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, pushedCamera, authz.RelOperator, framingModule)))
	require.ErrorIs(t, outside.Admit(t.Context(), params), cameraapi.ErrDenied)
	require.ErrorIs(t, outside.Accept(t.Context(), params, jpeg(0x01)), cameraapi.ErrDenied)

	// Inside the ceiling and granted: served.
	inside := moduleCamera{cameras: media, standing: moduleStanding{
		principal: framingModule, ceiling: []authz.Action{authz.ActionOperate},
	}}
	require.Eventually(t, func() bool {
		return inside.Admit(t.Context(), params) == nil && inside.Accept(t.Context(), params, jpeg(0x02)) == nil
	}, settle, 20*time.Millisecond, "a granted module could not hand its camera a frame")

	shot, err := media.Snapshot(t.Context(), vera, pushedCamera)
	require.NoError(t, err)
	require.Equal(t, jpeg(0x02), shot.Bytes, "the bytes served are not the bytes the module sent")

	// Bytes that are not a JPEG are refused before they reach the camera.
	require.ErrorIs(t, inside.Accept(t.Context(), params, []byte("GIF89a....")), cameraapi.ErrInvalidSnapshot)
}

// A module inside its ceiling with no grant on the camera is told the camera
// is not there, the answer the HTTP route gives a caller who cannot see it,
// and a name nothing declares reads the same.
func TestAModuleWithNoGrantReachesNoCamera(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	media := pushPlaneOn(t, graph, pushedCameraConfig(pushedCamera, boundPrinter))

	require.NoError(t, graph.UpsertDevice(t.Context(), boundPrinter, "Bound"))
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, boundPrinter, authz.RelOperator, vera)))
	requirePushable(t, media)

	module := moduleCamera{cameras: media, standing: moduleStanding{
		principal: framingModule, ceiling: []authz.Action{authz.ActionOperate},
	}}

	for _, name := range []string{pushedCamera, "nowhere"} {
		// Admit answers before a byte is read, and as Accept does.
		admitted := module.Admit(t.Context(), cameraapi.AcceptParams{Camera: name, Size: 4})
		require.ErrorIs(t, admitted, cameraapi.ErrNoCamera, name)
		require.NotContains(t, admitted.Error(), name, "the refusal names the camera")

		err := module.Accept(t.Context(), cameraapi.AcceptParams{Camera: name, Size: 4}, jpeg(0x01))
		require.ErrorIs(t, err, cameraapi.ErrNoCamera, name)
		require.NotContains(t, err.Error(), name, "the refusal names the camera")
	}
}

// moduleCameraOverlay is the overlay an install writes for a module offering
// the camera type buddy3d.
func moduleCameraOverlay() string {
	return `{
  "version": 1,
  "instances": [
    {"kind": "sink", "name": "buddy3d", "type": "com.github.aux.acme.cams/cams", "runtime": "service",
     "enabled": true, "required": false,
     "service": {"path": "/nonexistent", "checksum": "sha256:` + strings.Repeat("a", 64) + `",
                 "contract_major": 1},
     "module": {"version": "0.2.0", "camera_types": [{"name": "buddy3d", "title": "Buddy3D camera"}]}}
  ]
}
`
}

// modulePlaneOn is pushPlaneOn with the module that offers the camera's type
// installed, and the camera added once it is offered.
func modulePlaneOn(t *testing.T, store *authz.Store, camera, printer string) *cameras {
	t.Helper()

	dir := t.TempDir()

	types, err := builtin.Registry()
	require.NoError(t, err)

	host, err := resourcehost.New(resourcehost.Config{Registry: types, HostKeys: builtin.HostKeys, Logger: discard()})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	stopped := make(chan error, 1)

	go func() { stopped <- host.Run(ctx) }()

	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-stopped)
	})

	path := filepath.Join(dir, "config.yaml")
	write(t, path, lines("core:", "  log:", "    level: error"))
	write(t, filepath.Join(dir, "config.d", "resources.json"), moduleCameraOverlay())

	held, err := resource.New(resource.Config{
		Paths:  resource.Paths{File: path, Overlay: filepath.Join(dir, "config.d", "resources.json")},
		Plane:  host,
		Graph:  store,
		Logger: discard(),
	})
	require.NoError(t, err)

	require.NoError(t, held.Reload(t.Context()))

	addCameras(t, store, host, devicestore.Camera{
		ID: camera, Type: "com.github.aux.acme.cams/buddy3d", Config: json.RawMessage(`{"printer":"` + printer + `"}`),
	})

	return newCameras(store, host, &countingRelay{}, discard())
}

// ADR-20261106-km2: a camera of a type a module offers runs on the core's
// generic driver, carries its type in the listing, is listed among the camera
// types, and its module may hand it a frame with no grant anybody wrote.
func TestACameraOfAModulesTypeIsFedByThatModule(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	media := modulePlaneOn(t, graph, pushedCamera, boundPrinter)

	require.NoError(t, graph.UpsertDevice(t.Context(), boundPrinter, "Bound"))
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, boundPrinter, authz.RelViewer, vera)))
	requirePushable(t, media)

	listed, err := media.Cameras(t.Context(), vera)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, "com.github.aux.acme.cams/buddy3d", listed[0].Type)
	require.True(t, listed[0].Snapshot)
	require.False(t, listed[0].Stream, "a camera of a module's type declared a stream nobody asked for")

	types, err := media.CameraTypes(t.Context(), vera)
	require.NoError(t, err)
	require.Contains(t, types, httpapi.CameraType{
		Type: "com.github.aux.acme.cams/buddy3d", Title: "Buddy3D camera", Module: "com.github.aux.acme.cams",
	})

	module := moduleCamera{cameras: media, standing: moduleStanding{
		principal: framingModule, ceiling: []authz.Action{authz.ActionOperate},
	}}
	require.Eventually(t, func() bool {
		return module.Accept(t.Context(), cameraapi.AcceptParams{Camera: pushedCamera, Size: 7}, jpeg(0x04)) == nil
	}, settle, 20*time.Millisecond, "the module that offers the camera's type could not feed it")

	shot, err := media.Snapshot(t.Context(), vera, pushedCamera)
	require.NoError(t, err)
	require.Equal(t, jpeg(0x04), shot.Bytes)

	// The camera's type decides, so no grant is left behind for a type change
	// to forget, and another module is not its owner.
	other := moduleCamera{cameras: media, standing: moduleStanding{
		principal: "sink:other", ceiling: []authz.Action{authz.ActionOperate},
	}}
	err = other.Accept(t.Context(), cameraapi.AcceptParams{Camera: pushedCamera, Size: 7}, jpeg(0x05))
	require.ErrorIs(t, err, cameraapi.ErrNoCamera)
}
