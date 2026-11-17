package threemf

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// Arranging copies of objects onto beds.
//
// The PrusaSlicer command line does not arrange a 3MF project: 2.9.6 and
// 3.0.0-alpha12 both switch arranging off when they load one, because a
// project brings its own placement. So a program that wants twenty copies of
// one object arranges them here, and Rewrite writes the copies into the
// project with Selection.Positions.

// Piece is one object to arrange: the build item whose copies are placed,
// and how many copies.
type Piece struct {
	// Item is the build item, by Instance.Item. Its copies keep its
	// rotation, its scale and its height, and only move on the bed.
	Item int `json:"item"`
	// Copies is how many copies to place, at least one.
	Copies int `json:"copies"`
}

// Position puts one copy of a build item on a bed.
type Position struct {
	// Item is the build item, by Instance.Item, whose copy is placed.
	Item int `json:"item"`
	// X and Y are where the centre of the item's box stands, in millimetres
	// from the origin of the bed shape. The origin is the point the printer
	// calls 0,0, so the same placement means the same spot on every bed of
	// the setup.
	X float64 `json:"x"`
	Y float64 `json:"y"`
	// Rotate turns the copy about the vertical axis through the centre of
	// the item's box, in degrees counterclockwise: 0 or 90. Arrange turns a
	// copy only when it fits no empty bed as it stands.
	Rotate int `json:"rotate,omitempty"`
}

// RightAngle is the one turn a position may ask for besides none.
const RightAngle = 90

// Where PrusaSlicer puts a wipe tower whose position no setting names, in
// millimetres from the bed's origin.
const (
	defaultWipeTowerX = 180
	defaultWipeTowerY = 140
)

// Plate is one bed's worth of copies.
type Plate struct {
	Positions []Position `json:"positions"`
}

// Pieces counts the copies on the plate by build item, items ascending.
func (p Plate) Pieces() []Piece {
	counts := map[int]int{}
	for _, placed := range p.Positions {
		counts[placed.Item]++
	}

	out := make([]Piece, 0, len(counts))
	for item, copies := range counts {
		out = append(out, Piece{Item: item, Copies: copies})
	}

	slices.SortFunc(out, func(a, b Piece) int { return cmp.Compare(a.Item, b.Item) })

	return out
}

// ArrangeOptions tunes Arrange.
type ArrangeOptions struct {
	// Setup is the printer setup whose bed shape the copies go on.
	Setup int
	// Gap is the space kept between two copies, in millimetres. Zero takes
	// DefaultGap. The edge of the bed keeps half of it.
	Gap float64
	// MaxPlates bounds the answer. Zero takes DefaultMaxPlates.
	MaxPlates int
}

// The bounds of an arrangement.
const (
	// DefaultGap is the distance PrusaSlicer's arrange keeps between objects
	// on an FFF printer by default, 6 mm.
	DefaultGap = 6.0
	// DefaultMaxPlates is how many plates one Arrange makes at most.
	DefaultMaxPlates = 64
	// MaxPositions is how many copies one plate, and one Selection, holds.
	MaxPositions = 512
	// MaxCopies is how many copies one Arrange places in all.
	MaxCopies = 4096
)

// ErrDoesNotFit is a copy that does not fit an empty bed of the setup, or an
// arrangement that needs more plates than ArrangeOptions.MaxPlates.
var ErrDoesNotFit = errors.New("threemf: the copies do not fit the beds")

// Arrange places the copies of the pieces onto as few beds of one setup as
// hold them, and answers one Plate per bed.
//
// Each copy keeps the orientation of its build item and is only moved. A copy
// that fits no empty bed as it stands, and fits one turned by a right angle
// about the vertical axis, is turned; a turn about that axis keeps its layers
// and its supports as they were. A copy is placed by its outline, the convex hull Read computed seen from
// above, so two copies keep Gap between their outlines. Larger copies go
// first, and each copy takes the lowest, then leftmost, free spot on the
// first plate where it fits. A full plate is followed by a new one. When a
// plate is done, its copies are moved together to the centre of the bed.
//
// Plates made of the same copies come out the same, so twenty copies of one
// object that fit nine to a bed make two equal plates and a third with two.
//
// A setup that prints with a wipe tower keeps the area its settings name for
// the tower free, or the area PrusaSlicer gives the tower when they name none. The depth of a tower depends on the purge volumes, which are
// not read here, so the area kept is a square as deep as the tower is wide.
func Arrange(p *Project, pieces []Piece, opts ArrangeOptions) ([]Plate, error) {
	if p == nil || opts.Setup < 0 || opts.Setup >= len(p.Setups) {
		return nil, fmt.Errorf("%w: no setup %d", ErrSelection, opts.Setup)
	}

	if opts.Gap <= 0 {
		opts.Gap = DefaultGap
	}

	if opts.MaxPlates <= 0 {
		opts.MaxPlates = DefaultMaxPlates
	}

	setup := &p.Setups[opts.Setup]

	bed := bedOutline(setup)
	if len(bed) < minPolygon {
		return nil, fmt.Errorf("%w: setup %d has no bed shape", ErrSelection, opts.Setup)
	}

	copies, err := arrangeCopies(p, pieces)
	if err != nil {
		return nil, err
	}

	area := newArrangeBed(bed, opts.Gap, keptAreas(p, setup))

	turnWhereNeeded(copies, area)

	var plates []*plateWork

	for _, c := range copies {
		placed := false

		for _, plate := range plates {
			if plate.place(c) {
				placed = true

				break
			}
		}

		if placed {
			continue
		}

		if len(plates) == opts.MaxPlates {
			return nil, fmt.Errorf("%w: the copies need more than %d plates", ErrDoesNotFit, opts.MaxPlates)
		}

		plate := &plateWork{bed: area}
		if !plate.place(c) {
			return nil, fmt.Errorf("%w: a copy of build item %d does not fit an empty bed of setup %d",
				ErrDoesNotFit, c.item, opts.Setup)
		}

		plates = append(plates, plate)
	}

	out := make([]Plate, len(plates))
	for i, plate := range plates {
		out[i] = plate.done()
	}

	return out, nil
}

// arrangeCopy is one copy to place: its item and its outline around the
// centre of the item's box.
type arrangeCopy struct {
	item    int
	outline []Point
	lo, hi  Point
	area    float64
	// rotate is RightAngle for a copy that fits an empty bed only turned.
	rotate int
}

// arrangeCopies checks the pieces and expands them into copies, the largest
// first.
func arrangeCopies(p *Project, pieces []Piece) ([]arrangeCopy, error) {
	total := 0

	var out []arrangeCopy

	seen := map[int]bool{}

	for _, piece := range pieces {
		if piece.Copies < 1 {
			return nil, fmt.Errorf("%w: build item %d asks for %d copies", ErrSelection, piece.Item, piece.Copies)
		}

		if seen[piece.Item] {
			return nil, fmt.Errorf("%w: build item %d is named twice", ErrSelection, piece.Item)
		}

		seen[piece.Item] = true

		if total += piece.Copies; total > MaxCopies {
			return nil, fmt.Errorf("%w: more than %d copies", ErrSelection, MaxCopies)
		}

		outline, ok := itemOutline(p, piece.Item)
		if !ok {
			return nil, fmt.Errorf("%w: no build item %d with an outline", ErrSelection, piece.Item)
		}

		lo, hi := bounds2(outline)
		c := arrangeCopy{item: piece.Item, outline: outline, lo: lo, hi: hi, area: math.Abs(polygonArea(outline))}

		for range piece.Copies {
			out = append(out, c)
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("%w: nothing to arrange", ErrSelection)
	}

	slices.SortStableFunc(out, func(a, b arrangeCopy) int {
		if order := cmp.Compare(b.area, a.area); order != 0 {
			return order
		}

		return cmp.Compare(a.item, b.item)
	})

	return out, nil
}

// itemOutline is the outline of a build item around the centre of its box,
// or the box itself when the item has no outline.
func itemOutline(p *Project, item int) ([]Point, bool) {
	ref, ok := p.Item(item)
	if !ok {
		return nil, false
	}

	inst := p.Instance(ref)
	if inst == nil || inst.Bounds.Empty() {
		return nil, false
	}

	centre := boxCentre(inst.Bounds)

	outline := inst.Outline
	if len(outline) < minPolygon {
		b := inst.Bounds
		outline = []Point{{b.Min[0], b.Min[1]}, {b.Max[0], b.Min[1]}, {b.Max[0], b.Max[1]}, {b.Min[0], b.Max[1]}}
	}

	return translate(outline, Point{X: -centre.X, Y: -centre.Y}), true
}

func boxCentre(b Box) Point {
	return Point{X: (b.Min[0] + b.Max[0]) / 2, Y: (b.Min[1] + b.Max[1]) / 2} //nolint:mnd // the middle.
}

// bedOutline is the convex outline of a setup's bed shape, in the frame of
// the bed shape: the frame a Position is written in.
func bedOutline(s *Setup) []Point {
	if len(s.BedShape) < minPolygon {
		return nil
	}

	return bedHull(s.BedShape)
}

// arrangeBed is the room on one bed: its outline, the gap and the areas kept
// free.
type arrangeBed struct {
	outline []Point
	lo, hi  Point
	gap     float64
	kept    [][]Point
}

func newArrangeBed(outline []Point, gap float64, kept [][]Point) *arrangeBed {
	lo, hi := bounds2(outline)

	return &arrangeBed{outline: outline, lo: lo, hi: hi, gap: gap, kept: kept}
}

// holds reports whether the polygon stands on the bed, half a gap inside its
// edge, and clear of the areas kept free.
func (b *arrangeBed) holds(poly []Point) bool {
	margin := b.gap / 2 //nolint:mnd // the edge keeps half a gap.

	for _, p := range poly {
		if !insideBy(b.outline, p, margin) {
			return false
		}
	}

	for _, kept := range b.kept {
		if distanceAtLeast(kept, poly, b.gap) {
			continue
		}

		return false
	}

	return true
}

// insideBy reports whether p is inside the convex counter-clockwise polygon
// and at least margin from each of its edges.
func insideBy(poly []Point, p Point, margin float64) bool {
	for i := range poly {
		a, b := poly[i], poly[(i+1)%len(poly)]
		length := math.Hypot(b.X-a.X, b.Y-a.Y)

		if length == 0 {
			continue
		}

		if cross(a, b, p)/length < margin-placeEpsilon {
			return false
		}
	}

	return true
}

// distanceAtLeast reports whether two convex polygons stand at least gap
// apart along some edge normal of either. Two convex shapes that far apart
// along one direction are at least that far apart, so the test never lets
// two copies come closer than gap. It may refuse two that would fit corner
// to corner.
func distanceAtLeast(a, b []Point, gap float64) bool {
	loA, hiA := bounds2(a)
	loB, hiB := bounds2(b)

	if loA.X-hiB.X >= gap || loB.X-hiA.X >= gap || loA.Y-hiB.Y >= gap || loB.Y-hiA.Y >= gap {
		return true
	}

	return edgeGap(a, b) >= gap || edgeGap(b, a) >= gap
}

// edgeGap is the largest distance, along the outer normal of an edge of a, by
// which all of b stands outside that edge.
func edgeGap(a, b []Point) float64 {
	best := math.Inf(-1)

	if len(a) < minPolygon {
		return best
	}

	for i := range a {
		p, q := a[i], a[(i+1)%len(a)]
		length := math.Hypot(q.X-p.X, q.Y-p.Y)

		if length == 0 {
			continue
		}

		// cross is positive inside a counter-clockwise polygon, so the
		// distance outside is its negative.
		nearest := math.Inf(1)
		for _, point := range b {
			nearest = min(nearest, -cross(p, q, point)/length)
		}

		best = max(best, nearest)
	}

	return best
}

// keptAreas are the areas of a setup's bed no copy may take: the wipe tower,
// when the setup prints with one and the project uses more than one
// extruder. A setup that names no position for the tower keeps the one
// PrusaSlicer puts it at when nothing names it.
func keptAreas(p *Project, s *Setup) [][]Point {
	if len(p.Extruders) < 2 || !settingTrue(s, "wipe_tower") {
		return nil
	}

	x, okX := settingFloat(s, "wipe_tower_x")
	y, okY := settingFloat(s, "wipe_tower_y")

	if !okX || !okY {
		x, y = defaultWipeTowerX, defaultWipeTowerY
	}

	width, ok := settingFloat(s, "wipe_tower_width")
	if !ok || width <= 0 {
		width = 60
	}

	return [][]Point{{{x, y}, {x + width, y}, {x + width, y + width}, {x, y + width}}}
}

// settingText is the first value of key in any settings group of the setup,
// without the quotes a JSON value carries.
func settingText(s *Setup, key string) (string, bool) {
	for _, group := range s.Settings {
		if value, ok := group.Values[key]; ok {
			value = strings.Trim(strings.TrimSpace(value), `"`)
			// A setting of several extruders holds one value each, and the
			// first is the one a single tower takes.
			first, _, _ := strings.Cut(value, ",")

			return strings.TrimSpace(first), true
		}
	}

	return "", false
}

func settingFloat(s *Setup, key string) (float64, bool) {
	text, ok := settingText(s, key)
	if !ok {
		return 0, false
	}

	value, err := strconv.ParseFloat(text, 64)
	if err != nil || !finite(value) {
		return 0, false
	}

	return value, true
}

func settingTrue(s *Setup, key string) bool {
	text, _ := settingText(s, key)

	return text == "1" || text == "true"
}

// plateWork is one plate while copies are placed on it.
type plateWork struct {
	bed    *arrangeBed
	placed []placedCopy
}

// placedCopy is a copy on a plate: its item, its outline on the bed and the
// box of that outline.
type placedCopy struct {
	item    int
	at      Point
	outline []Point
	lo, hi  Point
	rotate  int
}

// place puts the copy at the lowest, then leftmost, spot where it fits, and
// reports false when there is none. The spots tried are the edge of the bed
// and the far side of each copy already placed, a gap away, in both
// directions.
func (w *plateWork) place(c arrangeCopy) bool {
	if len(w.placed) == MaxPositions {
		return false
	}

	return w.placeAs(c, c.rotate)
}

// turnWhereNeeded turns each copy that fits no empty bed as it stands and
// fits one turned. A copy that fits as it stands is never turned to fit more.
func turnWhereNeeded(copies []arrangeCopy, area *arrangeBed) {
	for i, c := range copies {
		if (&plateWork{bed: area}).placeAs(c, 0) {
			continue
		}

		if t := turned(c); (&plateWork{bed: area}).placeAs(t, RightAngle) {
			copies[i] = t
		}
	}
}

// turned is the copy turned by a right angle about the centre of its box.
func turned(c arrangeCopy) arrangeCopy {
	outline := turnPoints(c.outline)
	lo, hi := bounds2(outline)

	return arrangeCopy{item: c.item, outline: outline, lo: lo, hi: hi, area: c.area, rotate: RightAngle}
}

// turnPoints turns points by a right angle counterclockwise about the origin.
func turnPoints(points []Point) []Point {
	out := make([]Point, len(points))
	for i, point := range points {
		out[i] = Point{X: -point.Y, Y: point.X}
	}

	return out
}

// placeAs places the copy in one orientation, rotate degrees from its own.
func (w *plateWork) placeAs(c arrangeCopy, rotate int) bool {
	margin := w.bed.gap / 2 //nolint:mnd // the edge keeps half a gap.
	xs := []float64{w.bed.lo.X + margin}
	ys := []float64{w.bed.lo.Y + margin}

	for _, held := range w.placed {
		xs = append(xs, held.hi.X+w.bed.gap)
		ys = append(ys, held.hi.Y+w.bed.gap)
	}

	slices.Sort(xs)
	slices.Sort(ys)
	xs = slices.Compact(xs)
	ys = slices.Compact(ys)

	width, depth := c.hi.X-c.lo.X, c.hi.Y-c.lo.Y

	for _, y := range ys {
		if y+depth > w.bed.hi.Y-margin+placeEpsilon {
			break
		}

		for _, x := range xs {
			if x+width > w.bed.hi.X-margin+placeEpsilon {
				break
			}

			at := Point{X: x - c.lo.X, Y: y - c.lo.Y}
			if w.fits(c, at) {
				outline := translate(c.outline, at)
				lo, hi := bounds2(outline)
				w.placed = append(w.placed, placedCopy{
					item: c.item, at: at, outline: outline, lo: lo, hi: hi, rotate: rotate,
				})

				return true
			}
		}
	}

	return false
}

// fits reports whether the copy stands on the bed at the spot, a gap away
// from every copy placed.
func (w *plateWork) fits(c arrangeCopy, at Point) bool {
	outline := translate(c.outline, at)
	if !w.bed.holds(outline) {
		return false
	}

	for _, held := range w.placed {
		if !distanceAtLeast(held.outline, outline, w.bed.gap) {
			return false
		}
	}

	return true
}

// done moves the copies together to the centre of the bed, when they still
// fit there, and answers the plate.
func (w *plateWork) done() Plate {
	lo := Point{X: math.Inf(1), Y: math.Inf(1)}
	hi := Point{X: math.Inf(-1), Y: math.Inf(-1)}

	for _, held := range w.placed {
		lo.X, lo.Y = min(lo.X, held.lo.X), min(lo.Y, held.lo.Y)
		hi.X, hi.Y = max(hi.X, held.hi.X), max(hi.Y, held.hi.Y)
	}

	shift := Point{
		X: (w.bed.lo.X+w.bed.hi.X)/2 - (lo.X+hi.X)/2,
		Y: (w.bed.lo.Y+w.bed.hi.Y)/2 - (lo.Y+hi.Y)/2,
	}

	for _, held := range w.placed {
		if !w.bed.holds(translate(held.outline, shift)) {
			shift = Point{}

			break
		}
	}

	out := Plate{Positions: make([]Position, len(w.placed))}
	for i, held := range w.placed {
		out.Positions[i] = Position{
			Item: held.item, X: round3(held.at.X + shift.X), Y: round3(held.at.Y + shift.Y), Rotate: held.rotate,
		}
	}

	slices.SortStableFunc(out.Positions, func(a, b Position) int {
		if order := cmp.Compare(a.Item, b.Item); order != 0 {
			return order
		}

		if order := cmp.Compare(a.Y, b.Y); order != 0 {
			return order
		}

		return cmp.Compare(a.X, b.X)
	})

	return out
}

// round3 keeps a thousandth of a millimetre, so the same plate written twice
// has the same numbers.
func round3(value float64) float64 {
	return math.Round(value*1000) / 1000 //nolint:mnd // a thousandth.
}
