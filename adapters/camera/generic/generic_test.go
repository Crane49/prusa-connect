package generic_test

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/adapters/camera/generic"
	"github.com/prusa3d/prusa-connect/sdk/cameraapi"
)

var jpeg = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x01, 0xFF, 0xD9}

func build(t *testing.T, config map[string]any) cameraapi.Camera {
	t.Helper()

	camera, err := generic.New(cameraapi.ProviderSpec{ID: "front", Type: generic.Type, Config: config})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = camera.Close() })

	return camera
}

// With no address at all it is a camera that sends its own images, and it
// declares a still image and no stream.
func TestACameraWithNoAddressTakesTheImagesItSends(t *testing.T) {
	t.Parallel()

	camera := build(t, nil)

	if got := camera.Formats(); !slices.Equal(got, []cameraapi.Format{cameraapi.FormatJPEG}) {
		t.Fatalf("formats %v", got)
	}

	if _, err := camera.Snapshot(t.Context()); !errors.Is(err, cameraapi.ErrNoFrame) {
		t.Fatalf("before any image: %v", err)
	}

	receiver, ok := camera.(cameraapi.Receiver)
	if !ok {
		t.Fatal("the camera takes no image")
	}

	if err := receiver.Accept(t.Context(), cameraapi.Snapshot{
		Bytes: jpeg, MediaType: "image/jpeg", Taken: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	shot, err := camera.Snapshot(t.Context())
	if err != nil || !bytes.Equal(shot.Bytes, jpeg) {
		t.Fatalf("snapshot %v %v", shot.Bytes, err)
	}

	if _, err = camera.Open(t.Context(), cameraapi.FormatMJPEG); !errors.Is(err, cameraapi.ErrFormatNotSupported) {
		t.Fatalf("a stream from a camera that declares none: %v", err)
	}

	// The device decides how often it sends, so the camera declares no pace.
	if paced, isPaced := camera.(cameraapi.Paced); !isPaced || paced.MinInterval() != 0 {
		t.Fatal("a camera that is sent its images declared a pace")
	}
}

// With a snapshot address the images are fetched, and an image sent to it is
// refused rather than replaced by the next fetch.
func TestASnapshotAddressIsFetchedAndTakesNoSentImage(t *testing.T) {
	t.Parallel()

	device := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpeg)
	}))
	t.Cleanup(device.Close)

	camera := build(t, map[string]any{"snapshot_url": device.URL + "/snap.jpg"})

	shot, err := camera.Snapshot(t.Context())
	if err != nil || !bytes.Equal(shot.Bytes, jpeg) {
		t.Fatalf("snapshot %v %v", shot.Bytes, err)
	}

	receiver, _ := camera.(cameraapi.Receiver)
	if err = receiver.Accept(t.Context(), cameraapi.Snapshot{
		Bytes: jpeg, MediaType: "image/jpeg",
	}); !errors.Is(err, cameraapi.ErrPushNotSupported) {
		t.Fatalf("a sent image: %v", err)
	}
}

// The stream's address decides its format: RTSP is H.264 and HTTP is MJPEG.
// Neither is dialled until somebody opens it.
func TestTheStreamAddressDecidesTheFormat(t *testing.T) {
	t.Parallel()

	for address, want := range map[string]cameraapi.Format{
		"rtsp://user:secret@192.0.2.1/live": cameraapi.FormatH264AnnexB,
		"http://192.0.2.1/stream":           cameraapi.FormatMJPEG,
	} {
		camera := build(t, map[string]any{"stream_url": address})

		got := camera.Formats()
		if !slices.Equal(got, []cameraapi.Format{cameraapi.FormatJPEG, want}) {
			t.Errorf("%s: formats %v", address, got)
		}
	}
}

func TestAStreamAddressOfAnotherSchemeIsRefused(t *testing.T) {
	t.Parallel()

	_, err := generic.New(cameraapi.ProviderSpec{
		ID: "front", Type: generic.Type, Config: map[string]any{"stream_url": "ftp://192.0.2.1/x"},
	})
	if !errors.Is(err, generic.ErrStreamScheme) {
		t.Fatalf("got %v", err)
	}
}
