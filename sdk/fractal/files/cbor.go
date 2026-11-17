package files

import (
	"encoding/binary"
	"fmt"
	"math"
)

// The CBOR major types a listing uses (RFC 8949, section 3.1).
const (
	majorUint  byte = 0
	majorBytes byte = 2
	majorText  byte = 3
	majorArray byte = 4
)

// The additional information of a head (RFC 8949, section 3). A value below
// inlineMax is the argument, and followN says that N bytes follow with it.
const (
	inlineMax byte = 24
	follow1   byte = 24
	follow2   byte = 25
	follow4   byte = 26
	follow8   byte = 27
	majorBits      = 5
)

// appendHead appends the head of a data item of major type major and argument
// v, in the shortest form, as RFC 8949, section 4.2.1, requires.
func appendHead(b []byte, major byte, v uint64) []byte {
	m := major << majorBits
	switch {
	case v < uint64(inlineMax):
		return append(b, m|byte(v))
	case v <= math.MaxUint8:
		return append(b, m|follow1, byte(v))
	case v <= math.MaxUint16:
		return binary.BigEndian.AppendUint16(append(b, m|follow2), uint16(v))
	case v <= math.MaxUint32:
		return binary.BigEndian.AppendUint32(append(b, m|follow4), uint32(v))
	default:
		return binary.BigEndian.AppendUint64(append(b, m|follow8), v)
	}
}

// decoder reads the data items of a deterministic listing from b, and moves b
// past each item it reads.
type decoder struct {
	b []byte
}

// head reads the head of the next data item, which must have major type want
// and the shortest form of its argument.
func (d *decoder) head(want byte) (uint64, error) {
	if len(d.b) == 0 {
		return 0, fmt.Errorf("%w: the listing ends early", ErrMalformed)
	}
	major, info := d.b[0]>>majorBits, d.b[0]&(1<<majorBits-1)
	d.b = d.b[1:]
	if major != want {
		return 0, fmt.Errorf("%w: CBOR major type %d where %d belongs", ErrMalformed, major, want)
	}
	if info < inlineMax {
		return uint64(info), nil
	}
	var size int
	var least uint64
	switch info {
	case follow1:
		size, least = 1, uint64(inlineMax)
	case follow2:
		size, least = 2, math.MaxUint8+1
	case follow4:
		size, least = 4, math.MaxUint16+1
	case follow8:
		size, least = 8, math.MaxUint32+1
	default:
		return 0, fmt.Errorf("%w: CBOR additional information %d", ErrMalformed, info)
	}
	if len(d.b) < size {
		return 0, fmt.Errorf("%w: the listing ends early", ErrMalformed)
	}
	var buf [8]byte
	copy(buf[len(buf)-size:], d.b)
	v := binary.BigEndian.Uint64(buf[:])
	d.b = d.b[size:]
	if v < least {
		return 0, fmt.Errorf("%w: CBOR argument %d is not in its shortest form", ErrMalformed, v)
	}
	return v, nil
}

// bytes reads a byte string or a text string, by want, and returns its
// content.
func (d *decoder) bytes(want byte) ([]byte, error) {
	n, err := d.head(want)
	if err != nil {
		return nil, err
	}
	if n > uint64(len(d.b)) {
		return nil, fmt.Errorf("%w: the listing ends early", ErrMalformed)
	}
	content := d.b[:n]
	d.b = d.b[n:]
	return content, nil
}
