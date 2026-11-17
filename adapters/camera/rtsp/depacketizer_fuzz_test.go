package rtsp

import (
	"testing"
)

// FuzzDepacketizerTakesAnyPacket feeds the RFC 6184 depacketiser whatever a
// device puts on the wire.
//
// The packets are attacker-influenced in the strong sense: they come off a
// camera on somebody's network, they are parsed by hand, and every length in
// them is the device's to choose. The properties asserted are the two that hold
// whatever the device sends: nothing panics, and no unit grows past the limit
// this depacketiser was given.
//
// Four packets rather than one, because the FU-A path carries state between
// them, and truncated, overlapping and out-of-order fragments are only
// reachable in a sequence.
func FuzzDepacketizerTakesAnyPacket(f *testing.F) {
	const limit = 4 << 10

	// A well-formed single NAL, a STAP-A, and the three packets of a fragmented
	// unit, so the corpus starts on every branch.
	f.Add(
		[]byte{0x80, 0x60, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0x65, 0xAA, 0xBB},
		[]byte{0x80, 0x60, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 24, 0, 2, 0x67, 0x42, 0, 2, 0x68, 0xCE},
		[]byte{0x80, 0x60, 0, 3, 0, 0, 0, 0, 0, 0, 0, 0, 28, 0x85, 0x11, 0x22},
		[]byte{0x80, 0xE0, 0, 4, 0, 0, 0, 0, 0, 0, 0, 0, 28, 0x45, 0x33, 0x44},
	)

	// A padded packet, an extended one, one with a contributing source list,
	// and a fragment with no start.
	f.Add(
		[]byte{0xA0, 0x60, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0x65, 0xAA, 0x02},
		[]byte{0x90, 0x60, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0xBE, 0xDE, 0, 1, 0, 0, 0, 0, 0x65, 0xCC},
		[]byte{0x81, 0x60, 0, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0xDE, 0xAD, 0xBE, 0xEF, 0x65, 0xDD},
		[]byte{0x80, 0x60, 0, 4, 0, 0, 0, 0, 0, 0, 0, 0, 28, 0x05, 0x55},
	)

	f.Fuzz(func(t *testing.T, a, b, c, d []byte) {
		units := newDepacketizer(limit)

		out := make([]byte, 0, limit)

		for _, packet := range [][]byte{a, b, c, d} {
			var err error

			// The same call shape stream.pump makes: the frame is reset every
			// packet, so anything that grows across packets is the assembler's
			// own state.
			out, err = units.append(out[:0], packet)
			if err != nil {
				continue
			}

			// marked reads the header too, and it is on the same path.
			_ = marked(packet)
		}

		if len(units.unit) > limit {
			t.Fatalf("a reassembled unit reached %d bytes against a %d byte limit",
				len(units.unit), limit)
		}
	})
}
