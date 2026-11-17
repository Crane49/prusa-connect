package camerasim

import (
	"bytes"
	"image"
	"image/jpeg"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/cameraapi"
)

// testFPS is fast enough that a ten frame test finishes in a fifth of a second,
// and slow enough that the renderer is not the thing being measured.
const testFPS = 50

// start brings up a simulator on a kernel-chosen loopback port, so tests that
// run at the same time can never collide on one.
func start(t *testing.T) *Server {
	t.Helper()

	server, err := Start(t.Context(), Options{Addr: "127.0.0.1:0", FPS: testFPS, Label: "MK4-01"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close()) })

	return server
}

func get(t *testing.T, url string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	return resp
}

func TestASnapshotIsAJPEGThatDecodes(t *testing.T) {
	t.Parallel()

	server := start(t)

	resp := get(t, server.SnapshotURL())
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "image/jpeg", resp.Header.Get("Content-Type"))

	img, err := jpeg.Decode(resp.Body)
	require.NoError(t, err)
	require.Positive(t, img.Bounds().Dx())

	counter, err := ReadCounter(img)
	require.NoError(t, err)
	require.Positive(t, counter)
}

func TestTheStreamDeliversTenFramesWithAnAdvancingCounter(t *testing.T) {
	t.Parallel()

	const wanted = 10

	server := start(t)

	resp := get(t, server.StreamURL())
	require.Equal(t, http.StatusOK, resp.StatusCode)

	mediaType, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/x-mixed-replace", mediaType)
	require.Equal(t, cameraapi.MJPEGBoundary, params["boundary"])

	reader := multipart.NewReader(resp.Body, cameraapi.MJPEGBoundary)

	var previous uint64

	for range wanted {
		part, partErr := reader.NextPart()
		require.NoError(t, partErr)
		require.Equal(t, "image/jpeg", part.Header.Get("Content-Type"))
		require.NotEmpty(t, part.Header.Get("Content-Length"))

		img, decodeErr := jpeg.Decode(part)
		require.NoError(t, decodeErr)
		require.NoError(t, part.Close())

		counter, counterErr := ReadCounter(img)
		require.NoError(t, counterErr)
		require.Greater(t, counter, previous)

		previous = counter
	}
}

func TestTheCounterSurvivesAJPEGRoundTrip(t *testing.T) {
	t.Parallel()

	const counter = 0xDEADBEEF

	opts := Options{Quality: 40}.normalise()

	encoded, err := encodeFrame(opts, counter, time.Now().UTC())
	require.NoError(t, err)

	decoded, err := jpeg.Decode(bytes.NewReader(encoded))
	require.NoError(t, err)

	read, err := ReadCounter(decoded)
	require.NoError(t, err)
	require.Equal(t, uint64(counter), read)
}

func TestClosingTheServerEndsAStreamInFlight(t *testing.T) {
	t.Parallel()

	server, err := Start(t.Context(), Options{Addr: "127.0.0.1:0", FPS: testFPS})
	require.NoError(t, err)

	resp := get(t, server.StreamURL())
	reader := multipart.NewReader(resp.Body, cameraapi.MJPEGBoundary)

	_, err = reader.NextPart()
	require.NoError(t, err)

	require.NoError(t, server.Close())

	// Whatever the connection had already buffered is still readable, so the
	// assertion is that the stream ends rather than that it ends at once. A
	// stream that outlived its server would never reach the error.
	const bounded = 1000

	for range bounded {
		if _, partErr := reader.NextPart(); partErr != nil {
			return
		}
	}

	t.Fatal("the stream kept delivering frames after the server was closed")
}

func TestTheIndexNamesBothPaths(t *testing.T) {
	t.Parallel()

	server := start(t)

	resp := get(t, "http://"+server.Addr()+"/")
	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), "/snapshot.jpg")
	require.Contains(t, string(body), "/stream.mjpg")
}

func TestReadCounterRefusesAnImageWithNoRoomForOne(t *testing.T) {
	t.Parallel()

	_, err := ReadCounter(image.NewRGBA(image.Rect(0, 0, 16, 16)))
	require.ErrorIs(t, err, ErrImageTooSmall)
}

func TestTheFrameCountKeepsUpWithTheConfiguredRate(t *testing.T) {
	t.Parallel()

	server := start(t)

	first := server.Frames()
	require.Positive(t, first)

	require.Eventually(t, func() bool {
		return server.Frames() > first
	}, time.Second, 10*time.Millisecond)
}

// The size a driver is configured for is the size it gets. A snapshot that came
// back at another size would have a browser scaling every tile in a grid.
func TestASnapshotCarriesTheSizeItWasConfiguredWith(t *testing.T) {
	t.Parallel()

	const width, height = 640, 480

	server, err := Start(t.Context(), Options{
		Addr: "127.0.0.1:0", FPS: testFPS, Width: width, Height: height,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close()) })

	img, err := jpeg.Decode(get(t, server.SnapshotURL()).Body)
	require.NoError(t, err)
	require.Equal(t, width, img.Bounds().Dx())
	require.Equal(t, height, img.Bounds().Dy())
}

// A browser plays this in an img tag, so the stream has to be moving at the
// cadence the camera declared. Three frames in a second is the floor a person
// reads as live rather than as a picture that keeps being replaced.
func TestTheStreamDeliversThreeFramesInASecondAtTheDefaultRate(t *testing.T) {
	t.Parallel()

	const wanted = 3

	server, err := Start(t.Context(), Options{Addr: "127.0.0.1:0"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close()) })

	resp := get(t, server.StreamURL())

	mediaType, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/x-mixed-replace", mediaType)
	require.Equal(t, cameraapi.MJPEGBoundary, params["boundary"])

	reader := multipart.NewReader(resp.Body, cameraapi.MJPEGBoundary)
	started := time.Now()

	for range wanted {
		part, partErr := reader.NextPart()
		require.NoError(t, partErr)

		_, decodeErr := jpeg.Decode(part)
		require.NoError(t, decodeErr)
		require.NoError(t, part.Close())
	}

	require.Less(t, time.Since(started), time.Second,
		"three frames took longer than a second, so the stream is slower than it declared")
}

// The picture has to change as the print advances. A camera drawing the same
// frame for the whole job is the one failure a person watching a live view
// cannot tell from a working one.
func TestTheFrameChangesAsThePrintAdvances(t *testing.T) {
	t.Parallel()

	var progress atomic.Int64

	server, err := Start(t.Context(), Options{
		Addr: "127.0.0.1:0", FPS: testFPS, Label: "MK4-01",
		Status: func() Status {
			return Status{State: "PRINTING", Progress: int(progress.Load())}
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close()) })

	first, err := io.ReadAll(get(t, server.SnapshotURL()).Body)
	require.NoError(t, err)

	progress.Store(80)

	// The renderer draws on a ticker, so the next frame is the first one that
	// carries the new figure. Waiting for the counter to move is what makes this
	// a test of the picture rather than of the clock.
	before := server.Frames()
	require.Eventually(t, func() bool { return server.Frames() > before+1 },
		time.Second, 10*time.Millisecond)

	second, err := io.ReadAll(get(t, server.SnapshotURL()).Body)
	require.NoError(t, err)

	require.NotEqual(t, first, second, "the frame did not change while the print advanced")
	require.NotEqual(t, pixelsOf(t, first), pixelsOf(t, second),
		"the two frames decode to the same picture, so only the encoding changed")
}

// pixelsOf decodes one frame down to its pixels, so two frames are compared as
// pictures rather than as JPEG byte streams.
func pixelsOf(t *testing.T, encoded []byte) []uint8 {
	t.Helper()

	img, err := jpeg.Decode(bytes.NewReader(encoded))
	require.NoError(t, err)

	bounds := img.Bounds()
	out := make([]uint8, 0, bounds.Dx()*bounds.Dy())

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			out = append(out, luminance(img.At(x, y)))
		}
	}

	return out
}

// A camera that watches no printer draws no state line and no bar, and a camera
// that watches one draws both. The two frames differ, which is what says the
// state reached the picture at all.
func TestACameraWatchingAPrinterDrawsWhatItIsDoing(t *testing.T) {
	t.Parallel()

	opts := Options{Label: "MK4-01"}.normalise()

	watching := opts
	watching.Status = func() Status { return Status{State: "PRINTING", Progress: 42} }

	blank, err := encodeFrame(opts, 1, time.Unix(0, 0).UTC())
	require.NoError(t, err)

	drawn, err := encodeFrame(watching, 1, time.Unix(0, 0).UTC())
	require.NoError(t, err)

	require.NotEqual(t, pixelsOf(t, blank), pixelsOf(t, drawn))
}

// The bar is drawn from the figure the printer reported, and a driver that
// reported one outside the scale is drawn at the end of it rather than past the
// edge of the frame.
func TestAProgressFigureOutsideTheScaleIsDrawnAtItsEnd(t *testing.T) {
	t.Parallel()

	require.Equal(t, 0, clampPercent(-5))
	require.Equal(t, 100, clampPercent(400))
	require.Equal(t, "PRINTING 100%", stateLine(Status{State: "printing", Progress: 400}))
	require.Empty(t, stateLine(Status{}))
}

// TestAnEmptyAddressIsAFreePortOnLoopback keeps a simulated camera out of
// reach of other machines unless its caller names an address.
func TestAnEmptyAddressIsAFreePortOnLoopback(t *testing.T) {
	t.Parallel()

	require.Equal(t, "127.0.0.1:0", Options{}.normalise().Addr)
}
