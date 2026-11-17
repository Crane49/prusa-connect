package supervised

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/cameraapi"
)

// ErrNotACamera reports an instance that cannot serve media: one the host is
// not supervising, one of another kind, or one whose target does not satisfy
// the camera contract.
var ErrNotACamera = errors.New("supervised: the instance is not a camera")

// Bound is a camera and the printer the operator bound it to. A camera type's
// constructor returns it, so the binding stays with the instance and not in a
// side table the reconciler would have to keep in step.
//
// The host reads the binding from the declaration. The driver never supplies
// it. The binding is the authorization key: whoever may read that printer's
// telemetry may see this camera, so a driver that reported it would be choosing
// who sees its frames.
type Bound struct {
	// Camera is the driver.
	Camera cameraapi.Camera
	// Printer is the printer this camera watches.
	Printer string
}

// Close releases the driver. The supervisor calls it when the instance stops.
func (b Bound) Close() error {
	if b.Camera == nil {
		return nil
	}

	return b.Camera.Close()
}

// Camera is a supervised camera.
//
// Every media call goes through the instance's bulkhead, so one slow device
// uses up its own slots and nobody else's, and a camera that is not serving is
// refused at the gate and never reaches a socket. The instance is type-erased,
// because the host that reconciles configuration supervises every kind through
// one loop. So the assertion back to the contract happens per call, inside the
// supervised span, and a type declared under the camera kind that builds
// something else fails as a counted call and not as a panic on a viewer's
// goroutine.
type Camera struct {
	instance *supervisor.Instance[any]
}

// AttachCamera wraps an instance the host is already supervising. That is how a
// camera declared under resources: is served without a second supervision loop
// over one name.
func AttachCamera(instance *supervisor.Instance[any]) (*Camera, error) {
	if instance == nil {
		return nil, fmt.Errorf("%w: no instance", ErrNotACamera)
	}

	if kind := instance.Ref().Kind(); kind != ref.KindCamera {
		return nil, fmt.Errorf("%w: %s is a %s", ErrNotACamera, instance.Ref(), kind)
	}

	return &Camera{instance: instance}, nil
}

// Instance returns the supervised instance, for the host that runs it and the
// API that reports on it.
func (c *Camera) Instance() *supervisor.Instance[any] { return c.instance }

// Ref returns the instance name, which is the name an operator wrote and the
// name every log line and metric label carries.
func (c *Camera) Ref() ref.Ref { return c.instance.Ref() }

// Printer is the printer this camera watches, and the empty string for an
// instance that is not serving. An empty answer means no principal can be
// granted this camera. That is the safe answer: a camera whose binding cannot
// be read stays invisible instead of visible to everybody.
func (c *Camera) Printer() string {
	printer, ok := supervisor.Peek(c.instance, func(target any) string {
		bound, isBound := target.(Bound)
		if !isBound {
			return ""
		}

		return bound.Printer
	})

	if !ok {
		return ""
	}

	return printer
}

// Formats is what this camera declares it can emit, and nothing for an instance
// that is not serving.
func (c *Camera) Formats() []cameraapi.Format {
	formats, ok := supervisor.Peek(c.instance, func(target any) []cameraapi.Format {
		bound, isBound := target.(Bound)
		if !isBound {
			return nil
		}

		return bound.Camera.Formats()
	})

	if !ok {
		return nil
	}

	return formats
}

// MinInterval is the shortest gap between two new frames this camera declares.
//
// It is zero for a driver that declares none, and for an instance that is not
// serving. A caller can act on zero either way: there is no rate to publish, so
// a client keeps the wait it already had.
func (c *Camera) MinInterval() time.Duration {
	interval, ok := supervisor.Peek(c.instance, func(target any) time.Duration {
		bound, isBound := target.(Bound)
		if !isBound {
			return 0
		}

		paced, says := bound.Camera.(cameraapi.Paced)
		if !says {
			return 0
		}

		return paced.MinInterval()
	})

	if !ok {
		return 0
	}

	return interval
}

// Snapshot returns the camera's most recent still image, and refuses one the
// host could not serve.
//
// The check happens here and not at the transport, because a camera is an
// untrusted device on somebody's network. A driver that returned a media type
// of its own choosing would decide what this server's origin serves.
func (c *Camera) Snapshot(ctx context.Context) (cameraapi.Snapshot, error) {
	shot, err := supervisor.Call(ctx, c.instance, "snapshot",
		func(callCtx context.Context, target any) (cameraapi.Snapshot, error) {
			bound, mismatch := cameraOf(target)
			if mismatch != nil {
				return cameraapi.Snapshot{}, mismatch
			}

			taken, snapErr := bound.Camera.Snapshot(callCtx)
			if snapErr != nil {
				return cameraapi.Snapshot{}, snapErr
			}

			return taken, taken.Validate()
		})
	if err != nil {
		return cameraapi.Snapshot{}, err
	}

	return shot, nil
}

// Accept gives one still image to a camera that takes pushed frames, and
// refuses a camera that does not with ErrPushNotSupported.
//
// The bytes are checked here as well as at the transport, for the same reason
// Snapshot checks what a driver returns: this server decides what it serves to
// a browser, not whoever sent the frame.
//
// It goes through the same bulkhead as every other media call, so a driver that
// blocks on a push uses up its own slots and nobody else's.
func (c *Camera) Accept(ctx context.Context, shot cameraapi.Snapshot) error {
	if err := shot.Validate(); err != nil {
		return err
	}

	_, err := supervisor.Call(ctx, c.instance, "accept",
		func(callCtx context.Context, target any) (struct{}, error) {
			bound, mismatch := cameraOf(target)
			if mismatch != nil {
				return struct{}{}, mismatch
			}

			receiver, takes := bound.Camera.(cameraapi.Receiver)
			if !takes {
				return struct{}{}, fmt.Errorf("%w: %T reads a device",
					cameraapi.ErrPushNotSupported, bound.Camera)
			}

			return struct{}{}, receiver.Accept(callCtx, shot)
		})

	return err
}

// Open starts a stream. The supervised span ends when the returned body is
// closed, not when Open returns. A stream that fails after the first frame is a
// failure of this call, and its bulkhead slot stays taken until the bytes stop
// moving.
func (c *Camera) Open(ctx context.Context, format cameraapi.Format) (cameraapi.Stream, error) {
	stream, release, err := supervisor.Stream(ctx, c.instance, "open",
		func(callCtx context.Context, target any) (cameraapi.Stream, error) {
			bound, mismatch := cameraOf(target)
			if mismatch != nil {
				return cameraapi.Stream{}, mismatch
			}

			opened, openErr := bound.Camera.Open(callCtx, format)
			if openErr != nil {
				return cameraapi.Stream{}, openErr
			}

			if opened.Body == nil {
				return cameraapi.Stream{}, fmt.Errorf("%w: it opened a stream with no body", ErrNotACamera)
			}

			if opened.MediaType != format.MediaType() {
				_ = opened.Body.Close()

				return cameraapi.Stream{}, fmt.Errorf(
					"%w: it framed %s as %q", ErrNotACamera, format, opened.MediaType)
			}

			return opened, nil
		})
	if err != nil {
		return cameraapi.Stream{}, err
	}

	stream.Body = &supervisedFrames{camera: c, body: stream.Body, release: release}

	return stream, nil
}

// supervisedFrames is one viewer's stream. It keeps the supervised span open
// until the viewer stops reading.
type supervisedFrames struct {
	camera  *Camera
	body    io.ReadCloser
	release func(error)
	failed  error
}

// Read forwards to the driver's reader, remembering the first real failure.
func (s *supervisedFrames) Read(p []byte) (n int, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = supervisor.Recovered(s.camera.instance, "open", rec)
			s.failed = err
			s.release(err)
		}
	}()

	n, err = s.body.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		s.failed = err
	}

	return n, err
}

// Close ends the stream and the supervised span.
func (s *supervisedFrames) Close() (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = supervisor.Recovered(s.camera.instance, "open", rec)
			s.release(err)
		}
	}()

	err = s.body.Close()
	if err != nil && s.failed == nil {
		s.failed = err
	}

	s.release(s.failed)

	return err
}

// cameraOf reads the supervised target back as the binding. A camera type whose
// constructor builds something else is a build-time mistake that only a
// type-erased instance carries this far, so the error names it.
func cameraOf(target any) (Bound, error) {
	bound, ok := target.(Bound)
	if !ok {
		return Bound{}, fmt.Errorf("%w: it built a %T", ErrNotACamera, target)
	}

	if bound.Camera == nil {
		return Bound{}, fmt.Errorf("%w: it built no camera", ErrNotACamera)
	}

	return bound, nil
}
