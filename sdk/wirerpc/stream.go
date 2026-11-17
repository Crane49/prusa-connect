package wirerpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"

	"github.com/prusa3d/prusa-connect/sdk/wire"
)

// ErrStreamClosed reports a write on a stream this side already ended.
var ErrStreamClosed = errors.New("wirerpc: this side ended the stream")

// Stream is one conversation inside the connection. It is an io.ReadWriter
// over the stream's data frames. A write waits for the credit the peer
// granted, and a read grants that credit back as it consumes. Call and Notify
// put JSON-RPC objects on the same stream and cost no credit. Every method is
// safe to call from several goroutines.
type Stream struct {
	conn *Conn
	id   wire.StreamID

	mu sync.Mutex
	// change is closed and replaced whenever data, credit or an end arrives.
	// A waiting read or write wakes on it.
	change chan struct{}
	// gone is closed once, when the stream fails or either side resets it.
	gone chan struct{}
	// send is the credit the peer granted this side. receive is the credit
	// this side granted the peer, spent as that peer's data arrives.
	send    Balance
	receive Balance
	buffer  []byte
	// serving counts the handlers running on this stream. A stream is released
	// when the peer has ended it and none is left.
	serving  int
	ended    bool
	sentEnd  bool
	failed   error
	calls    map[string]chan *Response
	nextCall uint64
}

// newStream registers one stream on a connection.
func newStream(conn *Conn, id wire.StreamID) *Stream {
	return &Stream{
		conn:   conn,
		id:     id,
		change: make(chan struct{}),
		gone:   make(chan struct{}),
		calls:  map[string]chan *Response{},
	}
}

// ID is the stream id on the wire.
func (s *Stream) ID() wire.StreamID {
	return s.id
}

// Credit reports the payload bytes this side may send now. A writer does not
// have to read it: Write waits for credit.
func (s *Stream) Credit() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.send.Available()
}

// Call sends one request and waits for its answer, which arrives on the same
// stream.
func (s *Stream) Call(ctx context.Context, method Method, params any) (json.RawMessage, error) {
	id, answer, err := s.expect()
	if err != nil {
		return nil, err
	}

	defer s.forget(id)

	object, err := encodeRequest(method, params, json.RawMessage(id))
	if err != nil {
		return nil, err
	}

	if err = s.conn.write(wire.Control(s.id, object)); err != nil {
		return nil, err
	}

	select {
	case response := <-answer:
		if response.Error != nil {
			return nil, response.Error
		}

		return response.Result, nil
	case <-s.gone:
		return nil, s.reason()
	case <-s.conn.done:
		return nil, s.conn.Err()
	case <-ctx.Done():
		return nil, fmt.Errorf("call %s on stream %d: %w", method, s.id, ctx.Err())
	}
}

// Notify sends one request with no id, so the peer answers nothing.
func (s *Stream) Notify(method Method, params any) error {
	object, err := encodeRequest(method, params, nil)
	if err != nil {
		return err
	}

	return s.conn.write(wire.Control(s.id, object))
}

// Read copies out what the peer sent in data frames, and grants the credit
// back that the copied bytes spent. It returns io.EOF once the peer has ended
// the stream and the buffer is empty.
func (s *Stream) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	for {
		taken, change, err := s.take(p)
		if change == nil {
			return taken, err
		}

		if waited := s.await(change); waited != nil {
			// Data and the end of the stream can arrive together. Buffered
			// data is returned before the end is reported.
			if taken, _, _ = s.take(p); taken > 0 {
				return taken, nil
			}

			return 0, waited
		}
	}
}

// Write sends bulk bytes in data frames. It splits them into chunks the wire
// allows and waits for credit, so it blocks while the peer grants none.
func (s *Stream) Write(p []byte) (int, error) {
	written := 0

	for written < len(p) {
		room, err := s.reserve(len(p) - written)
		if err != nil {
			return written, err
		}

		if wrote := s.conn.write(wire.Data(s.id, p[written:written+room])); wrote != nil {
			return written, wrote
		}

		written += room
	}

	return written, nil
}

// CloseWrite says this side writes nothing more on the stream. The peer may
// still write, and a read here still returns what it sends.
func (s *Stream) CloseWrite() error {
	s.mu.Lock()

	if s.failed != nil {
		defer s.mu.Unlock()

		return s.failed
	}

	if s.sentEnd {
		s.mu.Unlock()

		return nil
	}

	s.sentEnd = true
	s.mu.Unlock()

	return s.conn.write(wire.End(s.id))
}

// Reset ends the stream in both directions at once. The reason is diagnostic
// text for the peer's log.
func (s *Stream) Reset(reason string) error {
	err := s.conn.write(wire.Reset(s.id, reason))

	s.fail(fmt.Errorf("%w: %s", ErrStreamClosed, reason))
	s.conn.release(s.id)

	return err
}

// Close ends this side's half of the stream and releases it. A read that is
// waiting returns.
func (s *Stream) Close() error {
	err := s.CloseWrite()

	s.fail(ErrStreamClosed)
	s.conn.release(s.id)

	return err
}

// Grant gives the peer n more payload bytes of credit on this stream, on top
// of the window the session grants. Call it before a large transfer to widen
// the window. The extra credit stays granted.
func (s *Stream) Grant(n uint32) error {
	return s.grant(n)
}

// grant sends a credit frame for n payload bytes and records it. A grant of
// zero is skipped.
func (s *Stream) grant(n uint32) error {
	if n == 0 {
		return nil
	}

	s.mu.Lock()

	if s.failed != nil {
		defer s.mu.Unlock()

		return s.failed
	}

	if err := s.receive.Grant(n); err != nil {
		s.mu.Unlock()

		return fmt.Errorf("grant on stream %d: %w", s.id, err)
	}

	s.mu.Unlock()

	return s.conn.write(wire.Credit(s.id, n))
}

// take copies out of the buffer. A nil channel means the read is answered.
// Otherwise it returns the channel that closes on the next change, read under
// the same lock as the buffer so that no change is missed.
func (s *Stream) take(p []byte) (int, <-chan struct{}, error) {
	s.mu.Lock()

	if len(s.buffer) > 0 {
		taken := copy(p, s.buffer)
		s.buffer = s.buffer[taken:]
		// A peer that has ended the stream gets no more credit.
		regrant := !s.ended
		s.mu.Unlock()

		if regrant {
			//nolint:gosec // taken is bounded by the buffer, which the window bounds.
			if err := s.grant(uint32(taken)); err != nil && !errors.Is(err, ErrStreamClosed) {
				return taken, nil, err
			}
		}

		return taken, nil, nil
	}

	defer s.mu.Unlock()

	if s.failed != nil {
		return 0, nil, s.failed
	}

	if s.ended {
		return 0, nil, io.EOF
	}

	return 0, s.change, nil
}

// await blocks until the change the caller was handed happens, or the stream
// or the connection stops.
func (s *Stream) await(change <-chan struct{}) error {
	select {
	case <-change:
		return nil
	case <-s.gone:
		return s.reason()
	case <-s.conn.done:
		return s.conn.Err()
	}
}

// reserve waits for credit and takes what it may send now, bounded by the
// chunk size the wire allows in one data frame.
func (s *Stream) reserve(want int) (int, error) {
	for {
		room, change, err := s.claim(want)
		if change == nil {
			return room, err
		}

		if waited := s.await(change); waited != nil {
			return 0, waited
		}
	}
}

// claim takes credit if there is any. A nil channel means the caller has its
// answer. Otherwise the channel is the one to wait on, as in take.
func (s *Stream) claim(want int) (int, <-chan struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failed != nil {
		return 0, nil, s.failed
	}

	if s.sentEnd {
		return 0, nil, ErrStreamClosed
	}

	available := int(s.send.Available())
	if available == 0 {
		return 0, s.change, nil
	}

	room := min(want, available, wire.ChunkBytes)
	if err := s.send.Spend(room); err != nil {
		return 0, nil, fmt.Errorf("spend on stream %d: %w", s.id, err)
	}

	return room, nil, nil
}

// expect records that this side is waiting for the answer to a new call, and
// mints the id of that answer.
func (s *Stream) expect() (string, chan *Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failed != nil {
		return "", nil, s.failed
	}

	s.nextCall++
	id := strconv.FormatUint(s.nextCall, 10)
	answer := make(chan *Response, 1)
	s.calls[id] = answer

	return id, answer, nil
}

// forget drops a call this side is no longer waiting for.
func (s *Stream) forget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.calls, id)
}

// deliverResponse passes an answer to the call that is waiting for it. An id
// nothing is waiting for is dropped.
func (s *Stream) deliverResponse(response *Response) {
	s.mu.Lock()
	defer s.mu.Unlock()

	answer, waiting := s.calls[string(response.ID)]
	if !waiting {
		return
	}

	delete(s.calls, string(response.ID))
	answer <- response
}

// deliverData buffers what the peer sent and charges it against the credit
// this side granted.
func (s *Stream) deliverData(payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failed != nil {
		return nil
	}

	if err := s.receive.Spend(len(payload)); err != nil {
		return fmt.Errorf("data on stream %d: %w", s.id, err)
	}

	s.buffer = append(s.buffer, payload...)
	s.wake()

	return nil
}

// deliverEnd records that the peer writes nothing more.
func (s *Stream) deliverEnd() {
	s.mu.Lock()
	s.ended = true
	s.wake()
	s.mu.Unlock()

	s.finish()
}

// serve records that a handler is running on this stream.
func (s *Stream) serve() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.serving++
}

// served records that a handler has finished, and releases the stream when
// that was the last thing either side had to do on it.
func (s *Stream) served() {
	s.mu.Lock()
	s.serving--
	s.mu.Unlock()

	s.finish()
}

// finish ends this side's half and releases the stream, once the peer has
// ended its own half and no handler is left running on it.
func (s *Stream) finish() {
	s.mu.Lock()

	if !s.ended || s.serving > 0 || len(s.calls) > 0 || s.failed != nil {
		s.mu.Unlock()

		return
	}

	unsent := !s.sentEnd
	s.sentEnd = true
	s.mu.Unlock()

	if unsent {
		if err := s.conn.write(wire.End(s.id)); err != nil && s.conn.Err() == nil {
			s.conn.opts.Logger.Warn("the end of a stream could not be sent",
				"stream", uint32(s.id), "error", err.Error())
		}
	}

	s.conn.release(s.id)
}

// deliverGrant adds a peer's grant to what this side may send.
func (s *Stream) deliverGrant(n uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failed != nil {
		return nil
	}

	if err := s.send.Grant(n); err != nil {
		return fmt.Errorf("grant on stream %d: %w", s.id, err)
	}

	s.wake()

	return nil
}

// fail ends the stream and releases everything waiting on it.
func (s *Stream) fail(reason error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failed != nil {
		return
	}

	s.failed = reason
	close(s.gone)
	s.wake()
}

// reason reports why the stream ended.
func (s *Stream) reason() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.failed
}

// wake releases every goroutine waiting on this stream. The caller holds the
// lock.
func (s *Stream) wake() {
	close(s.change)
	s.change = make(chan struct{})
}
