package printfile

import (
	"bytes"
	"encoding/binary"
	"image/color"
	"testing"
)

// TestDecodeQOIChunks drives each chunk the format defines, because a slicer
// picks whichever is shortest for the pixel it is writing and a decoder that
// only handles the plain one produces a picture nobody would recognise.
func TestDecodeQOIChunks(t *testing.T) {
	t.Parallel()

	// Eight pixels in a row. The comment on each chunk says what it makes.
	chunks := []byte{
		qoiOpRGB, 0x20, 0x40, 0x60, // 32,64,96,255 in full
		qoiOpRGBA, 0x10, 0x20, 0x30, 0x80, // 16,32,48,128 in full
		qoiOpIndex | qoiHash(color.NRGBA{R: 0x20, G: 0x40, B: 0x60, A: 0xFF}), // back to the first
		qoiOpDiff | 0x3F,     // +1,+1,+1 on each channel
		qoiOpLuma | 34, 0x88, // green +2, red +2, blue +2
		qoiOpRun | 1, // the same pixel twice more
		qoiOpRGB, 0x01, 0x02, 0x03,
	}

	raw := qoiFile(8, 1, chunks)

	img, ok := decodeQOI(raw)
	if !ok {
		t.Fatal("the picture did not decode")
	}

	want := []color.NRGBA{
		{R: 0x20, G: 0x40, B: 0x60, A: 0xFF},
		{R: 0x10, G: 0x20, B: 0x30, A: 0x80},
		{R: 0x20, G: 0x40, B: 0x60, A: 0xFF},
		{R: 0x21, G: 0x41, B: 0x61, A: 0xFF},
		{R: 0x23, G: 0x43, B: 0x63, A: 0xFF},
		{R: 0x23, G: 0x43, B: 0x63, A: 0xFF},
		{R: 0x23, G: 0x43, B: 0x63, A: 0xFF},
		{R: 0x01, G: 0x02, B: 0x03, A: 0xFF},
	}

	for x, expected := range want {
		if got := img.NRGBAAt(x, 0); got != expected {
			t.Errorf("pixel %d: got %+v, want %+v", x, got, expected)
		}
	}
}

// TestDecodeQOIRefuses covers the headers this package will not read.
func TestDecodeQOIRefuses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  []byte
	}{
		{name: "empty", raw: nil},
		{name: "not a qoi file", raw: []byte("PNG and eight more bytes here")},
		{name: "no pixels", raw: qoiFile(0, 0, nil)},
		{name: "more pixels than the header declares", raw: qoiFile(1, 1, []byte{qoiOpRun | 8})},
		{name: "truncated chunks", raw: qoiFile(4, 4, []byte{qoiOpRGB, 0x01})},
		{name: "a picture too large to hold", raw: qoiFile(1<<16, 1<<16, []byte{qoiOpRun})},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, ok := decodeQOI(tc.raw); ok {
				t.Error("the picture decoded and should not have")
			}
		})
	}
}

// TestQOIBecomesPNG proves the picture this package serves is a PNG of the same
// size, which is what a browser needs: no browser reads QOI.
func TestQOIBecomesPNG(t *testing.T) {
	t.Parallel()

	out, ok := pngFromQOI(fixtureQOI(12, 9))
	if !ok {
		t.Fatal("the picture did not convert")
	}

	decodePicture(t, out, 12, 9)
}

// qoiFile wraps chunks in a QOI header and the end marker.
func qoiFile(width, height uint32, chunks []byte) []byte {
	var out bytes.Buffer

	out.WriteString("qoif")
	_ = binary.Write(&out, binary.BigEndian, width)
	_ = binary.Write(&out, binary.BigEndian, height)
	out.WriteByte(4)
	out.WriteByte(0)
	out.Write(chunks)
	out.Write([]byte{0, 0, 0, 0, 0, 0, 0, 1})

	return out.Bytes()
}
