package webrtc

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

// FuzzNoAccessUnitGrowsPastTheCeiling feeds the access unit assembler whatever
// a camera puts on the wire.
//
// The bytes are attacker-influenced in the strong sense: they come off a camera
// on somebody's network, and where one access unit ends is a decision written
// into the stream by the device. The property asserted is the one that has to
// hold whatever it writes: nothing panics, and the buffer the assembler holds
// between two pictures never passes its ceiling. A ceiling of a kilobyte rather
// than four megabytes, so an input a fuzzer will actually generate can reach it.
func FuzzNoAccessUnitGrowsPastTheCeiling(f *testing.F) {
	const ceiling = 1 << 10

	// A well-formed picture, so the corpus starts on the emitting path.
	f.Add(annexB(unit(headerSPS, 4), unit(headerPPS, 3), unit(headerIDR, 16),
		unit(headerNonIDR, 8)))

	// The two shapes that hold the buffer open: a stream with no slice in it at
	// all, and slices that never carry first_mb_in_slice zero.
	f.Add(bytes.Repeat(annexB(unit(headerSEI, 24)), 64))
	f.Add(append(annexB(unit(headerIDR, 8)),
		bytes.Repeat(annexB(laterSlice(headerNonIDR, 24)), 64)...))

	// A stream that is not Annex-B at all, and one truncated mid-unit.
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xE0})
	f.Add([]byte{0, 0, 0, 1, headerIDR, 0x88, 0, 0})

	f.Fuzz(func(t *testing.T, stream []byte) {
		read := newPicturesWithCeiling(bytes.NewReader(stream), ceiling)

		for {
			done, err := read.next()
			if err != nil {
				break
			}

			require.NotEmpty(t, done, "an emitted access unit holds no bytes")
			require.LessOrEqual(t, len(read.pending), ceiling,
				"the assembler held %d bytes against a %d byte ceiling", len(read.pending), ceiling)
		}

		require.LessOrEqual(t, len(read.pending), ceiling,
			"the assembler held %d bytes against a %d byte ceiling when the stream ended",
			len(read.pending), ceiling)
	})
}
