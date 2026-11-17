// Package wiretest holds the conformance vectors for the plugin wire
// protocol, and nothing else.
//
// Every vector states bytes a human wrote down, not bytes a codec produced.
// That is the whole point of the file: sdk/wire is checked against these, and
// an implementation in another language is checked against the same vectors
// published as docs/wire/plugin-wire-protocol.json by
// cmd/generate-wire-protocol.
//
// A vector that a codec generated proves only that the codec agrees with
// itself.
package wiretest

import (
	"bytes"
	"encoding/hex"
	"fmt"

	"github.com/prusa3d/prusa-connect/sdk/wire"
)

// Reason names why a refusal vector is refused. The names are the wire's own
// and carry no Go in them, because an implementation in another language reads
// them out of the published artefact.
type Reason string

// The reasons a frame, a handshake line or a negotiation is refused.
const (
	// ReasonStreamZero is stream id 0.
	ReasonStreamZero Reason = "stream_zero"
	// ReasonUnknownKind is a kind byte this major does not define.
	ReasonUnknownKind Reason = "unknown_kind"
	// ReasonPayloadTooLarge is a payload over the reader's cap.
	ReasonPayloadTooLarge Reason = "payload_over_the_cap"
	// ReasonChunkTooLarge is a data payload over the chunk size.
	ReasonChunkTooLarge Reason = "data_over_the_chunk"
	// ReasonPayloadNotEmpty is an end frame carrying bytes.
	ReasonPayloadNotEmpty Reason = "end_carries_a_payload"
	// ReasonCreditLength is a credit payload that is not four bytes.
	ReasonCreditLength Reason = "credit_length"
	// ReasonCreditZero is a grant of zero.
	ReasonCreditZero Reason = "credit_zero"
	// ReasonResetTooLarge is a reset reason over the limit.
	ReasonResetTooLarge Reason = "reset_over_the_limit"
	// ReasonTruncated is a channel that closed inside a frame.
	ReasonTruncated Reason = "truncated"
	// ReasonHandshakeLine is a line that is not one usable JSON object.
	ReasonHandshakeLine Reason = "malformed_line"
	// ReasonHandshakeRange is a minor range whose bounds are the wrong way
	// round.
	ReasonHandshakeRange Reason = "empty_minor_range"
	// ReasonHandshakeAddress is a missing or unusable address.
	ReasonHandshakeAddress Reason = "no_usable_address"
	// ReasonProtocolMajor is two peers on different wire majors.
	ReasonProtocolMajor Reason = "different_majors"
	// ReasonNoCommonMinor is two minor ranges that do not overlap.
	ReasonNoCommonMinor Reason = "no_common_minor"
)

// Reasons returns every reason a vector may carry, in publication order. The
// conformance test refuses a reason no vector uses and a vector whose reason is
// not here, so neither list can quietly lose a case.
func Reasons() []Reason {
	return []Reason{
		ReasonStreamZero,
		ReasonUnknownKind,
		ReasonPayloadTooLarge,
		ReasonChunkTooLarge,
		ReasonPayloadNotEmpty,
		ReasonCreditLength,
		ReasonCreditZero,
		ReasonResetTooLarge,
		ReasonTruncated,
		ReasonHandshakeLine,
		ReasonHandshakeRange,
		ReasonHandshakeAddress,
		ReasonProtocolMajor,
		ReasonNoCommonMinor,
	}
}

// Repeat is a payload written as one byte repeated. A vector at the chunk
// boundary or at the payload cap is thousands of identical bytes, and a
// repetition rule states it in one line that any language can build.
type Repeat struct {
	// Byte is the byte to repeat.
	Byte byte
	// Count is how many times, and zero means the vector states no repetition.
	Count int
}

// Bytes builds the repeated payload.
func (r Repeat) Bytes() []byte {
	if r.Count <= 0 {
		return nil
	}

	return bytes.Repeat([]byte{r.Byte}, r.Count)
}

// FrameVector is one frame that every implementation encodes to the same bytes
// and decodes back to the same frame.
type FrameVector struct {
	// Name identifies the vector in a failure report.
	Name string
	// Doc says what the vector holds a reader to.
	Doc string
	// Stream is the frame's stream id.
	Stream wire.StreamID
	// Kind is the frame's kind.
	Kind wire.Kind
	// HeaderHex is the nine header bytes, written by hand.
	HeaderHex string
	// PayloadHex is the payload, written by hand, for a payload small enough
	// to read.
	PayloadHex string
	// Repeat is the payload for a vector at a size boundary, and it is used
	// only when PayloadHex is empty.
	Repeat Repeat
}

// Payload builds the payload this vector describes.
func (v FrameVector) Payload() ([]byte, error) {
	if v.PayloadHex != "" {
		payload, err := hex.DecodeString(v.PayloadHex)
		if err != nil {
			return nil, fmt.Errorf("vector %s: decode payload: %w", v.Name, err)
		}

		return payload, nil
	}

	return v.Repeat.Bytes(), nil
}

// Bytes builds the whole frame as it appears on the wire.
func (v FrameVector) Bytes() ([]byte, error) {
	header, err := hex.DecodeString(v.HeaderHex)
	if err != nil {
		return nil, fmt.Errorf("vector %s: decode header: %w", v.Name, err)
	}

	payload, err := v.Payload()
	if err != nil {
		return nil, err
	}

	return append(header, payload...), nil
}

// Frames returns every frame both directions are checked against.
//
// hides what the vector says, which is the one thing it exists to say.
//
//nolint:mnd // a vector states stream ids and byte counts. Naming each one
func Frames() []FrameVector {
	return []FrameVector{
		{
			Name:       "control_empty_object",
			Doc:        "The smallest control frame. Nine header bytes and two payload bytes.",
			Stream:     1,
			Kind:       wire.KindControl,
			HeaderHex:  "000000020000000101",
			PayloadHex: "7b7d",
		},
		{
			Name:      "control_request",
			Doc:       "A JSON-RPC request on a host-opened stream, which is an odd id.",
			Stream:    1,
			Kind:      wire.KindControl,
			HeaderHex: "000000350000000101",
			PayloadHex: "7b226a736f6e727063223a22322e30222c226964223a312c226d6574686f64223a2272" +
				"65736f757263652e6465736372696265227d",
		},
		{
			Name:      "control_notification",
			Doc:       "A JSON-RPC notification on a child-opened stream, which is an even id.",
			Stream:    2,
			Kind:      wire.KindControl,
			HeaderHex: "0000004b0000000201",
			PayloadHex: "7b226a736f6e727063223a22322e30222c226d6574686f64223a227072696e7465722e" +
				"74656c656d65747279222c22706172616d73223a7b227072696e7465725f6964223a2270" +
				"31227d7d",
		},
		{
			Name:      "data_empty",
			Doc:       "A data frame of no bytes. It is a frame and not an end.",
			Stream:    2,
			Kind:      wire.KindData,
			HeaderHex: "000000000000000202",
		},
		{
			Name:       "data_bytes",
			Doc:        "A data payload holding a zero byte and two bytes that are not UTF-8.",
			Stream:     2,
			Kind:       wire.KindData,
			HeaderHex:  "000000040000000202",
			PayloadHex: "0001feff",
		},
		{
			Name:      "data_at_the_chunk_limit",
			Doc:       "32768 payload bytes, which is the largest a data frame may carry.",
			Stream:    2,
			Kind:      wire.KindData,
			HeaderHex: "000080000000000202",
			Repeat:    Repeat{Byte: 'A', Count: wire.ChunkBytes},
		},
		{
			Name:      "control_at_the_default_cap",
			Doc:       "1048576 payload bytes, which is the default cap. The frame layer bounds the size and the layer above decides whether the bytes parse.",
			Stream:    1,
			Kind:      wire.KindControl,
			HeaderHex: "001000000000000101",
			Repeat:    Repeat{Byte: 'A', Count: wire.DefaultMaxPayloadBytes},
		},
		{
			Name:      "end",
			Doc:       "An end frame carries no payload.",
			Stream:    1,
			Kind:      wire.KindEnd,
			HeaderHex: "000000000000000103",
		},
		{
			Name:      "end_on_the_highest_stream",
			Doc:       "Stream 4294967295, the largest id the field holds, and an odd one.",
			Stream:    4294967295,
			Kind:      wire.KindEnd,
			HeaderHex: "00000000ffffffff03",
		},
		{
			Name:       "reset_with_reason",
			Doc:        "A reset carries text for a log and nothing a peer branches on.",
			Stream:     3,
			Kind:       wire.KindReset,
			HeaderHex:  "000000190000000304",
			PayloadHex: "746865206368696c642073746f707065642072656164696e67",
		},
		{
			Name:      "reset_empty",
			Doc:       "A reset with no reason is legal.",
			Stream:    3,
			Kind:      wire.KindReset,
			HeaderHex: "000000000000000304",
		},
		{
			Name:       "credit_one_chunk",
			Doc:        "A grant of 32768 payload bytes, big-endian in four bytes.",
			Stream:     2,
			Kind:       wire.KindCredit,
			HeaderHex:  "000000040000000205",
			PayloadHex: "00008000",
		},
		{
			Name:       "credit_max",
			Doc:        "A grant of 4294967295, the largest one frame may carry.",
			Stream:     4,
			Kind:       wire.KindCredit,
			HeaderHex:  "000000040000000405",
			PayloadHex: "ffffffff",
		},
	}
}

// RefusalVector is bytes an implementation refuses, and the reason it gives.
type RefusalVector struct {
	// Name identifies the vector in a failure report.
	Name string
	// Doc says what the vector holds a reader to.
	Doc string
	// BytesHex is what the reader is fed, written by hand.
	BytesHex string
	// Repeat is appended to BytesHex when its count is above zero, for a
	// vector that sits just past a size boundary.
	Repeat Repeat
	// MaxPayloadBytes is the cap the reader is built with.
	MaxPayloadBytes int
	// Reason is why the frame is refused.
	Reason Reason
}

// Bytes builds what the reader is fed.
func (v RefusalVector) Bytes() ([]byte, error) {
	head, err := hex.DecodeString(v.BytesHex)
	if err != nil {
		return nil, fmt.Errorf("vector %s: decode bytes: %w", v.Name, err)
	}

	return append(head, v.Repeat.Bytes()...), nil
}

// Refusals returns every frame an implementation refuses.
//
//nolint:mnd // as Frames: the numbers are the vector.
func Refusals() []RefusalVector {
	return []RefusalVector{
		{
			Name:            "stream_zero",
			Doc:             "Stream 0 is not a stream, so a header of nothing but zeroes is not a frame.",
			BytesHex:        "000000000000000003",
			MaxPayloadBytes: wire.DefaultMaxPayloadBytes,
			Reason:          ReasonStreamZero,
		},
		{
			Name:            "kind_zero",
			Doc:             "Kind 0 is not a kind. The five start at 1 so that a zeroed byte is refused.",
			BytesHex:        "000000000000000100",
			MaxPayloadBytes: wire.DefaultMaxPayloadBytes,
			Reason:          ReasonUnknownKind,
		},
		{
			Name:            "kind_unknown",
			Doc:             "Kind 6 is past the five this major defines.",
			BytesHex:        "000000000000000106",
			MaxPayloadBytes: wire.DefaultMaxPayloadBytes,
			Reason:          ReasonUnknownKind,
		},
		{
			Name:            "payload_over_the_cap",
			Doc:             "One byte past the default cap. A reader refuses from the header, before it allocates for the payload.",
			BytesHex:        "001000010000000101",
			Repeat:          Repeat{Byte: 'A', Count: wire.DefaultMaxPayloadBytes + 1},
			MaxPayloadBytes: wire.DefaultMaxPayloadBytes,
			Reason:          ReasonPayloadTooLarge,
		},
		{
			Name:            "payload_over_a_lowered_cap",
			Doc:             "A host that states a cap of 64 refuses a payload of 65, which is how a host bounds a child it knows the shape of.",
			BytesHex:        "000000410000000101",
			Repeat:          Repeat{Byte: 'A', Count: 65},
			MaxPayloadBytes: 64,
			Reason:          ReasonPayloadTooLarge,
		},
		{
			Name:            "data_over_the_chunk",
			Doc:             "32769 bytes in a data frame, one past the chunk size.",
			BytesHex:        "000080010000000202",
			Repeat:          Repeat{Byte: 'A', Count: wire.ChunkBytes + 1},
			MaxPayloadBytes: wire.DefaultMaxPayloadBytes,
			Reason:          ReasonChunkTooLarge,
		},
		{
			Name:            "end_with_a_payload",
			Doc:             "An end frame carrying one byte.",
			BytesHex:        "00000001000000010300",
			MaxPayloadBytes: wire.DefaultMaxPayloadBytes,
			Reason:          ReasonPayloadNotEmpty,
		},
		{
			Name:            "credit_of_zero",
			Doc:             "A grant of zero moves no sender.",
			BytesHex:        "00000004000000020500000000",
			MaxPayloadBytes: wire.DefaultMaxPayloadBytes,
			Reason:          ReasonCreditZero,
		},
		{
			Name:            "credit_of_three_bytes",
			Doc:             "A credit payload shorter than four bytes.",
			BytesHex:        "000000030000000205000001",
			MaxPayloadBytes: wire.DefaultMaxPayloadBytes,
			Reason:          ReasonCreditLength,
		},
		{
			Name:            "credit_of_five_bytes",
			Doc:             "A credit payload longer than four bytes.",
			BytesHex:        "0000000500000002050000000100",
			MaxPayloadBytes: wire.DefaultMaxPayloadBytes,
			Reason:          ReasonCreditLength,
		},
		{
			Name:            "reset_over_the_limit",
			Doc:             "A reset reason of 1025 bytes, one past the limit.",
			BytesHex:        "000004010000000304",
			Repeat:          Repeat{Byte: 'A', Count: wire.MaxResetBytes + 1},
			MaxPayloadBytes: wire.DefaultMaxPayloadBytes,
			Reason:          ReasonResetTooLarge,
		},
		{
			Name:            "truncated_header",
			Doc:             "Five bytes and then the channel closed. A header is nine or it is nothing.",
			BytesHex:        "0000000400",
			MaxPayloadBytes: wire.DefaultMaxPayloadBytes,
			Reason:          ReasonTruncated,
		},
		{
			Name:            "truncated_payload",
			Doc:             "A header promising four payload bytes, followed by two.",
			BytesHex:        "000000040000000202aabb",
			MaxPayloadBytes: wire.DefaultMaxPayloadBytes,
			Reason:          ReasonTruncated,
		},
	}
}

// HandshakeVector is one line a child may write, and what a host makes of it.
type HandshakeVector struct {
	// Name identifies the vector in a failure report.
	Name string
	// Doc says what the vector holds a reader to.
	Doc string
	// Line is the line as the child writes it, without its newline.
	Line string
	// Accepted says whether a host accepts the line.
	Accepted bool
	// Handshake is what the host reads out of an accepted line.
	Handshake wire.Handshake
	// Reason is why a refused line is refused.
	Reason Reason
}

// Handshakes returns every handshake line a host is checked against.
//
//nolint:mnd // as Frames: the numbers are the vector.
func Handshakes() []HandshakeVector {
	return []HandshakeVector{
		{
			Name:     "a_socket_address",
			Doc:      "The shape a child on Linux writes: a socket in a directory the host owns.",
			Line:     `{"protocol":1,"min_minor":0,"max_minor":3,"address":"unix:///run/prusa-connect/plugins/acme.sock"}`,
			Accepted: true,
			Handshake: wire.Handshake{
				Protocol: 1, MinMinor: 0, MaxMinor: 3,
				Address: "unix:///run/prusa-connect/plugins/acme.sock",
			},
		},
		{
			Name:      "the_stdio_pair",
			Doc:       "The shape a child on Windows writes, where the channel is the pair the host already holds.",
			Line:      `{"protocol":1,"min_minor":0,"max_minor":0,"address":"stdio"}`,
			Accepted:  true,
			Handshake: wire.Handshake{Protocol: 1, MinMinor: 0, MaxMinor: 0, Address: wire.StdioAddress},
		},
		{
			Name:      "a_member_the_host_does_not_know",
			Doc:       "An unknown member is ignored. This is what lets one child serve a host older than the minor it was built against.",
			Line:      `{"protocol":1,"min_minor":0,"max_minor":0,"address":"stdio","pid":4123}`,
			Accepted:  true,
			Handshake: wire.Handshake{Protocol: 1, MinMinor: 0, MaxMinor: 0, Address: wire.StdioAddress},
		},
		{
			Name:     "protocol_zero",
			Doc:      "Major 0 is no major, so a line of defaults is not a handshake.",
			Line:     `{"protocol":0,"min_minor":0,"max_minor":0,"address":"stdio"}`,
			Accepted: false,
			Reason:   ReasonHandshakeLine,
		},
		{
			Name:     "not_json",
			Doc:      "A child that printed a log line where its handshake belongs.",
			Line:     `starting acme driver`,
			Accepted: false,
			Reason:   ReasonHandshakeLine,
		},
		{
			Name:     "minors_the_wrong_way_round",
			Doc:      "A range whose lower bound is above its upper bound.",
			Line:     `{"protocol":1,"min_minor":4,"max_minor":2,"address":"stdio"}`,
			Accepted: false,
			Reason:   ReasonHandshakeRange,
		},
		{
			Name:     "no_address",
			Doc:      "A handshake with no address says nothing about where the host connects.",
			Line:     `{"protocol":1,"min_minor":0,"max_minor":0}`,
			Accepted: false,
			Reason:   ReasonHandshakeAddress,
		},
		{
			Name:     "an_address_carrying_a_control_character",
			Doc:      "A bell inside the address, which would reach an operator's terminal through the log.",
			Line:     "{\"protocol\":1,\"min_minor\":0,\"max_minor\":0,\"address\":\"std\\u0007io\"}",
			Accepted: false,
			Reason:   ReasonHandshakeAddress,
		},
	}
}

// NegotiationVector is two version ranges and the minor they settle on.
type NegotiationVector struct {
	// Name identifies the vector in a failure report.
	Name string
	// Doc says what the vector holds a reader to.
	Doc string
	// Host is the range the host implements.
	Host wire.Handshake
	// Child is the range the child declared.
	Child wire.Handshake
	// Accepted says whether the two speak a common version.
	Accepted bool
	// Minor is the version they settle on when they do.
	Minor uint32
	// Reason is why they do not, when they do not.
	Reason Reason
}

// Negotiations returns every version pairing a host is checked against.
//
//nolint:mnd // as Frames: the numbers are the vector.
func Negotiations() []NegotiationVector {
	return []NegotiationVector{
		{
			Name:     "the_same_range",
			Doc:      "Two peers built against the same version.",
			Host:     wire.Handshake{Protocol: 1, MinMinor: 0, MaxMinor: 0, Address: wire.StdioAddress},
			Child:    wire.Handshake{Protocol: 1, MinMinor: 0, MaxMinor: 0, Address: wire.StdioAddress},
			Accepted: true,
			Minor:    0,
		},
		{
			Name:     "an_older_child",
			Doc:      "A newer host and an older child settle on the newest minor the child has.",
			Host:     wire.Handshake{Protocol: 1, MinMinor: 0, MaxMinor: 3, Address: wire.StdioAddress},
			Child:    wire.Handshake{Protocol: 1, MinMinor: 0, MaxMinor: 1, Address: wire.StdioAddress},
			Accepted: true,
			Minor:    1,
		},
		{
			Name:     "an_older_host",
			Doc:      "A newer child offers more than the host knows, and the host takes its own newest.",
			Host:     wire.Handshake{Protocol: 1, MinMinor: 2, MaxMinor: 3, Address: wire.StdioAddress},
			Child:    wire.Handshake{Protocol: 1, MinMinor: 0, MaxMinor: 5, Address: wire.StdioAddress},
			Accepted: true,
			Minor:    3,
		},
		{
			Name:     "ranges_that_do_not_meet",
			Doc:      "A child that dropped the minors the host still speaks.",
			Host:     wire.Handshake{Protocol: 1, MinMinor: 0, MaxMinor: 1, Address: wire.StdioAddress},
			Child:    wire.Handshake{Protocol: 1, MinMinor: 2, MaxMinor: 3, Address: wire.StdioAddress},
			Accepted: false,
			Reason:   ReasonNoCommonMinor,
		},
		{
			Name:     "different_majors",
			Doc:      "A major is what changes the meaning of a frame an older peer would still parse, so no frame is exchanged at all.",
			Host:     wire.Handshake{Protocol: 1, MinMinor: 0, MaxMinor: 0, Address: wire.StdioAddress},
			Child:    wire.Handshake{Protocol: 2, MinMinor: 0, MaxMinor: 0, Address: wire.StdioAddress},
			Accepted: false,
			Reason:   ReasonProtocolMajor,
		},
	}
}
