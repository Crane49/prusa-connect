package threemf

import "math"

// This file judges where an instance stands the way each slicer judges it.
// The slicers differ, and each rule names the source it follows.

// volState is where one part, or one instance, stands against one bed.
type volState int

const (
	stOutside volState = iota
	stInside
	stColliding
	// stBelow is a part wholly below the bed's plane.
	stBelow
)

// judge holds the work budget of the vertex scans of one read. Past the
// budget, a part is judged by its outline and its box alone.
type judge struct {
	work int
}

// eachVertex calls fn with every vertex of the part in project coordinates,
// raised by dz. A piece whose vertices were not kept gives the corners of its
// box, and a part with no pieces the corners of its own box.
func (j *judge) eachVertex(ps *partShape, dz float64, fn func(x, y, z float64)) {
	if len(ps.pieces) == 0 || j.work < 0 {
		eachCorner(ps.box, dz, fn)

		return
	}

	for _, pc := range ps.pieces {
		m := pc.mesh
		lo, hi := pc.lo, pc.hi

		if hi < 0 || hi >= m.nverts {
			hi = m.nverts - 1
		}

		if m.dropped || len(m.verts) == 0 || lo > hi || lo < 0 {
			_, box, _ := boxPoints(m.box, pc.tr)
			eachCorner(box, dz, fn)

			continue
		}

		j.work -= hi - lo + 1

		for v := lo; v <= hi; v++ {
			x, y, z := pc.tr.Apply(float64(m.verts[3*v]), float64(m.verts[3*v+1]), float64(m.verts[3*v+2]))
			fn(x, y, z+dz)
		}
	}
}

func eachCorner(b Box, dz float64, fn func(x, y, z float64)) {
	if b.Empty() {
		return
	}

	for _, c := range b.corners() {
		fn(c[0], c[1], c[2]+dz)
	}
}

// eachCrossing calls fn with every point where an edge of the part's
// triangles crosses the plane at height plane, in project coordinates raised
// by dz. A piece whose triangles were not kept gives no points.
func (j *judge) eachCrossing(ps *partShape, dz, plane float64, fn func(x, y float64)) {
	if j.work < 0 {
		return
	}

	for _, pc := range ps.pieces {
		m := pc.mesh
		if m.dropped || len(m.tris) == 0 {
			continue
		}

		first, last := pc.tfirst, pc.tlast
		if last < 0 || last >= len(m.tris)/triangleCorners {
			last = len(m.tris)/triangleCorners - 1
		}

		j.work -= last - first + 1

		for t := max(first, 0); t <= last; t++ {
			crossTriangle(pc, t, dz, plane, fn)
		}
	}
}

// triangleCorners is how many vertices a triangle has.
const triangleCorners = 3

// crossTriangle calls fn with the points where the edges of triangle t of the
// piece cross the plane.
func crossTriangle(pc piece, t int, dz, plane float64, fn func(x, y float64)) {
	m := pc.mesh

	var p [triangleCorners][3]float64

	for k := range triangleCorners {
		v := int(m.tris[triangleCorners*t+k])
		if v < 0 || 3*v+2 >= len(m.verts) {
			return
		}

		x, y, z := float64(m.verts[3*v]), float64(m.verts[3*v+1]), float64(m.verts[3*v+2])
		p[k][0], p[k][1], p[k][2] = pc.tr.Apply(x, y, z)
		p[k][2] += dz
	}

	prev := triangleCorners - 1
	for k := range triangleCorners {
		a, b := p[prev], p[k]
		if (a[2] < plane && b[2] > plane) || (a[2] > plane && b[2] < plane) {
			f := (plane - a[2]) / (b[2] - a[2])
			fn(a[0]+(b[0]-a[0])*f, a[1]+(b[1]-a[1])*f)
		}

		prev = k
	}
}

// --- PrusaSlicer 2.9 ---

// psEpsilon is EPSILON of PrusaSlicer, src/libslic3r/libslic3r.h, in
// millimetres. BuildVolume::SceneEpsilon is the same value.
const psEpsilon = 1e-4

// ps2SinkingZ is SINKING_Z_THRESHOLD, src/libslic3r/Model.hpp of 2.9.6, as
// the float the slicer compares with.
const ps2SinkingZ = float64(float32(-0.001))

// ps2SinkingMinZ is SINKING_MIN_Z_THRESHOLD of the same file.
const ps2SinkingMinZ = 0.05

// The bed kinds of BuildVolume, src/libslic3r/BuildVolume.cpp of 2.9.6.
const (
	bedRectangle = iota
	bedCircle
	bedConvex
)

// ps2Bed is one bed shape as BuildVolume of PrusaSlicer 2.9 judges it: a
// rectangle by its box, a circle by the circle through its corners, and any
// other shape by its convex hull, each grown by psEpsilon.
type ps2Bed struct {
	kind   int
	lo, hi Point
	center Point
	radius float64
	hull   []Point
	height float64
}

// scaledEpsilon2 is SCALED_EPSILON squared, the area under which PrusaSlicer
// takes two areas as equal, in square millimetres.
const scaledEpsilon2 = 1e-8

const half = 0.5

// The tolerances of the circle test in the BuildVolume constructor: every
// corner within 0.005 mm of the circle, and no edge more than 3 mm inside it.
const (
	ps2CircleCorner = 0.005
	ps2CircleEdge   = 3.0
)

func newPS2Bed(shape []Point, height float64) ps2Bed {
	b := ps2Bed{kind: bedConvex, hull: bedHull(shape), height: height}
	b.lo, b.hi = bounds2(shape)

	area := math.Abs(polygonArea(shape))
	boxArea := (b.hi.X - b.lo.X) * (b.hi.Y - b.lo.Y)

	switch {
	case len(shape) >= 4 && math.Abs(area-boxArea) < scaledEpsilon2:
		b.kind = bedRectangle
	case len(shape) > minPolygon:
		if c, r, ok := fitCircle(shape); ok {
			b.kind, b.center, b.radius = bedCircle, c, r
		}
	}

	return b
}

// fitCircle fits a circle to the corners and reports whether every corner and
// every edge is as close to it as the slicer asks of a round bed.
func fitCircle(shape []Point) (Point, float64, bool) {
	c, r := leastSquaresCircle(shape)
	if r <= 0 {
		return Point{}, 0, false
	}

	prev := shape[len(shape)-1]
	for _, p := range shape {
		mid := Point{X: (prev.X + p.X) * half, Y: (prev.Y + p.Y) * half}
		if math.Abs(math.Hypot(p.X-c.X, p.Y-c.Y)-r) > ps2CircleCorner ||
			r-math.Hypot(mid.X-c.X, mid.Y-c.Y) > ps2CircleEdge {
			return Point{}, 0, false
		}

		prev = p
	}

	return c, r, true
}

// leastSquaresCircle is the algebraic circle fit of the points. For corners
// that lie on a circle it gives that circle, as the slicer's RANSAC fit does.
func leastSquaresCircle(pts []Point) (Point, float64) {
	var mx, my float64
	for _, p := range pts {
		mx += p.X
		my += p.Y
	}

	n := float64(len(pts))
	mx /= n
	my /= n

	var suu, svv, suv, suuu, svvv, suvv, svuu float64

	for _, p := range pts {
		u, v := p.X-mx, p.Y-my
		suu += u * u
		svv += v * v
		suv += u * v
		suuu += u * u * u
		svvv += v * v * v
		suvv += u * v * v
		svuu += v * u * u
	}

	det := suu*svv - suv*suv
	if math.Abs(det) < 1e-12 { //nolint:mnd // collinear points have no circle.
		return Point{}, 0
	}

	bu := (suuu + suvv) / 2 //nolint:mnd // the fit's normal equations.
	bv := (svvv + svuu) / 2 //nolint:mnd // the fit's normal equations.
	uc := (bu*svv - bv*suv) / det
	vc := (bv*suu - bu*suv) / det

	r := math.Sqrt(uc*uc + vc*vc + (suu+svv)/n)

	return Point{X: uc + mx, Y: vc + my}, r
}

func polygonArea(poly []Point) float64 {
	var area float64

	for i := range poly {
		a, b := poly[i], poly[(i+1)%len(poly)]
		area += a.X*b.Y - b.X*a.Y
	}

	return area / 2 //nolint:mnd // the shoelace formula.
}

// inside is the vertex test of BuildVolume::object_state for the bed moved by
// off: the box, circle or hull grown by psEpsilon, and below the highest
// print. A height of 0 means no limit.
func (b *ps2Bed) inside(x, y, z float64, off Point) bool {
	x -= off.X
	y -= off.Y

	switch b.kind {
	case bedRectangle:
		if b.height != 0 && z > b.height+psEpsilon {
			return false
		}

		return x >= b.lo.X-psEpsilon && x <= b.hi.X+psEpsilon && y >= b.lo.Y-psEpsilon && y <= b.hi.Y+psEpsilon
	case bedCircle:
		if b.height != 0 && z >= b.height+psEpsilon {
			return false
		}

		dx, dy, r := x-b.center.X, y-b.center.Y, b.radius+psEpsilon

		return dx*dx+dy*dy <= r*r
	default:
		if b.height != 0 && z >= b.height+psEpsilon {
			return false
		}

		return insideConvexBy(b.hull, Point{X: x, Y: y}, psEpsilon)
	}
}

// below reports whether a part whose highest point is z stays under the top
// of the bed, so that only its plane position matters.
func (b *ps2Bed) under(z float64) bool {
	return b.height == 0 || z < b.height+psEpsilon
}

// box is the plane box the bed covers when moved by off, grown by psEpsilon.
func (b *ps2Bed) box(off Point) (Point, Point) {
	lo, hi := b.lo, b.hi
	if b.kind == bedCircle {
		lo = Point{X: b.center.X - b.radius, Y: b.center.Y - b.radius}
		hi = Point{X: b.center.X + b.radius, Y: b.center.Y + b.radius}
	}

	return Point{X: lo.X + off.X - psEpsilon, Y: lo.Y + off.Y - psEpsilon},
		Point{X: hi.X + off.X + psEpsilon, Y: hi.Y + off.Y + psEpsilon}
}

// ps2PartState is BuildVolume::object_state of PrusaSlicer 2.9.6 for one bed:
// the part's vertices on or above the plane just below the bed, and the
// points where its edges cross that plane, each tested against the bed. Some
// inside is Inside or Colliding; none is Outside; no vertex above the plane is
// Below.
func (j *judge) ps2PartState(ps *partShape, dz float64, bed *ps2Bed, off Point) volState {
	const plane = -psEpsilon

	zlo, zhi := ps.box.Min[2]+dz, ps.box.Max[2]+dz
	if ps.box.Empty() || zhi < plane {
		return stBelow
	}

	if zlo > plane && bed.under(zhi) {
		return j.ps2PlaneState(ps, dz, bed, off)
	}

	var above, in int

	total := 0

	j.eachVertex(ps, dz, func(x, y, z float64) {
		total++

		if z < plane {
			return
		}

		above++

		if bed.inside(x, y, z, off) {
			in++
		}
	})

	if above == 0 {
		return stBelow
	}

	inside, outside := in > 0, in < above

	if above < total && (!inside || !outside) {
		j.eachCrossing(ps, dz, plane, func(x, y float64) {
			if bed.inside(x, y, plane, off) {
				inside = true
			} else {
				outside = true
			}
		})
	}

	return stateOf(inside, outside)
}

// ps2PlaneState judges a part that lies wholly above the plane and below the
// top. Only plane positions matter, and the bed is convex, so the part's hull
// decides unless no corner of the hull is on the bed.
func (j *judge) ps2PlaneState(ps *partShape, dz float64, bed *ps2Bed, off Point) volState {
	in := 0

	for _, p := range ps.hull {
		if bed.inside(p.X, p.Y, 0, off) {
			in++
		}
	}

	switch {
	case in == len(ps.hull) && in > 0:
		return stInside
	case in > 0:
		return stColliding
	}

	lo, hi := bed.box(off)
	plo, phi := bounds2(ps.hull)

	if phi.X < lo.X || plo.X > hi.X || phi.Y < lo.Y || plo.Y > hi.Y {
		return stOutside
	}

	// A vertex inside the hull may still be on the bed.
	found := false

	j.eachVertex(ps, dz, func(x, y, _ float64) {
		if !found && bed.inside(x, y, 0, off) {
			found = true
		}
	})

	if found {
		return stColliding
	}

	return stOutside
}

func stateOf(inside, outside bool) volState {
	switch {
	case inside && outside:
		return stColliding
	case inside:
		return stInside
	}

	return stOutside
}

// ps2Instance finds the bed of an instance and where it stands, as
// ModelObject::update_instances_print_volume_state of PrusaSlicer 2.9.6 does.
// Each model part is judged against the beds in order and takes the first it
// is not wholly outside of. The instance takes the bed of the first part that
// found one. It is inside when every part is inside its own bed, and a part
// wholly below the bed counts for neither.
func (j *judge) ps2Instance(sh *shape, dz float64, bed *ps2Bed, offset func(int) Point, beds int) (int, Placement) {
	found := -1

	var inside, outside bool

	for p := range sh.parts {
		st, at := stOutside, -1

		for b := range beds {
			if s := j.ps2PartState(&sh.parts[p], dz, bed, offset(b)); s != stOutside {
				st, at = s, b

				break
			}
		}

		if found < 0 {
			found = at
		}

		switch st {
		case stInside:
			inside = true
		case stOutside:
			outside = true
		case stColliding:
			inside, outside = true, true
		case stBelow:
		}
	}

	switch {
	case inside && outside:
		return found, PlacementPartlyOutside
	case inside:
		return found, PlacementInside
	}

	return found, PlacementOutside
}

// ps2Lift is how far PrusaSlicer 2.9.6 moves an object up or down when it
// opens a project: ModelObject::ensure_on_bed(true), called from
// Plater::priv::load_files. An object of one model part that does not sink
// into the bed, or lies wholly below it, is put on the bed. An object of more
// parts is moved up only when it is all below 0.05 mm.
func ps2Lift(spec *objectSpec) float64 {
	box := emptyBox()
	for _, sh := range spec.shapes {
		box = box.union(sh.box)
	}

	if box.Empty() {
		return 0
	}

	parts := 0

	for _, part := range spec.parts {
		if part.part.Type == PartModel {
			parts++
		}
	}

	if parts == 1 {
		if box.Min[2] >= ps2SinkingZ || box.Max[2] < 0 {
			return -box.Min[2]
		}

		return 0
	}

	if box.Max[2] < ps2SinkingMinZ {
		return ps2SinkingMinZ - box.Max[2]
	}

	return 0
}

// --- PrusaSlicer 3.0 ---

// ps3Z0 and ps3SinkingZ are the planes PrusaSlicer 3.0.0-alpha12 cuts an
// instance at: its outline is taken above 0, its box above
// SINKING_Z_THRESHOLD, src/slic3r-domain/include/Slic3r/Domain/Model.hpp.
const ps3SinkingZ = float64(float32(-0.001))

// ps3Edge is how far outside a bed a point may be and still be on it. The
// slicer casts a ray at the bed's triangles, which hits a point on an edge.
const ps3Edge = 1e-6

// ps3Footprint is what PrusaSlicer 3.0 judges an instance by:
// ModelObject::convex_hull_2d, the hull of its model parts above the bed's
// plane, and instance_bounding_box cut at SINKING_Z_THRESHOLD. defined is
// false when no part of it is above that plane.
func (j *judge) ps3Footprint(sh *shape) ([]Point, Box, bool) {
	if !sh.box.Empty() && sh.box.Min[2] >= 0 {
		return sh.outline, sh.box, true
	}

	var points []Point

	box := emptyBox()

	for p := range sh.parts {
		ps := &sh.parts[p]
		j.eachVertex(ps, 0, func(x, y, z float64) {
			if z >= 0 {
				points = append(points, Point{X: x, Y: y})
			}

			if z >= ps3SinkingZ {
				box = box.add(x, y, z)
			}
		})
		j.eachCrossing(ps, 0, 0, func(x, y float64) { points = append(points, Point{X: x, Y: y}) })
		j.eachCrossing(ps, 0, ps3SinkingZ, func(x, y float64) { box = box.add(x, y, ps3SinkingZ) })
	}

	return hull(points), box, !box.Empty()
}

// ps3Bed is one bed of PrusaSlicer 3.0: its shape moved by the bed's offset.
type ps3Bed struct {
	poly   []Point
	lo, hi Point
	convex bool
	height float64
}

func newPS3Bed(shape []Point, offset Point, height float64) ps3Bed {
	poly := translate(shape, offset)
	if polygonArea(poly) < 0 {
		poly = reversed(poly)
	}

	b := ps3Bed{poly: poly, height: height}
	b.lo, b.hi = bounds2(poly)
	// Algorithms::Bed::detect_bed_type_from_contour: a shape whose hull has
	// the same area is convex; a rectangle and a circle are convex too.
	b.convex = math.Abs(polygonArea(bedHull(poly))-polygonArea(poly)) < scaledEpsilon2

	return b
}

func reversed(points []Point) []Point {
	out := make([]Point, len(points))
	for i, p := range points {
		out[len(points)-1-i] = p
	}

	return out
}

// ps3State is Algorithms::Bed::contains_3d of PrusaSlicer 3.0.0-alpha12.
func ps3State(bed *ps3Bed, outline []Point, box Box) volState {
	st := ps3Plane(bed, outline, box)
	if st != stInside {
		return st
	}

	switch {
	case box.Max[2] < 0:
		return stBelow
	case box.Min[2] > bed.height:
		return stOutside
	case box.Max[2] > bed.height:
		return stColliding
	}

	return stInside
}

// ps3Plane is Algorithms::Bed::contains_2d: the box first, then the corners
// of the hull, then whether the hull and the bed overlap at all. A bed that is
// not convex is compared with the hull as a polygon.
func ps3Plane(bed *ps3Bed, outline []Point, box Box) volState {
	if box.Max[0] < bed.lo.X || box.Min[0] > bed.hi.X || box.Max[1] < bed.lo.Y || box.Min[1] > bed.hi.Y {
		return stOutside
	}

	if !bed.convex {
		return customState(bed.poly, outline)
	}

	corners := []Point{
		{X: box.Min[0], Y: box.Min[1]}, {X: box.Max[0], Y: box.Min[1]},
		{X: box.Max[0], Y: box.Max[1]}, {X: box.Min[0], Y: box.Max[1]},
	}

	all := true

	for _, c := range corners {
		if !insideConvexBy(bed.poly, c, ps3Edge) {
			all = false

			break
		}
	}

	if all {
		return stInside
	}

	var in, out bool

	for _, p := range outline {
		if insideConvexBy(bed.poly, p, ps3Edge) {
			in = true
		} else {
			out = true
		}

		if in && out {
			return stColliding
		}
	}

	if in {
		return stInside
	}

	if len(outline) > 0 && !separatedBy(bed.poly, outline, 0) && !separatedBy(outline, bed.poly, 0) {
		return stColliding
	}

	return stOutside
}

// customState compares a convex outline with a bed that is not convex, as
// contains_2d_custom does with a polygon difference: the outline wholly in the
// bed is Inside, sharing no area with it is Outside, and anything else is
// Colliding.
func customState(bed, outline []Point) volState {
	if len(outline) < minPolygon {
		for _, p := range outline {
			if insidePolygon(bed, p) {
				return stColliding
			}
		}

		return stOutside
	}

	crossing := edgesCross(bed, outline)

	var in, out int

	for _, p := range outline {
		if insidePolygon(bed, p) || onPolygon(bed, p) {
			in++
		} else {
			out++
		}
	}

	bedInHull := false

	for _, p := range bed {
		if insideConvexBy(outline, p, -ps3Edge) {
			bedInHull = true

			break
		}
	}

	switch {
	case out == 0 && !crossing && !bedInHull:
		return stInside
	case !crossing && !bedInHull && !sharesArea(bed, outline):
		return stOutside
	}

	return stColliding
}

// cornerStep is how far towards the centre a corner is moved to test a point
// just inside the outline, as a share of the way.
const cornerStep = 1e-3

// sharesArea reports whether some point just inside the outline, next to one
// of its corners or at its centre, is inside the bed.
func sharesArea(bed, outline []Point) bool {
	c, _ := centroid(outline)
	if insidePolygon(bed, c) {
		return true
	}

	for _, p := range outline {
		q := Point{X: p.X + (c.X-p.X)*cornerStep, Y: p.Y + (c.Y-p.Y)*cornerStep}
		if insidePolygon(bed, q) {
			return true
		}
	}

	return false
}

// insidePolygon is the even-odd test: whether p is strictly inside poly.
func insidePolygon(poly []Point, p Point) bool {
	in := false

	for i := range poly {
		a, b := poly[i], poly[(i+1)%len(poly)]
		if (a.Y > p.Y) != (b.Y > p.Y) && p.X < a.X+(p.Y-a.Y)*(b.X-a.X)/(b.Y-a.Y) {
			in = !in
		}
	}

	return in && !onPolygon(poly, p)
}

// onPolygon reports whether p lies on an edge of poly.
func onPolygon(poly []Point, p Point) bool {
	for i := range poly {
		a, b := poly[i], poly[(i+1)%len(poly)]

		length := math.Hypot(b.X-a.X, b.Y-a.Y)
		if length == 0 {
			continue
		}

		if math.Abs(cross(a, b, p))/length <= ps3Edge &&
			p.X >= min(a.X, b.X)-ps3Edge && p.X <= max(a.X, b.X)+ps3Edge &&
			p.Y >= min(a.Y, b.Y)-ps3Edge && p.Y <= max(a.Y, b.Y)+ps3Edge {
			return true
		}
	}

	return false
}

// edgesCross reports whether an edge of a crosses an edge of b at a point
// inside both edges.
func edgesCross(a, b []Point) bool {
	for i := range a {
		p, q := a[i], a[(i+1)%len(a)]

		for k := range b {
			r, s := b[k], b[(k+1)%len(b)]

			d1, d2 := cross(p, q, r), cross(p, q, s)
			d3, d4 := cross(r, s, p), cross(r, s, q)

			if ((d1 > 0 && d2 < 0) || (d1 < 0 && d2 > 0)) && ((d3 > 0 && d4 < 0) || (d3 < 0 && d4 > 0)) {
				return true
			}
		}
	}

	return false
}

// insideConvexBy reports whether p is inside the convex counter-clockwise
// polygon, or within eps of it. A negative eps asks for p strictly inside, by
// at least -eps.
func insideConvexBy(poly []Point, p Point, eps float64) bool {
	if len(poly) < minPolygon {
		return false
	}

	for i := range poly {
		a, b := poly[i], poly[(i+1)%len(poly)]

		length := math.Hypot(b.X-a.X, b.Y-a.Y)
		if length == 0 {
			continue
		}

		if cross(a, b, p)/length < -eps {
			return false
		}
	}

	return true
}

// separatedBy reports whether some edge of a has every point of b more than
// eps outside it.
func separatedBy(a, b []Point, eps float64) bool {
	if len(a) < minPolygon {
		return separatedByBox(a, b)
	}

	for i := range a {
		p, q := a[i], a[(i+1)%len(a)]

		length := math.Hypot(q.X-p.X, q.Y-p.Y)
		if length == 0 {
			continue
		}

		outside := true

		for _, point := range b {
			if cross(p, q, point)/length >= -eps {
				outside = false

				break
			}
		}

		if outside {
			return true
		}
	}

	return false
}

// --- OrcaSlicer ---

// orcaOnPlate is PartPlate::intersect_instance of OrcaSlicer 1.9 to 2.3: the
// box of the instance's model parts overlaps, by more than a touch, the box of
// the plate's shape grown by BuildVolume::SceneEpsilon, from the plate up to
// the printable height.
func orcaOnPlate(lo, hi Point, height float64, box Box) bool {
	minZ, maxZ := -psEpsilon, height+psEpsilon

	return lo.X-psEpsilon < box.Max[0] && hi.X+psEpsilon > box.Min[0] &&
		lo.Y-psEpsilon < box.Max[1] && hi.Y+psEpsilon > box.Min[1] &&
		minZ < box.Max[2] && maxZ > box.Min[2]
}
