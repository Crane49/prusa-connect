package webrtc

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

// One sample is one access unit, because the payloader marks the last packet of
// a sample and a decoder reads that mark as the end of a picture. A relay that
// wrote one sample per NAL unit would mark the end of a picture three times for
// one keyframe.
func TestAnAccessUnitHoldsOnePictureAndTheUnitsInFrontOfIt(t *testing.T) {
	t.Parallel()

	stream := annexB(
		unit(headerSPS, 4), unit(headerPPS, 3), unit(headerIDR, 16),
		unit(headerSEI, 2), unit(headerNonIDR, 8),
		unit(headerNonIDR, 9),
	)

	read := newPictures(bytes.NewReader(stream))

	first, err := read.next()
	require.NoError(t, err)
	require.Equal(t,
		annexB(unit(headerSPS, 4), unit(headerPPS, 3), unit(headerIDR, 16)),
		first, "the keyframe and its parameter sets are one access unit")

	second, err := read.next()
	require.NoError(t, err)
	require.Equal(t,
		annexB(unit(headerSEI, 2), unit(headerNonIDR, 8)),
		second, "the SEI belongs to the picture that follows it")

	third, err := read.next()
	require.NoError(t, err)
	require.Equal(t, annexB(unit(headerNonIDR, 9)), third)

	_, err = read.next()
	require.ErrorIs(t, err, io.EOF)
}

// A viewer who joins between two keyframes has no reference frame, so a relay
// that started sending would render artefacts until the next one. The wait is
// the honest answer and it is what this holds.
func TestNothingIsPlayedUntilTheFirstKeyframeArrives(t *testing.T) {
	t.Parallel()

	stream := annexB(
		unit(headerNonIDR, 5), unit(headerNonIDR, 6), unit(headerNonIDR, 7),
		unit(headerSPS, 4), unit(headerPPS, 3), unit(headerIDR, 16),
		unit(headerNonIDR, 8),
	)

	read := newPictures(bytes.NewReader(stream))

	first, err := read.next()
	require.NoError(t, err)
	require.Equal(t,
		annexB(unit(headerSPS, 4), unit(headerPPS, 3), unit(headerIDR, 16)),
		first, "a picture before the first keyframe was played, and a decoder has no reference for it")
}

// A camera that sends its parameter sets once at the top of the stream leaves
// every later viewer with a keyframe no decoder can configure itself from. The
// sets are replayed in front of the first access unit a viewer sees.
func TestTheFirstAccessUnitCarriesTheParameterSetsTheStreamOpenedWith(t *testing.T) {
	t.Parallel()

	stream := annexB(
		unit(headerSPS, 4), unit(headerPPS, 3), unit(headerIDR, 16),
		unit(headerNonIDR, 8),
		// A second keyframe with no parameter sets in front of it, which is
		// what a camera that sends them once produces.
		unit(headerIDR, 17),
	)

	read := newPictures(bytes.NewReader(stream))

	// Consume the opening keyframe, so the case is about what a later one
	// carries and not about the first.
	for range 2 {
		_, err := read.next()
		require.NoError(t, err)
	}

	third, err := read.next()
	require.NoError(t, err)
	require.Equal(t, annexB(unit(headerIDR, 17)), third,
		"the sets are replayed once, in front of the first unit a viewer sees, and not before every one")
}

// The replay happens for a viewer who joined after the parameter sets went by,
// which is the case it exists for.
func TestAViewerWhoJoinedAfterTheParameterSetsStillGetsThem(t *testing.T) {
	t.Parallel()

	// The SPS and PPS arrive, then a picture nothing can be built from, then
	// the keyframe this viewer's first access unit is.
	stream := annexB(
		unit(headerSPS, 4), unit(headerPPS, 3),
		unit(headerNonIDR, 5),
		unit(headerIDR, 16),
	)

	first, err := newPictures(bytes.NewReader(stream)).next()
	require.NoError(t, err)
	require.Equal(t,
		annexB(unit(headerSPS, 4), unit(headerPPS, 3), unit(headerIDR, 16)),
		first, "the keyframe went out with no SPS and no PPS, so a browser shows nothing")
}

// A picture is not one slice. x264 at its faster presets emits seven slices per
// picture, and a relay that read each one as a picture marks the end of a frame
// seven times and advances the RTP clock seven times as fast. A browser then
// decodes and drops almost everything it is sent.
func TestAPictureOfSeveralSlicesIsOneAccessUnit(t *testing.T) {
	t.Parallel()

	stream := annexB(
		unit(headerSPS, 4), unit(headerPPS, 3),
		unit(headerIDR, 20), laterSlice(headerIDR, 21), laterSlice(headerIDR, 22),
		unit(headerNonIDR, 10), laterSlice(headerNonIDR, 11),
	)

	read := newPictures(bytes.NewReader(stream))

	first, err := read.next()
	require.NoError(t, err)
	require.Equal(t,
		annexB(unit(headerSPS, 4), unit(headerPPS, 3),
			unit(headerIDR, 20), laterSlice(headerIDR, 21), laterSlice(headerIDR, 22)),
		first, "the keyframe's three slices were split across access units")

	second, err := read.next()
	require.NoError(t, err)
	require.Equal(t,
		annexB(unit(headerNonIDR, 10), laterSlice(headerNonIDR, 11)),
		second, "the next picture's two slices were split across access units")

	_, err = read.next()
	require.ErrorIs(t, err, io.EOF)
}
