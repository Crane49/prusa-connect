package simulator

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// The application framing above the websocket: a fixed nine-byte header of one
// type letter and eight hex digits of a uint32 id, then the payload.
//
// This is the printer's own reader. It shares no package with the driver that
// writes these frames. With a shared codec, an encoder bug would agree with
// itself on both sides and the test would still pass.
const (
	headerLength = 9
	idDigits     = 8
)

// The type letters. Server to printer only; printer to server is always text
// JSON with no header at all, classified by content.
const (
	frameJSON       = 'J'
	frameGCode      = 'G'
	frameForced     = 'F'
	frameDebug      = 'D'
	frameTransfer   = 'T'
	transferInline  = "inline"
	transferMaximum = 1 << 32 // the id and the size are uint32, so 4 GiB is not expressible
)

// ErrShortFrame is a frame with no room for the nine-byte header.
var ErrShortFrame = errors.New("frame shorter than the nine-byte header")

// ErrUnknownFrameType is a type letter the framing does not define.
var ErrUnknownFrameType = errors.New("unknown frame type")

// frame is one decoded server-to-printer message.
type frame struct {
	kind    byte
	id      uint32
	payload []byte
}

// jsonCommand is the payload of a J frame.
type jsonCommand struct {
	Command string         `json:"command"`
	Kwargs  map[string]any `json:"kwargs,omitempty"`
}

// event is one printer-to-server event. The server classifies an inbound text
// frame by content, and the presence of this key is what makes it an event.
type event struct {
	Event      string         `json:"event"`
	CommandID  *uint32        `json:"command_id,omitempty"`
	DialogID   *int64         `json:"dialog_id,omitempty"`
	TransferID *int           `json:"transfer_id,omitempty"`
	State      string         `json:"state,omitempty"`
	Reason     string         `json:"reason,omitempty"`
	Source     string         `json:"source,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
}

// decodeFrame reads the nine-byte header.
//
// It is stricter than a printer where the printer's behaviour is unknown. A
// header whose id is not eight hex digits is refused, and so is an undefined
// type letter.
func decodeFrame(data []byte) (frame, error) {
	if len(data) < headerLength {
		return frame{}, fmt.Errorf("%w: %d bytes", ErrShortFrame, len(data))
	}

	kind := data[0]

	switch kind {
	case frameJSON, frameGCode, frameForced, frameDebug, frameTransfer:
	default:
		return frame{}, fmt.Errorf("%w: %q", ErrUnknownFrameType, string(kind))
	}

	id, err := strconv.ParseUint(string(data[1:headerLength]), 16, idDigits*4)
	if err != nil {
		return frame{}, fmt.Errorf("frame id is not eight hex digits: %w", err)
	}

	return frame{kind: kind, id: uint32(id), payload: data[headerLength:]}, nil
}

// decodeJSONCommand reads a J frame's payload.
func decodeJSONCommand(payload []byte) (jsonCommand, error) {
	var command jsonCommand

	if err := json.Unmarshal(payload, &command); err != nil {
		return jsonCommand{}, fmt.Errorf("parse the command payload: %w", err)
	}

	if command.Command == "" {
		return jsonCommand{}, errors.New("the command payload names no command")
	}

	return command, nil
}
