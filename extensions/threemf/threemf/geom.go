package threemf

import (
	"math"
	"slices"
	"strconv"
	"strings"
)

// Transform is an affine transform in the order the 3MF specification writes
// it: m00 m01 m02 m10 m11 m12 m20 m21 m22 m30 m31 m32. A point (x, y, z)
// goes to (x*m00 + y*m10 + z*m20 + m30, x*m01 + y*m11 + z*m21 + m31,
// x*m02 + y*m12 + z*m22 + m32).
type Transform [12]float64

// Identity is the transform that moves nothing.
//
//nolint:gochecknoglobals // a constant value; Go has no constant arrays.
var Identity = Transform{1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0}

// transformValues is how many numbers a transform attribute holds.
const transformValues = 12

// parseTransform reads a transform attribute. An empty attribute is the
// identity, and anything else that is not twelve finite numbers is refused.
func parseTransform(text string) (Transform, bool) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return Identity, true
	}

	if len(fields) != transformValues {
		return Transform{}, false
	}

	var out Transform

	for i, field := range fields {
		value, err := strconv.ParseFloat(field, 64)
		if err != nil || !finite(value) {
			return Transform{}, false
		}

		out[i] = value
	}

	return out, true
}

// String writes the transform the way a transform attribute holds it.
func (t Transform) String() string {
	parts := make([]string, len(t))
	for i, value := range t {
		parts[i] = strconv.FormatFloat(value, 'g', -1, 64)
	}

	return strings.Join(parts, " ")
}

// Apply moves one point.
func (t Transform) Apply(x, y, z float64) (float64, float64, float64) {
	return x*t[0] + y*t[3] + z*t[6] + t[9],
		x*t[1] + y*t[4] + z*t[7] + t[10],
		x*t[2] + y*t[5] + z*t[8] + t[11]
}

// Then is the transform that applies t first and next after it.
func (t Transform) Then(next Transform) Transform {
	var out Transform

	for row := range 4 {
		for col := range 3 {
			value := t[row*3]*next[col] + t[row*3+1]*next[3+col] + t[row*3+2]*next[6+col]
			if row == 3 { //nolint:mnd // the translation row.
				value += next[9+col]
			}

			out[row*3+col] = value
		}
	}

	return out
}

// Translated is t followed by a move of (dx, dy).
func (t Transform) Translated(dx, dy float64) Transform {
	t[9] += dx
	t[10] += dy

	return t
}

// upright reports whether the transform keeps the vertical axis vertical:
// the position on the plane does not depend on height, and height does not
// depend on the position on the plane. A turn about the vertical axis, a
// scale and a mirror are upright; a tilt is not.
func (t Transform) upright() bool {
	return t[2] == 0 && t[5] == 0 && t[6] == 0 && t[7] == 0
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

// emptyBox is a box that holds nothing, ready to grow.
func emptyBox() Box {
	inf := math.Inf(1)

	return Box{Min: [3]float64{inf, inf, inf}, Max: [3]float64{-inf, -inf, -inf}}
}

// Empty reports whether the box holds no point.
func (b Box) Empty() bool {
	return b.Min[0] > b.Max[0] || b.Min[1] > b.Max[1] || b.Min[2] > b.Max[2]
}

func (b Box) add(x, y, z float64) Box {
	b.Min[0], b.Max[0] = min(b.Min[0], x), max(b.Max[0], x)
	b.Min[1], b.Max[1] = min(b.Min[1], y), max(b.Max[1], y)
	b.Min[2], b.Max[2] = min(b.Min[2], z), max(b.Max[2], z)

	return b
}

func (b Box) union(other Box) Box {
	if other.Empty() {
		return b
	}

	return b.add(other.Min[0], other.Min[1], other.Min[2]).add(other.Max[0], other.Max[1], other.Max[2])
}

// corners are the eight corners of a box that is not empty.
func (b Box) corners() [8][3]float64 {
	var out [8][3]float64

	for i := range out {
		for axis := range 3 {
			if i&(1<<axis) != 0 {
				out[i][axis] = b.Max[axis]
			} else {
				out[i][axis] = b.Min[axis]
			}
		}
	}

	return out
}

// hull is the convex hull of the points, counter-clockwise and without
// repeated or collinear points. It sorts points in place.
func hull(points []Point) []Point {
	slices.SortFunc(points, func(a, b Point) int {
		if a.X != b.X {
			return cmpFloat(a.X, b.X)
		}

		return cmpFloat(a.Y, b.Y)
	})
	points = slices.CompactFunc(points, func(a, b Point) bool { return a == b })

	if len(points) < minPolygon {
		return slices.Clone(points)
	}

	out := make([]Point, 0, len(points)+1)

	for _, p := range points {
		for len(out) >= 2 && cross(out[len(out)-2], out[len(out)-1], p) <= 0 {
			out = out[:len(out)-1]
		}

		out = append(out, p)
	}

	lower := len(out) + 1

	for i := len(points) - 1 - 1; i >= 0; i-- {
		p := points[i]
		for len(out) >= lower && cross(out[len(out)-2], out[len(out)-1], p) <= 0 {
			out = out[:len(out)-1]
		}

		out = append(out, p)
	}

	return slices.Clip(out[:len(out)-1])
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}

	return 0
}

// cross is the z of (a-o) x (b-o): positive when o, a, b turn left.
func cross(o, a, b Point) float64 {
	return (a.X-o.X)*(b.Y-o.Y) - (a.Y-o.Y)*(b.X-o.X)
}

// minPolygon is how many points a polygon needs to have an area.
const minPolygon = 3

// placeEpsilon is how far, in millimetres, a point may stand outside a bed and
// still count as on it. It absorbs the rounding of positions a slicer writes,
// such as 127.99999899999999.
const placeEpsilon = 1e-3

// insideConvex reports whether p is inside the convex counter-clockwise polygon
// poly, or within placeEpsilon of it.
func insideConvex(poly []Point, p Point) bool {
	if len(poly) < minPolygon {
		return false
	}

	for i := range poly {
		a, b := poly[i], poly[(i+1)%len(poly)]
		length := math.Hypot(b.X-a.X, b.Y-a.Y)

		if length == 0 {
			continue
		}

		if cross(a, b, p)/length < -placeEpsilon {
			return false
		}
	}

	return true
}

// convexOverlap reports whether two convex counter-clockwise polygons share
// any area, or touch within placeEpsilon. It is the separating axis test: two
// convex shapes are apart exactly when some edge of one has the other wholly
// on its outer side.
func convexOverlap(a, b []Point) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}

	return !separated(a, b) && !separated(b, a)
}

// separated reports whether some edge of a has all of b outside it.
func separated(a, b []Point) bool {
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
			if cross(p, q, point)/length >= -placeEpsilon {
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

// separatedByBox handles a degenerate a, a point or a segment, by its box.
func separatedByBox(a, b []Point) bool {
	minA, maxA := bounds2(a)
	minB, maxB := bounds2(b)

	return maxA.X+placeEpsilon < minB.X || maxB.X+placeEpsilon < minA.X ||
		maxA.Y+placeEpsilon < minB.Y || maxB.Y+placeEpsilon < minA.Y
}

func bounds2(points []Point) (Point, Point) {
	lo := Point{X: math.Inf(1), Y: math.Inf(1)}
	hi := Point{X: math.Inf(-1), Y: math.Inf(-1)}

	for _, p := range points {
		lo.X, lo.Y = min(lo.X, p.X), min(lo.Y, p.Y)
		hi.X, hi.Y = max(hi.X, p.X), max(hi.Y, p.Y)
	}

	return lo, hi
}

// translate returns the points moved by d.
func translate(points []Point, d Point) []Point {
	out := make([]Point, len(points))
	for i, p := range points {
		out[i] = Point{X: p.X + d.X, Y: p.Y + d.Y}
	}

	return out
}

// centroid is the centre of area of a polygon, or the mean of its points when
// it has no area.
func centroid(poly []Point) (Point, bool) {
	if len(poly) == 0 {
		return Point{}, false
	}

	var area, cx, cy float64

	for i := range poly {
		a, b := poly[i], poly[(i+1)%len(poly)]
		step := a.X*b.Y - b.X*a.Y
		area += step
		cx += (a.X + b.X) * step
		cy += (a.Y + b.Y) * step
	}

	if math.Abs(area) > 1e-12 { //nolint:mnd // below this the polygon has no area.
		return Point{X: cx / (3 * area), Y: cy / (3 * area)}, true //nolint:mnd // the centroid formula.
	}

	var sum Point
	for _, p := range poly {
		sum.X += p.X
		sum.Y += p.Y
	}

	n := float64(len(poly))

	return Point{X: sum.X / n, Y: sum.Y / n}, true
}
