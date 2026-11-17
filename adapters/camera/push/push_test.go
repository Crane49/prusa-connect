package push_test

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/adapters/camera/push"
	"github.com/prusa3d/prusa-connect/sdk/cameraapi"
)

// aFrame is one servable still image, the smallest this contract takes.
func aFrame(label string) cameraapi.Snapshot {
	return cameraapi.Snapshot{
		Bytes:     append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, label...),
		MediaType: cameraapi.FormatJPEG.MediaType(),
		Taken:     time.Now().UTC(),
	}
}

func built(t *testing.T, config map[string]any) cameraapi.Receiver {
	t.Helper()

	camera, err := push.New(cameraapi.ProviderSpec{ID: "front", Type: push.Type, Config: config})
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, camera.Close()) })

	receiver, takes := camera.(cameraapi.Receiver)
	require.True(t, takes, "a pushed camera that takes no frame is a camera with no way to get one")

	return receiver
}

// TestAPushedFrameIsTheOneTheSnapshotPathServes is the whole point of this
// driver: what the device sent is what the host hands out next.
func TestAPushedFrameIsTheOneTheSnapshotPathServes(t *testing.T) {
	t.Parallel()

	camera := built(t, nil)

	_, err := camera.Snapshot(t.Context())
	require.ErrorIs(t, err, cameraapi.ErrNoFrame,
		"a camera nobody has sent a frame to answered one anyway")

	first := aFrame("first")
	require.NoError(t, camera.Accept(t.Context(), first))

	held, err := camera.Snapshot(t.Context())
	require.NoError(t, err)
	require.Equal(t, first.Bytes, held.Bytes)

	// The last frame wins. There is no queue here: a still image nobody has
	// asked for yet is worth exactly one frame.
	second := aFrame("second")
	require.NoError(t, camera.Accept(t.Context(), second))

	held, err = camera.Snapshot(t.Context())
	require.NoError(t, err)
	require.Equal(t, second.Bytes, held.Bytes)
}

// TestAFrameThatIsNotServableIsRefused holds the driver to the same check the
// host makes. It is reachable from another host and from a test, and what a
// browser is handed must not be decided by whoever sent the bytes.
func TestAFrameThatIsNotServableIsRefused(t *testing.T) {
	t.Parallel()

	camera := built(t, nil)

	require.ErrorIs(t, camera.Accept(t.Context(), cameraapi.Snapshot{
		MediaType: cameraapi.FormatJPEG.MediaType(),
	}), cameraapi.ErrInvalidSnapshot, "a frame with no bytes was stored")

	require.ErrorIs(t, camera.Accept(t.Context(), cameraapi.Snapshot{
		Bytes: []byte("text"), MediaType: "text/html",
	}), cameraapi.ErrInvalidSnapshot, "a media type a browser runs was stored")

	_, err := camera.Snapshot(t.Context())
	require.ErrorIs(t, err, cameraapi.ErrNoFrame)
}

// TestACameraThatStoppedSendingStopsShowingItsLastPicture is the difference
// between a camera pointed at a still scene and a camera that is gone. A held
// frame past the hold time reads as no frame, which the host answers with 204.
func TestACameraThatStoppedSendingStopsShowingItsLastPicture(t *testing.T) {
	t.Parallel()

	camera := built(t, map[string]any{"stale": "20ms"})

	require.NoError(t, camera.Accept(t.Context(), aFrame("one")))

	held, err := camera.Snapshot(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, held.Bytes)

	require.Eventually(t, func() bool {
		_, stale := camera.Snapshot(t.Context())

		return stale != nil
	}, time.Second, 5*time.Millisecond, "a frame past the hold time was still served")
}

// TestAHoldTimeThatIsNotADurationIsRefused refuses the value at the moment the
// operator writes it. "300" is 300 nanoseconds to Go, and every frame would be
// stale before it was read.
func TestAHoldTimeThatIsNotADurationIsRefused(t *testing.T) {
	t.Parallel()

	for name, value := range map[string]any{
		"a number":       300,
		"not a duration": "five minutes",
		"zero":           "0s",
		"negative":       "-1m",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := push.New(cameraapi.ProviderSpec{
				ID: "front", Type: push.Type, Config: map[string]any{"stale": value},
			})
			require.ErrorIs(t, err, push.ErrStaleRange)
		})
	}
}

// TestTheStreamCarriesEveryPushedFrame is the second half of the task: the
// stream path serves what was pushed, framed with the boundary the contract
// fixes, so a browser plays it with an img tag.
func TestTheStreamCarriesEveryPushedFrame(t *testing.T) {
	t.Parallel()

	camera := built(t, nil)

	stream, err := camera.Open(t.Context(), cameraapi.FormatMJPEG)
	require.NoError(t, err)
	require.Equal(t, cameraapi.FormatMJPEG.MediaType(), stream.MediaType)

	t.Cleanup(func() { require.NoError(t, stream.Body.Close()) })

	require.NoError(t, camera.Accept(t.Context(), aFrame("first")))

	// The header and the bytes are two writes on an unbuffered pipe, so one
	// part is read until it is whole rather than in one call.
	var part []byte

	chunk := make([]byte, 4096)

	for !bytes.Contains(part, []byte("first")) {
		n, readErr := stream.Body.Read(chunk)
		require.NoError(t, readErr)

		part = append(part, chunk[:n]...)
	}

	require.True(t, bytes.HasPrefix(part, []byte("--"+cameraapi.MJPEGBoundary+"\r\n")),
		"the part is not framed with the boundary the contract fixes: %q", part)
	require.Contains(t, string(part), "Content-Type: image/jpeg")
}

// TestAFormatThisCameraDoesNotEmitIsUnsupported keeps an earlier design decision: an
// unsupported combination is reported as unsupported rather than converted to.
func TestAFormatThisCameraDoesNotEmitIsUnsupported(t *testing.T) {
	t.Parallel()

	camera := built(t, nil)

	_, err := camera.Open(t.Context(), cameraapi.FormatH264AnnexB)
	require.ErrorIs(t, err, cameraapi.ErrFormatNotSupported)
}

// TestClosingTheCameraEndsEveryStream is what the supervisor relies on. A
// viewer reading from an instance the host has stopped is watching a camera
// that is no longer declared.
func TestClosingTheCameraEndsEveryStream(t *testing.T) {
	t.Parallel()

	camera, err := push.New(cameraapi.ProviderSpec{ID: "front", Type: push.Type})
	require.NoError(t, err)

	stream, err := camera.Open(t.Context(), cameraapi.FormatMJPEG)
	require.NoError(t, err)

	require.NoError(t, camera.Close())

	_, err = io.ReadAll(stream.Body)
	require.NoError(t, err, "the stream never ended, so the reader would hold for the life of the process")
}
