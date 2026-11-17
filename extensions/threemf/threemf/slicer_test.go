package threemf_test

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/threemf/threemf"
)

// objectsInfo296Bed0 is what PrusaSlicer 2.9.6 --export-gcode wrote for bed 0
// of the 2.9.6 fixture, copied from the G-code.
const objectsInfo296Bed0 = `{"objects":[` +
	`{"name":"CubeA","polygon":[[60.000,60.000],[50.000,60.000],[50.000,50.000],[60.000,50.000]]},` +
	`{"name":"CubeB","polygon":[[170.000,110.000],[150.000,110.000],[150.000,100.000],[170.000,100.000]]},` +
	`{"name":"Bracket","polygon":[[130.000,160.000],[100.000,160.000],[100.000,150.000],[130.000,150.000]]}]}`

func TestParseObjectsInfo(t *testing.T) {
	t.Parallel()

	labels, err := threemf.ParseObjectsInfo(objectsInfo296Bed0)
	if err != nil {
		t.Fatalf("ParseObjectsInfo: %v", err)
	}

	if len(labels) != 3 || labels[2].ID != 2 || labels[2].Name != "Bracket" || len(labels[2].Polygon) != 4 ||
		labels[2].Polygon[0] != (threemf.Point{X: 130, Y: 160}) {
		t.Errorf("labels %+v", labels)
	}

	labels, err = threemf.ParseObjectsInfo(`{"objects":[{"name":"x","polygon":[[1],[1,2],[3,4,5]]}]}`)
	if err != nil || len(labels[0].Polygon) != 2 {
		t.Errorf("a short point was not left out: %+v %v", labels, err)
	}

	if _, err = threemf.ParseObjectsInfo(`{"objects":`); err == nil {
		t.Error("broken JSON was read")
	}
}

func TestMatchLabels(t *testing.T) {
	t.Parallel()

	p := read(t, readFile(t, fixture296))

	labels, err := threemf.ParseObjectsInfo(objectsInfo296Bed0)
	if err != nil {
		t.Fatal(err)
	}

	got := p.MatchLabels(threemf.BedRef{}, labels)
	want := []threemf.LabelMatch{
		{Label: 0, Instance: threemf.InstanceRef{Object: 0, Instance: 0}, Item: 0},
		{Label: 1, Instance: threemf.InstanceRef{Object: 1, Instance: 0}, Item: 4},
		{Label: 2, Instance: threemf.InstanceRef{Object: 2, Instance: 0}, Item: 6},
	}

	if !slices.Equal(got, want) {
		t.Errorf("MatchLabels = %+v, want %+v", got, want)
	}

	// The same outlines on bed 2 lie on nothing there.
	if other := p.MatchLabels(threemf.BedRef{Bed: 2}, labels); len(other) != 0 {
		t.Errorf("bed 2 matched %+v", other)
	}

	if missing := p.MatchLabels(threemf.BedRef{Bed: 7}, labels); missing != nil {
		t.Errorf("a missing bed matched %+v", missing)
	}
}

func TestMatchLabelsOfInstancesTheProjectDoesNotPrint(t *testing.T) {
	t.Parallel()

	// A project where only CubeB is printed. A reprint that selects all three
	// cubes of bed 1 labels all three, and each label must find its instance.
	data := readFile(t, fixture296)
	p := read(t, rewrite(t, data, read(t, data), threemf.Selection{Items: []int{4}}))

	labels, err := threemf.ParseObjectsInfo(objectsInfo296Bed0)
	if err != nil {
		t.Fatal(err)
	}

	var items []int
	for _, m := range p.MatchLabels(threemf.BedRef{}, labels) {
		items = append(items, m.Item)
	}

	if !slices.Equal(items, []int{0, 4, 6}) {
		t.Errorf("MatchLabels matched items %v, want [0 4 6]", items)
	}
}

// TestRewriteSlicesWithPrusaSlicer slices rewritten projects with the real
// command line, and checks that the G-code holds the selected instances and
// nothing else. It runs when THREEMF_PRUSASLICER2 names a PrusaSlicer 2.9.6
// binary or THREEMF_PRUSASLICER3 names a PrusaSlicer 3.0.0-alpha12 binary,
// such as PrusaSlicer.app/Contents/MacOS/PrusaSlicer.
func TestRewriteSlicesWithPrusaSlicer(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		env     string
		fixture string
		sel     threemf.Selection
	}{
		{"2.9.6 bed 2", "THREEMF_PRUSASLICER2", fixture296, threemf.Selection{Bed: threemf.BedRef{Bed: 1}}},
		{"2.9.6 bed 3", "THREEMF_PRUSASLICER2", fixture296, threemf.Selection{Bed: threemf.BedRef{Bed: 2}}},
		{"2.9.6 two items of bed 1", "THREEMF_PRUSASLICER2", fixture296, threemf.Selection{Items: []int{4, 6}}},
		{"2.8.1 bed 3 of the row", "THREEMF_PRUSASLICER2", fixture281, threemf.Selection{Bed: threemf.BedRef{Bed: 2}}},
		{"3.0 CORE One bed 2", "THREEMF_PRUSASLICER3", fixture3Alpha, threemf.Selection{Bed: threemf.BedRef{Bed: 1}}},
		{"3.0 MINI bed 1", "THREEMF_PRUSASLICER3", fixture3Alpha, threemf.Selection{Bed: threemf.BedRef{Setup: 1}}},
		{"3.0 one item of CORE One bed 1", "THREEMF_PRUSASLICER3", fixture3Alpha, threemf.Selection{Items: []int{4}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			binary := os.Getenv(tc.env)
			if binary == "" {
				t.Skipf("%s names no PrusaSlicer binary", tc.env)
			}

			data := readFile(t, tc.fixture)
			p := read(t, data)
			dir := t.TempDir()
			project := filepath.Join(dir, "reprint.3mf")
			gcode := filepath.Join(dir, "reprint.gcode")

			if err := os.WriteFile(project, rewrite(t, data, p, tc.sel), 0o600); err != nil {
				t.Fatal(err)
			}

			cmd := exec.CommandContext(t.Context(), binary,
				"--datadir", filepath.Join(dir, "datadir"), "--export-gcode", "--output", gcode, project)

			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s: %v\n%s", binary, err, out)
			}

			labels := objectsInfoOf(t, gcode)
			matches := p.MatchLabels(tc.sel.Bed, labels)

			var items []int
			for _, m := range matches {
				items = append(items, m.Item)
			}

			slices.Sort(items)

			want := selectedItems(p, tc.sel)
			if len(labels) != len(want) || !slices.Equal(items, want) {
				t.Errorf("G-code holds %d objects matching items %v, want items %v", len(labels), items, want)
			}
		})
	}
}

func selectedItems(p *threemf.Project, sel threemf.Selection) []int {
	if sel.Items != nil {
		return slices.Sorted(slices.Values(sel.Items))
	}

	var out []int

	for _, ref := range p.Bed(sel.Bed).Instances {
		if inst := p.Instance(ref); inst.Printable && inst.Placement == threemf.PlacementInside {
			out = append(out, inst.Item)
		}
	}

	slices.Sort(out)

	return out
}

// objectsInfoOf reads the objects_info comment of a text G-code file.
func objectsInfoOf(t *testing.T, path string) []threemf.LabelledObject {
	t.Helper()

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("the slicer wrote no G-code: %v", err)
	}

	lines := bufio.NewScanner(bytes.NewReader(data))
	lines.Buffer(nil, 1<<20)

	for lines.Scan() {
		if value, ok := strings.CutPrefix(lines.Text(), "; objects_info = "); ok {
			labels, parseErr := threemf.ParseObjectsInfo(value)
			if parseErr != nil {
				t.Fatalf("objects_info: %v", parseErr)
			}

			return labels
		}
	}

	t.Fatal("the G-code has no objects_info")

	return nil
}
