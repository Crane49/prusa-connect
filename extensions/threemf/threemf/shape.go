package threemf

import "math"

// piece is a run of a mesh's vertices placed in an object: the vertices lo to
// hi of mesh, moved by tr into the object's coordinates. hi < 0 means every
// vertex. The triangles tfirst to tlast are the ones that use them, and
// tlast < 0 means every triangle.
type piece struct {
	mesh          *mesh
	lo, hi        int
	tfirst, tlast int
	tr            Transform
}

// partSpec is one part of an object as a dialect resolved it.
type partSpec struct {
	part   Part
	pieces []piece
	// id is the id of the object the part's mesh is in: the component's
	// target, or the object itself when it holds the mesh.
	id int
	// settings are the part's own settings, and nil for none.
	settings getter
	// paint is painting the slicer keeps outside the model, added to that
	// of the part's triangles.
	paint paint
}

// runShape is the plane outline and the height range of one vertex run, in the
// mesh's own coordinates.
type runShape struct {
	outline []Point
	box     Box
}

// shape is what one instance covers.
type shape struct {
	// outline and box cover every model part.
	outline []Point
	box     Box
	approx  bool
	// parts are the model parts one by one, in the object's order, for the
	// slicers that judge each part on its own.
	parts []partShape
}

// partShape is one model part of one instance, in project coordinates.
type partShape struct {
	// hull is the convex outline of the part seen from above, and box the box
	// around it.
	hull []Point
	box  Box
	// pieces are the part's vertex runs, each with its transform into the
	// project. A part with no pieces is known only by its box.
	pieces []piece
}

// leaves resolves an object into the mesh runs it is made of, each moved into
// the coordinates of the object at the top. A cycle, or nesting deeper than
// the limit, is refused.
func (m *models) leaves(key resKey, tr Transform, depth int, limits Limits, out []piece) ([]piece, error) {
	if depth > limits.MaxComponentDepth {
		return nil, limitError("MaxComponentDepth", int64(limits.MaxComponentDepth))
	}

	res := m.resource(key)
	if res == nil {
		return out, nil
	}

	if res.mesh != nil {
		if len(out) >= limits.MaxMeshUses {
			return nil, limitError("MaxMeshUses", int64(limits.MaxMeshUses))
		}

		out = append(out, piece{mesh: res.mesh, lo: 0, hi: -1, tlast: -1, tr: tr})
	}

	for _, c := range res.comps {
		var err error

		out, err = m.leaves(c.ref, c.tr.Then(tr), depth+1, limits, out)
		if err != nil {
			return nil, err
		}
	}

	return out, nil
}

// shapeWork is how many vertices the shapes of one read may move one by one,
// as a multiple of MaxVertices. Only a tilted instance moves its vertices one
// by one; past the budget, a tilted instance is drawn from its boxes.
const shapeWork = 16

// shapeOf computes what an instance placed by itemTr covers. budget is the
// number of vertices left to move one by one.
func shapeOf(parts []partSpec, itemTr Transform, budget *int) shape {
	out := shape{box: emptyBox()}

	var points []Point

	for _, part := range parts {
		if part.part.Type != PartModel {
			continue
		}

		var partPoints []Point

		ps := partShape{box: emptyBox(), pieces: make([]piece, 0, len(part.pieces))}

		for _, pc := range part.pieces {
			tr := pc.tr.Then(itemTr)
			pts, box, approx := pc.world(tr, budget)
			*budget -= 1 + len(pts)
			partPoints = append(partPoints, pts...)
			out.box = out.box.union(box)
			ps.box = ps.box.union(box)
			out.approx = out.approx || approx

			placed := pc
			placed.tr = tr
			ps.pieces = append(ps.pieces, placed)
		}

		ps.hull = hull(append([]Point(nil), partPoints...))

		if len(partPoints) > 0 {
			out.parts = append(out.parts, ps)
		}

		points = append(points, partPoints...)
	}

	out.outline = hull(points)

	return out
}

// world returns the plane points that bound the piece once tr places it, and
// its box. The points are the vertices of a convex outline, or every vertex
// when the transform tilts the piece.
func (pc piece) world(tr Transform, budget *int) ([]Point, Box, bool) {
	m := pc.mesh
	lo, hi := pc.lo, pc.hi

	if hi < 0 || hi >= m.nverts {
		hi = m.nverts - 1
	}

	if m.dropped || len(m.verts) == 0 || lo > hi {
		return boxPoints(m.box, tr)
	}

	if tr.upright() {
		run := m.run(lo, hi)
		out := make([]Point, len(run.outline))

		for i, p := range run.outline {
			x, y, _ := tr.Apply(p.X, p.Y, 0)
			out[i] = Point{X: x, Y: y}
		}

		box := emptyBox()

		for _, corner := range run.box.corners() {
			x, y, z := tr.Apply(corner[0], corner[1], corner[2])
			box = box.add(x, y, z)
		}

		// The plane box of the corners would be wider than the outline after a
		// turn, so it is taken from the outline instead.
		lo2, hi2 := bounds2(out)
		box.Min[0], box.Min[1], box.Max[0], box.Max[1] = lo2.X, lo2.Y, hi2.X, hi2.Y

		return out, box, false
	}

	if *budget < hi-lo+1 {
		return boxPoints(m.box, tr)
	}

	*budget -= hi - lo + 1

	box := emptyBox()
	out := make([]Point, 0, hi-lo+1)

	for v := lo; v <= hi; v++ {
		x, y, z := tr.Apply(float64(m.verts[3*v]), float64(m.verts[3*v+1]), float64(m.verts[3*v+2]))
		box = box.add(x, y, z)
		out = append(out, Point{X: x, Y: y})
	}

	return hull(out), box, false
}

// boxShape is what an instance covers when the work budget is spent: the box
// of its object's model parts, placed by itemTr. The box is computed once for
// each object.
func boxShape(spec *objectSpec, itemTr Transform) shape {
	if spec.box == nil {
		local := emptyBox()

		for _, part := range spec.parts {
			if part.part.Type != PartModel {
				continue
			}

			for _, pc := range part.pieces {
				_, box, _ := boxPoints(pc.mesh.box, pc.tr)
				local = local.union(box)
			}
		}

		spec.box = &local
	}

	points, box, _ := boxPoints(*spec.box, itemTr)

	return shape{
		outline: points, box: box, approx: true,
		parts: []partShape{{hull: points, box: box}},
	}
}

// boxPoints stands in for a mesh whose vertices were not kept: the corners of
// its box, placed by tr.
func boxPoints(b Box, tr Transform) ([]Point, Box, bool) {
	box := emptyBox()
	if b.Empty() {
		return nil, box, true
	}

	out := make([]Point, 0, 8) //nolint:mnd // a box has eight corners.

	for _, corner := range b.corners() {
		x, y, z := tr.Apply(corner[0], corner[1], corner[2])
		box = box.add(x, y, z)
		out = append(out, Point{X: x, Y: y})
	}

	return hull(out), box, true
}

// run returns the outline and box of the vertices lo to hi, computing them
// once.
func (m *mesh) run(lo, hi int) *runShape {
	key := [2]int{lo, hi}
	if shape, ok := m.cache[key]; ok {
		return shape
	}

	box := emptyBox()
	points := make([]Point, 0, hi-lo+1)

	for v := lo; v <= hi; v++ {
		x, y, z := float64(m.verts[3*v]), float64(m.verts[3*v+1]), float64(m.verts[3*v+2])
		box = box.add(x, y, z)
		points = append(points, Point{X: x, Y: y})
	}

	shape := &runShape{outline: hull(points), box: box}

	if m.cache == nil {
		m.cache = map[[2]int]*runShape{}
	}

	m.cache[key] = shape

	return shape
}

// state says how an instance's outline and height stand against one bed.
func state(outline []Point, box Box, bed []Point, maxHeight float64) Placement {
	if len(outline) == 0 || len(bed) == 0 {
		return PlacementOutside
	}

	if !convexOverlap(bed, outline) {
		return PlacementOutside
	}

	for _, p := range outline {
		if !insideConvex(bed, p) {
			return PlacementPartlyOutside
		}
	}

	if maxHeight > 0 && !box.Empty() && box.Max[2] > maxHeight+placeEpsilon {
		if box.Min[2] > maxHeight {
			return PlacementOutside
		}

		return PlacementPartlyOutside
	}

	return PlacementInside
}

// bedHull is the convex outline of a bed shape. A bed that is not convex is
// judged by its hull, as PrusaSlicer 2.x judges it.
func bedHull(shape []Point) []Point {
	return hull(append([]Point(nil), shape...))
}

// size is the width and depth of a bed shape.
func size(shape []Point) Point {
	if len(shape) == 0 {
		return Point{}
	}

	lo, hi := bounds2(shape)

	return Point{X: hi.X - lo.X, Y: hi.Y - lo.Y}
}

// hypot is the length of a bed's diagonal.
func diagonal(s Point) float64 { return math.Hypot(s.X, s.Y) }

func (ps *partSpec) getter() getter {
	if ps.settings == nil {
		return none
	}

	return ps.settings
}
