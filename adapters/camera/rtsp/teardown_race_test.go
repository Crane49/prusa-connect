package rtsp_test

import (
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/adapters/camera/rtsp"
	"github.com/prusa3d/prusa-connect/sdk/cameraapi"
)

// TestTheTeardownRacesTheKeepaliveAndTheReader is the shape the fixed race had,
// driven hard enough to find another one beside it.
//
// A playing session has three goroutines on one connection: pump reading media,
// refresh writing the keepalive on its own clock, and whoever closes the stream
// writing the teardown and then closing the socket. The device declares a one
// second session, so the keepalive falls due every 500ms and every close lands
// somewhere inside that.
//
// Twenty sessions are opened and closed on staggered offsets across the
// keepalive interval, half of them from a second goroutine so the close does not
// always come from the reader's own. Run under -race this is the sweep; run
// without it, it is still the assertion that a close from anywhere ends the
// session and gives the device connection back.
func TestTheTeardownRacesTheKeepaliveAndTheReader(t *testing.T) {
	t.Parallel()

	const sessions = 20

	device := serve(t, &fake{codec: "H264", endless: true, shortSession: true})

	camera, err := rtsp.New(cameraapi.ProviderSpec{
		ID: "watch", Type: rtsp.Type, Printer: "p1",
		Config: map[string]any{"url": device.url},
	})
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, camera.Close()) })

	var running sync.WaitGroup

	for i := range sessions {
		running.Go(func() {
			stream, openErr := camera.Open(t.Context(), cameraapi.FormatH264AnnexB)
			if openErr != nil {
				return
			}

			// A reader that keeps the pump moving for the life of the session.
			reading := make(chan struct{})

			go func() {
				defer close(reading)

				_, _ = io.Copy(io.Discard, stream.Body)
			}()

			// Staggered across the 500ms keepalive interval, so the closes land
			// all over it rather than all in one place.
			time.Sleep(time.Duration(i) * 37 * time.Millisecond)

			if i%2 == 0 {
				// Closed from a goroutine that is neither the reader's nor the
				// one that opened it.
				closed := make(chan struct{})

				go func() { defer close(closed); _ = stream.Body.Close() }()

				<-closed
			} else {
				_ = stream.Body.Close()
			}

			select {
			case <-reading:
			case <-time.After(10 * time.Second):
				t.Error("a closed stream left its reader blocked")
			}
		})
	}

	running.Wait()

	require.Positive(t, device.keepalives.Load(),
		"no keepalive fell due, so the teardown never raced one")
}

// TestClosingAStreamTwiceFromTwoGoroutinesIsOneTeardown holds the idempotence
// Close claims, against two callers arriving together.
//
// Two do arrive together in the real path: the host closing the view, and follow
// closing it because the camera instance stopped. They meet on sync.Once, and
// the second one reads closeErr the first one wrote.
func TestClosingAStreamTwiceFromTwoGoroutinesIsOneTeardown(t *testing.T) {
	t.Parallel()

	device := serve(t, &fake{codec: "H264", endless: true, shortSession: true})

	camera, err := rtsp.New(cameraapi.ProviderSpec{
		ID: "watch", Type: rtsp.Type, Printer: "p1",
		Config: map[string]any{"url": device.url},
	})
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, camera.Close()) })

	for range 10 {
		stream, openErr := camera.Open(t.Context(), cameraapi.FormatH264AnnexB)
		require.NoError(t, openErr)

		read(t, stream.Body, 8)

		var closers sync.WaitGroup

		start := make(chan struct{})

		for range 4 {
			closers.Go(func() {
				<-start

				_ = stream.Body.Close()
			})
		}

		close(start)
		closers.Wait()
	}
}
