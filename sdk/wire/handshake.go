package wire

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxHandshakeBytes is the largest handshake line a host reads, the newline
// counted.
const MaxHandshakeBytes = 4096

// The refusals the handshake returns.
var (
	// ErrHandshakeLine reports a line that is not one JSON object, is over
	// MaxHandshakeBytes, or has a member with a value of the wrong shape.
	ErrHandshakeLine = errors.New("wire: malformed handshake line")

	// ErrHandshakeRange reports a minor range whose lower bound is above its
	// upper bound.
	ErrHandshakeRange = errors.New("wire: handshake declares an empty minor range")

	// ErrHandshakeAddress reports a missing address, or one with a control
	// character or a byte that is not UTF-8.
	ErrHandshakeAddress = errors.New("wire: handshake carries no usable address")

	// ErrProtocolMajor reports two peers whose wire majors differ.
	ErrProtocolMajor = errors.New("wire: the two peers speak different wire majors")

	// ErrNoCommonMinor reports two minor ranges that do not overlap.
	ErrNoCommonMinor = errors.New("wire: the two minor ranges do not overlap")
)

// StdioAddress is the address a child writes when the host gave it a stdio
// pair and no socket to listen on.
const StdioAddress = "stdio"

// Handshake is the one line a child writes to its standard output before it
// serves, and the version range of a host. A member this version does not
// define is ignored.
type Handshake struct {
	// Protocol is the wire major the peer implements.
	Protocol uint32 `json:"protocol"`
	// MinMinor is the oldest minor the peer implements.
	MinMinor uint32 `json:"min_minor"`
	// MaxMinor is the newest minor the peer implements.
	MaxMinor uint32 `json:"max_minor"`
	// Address is where the host reaches the child: a socket path, a URL, or
	// StdioAddress when the channel is the pair the host already has.
	Address string `json:"address"`
}

// Validate reports what makes a handshake unusable.
func (h Handshake) Validate() error {
	if h.Protocol == 0 {
		return fmt.Errorf("protocol 0: %w", ErrHandshakeLine)
	}

	if h.MinMinor > h.MaxMinor {
		return fmt.Errorf("minors %d to %d: %w", h.MinMinor, h.MaxMinor, ErrHandshakeRange)
	}

	if h.Address == "" {
		return ErrHandshakeAddress
	}

	if !utf8.ValidString(h.Address) {
		return fmt.Errorf("address is not utf-8: %w", ErrHandshakeAddress)
	}

	if strings.ContainsFunc(h.Address, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return fmt.Errorf("address carries a control character: %w", ErrHandshakeAddress)
	}

	return nil
}

// Line renders the handshake as the line a child writes, with its newline.
func (h Handshake) Line() (string, error) {
	if err := h.Validate(); err != nil {
		return "", err
	}

	encoded, err := json.Marshal(h)
	if err != nil {
		return "", fmt.Errorf("encode handshake: %w", err)
	}

	if len(encoded)+1 > MaxHandshakeBytes {
		return "", fmt.Errorf("line of %d bytes: %w", len(encoded)+1, ErrHandshakeLine)
	}

	return string(encoded) + "\n", nil
}

// ParseHandshake reads the line a child wrote. The trailing newline is
// optional.
func ParseHandshake(line string) (Handshake, error) {
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")

	if len(line)+1 > MaxHandshakeBytes {
		return Handshake{}, fmt.Errorf("line of %d bytes: %w", len(line)+1, ErrHandshakeLine)
	}

	if strings.ContainsAny(line, "\n\r") {
		return Handshake{}, fmt.Errorf("line carries a line break: %w", ErrHandshakeLine)
	}

	var h Handshake
	if err := json.Unmarshal([]byte(line), &h); err != nil {
		return Handshake{}, fmt.Errorf("decode handshake: %w: %w", ErrHandshakeLine, err)
	}

	if err := h.Validate(); err != nil {
		return Handshake{}, err
	}

	return h, nil
}

// Negotiate returns the minor version two peers speak: the newest both
// implement. The majors must be equal. Address is not read.
func Negotiate(host, child Handshake) (uint32, error) {
	if host.Protocol != child.Protocol {
		return 0, fmt.Errorf("host speaks major %d and the child speaks %d: %w",
			host.Protocol, child.Protocol, ErrProtocolMajor)
	}

	if host.MinMinor > host.MaxMinor || child.MinMinor > child.MaxMinor {
		return 0, fmt.Errorf("minors %d to %d against %d to %d: %w",
			host.MinMinor, host.MaxMinor, child.MinMinor, child.MaxMinor, ErrHandshakeRange)
	}

	low := max(host.MinMinor, child.MinMinor)
	high := min(host.MaxMinor, child.MaxMinor)

	if low > high {
		return 0, fmt.Errorf("host speaks %d to %d and the child speaks %d to %d: %w",
			host.MinMinor, host.MaxMinor, child.MinMinor, child.MaxMinor, ErrNoCommonMinor)
	}

	return high, nil
}
