package threemf_test

import (
	"encoding/json"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/threemf/threemf"
)

// The golden fixtures in testdata/golden are synthetic projects of plain
// boxes and frustums, written by testdata/golden/gen.py from the probes in
// testdata/golden/cases.py. Each probe is one object, placed to test one rule:
// an edge, a tolerance, a turn, a mirror, a part below the bed, a bed shape.
//
// The command line of a slicer slices one bed and writes only the objects it
// put on that bed and judged whole on it. recorded.json holds what the public
// PrusaSlicer 2.9.6 and 3.0.0-alpha12 command lines wrote, with
// --no-ensure-on-bed so that they keep every object at the height the file
// gives it, as the slicers do when they open a project.
const goldenDir = "testdata/golden"

type goldenFile struct {
	Args     []string `json:"args"`
	Fixtures map[string]struct {
		// Printed are the objects the slicer printed, by name, with the box of
		// each polygon in bed coordinates.
		Printed map[string][2][2]float64 `json:"printed"`
	} `json:"fixtures"`
}

// goldenQuirks are objects the 3.0.0-alpha12 command line prints although its
// own bed test leaves them off the bed. It prints a colliding instance of a
// bed whose shape does not start at 0,0 moved by that corner. The bed test,
// Algorithms::Bed::contains_2d, puts it on no bed, and so does Read.
//
//nolint:gochecknoglobals // a constant table.
var goldenQuirks = map[string]string{
	"ps3_circle/c_corner_out": "printed moved by the bed's corner (-100,-100); off the bed by contains_2d",
}

func readGolden(t *testing.T) goldenFile {
	t.Helper()

	var g goldenFile
	if err := json.Unmarshal(readFile(t, filepath.Join(goldenDir, "recorded.json")), &g); err != nil {
		t.Fatal(err)
	}

	return g
}

// printedByRead is what Read says the command line prints: the printable
// instances whole on the bed it slices, bed 0 for PrusaSlicer 2.x and the
// first bed of the first setup for 3.0, by name, with the box of each outline
// in that bed's coordinates.
func printedByRead(p *threemf.Project) (map[string][2][2]float64, map[string]bool) {
	out := map[string][2][2]float64{}
	sinks := map[string]bool{}

	offset := p.Setups[0].Beds[0].Offset

	for _, object := range p.Objects {
		for _, inst := range object.Instances {
			if inst.Bed == nil || *inst.Bed != (threemf.BedRef{}) || inst.Placement != threemf.PlacementInside ||
				!inst.Printable {
				continue
			}

			lo := [2]float64{math.Inf(1), math.Inf(1)}
			hi := [2]float64{math.Inf(-1), math.Inf(-1)}

			for _, pt := range inst.Outline {
				lo[0], lo[1] = min(lo[0], pt.X-offset.X), min(lo[1], pt.Y-offset.Y)
				hi[0], hi[1] = max(hi[0], pt.X-offset.X), max(hi[1], pt.Y-offset.Y)
			}

			out[object.Name] = [2][2]float64{lo, hi}
			sinks[object.Name] = inst.Bounds.Min[2] < 0
		}
	}

	return out, sinks
}

// TestGoldenBedsAgainstRecordedSlicers reads every golden fixture and checks
// that the instances Read puts whole on the sliced bed are the ones the
// slicer printed, at the place the slicer printed them.
func TestGoldenBedsAgainstRecordedSlicers(t *testing.T) {
	t.Parallel()

	g := readGolden(t)

	for name, fixture := range g.Fixtures {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p := read(t, readFile(t, filepath.Join(goldenDir, name+".3mf")))
			got, sinks := printedByRead(p)
			compareGolden(t, name, got, sinks, fixture.Printed)
		})
	}
}

// compareGolden compares what Read and a slicer say is printed. The outline
// of an instance that reaches below the bed covers the parts below it too; the
// slicer's polygon may leave a part wholly below the bed out, so for such an
// instance the slicer's box need only lie within Read's.
func compareGolden(t *testing.T, fixture string, got map[string][2][2]float64, sinks map[string]bool,
	want map[string][2][2]float64,
) {
	t.Helper()

	names := map[string]bool{}
	for n := range got {
		names[n] = true
	}

	for n := range want {
		names[n] = true
	}

	for _, n := range slices.Sorted(maps.Keys(names)) {
		if reason, quirk := goldenQuirks[fixture+"/"+n]; quirk {
			t.Logf("%s: skipped, %s", n, reason)

			continue
		}

		g, inGot := got[n]
		w, inWant := want[n]

		switch {
		case inGot && !inWant:
			t.Errorf("%s: Read puts it whole on the sliced bed, the slicer did not print it", n)
		case !inGot && inWant:
			t.Errorf("%s: the slicer printed it, Read does not put it whole on the sliced bed", n)
		case inGot && sinks[n] && !boxWithin(w, g):
			t.Errorf("%s: the slicer printed it at %v, outside Read's %v", n, w, g)
		case inGot && !sinks[n] && !boxNear(g, w):
			t.Errorf("%s: Read places it at %v on the bed, the slicer at %v", n, g, w)
		}
	}
}

func boxWithin(inner, outer [2][2]float64) bool {
	return inner[0][0] >= outer[0][0]-1e-3 && inner[0][1] >= outer[0][1]-1e-3 &&
		inner[1][0] <= outer[1][0]+1e-3 && inner[1][1] <= outer[1][1]+1e-3
}

func boxNear(a, b [2][2]float64) bool {
	for i := range 2 {
		for k := range 2 {
			if math.Abs(a[i][k]-b[i][k]) > 1e-3 {
				return false
			}
		}
	}

	return true
}

// TestGoldenBedsWithLiveSlicers slices every golden fixture again with the
// command line that THREEMF_PRUSASLICER2 or THREEMF_PRUSASLICER3 names, and
// checks the result against Read. A slicer whose answers changed since
// recorded.json fails here first.
func TestGoldenBedsWithLiveSlicers(t *testing.T) {
	t.Parallel()

	g := readGolden(t)

	for name := range g.Fixtures {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			env := "THREEMF_PRUSASLICER2"
			if strings.HasPrefix(name, "ps3") {
				env = "THREEMF_PRUSASLICER3"
			}

			binary := os.Getenv(env)
			if binary == "" {
				t.Skipf("%s names no PrusaSlicer binary", env)
			}

			dir := t.TempDir()
			gcode := filepath.Join(dir, name+".gcode")
			args := append([]string{"--datadir", filepath.Join(dir, "datadir")}, g.Args...)
			args = append(args, "--output", gcode, filepath.Join(goldenDir, name+".3mf"))

			// PrusaSlicer 2.9.6 may crash after it wrote the G-code header
			// with objects_info, so the exit code is not checked.
			out, _ := exec.CommandContext(t.Context(), binary, args...).CombinedOutput()

			if _, err := os.Stat(gcode); err != nil {
				t.Fatalf("%s wrote no G-code:\n%s", binary, out)
			}

			printed := map[string][2][2]float64{}

			for _, label := range objectsInfoOf(t, gcode) {
				lo := [2]float64{math.Inf(1), math.Inf(1)}
				hi := [2]float64{math.Inf(-1), math.Inf(-1)}

				for _, pt := range label.Polygon {
					lo[0], lo[1] = min(lo[0], pt.X), min(lo[1], pt.Y)
					hi[0], hi[1] = max(hi[0], pt.X), max(hi[1], pt.Y)
				}

				printed[label.Name] = [2][2]float64{lo, hi}
			}

			p := read(t, readFile(t, filepath.Join(goldenDir, name+".3mf")))
			got, sinks := printedByRead(p)
			compareGolden(t, name, got, sinks, printed)
		})
	}
}
