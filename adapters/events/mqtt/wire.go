package mqtt

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// The MQTT 3.1.1 control packets this adapter uses. It never subscribes.
const (
	connectPacket    byte = 1
	connackPacket    byte = 2
	publishPacket    byte = 3
	pubackPacket     byte = 4
	pubrecPacket     byte = 5
	pubrelPacket     byte = 6
	pubcompPacket    byte = 7
	pingreqPacket    byte = 12
	pingrespPacket   byte = 13
	disconnectPacket byte = 14
)

// protocolLevel 4 is MQTT 3.1.1.
const protocolLevel byte = 4

// The quality of service levels above 0.
const (
	qosAtLeastOnce byte = 1
	qosExactlyOnce byte = 2
)

// Field widths and masks of MQTT 3.1.1.
const (
	// maxStringLength is the longest UTF-8 field. Its length is two bytes.
	maxStringLength = 0xFFFF
	// maxFixedHeader is one type byte plus a remaining length of up to four.
	maxFixedHeader = 5
	// packetIDBytes is the size of a packet id.
	packetIDBytes = 2
	// packetTypeShift and packetFlagsMask split the first byte of a packet.
	packetTypeShift = 4
	packetFlagsMask = 0x0F
	// The remaining length has seven bits per byte plus a continuation bit.
	varintBase      = 128
	varintDigit     = 0x7F
	varintContinues = 0x80
	// The specification allows it at most four bytes.
	varintMaxBytes = 4
)

// maxPacketBody bounds one packet read from the broker.
const maxPacketBody = 1 << 20

// The bit positions of the CONNECT flags.
const (
	flagCleanSession byte = 1 << 1
	flagWill         byte = 1 << 2
	flagWillRetain   byte = 1 << 5
	flagPassword     byte = 1 << 6
	flagUsername     byte = 1 << 7
	willQoSShift          = 3
)

var (
	errPacketTooLarge  = errors.New("mqtt: broker sent a packet larger than this adapter accepts")
	errMalformedPacket = errors.New("mqtt: malformed packet from broker")
	errStringTooLong   = errors.New("mqtt: string field exceeds the 65535 byte protocol limit")
)

// connackReturnCodes are the refusals a broker can answer a CONNECT with.
var connackReturnCodes = map[byte]string{
	1: "unacceptable protocol version",
	2: "client identifier rejected",
	3: "server unavailable",
	4: "bad username or password",
	5: "not authorised",
}

// will is the message a broker publishes for a client that disappears
// without sending DISCONNECT.
type will struct {
	topic   string
	payload string
	qos     byte
	retain  bool
}

// credentials are optional. With an empty username neither field is sent.
type credentials struct {
	username string
	password string
}

// encodeConnect builds a CONNECT packet. Clean session is always set: this
// adapter never subscribes, so the broker has no session state to keep.
func encodeConnect(clientID string, keepaliveSeconds uint16, creds credentials, w will) ([]byte, error) {
	flags := flagCleanSession
	if w.topic != "" {
		flags |= flagWill | w.qos<<willQoSShift

		if w.retain {
			flags |= flagWillRetain
		}
	}

	if creds.username != "" {
		flags |= flagUsername | flagPassword
	}

	body := []byte{}

	var err error
	if body, err = appendString(body, "MQTT"); err != nil {
		return nil, err
	}

	body = append(body, protocolLevel, flags)
	body = binary.BigEndian.AppendUint16(body, keepaliveSeconds)

	fields := []string{clientID}
	if w.topic != "" {
		fields = append(fields, w.topic, w.payload)
	}

	if creds.username != "" {
		fields = append(fields, creds.username, creds.password)
	}

	for _, field := range fields {
		if body, err = appendString(body, field); err != nil {
			return nil, err
		}
	}

	return encodePacket(connectPacket, 0, body), nil
}

// encodePublish builds a PUBLISH packet. The packet id is written only above
// QoS 0.
func encodePublish(topic, payload string, qos byte, retain bool, packetID uint16) ([]byte, error) {
	flags := qos << 1
	if retain {
		flags |= 1
	}

	body, err := appendString(nil, topic)
	if err != nil {
		return nil, err
	}

	if qos > 0 {
		body = binary.BigEndian.AppendUint16(body, packetID)
	}

	body = append(body, payload...)

	return encodePacket(publishPacket, flags, body), nil
}

// encodePacketID builds an acknowledgement packet: PUBACK, PUBREC, PUBREL or
// PUBCOMP. PUBREL must have the reserved flags 0x02, or the broker closes the
// connection.
func encodePacketID(kind byte, packetID uint16) []byte {
	var flags byte
	if kind == pubrelPacket {
		flags = 0x02
	}

	return encodePacket(kind, flags, binary.BigEndian.AppendUint16(nil, packetID))
}

// encodeEmpty builds PINGREQ and DISCONNECT, which have no body.
func encodeEmpty(kind byte) []byte {
	return encodePacket(kind, 0, nil)
}

func encodePacket(kind, flags byte, body []byte) []byte {
	out := make([]byte, 0, len(body)+maxFixedHeader)
	out = append(out, kind<<4|flags)
	out = appendVarint(out, len(body))

	return append(out, body...)
}

func appendString(dst []byte, s string) ([]byte, error) {
	if len(s) > maxStringLength {
		return nil, fmt.Errorf("%w: %d bytes", errStringTooLong, len(s))
	}

	//nolint:gosec // G115: the length is bounded by maxStringLength on the line above.
	dst = binary.BigEndian.AppendUint16(dst, uint16(len(s)))

	return append(dst, s...), nil
}

// appendVarint writes the remaining length.
func appendVarint(dst []byte, n int) []byte {
	for {
		//nolint:gosec // G115: the modulo holds the value below varintBase, which is 128.
		digit := byte(n % varintBase)
		n /= varintBase

		if n > 0 {
			digit |= varintContinues
		}

		dst = append(dst, digit)

		if n == 0 {
			return dst
		}
	}
}

// readPacket reads one control packet and returns its type, its flags and its
// body.
func readPacket(r *bufio.Reader) (kind, flags byte, body []byte, err error) {
	header, err := r.ReadByte()
	if err != nil {
		return 0, 0, nil, err
	}

	length, err := readVarint(r)
	if err != nil {
		return 0, 0, nil, err
	}

	if length > maxPacketBody {
		return 0, 0, nil, fmt.Errorf("%w: %d bytes", errPacketTooLarge, length)
	}

	body = make([]byte, length)
	if _, err = io.ReadFull(r, body); err != nil {
		return 0, 0, nil, err
	}

	return header >> packetTypeShift, header & packetFlagsMask, body, nil
}

func readVarint(r *bufio.Reader) (int, error) {
	value, multiplier := 0, 1

	for range varintMaxBytes {
		digit, err := r.ReadByte()
		if err != nil {
			return 0, err
		}

		value += int(digit&varintDigit) * multiplier

		if digit&varintContinues == 0 {
			return value, nil
		}

		multiplier *= varintBase
	}

	return 0, fmt.Errorf("%w: remaining length is longer than four bytes", errMalformedPacket)
}

// connackError reads a CONNACK body. It returns nil when the broker accepted.
func connackError(body []byte) error {
	if len(body) < packetIDBytes {
		return fmt.Errorf("%w: CONNACK carried %d bytes, expected 2", errMalformedPacket, len(body))
	}

	code := body[1]
	if code == 0 {
		return nil
	}

	reason, known := connackReturnCodes[code]
	if !known {
		reason = "unknown return code"
	}

	return fmt.Errorf("mqtt: broker refused the connection: %s (code %d)", reason, code)
}

// packetIDFrom reads the packet id of an acknowledgement.
func packetIDFrom(body []byte) (uint16, error) {
	if len(body) < packetIDBytes {
		return 0, fmt.Errorf("%w: acknowledgement carried %d bytes, expected 2", errMalformedPacket, len(body))
	}

	return binary.BigEndian.Uint16(body), nil
}
