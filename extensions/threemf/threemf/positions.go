package threemf

import (
	"fmt"
	"strings"
)

// The positions of a selection: where Rewrite moves build items, and the
// copies it adds (ADR-20261027-2rq).

// placing is what the positions of a selection do to the build: how far
// each placed item moves, and the copies added after the last item.
type placing struct {
	// moves is where each placed item goes, in project coordinates.
	moves map[int]placedItem
	// copies are the added copies, in the order they are written.
	copies []placedItem
	// shift moves every copy further, for a dialect that moves the chosen
	// bed onto another one, as PrusaSlicer 2.x moves it onto bed 0.
	shift Point
}

// placedItem is one added copy: the item it copies, and how far from that
// item, as the project holds it, the copy stands.
type placedItem struct {
	item int
	move Point
	// turn turns the copy by a right angle about centre, the centre of the
	// item's box in project coordinates, before it moves.
	turn   bool
	centre Point
}

// resolvePositions checks the positions of a selection and works out what
// they do to the build. A selection without positions answers nil.
//
// Each copy must stand on the bed, inside the outline of its bed shape, and
// no two copies may overlap, so a rewritten project slices what Arrange
// planned and nothing the slicer would refuse.
func resolvePositions(p *Project, sel Selection) (*placing, error) {
	if len(sel.Positions) == 0 {
		return nil, nil //nolint:nilnil // no positions is no change.
	}

	if len(sel.Positions) > MaxPositions {
		return nil, fmt.Errorf("%w: %d positions, and a bed holds at most %d",
			ErrSelection, len(sel.Positions), MaxPositions)
	}

	bed := p.Bed(sel.Bed)
	setup := &p.Setups[sel.Bed.Setup]

	shape := bedOutline(setup)
	if len(shape) < minPolygon {
		return nil, fmt.Errorf("%w: setup %d has no bed shape", ErrSelection, sel.Bed.Setup)
	}

	out := &placing{moves: map[int]placedItem{}}

	var outlines [][]Point

	for _, position := range sel.Positions {
		local, placed, err := checkPosition(p, bed, shape, position)
		if err != nil {
			return nil, err
		}

		for _, other := range outlines {
			if convexOverlap(other, local) && !touching(other, local) {
				return nil, fmt.Errorf("%w: a copy of build item %d overlaps another copy", ErrSelection, position.Item)
			}
		}

		outlines = append(outlines, local)

		if _, moved := out.moves[position.Item]; !moved {
			out.moves[position.Item] = placed

			continue
		}

		out.copies = append(out.copies, placed)
	}

	return out, nil
}

// touching reports whether two convex polygons that overlap by the test's
// tolerance only touch along an edge or at a corner.
func touching(a, b []Point) bool {
	return edgeGap(a, b) > -placeEpsilon || edgeGap(b, a) > -placeEpsilon
}

// checkPosition answers the outline of one copy on the bed, in the frame of
// the bed shape, and how far its item moves in project coordinates.
func checkPosition(p *Project, bed *Bed, shape []Point, position Position) ([]Point, placedItem, error) {
	if !finite(position.X) || !finite(position.Y) {
		return nil, placedItem{}, fmt.Errorf("%w: build item %d is placed at no number", ErrSelection, position.Item)
	}

	if position.Rotate != 0 && position.Rotate != RightAngle {
		return nil, placedItem{}, fmt.Errorf("%w: build item %d is turned by %d degrees, and a copy turns by 0 or %d",
			ErrSelection, position.Item, position.Rotate, RightAngle)
	}

	ref, ok := p.Item(position.Item)
	if !ok {
		return nil, placedItem{}, fmt.Errorf("%w: no build item %d", ErrSelection, position.Item)
	}

	inst := p.Instance(ref)
	if inst.Bounds.Empty() {
		return nil, placedItem{}, fmt.Errorf("%w: build item %d has no shape to place", ErrSelection, position.Item)
	}

	centre := boxCentre(inst.Bounds)
	placed := placedItem{
		item:   position.Item,
		move:   Point{X: bed.Offset.X + position.X - centre.X, Y: bed.Offset.Y + position.Y - centre.Y},
		turn:   position.Rotate == RightAngle,
		centre: centre,
	}

	outline, _ := itemOutline(p, position.Item)
	if placed.turn {
		outline = turnPoints(outline)
	}

	local := translate(outline, Point{X: position.X, Y: position.Y})

	for _, point := range local {
		if !insideConvex(shape, point) {
			return nil, placedItem{}, fmt.Errorf(
				"%w: a copy of build item %d stands off the bed",
				ErrSelection,
				position.Item,
			)
		}
	}

	return local, placed, nil
}

// positionedItems is the set of items a selection with positions prints.
func positionedItems(sel Selection) (map[int]bool, error) {
	if sel.Items != nil {
		return nil, fmt.Errorf("%w: a selection names items or positions, not both", ErrSelection)
	}

	out := map[int]bool{}
	for _, position := range sel.Positions {
		out[position.Item] = true
	}

	return out, nil
}

// placeItem moves a build item its first position names, and keeps its tag
// as the template of the item's copies.
func (rw *rewriting) placeItem(item int, t *tag, templates map[int]*tag) {
	if rw.placed == nil {
		return
	}

	placed, moved := rw.placed.moves[item]
	if !moved {
		return
	}

	templates[item] = t.clone()
	t.attrs.placeTransform(placed, Point{})
}

// copiesMarkup writes the added copies, each as a build item made from the
// item it copies, moved to its place and printable. It is written before the
// end tag of the build, whose opening bracket is already out, so the text
// starts inside a tag and ends with the bracket of the end tag.
//
// A copy gets no identifier of its own: the attributes of the 3MF production
// extension that name a build item, p:UUID and p:path among them, are left out,
// so no two items claim one identity.
func (rw *rewriting) copiesMarkup(templates map[int]*tag) string {
	if rw.placed == nil || len(rw.placed.copies) == 0 {
		return ""
	}

	var b strings.Builder

	for _, c := range rw.placed.copies {
		template := templates[c.item]
		if template == nil {
			continue
		}

		copied := template.clone()
		copied.empty = true

		kept := copied.attrs.list[:0]
		for _, attr := range copied.attrs.list {
			if strings.Contains(attr.name, ":") {
				continue
			}

			kept = append(kept, attr)
		}

		copied.attrs.list = kept
		copied.attrs.placeTransform(c, rw.placed.shift)
		copied.attrs.set("printable", boolAttr(true))

		b.WriteString(copied.render())
		b.WriteString("\n  <")
	}

	return b.String()
}

// placeTransform turns the item when the copy asks for it, and moves it to its
// place and further by shift.
func (a *tagAttrs) placeTransform(placed placedItem, shift Point) {
	tr, ok := parseTransform(a.get("transform"))
	if !ok {
		return
	}

	if placed.turn {
		tr = tr.Translated(-placed.centre.X, -placed.centre.Y).Then(quarterTurn).
			Translated(placed.centre.X, placed.centre.Y)
	}

	a.set("transform", tr.Translated(placed.move.X+shift.X, placed.move.Y+shift.Y).String())
}

// quarterTurn turns a point by a right angle counterclockwise about the
// vertical axis through the origin.
//
//nolint:gochecknoglobals // a constant value; Go has no constant arrays.
var quarterTurn = Transform{0, 1, 0, -1, 0, 0, 0, 0, 1, 0, 0, 0}
