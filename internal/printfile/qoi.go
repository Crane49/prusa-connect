package printfile

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
)

// The QOI image format, as qoiformat.org specifies it. PrusaSlicer writes it
// for printers whose firmware has little memory to decode a picture in. No
// browser reads it, so this package decodes it and re-encodes the picture as
// PNG.
//
// A QOI file is a fourteen byte header and then a run of chunks, each of which
// says how the next pixel differs from the one before it.
const (
	qoiHeaderBytes = 14
	// qoiPaddingBytes is the end marker: seven zero bytes and a one.
	qoiPaddingBytes = 8
	// qoiIndexSize is the size of the table of recently seen pixels a chunk can
	// point back into.
	qoiIndexSize = 64
	// qoiMaxRun is the longest run one chunk can stand for, which is also the
	// most pixels one byte of payload can produce. It tells whether a payload
	// can cover the size the header declares.
	qoiMaxRun = 62
)

// The chunk tags. The four eight-bit tags are checked first; what is left is
// read by its top two bits.
const (
	qoiOpRGB  = 0xFE
	qoiOpRGBA = 0xFF

	qoiMask2 = 0xC0

	qoiOpIndex = 0x00
	qoiOpDiff  = 0x40
	qoiOpLuma  = 0x80
	qoiOpRun   = 0xC0
)

// The fields packed inside a chunk, and the bias each one is stored with.
const (
	// qoiOpaque is the alpha a picture starts at, and the alpha every chunk
	// that carries no alpha keeps.
	qoiOpaque = 0xFF
	// qoiSixBits is the payload of a one byte chunk.
	qoiSixBits = 0x3F
	// qoiTwoBits is one channel of a difference chunk.
	qoiTwoBits = 0x03
	// qoiFourBits is one channel of a luma chunk.
	qoiFourBits = 0x0F
	// qoiDiffBias is what a difference chunk's two bit channel is offset by.
	qoiDiffBias = 2
	// qoiLumaBias is what a luma chunk's four bit channels are offset by.
	qoiLumaBias = 8
	// qoiGreenBias is what a luma chunk's six bit green is offset by.
	qoiGreenBias = 32
	// qoiChannels is how many bytes one pixel takes in the decoded picture.
	qoiChannels = 4
	// qoiRedShift and qoiGreenShift are where a difference chunk keeps its red
	// and its green. A luma chunk keeps its red at the same shift as red here.
	qoiRedShift   = 4
	qoiGreenShift = 2
)

//nolint:gochecknoglobals // a constant byte string, never written to.
var qoiMagic = []byte("qoif")

// pngFromQOI decodes a QOI picture and re-encodes it as PNG. It reports false
// for anything it cannot read, and for a picture larger than this package
// serves.
func pngFromQOI(raw []byte) ([]byte, bool) {
	img, ok := decodeQOI(raw)
	if !ok {
		return nil, false
	}

	var out bytes.Buffer

	encoder := png.Encoder{CompressionLevel: png.DefaultCompression}
	if err := encoder.Encode(&out, img); err != nil {
		return nil, false
	}

	if out.Len() > MaxThumbnailBytes {
		return nil, false
	}

	return out.Bytes(), true
}

// decodeQOI turns a QOI file into an image.
func decodeQOI(raw []byte) (*image.NRGBA, bool) {
	if len(raw) < qoiHeaderBytes+qoiPaddingBytes || !hasPrefix(raw, qoiMagic) {
		return nil, false
	}

	width := int64(binary.BigEndian.Uint32(raw[4:8]))
	height := int64(binary.BigEndian.Uint32(raw[8:12]))

	if !sizeAllowed(width, height) {
		return nil, false
	}

	chunks := raw[qoiHeaderBytes : len(raw)-qoiPaddingBytes]

	// The header says how big the picture is and the chunks have to fill it.
	// One byte of payload stands for at most qoiMaxRun pixels, so a
	// payload shorter than that is a header that cannot be true, and the
	// picture buffer is never allocated for it. Twenty-two bytes declaring
	// 2048x2048 would otherwise cost sixteen megabytes each time.
	if width*height > qoiMaxRun*int64(len(chunks)) {
		return nil, false
	}

	img := image.NewNRGBA(image.Rect(0, 0, int(width), int(height)))

	if !decodeQOIPixels(chunks, img.Pix, int(width*height)) {
		return nil, false
	}

	return img, true
}

// decodeQOIPixels writes every pixel of the picture into dst, which holds four
// bytes per pixel.
//
// It returns false when the chunks run out before the picture is full, which is
// a truncated file, and when a run would write past the end, which is a file
// claiming more pixels than it declared.
func decodeQOIPixels(chunks []byte, dst []byte, pixels int) bool {
	prev := color.NRGBA{A: qoiOpaque}

	var seen [qoiIndexSize]color.NRGBA

	at := 0
	read := 0

	for at < pixels {
		if read >= len(chunks) {
			return false
		}

		tag := chunks[read]
		read++

		run := 1

		switch {
		case tag == qoiOpRGB:
			if read+3 > len(chunks) {
				return false
			}

			prev.R, prev.G, prev.B = chunks[read], chunks[read+1], chunks[read+2]
			read += 3
		case tag == qoiOpRGBA:
			if read+4 > len(chunks) {
				return false
			}

			prev.R, prev.G, prev.B, prev.A =
				chunks[read], chunks[read+1], chunks[read+2], chunks[read+3]
			read += 4
		case tag&qoiMask2 == qoiOpIndex:
			prev = seen[tag&qoiSixBits]
		case tag&qoiMask2 == qoiOpDiff:
			prev.R += (tag>>qoiRedShift)&qoiTwoBits - qoiDiffBias
			prev.G += (tag>>qoiGreenShift)&qoiTwoBits - qoiDiffBias
			prev.B += tag&qoiTwoBits - qoiDiffBias
		case tag&qoiMask2 == qoiOpLuma:
			if read >= len(chunks) {
				return false
			}

			next := chunks[read]
			read++

			green := tag&qoiSixBits - qoiGreenBias
			prev.R += green + (next>>qoiRedShift)&qoiFourBits - qoiLumaBias
			prev.G += green
			prev.B += green + next&qoiFourBits - qoiLumaBias
		case tag&qoiMask2 == qoiOpRun:
			run = int(tag&qoiSixBits) + 1
		}

		if at+run > pixels {
			return false
		}

		seen[qoiHash(prev)] = prev

		for range run {
			base := at * qoiChannels
			dst[base], dst[base+1], dst[base+2], dst[base+3] = prev.R, prev.G, prev.B, prev.A
			at++
		}
	}

	return true
}

// qoiHash is the index a pixel is remembered under, which the specification
// fixes as (r*3 + g*5 + b*7 + a*11) modulo 64.
func qoiHash(p color.NRGBA) byte {
	return (p.R*3 + p.G*5 + p.B*7 + p.A*11) % qoiIndexSize
}
