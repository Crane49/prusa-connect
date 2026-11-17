// Command camerasim serves one simulated MJPEG camera on a loopback port.
//
// It answers /snapshot.jpg with a single JPEG and /stream.mjpg with a multipart
// stream framed the way the camera contract fixes, so a driver, a browser or
// ffmpeg can be pointed at it without a device on the desk. Every frame carries
// a frame counter drawn as machine-readable blocks along the top edge, which is
// what lets a test say whether the image it is looking at is the current one.
//
// Run one camera for a manual look at the frontend:
//
//	camerasim -addr 127.0.0.1:8091 -label MK4-01
//
// Or a faster one with an RTSP publication in front of it, for the paths that
// need H.264 rather than MJPEG:
//
//	camerasim -addr 127.0.0.1:8092 -fps 15 -width 640 -height 480 -rtsp
//
// The -rtsp flag is best effort. It needs mediamtx and ffmpeg on PATH, and when
// they are missing the MJPEG camera still runs and the reason is printed.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/prusa3d/prusa-connect/tools/camerasim"
)

const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

// errUsage marks a failure the operator caused by how they invoked the command,
// which is the difference between exit code 2 and exit code 1.
var errUsage = errors.New("invalid flags")

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "camerasim:", err)

		if errors.Is(err, errUsage) {
			os.Exit(exitUsage)
		}

		os.Exit(exitFailure)
	}

	os.Exit(exitOK)
}

func run() error {
	var (
		opts camerasim.Options
		rtsp bool
	)

	flag.StringVar(&opts.Addr, "addr", "127.0.0.1:8091", "listen address")
	flag.Float64Var(&opts.FPS, "fps", 5, "frames per second on the stream")
	flag.IntVar(&opts.Width, "width", 320, "frame width in pixels")
	flag.IntVar(&opts.Height, "height", 240, "frame height in pixels")
	flag.StringVar(&opts.Label, "label", "", "text drawn on every frame, such as the printer id")
	flag.BoolVar(&rtsp, "rtsp", false, "also publish H.264 over RTSP, if mediamtx and ffmpeg are on PATH")
	flag.Parse()

	if flag.NArg() > 0 {
		return fmt.Errorf("%w: unexpected argument %q", errUsage, flag.Arg(0))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server, err := camerasim.Start(ctx, opts)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "snapshot: %s\n", server.SnapshotURL())
	fmt.Fprintf(os.Stdout, "stream:   %s\n", server.StreamURL())

	if rtsp {
		defer startRTSP(ctx, server)()
	}

	<-ctx.Done()

	return server.Close()
}

// startRTSP publishes the camera over RTSP and returns the function that stops
// it again. A missing tool is reported and nothing else happens: the MJPEG
// camera is the product here and RTSP is the extra.
func startRTSP(ctx context.Context, server *camerasim.Server) func() {
	source, err := camerasim.StartRTSP(ctx, server, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "camerasim: rtsp not started:", err)

		return func() {}
	}

	fmt.Fprintf(os.Stdout, "rtsp:     %s\n", source.URL())

	return func() {
		if closeErr := source.Close(); closeErr != nil {
			fmt.Fprintln(os.Stderr, "camerasim:", closeErr)
		}
	}
}
