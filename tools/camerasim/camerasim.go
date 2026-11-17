// Package camerasim serves a camera that does not exist: an HTTP endpoint that
// answers one JPEG, and an HTTP endpoint that answers a multipart stream of
// them, framed with the boundary the camera contract fixes.
//
// It exists because every test of the camera path so far has needed either a
// real device on somebody's desk or a recorded file, and neither can answer the
// question that matters: did the frame the browser is looking at come from the
// device a moment ago, or is it a stale one the host kept? A recorded file
// cannot tell those apart, and a real camera pointed at a static wall cannot
// either. So every frame here carries a counter that a machine can read back
// out of the pixels, which turns "is this frame fresh" into an assertion rather
// than a judgement.
//
// It is a package rather than only a command so a test can start one in the
// same process on port zero and never touch the network beyond loopback. The
// command under cmd/camerasim is a thin wrapper for the manual lane.
//
// Nothing here is a driver. It is the other side of the wire from
// adapters/camera/mjpeg, and the two are meant to be pointed at each other.
package camerasim

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/cameraapi"
)

const (
	// defaultWidth and defaultHeight are 320x240, which is the smallest size an
	// ESP32-CAM commonly reports and so the smallest a driver has to cope with.
	defaultWidth  = 320
	defaultHeight = 240
	// defaultFPS is deliberately slow. A simulator is a background process on a
	// developer's machine, and five frames a second is already faster than any
	// browser grid refreshes.
	defaultFPS = 5.0
	// defaultQuality is the JPEG quality. It is high enough that the counter
	// blocks survive the encoder and low enough that the frames look like a
	// camera's rather than like a screenshot.
	defaultQuality = 70
	// maxQuality is the ceiling image/jpeg accepts.
	maxQuality = 100
	// defaultAddr is a free port on loopback.
	defaultAddr = "127.0.0.1:0"

	// readHeaderTimeout bounds how long a client may take to send its request
	// line and headers. There is no write timeout on purpose: a stream is a
	// response that never finishes, and a write deadline would cut it.
	readHeaderTimeout = 10 * time.Second

	// snapshotPath and streamPath are the two routes. They are named after the
	// Prusa ESP32-CAM firmware's own paths, so a driver already configured
	// against a real camera can be pointed here by changing the host alone.
	snapshotPath = "/snapshot.jpg"
	streamPath   = "/stream.mjpg"

	mediaTypeJPEG = "image/jpeg"
)

// ErrImageTooSmall reports an image with no room for the counter readout. It is
// returned rather than a zero counter because a caller that cannot tell those
// two apart would read every undersized frame as frame zero.
var ErrImageTooSmall = errors.New("camerasim: the image is too small to hold the frame counter")

// Status is what the printer this camera watches is doing at the moment a
// frame is drawn.
//
// The zero value is a camera watching nothing, and it draws neither the state
// line nor the progress bar. That is the honest picture for a camera on a shelf
// with no printer behind it: a bar at zero would say a print is running and has
// not started.
type Status struct {
	// State is the printer's own word for what it is doing, such as IDLE or
	// PRINTING. It is drawn in capitals, as the printer spelled it.
	State string
	// Progress is how far the print has gone, in whole percent. It is drawn as
	// a bar whenever State is set, so a machine with nothing on the bed reads as
	// a bar at zero rather than as a bar that is missing.
	Progress int
}

// Options configures one simulated camera. The zero value is usable: every
// field falls back to a default that produces a readable frame.
type Options struct {
	// Addr is the listen address. Empty is "127.0.0.1:0", a free port on
	// loopback, so two tests running at once never pick the same port and no
	// other machine can reach the camera.
	Addr string
	// Width is the frame width in pixels. A width below the 256 pixels the
	// counter readout needs is raised to it, because a frame whose counter
	// cannot be read back is the one thing this simulator must not produce.
	Width int
	// Height is the frame height in pixels.
	Height int
	// FPS is how many frames a second the renderer produces, and so the cadence
	// of the stream. Raise it in a test to keep the test short.
	FPS float64
	// Label is drawn on every frame. Passing the printer id makes a grid of
	// simulated cameras tell you which tile belongs to which printer.
	Label string
	// Quality is the JPEG quality, 1 to 100.
	Quality int
	// Status reports what the printer this camera watches is doing. It is read
	// once per frame on the renderer's goroutine, so it must not block and it
	// must be safe to call from another goroutine.
	//
	// It is a function rather than a value because the machine keeps moving. A
	// state read once at start-up would draw the same picture for the whole run,
	// which is the frozen stream this simulator exists to make visible. Leave it
	// nil for a camera that watches no printer.
	Status func() Status
}

// status reads the printer's state for one frame. A camera that watches no
// printer reports the zero Status, which draws no state line and no bar.
func (o Options) status() Status {
	if o.Status == nil {
		return Status{}
	}

	return o.Status()
}

// normalise fills in the defaults and raises the sizes that would otherwise
// produce a frame nothing can read back. It is a method rather than code inside
// Start so the renderer and the tests work from one set of rules.
func (o Options) normalise() Options {
	if o.Width < minFrameWidth {
		o.Width = defaultWidth
	}

	if o.Height < minFrameHeight {
		o.Height = defaultHeight
	}

	if o.FPS <= 0 {
		o.FPS = defaultFPS
	}

	if o.Quality <= 0 || o.Quality > maxQuality {
		o.Quality = defaultQuality
	}

	if o.Addr == "" {
		o.Addr = defaultAddr
	}

	return o
}

// frame is one rendered image, already encoded. It is immutable once published,
// so a viewer holding a pointer to it can write the bytes out at its own pace
// while the renderer moves on to the next one.
type frame struct {
	jpeg    []byte
	counter uint64
}

// Server is a running simulated MJPEG camera.
//
// One renderer goroutine produces frames on a ticker and every viewer is served
// the latest one. That is the shape a real camera has, and it is also the shape
// this project's own rule demands: nothing in the frame path may allocate per
// subscriber, and a simulator that rendered per viewer would hide the cost of
// getting that wrong.
type Server struct {
	opts     Options
	listener net.Listener
	http     *http.Server

	frames atomic.Uint64

	// mu guards latest, updated and closeErr. updated is closed and replaced
	// every time a frame is published, which is how a viewer waits for the next
	// one without polling and without the server keeping a channel per viewer.
	mu       sync.Mutex
	latest   *frame
	updated  chan struct{}
	closeErr error

	// done is closed by Close and ends every stream in flight.
	done      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

// Start binds the address and serves until the context ends or Close is called.
//
// It renders the first frame before it returns, so a snapshot taken immediately
// after Start always has something to answer with. The alternative is a service
// unavailable on the first request and a retry loop in every caller, which is a
// worse contract than one synchronous frame at startup.
func Start(ctx context.Context, opts Options) (*Server, error) {
	opts = opts.normalise()

	var config net.ListenConfig

	listener, err := config.Listen(ctx, "tcp", opts.Addr)
	if err != nil {
		return nil, fmt.Errorf("camerasim: listen on %q: %w", opts.Addr, err)
	}

	server := &Server{
		opts:     opts,
		listener: listener,
		updated:  make(chan struct{}),
		done:     make(chan struct{}),
	}

	server.http = &http.Server{Handler: http.HandlerFunc(server.route), ReadHeaderTimeout: readHeaderTimeout}

	if publishErr := server.publish(); publishErr != nil {
		_ = listener.Close()

		return nil, publishErr
	}

	server.wg.Add(backgroundGoroutines)

	go server.serve()
	go server.render()

	// The context watcher is deliberately outside the wait group: Close waits
	// for that group, so a watcher inside it that waited for Close would
	// deadlock.
	go func() {
		select {
		case <-ctx.Done():
			_ = server.Close()
		case <-server.done:
		}
	}()

	return server, nil
}

// Addr is the bound address as host:port. With a port of zero in the options
// this is the only way to learn which port the kernel picked.
func (s *Server) Addr() string { return s.listener.Addr().String() }

// SnapshotURL is the URL that answers one JPEG.
func (s *Server) SnapshotURL() string { return "http://" + s.Addr() + snapshotPath }

// StreamURL is the URL that answers the multipart stream.
func (s *Server) StreamURL() string { return "http://" + s.Addr() + streamPath }

// Frames is how many frames the renderer has produced since Start. It is also
// the counter drawn into the most recent frame, which is what lets a test say
// how far behind a viewer is rather than only that it is behind.
func (s *Server) Frames() uint64 { return s.frames.Load() }

// Close stops serving, ends every stream in flight and closes the listener. It
// is safe to call more than once, and safe to call after the context that
// started the server has already ended.
func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)

		if err := s.http.Close(); err != nil {
			s.setCloseErr(fmt.Errorf("camerasim: close server: %w", err))
		}

		s.wg.Wait()
	})

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.closeErr
}

// serve runs the HTTP server. A closed server is the ordinary way this ends, so
// that one error is not worth recording.
func (s *Server) serve() {
	defer s.wg.Done()

	if err := s.http.Serve(s.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.setCloseErr(fmt.Errorf("camerasim: serve: %w", err))
	}
}

// render produces frames on a ticker until the server closes.
func (s *Server) render() {
	defer s.wg.Done()

	interval := time.Duration(float64(time.Second) / s.opts.FPS)
	if interval <= 0 {
		interval = time.Second / defaultFPS
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			if err := s.publish(); err != nil {
				s.setCloseErr(err)

				return
			}
		}
	}
}

// publish renders one frame, encodes it, and hands it to every waiting viewer
// by closing the channel they are parked on.
func (s *Server) publish() error {
	counter := s.frames.Add(1)

	encoded, err := encodeFrame(s.opts, counter, time.Now().UTC())
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.latest = &frame{jpeg: encoded, counter: counter}
	close(s.updated)
	s.updated = make(chan struct{})

	return nil
}

// current returns the latest frame together with the channel that closes when a
// newer one arrives. Both are read under the one lock, so a viewer can never
// take a frame and then park on a channel that has already fired.
func (s *Server) current() (*frame, <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.latest, s.updated
}

// setCloseErr records the first failure a background goroutine hit, so Close
// reports it instead of a caller learning about it from a log line nobody
// reads.
func (s *Server) setCloseErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closeErr == nil {
		s.closeErr = err
	}
}

// route dispatches the three paths this device answers.
//
// It is a switch rather than a net/http mux, and the reason is a gate in the
// core: TestNothingOutsideTheSealedPackageMountsAServeMux refuses
// http.NewServeMux outside the sealed API package, because a second mux beside
// the real router is how an undocumented route reaches the same origin. Nothing
// here is served on that origin, this is a device the server dials, not a
// route it serves, but three paths are not worth an exemption in somebody
// else's table.
func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "this device answers GET only", http.StatusMethodNotAllowed)

		return
	}

	switch r.URL.Path {
	case snapshotPath:
		s.handleSnapshot(w, r)
	case streamPath:
		s.handleStream(w, r)
	default:
		s.handleIndex(w, r)
	}
}

// handleIndex names the two paths, so somebody who opened the root of this
// device in a browser is told where the pictures are.
func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	_, _ = fmt.Fprintf(w, "camerasim\nsnapshot: %s\nstream:   %s\n", snapshotPath, streamPath)
}

func (s *Server) handleSnapshot(w http.ResponseWriter, _ *http.Request) {
	latest, _ := s.current()
	if latest == nil {
		http.Error(w, "no frame yet", http.StatusServiceUnavailable)

		return
	}

	w.Header().Set("Content-Type", mediaTypeJPEG)
	w.Header().Set("Content-Length", strconv.Itoa(len(latest.jpeg)))
	w.Header().Set("Cache-Control", "no-store")

	_, _ = w.Write(latest.jpeg)
}

// handleStream writes one multipart part per rendered frame and flushes after
// each, which is what makes a browser show a frame rather than buffer it. The
// frame already in hand goes out first, so a viewer never waits a whole frame
// interval to see anything.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", cameraapi.FormatMJPEG.MediaType())
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	var sent uint64

	for {
		latest, updated := s.current()
		if latest != nil && latest.counter != sent {
			if err := writePart(w, latest.jpeg); err != nil {
				return
			}

			flusher.Flush()

			sent = latest.counter
		}

		select {
		case <-updated:
		case <-r.Context().Done():
			return
		case <-s.done:
			return
		}
	}
}

// writePart writes one MJPEG part. The boundary comes from the contract rather
// than from a literal here, because a simulator that framed its stream
// differently from every driver would be exercising the wrong thing.
func writePart(w http.ResponseWriter, jpegBytes []byte) error {
	header := fmt.Sprintf(
		"--%s\r\nContent-Type: %s\r\nContent-Length: %d\r\n\r\n",
		cameraapi.MJPEGBoundary, mediaTypeJPEG, len(jpegBytes),
	)

	if _, err := w.Write([]byte(header)); err != nil {
		return fmt.Errorf("camerasim: write part header: %w", err)
	}

	if _, err := w.Write(jpegBytes); err != nil {
		return fmt.Errorf("camerasim: write part body: %w", err)
	}

	if _, err := w.Write([]byte("\r\n")); err != nil {
		return fmt.Errorf("camerasim: write part terminator: %w", err)
	}

	return nil
}

// ReadCounter reads the frame counter back out of the block row of a decoded
// frame.
//
// This is the whole point of the simulator. A test holding a decoded JPEG can
// say which frame it is looking at without optical character recognition,
// without a fixture file, and without trusting a header the host under test
// could have invented. The counter is drawn as 32 blocks along the top edge,
// most significant bit on the left, and is read back by sampling the centre of
// each block and thresholding on luminance: the centre is far enough from every
// edge that the encoder's ringing cannot push it across the threshold.
func ReadCounter(img image.Image) (uint64, error) {
	bounds := img.Bounds()
	if bounds.Dx() < minFrameWidth || bounds.Dy() < blockSize {
		return 0, fmt.Errorf("%w: %dx%d", ErrImageTooSmall, bounds.Dx(), bounds.Dy())
	}

	var counter uint64

	for bit := range counterBits {
		x := bounds.Min.X + bit*blockSize + blockCentre
		y := bounds.Min.Y + blockCentre

		counter <<= 1

		if luminance(img.At(x, y)) >= blockThreshold {
			counter |= 1
		}
	}

	return counter, nil
}

// luminance reduces a pixel to the one channel the readout cares about.
func luminance(c color.Color) uint8 {
	grey, ok := color.GrayModel.Convert(c).(color.Gray)
	if !ok {
		return 0
	}

	return grey.Y
}

// encodeFrame renders one frame and encodes it as JPEG.
func encodeFrame(opts Options, counter uint64, now time.Time) ([]byte, error) {
	var buf bytes.Buffer

	options := jpeg.Options{Quality: opts.Quality}
	if err := jpeg.Encode(&buf, renderFrame(opts, opts.status(), counter, now), &options); err != nil {
		return nil, fmt.Errorf("camerasim: encode frame %d: %w", counter, err)
	}

	return buf.Bytes(), nil
}
