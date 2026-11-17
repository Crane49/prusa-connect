package wire

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// The fixed sizes of the wire, as the specification publishes them.
const (
	// HeaderBytes is the fixed part of a frame: a payload length, a stream id
	// and a kind, in that order and big-endian.
	HeaderBytes = 9

	// DefaultMaxPayloadBytes is the largest payload a reader accepts when its
	// caller states no smaller cap. A caller should state a smaller one, because
	// the cap bounds a hostile or broken peer.
	DefaultMaxPayloadBytes = 1 << 20

	// ChunkBytes is the largest payload of a data frame. A sender with more
	// bytes sends several data frames on the same stream.
	ChunkBytes = 32 * 1024

	// MaxResetBytes is the largest reason of a reset frame. The reason is
	// diagnostic text for a log.
	MaxResetBytes = 1024

	// CreditBytes is the payload length of a credit frame, which is one
	// big-endian count of payload bytes.
	CreditBytes = 4

	// MaxCredit is the largest total credit of one stream. A grant that would
	// take the total past it is a protocol error.
	MaxCredit = 1<<32 - 1
)

// The wire version this package implements. The major is in the handshake
// line, and two peers with different majors never exchange a frame.
const (
	// ProtocolMajor is the wire major. It changes when a frame that an older
	// peer parses would be understood wrongly.
	ProtocolMajor = 1

	// MinMinor is the oldest minor this package speaks.
	MinMinor = 0

	// MaxMinor is the newest minor this package speaks. A minor adds something
	// a peer that does not know it may ignore.
	MaxMinor = 0
)

// The refusals every reader and writer here returns. A caller separates a
// malformed peer from a broken pipe by testing these with errors.Is.
var (
	// ErrStreamZero reports stream id 0, which is not a stream.
	ErrStreamZero = errors.New("wire: stream 0 is not a stream")

	// ErrUnknownKind reports a kind byte outside the five this version defines.
	ErrUnknownKind = errors.New("wire: unknown frame kind")

	// ErrPayloadTooLarge reports a payload over the reader's or writer's cap.
	ErrPayloadTooLarge = errors.New("wire: payload over the cap")

	// ErrChunkTooLarge reports a data payload over ChunkBytes.
	ErrChunkTooLarge = errors.New("wire: data payload over the chunk size")

	// ErrResetTooLarge reports a reset reason over MaxResetBytes.
	ErrResetTooLarge = errors.New("wire: reset reason over the limit")

	// ErrPayloadNotEmpty reports an end frame with a payload.
	ErrPayloadNotEmpty = errors.New("wire: end carries a payload")

	// ErrCreditLength reports a credit payload that is not CreditBytes long.
	ErrCreditLength = errors.New("wire: credit payload is not four bytes")

	// ErrCreditZero reports a grant of zero.
	ErrCreditZero = errors.New("wire: credit of zero")

	// ErrDesynchronised reports a reader that refused a frame earlier. Close
	// the connection.
	ErrDesynchronised = errors.New("wire: reader stopped at a refused frame")
)

// Kind is what a frame's payload means. The five values are the closed set of
// wire major 1.
type Kind uint8

// The five kinds. The zero byte is not one of them, so a zeroed header is
// refused.
const (
	// KindControl is one JSON-RPC 2.0 object as UTF-8.
	KindControl Kind = 1

	// KindData is raw bytes, at most ChunkBytes of them.
	KindData Kind = 2

	// KindEnd says the sender writes nothing more on this stream.
	KindEnd Kind = 3

	// KindReset ends a stream in both directions and gives a reason.
	KindReset Kind = 4

	// KindCredit grants the peer payload bytes to send on this stream.
	KindCredit Kind = 5
)

// KindDoc describes one kind, as the specification publishes it.
type KindDoc struct {
	// Kind is the byte on the wire.
	Kind Kind
	// Name is the kind's name in the specification.
	Name string
	// Summary is what the kind is for, in one sentence.
	Summary string
	// Payload is what the payload of this kind must be.
	Payload string
}

// Kinds returns the closed set of kinds in wire order.
func Kinds() []KindDoc {
	return []KindDoc{
		{
			Kind:    KindControl,
			Name:    "control",
			Summary: "One call, one answer or one notification.",
			Payload: "One JSON-RPC 2.0 object, encoded as UTF-8. One object per frame and never a batch.",
		},
		{
			Kind:    KindData,
			Name:    "data",
			Summary: "Bulk bytes, so that a file or a video frame never travels inside JSON.",
			Payload: "Raw bytes, at most 32768 of them.",
		},
		{
			Kind:    KindEnd,
			Name:    "end",
			Summary: "The sender writes nothing more on this stream.",
			Payload: "Empty.",
		},
		{
			Kind:    KindReset,
			Name:    "reset",
			Summary: "The stream ends now, in both directions.",
			Payload: "A reason as UTF-8, at most 1024 bytes, and it may be empty.",
		},
		{
			Kind:    KindCredit,
			Name:    "credit",
			Summary: "The receiver grants the sender room to send.",
			Payload: "Four bytes, a big-endian count of payload bytes, and never zero.",
		},
	}
}

// String returns the kind's name in the specification, or a decimal for a byte
// this version does not define.
func (k Kind) String() string {
	for _, doc := range Kinds() {
		if doc.Kind == k {
			return doc.Name
		}
	}

	return fmt.Sprintf("kind(%d)", uint8(k))
}

// Valid reports whether the kind is one of the five.
func (k Kind) Valid() bool {
	return k >= KindControl && k <= KindCredit
}

// StreamID names one stream inside a connection. Odd ids are opened by the
// host and even ids by the child. An id is used once per connection.
type StreamID uint32

// Valid reports whether the id names a stream. Zero does not.
func (s StreamID) Valid() bool {
	return s != 0
}

// OpenedByHost reports whether the host opened this stream. A valid id that
// answers false was opened by the child.
func (s StreamID) OpenedByHost() bool {
	return s.Valid() && s%2 == 1
}

// Frame is one message on the wire. The Payload of a frame a Reader returned
// points into the reader's buffer and is valid until the next call to Next.
// Clone copies it out.
type Frame struct {
	// Stream is the stream this frame belongs to.
	Stream StreamID
	// Kind is what the payload means.
	Kind Kind
	// Payload is the bytes after the header, and it is empty for an end frame.
	Payload []byte
}

// Clone returns a frame that owns its payload.
func (f Frame) Clone() Frame {
	f.Payload = append([]byte(nil), f.Payload...)

	return f
}

// Control returns a control frame with one encoded JSON-RPC object.
func Control(stream StreamID, object []byte) Frame {
	return Frame{Stream: stream, Kind: KindControl, Payload: object}
}

// Data returns a data frame with bulk bytes.
func Data(stream StreamID, payload []byte) Frame {
	return Frame{Stream: stream, Kind: KindData, Payload: payload}
}

// End returns an end frame.
func End(stream StreamID) Frame {
	return Frame{Stream: stream, Kind: KindEnd}
}

// Reset returns a reset frame with a reason for a log.
func Reset(stream StreamID, reason string) Frame {
	return Frame{Stream: stream, Kind: KindReset, Payload: []byte(reason)}
}

// Credit returns a credit frame granting the peer n payload bytes.
func Credit(stream StreamID, n uint32) Frame {
	payload := make([]byte, CreditBytes)
	binary.BigEndian.PutUint32(payload, n)

	return Frame{Stream: stream, Kind: KindCredit, Payload: payload}
}

// Grant reads the count of a credit frame.
func Grant(f Frame) (uint32, error) {
	if f.Kind != KindCredit {
		return 0, fmt.Errorf("read a grant from a %s frame: %w", f.Kind, ErrUnknownKind)
	}

	if err := checkPayload(f); err != nil {
		return 0, err
	}

	return binary.BigEndian.Uint32(f.Payload), nil
}

// checkPayload checks a payload against the rules of its kind. A writer runs
// it before sending and a reader before returning.
func checkPayload(f Frame) error {
	switch f.Kind {
	case KindData:
		if len(f.Payload) > ChunkBytes {
			return fmt.Errorf("data payload of %d bytes: %w", len(f.Payload), ErrChunkTooLarge)
		}
	case KindEnd:
		if len(f.Payload) != 0 {
			return fmt.Errorf("end payload of %d bytes: %w", len(f.Payload), ErrPayloadNotEmpty)
		}
	case KindReset:
		if len(f.Payload) > MaxResetBytes {
			return fmt.Errorf("reset reason of %d bytes: %w", len(f.Payload), ErrResetTooLarge)
		}
	case KindCredit:
		if len(f.Payload) != CreditBytes {
			return fmt.Errorf("credit payload of %d bytes: %w", len(f.Payload), ErrCreditLength)
		}

		if binary.BigEndian.Uint32(f.Payload) == 0 {
			return ErrCreditZero
		}
	case KindControl:
	}

	return nil
}

// checkFrame checks every rule that does not depend on the direction: the
// stream, the kind, the cap and the payload.
func checkFrame(f Frame, maxPayload int) error {
	if !f.Stream.Valid() {
		return ErrStreamZero
	}

	if !f.Kind.Valid() {
		return fmt.Errorf("kind %d: %w", uint8(f.Kind), ErrUnknownKind)
	}

	if len(f.Payload) > maxPayload {
		return fmt.Errorf("payload of %d bytes against a cap of %d: %w",
			len(f.Payload), maxPayload, ErrPayloadTooLarge)
	}

	return checkPayload(f)
}
