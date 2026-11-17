package cameraapi

// The module wire. The rest of this package is the driver contract, which runs
// inside the host; this is how a module that is no driver hands a camera a
// frame, over the session the host already has with it.
//
// It exists for a camera that speaks a protocol of its own to a module, such
// as a cloud camera pointed at a server the module runs. The module receives
// the frame and hands it on, and the host stores it in a push camera exactly as
// it stores a frame a device sent over HTTP.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// MethodAccept hands one still image to a push camera, which keeps it as its
// most recent frame.
//
// The module opens a stream and makes this call on it. The host answers the
// call by writing one byte on the stream when it is ready, and the module then
// writes exactly AcceptParams.Size bytes as data and ends its half of the
// stream. It is the protocol storage.write uses, so a frame costs no larger
// control frame and no base64.
const MethodAccept wirerpc.Method = "camera.accept"

// MaxAcceptBytes is the largest frame one call carries. It is the bound the
// HTTP push route holds a device to, so a frame is not refused on one door and
// taken on the other.
const MaxAcceptBytes = 8 << 20

// AcceptParams is what MethodAccept carries.
type AcceptParams struct {
	// Principal is who the call is made for, and empty for the module's own
	// work.
	Principal string `json:"principal,omitempty"`

	// Camera is the camera's declared name, the one its HTTP routes take.
	Camera string `json:"camera"`
	// Size is how many bytes follow the call, at least one. The host refuses
	// a stream that carries fewer or more.
	Size int64 `json:"size"`
}

// Validate holds one call to the bounds this contract publishes.
func (p AcceptParams) Validate() error {
	if p.Camera == "" {
		return fmt.Errorf("%w: the call names no camera", ErrNoCamera)
	}

	if p.Size < 1 || p.Size > MaxAcceptBytes {
		return fmt.Errorf("%w: the size is %d, and a frame carries 1 to %d bytes",
			ErrInvalidSnapshot, p.Size, MaxAcceptBytes)
	}

	return nil
}

// The refusals a module reads, beside ErrInvalidSnapshot.
var (
	// ErrNoCamera reports a camera this module cannot reach: one that is not
	// declared, one it may not see, and one whose driver takes no frame. They
	// are one answer, so a module cannot list the cameras of a server by
	// trying names.
	ErrNoCamera = errors.New("cameraapi: no camera of that name takes frames from this caller")
	// ErrDenied reports a caller who may see the camera and may not send it a
	// frame, which takes operate on the camera.
	ErrDenied = errors.New("cameraapi: this caller may not send the camera a frame")
	// ErrShortFrame reports a stream that carried fewer or more bytes than the
	// call named. The camera kept the frame it had.
	ErrShortFrame = errors.New("cameraapi: the stream did not carry the size the call named")
	// ErrNoStream reports a client built over a session that opens no stream.
	ErrNoStream = errors.New("cameraapi: the session opens no stream")
)

// The codes the refusals travel as. 8000 is the first block no other contract
// of this module uses.
const (
	// CodeNoCamera is ErrNoCamera.
	CodeNoCamera = 8000
	// CodeDenied is ErrDenied.
	CodeDenied = 8001
	// CodeInvalidSnapshot is ErrInvalidSnapshot.
	CodeInvalidSnapshot = 8002
	// CodeShortFrame is ErrShortFrame.
	CodeShortFrame = 8003
)

// Frames is what the host answers MethodAccept with, bound to one module.
// A module never constructs one.
type Frames interface {
	// Admit decides whether the call may hand the camera a frame at all. The
	// handler asks it before it reads a byte, so a refused call costs the
	// host no frame's worth of memory.
	Admit(ctx context.Context, params AcceptParams) error
	// Accept stores frame as the camera's most recent one. The handler has
	// read exactly params.Size bytes into frame and checked nothing else.
	Accept(ctx context.Context, params AcceptParams, frame []byte) error
}

// Opener is the half of a wirerpc session this contract needs: a stream of its
// own for each frame. *wirerpc.Conn is one.
type Opener interface {
	Open() (*wirerpc.Stream, error)
}

// Client is a module's way of handing frames to the host's cameras.
type Client struct {
	conn Opener
}

// NewClient returns the client a module hands frames through.
//
// A host that serves no camera answers CodeMethodNotFound, which arrives as an
// ordinary error on the first call rather than as a failure to construct.
func NewClient(conn Opener) *Client {
	return &Client{conn: conn}
}

// Accept hands one JPEG to the push camera named camera.
//
// It takes operate on the camera, which is what the HTTP push route takes. A
// module's own frame is held to the instance's permissions list first, so an
// instance installed without operate sends nothing. A camera of a type the
// module offers then takes it with no grant, and any other camera needs one,
// such as device:<camera>#operator@sink:<instance>.
func (c *Client) Accept(ctx context.Context, principal, camera string, frame []byte) error {
	params := AcceptParams{Principal: principal, Camera: camera, Size: int64(len(frame))}
	if err := params.Validate(); err != nil {
		return fmt.Errorf("cameraapi: %s: %w", MethodAccept, err)
	}

	if c.conn == nil {
		return fmt.Errorf("cameraapi: %s: %w", MethodAccept, ErrNoStream)
	}

	stream, err := c.conn.Open()
	if err != nil {
		return fmt.Errorf("cameraapi: %s: open a stream: %w", MethodAccept, err)
	}

	// The bytes go beside the call, once the host said it took the call, for
	// the reason storage.write sends them that way: an end sent before the
	// call could reach the host first.
	sent := make(chan error, 1)

	go func() { sent <- send(stream, frame) }()

	_, called := stream.Call(ctx, MethodAccept, params)

	_ = stream.Close()
	copied := <-sent

	if called != nil {
		return mapped(called)
	}

	if copied != nil && !errors.Is(copied, wirerpc.ErrStreamClosed) {
		return fmt.Errorf("cameraapi: %s: send the frame: %w", MethodAccept, copied)
	}

	return nil
}

// ready is the byte the host writes on the stream when it has taken a call.
const ready = 1

// send waits for the host's ready byte, writes the frame and ends this side of
// the stream.
func send(stream *wirerpc.Stream, frame []byte) error {
	var held [1]byte
	if _, err := io.ReadFull(stream, held[:]); err != nil {
		return err
	}

	if _, err := stream.Write(frame); err != nil {
		return err
	}

	return stream.CloseWrite()
}

// mapped turns a coded refusal back into the sentinel the host raised.
func mapped(err error) error {
	if coded, ok := errors.AsType[*wirerpc.Error](err); ok {
		if sentinel := sentinelFor(coded.Code); sentinel != nil {
			return fmt.Errorf("%s: %w", MethodAccept, &remote{sentinel: sentinel, message: coded.Message})
		}
	}

	return fmt.Errorf("cameraapi: %s: %w", MethodAccept, err)
}

// remote carries the host's own message under this contract's sentinel.
type remote struct {
	sentinel error
	message  string
}

func (r *remote) Error() string { return r.message }
func (r *remote) Unwrap() error { return r.sentinel }

// coded pairs each code with its sentinel, in the order a refusal is matched.
//
//nolint:gochecknoglobals // a fixed table, never written to.
var coded = []struct {
	code     int
	sentinel error
}{
	{CodeDenied, ErrDenied},
	{CodeShortFrame, ErrShortFrame},
	{CodeInvalidSnapshot, ErrInvalidSnapshot},
	{CodeNoCamera, ErrNoCamera},
}

func sentinelFor(code int) error {
	for _, known := range coded {
		if known.code == code {
			return known.sentinel
		}
	}

	return nil
}

// HostFailed is what a module is told when a call failed for a reason of the
// host's own. The host has the detail in its own log.
const HostFailed = "cameraapi: the host could not answer"

// refusal renders a host-side error as the code this contract numbers.
func refusal(err error) error {
	for _, known := range coded {
		if errors.Is(err, known.sentinel) {
			return &wirerpc.Error{Code: known.code, Message: err.Error()}
		}
	}

	return &wirerpc.Error{Code: wirerpc.CodeInternal, Message: HostFailed}
}

// Handlers is the host's method set for the module wire, bound to one module's
// view of the cameras.
//
// A nil frames is a host that serves none: it returns an empty set, so a call
// reads CodeMethodNotFound rather than reaching a nil.
func Handlers(frames Frames) wirerpc.Methods {
	if frames == nil {
		return wirerpc.Methods{}
	}

	return wirerpc.Methods{MethodAccept: acceptHandler(frames)}
}

func acceptHandler(frames Frames) wirerpc.Handler {
	return func(ctx context.Context, call wirerpc.Call) (any, error) {
		var params AcceptParams

		if len(call.Params) == 0 || json.Unmarshal(call.Params, &params) != nil {
			return nil, &wirerpc.Error{
				Code:    wirerpc.CodeInvalidParams,
				Message: string(MethodAccept) + " carries a params object",
			}
		}

		if err := params.Validate(); err != nil {
			return nil, refusal(err)
		}

		// A call made without a stream has no bytes behind it.
		if call.Stream == nil {
			return nil, refusal(fmt.Errorf("%w: %s is made on a stream", ErrShortFrame, MethodAccept))
		}

		if err := frames.Admit(ctx, params); err != nil {
			return nil, refusal(err)
		}

		if _, err := call.Stream.Write([]byte{ready}); err != nil {
			return nil, refusal(fmt.Errorf("%w: %w", ErrShortFrame, err))
		}

		frame, err := readFrame(call.Stream, params.Size)
		if err != nil {
			return nil, refusal(err)
		}

		if err = frames.Accept(ctx, params, frame); err != nil {
			return nil, refusal(err)
		}

		return struct{}{}, nil
	}
}

// readFrame reads a frame of exactly size bytes. The frame is held whole, as
// the camera holds it, so it is read whole into a buffer of that size: one
// byte past it is how a stream that carried more is caught.
func readFrame(stream io.Reader, size int64) ([]byte, error) {
	frame := make([]byte, size)

	read, err := io.ReadFull(stream, frame)
	if err == nil {
		var past [1]byte
		if extra, _ := stream.Read(past[:]); extra > 0 {
			read += extra
			err = io.ErrShortBuffer
		}
	}

	switch {
	case err == nil:
		return frame, nil
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.ErrShortBuffer):
		return nil, fmt.Errorf("%w: %d bytes arrived and the call named %d", ErrShortFrame, read, size)
	default:
		return nil, fmt.Errorf("%w: %w", ErrShortFrame, err)
	}
}
