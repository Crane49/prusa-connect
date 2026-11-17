package webrtc

import (
	"errors"
	"fmt"
	"io"
)

// The refusals the reader reports. A caller inspects them with errors.Is.
var (
	// ErrNotAnnexB reports bytes that do not begin with a start code, which is
	// a driver that declared h264/annexb and framed something else.
	ErrNotAnnexB = errors.New("webrtc: the stream does not start with an H.264 start code")
	// ErrUnitTooLarge reports one NAL unit over the ceiling. A camera that
	// never sends another start code is otherwise the host's whole memory.
	ErrUnitTooLarge = errors.New("webrtc: an H.264 NAL unit is over the ceiling")
)

// unitCeiling bounds one NAL unit. A 1080p keyframe from the cameras this
// product meets is under 200 KiB, so 4 MiB is two orders of margin over the
// largest legitimate unit and still small enough that a hostile device cannot
// spend the host's memory.
const unitCeiling = 4 << 20

// The two widths of an Annex-B start code, and the mask that reads a unit's
// type out of its header byte. H.264 annex B allows either width anywhere.
const (
	shortStartCode = 3
	longStartCode  = 4
	unitTypeMask   = 0x1F
)

// readChunk is how much is taken from the device per read. One buffer for the
// life of the stream, because a fresh slice per read is an allocation per frame
// per viewer.
const readChunk = 32 << 10

// The NAL unit types this package acts on. The full set is H.264 table 7-1;
// these are the ones that decide an access unit boundary or carry parameters.
const (
	unitNonIDR uint8 = 1
	unitIDR    uint8 = 5
	unitSEI    uint8 = 6
	unitSPS    uint8 = 7
	unitPPS    uint8 = 8
	unitAUD    uint8 = 9
)

// units splits an Annex-B elementary stream into NAL units.
//
// It is written here rather than taken from pion's h264reader for one reason:
// that reader grows its buffer until it finds the next start code, and a device
// that sends one and then never sends another is a camera on somebody's network
// spending this host's memory. unitCeiling is the bound.
type units struct {
	src    io.Reader
	buf    []byte
	chunk  []byte
	limit  int
	start  int
	scan   int
	opened bool
	eof    bool
}

func newUnits(src io.Reader) *units { return newUnitsWithLimit(src, unitCeiling) }

// newUnitsWithLimit is newUnits with the ceiling stated, so a test can reach
// the ceiling with a small stream.
func newUnitsWithLimit(src io.Reader, limit int) *units {
	return &units{src: src, chunk: make([]byte, readChunk), limit: limit}
}

// next returns the next NAL unit, without its start code and without the
// trailing zeroes that belong to the next one. It reports io.EOF when the
// stream ends cleanly.
func (u *units) next() ([]byte, error) {
	if !u.opened {
		if err := u.open(); err != nil {
			return nil, err
		}

		u.opened = true
	}

	for {
		if unit, found := u.take(); found {
			return unit, nil
		}

		if u.eof {
			return u.last()
		}

		if err := u.fill(); err != nil {
			return nil, err
		}
	}
}

// open consumes the leading start code, refusing a stream that has none. A
// driver that declared h264/annexb and framed MJPEG fails here, before a byte
// of it reaches a browser.
func (u *units) open() error {
	for {
		if at, width, found := startCode(u.buf, 0); found {
			if at != 0 {
				return fmt.Errorf("%w: %d bytes precede it", ErrNotAnnexB, at)
			}

			u.start = width
			u.scan = width

			return nil
		}

		if u.eof {
			return ErrNotAnnexB
		}

		// Four bytes is the longest start code, so anything longer than that
		// without one is not an Annex-B stream and there is no point reading on.
		if len(u.buf) > longStartCode {
			return ErrNotAnnexB
		}

		if err := u.fill(); err != nil {
			return err
		}
	}
}

// take returns the unit ending at the next start code, when the buffer holds
// one.
func (u *units) take() ([]byte, bool) {
	at, width, found := startCode(u.buf, u.scan)
	if !found {
		// Rescan from three bytes back, because a start code can straddle the
		// boundary between two reads.
		if u.scan = len(u.buf) - shortStartCode; u.scan < u.start {
			u.scan = u.start
		}

		return nil, false
	}

	unit := trimTrailingZeroes(u.buf[u.start:at])

	u.start = at + width
	u.scan = u.start

	return unit, true
}

// last returns whatever is left when the device closed the stream.
func (u *units) last() ([]byte, error) {
	if u.start >= len(u.buf) {
		return nil, io.EOF
	}

	unit := trimTrailingZeroes(u.buf[u.start:])
	u.start = len(u.buf)

	if len(unit) == 0 {
		return nil, io.EOF
	}

	return unit, nil
}

// fill reads more of the stream, compacting what has already been handed out
// and refusing a unit that has grown past the ceiling.
func (u *units) fill() error {
	if u.start > 0 {
		u.buf = append(u.buf[:0], u.buf[u.start:]...)
		u.scan -= u.start
		u.start = 0

		if u.scan < 0 {
			u.scan = 0
		}
	}

	if len(u.buf) > u.limit {
		return fmt.Errorf("%w: it passed %d bytes", ErrUnitTooLarge, u.limit)
	}

	n, err := u.src.Read(u.chunk)
	if n > 0 {
		u.buf = append(u.buf, u.chunk[:n]...)
	}

	switch {
	case err == nil:
		return nil
	case errors.Is(err, io.EOF):
		u.eof = true

		return nil
	default:
		return fmt.Errorf("read the camera stream: %w", err)
	}
}

// startCode finds the next three-byte start code at or after from, and reports
// how wide it is with the zero that may precede it.
func startCode(buf []byte, from int) (at int, width int, found bool) {
	for i := from; i+2 < len(buf); i++ {
		if buf[i] != 0 || buf[i+1] != 0 || buf[i+2] != 1 {
			continue
		}

		if i > 0 && buf[i-1] == 0 {
			return i - 1, longStartCode, true
		}

		return i, shortStartCode, true
	}

	return 0, 0, false
}

// trimTrailingZeroes drops the trailing_zero_8bits a device may pad a unit
// with. They are not part of the unit and a payloader that kept them would put
// them on the wire.
func trimTrailingZeroes(unit []byte) []byte {
	end := len(unit)
	for end > 0 && unit[end-1] == 0 {
		end--
	}

	return unit[:end]
}

// unitType is the five-bit nal_unit_type of one unit, and zero for an empty
// one, which is the unspecified type nothing acts on.
func unitType(unit []byte) uint8 {
	if len(unit) == 0 {
		return 0
	}

	return unit[0] & unitTypeMask
}

// isPicture reports whether this unit is part of a coded picture. Types 1 to 5
// are the VCL units, and one access unit holds the VCL units of exactly one
// picture.
func isPicture(kind uint8) bool { return kind >= unitNonIDR && kind <= unitIDR }
