package mjpeg_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/adapters/camera/mjpeg"
	"github.com/prusa3d/prusa-connect/sdk/cameraapi"
	"github.com/prusa3d/prusa-connect/sdk/cameraapi/cameraapitest"
)

// jpegFrame is the smallest byte sequence a JPEG reader accepts as a start of
// image. Every case here needs an image the driver will not refuse, and none of
// them decodes one.
func jpegFrame(mark byte) []byte {
	return append([]byte{0xFF, 0xD8, 0xFF, 0xE0, mark}, 0xFF, 0xD9)
}

// device is a camera on the network: one snapshot endpoint, one multipart
// stream endpoint, and a switch for each failure a real one presents.
type device struct {
	server *httptest.Server

	fetches atomic.Int64
	streams atomic.Int64

	mu       sync.Mutex
	frame    []byte
	status   int
	body     []byte
	bodyType string
	// held blocks the stream handler, so a case can hold a reader open and
	// then take the camera away underneath it.
	held chan struct{}
}

func newDevice(t *testing.T) *device {
	t.Helper()

	dev := &device{frame: jpegFrame(0x01), status: http.StatusOK}

	dev.server = httptest.NewServer(http.HandlerFunc(dev.serve))
	t.Cleanup(dev.server.Close)

	return dev
}

// serve routes on the path itself. A ServeMux would be a second router on this
// tree, which internal/transport/http/httpapi refuses wherever it appears.
func (d *device) serve(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/snap.jpg":
		d.serveSnapshot(w, r)
	case "/stream.mjpg":
		d.serveStream(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (d *device) serveSnapshot(w http.ResponseWriter, _ *http.Request) {
	d.fetches.Add(1)

	d.mu.Lock()
	status, frame, body, bodyType := d.status, d.frame, d.body, d.bodyType
	d.mu.Unlock()

	if status != http.StatusOK {
		w.WriteHeader(status)

		return
	}

	if body != nil {
		w.Header().Set("Content-Type", bodyType)
		_, _ = w.Write(body)

		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	_, _ = w.Write(frame)
}

func (d *device) serveStream(w http.ResponseWriter, r *http.Request) {
	d.streams.Add(1)

	d.mu.Lock()
	held := d.held
	d.mu.Unlock()

	w.Header().Set("Content-Type", `multipart/x-mixed-replace; boundary=devicewall`)
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)

	for i := range 1000 {
		frame := jpegFrame(byte(i))

		_, err := fmt.Fprintf(w, "--devicewall\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n",
			len(frame))
		if err != nil {
			return
		}

		if _, err = w.Write(append(frame, "\r\n"...)); err != nil {
			return
		}

		if flusher != nil {
			flusher.Flush()
		}

		if held != nil {
			select {
			case <-held:
			case <-r.Context().Done():
				return
			}
		}

		select {
		case <-r.Context().Done():
			return
		case <-time.After(time.Millisecond):
		}
	}
}

func (d *device) setStatus(status int) {
	d.mu.Lock()
	d.status = status
	d.mu.Unlock()
}

func (d *device) answerWith(body []byte, mediaType string) {
	d.mu.Lock()
	d.body, d.bodyType = body, mediaType
	d.mu.Unlock()
}

func (d *device) snapshotURL() string { return d.server.URL + "/snap.jpg" }
func (d *device) streamURL() string   { return d.server.URL + "/stream.mjpg" }

// build makes one camera against this device.
func build(t *testing.T, dev *device, overrides map[string]any) cameraapi.Camera {
	t.Helper()

	settings := map[string]any{
		"snapshot_url": dev.snapshotURL(),
		"stream_url":   dev.streamURL(),
		"min_interval": "0s",
	}

	maps.Copy(settings, overrides)

	camera, err := mjpeg.New(cameraapi.ProviderSpec{Type: mjpeg.Type, Printer: "mk4-017", Config: settings})
	require.NoError(t, err)
	t.Cleanup(func() { _ = camera.Close() })

	return camera
}

// TestContract runs the published conformance suite, which is the whole of what
// a host may assume about any camera driver.
func TestContract(t *testing.T) {
	t.Parallel()

	dev := newDevice(t)

	cameraapitest.RunCameraContract(t, func(t *testing.T) cameraapi.Camera {
		t.Helper()

		camera, err := mjpeg.New(cameraapi.ProviderSpec{
			Type:    mjpeg.Type,
			Printer: "mk4-017",
			Config: map[string]any{
				"snapshot_url": dev.snapshotURL(),
				"stream_url":   dev.streamURL(),
				"min_interval": "0s",
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		return camera
	})
}

// TestTheDeclaredFormatSetFollowsWhatWasConfigured is an earlier design decision at its
// smallest: a camera declares what it can emit, and a path nothing was
// configured for is not declared rather than declared and then refused.
func TestTheDeclaredFormatSetFollowsWhatWasConfigured(t *testing.T) {
	t.Parallel()

	dev := newDevice(t)

	cases := map[string]struct {
		settings map[string]any
		want     []cameraapi.Format
	}{
		"both endpoints": {
			settings: map[string]any{},
			want:     []cameraapi.Format{cameraapi.FormatJPEG, cameraapi.FormatMJPEG},
		},
		"a snapshot endpoint only": {
			settings: map[string]any{"stream_url": ""},
			want:     []cameraapi.Format{cameraapi.FormatJPEG},
		},
		"a stream endpoint only": {
			settings: map[string]any{"snapshot_url": ""},
			want:     []cameraapi.Format{cameraapi.FormatMJPEG},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			camera := build(t, dev, tc.settings)
			require.Equal(t, tc.want, camera.Formats())
		})
	}
}

// TestACameraWithNoEndpointIsRefused holds the build-time half of V2. A camera
// that can emit nothing is refused where an operator can see it, rather than at
// the first viewer.
func TestACameraWithNoEndpointIsRefused(t *testing.T) {
	t.Parallel()

	_, err := mjpeg.New(cameraapi.ProviderSpec{Type: mjpeg.Type, Printer: "mk4-017", Config: map[string]any{}})
	require.ErrorIs(t, err, mjpeg.ErrNoURL)
}

// TestAnUnfetchableSchemeIsRefused is the other build-time refusal. A camera
// URL is fetched by this process, so a scheme it cannot fetch is a camera that
// can never work, and a file:// URL would be this server reading its own disk
// on a name an operator wrote.
func TestAnUnfetchableSchemeIsRefused(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"a file url":  "file:///etc/passwd",
		"an rtsp url": "rtsp://192.0.2.1/live",
		"no host":     "http:///snap.jpg",
	}

	for name, endpoint := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := mjpeg.New(cameraapi.ProviderSpec{
				Type: mjpeg.Type, Printer: "mk4-017",
				Config: map[string]any{"snapshot_url": endpoint},
			})
			require.ErrorIs(t, err, mjpeg.ErrInsecureURL)
			require.NotContains(t, err.Error(), endpoint,
				"the refusal quotes the URL, and a camera URL carries credentials")
		})
	}
}

// TestABodyThatIsNotAJpegIsRefused is the hostile-device case, and it is the
// one that decides what this server's origin serves.
//
// A camera is a device on somebody's network. One that has been taken over
// answers the snapshot endpoint with whatever its new owner chose, and a host
// that relayed it would be serving that from its own origin, under its own
// session cookie.
func TestABodyThatIsNotAJpegIsRefused(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		body      []byte
		mediaType string
	}{
		"html": {body: []byte(`<script>alert(1)</script>`), mediaType: "text/html"},
		"html labelled as an image": {
			body:      []byte(`<script>alert(1)</script>`),
			mediaType: "image/jpeg",
		},
		"an empty body": {body: []byte{}, mediaType: "image/jpeg"},
		"a truncated marker": {
			body:      []byte{0xFF, 0xD8},
			mediaType: "image/jpeg",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dev := newDevice(t)
			dev.answerWith(tc.body, tc.mediaType)

			camera := build(t, dev, nil)

			_, err := camera.Snapshot(t.Context())
			require.ErrorIs(t, err, mjpeg.ErrBadFrame)
		})
	}
}

// TestAnEndlessBodyIsCutOffAtTheCeiling is the other half of the hostile
// device. A camera that answers with a body that never ends is a camera that
// takes the host's whole memory, and a reader with no ceiling is what lets it.
func TestAnEndlessBodyIsCutOffAtTheCeiling(t *testing.T) {
	t.Parallel()

	endless := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte{0xFF, 0xD8, 0xFF})

		block := bytes.Repeat([]byte{0x00}, 4096)

		for r.Context().Err() == nil {
			if _, err := w.Write(block); err != nil {
				return
			}
		}
	}))
	t.Cleanup(endless.Close)

	camera, err := mjpeg.New(cameraapi.ProviderSpec{
		Type: mjpeg.Type, Printer: "mk4-017",
		Config: map[string]any{
			"snapshot_url":    endless.URL,
			"max_frame_bytes": 64 << 10,
			"min_interval":    "0s",
			"timeout":         "10s",
		},
	})
	require.NoError(t, err)

	t.Cleanup(func() { _ = camera.Close() })

	_, err = camera.Snapshot(t.Context())
	require.ErrorIs(t, err, mjpeg.ErrBadFrame)
	require.Contains(t, err.Error(), "ceiling")
}

// TestOneFetchAnswersEveryViewerInsideTheInterval is a design requirement's
// arithmetic. A thumbnail grid of four hundred printers must not become four
// hundred device fetches, and the held frame is what stops it.
func TestOneFetchAnswersEveryViewerInsideTheInterval(t *testing.T) {
	t.Parallel()

	dev := newDevice(t)
	camera := build(t, dev, map[string]any{"min_interval": "1h"})

	const viewers = 40

	var (
		wait    sync.WaitGroup
		refused atomic.Int64
	)

	for range viewers {
		wait.Go(func() {
			if _, err := camera.Snapshot(t.Context()); err != nil {
				refused.Add(1)
			}
		})
	}

	wait.Wait()

	require.Zero(t, refused.Load(), "a viewer was refused while the frame was being held")

	require.Equal(t, int64(1), dev.fetches.Load(),
		"%d viewers inside one interval cost %d device fetches", viewers, dev.fetches.Load())
}

// TestAFailedFetchKeepsTheFrameAlreadyHeld is the degradation a grid needs. A
// camera on a busy wireless network misses requests, and a cell that emptied on
// every miss would flicker for the whole fleet.
func TestAFailedFetchKeepsTheFrameAlreadyHeld(t *testing.T) {
	t.Parallel()

	dev := newDevice(t)
	camera := build(t, dev, nil)

	first, err := camera.Snapshot(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, first.Bytes)

	dev.setStatus(http.StatusInternalServerError)

	second, err := camera.Snapshot(t.Context())
	require.NoError(t, err, "a camera that missed one request answered with nothing at all")
	require.Equal(t, first.Bytes, second.Bytes)
}

// TestTheFirstFailureWithNothingHeldIsReported is the other direction. A camera
// that has never answered is a camera with nothing to show, and reporting
// success there would put an empty image in a grid cell forever.
func TestTheFirstFailureWithNothingHeldIsReported(t *testing.T) {
	t.Parallel()

	dev := newDevice(t)
	dev.setStatus(http.StatusUnauthorized)

	camera := build(t, dev, nil)

	_, err := camera.Snapshot(t.Context())
	require.ErrorIs(t, err, mjpeg.ErrBadFrame)
}

// TestAnEmptyDeviceAnswerIsNoFrameRatherThanAFailure separates a camera that is
// starting from one that is broken. The first is answered 204 by the host; the
// second is a failure the supervision plane counts.
func TestAnEmptyDeviceAnswerIsNoFrameRatherThanAFailure(t *testing.T) {
	t.Parallel()

	dev := newDevice(t)
	dev.setStatus(http.StatusNoContent)

	camera := build(t, dev, nil)

	_, err := camera.Snapshot(t.Context())
	require.ErrorIs(t, err, cameraapi.ErrNoFrame)
}

// TestTheStreamIsReframedWithTheContractBoundary is the pass-through claim,
// checked on the bytes. The device frames with its own boundary and the host
// receives the contract's, and every frame arrives whole, so the host can state
// the content type without asking the driver.
func TestTheStreamIsReframedWithTheContractBoundary(t *testing.T) {
	t.Parallel()

	dev := newDevice(t)
	camera := build(t, dev, nil)

	stream, err := camera.Open(t.Context(), cameraapi.FormatMJPEG)
	require.NoError(t, err)

	defer func() { _ = stream.Body.Close() }()

	require.Equal(t, cameraapi.FormatMJPEG.MediaType(), stream.MediaType)

	read := make([]byte, 4096)

	var seen []byte

	for len(seen) < 200 {
		n, readErr := stream.Body.Read(read)
		seen = append(seen, read[:n]...)

		require.NoError(t, readErr)
	}

	body := string(seen)
	require.True(t, strings.HasPrefix(body, "--"+cameraapi.MJPEGBoundary+"\r\n"),
		"the stream does not open with the contract's boundary: %q", body[:40])
	require.Contains(t, body, "Content-Type: image/jpeg")
	require.Contains(t, body, "Content-Length: 7")
	require.NotContains(t, body, "devicewall", "the device's own boundary reached the host")
}

// TestAnUndeclaredFormatIsUnsupported is an earlier design decision on this driver. Nothing is
// converted, and a format the camera does not emit is reported as unsupported.
func TestAnUndeclaredFormatIsUnsupported(t *testing.T) {
	t.Parallel()

	dev := newDevice(t)
	camera := build(t, dev, map[string]any{"stream_url": ""})

	_, err := camera.Open(t.Context(), cameraapi.FormatMJPEG)
	require.ErrorIs(t, err, cameraapi.ErrFormatNotSupported)

	_, err = camera.Open(t.Context(), cameraapi.FormatH264AnnexB)
	require.ErrorIs(t, err, cameraapi.ErrFormatNotSupported)
}

// TestClosingTheCameraEndsAStreamSomebodyIsStillReading is the camera that goes
// away mid-stream. The host stops an instance without knowing who is reading
// from it, and a reader left blocked on a device that will never answer again
// is a goroutine per viewer the process cannot get back.
func TestClosingTheCameraEndsAStreamSomebodyIsStillReading(t *testing.T) {
	t.Parallel()

	dev := newDevice(t)

	held := make(chan struct{})
	dev.mu.Lock()
	dev.held = held
	dev.mu.Unlock()

	camera := build(t, dev, nil)

	stream, err := camera.Open(t.Context(), cameraapi.FormatMJPEG)
	require.NoError(t, err)

	reading := make(chan error, 1)

	go func() {
		_, copyErr := io.Copy(io.Discard, stream.Body)
		reading <- copyErr
	}()

	// The reader is inside a Read the device is not going to answer, which is
	// the state a camera that has been unplugged leaves every viewer in.
	require.NoError(t, camera.Close())

	select {
	case <-reading:
	case <-time.After(5 * time.Second):
		t.Fatal("closing the camera left a reader blocked on a device that is gone")
	}

	_ = stream.Body.Close()
	close(held)
}

// TestCredentialsNeverReachAnErrorOrAString is a design requirement held on the one
// path a credential could take: a driver's own error text. A camera password is
// in the configuration, so the only thing that keeps it out of a log is having
// no code that formats one.
func TestCredentialsNeverReachAnErrorOrAString(t *testing.T) {
	t.Parallel()

	const password = "hunter2-the-camera-password"

	// A port nothing is listening on, so every call fails and every failure has
	// to render an error.
	camera, err := mjpeg.New(cameraapi.ProviderSpec{
		Type: mjpeg.Type, Printer: "mk4-017",
		Config: map[string]any{
			"snapshot_url": "http://127.0.0.1:1/snap.jpg",
			"stream_url":   "http://127.0.0.1:1/stream.mjpg",
			"username":     "admin",
			"password":     password,
			"timeout":      "200ms",
			"min_interval": "0s",
		},
	})
	require.NoError(t, err)

	t.Cleanup(func() { _ = camera.Close() })

	_, snapErr := camera.Snapshot(t.Context())
	require.Error(t, snapErr)
	require.NotContains(t, snapErr.Error(), password)
	require.NotContains(t, snapErr.Error(), "admin")

	_, openErr := camera.Open(t.Context(), cameraapi.FormatMJPEG)
	require.Error(t, openErr)
	require.NotContains(t, openErr.Error(), password)

	require.NotContains(t, fmt.Sprintf("%v %+v", camera, camera), password,
		"the camera renders its own password when something formats it")
}

// TestARedirectIsNotChased is the camera pointing this server somewhere else. A
// device that answers a snapshot with a redirect is naming a host the operator
// did not configure, and following it would make this process fetch from it.
func TestARedirectIsNotChased(t *testing.T) {
	t.Parallel()

	var elsewhere atomic.Int64

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		elsewhere.Add(1)
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegFrame(0x02))
	}))
	t.Cleanup(target.Close)

	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	t.Cleanup(redirecting.Close)

	camera, err := mjpeg.New(cameraapi.ProviderSpec{
		Type: mjpeg.Type, Printer: "mk4-017",
		Config: map[string]any{"snapshot_url": redirecting.URL, "min_interval": "0s"},
	})
	require.NoError(t, err)

	t.Cleanup(func() { _ = camera.Close() })

	_, err = camera.Snapshot(t.Context())
	require.Error(t, err)
	require.Zero(t, elsewhere.Load(), "the driver fetched from a host the operator never configured")
}

// TestTheDeviceStampIsWhatAConditionalGetCompares holds the field a thumbnail
// grid's cheapness rests on. A device that says when it took the frame gives
// every client a validator that survives this server restarting.
func TestTheDeviceStampIsWhatAConditionalGetCompares(t *testing.T) {
	t.Parallel()

	stamped := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Last-Modified", stamped.Format(http.TimeFormat))
		_, _ = w.Write(jpegFrame(0x03))
	}))
	t.Cleanup(server.Close)

	camera, err := mjpeg.New(cameraapi.ProviderSpec{
		Type: mjpeg.Type, Printer: "mk4-017",
		Config: map[string]any{"snapshot_url": server.URL, "min_interval": "0s"},
	})
	require.NoError(t, err)

	t.Cleanup(func() { _ = camera.Close() })

	shot, err := camera.Snapshot(t.Context())
	require.NoError(t, err)
	require.Equal(t, stamped, shot.Taken)
}

// TestSnapshotAfterCloseReportsNoFrame is the reconcile race. A stop and a
// viewer arrive together often enough that the loser has to be defined, and a
// closed camera that reached the network again would be a driver the host
// cannot stop.
func TestSnapshotAfterCloseReportsNoFrame(t *testing.T) {
	t.Parallel()

	dev := newDevice(t)
	camera := build(t, dev, nil)

	require.NoError(t, camera.Close())

	_, err := camera.Snapshot(context.Background())
	require.ErrorIs(t, err, cameraapi.ErrNoFrame)
	require.Zero(t, dev.fetches.Load(), "a closed camera fetched from the device")
}
