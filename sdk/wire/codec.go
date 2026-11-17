package wire

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Writer frames values onto a duplex channel. One frame is one call to the
// underlying writer. A Writer is not safe for concurrent use.
type Writer struct {
	w   io.Writer
	max int
	buf []byte
}

// NewWriter returns a Writer that refuses a payload over maxPayload. A
// maxPayload of zero or less means DefaultMaxPayloadBytes.
func NewWriter(w io.Writer, maxPayload int) *Writer {
	return &Writer{w: w, max: capOf(maxPayload)}
}

// Write sends one frame. It refuses a frame this version does not define.
func (w *Writer) Write(f Frame) error {
	if err := checkFrame(f, w.max); err != nil {
		return fmt.Errorf("write %s frame on stream %d: %w", f.Kind, f.Stream, err)
	}

	size := HeaderBytes + len(f.Payload)
	if cap(w.buf) < size {
		w.buf = make([]byte, size)
	}

	w.buf = w.buf[:size]
	// checkFrame refused a payload over the cap, and capOf clamps the cap to
	// what the length field can express, so this conversion cannot truncate.
	//nolint:gosec // bounded above by checkFrame and capOf.
	binary.BigEndian.PutUint32(w.buf[0:4], uint32(len(f.Payload)))
	binary.BigEndian.PutUint32(w.buf[4:8], uint32(f.Stream))
	w.buf[8] = byte(f.Kind)
	copy(w.buf[HeaderBytes:], f.Payload)

	if _, err := w.w.Write(w.buf); err != nil {
		return fmt.Errorf("write %s frame on stream %d: %w", f.Kind, f.Stream, err)
	}

	return nil
}

// Reader parses frames from a duplex channel. A refusal is final: the
// position in the stream is lost, so every later call returns
// ErrDesynchronised. The caller closes the connection.
type Reader struct {
	r      io.Reader
	max    int
	header [HeaderBytes]byte
	buf    []byte
	err    error
}

// NewReader returns a Reader that refuses a payload over maxPayload before
// reading it. A maxPayload of zero or less means DefaultMaxPayloadBytes.
func NewReader(r io.Reader, maxPayload int) *Reader {
	return &Reader{r: r, max: capOf(maxPayload)}
}

// Next reads one frame. The returned payload points into the Reader's buffer
// and is valid until the next call. Frame.Clone copies it out. Next returns
// io.EOF when the peer closed the channel between frames, and
// io.ErrUnexpectedEOF when it closed inside one.
func (r *Reader) Next() (Frame, error) {
	if r.err != nil {
		return Frame{}, r.err
	}

	if _, err := io.ReadFull(r.r, r.header[:]); err != nil {
		if errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			return Frame{}, io.EOF
		}

		r.err = fmt.Errorf("read frame header: %w", err)

		return Frame{}, r.err
	}

	size := binary.BigEndian.Uint32(r.header[0:4])
	frame := Frame{
		Stream: StreamID(binary.BigEndian.Uint32(r.header[4:8])),
		Kind:   Kind(r.header[8]),
	}

	// The header is checked before the payload is read, so the cap bounds
	// memory.
	if !frame.Stream.Valid() {
		return r.refuse(ErrStreamZero)
	}

	if !frame.Kind.Valid() {
		return r.refuse(fmt.Errorf("kind %d: %w", r.header[8], ErrUnknownKind))
	}

	if int64(size) > int64(r.max) {
		return r.refuse(fmt.Errorf("payload of %d bytes against a cap of %d: %w",
			size, r.max, ErrPayloadTooLarge))
	}

	if cap(r.buf) < int(size) {
		r.buf = make([]byte, size)
	}

	frame.Payload = r.buf[:size]

	if _, err := io.ReadFull(r.r, frame.Payload); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}

		r.err = fmt.Errorf("read %s payload on stream %d: %w", frame.Kind, frame.Stream, err)

		return Frame{}, r.err
	}

	if err := checkPayload(frame); err != nil {
		return r.refuse(err)
	}

	return frame, nil
}

// refuse reports why the Reader stopped, and records that it stopped. Every
// later call answers ErrDesynchronised.
func (r *Reader) refuse(err error) (Frame, error) {
	r.err = fmt.Errorf("%w: %w", ErrDesynchronised, err)

	return Frame{}, err
}

// lengthFieldBits is the width of a frame's payload length field.
const lengthFieldBits = 32

// capOf turns a caller's cap into the one a Reader or a Writer uses. A cap
// above what the length field can express is clamped to it.
func capOf(maxPayload int) int {
	if maxPayload <= 0 {
		return DefaultMaxPayloadBytes
	}

	// The largest payload the length field can express. It is a local,
	// because a constant conversion would not compile on a 32-bit build.
	limit := int64(1)<<lengthFieldBits - 1
	if int64(maxPayload) > limit {
		return int(limit)
	}

	return maxPayload
}
