package threemf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
)

// ErrNoObject is an object index the project does not have.
var ErrNoObject = errors.New("threemf: no such object")

// Mesh is one object of a project as triangles, made light enough to draw.
//
// The object stands as its first instance stands: turned and scaled by that
// instance's transform and not moved by it. It is centred on the origin in x
// and y, and its lowest point is at z 0, so a viewer frames it the same way
// for every object.
type Mesh struct {
	// Vertices are x, y and z in millimetres, three numbers a vertex.
	Vertices []float32 `json:"vertices"`
	// Triangles are indices into the vertices, three a triangle, part after
	// part in the order of Parts.
	Triangles []uint32 `json:"triangles"`
	// Parts are the object's model parts in the order their triangles come.
	Parts []MeshPart `json:"parts"`
	// SourceTriangles is how many triangles the model parts have in the file.
	SourceTriangles int `json:"source_triangles"`
	// Box is true when a mesh was larger than the Limits allow to keep, and
	// its part is drawn as the box around it.
	Box bool `json:"box,omitempty"`
}

// MeshPart is one model part of a Mesh.
type MeshPart struct {
	Name string `json:"name,omitempty"`
	// Extruder is the extruder of the part's outer wall, counted from 1, as
	// Part.Extruder says it.
	Extruder int `json:"extruder,omitempty"`
	// Triangles is how many triangles of Mesh.Triangles belong to the part.
	Triangles int `json:"triangles"`
}

// The bounds of maxTriangles for ReadMesh.
const (
	MinMeshTriangles = 1_000
	MaxMeshTriangles = 200_000
)

// ReadMesh reads the project in r, as Read does, and answers the model parts
// of one object as at most maxTriangles triangles. A mesh with more is made
// coarser by merging the vertices that fall in one cell of a grid, the grid
// growing coarser until the triangles fit. maxTriangles is held between
// MinMeshTriangles and MaxMeshTriangles.
//
// An object the project does not have is ErrNoObject. The errors of Read are
// returned as Read returns them.
func ReadMesh(
	ctx context.Context, r io.ReaderAt, size int64, limits Limits, object int, maxTriangles int,
) (*Mesh, error) {
	project, specs, err := read(ctx, r, size, limits)
	if err != nil {
		return nil, err
	}

	if object < 0 || object >= len(specs) || object >= len(project.Objects) {
		return nil, fmt.Errorf("%w: %d", ErrNoObject, object)
	}

	maxTriangles = min(max(maxTriangles, MinMeshTriangles), MaxMeshTriangles)

	// The first instance's turn and scale, with no move.
	place := Identity
	if instances := project.Objects[object].Instances; len(instances) > 0 {
		place = instances[0].Transform
		place[9], place[10], place[11] = 0, 0, 0
	}

	var b meshBuilder

	spec := specs[object]
	for p, part := range spec.parts {
		if part.part.Type != PartModel {
			continue
		}

		if err = ctx.Err(); err != nil {
			return nil, err
		}

		start := len(b.tris)
		for _, pc := range part.pieces {
			b.addPiece(pc, pc.tr.Then(place))
		}

		extruder := project.Objects[object].Parts[p].Extruder
		b.parts = append(b.parts, meshPartRun{
			part:  MeshPart{Name: part.part.Name, Extruder: extruder},
			first: start / triangleCorners,
			count: (len(b.tris) - start) / triangleCorners,
		})
	}

	return b.finish(maxTriangles), nil
}

// meshPartRun is one part's run of triangles while a mesh is built.
type meshPartRun struct {
	part         MeshPart
	first, count int
}

// meshBuilder gathers the triangles of an object in float64, before they are
// made coarser and rounded.
type meshBuilder struct {
	verts  []float64
	tris   []int
	parts  []meshPartRun
	source int
	box    bool
}

// addPiece adds the triangles of one vertex run, placed by tr. A mesh whose
// triangles were not kept adds the box around it.
func (b *meshBuilder) addPiece(pc piece, tr Transform) {
	m := pc.mesh
	if m.dropped || len(m.tris) == 0 || len(m.verts) == 0 {
		b.addBox(m.box, tr)
		b.box = true
		b.source += m.ntris

		return
	}

	first, last := pc.tfirst, pc.tlast
	if last < 0 || last >= len(m.tris)/triangleCorners {
		last = len(m.tris)/triangleCorners - 1
	}

	first = max(first, 0)
	// The mesh's vertex index to this builder's.
	seen := map[int32]int{}

	for t := first; t <= last; t++ {
		var corner [triangleCorners]int

		ok := true

		for k := range triangleCorners {
			v := m.tris[triangleCorners*t+k]
			if v < 0 || 3*int(v)+2 >= len(m.verts) {
				ok = false

				break
			}

			at, known := seen[v]
			if !known {
				x, y, z := tr.Apply(float64(m.verts[3*v]), float64(m.verts[3*v+1]), float64(m.verts[3*v+2]))
				at = len(b.verts) / axes
				b.verts = append(b.verts, x, y, z)
				seen[v] = at
			}

			corner[k] = at
		}

		if ok {
			b.tris = append(b.tris, corner[0], corner[1], corner[2])
			b.source++
		}
	}
}

// boxFaces are the twelve triangles of a box, by the corners Box.corners
// lists: bit 0 of a corner's index is x, bit 1 y and bit 2 z.
//
//nolint:gochecknoglobals // a constant table.
var boxFaces = [12][3]int{
	{0, 2, 1}, {1, 2, 3}, {4, 5, 6}, {5, 7, 6},
	{0, 1, 4}, {1, 5, 4}, {2, 6, 3}, {3, 6, 7},
	{0, 4, 2}, {2, 4, 6}, {1, 3, 5}, {3, 7, 5},
}

func (b *meshBuilder) addBox(box Box, tr Transform) {
	if box.Empty() {
		return
	}

	base := len(b.verts) / axes

	for _, c := range box.corners() {
		x, y, z := tr.Apply(c[0], c[1], c[2])
		b.verts = append(b.verts, x, y, z)
	}

	for _, face := range boxFaces {
		b.tris = append(b.tris, base+face[0], base+face[1], base+face[2])
	}
}

// axes is how many numbers a vertex has: x, y and z.
const axes = 3

// meshGridStart is the cells along the longest side of the first grid a
// mesh too large is merged on, and meshGridStep how much each next grid
// shrinks that number, in hundredths.
const (
	meshGridStart = 512
	meshGridStep  = 75
	meshGridFloor = 4
	percent       = 100
	// meshRound is how finely a vertex is written: a hundredth of a millimetre.
	meshRound = 100
)

// finish makes the mesh coarser until it fits, centres it and rounds it.
func (b *meshBuilder) finish(maxTriangles int) *Mesh {
	verts, tris, counts := b.verts, b.tris, b.counts()

	for grid := meshGridStart; len(tris)/triangleCorners > maxTriangles && grid >= meshGridFloor; grid = grid * meshGridStep / percent {
		verts, tris, counts = b.cluster(grid)
	}

	out := &Mesh{
		SourceTriangles: b.source,
		Box:             b.box,
		Parts:           make([]MeshPart, len(b.parts)),
		Triangles:       make([]uint32, len(tris)),
		Vertices:        make([]float32, len(verts)),
	}

	for i, run := range b.parts {
		out.Parts[i] = run.part
		out.Parts[i].Triangles = counts[i]
	}

	for i, v := range tris {
		out.Triangles[i] = uint32(v) //nolint:gosec // an index into a slice, never negative.
	}

	lo, hi := bounds3(verts)
	shift := [3]float64{(lo[0] + hi[0]) / 2, (lo[1] + hi[1]) / 2, lo[2]}

	for i, v := range verts {
		out.Vertices[i] = float32(math.Round((v-shift[i%3])*meshRound) / meshRound)
	}

	return out
}

// counts is how many triangles each part has before any merge.
func (b *meshBuilder) counts() []int {
	out := make([]int, len(b.parts))
	for i, run := range b.parts {
		out[i] = run.count
	}

	return out
}

// cluster merges every vertex in one cell of a grid of the given number of
// cells along the mesh's longest side into one, at their average, and drops
// the triangles that lose a corner to the merge.
func (b *meshBuilder) cluster(grid int) ([]float64, []int, []int) {
	lo, hi := bounds3(b.verts)
	side := max(hi[0]-lo[0], hi[1]-lo[1], hi[2]-lo[2])
	cell := side / float64(grid)

	if cell <= 0 {
		return b.verts, b.tris, b.counts()
	}

	type key struct{ x, y, z int32 }

	index := map[key]int{}
	remap := make([]int, len(b.verts)/axes)

	var (
		sums  []float64
		count []int
	)

	for v := range remap {
		k := key{
			x: int32((b.verts[3*v] - lo[0]) / cell),
			y: int32((b.verts[3*v+1] - lo[1]) / cell),
			z: int32((b.verts[3*v+2] - lo[2]) / cell),
		}

		at, known := index[k]
		if !known {
			at = len(count)
			index[k] = at
			sums = append(sums, 0, 0, 0)
			count = append(count, 0)
		}

		sums[3*at] += b.verts[3*v]
		sums[3*at+1] += b.verts[3*v+1]
		sums[3*at+2] += b.verts[3*v+2]
		count[at]++
		remap[v] = at
	}

	for at, n := range count {
		for k := range 3 {
			sums[3*at+k] /= float64(n)
		}
	}

	tris := make([]int, 0, len(b.tris))
	counts := make([]int, len(b.parts))

	for p, run := range b.parts {
		for t := run.first; t < run.first+run.count; t++ {
			a, c, d := remap[b.tris[3*t]], remap[b.tris[3*t+1]], remap[b.tris[3*t+2]]
			if a == c || c == d || a == d {
				continue
			}

			tris = append(tris, a, c, d)
			counts[p]++
		}
	}

	return sums, tris, counts
}

// bounds3 is the box around flat x, y, z triples, or zeros for none.
func bounds3(verts []float64) ([3]float64, [3]float64) {
	if len(verts) < axes {
		return [3]float64{}, [3]float64{}
	}

	lo := [3]float64{verts[0], verts[1], verts[2]}
	hi := lo

	for i := 3; i+2 < len(verts); i += 3 {
		for k := range 3 {
			lo[k] = min(lo[k], verts[i+k])
			hi[k] = max(hi[k], verts[i+k])
		}
	}

	return lo, hi
}
