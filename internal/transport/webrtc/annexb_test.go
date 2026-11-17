package webrtc

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The NAL unit headers a synthetic stream is built from. The low five bits are
// the type and the two above them are nal_ref_idc, which is what a real encoder
// writes for each of these.
const (
	headerSPS    = byte(0x67)
	headerPPS    = byte(0x68)
	headerIDR    = byte(0x65)
	headerNonIDR = byte(0x41)
	headerSEI    = byte(0x06)
)

// annexB renders NAL units as an elementary stream with four-byte start codes.
func annexB(units ...[]byte) []byte {
	var out []byte
	for _, unit := range units {
		out = append(out, 0, 0, 0, 1)
		out = append(out, unit...)
	}

	return out
}

// unit builds one NAL unit of a given header and length.
//
// The first body byte is 0x88, which is first_mb_in_slice = 0 followed by
// filler. A slice header that read as anything else would be a later slice of
// the picture in front of it, which is what laterSlice is for.
func unit(header byte, size int) []byte {
	body := bytes.Repeat([]byte{0x42}, size)
	body[0] = 0x88

	return append([]byte{header}, body...)
}

// laterSlice builds a slice that is not the first of its picture: the leading
// ue(v) reads as a macroblock address past zero.
func laterSlice(header byte, size int) []byte {
	body := bytes.Repeat([]byte{0x42}, size)
	body[0] = 0x02

	return append([]byte{header}, body...)
}

// TestTheSplitterReadsEveryUnitBackWhateverTheStartCodeWidth holds the one thing
// the whole remux rests on. A splitter that loses a unit loses a picture, and a
// splitter that keeps the start code's trailing zero puts them on the wire.
func TestTheSplitterReadsEveryUnitBackWhateverTheStartCodeWidth(t *testing.T) {
	t.Parallel()

	// Three-byte and four-byte start codes in one stream, which is what an
	// encoder writes: four bytes before a parameter set and three between
	// slices. The run of five zeroes before the last unit is the trailing
	// padding H.264 allows, and it belongs to neither unit.
	stream := []byte{
		0, 0, 0, 1, headerSPS, 0x11,
		0, 0, 1, headerPPS, 0x22,
		0, 0, 0, 0, 0, 1, headerIDR, 0x33,
	}

	read := newUnits(bytes.NewReader(stream))

	for _, want := range [][]byte{
		{headerSPS, 0x11}, {headerPPS, 0x22}, {headerIDR, 0x33},
	} {
		got, err := read.next()
		require.NoError(t, err)
		require.Equal(t, want, got)
	}

	_, err := read.next()
	require.ErrorIs(t, err, io.EOF)
}

// A start code that straddles two reads is the case a scan that never looks
// back loses. The device sends whatever the socket gives it, so the boundary
// falls wherever it falls.
func TestAStartCodeSplitAcrossTwoReadsStillDelimitsTheUnit(t *testing.T) {
	t.Parallel()

	read := newUnits(&trickle{chunks: [][]byte{
		{0, 0, 0, 1, headerSPS, 0x11, 0, 0},
		{0, 1, headerIDR, 0x33},
	}})

	first, err := read.next()
	require.NoError(t, err)
	require.Equal(t, []byte{headerSPS, 0x11}, first)

	second, err := read.next()
	require.NoError(t, err)
	require.Equal(t, []byte{headerIDR, 0x33},
		second, "the start code fell across two reads and the unit before it was lost")
}

// A camera that sends a start code and then never sends another is a device on
// somebody's network spending this host's memory. The ceiling is what stops it,
// and this case fails without it by allocating until the process dies.
func TestAUnitThatNeverEndsIsRefusedAtTheCeiling(t *testing.T) {
	t.Parallel()

	// The source is bounded so the case is bounded. A build with the ceiling
	// deleted reads all of it and reports the end of the stream, which is a
	// different error and a failure here.
	endless := io.MultiReader(
		bytes.NewReader([]byte{0, 0, 0, 1, headerIDR}),
		io.LimitReader(neverZero{}, 1<<20),
	)

	_, err := newUnitsWithLimit(endless, 4<<10).next()
	require.ErrorIs(t, err, ErrUnitTooLarge)
}

// A driver that declared h264/annexb and framed something else is refused
// before a byte of it reaches a browser. MJPEG is the realistic mistake,
// because it is the other format every camera here declares.
func TestAStreamThatIsNotAnnexBIsRefusedAtTheFirstRead(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"multipart": "--prusaframe\r\nContent-Type: image/jpeg\r\n\r\n",
		"html":      "<html><body>camera</body></html>",
		"empty":     "",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := newUnits(strings.NewReader(body)).next()
			require.ErrorIs(t, err, ErrNotAnnexB)
		})
	}
}

// A read failure from the device is reported as one, and not as the end of the
// stream. A relay that read the two the same way would report a camera that was
// pulled off the network as a camera that finished.
func TestAReadFailureIsNotReportedAsTheEndOfTheStream(t *testing.T) {
	t.Parallel()

	broken := errors.New("the network went away")

	_, err := newUnits(io.MultiReader(
		bytes.NewReader([]byte{0, 0, 0, 1, headerIDR, 0x33}),
		failing{err: broken},
	)).next()

	require.ErrorIs(t, err, broken)
	require.NotErrorIs(t, err, io.EOF)
}

// trickle hands out one chunk per read, which is what a socket does and what a
// reader assembled from one big slice never exercises.
type trickle struct {
	chunks [][]byte
	at     int
}

func (t *trickle) Read(p []byte) (int, error) {
	if t.at >= len(t.chunks) {
		return 0, io.EOF
	}

	n := copy(p, t.chunks[t.at])
	t.at++

	return n, nil
}

// neverZero is a device that answers for ever and never sends another start
// code. The bytes are chosen so no three of them are a start code.
type neverZero struct{}

func (neverZero) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0x42
	}

	return len(p), nil
}

// failing is a device that fails partway through.
type failing struct{ err error }

func (f failing) Read([]byte) (int, error) { return 0, f.err }
