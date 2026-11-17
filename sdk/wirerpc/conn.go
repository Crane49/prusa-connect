package wirerpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/wire"
)

// The refusals a session returns.
var (
	// ErrClosed reports a call on a connection that has stopped.
	ErrClosed = errors.New("wirerpc: the connection has stopped")

	// ErrStreamsExhausted reports a side that has opened every stream id in
	// its half. An id is used once per connection and never opened again.
	ErrStreamsExhausted = errors.New("wirerpc: every stream id in this half is used")

	// ErrStreamParity reports a peer naming a stream in the half this side
	// allocates from. Each side opens from its own half, so an id from the
	// other half that this side never opened is a peer that is broken.
	ErrStreamParity = errors.New("wirerpc: the peer named a stream this side opens")

	// ErrStreamOpening reports a peer opening a stream with a frame that is
	// neither a control request nor a grant. A stream carries a method call,
	// and the side that opens one may make room before it speaks.
	ErrStreamOpening = errors.New("wirerpc: a stream the peer opened does not open with a call or a grant")

	// ErrStreamReset reports a stream the peer ended in both directions.
	ErrStreamReset = errors.New("wirerpc: the peer reset the stream")

	// ErrRole reports Options with no Role, which decides the half of the
	// stream ids this side opens from.
	ErrRole = errors.New("wirerpc: a connection states which role it is")
)

// Role says which half of the stream ids a side allocates from. Odd ids belong
// to the host and even ids to the child.
type Role uint8

// The two roles. The zero value is neither, and NewConn refuses it.
const (
	// RoleHost allocates the odd stream ids.
	RoleHost Role = iota + 1

	// RoleChild allocates the even stream ids.
	RoleChild
)

// Options is how a Conn is built.
type Options struct {
	// Role says which half of the stream ids this side opens from. It is
	// mandatory.
	Role Role

	// Methods is the method set this side answers. A side that answers
	// nothing leaves it nil, and a call then returns CodeMethodNotFound.
	Methods Methods

	// MaxPayload bounds a frame this side accepts, before it allocates for
	// one. Zero means wire.DefaultMaxPayloadBytes.
	MaxPayload int

	// Window is the credit this side grants on a stream when the stream
	// opens, and the amount a read grants back as it consumes. Zero means
	// wire.ChunkBytes, which is one data frame in flight.
	Window uint32

	// Logger receives what this side cannot tell the peer, such as an answer
	// that could not be sent. Nil means slog.Default.
	Logger *slog.Logger
}

// Handler answers one call the peer made. It runs on its own goroutine.
// Return an *Error to choose the code and message the peer reads. Any other
// error is reported as CodeInternal with its text.
type Handler func(ctx context.Context, call Call) (any, error)

// Methods is the method set one side answers.
type Methods map[Method]Handler

// Call is one call as the handler receives it.
type Call struct {
	// Stream is the stream the call arrived on. A handler that answers with
	// bulk bytes writes them here.
	Stream *Stream
	// Method is the method the peer named.
	Method Method
	// Params is the argument object, unread.
	Params json.RawMessage
}

// Conn is one session: the frames of one channel, demultiplexed into streams,
// with the control objects on them decoded and routed. Serve runs the read
// loop, and every other method needs it running.
type Conn struct {
	reader *wire.Reader
	writer *wire.Writer
	closer io.Closer
	opts   Options

	// writeMu serialises the writer. One frame is one write.
	writeMu sync.Mutex

	// watchdog closes the channel when one write overruns writeBound. It is
	// read and written under writeMu.
	watchdog *time.Timer

	handlers sync.WaitGroup

	mu      sync.Mutex
	streams map[wire.StreamID]*Stream
	next    uint64
	done    chan struct{}
	err     error
}

// NewConn builds a session over one duplex channel. Close closes the channel
// when it is an io.Closer.
func NewConn(channel io.ReadWriter, opts Options) (*Conn, error) {
	if opts.Role != RoleHost && opts.Role != RoleChild {
		return nil, ErrRole
	}

	if opts.Window == 0 {
		opts.Window = wire.ChunkBytes
	}

	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	first := uint64(1)
	if opts.Role == RoleChild {
		first = 2
	}

	conn := &Conn{
		reader:  wire.NewReader(channel, opts.MaxPayload),
		writer:  wire.NewWriter(channel, opts.MaxPayload),
		opts:    opts,
		streams: map[wire.StreamID]*Stream{},
		next:    first,
		done:    make(chan struct{}),
	}

	if closer, ok := channel.(io.Closer); ok {
		conn.closer = closer
	}

	return conn, nil
}

// Serve reads frames until the peer closes the channel or a peer error stops
// the session, and returns why it stopped. A channel that ended between two
// frames returns nil. Cancelling ctx closes the channel.
func (c *Conn) Serve(ctx context.Context) error {
	watch := make(chan struct{})

	go func() {
		defer close(watch)

		select {
		case <-ctx.Done():
			_ = c.Close()
		case <-c.done:
		}
	}()

	err := c.read(ctx)

	// A read that failed because Close shut the channel is not a fault.
	if err != nil && errors.Is(c.Err(), ErrClosed) {
		err = nil
	}

	c.stop(err)
	<-watch
	c.handlers.Wait()

	return err
}

// maxStreamID is the largest id the four-byte stream field expresses. The id
// after it is refused.
const maxStreamID = 1<<32 - 1

// Open takes the next stream id in this side's half.
func (c *Conn) Open() (*Stream, error) {
	stream, err := c.take()
	if err != nil {
		return nil, err
	}

	if err = stream.grant(c.opts.Window); err != nil {
		return nil, err
	}

	return stream, nil
}

// take allocates the next id and registers a stream on it.
func (c *Conn) take() (*Stream, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.err != nil {
		return nil, c.err
	}

	if c.next > maxStreamID {
		return nil, ErrStreamsExhausted
	}

	stream := newStream(c, wire.StreamID(c.next))
	c.next += 2
	c.streams[stream.id] = stream

	return stream, nil
}

// Call opens a stream, calls one method on it, and ends it. Use it for a call
// with no bulk bytes.
func (c *Conn) Call(ctx context.Context, method Method, params any) (json.RawMessage, error) {
	stream, err := c.Open()
	if err != nil {
		return nil, err
	}

	defer func() {
		_ = stream.Close()
	}()

	return stream.Call(ctx, method, params)
}

// Done is closed when the session stops.
func (c *Conn) Done() <-chan struct{} {
	return c.done
}

// Err reports why the session stopped, or nil while it runs.
func (c *Conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.err
}

// Close stops the session and closes the channel under it. It is safe to call
// more than once.
func (c *Conn) Close() error {
	c.stop(ErrClosed)

	if c.closer == nil {
		return nil
	}

	if err := c.closer.Close(); err != nil {
		return fmt.Errorf("close the channel: %w", err)
	}

	return nil
}

// read is the loop itself.
func (c *Conn) read(ctx context.Context) error {
	for {
		frame, err := c.reader.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}

			return fmt.Errorf("read a frame: %w", err)
		}

		if err = c.dispatch(ctx, frame); err != nil {
			return err
		}
	}
}

// dispatch routes one frame to its stream. An error it returns is a protocol
// error and stops the session.
func (c *Conn) dispatch(ctx context.Context, frame wire.Frame) error {
	stream, opened, err := c.streamFor(frame)
	if err != nil {
		return err
	}

	// A frame for a stream this side opened and has since released. Both sides
	// ended it, so the frame is the peer's last word on it.
	if stream == nil {
		return nil
	}

	// A stream the peer opened is granted its window before its call is
	// answered, so a handler that streams bulk bytes back has room to write.
	if opened {
		if err = stream.grant(c.opts.Window); err != nil {
			return err
		}
	}

	switch frame.Kind {
	case wire.KindControl:
		return c.deliverControl(ctx, stream, frame.Payload)
	case wire.KindData:
		return stream.deliverData(frame.Payload)
	case wire.KindEnd:
		stream.deliverEnd()

		return nil
	case wire.KindReset:
		stream.fail(fmt.Errorf("%w: %s", ErrStreamReset, frame.Payload))
		c.release(stream.id)

		return nil
	case wire.KindCredit:
		granted, unread := wire.Grant(frame)
		if unread != nil {
			return fmt.Errorf("read a grant on stream %d: %w", frame.Stream, unread)
		}

		return stream.deliverGrant(granted)
	default:
		return fmt.Errorf("kind %d on stream %d: %w", uint8(frame.Kind), frame.Stream, wire.ErrUnknownKind)
	}
}

// streamFor finds the stream a frame names, opening one when the peer opened
// it with a call. The second value reports that it opened one.
func (c *Conn) streamFor(frame wire.Frame) (*Stream, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.err != nil {
		return nil, false, c.err
	}

	if stream, known := c.streams[frame.Stream]; known {
		return stream, false, nil
	}

	if frame.Stream.OpenedByHost() == (c.opts.Role == RoleHost) {
		// An id below the counter is one this side opened and released. An id
		// at or above it was never opened here, so the peer broke parity.
		if uint64(frame.Stream) < c.next {
			return nil, false, nil
		}

		return nil, false, fmt.Errorf("stream %d: %w", frame.Stream, ErrStreamParity)
	}

	if frame.Kind != wire.KindControl && frame.Kind != wire.KindCredit {
		return nil, false, fmt.Errorf("stream %d opens with a %s frame: %w",
			frame.Stream, frame.Kind, ErrStreamOpening)
	}

	stream := newStream(c, frame.Stream)
	c.streams[frame.Stream] = stream

	return stream, true, nil
}

// deliverControl decodes one control payload and either answers a call or
// completes one this side made.
func (c *Conn) deliverControl(ctx context.Context, stream *Stream, payload []byte) error {
	object, err := decodeControl(payload)
	if err != nil {
		return fmt.Errorf("control on stream %d: %w", stream.id, err)
	}

	if object.response != nil {
		stream.deliverResponse(object.response)

		return nil
	}

	stream.serve()

	c.handlers.Go(func() {
		defer stream.served()

		c.answer(ctx, stream, *object.request)
	})

	return nil
}

// answer runs one handler and writes what it returned.
func (c *Conn) answer(ctx context.Context, stream *Stream, request Request) {
	result, failure := c.invoke(ctx, stream, request)

	if request.IsNotification() {
		return
	}

	object, err := encodeResponse(request.ID, result, failure)
	if err != nil {
		c.opts.Logger.WarnContext(ctx, "a handler returned a result that could not be encoded",
			"method", string(request.Method), "stream", uint32(stream.id), "error", err.Error())

		c.answerFailure(ctx, stream, request, &Error{Code: CodeInternal, Message: "the result could not be encoded"})

		return
	}

	err = c.write(wire.Control(stream.id, object))
	if err == nil {
		return
	}

	// The writer refuses a frame over the cap before any byte of it is sent,
	// so the stream is still usable and the caller can be told why.
	if errors.Is(err, wire.ErrPayloadTooLarge) {
		c.opts.Logger.WarnContext(ctx, "an answer is over the frame limit and was not sent",
			"method", string(request.Method), "stream", uint32(stream.id),
			"bytes", len(object), "error", err.Error())

		c.answerFailure(ctx, stream, request, &Error{
			Code:    CodeResultTooLarge,
			Message: fmt.Sprintf("the answer to %s is %d bytes, over the frame limit", request.Method, len(object)),
		})

		return
	}

	// A connection that has stopped already said why, through Serve.
	if c.Err() == nil {
		c.opts.Logger.WarnContext(ctx, "an answer could not be sent",
			"method", string(request.Method), "stream", uint32(stream.id), "error", err.Error())
	}
}

// answerFailure sends an error in place of an answer that could not be sent.
// A failure here is logged.
func (c *Conn) answerFailure(ctx context.Context, stream *Stream, request Request, failure *Error) {
	object, err := encodeResponse(request.ID, nil, failure)
	if err == nil {
		err = c.write(wire.Control(stream.id, object))
	}

	if err != nil && c.Err() == nil {
		c.opts.Logger.WarnContext(ctx, "an error answer could not be sent",
			"method", string(request.Method), "stream", uint32(stream.id),
			"code", failure.Code, "error", err.Error())
	}
}

// invoke calls the handler for a method and turns what it returned into either
// a result or a JSON-RPC error.
func (c *Conn) invoke(ctx context.Context, stream *Stream, request Request) (any, *Error) {
	if request.Method == MethodPing {
		return nil, nil
	}

	handler, served := c.opts.Methods[request.Method]
	if !served {
		return nil, &Error{
			Code:    CodeMethodNotFound,
			Message: fmt.Sprintf("this side serves no method %s", request.Method),
		}
	}

	result, err := call(ctx, handler, Call{Stream: stream, Method: request.Method, Params: request.Params})
	if err == nil {
		return result, nil
	}

	if chosen, ok := errors.AsType[*Error](err); ok {
		return nil, chosen
	}

	return nil, &Error{Code: CodeInternal, Message: err.Error()}
}

// ErrHandlerPanic reports a handler that panicked. The session survives it.
var ErrHandlerPanic = errors.New("wirerpc: the handler panicked")

// call runs one handler and turns a panic into an error.
func call(ctx context.Context, handler Handler, received Call) (result any, err error) {
	defer func() {
		if panicked := recover(); panicked != nil {
			result = nil
			err = fmt.Errorf("%s: %w: %v", received.Method, ErrHandlerPanic, panicked)
		}
	}()

	return handler(ctx, received)
}

// writeBound is how long one frame may take to reach the channel before the
// watchdog closes the connection. A blocked write observes no context and
// blocks every other writer, so nothing else would end it.
const writeBound = 30 * time.Second

// write sends one frame, serialised against every other writer and bounded by
// the watchdog.
func (c *Conn) write(frame wire.Frame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	select {
	case <-c.done:
		return c.Err()
	default:
	}

	c.arm()

	err := c.writer.Write(frame)
	c.watchdog.Stop()

	if err != nil {
		return fmt.Errorf("write to the channel: %w", err)
	}

	return nil
}

// arm starts the watchdog for one write. The timer is built on the first
// write.
func (c *Conn) arm() {
	if c.watchdog == nil {
		c.watchdog = time.AfterFunc(writeBound, func() { _ = c.Close() })

		return
	}

	c.watchdog.Reset(writeBound)
}

// release forgets a stream.
func (c *Conn) release(id wire.StreamID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.streams, id)
}

// stop records why the session ended and unblocks everything waiting on it. A
// reader waiting for data and a writer waiting for credit both end here.
func (c *Conn) stop(reason error) {
	c.mu.Lock()

	if c.err != nil {
		c.mu.Unlock()

		return
	}

	if reason == nil {
		reason = io.EOF
	}

	c.err = reason
	close(c.done)

	streams := make([]*Stream, 0, len(c.streams))
	for _, stream := range c.streams {
		streams = append(streams, stream)
	}

	c.streams = map[wire.StreamID]*Stream{}
	c.mu.Unlock()

	for _, stream := range streams {
		stream.fail(reason)
	}
}
