package xbuddy

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

// The application framing above the websocket, frozen by earlier research
// and implemented by legacy/connector/xbuddy/protocol/buddy_encoder.go.
//
// Server to printer: a fixed nine-byte header of one type letter and eight hex
// digits of a uint32 id, then the payload. Printer to server: text JSON with no
// header at all, classified by content.
const (
	headerLength = 9
	idDigits     = 8
)

// The type letters the server writes. D belongs to debug, which this port does
// not send.
const (
	frameJSON   = 'J'
	frameGCode  = 'G'
	frameForced = 'F'
	// frameTransfer carries a chunk of a file the device asked for. It is the
	// one frame this port sends as binary and the one whose header id is a
	// transfer's file id rather than a command's.
	frameTransfer = 'T'
)

// transferInline is the value of the transfer key that makes an inbound frame a
// block request rather than telemetry.
const transferInline = `"inline"`

// The errors the codec reports. Callers inspect them with errors.Is.
var (
	// ErrMalformedFrame reports an inbound text frame that is not a JSON object.
	ErrMalformedFrame = errors.New("xbuddy: the printer sent a frame that is not a JSON object")
	// ErrCommandIDRange reports an event whose command_id does not fit the
	// uint32 the framing carries.
	ErrCommandIDRange = errors.New("xbuddy: the command id does not fit a uint32")
	// ErrBlockRequestRange reports a block request whose file id or byte range
	// the transfer wire cannot express.
	ErrBlockRequestRange = errors.New("xbuddy: the block request names a range this wire cannot carry")
)

// frameKind is the type letter one verb travels under.
type frameKind byte

// encodeFrame renders one server-to-printer frame.
func encodeFrame(kind frameKind, id uint32, payload []byte) []byte {
	out := make([]byte, 0, headerLength+len(payload))
	out = fmt.Appendf(out, "%c%08x", rune(kind), id)

	return append(out, payload...)
}

// encodeJSONCommand renders the payload of a J frame: the verb and, when it has
// arguments, a kwargs object.
func encodeJSONCommand(name string, kwargs map[string]json.RawMessage) ([]byte, error) {
	body := struct {
		Command string                     `json:"command"`
		Kwargs  map[string]json.RawMessage `json:"kwargs,omitempty"`
	}{Command: name, Kwargs: kwargs}

	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode the %s payload: %w", name, err)
	}

	return encoded, nil
}

// inboundKind is what an inbound text frame turned out to be. The printer tags
// nothing, so the content is the whole of the classification.
type inboundKind uint8

const (
	// inboundTelemetry is the default: anything that is not an event and not a
	// block request.
	inboundTelemetry inboundKind = iota
	// inboundEvent carries an event key.
	inboundEvent
	// inboundBlockRequest carries transfer:"inline".
	inboundBlockRequest
)

// printerEvent is one printer-to-server event.
//
// CommandID is int64 rather than uint32 because the wire is JSON and a firmware
// that sent a negative number would otherwise land on a valid id by conversion.
// It is range-checked once, in commandID.
type printerEvent struct {
	Event         string          `json:"event"`
	CommandID     *int64          `json:"command_id,omitempty"`
	TransferID    *int64          `json:"transfer_id,omitempty"`
	DialogID      *int64          `json:"dialog_id,omitempty"`
	Reason        string          `json:"reason,omitempty"`
	MachineReason string          `json:"machine_reason,omitempty"`
	State         string          `json:"state,omitempty"`
	Data          json.RawMessage `json:"data,omitempty"`
}

// commandID reports the event's command id. ok is false when the event carries
// none, and err reports one outside the uint32 the framing can express.
func (e printerEvent) commandID() (id uint32, ok bool, err error) {
	if e.CommandID == nil {
		return 0, false, nil
	}

	value := *e.CommandID
	if value < 0 || value > math.MaxUint32 {
		return 0, false, fmt.Errorf("%w: %d", ErrCommandIDRange, value)
	}

	return uint32(value), true, nil
}

// blockRequest is one ask for a range of the file a transfer is carrying.
//
// Every field is int64 rather than the uint32 the wire carries for the reason
// printerEvent.CommandID is: the wire is JSON, and a firmware that sent a
// negative number would otherwise land on a valid id by conversion. They are
// range-checked once, in readBlockRequest.
//
// Hash and TeamID appear on the first request of a transfer and on no other, so
// they are what names the transfer the device is starting; every request after
// it is keyed by FileID alone.
type blockRequest struct {
	FileID int64  `json:"file_id"`
	Chunk  int64  `json:"chunk"`
	Start  int64  `json:"start"`
	End    int64  `json:"end"`
	Hash   string `json:"hash"`
	TeamID int64  `json:"team_id"`
}

// block is one block request whose numbers have been found expressible.
//
// End is inclusive, which is what the shipping streamer reads it as
// (legacy/connector/xbuddy/transfer/transfer.go: it stops when the next chunk
// would start past End).
type block struct {
	fileID uint32
	chunk  int
	start  int64
	end    int64
	// initial says the request carried a hash, so it opens a transfer rather
	// than continuing one.
	initial bool
	hash    string
	teamID  int64
}

// readBlockRequest reads one block request and reports a range this wire cannot
// carry rather than converting it into a different one.
//
// The file id is checked because it is echoed back as the header of every chunk
// this transfer sends: an id that wrapped would address another transfer's
// frames. The byte range is checked because a negative start reads a file
// backwards, an end below the start is a block with no bytes in it, and an end at
// the int64 ceiling makes the length of the range overflow to a negative count
// that serves nothing. All of them arrive from a firmware this build does not
// control, and the whole transfer wire is uint32-shaped, so that is the bound the
// range is held to as well.
func readBlockRequest(fields map[string]json.RawMessage) (block, error) {
	raw, err := decodeBlockRequest(fields)
	if err != nil {
		return block{}, err
	}

	switch {
	case raw.FileID < 0 || raw.FileID > math.MaxUint32:
		return block{}, fmt.Errorf("%w: file_id %d", ErrBlockRequestRange, raw.FileID)
	case raw.Start < 0 || raw.End < raw.Start || raw.End > math.MaxUint32:
		return block{}, fmt.Errorf("%w: bytes %d to %d", ErrBlockRequestRange, raw.Start, raw.End)
	case raw.Chunk <= 0 || raw.Chunk > maxChunkBytes:
		return block{}, fmt.Errorf("%w: chunk %d", ErrBlockRequestRange, raw.Chunk)
	}

	_, carriesHash := fields["hash"]

	return block{
		fileID:  uint32(raw.FileID),
		chunk:   int(raw.Chunk),
		start:   raw.Start,
		end:     raw.End,
		initial: carriesHash,
		hash:    raw.Hash,
		teamID:  raw.TeamID,
	}, nil
}

// decodeBlockRequest reads the request out of the fields the frame was
// classified from, so the frame is parsed once.
func decodeBlockRequest(fields map[string]json.RawMessage) (blockRequest, error) {
	encoded, err := json.Marshal(fields)
	if err != nil {
		return blockRequest{}, fmt.Errorf("%w: %w", ErrMalformedFrame, err)
	}

	var raw blockRequest
	if unmarshalErr := json.Unmarshal(encoded, &raw); unmarshalErr != nil {
		return blockRequest{}, fmt.Errorf("%w: %w", ErrMalformedFrame, unmarshalErr)
	}

	return raw, nil
}

// writeTransferHeader puts a chunk's nine-byte header at the front of out and
// reports how many bytes it wrote.
//
// It writes into a buffer the caller owns because the caller reads the chunk
// into the same one: a transfer allocates its buffer once and nothing it sends
// scales with the file's length.
func writeTransferHeader(out []byte, fileID uint32) int {
	header := fmt.Appendf(out[:0], "%c%08x", frameTransfer, fileID)

	return len(header)
}

// inboundMessage is one classified printer-to-server frame.
type inboundMessage struct {
	kind inboundKind
	// event is set when kind is inboundEvent.
	event printerEvent
	// fields is the whole object, values left encoded. Telemetry reads it, and
	// a result carries it back to the caller verbatim so that a key this
	// version does not understand survives into the result's Raw.
	fields map[string]json.RawMessage
	// raw is the frame as it arrived.
	raw []byte
}

// classify reads one inbound text frame.
//
// The order matters and is the shipping order
// (legacy/connector/xbuddy/protocol/buddy_decoder.go): an event key wins, then
// transfer:"inline", and everything else is telemetry. A frame carrying both an
// event key and a transfer key is an event, which is what the firmware means by
// TRANSFER_FINISHED.
func classify(data []byte) (inboundMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return inboundMessage{}, fmt.Errorf("%w: %w", ErrMalformedFrame, err)
	}

	message := inboundMessage{fields: fields, raw: data}

	if _, isEvent := fields["event"]; isEvent {
		if err := json.Unmarshal(data, &message.event); err != nil {
			return inboundMessage{}, fmt.Errorf("%w: %w", ErrMalformedFrame, err)
		}

		message.kind = inboundEvent

		return message, nil
	}

	if transfer, ok := fields["transfer"]; ok && string(transfer) == transferInline {
		message.kind = inboundBlockRequest

		return message, nil
	}

	message.kind = inboundTelemetry

	return message, nil
}
