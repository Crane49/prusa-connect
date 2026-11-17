package printfile

import (
	"encoding/binary"
	"io"
)

// A binary G-code file keeps its G-code in blocks after the metadata, and the
// firmware reads the first lines of the first one for its checks. Those blocks
// are compressed with heatshrink and their text is packed with MeatPack, and
// the standard library reads neither, so both are decoded here. Only the first
// block is read, and only as far as maxGCodeHeadBytes of text.

// maxGCodeHeadBytes bounds the text decoded out of the first G-code block. The
// first two hundred G-code lines of a PrusaSlicer file are a few kilobytes.
const maxGCodeHeadBytes = 64 << 10

// maxGCodeHeadInput bounds the compressed bytes read for it. Heatshrink never
// grows its input by more than one bit in eight, so this is enough input for
// the text bound.
const maxGCodeHeadInput = maxGCodeHeadBytes + maxGCodeHeadBytes/8 + 16

// The encodings of a G-code block (libbgcode's EGCodeEncodingType).
const (
	encodingNone             = 0
	encodingMeatPack         = 1
	encodingMeatPackComments = 2
)

// The heatshrink window and lookahead sizes, as powers of two, for the two
// heatshrink compressions the specification names.
const (
	heatshrinkLookahead = 4
	heatshrinkWindow11  = 11
	heatshrinkWindow12  = 12
)

// readGCodeHead decodes the start of one G-code block and feeds its lines to
// the scanner. A block it cannot decode leaves the scanner untouched.
func readGCodeHead(r io.ReaderAt, b block, scanner *headScanner) {
	if len(b.params) < metadataParamBytes || b.dataBytes <= 0 {
		return
	}

	encoding := binary.LittleEndian.Uint16(b.params[0:2])

	raw := readSection(r, b.dataAt, min(b.dataBytes, maxGCodeHeadInput))
	if len(raw) == 0 {
		return
	}

	var plain []byte

	switch b.compressed {
	case compressionNone:
		plain = raw[:min(len(raw), maxGCodeHeadBytes)]
	case compressionDeflate:
		inflated, ok := inflate(raw, maxGCodeHeadBytes)
		if !ok {
			return
		}

		plain = inflated
	case compressionHeatshrink1:
		plain = unheatshrink(raw, heatshrinkWindow11, maxGCodeHeadBytes)
	case compressionHeatshrink2:
		plain = unheatshrink(raw, heatshrinkWindow12, maxGCodeHeadBytes)
	default:
		return
	}

	switch encoding {
	case encodingNone:
	case encodingMeatPack, encodingMeatPackComments:
		plain = unmeatpack(plain, maxGCodeHeadBytes)
	default:
		return
	}

	scanGCodeText(string(plain), scanner)
}

// bitsPerByte is the bits of one byte, the most significant read first.
const bitsPerByte = 8

// expansion is a guess at how much larger a decoded block is than its input,
// used only to size the first buffer.
const expansion = 2

// bitReader reads a byte slice one bit at a time, the most significant bit of
// each byte first, which is the order heatshrink writes in.
type bitReader struct {
	data []byte
	at   int
	bit  uint
}

// read returns the next count bits as a number, and false once the input runs
// out.
func (b *bitReader) read(count uint) (int, bool) {
	value := 0

	for range count {
		if b.at >= len(b.data) {
			return 0, false
		}

		value = value<<1 | int(b.data[b.at]>>(bitsPerByte-1-b.bit)&1)

		b.bit++
		if b.bit == bitsPerByte {
			b.bit = 0
			b.at++
		}
	}

	return value, true
}

// readByte returns the next eight bits as a byte.
func (b *bitReader) readByte() (byte, bool) {
	var out byte

	for range bitsPerByte {
		bit, ok := b.read(1)
		if !ok {
			return 0, false
		}

		out = out<<1 | byte(bit&1)
	}

	return out, true
}

// unheatshrink decodes a heatshrink stream, stopping at limit bytes of output.
//
// Heatshrink is LZSS. Each item starts with a tag bit: one means a literal
// byte follows in eight bits, and zero means a reference back into the output,
// written as an offset of window bits and a length of heatshrinkLookahead
// bits, each stored as one less than its value.
//
// The window starts filled with zero bytes, and the encoder may refer to them
// before it has written anything: MeatPack packs "00" into a zero byte, so
// PrusaSlicer's files do. A reference before the start of the output reads
// zeros, as the firmware's decoder does (heatshrink_decoder_reset clears the
// buffer in lib/heatshrink/heatshrink_decoder.c).
func unheatshrink(data []byte, window uint, limit int) []byte {
	in := bitReader{data: data}
	out := make([]byte, 0, min(limit, len(data)*expansion))

	for len(out) < limit {
		tag, more := in.read(1)
		if !more {
			break
		}

		if tag == 1 {
			literal, read := in.readByte()
			if !read {
				break
			}

			out = append(out, literal)

			continue
		}

		offset, read := in.read(window)
		if !read {
			break
		}

		length, read := in.read(heatshrinkLookahead)
		if !read {
			break
		}

		from := len(out) - (offset + 1)

		for i := range length + 1 {
			if len(out) >= limit {
				break
			}

			if from+i < 0 {
				out = append(out, 0)

				continue
			}

			out = append(out, out[from+i])
		}
	}

	return out
}

// The MeatPack command bytes. Two 0xFF bytes in a row announce a command, and
// the byte after them is the command itself.
const (
	meatpackSignal          = 0xFF
	meatpackEnablePacking   = 251
	meatpackDisablePacking  = 250
	meatpackResetAll        = 249
	meatpackEnableNoSpaces  = 247
	meatpackDisableNoSpaces = 246

	// meatpackLiteral is the nibble that says the character is not packed and
	// follows as a whole byte.
	meatpackLiteral = 0x0F
	// nibbleBits is the width of one packed character.
	nibbleBits = 4
	// meatpackSpaceIndex is the table entry that holds a space, or an E once
	// spaces are left out.
	meatpackSpaceIndex = 11
)

// meatpackTable is the fifteen characters MeatPack packs into four bits, in
// nibble order. The sixteenth nibble marks a literal.
const meatpackTable = "0123456789. \nGX"

// unmeatpack expands MeatPack text, stopping at limit bytes of output.
//
// MeatPack puts two common characters into one byte, the first in the low
// nibble. A nibble of 0xF means that character is not in the table and comes
// next as a whole byte. A newline in the low nibble ends the byte. The
// firmware decodes it the same way (src/common/gcode/meatpack.cpp).
func unmeatpack(data []byte, limit int) []byte {
	decoder := meatpack{
		table: []byte(meatpackTable),
		limit: limit,
		out:   make([]byte, 0, min(limit, len(data)*expansion)),
	}

	for _, c := range data {
		if len(decoder.out) >= limit {
			break
		}

		decoder.feed(c)
	}

	return decoder.out
}

// meatpack is the state of one MeatPack decode.
type meatpack struct {
	table     []byte
	limit     int
	out       []byte
	active    bool
	signals   int
	command   bool
	literals  int
	second    byte
	hasSecond bool
}

// feed reads one byte of the stream: a signal, a command or packed text.
func (m *meatpack) feed(c byte) {
	switch {
	case c == meatpackSignal:
		if m.signals > 0 {
			m.command, m.signals = true, 0
		} else {
			m.signals++
		}
	case m.command:
		m.command = false
		m.apply(c)
	default:
		if m.signals > 0 {
			// A single 0xFF is a byte of its own, both nibbles literal.
			m.signals = 0
			m.unpack(meatpackSignal)
		}

		m.unpack(c)
	}
}

// apply carries out one command.
func (m *meatpack) apply(c byte) {
	switch c {
	case meatpackEnablePacking:
		m.active = true
	case meatpackDisablePacking:
		m.active = false
	case meatpackResetAll:
		m.active, m.literals, m.hasSecond = false, 0, false
		m.table[meatpackSpaceIndex] = ' '
	case meatpackEnableNoSpaces:
		m.table[meatpackSpaceIndex] = 'E'
	case meatpackDisableNoSpaces:
		m.table[meatpackSpaceIndex] = ' '
	}
}

func (m *meatpack) emit(c byte) {
	if len(m.out) < m.limit {
		m.out = append(m.out, c)
	}
}

// unpack reads one byte of text.
func (m *meatpack) unpack(c byte) {
	switch {
	case !m.active:
		m.emit(c)
	case m.literals > 0:
		m.emit(c)

		if m.hasSecond {
			m.emit(m.second)
			m.hasSecond = false
		}

		m.literals--
	default:
		m.unpackPair(c&meatpackLiteral, c>>nibbleBits&meatpackLiteral)
	}
}

// unpackPair reads the two nibbles of one packed byte.
func (m *meatpack) unpackPair(low, high byte) {
	if low == meatpackLiteral {
		m.literals++

		if high == meatpackLiteral {
			m.literals++
		} else {
			m.second, m.hasSecond = m.table[high], true
		}

		return
	}

	m.emit(m.table[low])

	switch {
	case m.table[low] == '\n':
	case high == meatpackLiteral:
		m.literals++
	default:
		m.emit(m.table[high])
	}
}
