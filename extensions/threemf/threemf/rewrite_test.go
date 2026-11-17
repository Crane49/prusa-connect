package threemf_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"testing"

	"github.com/prusa3d/threemf/threemf"
)

func rewrite(t *testing.T, data []byte, p *threemf.Project, sel threemf.Selection) []byte {
	t.Helper()

	var out bytes.Buffer
	if err := threemf.Rewrite(
		t.Context(),
		&out,
		bytes.NewReader(data),
		int64(len(data)),
		p,
		sel,
		threemf.Limits{},
	); err != nil {
		t.Fatalf("Rewrite: %v", err)
	}

	return out.Bytes()
}

// printableItems lists the build items that are printable.
func printableItems(p *threemf.Project) []int {
	var out []int

	for _, object := range p.Objects {
		for _, inst := range object.Instances {
			if inst.Printable {
				out = append(out, inst.Item)
			}
		}
	}

	slices.Sort(out)

	return out
}

// rawEntries maps each entry of a zip to its compressed bytes.
func rawEntries(t *testing.T, data []byte) map[string][]byte {
	t.Helper()

	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip: %v", err)
	}

	out := map[string][]byte{}

	for _, f := range r.File {
		body, openErr := f.OpenRaw()
		if openErr != nil {
			t.Fatalf("open %s: %v", f.Name, openErr)
		}

		raw, readErr := io.ReadAll(body)
		if readErr != nil {
			t.Fatalf("read %s: %v", f.Name, readErr)
		}

		out[f.Name] = raw
	}

	return out
}

// checkUntouched checks every entry but the changed ones kept its bytes.
func checkUntouched(t *testing.T, before, after []byte, changed ...string) {
	t.Helper()

	old, cur := rawEntries(t, before), rawEntries(t, after)
	if len(old) != len(cur) {
		t.Fatalf("%d entries, want %d", len(cur), len(old))
	}

	for name, raw := range old {
		if slices.Contains(changed, name) {
			continue
		}

		if !bytes.Equal(raw, cur[name]) {
			t.Errorf("entry %s changed", name)
		}
	}
}

func TestRewritePrusaSlicer2MovesTheChosenBedToBedZero(t *testing.T) {
	t.Parallel()

	data := readFile(t, fixture296)
	p := read(t, data)

	out := rewrite(t, data, p, threemf.Selection{Bed: threemf.BedRef{Bed: 1}})
	checkUntouched(t, data, out, "3D/3dmodel.model", "Metadata/Prusa_Slicer_wipe_tower_information.xml")

	// The wipe tower of bed 0 is the wipe tower of bed 1 now.
	if tower := entryText(
		t,
		out,
		"Metadata/Prusa_Slicer_wipe_tower_information.xml",
	); !bytes.Contains(
		tower,
		[]byte(`bed_idx="1"`),
	) {
		t.Errorf("wipe tower %s", tower)
	}

	q := read(t, out)

	// Bed 1 held CubeA 3 (item 2) and CubeB 2 (item 5). They are now on bed
	// 0, and nothing else is printable.
	if got := printableItems(q); !slices.Equal(got, []int{2, 5}) {
		t.Fatalf("printable items %v, want [2 5]", got)
	}

	for _, item := range []int{2, 5} {
		ref, _ := q.Item(item)
		if inst := q.Instance(ref); inst.Bed == nil || inst.Bed.Bed != 0 || inst.Placement != threemf.PlacementInside {
			t.Errorf("item %d stands on %v, %s", item, inst.Bed, inst.Placement)
		}
	}

	// Bed 0's instances went to where bed 1 was.
	for _, item := range []int{0, 1, 4, 6} {
		ref, _ := q.Item(item)
		if inst := q.Instance(ref); inst.Bed == nil || inst.Bed.Bed != 1 {
			t.Errorf("item %d stands on %v, want bed 1", item, inst.Bed)
		}
	}
}

func TestRewritePrusaSlicer2KeepsBedZeroInPlace(t *testing.T) {
	t.Parallel()

	data := readFile(t, fixture296)
	p := read(t, data)

	out := rewrite(t, data, p, threemf.Selection{Bed: threemf.BedRef{}, Items: []int{6}})
	q := read(t, out)

	if got := printableItems(q); !slices.Equal(got, []int{6}) {
		t.Fatalf("printable items %v, want [6]", got)
	}

	for o, object := range p.Objects {
		for i, inst := range object.Instances {
			if q.Objects[o].Instances[i].Transform != inst.Transform {
				t.Errorf("item %d moved", inst.Item)
			}
		}
	}
}

func TestRewritePrusaSlicer3KeepsOnlyTheChosenBed(t *testing.T) {
	t.Parallel()

	data := readFile(t, fixture3Alpha)
	p := read(t, data)

	cases := []struct {
		name      string
		sel       threemf.Selection
		printable []int
		model     string
		offset    threemf.Point
	}{
		{
			"second bed of the CORE One",
			threemf.Selection{Bed: threemf.BedRef{Bed: 1}},
			[]int{2, 5},
			"COREONE",
			threemf.Point{X: 270},
		},
		{
			"one item of the first bed",
			threemf.Selection{Bed: threemf.BedRef{}, Items: []int{4}},
			[]int{4},
			"COREONE",
			threemf.Point{},
		},
		{"the MINI", threemf.Selection{Bed: threemf.BedRef{Setup: 1}}, []int{3}, "MINI", threemf.Point{Y: 240}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out := rewrite(t, data, p, tc.sel)
			checkUntouched(t, data, out, "Metadata/PrusaSlicer3_project.json")

			q := read(t, out)

			if got := printableItems(q); !slices.Equal(got, tc.printable) {
				t.Errorf("printable items %v, want %v", got, tc.printable)
			}

			if len(q.Setups) != 1 || q.Setups[0].PrinterModel != tc.model || len(q.Setups[0].Beds) != 1 ||
				q.Setups[0].Beds[0].Offset != tc.offset {
				t.Fatalf("setups %+v", q.Setups)
			}

			for _, item := range tc.printable {
				ref, _ := q.Item(item)
				if inst := q.Instance(ref); inst.Bed == nil || *inst.Bed != (threemf.BedRef{}) {
					t.Errorf("item %d stands on %v", item, inst.Bed)
				}
			}
		})
	}
}

func TestRewriteRefusesOtherProducers(t *testing.T) {
	t.Parallel()

	model := fmt.Sprintf(modelHead, "millimeter", "Modeller-1.4") +
		`<resources><object id="1">` + box(10, 10, 10) + `</object></resources><build><item objectid="1"/></build></model>`
	data := zipOf(t, entry{"3D/3dmodel.model", model})
	p := read(t, data)

	err := threemf.Rewrite(t.Context(), io.Discard, bytes.NewReader(data), int64(len(data)), p, threemf.Selection{},
		threemf.Limits{})
	if !errors.Is(err, threemf.ErrUnsupported) {
		t.Errorf("Rewrite of a generic project: %v, want ErrUnsupported", err)
	}
}

func TestRewriteSkipsItemsInComments(t *testing.T) {
	t.Parallel()

	// A PrusaSlicer 2.x project without its settings, so its one bed is not
	// known.
	model := `<?xml version="1.0"?><model unit="millimeter" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02">` +
		`<metadata name="Application">PrusaSlicer-2.9.6</metadata>` +
		`<!-- <item objectid="1"/> in a comment is not an item --><resources><object id="1" name="A">` + box(
		10,
		10,
		10,
	) +
		`</object></resources><build><item objectid="1"/><item objectid="1" transform="1 0 0 0 1 0 0 0 1 20 0 0"></item></build></model>`
	data := zipOf(t, entry{"3D/3dmodel.model", model})
	p := read(t, data)

	out := rewrite(t, data, p, threemf.Selection{Items: []int{1}})
	q := read(t, out)

	if got := printableItems(q); !slices.Equal(got, []int{1}) {
		t.Errorf("printable items %v, want [1]", got)
	}

	body := rawModel(t, out)
	if !bytes.Contains(body, []byte(`<!-- <item objectid="1"/> in a comment is not an item -->`)) ||
		!bytes.Contains(body, []byte(`<item objectid="1" printable="0"/>`)) {
		t.Errorf("model %s", body)
	}
}

func rawModel(t *testing.T, data []byte) []byte {
	t.Helper()

	return entryText(t, data, "3D/3dmodel.model")
}

func entryText(t *testing.T, data []byte, name string) []byte {
	t.Helper()

	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip: %v", err)
	}

	f, err := r.Open(name)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	body, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	return body
}

func TestRewriteRefusesWhatTheProjectCannotDo(t *testing.T) {
	t.Parallel()

	data3 := readFile(t, fixture3Alpha)
	p3 := read(t, data3)
	data2 := readFile(t, fixture296)
	p2 := read(t, data2)
	legacy := read(t, readFile(t, fixture281))

	cases := []struct {
		name    string
		data    []byte
		project *threemf.Project
		sel     threemf.Selection
		want    error
	}{
		{"no such bed", data3, p3, threemf.Selection{Bed: threemf.BedRef{Bed: 2}}, threemf.ErrSelection},
		{"no such setup", data3, p3, threemf.Selection{Bed: threemf.BedRef{Setup: 2}}, threemf.ErrSelection},
		{"an item of another bed", data3, p3, threemf.Selection{Items: []int{2}}, threemf.ErrSelection},
		{"an item on no bed", data3, p3, threemf.Selection{Items: []int{6}}, threemf.ErrSelection},
		{"an empty selection", data2, p2, threemf.Selection{Items: []int{}}, threemf.ErrSelection},
		{"no project", data2, nil, threemf.Selection{}, threemf.ErrSelection},
		{"a project of another file", data2, legacy, threemf.Selection{}, threemf.ErrMismatch},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := threemf.Rewrite(
				t.Context(),
				io.Discard,
				bytes.NewReader(tc.data),
				int64(len(tc.data)),
				tc.project,
				tc.sel,
				threemf.Limits{},
			)
			if !errors.Is(err, tc.want) {
				t.Errorf("Rewrite = %v, want %v", err, tc.want)
			}
		})
	}
}
