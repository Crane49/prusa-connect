package cameraapi_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/cameraapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// frameKeeper is a host that keeps the last frame each camera was handed.
type frameKeeper struct {
	mu     sync.Mutex
	held   map[string][]byte
	saw    cameraapi.AcceptParams
	refuse error
	// bar refuses a call at Admit, before its bytes are read.
	bar      error
	accepted int
}

func (k *frameKeeper) Admit(_ context.Context, _ cameraapi.AcceptParams) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	return k.bar
}

func (k *frameKeeper) Accept(_ context.Context, params cameraapi.AcceptParams, frame []byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	k.saw = params
	k.accepted++

	if k.refuse != nil {
		return k.refuse
	}

	if k.held == nil {
		k.held = map[string][]byte{}
	}

	k.held[params.Camera] = bytes.Clone(frame)

	return nil
}

func (k *frameKeeper) frame(camera string) []byte {
	k.mu.Lock()
	defer k.mu.Unlock()

	return k.held[camera]
}

type pipeEnd struct {
	reader *os.File
	writer *os.File
}

func (c pipeEnd) Read(p []byte) (int, error)  { return c.reader.Read(p) }
func (c pipeEnd) Write(p []byte) (int, error) { return c.writer.Write(p) }

// serveFrames runs a host serving camera.accept over frames, and answers the
// child's session.
func serveFrames(tb testing.TB, frames cameraapi.Frames) *wirerpc.Conn {
	tb.Helper()

	hostReads, childWrites, err := os.Pipe()
	if err != nil {
		tb.Fatalf("open a pipe: %v", err)
	}

	childReads, hostWrites, err := os.Pipe()
	if err != nil {
		tb.Fatalf("open a pipe: %v", err)
	}

	host, err := wirerpc.NewConn(pipeEnd{reader: hostReads, writer: hostWrites},
		wirerpc.Options{Role: wirerpc.RoleHost, Methods: cameraapi.Handlers(frames)})
	if err != nil {
		tb.Fatalf("build the host: %v", err)
	}

	child, err := wirerpc.NewConn(pipeEnd{reader: childReads, writer: childWrites},
		wirerpc.Options{Role: wirerpc.RoleChild})
	if err != nil {
		tb.Fatalf("build the child: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	var running sync.WaitGroup

	running.Add(2)

	go func() { defer running.Done(); _ = host.Serve(ctx) }()
	go func() { defer running.Done(); _ = child.Serve(ctx) }()

	tb.Cleanup(func() {
		cancel()
		_ = host.Close()
		_ = child.Close()
		_ = hostWrites.Close()
		_ = childWrites.Close()
		running.Wait()
	})

	return child
}

// jpeg returns size bytes that start the way a JPEG does.
func jpeg(size int) []byte {
	frame := bytes.Repeat([]byte{0x5A}, size)
	copy(frame, []byte{0xFF, 0xD8, 0xFF})

	return frame
}

// A frame larger than one data frame and one credit window crosses whole.
func TestAFrameCrossesTheSessionWhole(t *testing.T) {
	t.Parallel()

	host := &frameKeeper{}
	cameras := cameraapi.NewClient(serveFrames(t, host))
	frame := jpeg(3 << 20)

	if err := cameras.Accept(t.Context(), "", "bed-cam", frame); err != nil {
		t.Fatalf("accept: %v", err)
	}

	if got := host.frame("bed-cam"); !bytes.Equal(got, frame) {
		t.Fatalf("the host kept %d bytes, want the %d sent", len(got), len(frame))
	}

	if host.saw.Size != int64(len(frame)) || host.saw.Principal != "" {
		t.Fatalf("the host saw %+v", host.saw)
	}
}

// A refusal arrives as the sentinel the host raised, and the session still
// carries the next frame.
func TestARefusalKeepsItsSentinel(t *testing.T) {
	t.Parallel()

	for _, refuse := range []error{cameraapi.ErrDenied, cameraapi.ErrNoCamera, cameraapi.ErrInvalidSnapshot} {
		host := &frameKeeper{refuse: fmt.Errorf("%w: because", refuse)}
		cameras := cameraapi.NewClient(serveFrames(t, host))

		if err := cameras.Accept(t.Context(), "", "bed-cam", jpeg(64)); !errors.Is(err, refuse) {
			t.Fatalf("accept refused with %v: got %v", refuse, err)
		}

		host.mu.Lock()
		host.refuse = nil
		host.mu.Unlock()

		if err := cameras.Accept(t.Context(), "", "bed-cam", jpeg(64)); err != nil {
			t.Fatalf("the next frame: %v", err)
		}
	}
}

// A host failure of its own reads as a fixed sentence and no sentinel.
func TestAHostFailureNamesNoInternals(t *testing.T) {
	t.Parallel()

	host := &frameKeeper{refuse: errors.New("disk /var/lib/secret is full")}
	cameras := cameraapi.NewClient(serveFrames(t, host))

	err := cameras.Accept(t.Context(), "", "bed-cam", jpeg(64))
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("got %v", err)
	}
}

// A call outside the bounds is refused before anything is sent.
func TestAFrameOutsideTheBoundsIsRefused(t *testing.T) {
	t.Parallel()

	host := &frameKeeper{}
	cameras := cameraapi.NewClient(serveFrames(t, host))

	if err := cameras.Accept(t.Context(), "", "bed-cam", nil); !errors.Is(err, cameraapi.ErrInvalidSnapshot) {
		t.Fatalf("an empty frame: %v", err)
	}

	err := cameras.Accept(t.Context(), "", "bed-cam", jpeg(cameraapi.MaxAcceptBytes+1))
	if !errors.Is(err, cameraapi.ErrInvalidSnapshot) {
		t.Fatalf("a frame over the bound: %v", err)
	}

	if err = cameras.Accept(t.Context(), "", "", jpeg(8)); !errors.Is(err, cameraapi.ErrNoCamera) {
		t.Fatalf("no camera: %v", err)
	}

	if host.frame("bed-cam") != nil {
		t.Fatal("a refused frame reached the host")
	}
}

// A stream that carries fewer bytes than the call named stores nothing, which
// a module that does not use Client could still send.
func TestAShortStreamStoresNothing(t *testing.T) {
	t.Parallel()

	host := &frameKeeper{}
	child := serveFrames(t, host)

	stream, err := child.Open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer stream.Close()

	go func() {
		var held [1]byte
		if _, readErr := stream.Read(held[:]); readErr == nil {
			_, _ = stream.Write(jpeg(10))
			_ = stream.CloseWrite()
		}
	}()

	_, err = stream.Call(t.Context(), cameraapi.MethodAccept, cameraapi.AcceptParams{Camera: "bed-cam", Size: 100})

	coded, ok := errors.AsType[*wirerpc.Error](err)
	if !ok || coded.Code != cameraapi.CodeShortFrame {
		t.Fatalf("got %v, want code %d", err, cameraapi.CodeShortFrame)
	}

	if host.frame("bed-cam") != nil {
		t.Fatal("a short frame reached the host")
	}
}

// Many small frames side by side all end. A frame of a few bytes is sent at
// once, so its end of stream reaches the host about when the call does.
func TestManySmallFramesAllEnd(t *testing.T) {
	t.Parallel()

	host := &frameKeeper{}
	cameras := cameraapi.NewClient(serveFrames(t, host))

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	var group sync.WaitGroup

	for index := range 200 {
		group.Go(func() {
			if err := cameras.Accept(ctx, "", "cam-"+strconv.Itoa(index), jpeg(10)); err != nil {
				t.Errorf("frame %d: %v", index, err)
			}
		})
	}

	group.Wait()
}

// A host that serves no camera installs no method.
func TestNoFramesIsNoMethod(t *testing.T) {
	t.Parallel()

	if got := cameraapi.Handlers(nil); len(got) != 0 {
		t.Fatalf("got %d methods", len(got))
	}
}

// A call the host refuses at Admit is answered before the module sends a byte,
// so a module with no right to the camera cannot make the host read a frame.
func TestAnUnadmittedFrameIsNeverRead(t *testing.T) {
	t.Parallel()

	host := &frameKeeper{bar: fmt.Errorf("%w: no operate", cameraapi.ErrDenied)}
	cameras := cameraapi.NewClient(serveFrames(t, host))

	if err := cameras.Accept(t.Context(), "", "bed-cam", jpeg(64)); !errors.Is(err, cameraapi.ErrDenied) {
		t.Fatalf("accept: got %v, want ErrDenied", err)
	}

	host.mu.Lock()
	defer host.mu.Unlock()

	if host.accepted != 0 {
		t.Fatalf("the host read %d frames it had refused", host.accepted)
	}
}
