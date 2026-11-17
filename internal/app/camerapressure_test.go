package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// TestAViewerPastTheBulkheadWaitsAndThenGoesAway is what a camera costs when
// more people watch it than its bulkhead is wide.
//
// One camera admits supervisor.DefaultConcurrency streams at once. A viewer
// past that waits on the bulkhead rather than being refused, which is the
// supervision plane's behaviour for every kind and not something the media path
// chose. What this holds is the part that has to be true for it to be safe: the
// wait ends when the viewer goes, and the goroutine behind it ends with it.
//
// A viewer who never goes away is bounded by the server's own write timeout,
// and the queue behind one camera is an open row on the old backlog rather than a defect
// this case hides.
func TestAViewerPastTheBulkheadWaitsAndThenGoesAway(t *testing.T) {
	t.Parallel()

	device := newCameraDevice(t)
	running := start(t, t.TempDir(), baseConfig())

	dialIn(t, running, "SIM-CAM-8")

	printer := onlyPrinter(t, running)
	declareCamera(t, running, device, printer.ID)
	requireCameraReady(t, running)

	// One more viewer than the camera admits, so the last one is the one that
	// has to wait.
	viewers := supervisor.DefaultConcurrency + 1

	before := runtime.NumGoroutine()

	ctx, leave := context.WithCancel(context.WithoutCancel(t.Context()))

	var (
		wait     sync.WaitGroup
		mu       sync.Mutex
		admitted int
	)

	for range viewers {
		wait.Go(func() {
			request, err := http.NewRequestWithContext(ctx, http.MethodGet,
				"http://"+running.addr+"/api/v1/cameras/"+cameraName+"/stream", nil)
			if err != nil {
				return
			}

			response, err := http.DefaultClient.Do(request)
			if err != nil {
				return
			}

			defer func() { _ = response.Body.Close() }()

			if response.StatusCode == http.StatusOK {
				mu.Lock()
				admitted++
				mu.Unlock()
			}

			_, _ = io.Copy(io.Discard, response.Body)
		})
	}

	// The bulkhead is what decides how many of them reach the device at once.
	require.Eventually(t, func() bool {
		return device.streaming.Load() == int64(supervisor.DefaultConcurrency)
	}, settle, 20*time.Millisecond,
		"%d of %d viewers reached the camera, and the bulkhead is %d wide",
		device.streaming.Load(), viewers, supervisor.DefaultConcurrency)

	// Every viewer leaves, which is what a closed tab does.
	leave()

	finished := make(chan struct{})

	go func() {
		wait.Wait()
		close(finished)
	}()

	select {
	case <-finished:
	case <-time.After(settle):
		t.Fatal("a viewer that left is still waiting on the bulkhead")
	}

	require.Eventually(t, func() bool {
		return device.streaming.Load() == 0
	}, settle, 20*time.Millisecond,
		"the viewers left and this server is still reading from the camera")

	// The goroutines behind them end too. A viewer that leaves must cost
	// nothing, because a browser refreshing a live view leaves several times a
	// minute.
	require.Eventually(t, func() bool {
		return runtime.NumGoroutine() <= before+viewers
	}, settle, 50*time.Millisecond,
		"goroutines went from %d to %d after %d viewers came and went",
		before, runtime.NumGoroutine(), viewers)
}

// TestACameraWithdrawnMidStreamEndsEveryViewer is the camera that goes away
// while somebody is watching it.
//
// An operator removes the declaration, the supervision plane stops the
// instance, and the driver's Close ends the device connection. A viewer whose
// body never ended would sit on a stream nothing will ever write to again, and
// the goroutine relaying it would outlive the resource it was relaying.
func TestACameraWithdrawnMidStreamEndsEveryViewer(t *testing.T) {
	t.Parallel()

	device := newCameraDevice(t)
	running := start(t, t.TempDir(), baseConfig())

	dialIn(t, running, "SIM-CAM-9")

	printer := onlyPrinter(t, running)
	declareCamera(t, running, device, printer.ID)
	requireCameraReady(t, running)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		"http://"+running.addr+"/api/v1/cameras/"+cameraName+"/stream", nil)
	require.NoError(t, err)

	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)

	defer func() { _ = response.Body.Close() }()

	require.Equal(t, http.StatusOK, response.StatusCode)

	// One frame read, so the viewer is genuinely on the stream and not merely
	// holding a response nothing has written to.
	buf := make([]byte, 64)
	_, err = response.Body.Read(buf)
	require.NoError(t, err)

	require.Equal(t, int64(1), device.streaming.Load())

	withdrawn := running.send(t, http.MethodDelete,
		"/api/v1/cameras/"+cameraName, "", http.Header{"If-Match": []string{currentVersion(t, running)}})
	require.Equal(t, http.StatusNoContent, withdrawn.status, string(withdrawn.body))

	drained := make(chan error, 1)

	go func() {
		_, copyErr := io.Copy(io.Discard, response.Body)
		drained <- copyErr
	}()

	select {
	case <-drained:
	case <-time.After(settle):
		t.Fatal("the camera was withdrawn and a viewer is still on its stream")
	}

	require.Eventually(t, func() bool {
		return device.streaming.Load() == 0
	}, settle, 20*time.Millisecond,
		"the camera was withdrawn and this server is still reading from the device")

	// And the media path agrees the camera is gone, with the same refusal a
	// caller who may not see it gets.
	gone := running.raw(t, "/api/v1/cameras/"+cameraName+"/snapshot", nil)
	require.Equal(t, http.StatusNotFound, gone.status)
}

// currentVersion is the version an operator quotes to change or remove an
// instance.
func currentVersion(t *testing.T, running *process) string {
	t.Helper()

	read := running.get(t, "/api/v1/camera-settings")
	require.Equal(t, http.StatusOK, read.status, read.body)

	var listing struct {
		Items []httpapi.CameraSetting `json:"items"`
	}

	require.NoError(t, json.Unmarshal([]byte(read.body), &listing))

	for _, camera := range listing.Items {
		if camera.Name == cameraName {
			return `"` + camera.Version + `"`
		}
	}

	t.Fatalf("no camera %s in %s", cameraName, read.body)

	return ""
}
