package amqp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"time"
)

// The AMQP 0-9-1 frame types.
const (
	frameMethod    = 1
	frameHeader    = 2
	frameBody      = 3
	frameHeartbeat = 8
	frameEnd       = 0xCE
)

// The AMQP 0-9-1 class and method identifiers this adapter uses.
const (
	classConnection = 10
	classChannel    = 20
	classExchange   = 40
	classBasic      = 60
	classConfirm    = 85

	connectionStart   = 10
	connectionStartOk = 11
	connectionSecure  = 20
	connectionTune    = 30
	connectionTuneOk  = 31
	connectionOpen    = 40
	connectionOpenOk  = 41
	connectionClose   = 50
	connectionCloseOk = 51

	channelOpen    = 10
	channelOpenOk  = 11
	channelClose   = 40
	channelCloseOk = 41

	exchangeDeclare   = 10
	exchangeDeclareOk = 11

	basicPublish = 40
	basicReturn  = 50
	basicAck     = 80
	basicNack    = 120

	confirmSelect   = 10
	confirmSelectOk = 11
)

// protocolHeader opens the connection: "AMQP", the protocol id and the
// version.
var protocolHeader = []byte{'A', 'M', 'Q', 'P', 0, 0, 9, 1}

// The field widths, in bytes.
const (
	octetWidth     = 1
	shortWidth     = 2
	longWidth      = 4
	longlongWidth  = 8
	methodWidth    = 2 * shortWidth // the class and the method identifiers
	frameHeadWidth = octetWidth + shortWidth + longWidth
)

// maxFrameBytes bounds a frame read from the broker. The tuning step
// negotiates the frame size down to it.
const maxFrameBytes = 128 * 1024

// ErrProtocol reports a broker answer this adapter cannot read. It usually
// means the far end is not an AMQP 0-9-1 broker.
var ErrProtocol = errors.New("amqp: the broker answered outside the protocol")

// frame is one unit on the wire: a type, a channel and a payload, without the
// trailing frame-end octet.
type frame struct {
	kind    byte
	channel uint16
	payload []byte
}

// method is a decoded method frame: its class, its method and the arguments
// left to read.
type method struct {
	channel uint16
	class   uint16
	id      uint16
	args    *reader
}

func (m method) is(class, id uint16) bool { return m.class == class && m.id == id }

func (m method) String() string { return fmt.Sprintf("class %d method %d", m.class, m.id) }

// readFrame reads one whole frame.
func readFrame(r io.Reader) (frame, error) {
	var head [frameHeadWidth]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return frame{}, err
	}

	size := binary.BigEndian.Uint32(head[octetWidth+shortWidth:])
	if size > maxFrameBytes {
		return frame{}, fmt.Errorf("%w: a %d byte frame is past the negotiated maximum", ErrProtocol, size)
	}

	payload := make([]byte, size+1) // the frame-end octet rides along
	if _, err := io.ReadFull(r, payload); err != nil {
		return frame{}, err
	}

	if payload[size] != frameEnd {
		return frame{}, fmt.Errorf("%w: the frame does not end with 0x%X", ErrProtocol, frameEnd)
	}

	return frame{
		kind:    head[0],
		channel: binary.BigEndian.Uint16(head[octetWidth:]),
		payload: payload[:size],
	}, nil
}

// writeFrame writes one frame with its header and its terminator.
func writeFrame(w io.Writer, f frame) error {
	head := make([]byte, 0, frameHeadWidth+len(f.payload)+octetWidth)
	head = append(head, f.kind)
	head = binary.BigEndian.AppendUint16(head, f.channel)
	//nolint:gosec // G115: the payload is at most maxFrameBytes, which readFrame and the tuning both hold it to.
	head = binary.BigEndian.AppendUint32(head, uint32(len(f.payload)))
	head = append(head, f.payload...)
	head = append(head, frameEnd)

	if _, err := w.Write(head); err != nil {
		return err
	}

	return nil
}

// asMethod decodes a method frame's class and method identifiers.
func (f frame) asMethod() (method, error) {
	if f.kind != frameMethod {
		return method{}, fmt.Errorf("%w: expected a method frame, got type %d", ErrProtocol, f.kind)
	}

	if len(f.payload) < methodWidth {
		return method{}, fmt.Errorf("%w: a method frame carries at least four bytes", ErrProtocol)
	}

	return method{
		channel: f.channel,
		class:   binary.BigEndian.Uint16(f.payload),
		id:      binary.BigEndian.Uint16(f.payload[shortWidth:]),
		args:    &reader{buf: f.payload[methodWidth:]},
	}, nil
}

// writer builds a method payload. It encodes only the argument types this
// adapter sends.
type writer struct {
	buf  []byte
	bits byte // packed booleans waiting for the octet they share
	held int
}

func newMethod(class, id uint16) *writer {
	w := &writer{}
	w.buf = binary.BigEndian.AppendUint16(w.buf, class)
	w.buf = binary.BigEndian.AppendUint16(w.buf, id)

	return w
}

func (w *writer) octet(v byte) *writer {
	w.flush()
	w.buf = append(w.buf, v)

	return w
}

func (w *writer) short(v uint16) *writer {
	w.flush()
	w.buf = binary.BigEndian.AppendUint16(w.buf, v)

	return w
}

func (w *writer) long(v uint32) *writer {
	w.flush()
	w.buf = binary.BigEndian.AppendUint32(w.buf, v)

	return w
}

func (w *writer) longlong(v uint64) *writer {
	w.flush()
	w.buf = binary.BigEndian.AppendUint64(w.buf, v)

	return w
}

// shortstr writes a string of at most 255 bytes, prefixed with its length in
// one octet. A longer string is truncated.
func (w *writer) shortstr(v string) *writer {
	w.flush()

	if len(v) > math.MaxUint8 {
		v = v[:math.MaxUint8]
	}

	//nolint:gosec // G115: the two lines above clamp len(v) to math.MaxUint8.
	w.buf = append(w.buf, byte(len(v)))
	w.buf = append(w.buf, v...)

	return w
}

func (w *writer) longstr(v []byte) *writer {
	w.flush()
	//nolint:gosec // G115: every longstr here is a credential or a name from configuration, far below four gigabytes.
	w.buf = binary.BigEndian.AppendUint32(w.buf, uint32(len(v)))
	w.buf = append(w.buf, v...)

	return w
}

const bitsPerOctet = 8

// bit packs a boolean. Consecutive booleans share one octet, lowest bit first.
func (w *writer) bit(v bool) *writer {
	if w.held == bitsPerOctet {
		w.flush()
	}

	if v {
		w.bits |= 1 << w.held
	}

	w.held++

	return w
}

// table writes an empty field table. RabbitMQ accepts a client that sends no
// peer properties.
func (w *writer) table() *writer {
	return w.long(0)
}

func (w *writer) flush() {
	if w.held == 0 {
		return
	}

	w.buf = append(w.buf, w.bits)
	w.bits, w.held = 0, 0
}

func (w *writer) bytes() []byte {
	w.flush()

	return w.buf
}

// reader decodes method arguments. A short read sets err, and every later
// read returns zero.
type reader struct {
	buf []byte
	err error
}

func (r *reader) octet() byte {
	if r.err != nil || len(r.buf) < 1 {
		r.fail(1)

		return 0
	}

	v := r.buf[0]
	r.buf = r.buf[1:]

	return v
}

func (r *reader) short() uint16 {
	if r.err != nil || len(r.buf) < shortWidth {
		r.fail(shortWidth)

		return 0
	}

	v := binary.BigEndian.Uint16(r.buf)
	r.buf = r.buf[shortWidth:]

	return v
}

func (r *reader) long() uint32 {
	if r.err != nil || len(r.buf) < longWidth {
		r.fail(longWidth)

		return 0
	}

	v := binary.BigEndian.Uint32(r.buf)
	r.buf = r.buf[longWidth:]

	return v
}

func (r *reader) longlong() uint64 {
	if r.err != nil || len(r.buf) < longlongWidth {
		r.fail(longlongWidth)

		return 0
	}

	v := binary.BigEndian.Uint64(r.buf)
	r.buf = r.buf[longlongWidth:]

	return v
}

func (r *reader) shortstr() string {
	size := int(r.octet())
	if r.err != nil || len(r.buf) < size {
		r.fail(size)

		return ""
	}

	v := string(r.buf[:size])
	r.buf = r.buf[size:]

	return v
}

// longstr reads a string prefixed with its length in four octets.
func (r *reader) longstr() string {
	size := int(r.long())
	if r.err != nil || len(r.buf) < size {
		r.fail(size)

		return ""
	}

	v := string(r.buf[:size])
	r.buf = r.buf[size:]

	return v
}

func (r *reader) fail(want int) {
	if r.err == nil {
		r.err = fmt.Errorf("%w: %d more bytes were needed and %d were left", ErrProtocol, want, len(r.buf))
	}

	r.buf = nil
}

// contentHeader builds the frame that follows Basic.Publish: the body size
// and the message properties. The property flags are a bitmap from the top
// bit: content-type is bit 15, delivery-mode bit 12 and timestamp bit 6.
func contentHeader(size int, contentType string, persistent bool, when time.Time) []byte {
	const (
		contentTypeFlag  = 0x8000
		deliveryModeFlag = 0x1000
		timestampFlag    = 0x0040

		deliveryTransient  = 1
		deliveryPersistent = 2
	)

	flags := uint16(contentTypeFlag | deliveryModeFlag | timestampFlag)

	mode := byte(deliveryTransient)
	if persistent {
		mode = deliveryPersistent
	}

	w := &writer{}
	w.buf = binary.BigEndian.AppendUint16(w.buf, classBasic)
	w.buf = binary.BigEndian.AppendUint16(w.buf, 0) // weight, always zero
	//nolint:gosec // G115: the body is one JSON event, and both conversions are of values that cannot be negative.
	w.buf = binary.BigEndian.AppendUint64(w.buf, uint64(size))
	w.buf = binary.BigEndian.AppendUint16(w.buf, flags)

	//nolint:gosec // G115: Unix seconds are positive for every time this runs at.
	return w.shortstr(contentType).octet(mode).longlong(uint64(when.Unix())).bytes()
}
