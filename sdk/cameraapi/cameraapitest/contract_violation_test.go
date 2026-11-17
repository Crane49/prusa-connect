package cameraapitest

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/cameraapi"
)

// A conformance suite that has never been watched failing is a suite that
// proves nothing. Each case here is a camera that breaks one clause, run
// through the whole of RunCameraContract, with the run required to fail and to
// name the clause.
//
// The run happens in a child copy of this test binary. A subtest that fails
// fails its parent, so there is no way to assert "this run failed" in-process,
// and the camera whose Close never releases a reader has to be watched failing
// rather than hanging the binary, which is the other half of what is proved.
const (
	violatorEnv     = "CAMERAAPITEST_VIOLATOR"
	violatorTest    = "TestTheSuiteAgainstAViolatingCamera"
	childRunTimeout = 2 * time.Minute

	// deadlockReport is how the runtime ends a process whose every goroutine is
	// blocked.
	deadlockReport = "all goroutines are asleep"
)

// violators are cameras that each break exactly one clause, with the words the
// suite must say when it catches them.
//
//nolint:gochecknoglobals // a fixture table the child process reads by name.
var violators = map[string]struct {
	build func() cameraapi.Camera
	names string
}{
	"declares-nothing": {
		build: func() cameraapi.Camera { return &camera{declaresNothing: true} },
		names: "the camera declares no format",
	},
	"declares-an-unknown-format": {
		build: func() cameraapi.Camera { return &camera{declaresUnknown: true} },
		names: "is not one of",
	},
	"changes-its-declared-set": {
		build: func() cameraapi.Camera { return &camera{shuffles: true} },
		names: "a declared set the host cannot rely on",
	},
	"hands-back-its-own-slice": {
		build: func() cameraapi.Camera { return &camera{sharesTheSlice: true} },
		names: "hands back the driver's own slice",
	},
	"serves-an-undeclared-format": {
		build: func() cameraapi.Camera { return &camera{opensAnything: true} },
		names: "for a format Formats() does not declare",
	},
	"reports-a-plain-error-for-an-unsupported-format": {
		build: func() cameraapi.Camera { return &camera{plainRefusal: true} },
		names: "cameraapi.ErrFormatNotSupported",
	},
	"answers-a-snapshot-that-is-not-an-image": {
		build: func() cameraapi.Camera { return &camera{wrongMediaType: true} },
		names: "an image the host cannot serve",
	},
	"stamps-nothing-on-a-snapshot": {
		build: func() cameraapi.Camera { return &camera{noStamp: true} },
		names: "every conditional GET against this camera transfers",
	},
	"frames-a-stream-as-something-else": {
		build: func() cameraapi.Camera { return &camera{wrongStreamType: true} },
		names: "and the format means",
	},
	"never-releases-a-blocked-read": {
		build: func() cameraapi.Camera { return &camera{closeLeavesTheReader: true} },
		names: "still blocked",
	},
	"panics-on-the-second-close": {
		build: func() cameraapi.Camera { return &camera{closeTwicePanics: true} },
		names: "panic",
	},
	"leaks-a-goroutine": {
		build: func() cameraapi.Camera { return &camera{leaks: true} },
		names: "across build and Close",
	},
}

func TestTheSuiteFailsEveryViolatingCamera(t *testing.T) {
	t.Parallel()

	if os.Getenv(violatorEnv) != "" {
		t.Skip("this is the child run; it exists to be failed by the parent's case")
	}

	for name, tc := range violators {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			output, err := runChild(t, name)
			if err == nil {
				t.Fatalf("the suite passed a camera that %s:\n%s", name, output)
			}

			if !strings.Contains(output, tc.names) {
				t.Errorf("the suite failed the camera but never said %q, so the author is not told "+
					"which clause broke:\n%s", tc.names, output)
			}
		})
	}
}

// The same camera with every defect off must pass, or the case above proves
// only that the suite dislikes something.
func TestTheSuitePassesAConformingCamera(t *testing.T) {
	t.Parallel()

	if os.Getenv(violatorEnv) != "" {
		t.Skip("this is the child run")
	}

	output, err := runChild(t, "conforming")
	if err != nil {
		t.Fatalf("the suite failed a camera that keeps the contract: %v\n%s", err, output)
	}
}

// TestTheSuiteAgainstAViolatingCamera is the child. It runs the whole contract
// against the camera its parent named and is expected to fail.
func TestTheSuiteAgainstAViolatingCamera(t *testing.T) {
	name := os.Getenv(violatorEnv)
	if name == "" {
		t.Skip("no violator named: this case only runs as a child of TestTheSuiteFailsEveryViolatingCamera")
	}

	if name == "conforming" {
		RunCameraContract(t, func(*testing.T) cameraapi.Camera { return &camera{} })

		return
	}

	tc, ok := violators[name]
	if !ok {
		t.Fatalf("no violator named %q", name)
	}

	RunCameraContract(t, func(*testing.T) cameraapi.Camera { return tc.build() })
}

func runChild(t *testing.T, name string) (string, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), childRunTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run="+violatorTest, "-test.v")
	cmd.Env = append(os.Environ(), violatorEnv+"="+name)

	output, err := cmd.CombinedOutput()

	// A child that hangs also exits non-zero in the end, which the cases above
	// would read as the suite failing the fixture. It is the suite hanging on
	// it, which the bounded waits in the suite are there to prevent, and it has
	// to say so. It ends one of two ways: killed at the deadline, or, on a
	// machine idle enough that no timer is pending, by the runtime's deadlock
	// detector. Which one depends on the machine.
	if ctx.Err() != nil {
		t.Fatalf("the suite hung on the %s fixture until it was killed after %s, instead of "+
			"failing it:\n%s", name, childRunTimeout, output)
	}

	if strings.Contains(string(output), deadlockReport) {
		t.Fatalf("the suite hung on the %s fixture until the runtime reported a deadlock, "+
			"instead of failing it:\n%s", name, output)
	}

	return string(output), err
}

// still is the smallest byte sequence a JPEG reader accepts as a start of
// image. Nothing here decodes one.
//
//nolint:gochecknoglobals // a fixture constant; a byte slice cannot be one.
var still = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0xFF, 0xD9}

// camera is a conforming camera with a switch for each clause it can be made to
// break. Nothing it returns comes from a device, because what the suite asserts
// is how a driver behaves and not what it is pointed at.
type camera struct {
	declaresNothing      bool
	declaresUnknown      bool
	shuffles             bool
	sharesTheSlice       bool
	opensAnything        bool
	plainRefusal         bool
	wrongMediaType       bool
	noStamp              bool
	wrongStreamType      bool
	closeLeavesTheReader bool
	closeTwicePanics     bool
	leaks                bool

	formats []cameraapi.Format

	mu     sync.Mutex
	closed bool
	live   []*frames
	asked  int
}

func (c *camera) Formats() []cameraapi.Format {
	switch {
	case c.declaresNothing:
		return nil
	case c.declaresUnknown:
		return []cameraapi.Format{cameraapi.FormatJPEG, "avif"}
	}

	formats := c.declaredFormats()

	if c.shuffles {
		c.mu.Lock()
		c.asked++
		odd := c.asked%2 == 1
		c.mu.Unlock()

		if odd {
			return []cameraapi.Format{cameraapi.FormatJPEG}
		}
	}

	if c.sharesTheSlice {
		return formats
	}

	return append([]cameraapi.Format(nil), formats...)
}

// declaredFormats is the set this fixture declares, built on the first call.
// The build is under the lock because the contract's concurrency clause calls
// Formats from eight goroutines at once, and a fixture that races there fails
// the suite it exists to hold. It is the same slice on every call, which is
// what the sharesTheSlice defect needs to be observable.
func (c *camera) declaredFormats() []cameraapi.Format {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.formats == nil {
		c.formats = []cameraapi.Format{cameraapi.FormatJPEG, cameraapi.FormatMJPEG}
	}

	return c.formats
}

func (c *camera) Snapshot(context.Context) (cameraapi.Snapshot, error) {
	shot := cameraapi.Snapshot{
		Bytes:     still,
		MediaType: cameraapi.FormatJPEG.MediaType(),
		Taken:     time.Now().UTC(),
	}

	if c.wrongMediaType {
		shot.MediaType = "text/html"
	}

	if c.noStamp {
		shot.Taken = time.Time{}
	}

	return shot, nil
}

func (c *camera) Open(_ context.Context, format cameraapi.Format) (cameraapi.Stream, error) {
	if format != cameraapi.FormatMJPEG && !c.opensAnything {
		if c.plainRefusal {
			return cameraapi.Stream{}, errors.New("cannot do that")
		}

		return cameraapi.Stream{}, cameraapi.ErrFormatNotSupported
	}

	mediaType := cameraapi.FormatMJPEG.MediaType()
	if c.wrongStreamType {
		mediaType = "video/mp4"
	}

	body := &frames{released: make(chan struct{}), stuck: c.closeLeavesTheReader}

	c.mu.Lock()
	c.live = append(c.live, body)
	c.mu.Unlock()

	if c.leaks {
		go func() { time.Sleep(time.Hour) }()
	}

	return cameraapi.Stream{Body: body, MediaType: mediaType}, nil
}

func (c *camera) Close() error {
	c.mu.Lock()
	wasClosed := c.closed
	c.closed = true
	live := c.live
	c.live = nil
	c.mu.Unlock()

	if wasClosed && c.closeTwicePanics {
		panic("closed twice")
	}

	for _, body := range live {
		_ = body.Close()
	}

	return nil
}

// frames is one viewer's stream. It delivers forever until it is closed, which
// is what a live view is.
type frames struct {
	released  chan struct{}
	stuck     bool
	closeOnce sync.Once
}

func (f *frames) Read(p []byte) (int, error) {
	select {
	case <-f.released:
		return 0, io.EOF
	case <-time.After(time.Millisecond):
	}

	return copy(p, still), nil
}

func (f *frames) Close() error {
	if f.stuck {
		return nil
	}

	f.closeOnce.Do(func() { close(f.released) })

	return nil
}
