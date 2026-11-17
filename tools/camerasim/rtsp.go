package camerasim

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"time"
)

// The refusals the RTSP helper reports. Callers inspect them with errors.Is,
// and every one of them means "not available here", never "broken".
var (
	// ErrNoRTSPTool reports a machine with neither mediamtx nor ffmpeg on PATH.
	// RTSP is a convenience for the manual lane, so this is a fact to report and
	// not a failure to escalate.
	ErrNoRTSPTool = errors.New("camerasim: neither mediamtx nor ffmpeg is on PATH")
	// ErrNoRTSPServer reports ffmpeg present with nowhere to publish. ffmpeg can
	// encode and push a stream but it cannot accept a viewer, so an RTSP server
	// has to exist first: install mediamtx, or pass the URL of a server you
	// already run.
	ErrNoRTSPServer = errors.New("camerasim: publishing needs an RTSP server; install mediamtx or pass an RTSP URL")
)

const (
	// defaultRTSPPort is mediamtx's own default, which is what makes running it
	// with no configuration file work at all.
	defaultRTSPPort = "8554"
	defaultRTSPPath = "camerasim"
	// rtspReadyTimeout bounds the wait for a freshly started mediamtx to accept
	// connections. ffmpeg started before that fails immediately rather than
	// retrying, so the wait is the difference between working and not.
	rtspReadyTimeout = 5 * time.Second
	rtspReadyPoll    = 100 * time.Millisecond
	rtspDialTimeout  = 250 * time.Millisecond
)

// RTSPSource is a running H.264 RTSP publication of a simulated camera.
type RTSPSource struct {
	url      string
	ffmpeg   *exec.Cmd
	mediamtx *exec.Cmd
}

// RTSPAvailable reports the path of an external tool that can serve RTSP, and
// whether one was found. It looks for mediamtx then ffmpeg on PATH.
//
// It is exported so a test can skip rather than fail on a machine without the
// tools. Nothing in this package requires RTSP, and nothing should start doing
// so: it is here because a WebRTC or ONVIF path eventually needs a real H.264
// source to point at, and standing one up by hand each time is the sort of
// friction that stops people testing.
func RTSPAvailable() (string, bool) {
	for _, name := range []string{"mediamtx", "ffmpeg"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, true
		}
	}

	return "", false
}

// StartRTSP starts an H.264 RTSP source in front of a running MJPEG server,
// using ffmpeg to read the MJPEG stream and publish to an RTSP URL, and reports
// the URL it published to.
//
// With an empty rtspURL it starts mediamtx on its default port and publishes
// there, which is the case that needs no setup at all. With a URL it publishes
// to a server the caller already runs and starts nothing else. It reports
// ErrNoRTSPTool when neither tool is on PATH, and ErrNoRTSPServer when ffmpeg is
// present but there is nowhere to publish.
//
// The transcode is deliberately the cheapest one ffmpeg offers. The point is a
// stream that exists, not a stream that looks good.
func StartRTSP(ctx context.Context, mjpeg *Server, rtspURL string) (*RTSPSource, error) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, ErrNoRTSPTool
	}

	var mediamtx *exec.Cmd

	if rtspURL == "" {
		mediamtxPath, lookErr := exec.LookPath("mediamtx")
		if lookErr != nil {
			return nil, ErrNoRTSPServer
		}

		address := net.JoinHostPort("127.0.0.1", defaultRTSPPort)
		rtspURL = "rtsp://" + address + "/" + defaultRTSPPath

		// #nosec G204 -- the path comes from exec.LookPath of a fixed name and
		// the command takes no arguments.
		mediamtx = exec.CommandContext(ctx, mediamtxPath)
		if startErr := mediamtx.Start(); startErr != nil {
			return nil, fmt.Errorf("camerasim: start mediamtx: %w", startErr)
		}

		if readyErr := waitForRTSP(ctx, address); readyErr != nil {
			_ = mediamtx.Process.Kill()

			return nil, readyErr
		}
	}

	// #nosec G204 -- the path comes from exec.LookPath of a fixed name; the two
	// variable arguments are this server's own loopback URL and a URL the
	// caller chose for its own process.
	ffmpeg := exec.CommandContext(ctx, ffmpegPath,
		"-hide_banner", "-loglevel", "error",
		"-f", "mjpeg", "-i", mjpeg.StreamURL(),
		"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
		"-pix_fmt", "yuv420p",
		"-f", "rtsp", "-rtsp_transport", "tcp", rtspURL,
	)

	if startErr := ffmpeg.Start(); startErr != nil {
		if mediamtx != nil {
			_ = mediamtx.Process.Kill()
		}

		return nil, fmt.Errorf("camerasim: start ffmpeg: %w", startErr)
	}

	return &RTSPSource{url: rtspURL, ffmpeg: ffmpeg, mediamtx: mediamtx}, nil
}

// URL is the RTSP URL a player can open.
func (r *RTSPSource) URL() string { return r.url }

// Close stops ffmpeg and, if this source started one, mediamtx. Both are killed
// rather than signalled: they hold no state worth flushing, and a transcoder
// asked politely to stop can take longer than the process that is waiting.
func (r *RTSPSource) Close() error {
	var errs []error

	for _, cmd := range []*exec.Cmd{r.ffmpeg, r.mediamtx} {
		if cmd == nil || cmd.Process == nil {
			continue
		}

		if err := cmd.Process.Kill(); err != nil {
			errs = append(errs, err)
		}

		// The exit status of a process this code just killed says nothing, so
		// only the wait matters: it is what reaps the child.
		_ = cmd.Wait()
	}

	if len(errs) > 0 {
		return fmt.Errorf("camerasim: stop rtsp source: %w", errors.Join(errs...))
	}

	return nil
}

// waitForRTSP blocks until the freshly started server accepts a connection.
func waitForRTSP(ctx context.Context, address string) error {
	deadline := time.Now().Add(rtspReadyTimeout)

	for time.Now().Before(deadline) {
		var dialer net.Dialer

		dialer.Timeout = rtspDialTimeout

		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err == nil {
			_ = conn.Close()

			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("camerasim: wait for rtsp server: %w", ctx.Err())
		case <-time.After(rtspReadyPoll):
		}
	}

	return fmt.Errorf("camerasim: rtsp server at %s did not accept a connection: %w", address, ErrNoRTSPServer)
}
