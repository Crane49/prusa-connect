package app

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/cameraapi"
)

// cameraName is the instance every case here declares.
const cameraName = "workshop"

// jpeg is the smallest byte sequence that starts with a JPEG marker. Nothing
// here decodes one; the marker is what the media path checks before it serves
// bytes a device sent.
func jpeg(mark byte) []byte { return []byte{0xFF, 0xD8, 0xFF, 0xE0, mark, 0xFF, 0xD9} }

// TestASnapshotReachesAGrantedViewerThroughTheWholeCore is the epic's first row
// end to end on the real composition root: the real registry, the real
// supervision plane, the real authorization graph, the shipping camera adapter
// and a printer dialling in over the frozen wire.
//
// The camera is declared after the printer arrives because that is the only
// order an operator can work in: a printer's id is minted when it dials in, and
// the camera names it.
func TestASnapshotReachesAGrantedViewerThroughTheWholeCore(t *testing.T) {
	t.Parallel()

	device := newCameraDevice(t)
	running := start(t, t.TempDir(), baseConfig())

	dialIn(t, running, "SIM-CAM-1")

	printer := onlyPrinter(t, running)
	declareCamera(t, running, device, printer.ID)
	requireCameraReady(t, running)

	listed := listedCameras(t, running)
	require.Len(t, listed, 1)
	require.Equal(t, cameraName, listed[0].Name)
	require.Equal(t, printer.ID, listed[0].DeviceID)
	require.Equal(t, []string{"jpeg", "mjpeg"}, listed[0].Formats)
	require.True(t, listed[0].Snapshot)
	require.True(t, listed[0].Stream)

	// The declaration says min_interval: 1h, and the driver's own cadence is
	// what the listing publishes, in milliseconds (ADR-20260826-c77 rule 10). A client
	// that asks faster than this reads the frame the camera already holds.
	require.Equal(t, int64(3_600_000), listed[0].MinIntervalMS)

	shot := running.raw(t, "/api/v1/cameras/"+cameraName+"/snapshot", nil)
	require.Equal(t, http.StatusOK, shot.status, string(shot.body))
	require.Equal(t, "image/jpeg", shot.header.Get("Content-Type"))
	require.Equal(t, jpeg(0x01), shot.body, "the bytes served are not the bytes the device sent")

	// The media plane moves bytes and never touches pixels, so
	// what the device sent is what a viewer receives, byte for byte.
	require.Equal(t, int64(1), device.fetches.Load())
}

// TestAViewerTheGraphGrantsNothingCannotLearnTheCameraExists is a design requirement,
// both halves. A caller who may not read a printer's telemetry may not see its
// camera, and the refusal does not tell them it is there.
//
// One camera is moved between two bindings and nothing else changes. Bound to a
// printer the graph holds no grant on it is invisible and refused; bound to the
// printer that dialled in it is listed and served.
func TestAViewerTheGraphGrantsNothingCannotLearnTheCameraExists(t *testing.T) {
	t.Parallel()

	device := newCameraDevice(t)
	running := start(t, t.TempDir(), baseConfig())

	dialIn(t, running, "SIM-CAM-2")

	printer := onlyPrinter(t, running)

	// ungranted is a well-formed printer id that no printer registered under and
	// that no tuple names, which is what a printer somebody else owns looks like
	// to this caller.
	const ungranted = "punheldprinter00000000000"

	declareCamera(t, running, device, ungranted)

	require.Empty(t, listedCameras(t, running),
		"a camera bound to a printer the caller holds no grant on was listed")

	for _, path := range []string{"/snapshot", "/stream"} {
		refused := running.raw(t, "/api/v1/cameras/"+cameraName+path, nil)
		require.Equal(t, http.StatusNotFound, refused.status,
			"%s answered %d, and telling a caller a camera exists is what a design requirement forbids",
			path, refused.status)
		require.NotContains(t, string(refused.body), device.server.URL,
			"the refusal quotes the camera's address")
	}

	require.Zero(t, device.fetches.Load(), "a refused caller reached the device")

	// The same camera, bound to the printer this caller may read. Nothing else
	// moved, so what changed is the grant the media path consults.
	redeclareCamera(t, running, device, printer.ID)
	requireCameraReady(t, running)

	require.Eventually(t, func() bool {
		listed := listedCameras(t, running)

		return len(listed) == 1 && listed[0].DeviceID == printer.ID
	}, settle, 50*time.Millisecond, "the camera never followed its binding onto a printer the caller may read")

	served := running.raw(t, "/api/v1/cameras/"+cameraName+"/snapshot", nil)
	require.Equal(t, http.StatusOK, served.status, string(served.body))
}

// TestAThumbnailGridIsConditionalGetsRatherThanTransfers is a design requirement's
// arithmetic, measured on the wire. A list view of four hundred printers must
// not open four hundred streams, and it must not transfer four hundred images
// per refresh either.
//
// Both halves are asserted: the client is told it may hold the frame, and a
// client that says what it holds is answered 304 with no body.
func TestAThumbnailGridIsConditionalGetsRatherThanTransfers(t *testing.T) {
	t.Parallel()

	device := newCameraDevice(t)
	running := start(t, t.TempDir(), baseConfig())

	dialIn(t, running, "SIM-CAM-3")

	printer := onlyPrinter(t, running)
	declareCamera(t, running, device, printer.ID)
	requireCameraReady(t, running)

	first := running.raw(t, "/api/v1/cameras/"+cameraName+"/snapshot", nil)
	require.Equal(t, http.StatusOK, first.status)

	stamp := first.header.Get("Last-Modified")
	require.NotEmpty(t, stamp, "no validator, so every grid refresh is a whole transfer")

	require.Contains(t, first.header.Get("Cache-Control"), "private",
		"a shared proxy may hold one caller's camera frame and serve it to another")
	require.Contains(t, first.header.Get("Cache-Control"), "max-age=")
	require.NotEmpty(t, first.header.Get("Expires"))

	// nosniff, because the bytes came from a device on somebody's network and a
	// browser that guessed a type of its own would run whatever it decided.
	require.Equal(t, "nosniff", first.header.Get("X-Content-Type-Options"))

	// Forty cells of a grid, each saying what it already has. None of them
	// transfers an image and none of them reaches the device.
	const cells = 40

	for range cells {
		again := running.raw(t, "/api/v1/cameras/"+cameraName+"/snapshot",
			http.Header{"If-Modified-Since": []string{stamp}})
		require.Equal(t, http.StatusNotModified, again.status)
		require.Empty(t, again.body, "a 304 carried a body")
	}

	require.Equal(t, int64(1), device.fetches.Load(),
		"%d grid cells cost %d device fetches", cells, device.fetches.Load())
}

// TestACameraWithNoFrameYetAnswersNoContent separates a camera that is starting
// from one that is broken. A grid cell that showed an error for the first ten
// seconds of every camera's life would be an error nobody can act on.
func TestACameraWithNoFrameYetAnswersNoContent(t *testing.T) {
	t.Parallel()

	device := newCameraDevice(t)
	device.setStatus(http.StatusNoContent)

	running := start(t, t.TempDir(), baseConfig())

	dialIn(t, running, "SIM-CAM-4")

	printer := onlyPrinter(t, running)
	declareCamera(t, running, device, printer.ID)
	requireCameraReady(t, running)

	empty := running.raw(t, "/api/v1/cameras/"+cameraName+"/snapshot", nil)
	require.Equal(t, http.StatusNoContent, empty.status)
	require.Empty(t, empty.body)
}

// TestACameraThatCannotBeReachedAnswersABadGateway separates the camera from
// the server. The instance is declared and the caller may see it; the device
// behind it refuses the connection.
//
// A 500 here is the wrong answer twice: it sends an operator reading this
// server's log for a failure on their own network, and a browser grid full of
// them reports the server as broken while one camera is unplugged.
func TestACameraThatCannotBeReachedAnswersABadGateway(t *testing.T) {
	t.Parallel()

	device := newCameraDevice(t)
	running := start(t, t.TempDir(), baseConfig())

	dialIn(t, running, "SIM-CAM-8")

	printer := onlyPrinter(t, running)
	declareCamera(t, running, device, printer.ID)
	requireCameraReady(t, running)

	// Nothing has fetched a frame yet, so the driver holds none to serve back
	// in place of the one it cannot fetch.
	require.Zero(t, device.fetches.Load())

	device.server.Close()

	refused := running.raw(t, "/api/v1/cameras/"+cameraName+"/snapshot", nil)
	require.Equal(t, http.StatusBadGateway, refused.status, string(refused.body))

	var problem struct {
		Code      string `json:"code"`
		Retryable bool   `json:"retryable"`
	}

	require.NoError(t, json.Unmarshal(refused.body, &problem))
	require.Equal(t, "core.resource/CAMERA_UNREACHABLE", problem.Code)
	require.True(t, problem.Retryable, "a camera that may come back was reported as final")
	require.NotContains(t, string(refused.body), device.server.URL,
		"the refusal quotes the camera's address")
}

// TestALiveViewRelaysTheCameraFramesUntouched is the epic's MJPEG row, on the
// wire. What the device framed is what the browser receives, reframed with the
// contract's own boundary and nothing else changed.
func TestALiveViewRelaysTheCameraFramesUntouched(t *testing.T) {
	t.Parallel()

	device := newCameraDevice(t)
	running := start(t, t.TempDir(), baseConfig())

	dialIn(t, running, "SIM-CAM-5")

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
	require.Equal(t, cameraapi.FormatMJPEG.MediaType(), response.Header.Get("Content-Type"))

	// nginx defaults to proxy_buffering on, which holds an endless body and
	// delivers nothing. This header is the fix that needs no operator action.
	require.Equal(t, "no", response.Header.Get("X-Accel-Buffering"))
	require.Equal(t, "nosniff", response.Header.Get("X-Content-Type-Options"))
	require.Equal(t, "no-store", response.Header.Get("Cache-Control"))

	reader := bufio.NewReader(response.Body)
	seen := make([]byte, 220)

	_, err = io.ReadFull(reader, seen)
	require.NoError(t, err)

	body := string(seen)
	require.True(t, strings.HasPrefix(body, "--"+cameraapi.MJPEGBoundary+"\r\n"), body[:40])
	require.Contains(t, body, "Content-Type: image/jpeg")
	require.NotContains(t, body, "devicewall", "the device's own boundary reached the browser")
}

// TestAViewerWhoLeavesEndsTheStreamAtTheDevice is what an abandoned live view
// costs. A browser tab closes without saying so, and a server that kept reading
// from the camera would hold one device connection per tab that was ever open.
func TestAViewerWhoLeavesEndsTheStreamAtTheDevice(t *testing.T) {
	t.Parallel()

	device := newCameraDevice(t)
	running := start(t, t.TempDir(), baseConfig())

	dialIn(t, running, "SIM-CAM-6")

	printer := onlyPrinter(t, running)
	declareCamera(t, running, device, printer.ID)
	requireCameraReady(t, running)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		"http://"+running.addr+"/api/v1/cameras/"+cameraName+"/stream", nil)
	require.NoError(t, err)

	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)

	// One frame read, then the viewer goes, which is what a closed tab does.
	buf := make([]byte, 64)
	_, err = response.Body.Read(buf)
	require.NoError(t, err)

	require.Equal(t, int64(1), device.streaming.Load())
	require.NoError(t, response.Body.Close())

	require.Eventually(t, func() bool {
		return device.streaming.Load() == 0
	}, settle, 50*time.Millisecond,
		"the viewer left and this server is still reading from the camera")
}

// TestACameraBoundToNoPrinterIsRefused is the one refusal there is no safe
// default for. A camera with no printer names no grant, so it would be either a
// camera nobody can ever see or a camera everybody can, and a design requirement
// cannot be answered for it at all.
func TestACameraBoundToNoPrinterIsRefused(t *testing.T) {
	t.Parallel()

	device := newCameraDevice(t)
	running := start(t, t.TempDir(), baseConfig())

	refused := running.put(t, "/api/v1/cameras/"+cameraName,
		`{"type":"mjpeg","config":{"snapshot_url":"`+device.snapshotURL()+`"}}`)

	require.Eventually(t, func() bool {
		return cameraState(t, running) != condition.StateReady().String()
	}, settle, 20*time.Millisecond,
		"a camera naming no printer reached ready, so its frames answer to no grant")

	require.NotEqual(t, condition.StateReady().String(), cameraState(t, running),
		refused.body)
}

// TestACameraIsAnnouncedOnTheSameEventPlaneAsEverythingElse holds the third of
// the four surfaces a resource has to reach. A camera's lifecycle rides the
// event bus as com.prusa.connect.resource.state.changed with a camera origin,
// which is the same frame a storage instance and a sink produce, so a badge in
// a user interface changes without a second mechanism.
func TestACameraIsAnnouncedOnTheSameEventPlaneAsEverythingElse(t *testing.T) {
	t.Parallel()

	device := newCameraDevice(t)
	running := start(t, t.TempDir(), baseConfig())

	frames := watchEvents(t, running)

	dialIn(t, running, "SIM-CAM-7")

	printer := onlyPrinter(t, running)
	declareCamera(t, running, device, printer.ID)
	requireCameraReady(t, running)

	require.Eventually(t, func() bool {
		for _, frame := range frames() {
			if strings.Contains(frame, "camera:"+cameraName) &&
				strings.Contains(frame, "resource.state.changed") {
				return true
			}
		}

		return false
	}, settle, 50*time.Millisecond, "no camera transition reached the event stream")
}

// cameraDevice is a camera on the network, standing in for whatever an operator
// points an instance at.
type cameraDevice struct {
	server *httptest.Server

	fetches   atomic.Int64
	streaming atomic.Int64

	mu     sync.Mutex
	status int
}

func newCameraDevice(t *testing.T) *cameraDevice {
	t.Helper()

	device := &cameraDevice{status: http.StatusOK}

	device.server = httptest.NewServer(http.HandlerFunc(device.serve))
	t.Cleanup(device.server.Close)

	return device
}

// serve routes on the path itself. A ServeMux would be a second router on this
// tree, which internal/transport/http/httpapi refuses wherever it appears.
func (d *cameraDevice) serve(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/snap.jpg":
		d.serveSnapshot(w, r)
	case "/stream.mjpg":
		d.serveStream(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (d *cameraDevice) serveSnapshot(w http.ResponseWriter, _ *http.Request) {
	d.fetches.Add(1)

	d.mu.Lock()
	status := d.status
	d.mu.Unlock()

	if status != http.StatusOK {
		w.WriteHeader(status)

		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
	_, _ = w.Write(jpeg(0x01))
}

func (d *cameraDevice) serveStream(w http.ResponseWriter, r *http.Request) {
	d.streaming.Add(1)
	defer d.streaming.Add(-1)

	w.Header().Set("Content-Type", `multipart/x-mixed-replace; boundary=devicewall`)
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)

	for i := range 10000 {
		frame := jpeg(byte(i))

		_, err := fmt.Fprintf(w, "--devicewall\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n%s\r\n",
			len(frame), frame)
		if err != nil {
			return
		}

		if flusher != nil {
			flusher.Flush()
		}

		select {
		case <-r.Context().Done():
			return
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func (d *cameraDevice) setStatus(status int) {
	d.mu.Lock()
	d.status = status
	d.mu.Unlock()
}

func (d *cameraDevice) snapshotURL() string { return d.server.URL + "/snap.jpg" }
func (d *cameraDevice) streamURL() string   { return d.server.URL + "/stream.mjpg" }

// baseConfig is a core that declares no resource. Every case here declares its
// camera through the resource API instead, because a camera names the printer
// it watches and a printer's id is minted when it dials in.
func baseConfig() string {
	return lines("core:", "  log:", "    level: error")
}

// declareCamera writes the instance through the published API, the way an
// operator would.
func declareCamera(t *testing.T, running *process, device *cameraDevice, printer string) {
	t.Helper()

	written := running.put(t, "/api/v1/cameras/"+cameraName, cameraSpec(device, printer))

	require.Equal(t, http.StatusCreated, written.status, written.body)
}

// redeclareCamera rewrites an instance that already exists, which is a replace
// rather than a create and therefore quotes the version it read.
func redeclareCamera(t *testing.T, running *process, device *cameraDevice, printer string) {
	t.Helper()

	version := currentVersion(t, running)

	written := running.send(t, http.MethodPut, "/api/v1/cameras/"+cameraName,
		cameraSpec(device, printer), http.Header{"If-Match": []string{version}})

	require.Equal(t, http.StatusOK, written.status, string(written.body))
}

// cameraSpec is the declaration every case here writes.
func cameraSpec(device *cameraDevice, printer string) string {
	return `{"type":"mjpeg","config":{` +
		`"printer":"` + printer + `",` +
		`"snapshot_url":"` + device.snapshotURL() + `",` +
		`"stream_url":"` + device.streamURL() + `",` +
		`"min_interval":"1h"}}`
}

func requireCameraReady(t *testing.T, running *process) {
	t.Helper()

	require.Eventually(t, func() bool {
		return cameraState(t, running) == condition.StateReady().String()
	}, settle, 20*time.Millisecond, "the declared camera never reported ready")
}

// cameraState is ready while the camera serves, which is what the listing
// says of it, and empty otherwise.
func cameraState(t *testing.T, running *process) string {
	t.Helper()

	for _, camera := range listedCameras(t, running) {
		if camera.Name == cameraName && camera.Serving {
			return condition.StateReady().String()
		}
	}

	return ""
}

// listedCameras reads the media listing through the published API, which is the same
// bytes a client reads.
func listedCameras(t *testing.T, running *process) []httpapi.Camera {
	t.Helper()

	read := running.get(t, "/api/v1/cameras")
	require.Equal(t, http.StatusOK, read.status, read.body)

	var listing struct {
		Items []httpapi.Camera `json:"items"`
	}

	require.NoError(t, json.Unmarshal([]byte(read.body), &listing))

	return listing.Items
}
