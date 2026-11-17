package stream

import (
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/event"
)

// Encoding is a wire form of an envelope. Memoisation is keyed on the encoding
// and never on the subscriber. Subscribers differ only by protocol, so
// identical data is marshalled once per encoding and not once per subscriber.
type Encoding uint8

const (
	// EncodingJSON is structured CloudEvents JSON, the only mode every binding
	// agrees on.
	EncodingJSON Encoding = iota
	// EncodingSSE is that same JSON inside a server-sent event frame. The byte
	// layout only; the handler that writes it to a socket lives elsewhere.
	EncodingSSE

	encodingCount
)

const (
	sseIDField    = "id: "
	sseEventField = "event: "
	sseDataField  = "data: "
	sseNewline    = "\n"
	// sseOverhead is the field names and newlines of one frame, so the buffer
	// is sized in a single allocation.
	sseOverhead = len(sseIDField) + len(sseEventField) + len(sseDataField) + 4*len(sseNewline)
)

// SSEHeartbeat is the comment frame an idle connection sends, so an
// intermediary that reaps a silent socket does not take a subscriber off a
// stream that is merely quiet. A comment carries no id, so it cannot move the
// place a reader would resume from.
const SSEHeartbeat = ": heartbeat" + sseNewline + sseNewline

const (
	// sseRetryField is the reconnection time, in milliseconds. A client that is
	// given none reconnects on whatever its own engine defaults to.
	sseRetryField = "retry: "
	// decimal is the base every number on this wire is written in.
	decimal = 10
)

// AppendSSERetry appends the frame that sets a reader's reconnection delay. It
// carries no data member, so the HTML standard's dispatch step returns before
// any event reaches page code: the delay is taken and nothing is delivered.
func AppendSSERetry(dst []byte, d time.Duration) []byte {
	dst = append(dst, sseRetryField...)
	dst = strconv.AppendInt(dst, d.Milliseconds(), decimal)
	dst = append(dst, sseNewline...)

	return append(dst, sseNewline...)
}

// AppendSSECursor appends a frame carrying only the place to resume from. An
// event's own id names one origin, so a reader following several that replays
// it resumes one of them and starts the rest wherever the hub puts an origin it
// was handed no place in; this stamps the whole cursor instead.
//
// It carries no data member, so the HTML standard's dispatch step sets the last
// event id and returns before any event reaches page code. The reader's next
// Last-Event-ID is complete and nothing is delivered to be filtered out.
func AppendSSECursor(dst []byte, cursor event.Cursor) []byte {
	dst = append(dst, sseIDField...)
	dst, _ = cursor.AppendText(dst)
	dst = append(dst, sseNewline...)

	return append(dst, sseNewline...)
}

// Valid reports whether e names an encoding this package can produce.
func (e Encoding) Valid() bool { return e < encodingCount }

// String names the encoding, for logs and metric labels.
func (e Encoding) String() string {
	switch e {
	case EncodingJSON:
		return "json"
	case EncodingSSE:
		return "sse"
	case encodingCount:
		return "invalid"
	}

	return "invalid"
}

// Frame is one encoded envelope on its way to one subscriber. Bytes is shared
// with every other subscriber on the same encoding and must not be modified.
//
// Position is the event's place in its own origin's stream, and is the zero
// offset for telemetry, which occupies no place in it.
type Frame struct {
	Position event.Position
	Class    event.Class
	Type     string
	Bytes    []byte
}

// SSEControlFrame renders a frame the transport writes for one reader. The hub
// did not publish it: there is no envelope behind it and no origin it belongs
// to. It is built per reader, so the caller keeps it off the fan-out path.
//
// The byte layout lives here because this package owns it.
func SSEControlFrame(id, eventType string, data []byte) []byte {
	out := make([]byte, 0, len(id)+len(eventType)+len(data)+sseOverhead)

	if id != "" {
		out = append(out, sseIDField...)
		out = append(out, id...)
		out = append(out, sseNewline...)
	}

	out = append(out, sseEventField...)
	out = append(out, eventType...)
	out = append(out, sseNewline...)
	out = append(out, sseDataField...)
	out = append(out, data...)
	out = append(out, sseNewline...)
	out = append(out, sseNewline...)

	return out
}

// variant is one lazily-derived encoding of a message, computed by whichever
// subscriber reaches it first.
type variant struct {
	once  sync.Once
	bytes []byte
}

// Message is one published envelope during its fan-out. Its JSON is marshalled
// once, when it enters the hub, and every subscriber gets the same backing
// array; any further encoding is derived from those bytes at most once.
//
// Nothing may write through a slice this type returns.
type Message struct {
	env event.Envelope
	// key collapses repeated telemetry: the newest value for a key supersedes
	// whatever was queued under it.
	key      string
	position event.Position
	json     []byte
	// aclEpoch is the generation of the authorization graph when this event was
	// published. A subscriber attached to the origin afterwards replays only
	// what was published at a generation its own grant covers.
	aclEpoch uint64
	// variants is indexed by Encoding. Slot zero is unused because JSON is
	// eager: the hub needs those bytes for every subscriber anyway.
	variants [encodingCount]variant
}

// newMessage marshals the envelope once. A payload the core cannot encode is
// reported to the publisher rather than to every subscriber.
func newMessage(env event.Envelope, origin event.Origin, key string) (*Message, error) {
	encoded, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("encode envelope %s: %w", env.ID, err)
	}

	return &Message{env: env, key: key, position: event.Position{Origin: origin}, json: encoded}, nil
}

// Envelope returns the envelope this message carries.
func (m *Message) Envelope() event.Envelope { return m.env }

// Position returns the message's place in its origin's stream.
func (m *Message) Position() event.Position { return m.position }

// Size returns the marshalled size in bytes, which is what the per-subscriber
// queues are bounded by. A count bound would not bound memory: telemetry frames
// vary by an order of magnitude.
func (m *Message) Size() int { return len(m.json) }

// Encode returns the message in enc. The first caller for an encoding pays for
// it and every later caller shares the result, so a thousand subscribers cost
// one marshal.
func (m *Message) Encode(enc Encoding) []byte {
	if enc == EncodingJSON || !enc.Valid() {
		return m.json
	}

	v := &m.variants[enc]
	v.once.Do(func() { v.bytes = m.derive(enc) })

	return v.bytes
}

// frame packages the message for one subscriber's encoding.
func (m *Message) frame(enc Encoding) Frame {
	return Frame{
		Position: m.position,
		Class:    m.env.Class,
		Type:     m.env.Type,
		Bytes:    m.Encode(enc),
	}
}

func (m *Message) derive(enc Encoding) []byte {
	if enc != EncodingSSE {
		return m.json
	}

	id := m.position.ID(m.env.Epoch)
	out := make([]byte, 0, len(m.json)+len(id)+len(m.env.Type)+sseOverhead)

	// A frame with no id leaves Last-Event-ID alone, which is what telemetry
	// needs: it holds no resumable place, and stamping one would hand a reader
	// a position it cannot resume from.
	if id != "" {
		out = append(out, sseIDField...)
		out = append(out, id...)
		out = append(out, sseNewline...)
	}

	out = append(out, sseEventField...)
	out = append(out, m.env.Type...)
	out = append(out, sseNewline...)
	out = append(out, sseDataField...)
	out = append(out, m.json...)
	out = append(out, sseNewline...)
	out = append(out, sseNewline...)

	return out
}
