package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"maps"
	"mime"
	"mime/multipart"
	"net/http"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/cameraapi"
	"github.com/prusa3d/prusa-connect/tools/camerasim"
)

// The camera lane. It hangs a simulated camera on a simulated printer through
// the resource API, and then reads the two media paths the way a browser reads
// them: one still image, and a multipart stream.
//
// The frames carry their own sequence number, drawn into the image as a row of
// black and white blocks, so a case can assert that the stream is moving
// without decoding text or trusting a wall clock. camerasim.ReadCounter reads
// it back out of a decoded frame, which makes "the picture changed" a
// measurement rather than a byte comparison that a re-encode could flatter.

// cameraStreamFrames is how many frames the stream case reads. Ten is enough to
// see the counter advance several times over and short enough that a stalled
// stream fails inside the budget rather than at the test timeout.
const cameraStreamFrames = 10

// cameraFPS is what the simulated device renders at. It is fast so the lane is
// quick, and it is nothing like a real camera's cadence: what is under test is
// the relay, and the relay does not know what a frame rate is.
const cameraFPS = 25

// cameraSnapshotCadence is what the driver is told about the device's own
// cadence. The shipped default is ten seconds, which would serve every case
// here the same held frame.
const cameraSnapshotCadence = "50ms"

// TestACameraOnAPrinterServesASnapshotAndAMovingStream is item 3 of the media
// plane, end to end over real sockets: the binary, the resource API, the mjpeg
// driver, a device on the other end of a TCP connection, and the two paths a
// browser fetches.
//
// The subtests are steps over one camera and one device, and the stream case
// holds a live view open while it reads. Run in parallel they would be three
// viewers of one simulated camera racing each other's frame counters, which is
// a different question from the one being asked here.
//
//nolint:tparallel // the subtests are steps of one sequence, not independent cases
func TestACameraOnAPrinterServesASnapshotAndAMovingStream(t *testing.T) {
	t.Parallel()

	srv := startServer(t)
	srv.awaitReady(t)

	_, printer := srv.attachPrinter(t, "MK4S")
	device := startCameraDevice(t, printer.Serial)

	const camera = "e2e-cam"

	srv.declareCamera(t, camera, printer.ID, device, nil)
	srv.awaitCameraListed(t, camera, nil)

	t.Run("the listing names the camera against its printer", func(t *testing.T) {
		listed := srv.cameras(t, "?device="+printer.ID, nil)
		if len(listed) != 1 {
			t.Fatalf("the printer's cameras are %v, want exactly one", listed)
		}

		if listed[0].DeviceID != printer.ID {
			t.Fatalf("the camera names device %q, want %q", listed[0].DeviceID, printer.ID)
		}

		if !listed[0].Snapshot || !listed[0].Stream {
			t.Fatalf("the camera declares snapshot=%v stream=%v, want both",
				listed[0].Snapshot, listed[0].Stream)
		}

		// A device the caller can see and that has no camera answers an empty
		// list, so the filter is a filter and not an accident of ordering.
		if other := srv.cameras(t, "?device=pnothinghere000000000000", nil); len(other) != 0 {
			t.Fatalf("a device with no camera listed %d cameras", len(other))
		}
	})

	t.Run("the snapshot is a JPEG the device produced", func(t *testing.T) {
		shot := srv.get(t, "/api/v1/cameras/"+camera+"/snapshot")
		if shot.status != http.StatusOK {
			t.Fatalf("the snapshot answered %d: %s", shot.status, shot.body)
		}

		if got := shot.header.Get("Content-Type"); got != "image/jpeg" {
			t.Fatalf("the snapshot is %q, want image/jpeg", got)
		}

		frame, err := jpeg.Decode(bytes.NewReader(shot.body))
		if err != nil {
			t.Fatalf("the snapshot does not decode as a JPEG: %v", err)
		}

		if _, err = camerasim.ReadCounter(frame); err != nil {
			t.Fatalf("the snapshot carries no frame counter: %v", err)
		}
	})

	t.Run("the stream delivers frames whose counter advances", func(t *testing.T) {
		counters := srv.readStream(t, camera, cameraStreamFrames, nil)

		for i := 1; i < len(counters); i++ {
			if counters[i] <= counters[i-1] {
				t.Fatalf("frame %d carries counter %d and frame %d carries %d, so the stream is not moving: %v",
					i-1, counters[i-1], i, counters[i], counters)
			}
		}
	})
}

// TestAPersonWhoMayNotViewThePrinterIsRefusedItsCamera is item 5's permission
// case and a design requirement read over the wire.
//
// It runs three states in one sequence, because each stands on the grant the
// one before it wrote. The order matters more than it looks: the viewer grant
// on the printer is written **before** the camera is declared, so the attach
// copies it onto the camera's own row. That is what makes the last step a real
// test, the person then reaches the camera by a path that does not run through
// the printer, and a block that reached only the printer would leave them
// watching it. With the grant written after the attach nothing is copied, the
// only path is the parent edge, and the block would appear to work for a reason
// that has nothing to do with the fix.
//
//nolint:tparallel // the subtests are steps of one sequence, not independent cases
func TestAPersonWhoMayNotViewThePrinterIsRefusedItsCamera(t *testing.T) {
	t.Parallel()

	srv := startServerCore(t, localAuth, "")
	srv.awaitReady(t)

	admin := srv.setupFirstAdmin(t)
	printer := srv.attachAs(t, admin, "MK4S", "LIVE-CAM-1")
	device := startCameraDevice(t, "LIVE-CAM-1")

	const camera = "e2e-guarded-cam"

	bob := srv.createPerson(t, admin, "livetest-camera-bob", "")

	t.Run("holding nothing, a camera on the printer is not there", func(t *testing.T) {
		srv.declareCamera(t, camera+"-unseen", printer, device, admin.headers())
		srv.awaitCameraListed(t, camera+"-unseen", admin.headers())

		srv.requireCameraUnreachable(t, camera+"-unseen", bob)
	})

	t.Run("a camera attached under a grant takes that grant onto its own row", func(t *testing.T) {
		srv.grant(t, admin, printer, "viewer", bob.subject)

		srv.declareCamera(t, camera, printer, device, admin.headers())
		srv.awaitCameraListed(t, camera, admin.headers())

		eventually(t, "the granted person sees the camera", func() bool {
			return len(srv.cameras(t, "?device="+printer, bob.headers())) == 2
		})

		// The copy is on the camera itself, so it is readable as a grant on the
		// camera and not only as something the printer confers.
		held := srv.as(t, admin, http.MethodGet, "/api/v1/grants/device/"+camera, "")
		if held.status != http.StatusOK {
			t.Fatalf("reading the camera's grants answered %d: %s", held.status, held.body)
		}

		if !bytes.Contains(held.body, []byte(bob.subject)) {
			t.Fatalf("the camera carries no grant of its own for %s: %s", bob.subject, held.body)
		}

		shot := srv.as(t, bob, http.MethodGet, "/api/v1/cameras/"+camera+"/snapshot", "")
		if shot.status != http.StatusOK {
			t.Fatalf("the granted person's snapshot answered %d, want 200: %s", shot.status, shot.body)
		}
	})

	t.Run("a block on the printer takes its cameras away too", func(t *testing.T) {
		srv.grant(t, admin, printer, "blocked", bob.subject)

		eventually(t, "the blocked person no longer sees the camera", func() bool {
			return len(srv.cameras(t, "", bob.headers())) == 0
		})

		srv.requireCameraUnreachable(t, camera, bob)
	})
}

// requireCameraUnreachable is the whole refusal: the camera is absent from the
// listing and every media path answers 404, which is the same answer a name
// nothing supervises gets.
func (s *server) requireCameraUnreachable(t *testing.T, camera string, who person) {
	t.Helper()

	if listed := s.cameras(t, "", who.headers()); len(listed) != 0 {
		t.Fatalf("the listing named %d cameras, want none: %v", len(listed), listed)
	}

	for _, path := range []string{"/snapshot", "/stream"} {
		got := s.as(t, who, http.MethodGet, "/api/v1/cameras/"+camera+path, "")
		if got.status != http.StatusNotFound {
			t.Fatalf("%s answered %d, want 404: %s", path, got.status, got.body)
		}
	}
}

// cameraView is one row of the camera listing, as much of it as this lane reads.
type cameraView struct {
	Name     string `json:"name"`
	DeviceID string `json:"device_id"`
	Serving  bool   `json:"serving"`
	Snapshot bool   `json:"snapshot"`
	Stream   bool   `json:"stream"`
}

// cameras reads the camera listing, with whatever query and headers the caller
// wants on it.
func (s *server) cameras(t *testing.T, query string, headers map[string]string) []cameraView {
	t.Helper()

	listed := s.do(t, http.MethodGet, "/api/v1/cameras"+query, "", headers)
	if listed.status != http.StatusOK {
		t.Fatalf("/api/v1/cameras answered %d: %s", listed.status, listed.body)
	}

	var page struct {
		Items []cameraView `json:"items"`
	}

	if err := json.Unmarshal(listed.body, &page); err != nil {
		t.Fatalf("decode the camera listing %s: %v", listed.body, err)
	}

	return page.Items
}

// declareCamera adds one camera through the camera API, which is what an
// operator does when they hang a camera on a printer that is already
// running.
//
// If-None-Match: * is what makes it a create. Every write to this surface is
// fenced with a token, and one carrying none is refused rather than allowed to
// overwrite whatever is under the name.
func (s *server) declareCamera(
	t *testing.T, name, printer string, device *camerasim.Server, headers map[string]string,
) {
	t.Helper()

	body := fmt.Sprintf(
		`{"type":"mjpeg","config":{`+
			`"printer":%q,"snapshot_url":%q,"stream_url":%q,"min_interval":%q}}`,
		printer, device.SnapshotURL(), device.StreamURL(), cameraSnapshotCadence)

	with := map[string]string{"If-None-Match": "*"}
	maps.Copy(with, headers)

	written := s.do(t, http.MethodPut, "/api/v1/cameras/"+name, body, with)
	if written.status != http.StatusCreated {
		t.Fatalf("declaring the camera answered %d, want 201: %s", written.status, written.body)
	}
}

// awaitCameraListed waits until the supervision plane has the camera running
// and the media plane will answer for it.
func (s *server) awaitCameraListed(t *testing.T, name string, headers map[string]string) {
	t.Helper()

	eventually(t, "the declared camera reached the listing and is serving", func() bool {
		for _, listed := range s.cameras(t, "", headers) {
			if listed.Name == name && listed.Serving {
				return true
			}
		}

		return false
	})
}

// readStream reads the first want frames off one camera's live view and returns
// the counter drawn into each of them.
//
// The multipart boundary is the one the contract fixes rather than one read off
// the response, on purpose: a driver that framed with a boundary of its own
// would produce a stream no host could describe, and this lane is one of the
// places that would go unnoticed.
func (s *server) readStream(t *testing.T, camera string, want int, headers map[string]string) []uint64 {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), streamBudget)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		s.baseURL+"/api/v1/cameras/"+camera+"/stream", nil)
	if err != nil {
		t.Fatalf("build the stream request: %v", err)
	}

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := (&http.Client{Timeout: streamBudget}).Do(req)
	if err != nil {
		t.Fatalf("open the stream: %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the stream answered %d", resp.StatusCode)
	}

	kind, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || kind != "multipart/x-mixed-replace" {
		t.Fatalf("the stream is %q, want multipart/x-mixed-replace: %v", resp.Header.Get("Content-Type"), err)
	}

	if params["boundary"] != cameraapi.MJPEGBoundary {
		t.Fatalf("the stream is framed with boundary %q, want %q",
			params["boundary"], cameraapi.MJPEGBoundary)
	}

	return readFrameCounters(t, bufio.NewReader(resp.Body), params["boundary"], want)
}

// streamBudget bounds the whole read of a live view. It is generous because the
// binary is starting, a printer is on the wire and a camera is rendering, and
// it is finite because a stream that stops delivering must fail here rather
// than at the package timeout.
const streamBudget = 60 * time.Second

// readFrameCounters decodes want parts off a multipart stream and reads the
// counter out of each one.
func readFrameCounters(t *testing.T, body *bufio.Reader, boundary string, want int) []uint64 {
	t.Helper()

	reader := multipart.NewReader(body, boundary)
	counters := make([]uint64, 0, want)

	for len(counters) < want {
		part, err := reader.NextPart()
		if err != nil {
			t.Fatalf("read frame %d of %d: %v", len(counters)+1, want, err)
		}

		frame, err := jpeg.Decode(part)
		if err != nil {
			t.Fatalf("frame %d does not decode as a JPEG: %v", len(counters)+1, err)
		}

		counter, err := camerasim.ReadCounter(frame)
		if err != nil {
			t.Fatalf("frame %d carries no counter: %v", len(counters)+1, err)
		}

		counters = append(counters, counter)

		_ = part.Close()
	}

	return counters
}

// startCameraDevice runs one simulated camera for the length of the test. It is
// the far end of the wire: the binary under test dials it exactly as it would
// dial an ESP32-CAM on somebody's bench.
func startCameraDevice(t *testing.T, label string) *camerasim.Server {
	t.Helper()

	device, err := camerasim.Start(context.WithoutCancel(t.Context()), camerasim.Options{
		Addr:  "127.0.0.1:0",
		FPS:   cameraFPS,
		Label: label,
	})
	if err != nil {
		t.Fatalf("start the simulated camera: %v", err)
	}

	t.Cleanup(func() { _ = device.Close() })

	return device
}
