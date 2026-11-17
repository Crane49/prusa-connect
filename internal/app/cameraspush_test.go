package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/builtin"
	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/platform/devicestore"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// pushedCamera is the instance the cases here declare, of the type that takes
// frames rather than reading a device.
const pushedCamera = "gatehouse"

// pushedFrame is one still image a camera sends for itself.
func pushedFrame(mark byte) httpapi.CameraFrame {
	return httpapi.CameraFrame{
		Bytes:     jpeg(mark),
		MediaType: "image/jpeg",
		Taken:     time.Now().UTC(),
	}
}

// TestAPushedFrameIsServedByTheSnapshotPath is task 19 of the old backlog on the real
// supervision plane and the real authorization graph.
//
// A camera nothing here can dial sends its own picture, and the two read paths
// then serve it exactly as they serve a frame this server fetched. The graph
// decides it: a viewer of the printer may watch the camera and may not decide
// what it shows.
func TestAPushedFrameIsServedByTheSnapshotPath(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	media := pushPlaneOn(t, graph, pushedCameraConfig(pushedCamera, boundPrinter))

	require.NoError(t, graph.UpsertDevice(t.Context(), boundPrinter, "Bound"))
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, boundPrinter, authz.RelViewer, vera)))

	// Until the camera is supervised the refusal below would be an absent
	// instance rather than the decision the graph took.
	requirePushable(t, media)

	err := media.Push(t.Context(), vera, pushedCamera, pushedFrame(0x01))
	require.Error(t, err, "a viewer of the printer decided what its camera shows")

	var refusal *fault.Fault
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, authz.CodeAccessDenied(), refusal.Code())

	shot, err := media.Snapshot(t.Context(), vera, pushedCamera)
	require.NoError(t, err)
	require.Empty(t, shot.Bytes, "the refused frame was stored anyway")

	// The other direction. Operator on the printer reaches the camera bound to
	// it, and nothing else moved, so what changed is the grant this path reads.
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, boundPrinter, authz.RelOperator, vera)))

	require.Eventually(t, func() bool {
		return media.Push(t.Context(), vera, pushedCamera, pushedFrame(0x02)) == nil
	}, settle, 20*time.Millisecond, "an operator of the printer still may not send its camera a frame")

	shot, err = media.Snapshot(t.Context(), vera, pushedCamera)
	require.NoError(t, err)
	require.Equal(t, jpeg(0x02), shot.Bytes, "the bytes served are not the bytes the camera sent")
	require.Equal(t, "image/jpeg", shot.MediaType)

	// The last frame wins, because a still image nobody has asked for yet is
	// worth exactly one frame.
	require.NoError(t, media.Push(t.Context(), vera, pushedCamera, pushedFrame(0x03)))

	shot, err = media.Snapshot(t.Context(), vera, pushedCamera)
	require.NoError(t, err)
	require.Equal(t, jpeg(0x03), shot.Bytes)
}

// TestACameraThatReadsADeviceTakesNoPushedFrame keeps the push path's refusals
// the refusals of the rest of the media surface. A driver that fetches has
// nowhere to put a frame somebody sent it, and storing one would make its next
// snapshot a lie.
func TestACameraThatReadsADeviceTakesNoPushedFrame(t *testing.T) {
	t.Parallel()

	device := newCameraDevice(t)
	graph := newGraph(t)
	media := pushPlaneOn(t, graph, readingCameraConfig(pushedCamera, boundPrinter, device))

	require.NoError(t, graph.UpsertDevice(t.Context(), boundPrinter, "Bound"))
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, boundPrinter, authz.RelOperator, vera)))

	requirePushable(t, media)

	require.EqualError(t,
		media.Push(t.Context(), vera, pushedCamera, pushedFrame(0x01)),
		httpapi.CameraNotFound(pushedCamera).Error(),
		"a camera that reads a device answered a push with anything but the surface's one refusal")
}

// requirePushable waits until the declared camera is serving, by reading it as
// the caller who holds a grant on its printer. A declared instance that has not
// started yet refuses every media call, so without this the refusals below
// would be a camera that is not up rather than a decision the graph took.
func requirePushable(t *testing.T, media *cameras) {
	t.Helper()

	require.Eventually(t, func() bool {
		_, err := media.Snapshot(t.Context(), vera, pushedCamera)

		return err == nil
	}, settle, 20*time.Millisecond, "the declared camera never started serving")
}

// pushedCameraConfig is one camera of the type that takes frames.
func pushedCameraConfig(name, printer string) devicestore.Camera {
	return devicestore.Camera{ID: name, Type: "push", Config: json.RawMessage(`{"printer":"` + printer + `"}`)}
}

// readingCameraConfig is one camera of the type that fetches from a device,
// under the same name, so the two cases differ in the driver alone.
func readingCameraConfig(name, printer string, device *cameraDevice) devicestore.Camera {
	return devicestore.Camera{ID: name, Type: "mjpeg", Config: json.RawMessage(
		`{"printer":"` + printer + `","snapshot_url":"` + device.snapshotURL() + `"}`)}
}

// pushPlaneOn is the media plane over a real supervision plane running whatever
// the configuration text declares, reading the graph the case hands it.
//
// It is mediaPlaneOn with the camera as an argument. The cases here turn on
// which driver is behind the instance, which is exactly what that helper
// fixes.
func pushPlaneOn(t *testing.T, store *authz.Store, camera devicestore.Camera) *cameras {
	t.Helper()

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

	addCameras(t, store, host, camera)

	return newCameras(store, host, &countingRelay{}, discard())
}

// TestAStoredFrameIsAnnounced tells a page there is a new still, on the
// camera's own origin, so the page loads it then rather than on a clock.
func TestAStoredFrameIsAnnounced(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	media := pushPlaneOn(t, graph, pushedCameraConfig(pushedCamera, boundPrinter))

	require.NoError(t, graph.UpsertDevice(t.Context(), boundPrinter, "Bound"))
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, boundPrinter, authz.RelOperator, vera)))
	requirePushable(t, media)

	// The camera's own device row is in the organisation, so the owner sees
	// the origin the frame is announced on.
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, pushedCamera, authz.RelOrg, "org:"+bootstrapOrg)))

	hub, err := stream.New(stream.Options{
		Site: "local", Interval: stream.MinInterval, Authorizer: visibility{graph: graph}, Logger: discard(),
	})
	require.NoError(t, err)
	t.Cleanup(hub.Close)
	media.hub = hub

	sub, err := hub.Subscribe(context.WithoutCancel(t.Context()), stream.SubscribeRequest{
		Principal: "user:" + config.DefaultPrincipal, Encoding: stream.EncodingJSON, Interval: stream.MinInterval,
	})
	require.NoError(t, err)
	t.Cleanup(sub.Close)

	require.Eventually(t, func() bool {
		return media.Push(t.Context(), vera, pushedCamera, pushedFrame(0x01)) == nil
	}, settle, 20*time.Millisecond, "an operator of the printer may not send its camera a frame")

	deadline := time.After(2 * time.Second)

	for {
		select {
		case batch := <-sub.Batches():
			// A frame announcement is telemetry, so it rides in that lane.
			for _, frame := range batch.Telemetry {
				if frame.Type != event.TypeCameraFrameReceived {
					continue
				}

				var envelope event.Envelope
				require.NoError(t, json.Unmarshal(frame.Bytes, &envelope))
				require.Equal(t, "device:"+pushedCamera, envelope.Subject)

				return
			}
		case <-deadline:
			t.Fatal("a stored frame was not announced")
		}
	}
}
