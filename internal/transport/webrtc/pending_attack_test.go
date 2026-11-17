package webrtc

import (
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

// errStreamCut is what the hostile device answers with once it has sent enough
// to make the point. It is not io.EOF, because an EOF would run flush and put
// the assembler back to a state a test cannot read the growth off.
var errStreamCut = errors.New("the camera stopped answering")

// endless writes one NAL unit over and over until it has produced sent bytes,
// then fails. It never returns io.EOF, which is what a device that keeps its
// socket open and keeps talking looks like.
//
// The stream is generated rather than held, so the test's own memory is the
// reader's buffer and not the attack.
type endless struct {
	lead    []byte
	repeat  []byte
	budget  int
	written int
	at      int
}

func (e *endless) Read(p []byte) (int, error) {
	if e.written >= e.budget {
		return 0, errStreamCut
	}

	wrote := 0

	for wrote < len(p) && e.written < e.budget {
		source := e.repeat
		if e.written < len(e.lead) {
			source = e.lead[e.written:]

			n := copy(p[wrote:], source)
			wrote += n
			e.written += n

			continue
		}

		n := copy(p[wrote:], source[e.at:])
		wrote += n
		e.written += n
		e.at += n

		if e.at == len(source) {
			e.at = 0
		}
	}

	return wrote, nil
}

// nalUnit is one Annex-B framed NAL unit with a four-byte start code, its
// header byte, and body bytes after it.
func nalUnit(header byte, body ...byte) []byte {
	out := []byte{0, 0, 0, 1, header}

	return append(out, body...)
}

// TestNoAccessUnitGrowsPastTheUnitCeiling is the memory bound over the whole
// remux, rather than over one NAL unit.
//
// units already refuses a single unit over unitCeiling, and the reason it gives
// is that "a device that sends one start code and then never sends another is a
// camera on somebody's network spending this host's memory". pictures then
// accumulates well-formed units into p.pending with no ceiling at all, and the
// boundary rule is what a device decides: nothing before the first slice ends
// an access unit, so a device that never sends a slice is that same memory
// attack one layer up, in units this reader is happy to accept.
//
// The stream here is legal H.264 framing throughout. Every unit is small, every
// start code is where it belongs, and nothing in it trips unitCeiling.
func TestNoAccessUnitGrowsPastTheUnitCeiling(t *testing.T) {
	t.Parallel()

	const sent = 32 << 20

	// A run of SEI units and nothing else. isPicture is false for all of them,
	// so hasSlice never becomes true, so starts never returns true, so nothing
	// is ever flushed.
	device := &endless{
		lead:   nalUnit(unitSPS, 0x42, 0x00, 0x1E),
		repeat: nalUnit(unitSEI, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x80),
		budget: sent,
	}

	assembler := newPictures(device)

	_, err := assembler.next()
	require.ErrorIs(t, err, errStreamCut, "the reader stopped for a reason this test did not arrange")

	require.LessOrEqual(t, len(assembler.pending), unitCeiling,
		"a device that sends no slice grew the access unit to %d MiB against a %d MiB ceiling on a unit",
		len(assembler.pending)>>20, unitCeiling>>20)
}

// TestNoAccessUnitGrowsPastTheUnitCeilingOnSlicesThatNeverStartOne is the same
// unbounded append reached through the multi-slice rule instead.
//
// first_mb_in_slice is what tells one picture from the next, because x264 emits
// seven slices per picture and a boundary at every slice would run the clock
// seven times fast. A device that sets it non-zero on every slice it ever sends
// declares an access unit that never ends, and the assembler holds every byte
// of it. This one gets past the keyframe gate first, so it is the case where
// the samples were flowing normally a moment before.
func TestNoAccessUnitGrowsPastTheUnitCeilingOnSlicesThatNeverStartOne(t *testing.T) {
	t.Parallel()

	const sent = 32 << 20

	// An IDR opens the stream so seenIDR is set and the assembler is emitting,
	// then every slice after it carries first_mb_in_slice != 0: the leading bit
	// of the slice header is zero, which is the ue(v) prefix of a non-zero
	// value.
	lead := nalUnit(unitSPS, 0x42, 0x00, 0x1E)
	lead = append(lead, nalUnit(unitPPS, 0xCE, 0x3C, 0x80)...)
	lead = append(lead, nalUnit(unitIDR, 0x88, 0x84, 0x00, 0x10)...)

	device := &endless{
		lead:   lead,
		repeat: nalUnit(unitNonIDR, 0x21, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77),
		budget: sent,
	}

	assembler := newPictures(device)

	for {
		_, err := assembler.next()
		if err != nil {
			require.ErrorIs(t, err, errStreamCut,
				"the reader stopped for a reason this test did not arrange")

			break
		}
	}

	require.LessOrEqual(t, len(assembler.pending), unitCeiling,
		"slices that never start a picture grew the access unit to %d MiB against a %d MiB ceiling on a unit",
		len(assembler.pending)>>20, unitCeiling>>20)
}

// TestTheReaderStopsWhenTheDeviceDoes is the control. It fails the same way if
// the fixture above is wrong rather than the code, so the two tests are not
// both reading one broken reader.
func TestTheReaderStopsWhenTheDeviceDoes(t *testing.T) {
	t.Parallel()

	device := &endless{
		lead:   nalUnit(unitSPS, 0x42),
		repeat: nalUnit(unitSEI, 0x80),
		budget: 1 << 10,
	}

	reader := newUnits(device)

	seen := 0

	for {
		unit, err := reader.next()
		if err != nil {
			require.ErrorIs(t, err, errStreamCut)

			break
		}

		require.NotEmpty(t, unit)

		seen++
	}

	require.Positive(t, seen, "the fixture produced no readable unit at all")
	require.Less(t, seen, 1<<10, "the fixture produced more units than it wrote bytes")
}

var _ io.Reader = (*endless)(nil)
