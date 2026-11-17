package threemf_test

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/prusa3d/threemf/threemf"
)

func readMesh(t *testing.T, data []byte, limits threemf.Limits, object, maxTriangles int) (*threemf.Mesh, error) {
	t.Helper()

	return threemf.ReadMesh(t.Context(), bytes.NewReader(data), int64(len(data)), limits, object, maxTriangles)
}

// meshBounds is the box around a mesh's vertices.
func meshBounds(m *threemf.Mesh) ([3]float64, [3]float64) {
	lo := [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
	hi := [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}

	for i, v := range m.Vertices {
		lo[i%3] = math.Min(lo[i%3], float64(v))
		hi[i%3] = math.Max(hi[i%3], float64(v))
	}

	return lo, hi
}

// twoBlocks is a file of one object of two parts in centimetres: a block
// from 0 to 1 cm and a side from 2 to 3 cm by 2 cm, its first copy moved and
// its second turned.
func twoBlocks(t *testing.T) []byte {
	t.Helper()

	model := fmt.Sprintf(modelHead, "centimeter", "Modeller-1.4") + ` <resources>
  <object id="1" name="Block" type="model">` + box(1, 1, 1) + `</object>
  <object id="2" name="Side" type="model">` + box(1, 2, 1) + `</object>
  <object id="3" name="Assembly" type="model"><components>
   <component objectid="1"/>
   <component objectid="2" transform="1 0 0 0 1 0 0 0 1 2 0 0"/>
  </components></object>
 </resources>
 <build>
  <item objectid="3" transform="1 0 0 0 1 0 0 0 1 5 5 0"/>
  <item objectid="3" transform="0 1 0 -1 0 0 0 0 1 20 5 0"/>
 </build>
</model>`

	return zipOf(t, entry{"3D/3dmodel.model", model})
}

func TestReadMeshStandsTheObjectAtTheOrigin(t *testing.T) {
	t.Parallel()

	m, err := readMesh(t, twoBlocks(t), threemf.Limits{}, 0, 0)
	if err != nil {
		t.Fatalf("ReadMesh: %v", err)
	}

	if len(m.Triangles) != 2*12*3 || m.SourceTriangles != 24 || m.Box {
		t.Fatalf("triangles %d, source %d, box %v", len(m.Triangles)/3, m.SourceTriangles, m.Box)
	}

	if len(m.Parts) != 2 || m.Parts[0].Name != "Block" || m.Parts[1].Name != "Side" ||
		m.Parts[0].Triangles != 12 || m.Parts[1].Triangles != 12 || m.Parts[0].Extruder != 1 {
		t.Fatalf("parts %+v", m.Parts)
	}

	// Centimetres become millimetres, the first copy's move is left out, and
	// the object is centred in x and y with its foot at z 0.
	lo, hi := meshBounds(m)
	if !near(lo[0], -15) || !near(hi[0], 15) || !near(lo[1], -10) || !near(hi[1], 10) ||
		!near(lo[2], 0) || !near(hi[2], 10) {
		t.Errorf("bounds %v %v", lo, hi)
	}

	for _, v := range m.Triangles {
		if int(v) >= len(m.Vertices)/3 {
			t.Fatalf("index %d past %d vertices", v, len(m.Vertices)/3)
		}
	}
}

func TestReadMeshRefusesAnObjectItDoesNotHave(t *testing.T) {
	t.Parallel()

	for _, object := range []int{-1, 1} {
		if _, err := readMesh(t, twoBlocks(t), threemf.Limits{}, object, 0); !errors.Is(err, threemf.ErrNoObject) {
			t.Errorf("object %d: %v, want ErrNoObject", object, err)
		}
	}
}

func TestReadMeshDrawsTheBoxOfAMeshTooLargeToKeep(t *testing.T) {
	t.Parallel()

	m, err := readMesh(t, twoBlocks(t), threemf.Limits{MaxVertices: 1}, 0, 0)
	if err != nil {
		t.Fatalf("ReadMesh: %v", err)
	}

	if !m.Box || len(m.Triangles) != 2*12*3 {
		t.Fatalf("box %v, triangles %d", m.Box, len(m.Triangles)/3)
	}
}

// plate is a file of one object: a wavy square plate of side mm with n by n
// cells, two triangles a cell on top and a flat bottom.
func plate(t *testing.T, n int, side float64) []byte {
	t.Helper()

	var b strings.Builder

	fmt.Fprintf(&b, modelHead, "millimeter", "Modeller-1.4")
	b.WriteString(` <resources><object id="1" name="Plate" type="model"><mesh><vertices>`)

	at := func(i, j int) int { return i*(n+1) + j }

	for i := range n + 1 {
		for j := range n + 1 {
			x, y := side*float64(i)/float64(n), side*float64(j)/float64(n)
			z := 5 + 2*math.Sin(x/7)*math.Cos(y/5)
			fmt.Fprintf(&b, `<vertex x="%g" y="%g" z="%g"/>`, x, y, z)
		}
	}

	bottom := (n + 1) * (n + 1)
	fmt.Fprintf(
		&b,
		`<vertex x="0" y="0" z="0"/><vertex x="%g" y="0" z="0"/><vertex x="%g" y="%g" z="0"/><vertex x="0" y="%g" z="0"/>`,
		side,
		side,
		side,
		side,
	)
	b.WriteString(`</vertices><triangles>`)

	for i := range n {
		for j := range n {
			fmt.Fprintf(&b, `<triangle v1="%d" v2="%d" v3="%d"/><triangle v1="%d" v2="%d" v3="%d"/>`,
				at(i, j), at(i+1, j), at(i+1, j+1), at(i, j), at(i+1, j+1), at(i, j+1))
		}
	}

	fmt.Fprintf(&b, `<triangle v1="%d" v2="%d" v3="%d"/><triangle v1="%d" v2="%d" v3="%d"/>`,
		bottom, bottom+2, bottom+1, bottom, bottom+3, bottom+2)
	b.WriteString(`</triangles></mesh></object></resources>
 <build><item objectid="1" transform="1 0 0 0 1 0 0 0 1 100 100 0"/></build>
</model>`)

	return zipOf(t, entry{"3D/3dmodel.model", b.String()})
}

func TestReadMeshMakesALargeMeshCoarser(t *testing.T) {
	t.Parallel()

	const cells, side = 120, 100.0

	m, err := readMesh(t, plate(t, cells, side), threemf.Limits{}, 0, threemf.MinMeshTriangles)
	if err != nil {
		t.Fatalf("ReadMesh: %v", err)
	}

	got := len(m.Triangles) / 3
	if m.SourceTriangles != 2*cells*cells+2 || got > threemf.MinMeshTriangles || got < threemf.MinMeshTriangles/10 {
		t.Fatalf("source %d, kept %d", m.SourceTriangles, got)
	}

	if m.Parts[0].Triangles != got {
		t.Errorf("part counts %d of %d", m.Parts[0].Triangles, got)
	}

	// Merging moves a vertex by at most one cell of the grid, so the size
	// holds to a few millimetres.
	lo, hi := meshBounds(m)
	if math.Abs(hi[0]-lo[0]-side) > 5 || math.Abs(hi[1]-lo[1]-side) > 5 || !near(lo[2], 0) {
		t.Errorf("bounds %v %v", lo, hi)
	}
}

func TestReadMeshKeepsASmallMeshWhole(t *testing.T) {
	t.Parallel()

	data := readFile(t, fixture296)

	p := read(t, data)
	for o := range p.Objects {
		m, err := readMesh(t, data, threemf.Limits{}, o, 0)
		if err != nil {
			t.Fatalf("object %d: %v", o, err)
		}

		if len(m.Triangles)/3 != m.SourceTriangles || m.SourceTriangles == 0 {
			t.Errorf("object %d: kept %d of %d", o, len(m.Triangles)/3, m.SourceTriangles)
		}

		size := p.Objects[o].Instances[0].Bounds
		lo, hi := meshBounds(m)

		for axis := range 3 {
			if want := size.Max[axis] - size.Min[axis]; math.Abs(hi[axis]-lo[axis]-want) > 0.05 {
				t.Errorf("object %d axis %d: %v, want %v", o, axis, hi[axis]-lo[axis], want)
			}
		}
	}
}
