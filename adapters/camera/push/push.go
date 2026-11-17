// Package push is the driver for a camera this server cannot dial.
//
// The camera sends its own frame with PUT /api/v1/cameras/{camera}/snapshot,
// and this driver keeps the last one. A snapshot is the bytes the device sent.
// A stream is those bytes framed with the boundary cameraapi fixes. Nothing is
// decoded.
//
// It is a reference implementation of cameraapi.Receiver.
package push

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/cameraapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// Type is the `type` of a camera instance that selects this driver.
const Type = "push"

const (
	// defaultStale is how long a pushed frame is served after it arrived.
	// After that the camera reports no frame, so a camera that stopped
	// sending does not show its last picture forever.
	defaultStale = 5 * time.Minute

	// pollInterval is how often a viewer's stream looks for a new frame.
	pollInterval = 100 * time.Millisecond
)

// ErrStaleRange rejects a hold time that is not a duration above zero.
var ErrStaleRange = errors.New("push: config.stale is a duration greater than zero")

//nolint:gochecknoinits // registration has to happen before main; the host reaches it through a blank import.
func init() { cameraapi.Register(Type, doc(), New) }

// doc is this driver's entry in the published extension reference.
func doc() extension.Doc {
	return extension.Doc{
		Summary: "Holds the frames a camera pushes to this server, for a camera nothing here can dial.",
		Description: "A camera behind a router or on a network this server has no route to cannot be " +
			"read, so it sends its own frame to PUT /api/v1/cameras/{camera}/snapshot and this driver " +
			"holds the last one. The snapshot and the stream paths then serve it exactly as they " +
			"serve a frame the host fetched. Nothing is decoded or re-encoded. A camera that stops " +
			"sending is reported as one that has produced no frame, rather than showing its last " +
			"picture forever.",
		Version:   "1.0.0",
		Contract:  cameraapi.Contract,
		Stability: extension.StabilityStable,
		Options: []extension.Option{
			{
				Key:     "stale",
				Type:    extension.OptionDuration,
				Default: "5m",
				Summary: "How long a pushed frame is served after it arrived. Past it this camera answers as one that has produced nothing.",
			},
		},
	}
}

// Camera implements cameraapi.Camera and cameraapi.Receiver. It keeps one
// frame. The host decides who may see it.
type Camera struct {
	stale time.Duration

	mu   sync.Mutex
	held cameraapi.Snapshot
	// sequence counts the frames received. A viewer's stream writes a frame
	// only when it changes.
	sequence uint64

	closeOnce sync.Once
	closed    chan struct{}
}

var (
	_ cameraapi.Camera   = (*Camera)(nil)
	_ cameraapi.Paced    = (*Camera)(nil)
	_ cameraapi.Receiver = (*Camera)(nil)
)

// New builds a Camera from its spec.
func New(spec cameraapi.ProviderSpec) (cameraapi.Camera, error) {
	stale, err := staleOf(spec.Config)
	if err != nil {
		return nil, err
	}

	return &Camera{stale: stale, closed: make(chan struct{})}, nil
}

// staleOf reads the hold time. A value that is not a duration string is an
// error, not the default.
func staleOf(config map[string]any) (time.Duration, error) {
	value, declared := config["stale"]
	if !declared {
		return defaultStale, nil
	}

	written, isString := value.(string)
	if !isString {
		return 0, fmt.Errorf("%w: it is a %T", ErrStaleRange, value)
	}

	if written == "" {
		return defaultStale, nil
	}

	hold, err := time.ParseDuration(written)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrStaleRange, err)
	}

	if hold <= 0 {
		return 0, fmt.Errorf("%w: it is %s", ErrStaleRange, written)
	}

	return hold, nil
}

// Formats is what this camera can emit. It declares both formats before any
// frame has arrived.
func (c *Camera) Formats() []cameraapi.Format {
	return []cameraapi.Format{cameraapi.FormatJPEG, cameraapi.FormatMJPEG}
}

// MinInterval is the poll interval. The device decides how often it sends, but
// a frame reaches a viewer no sooner than the next poll.
func (c *Camera) MinInterval() time.Duration { return pollInterval }

// Accept stores one still image as this camera's most recent frame. It
// validates the bytes again, because not every caller is the host.
func (c *Camera) Accept(_ context.Context, shot cameraapi.Snapshot) error {
	if err := shot.Validate(); err != nil {
		return err
	}

	if shot.Taken.IsZero() {
		shot.Taken = time.Now().UTC()
	}

	select {
	case <-c.closed:
		return fmt.Errorf("%w: the camera is closed", cameraapi.ErrPushNotSupported)
	default:
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.held = shot
	c.sequence++

	return nil
}

// Snapshot returns the frame this camera was last given. It returns ErrNoFrame
// when there is none or the last one is older than the hold time.
func (c *Camera) Snapshot(_ context.Context) (cameraapi.Snapshot, error) {
	held, _, ok := c.frame()
	if !ok {
		return cameraapi.Snapshot{}, cameraapi.ErrNoFrame
	}

	return held, nil
}

// Open starts a stream of the frames this camera is given. One goroutine per
// viewer writes the held frame whenever it changes. The stream outlives the
// call, so it ignores the caller's context. Closing the body or Close ends it.
func (c *Camera) Open(_ context.Context, format cameraapi.Format) (cameraapi.Stream, error) {
	if format != cameraapi.FormatMJPEG {
		return cameraapi.Stream{}, fmt.Errorf("%w: this camera emits %v",
			cameraapi.ErrFormatNotSupported, c.Formats())
	}

	reader, writer := io.Pipe()

	go c.write(writer)

	return cameraapi.Stream{Body: reader, MediaType: cameraapi.FormatMJPEG.MediaType()}, nil
}

// Close releases the camera and ends every stream open on it.
func (c *Camera) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })

	return nil
}

// frame returns the held snapshot, its sequence number, and whether it is
// fresh enough to serve.
func (c *Camera) frame() (cameraapi.Snapshot, uint64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sequence == 0 {
		return cameraapi.Snapshot{}, 0, false
	}

	if time.Since(c.held.Taken) > c.stale {
		return cameraapi.Snapshot{}, c.sequence, false
	}

	return c.held, c.sequence, true
}

// write feeds one viewer until a write to it fails or the camera closes.
func (c *Camera) write(writer *io.PipeWriter) {
	defer func() { _ = writer.Close() }()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var written uint64

	for {
		held, sequence, ok := c.frame()

		if ok && sequence != written {
			if err := writePart(writer, held); err != nil {
				return
			}

			written = sequence
		}

		select {
		case <-c.closed:
			return
		case <-ticker.C:
		}
	}
}

// writePart writes one multipart part with the boundary cameraapi fixes.
// mime/multipart is not used because it would pick its own boundary.
func writePart(writer io.Writer, shot cameraapi.Snapshot) error {
	header := "--" + cameraapi.MJPEGBoundary + "\r\n" +
		"Content-Type: " + shot.MediaType + "\r\n" +
		"Content-Length: " + strconv.Itoa(len(shot.Bytes)) + "\r\n\r\n"

	if _, err := io.WriteString(writer, header); err != nil {
		return err
	}

	if _, err := writer.Write(shot.Bytes); err != nil {
		return err
	}

	_, err := io.WriteString(writer, "\r\n")

	return err
}
