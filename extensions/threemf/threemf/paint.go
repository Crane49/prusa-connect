package threemf

import (
	"encoding/xml"
	"math/bits"
)

// stateSet is a set of painting states, 0 to 255. State 0 is an unpainted
// area. For the multi-material painting, state n is extruder n; for supports
// 1 adds support and 2 blocks it, and for seams 1 puts the seam there and 2
// keeps it away.
type stateSet [4]uint64

// maxPaintState is the highest state a set holds. PrusaSlicer 2.9 keeps 256
// states and ignores any above them.
const maxPaintState = 255

// wordBits is the number of states one word of a stateSet holds.
const wordBits = 64

func (s *stateSet) add(state int) {
	if state >= 0 && state <= maxPaintState {
		s[state/wordBits] |= 1 << (state % wordBits)
	}
}

func (s *stateSet) has(state int) bool {
	return state >= 0 && state <= maxPaintState && s[state/wordBits]&(1<<(state%wordBits)) != 0
}

func (s *stateSet) merge(o stateSet) {
	for i := range s {
		s[i] |= o[i]
	}
}

func (s *stateSet) empty() bool { return *s == stateSet{} }

// painted reports whether any state but 0 is in the set.
func (s *stateSet) painted() bool {
	return s[0]&^1 != 0 || s[1] != 0 || s[2] != 0 || s[3] != 0
}

// each calls f for every state in the set, from the lowest.
func (s *stateSet) each(f func(int)) {
	for i, word := range s {
		for word != 0 {
			b := bits.TrailingZeros64(word)
			f(i*wordBits + b)
			word &^= 1 << b
		}
	}
}

// paint is what the painting tools of a slicer left on a run of triangles.
type paint struct {
	// colors are the multi-material states of the triangles. State 0 is a
	// triangle that prints with its part's own extruder, painted or not.
	colors   stateSet
	supports bool
	// enforcers is true when an area is painted to have support.
	enforcers bool
	seams     bool
}

func (p *paint) merge(o paint) {
	p.colors.merge(o.colors)
	p.supports = p.supports || o.supports
	p.enforcers = p.enforcers || o.enforcers
	p.seams = p.seams || o.seams
}

// The support painting states of TriangleStateType: 1 adds support.
const paintEnforcer = 1

// paintKind is which painting tool an attribute of a triangle belongs to.
type paintKind int

const (
	paintNone paintKind = iota
	paintColors
	paintSupports
	paintSeams
)

// paintKindOf names the painting tool of a triangle attribute. PrusaSlicer
// writes slic3rpe:mmu_segmentation, slic3rpe:custom_supports and
// slic3rpe:custom_seam (src/libslic3r/Format/3mf.cpp of 2.9.6); OrcaSlicer
// writes paint_color, paint_supports and paint_seam with no namespace
// (src/libslic3r/Format/bbs_3mf.cpp).
func paintKindOf(name xml.Name, prefixes map[string]string) paintKind {
	if name.Space != "" {
		if prefixes[name.Space] != nsSlic3r {
			return paintNone
		}

		switch name.Local {
		case "mmu_segmentation":
			return paintColors
		case "custom_supports":
			return paintSupports
		case "custom_seam":
			return paintSeams
		}

		return paintNone
	}

	switch name.Local {
	case "paint_color":
		return paintColors
	case "paint_supports":
		return paintSupports
	case "paint_seam":
		return paintSeams
	}

	return paintNone
}

// The nibbles of the painting encoding. A nibble whose two low bits are not
// zero splits the triangle. A leaf nibble holds its state in its two high
// bits, and the value 3 there says the state follows in more nibbles.
const (
	nibbleSplit    = 0b0011
	nibbleLong     = 0b1100
	nibbleBits     = 4
	shortStates    = 3
	ps3LongMarker  = 0b1110
	ps3LongStates  = 17
	orcaLongMarker = 0b1111
	orcaLongStates = 18
)

// decodePaint adds the states of the leaf triangles of one painted triangle
// to states, and reports whether the text was read.
//
// The text is hexadecimal, one digit a nibble, with the first nibble last:
// FacetsAnnotation::get_triangle_as_string writes each digit in front of the
// one before. A leaf with a state of 3 or more is followed by a nibble that
// holds the state less 3. Above 16 the slicers differ.
// TriangleSelector::deserialize of PrusaSlicer 2.9.6 reads 0b1110 there as
// two more nibbles, low first, holding the state less 17. OrcaSlicer reads
// 0b1111 there as one more nibble holding the state less 18.
func decodePaint(text string, orca bool, states *stateSet) bool {
	pos := len(text)
	next := func() (int, bool) {
		if pos == 0 {
			return 0, false
		}

		pos--

		return hexNibble(text[pos])
	}

	read := false

	for pos > 0 {
		code, ok := next()
		if !ok {
			return read
		}

		read = true

		if code&nibbleSplit != 0 {
			continue
		}

		if code&nibbleLong != nibbleLong {
			states.add(code >> 2) //nolint:mnd // the two high bits of the nibble.

			continue
		}

		state, ok := longState(next, orca)
		if !ok {
			return read
		}

		states.add(state)
	}

	return read
}

// longState reads the state of a leaf after its 0b1100 nibble.
func longState(next func() (int, bool), orca bool) (int, bool) {
	second, ok := next()
	if !ok {
		return 0, false
	}

	switch {
	case orca && second == orcaLongMarker:
		n, okN := next()

		return n + orcaLongStates, okN
	case !orca && second == ps3LongMarker:
		lo, okLo := next()
		hi, okHi := next()

		return (lo | hi<<nibbleBits) + ps3LongStates, okLo && okHi
	}

	return second + shortStates, true
}

func hexNibble(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true //nolint:mnd // hexadecimal.
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true //nolint:mnd // hexadecimal.
	}

	return 0, false
}

// paintOfPart joins the painting of the triangles a part is made of.
func paintOfPart(pieces []piece) paint {
	var out paint

	for _, pc := range pieces {
		if pc.mesh == nil {
			continue
		}

		if pc.hi < 0 && pc.tlast < 0 {
			out.merge(pc.mesh.paint)

			continue
		}

		for _, r := range pc.mesh.ranges {
			if r.firstTri == pc.tfirst && r.lastTri == pc.tlast {
				out.merge(r.paint)

				break
			}
		}
	}

	return out
}
