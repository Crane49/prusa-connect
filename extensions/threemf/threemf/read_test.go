package threemf_test

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/prusa3d/threemf/threemf"
)

// The fixtures are small projects of plain boxes.
//
//   - prusaslicer-2.9.6-multibed.3mf was written by the PrusaSlicer 2.9.6
//     command line (--export-3mf) from a project laid out over three beds of
//     an MK4S. That command line writes no settings, so Metadata/Slic3r_PE.config
//     is what the same build wrote with --save, in the form the 2.9.6 project
//     writer uses, and Metadata/thumbnail.png is a 16x16 picture.
//   - prusaslicer-2.8.1-legacy-row.3mf is laid out by hand in the 2.x format,
//     with Application PrusaSlicer-2.8.1 and the beds in one row as before 2.9.
//     PrusaSlicer 2.9.6 loads and slices it.
//   - prusaslicer-3.0.0-alpha12-two-setups.3mf was written by the PrusaSlicer
//     3.0.0-alpha12 command line from a project with two setups, a CORE One
//     with two beds and a MINI with one. The command line laid the beds out
//     itself on load.
const (
	fixture296    = "testdata/prusaslicer-2.9.6-multibed.3mf"
	fixture281    = "testdata/prusaslicer-2.8.1-legacy-row.3mf"
	fixture3Alpha = "testdata/prusaslicer-3.0.0-alpha12-two-setups.3mf"
)

func readFile(t testing.TB, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return data
}

func read(t *testing.T, data []byte) *threemf.Project {
	t.Helper()

	p, err := threemf.Read(t.Context(), bytes.NewReader(data), int64(len(data)), threemf.Limits{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	return p
}

// instanceWant is what a test expects of one instance.
type instanceWant struct {
	item      int
	printable bool
	bed       *threemf.BedRef
	placement threemf.Placement
	minX      float64
	minY      float64
	maxX      float64
	maxY      float64
}

func bed(setup, b int) *threemf.BedRef { return &threemf.BedRef{Setup: setup, Bed: b} }

func near(a, b float64) bool { return math.Abs(a-b) < 1e-3 }

func checkInstances(t *testing.T, object threemf.Object, want []instanceWant) {
	t.Helper()

	if len(object.Instances) != len(want) {
		t.Fatalf("%s: %d instances, want %d", object.Name, len(object.Instances), len(want))
	}

	for i, w := range want {
		got := object.Instances[i]
		if got.Index != i || got.Item != w.item || got.Printable != w.printable || got.Placement != w.placement {
			t.Errorf(
				"%s instance %d: index %d item %d printable %v placement %s, want item %d printable %v placement %s",
				object.Name,
				i,
				got.Index,
				got.Item,
				got.Printable,
				got.Placement,
				w.item,
				w.printable,
				w.placement,
			)
		}

		if (got.Bed == nil) != (w.bed == nil) || (got.Bed != nil && *got.Bed != *w.bed) {
			t.Errorf("%s instance %d: bed %v, want %v", object.Name, i, got.Bed, w.bed)
		}

		b := got.Bounds
		if !near(b.Min[0], w.minX) || !near(b.Min[1], w.minY) || !near(b.Max[0], w.maxX) || !near(b.Max[1], w.maxY) {
			t.Errorf(
				"%s instance %d: bounds %v, want x %v..%v y %v..%v",
				object.Name,
				i,
				b,
				w.minX,
				w.maxX,
				w.minY,
				w.maxY,
			)
		}

		if len(got.Outline) < 3 {
			t.Errorf("%s instance %d: outline %v", object.Name, i, got.Outline)
		}
	}
}

func TestReadPrusaSlicer296(t *testing.T) {
	t.Parallel()

	p := read(t, readFile(t, fixture296))

	if p.Producer != (threemf.Producer{
		Application: "PrusaSlicer-2.9.6", Name: "PrusaSlicer", Version: "2.9.6",
		FormatVersion: "1", Dialect: threemf.DialectPrusaSlicer2,
	}) {
		t.Errorf("producer %+v", p.Producer)
	}

	if len(p.Setups) != 1 {
		t.Fatalf("%d setups, want 1", len(p.Setups))
	}

	s := p.Setups[0]
	if s.PrinterModel != "MK4S" || s.PrinterProfile != "Fixture MK4S" || s.PrintProfile != "Fixture 0.20mm" ||
		len(s.MaterialProfiles) != 1 || s.MaterialProfiles[0] != "Fixture PLA" || s.MaxPrintHeight != 220 ||
		s.LabelObjects != "firmware" || s.GCodeFlavor != "marlin2" || s.BinaryGCode == nil || *s.BinaryGCode {
		t.Errorf("setup %+v", s)
	}

	if len(s.BedShape) != 4 || s.BedShape[2] != (threemf.Point{X: 250, Y: 210}) {
		t.Errorf("bed shape %v", s.BedShape)
	}

	if len(s.Nozzles) != 1 || s.Nozzles[0].DiameterMM != 0.4 {
		t.Errorf("nozzles %+v", s.Nozzles)
	}

	if len(s.Settings) != 1 || s.Settings[0].Values["bed_shape"] != "0x0,250x0,250x210,0x210" {
		t.Errorf("settings %v", len(s.Settings))
	}

	// The gap of an MK4S bed is 0.3 of its diagonal, 97.95 mm, so the beds
	// stand 347.95 mm apart in X and 307.95 mm apart in Y.
	gap := 0.3 * math.Hypot(250, 210)
	wantOffsets := []threemf.Point{{}, {X: 250 + gap}, {Y: 210 + gap}}

	if len(s.Beds) != len(wantOffsets) {
		t.Fatalf("%d beds, want %d", len(s.Beds), len(wantOffsets))
	}

	for i, w := range wantOffsets {
		if !near(s.Beds[i].Offset.X, w.X) || !near(s.Beds[i].Offset.Y, w.Y) ||
			s.Beds[i].Size != (threemf.Point{X: 250, Y: 210}) {
			t.Errorf("bed %d: offset %v size %v, want offset %v", i, s.Beds[i].Offset, s.Beds[i].Size, w)
		}
	}

	if len(p.Objects) != 3 {
		t.Fatalf("%d objects, want 3", len(p.Objects))
	}

	cubeA, cubeB, bracket := p.Objects[0], p.Objects[1], p.Objects[2]
	if cubeA.Name != "CubeA" || cubeB.Name != "CubeB" || bracket.Name != "Bracket" {
		t.Fatalf("names %q %q %q", cubeA.Name, cubeB.Name, bracket.Name)
	}

	checkInstances(t, cubeA, []instanceWant{
		{0, true, bed(0, 0), threemf.PlacementInside, 50, 50, 60, 60},
		{1, false, bed(0, 0), threemf.PlacementInside, 100, 50, 110, 60},
		{2, true, bed(0, 1), threemf.PlacementInside, 397.94, 50, 407.94, 60},
		{3, true, bed(0, 2), threemf.PlacementInside, 50, 387.94, 60, 397.94},
	})
	// The second CubeB is turned a quarter turn about the vertical axis.
	checkInstances(t, cubeB, []instanceWant{
		{4, true, bed(0, 0), threemf.PlacementInside, 150, 100, 170, 110},
		{5, true, bed(0, 1), threemf.PlacementInside, 470, 100, 480, 120},
	})
	// The outline of the bracket leaves out its modifier, which stands beside
	// the body.
	checkInstances(t, bracket, []instanceWant{
		{6, true, bed(0, 0), threemf.PlacementInside, 100, 150, 130, 160},
	})

	if len(bracket.Parts) != 2 || !samePart(bracket.Parts[0], "Body", threemf.PartModel) ||
		!samePart(bracket.Parts[1], "Denser", threemf.PartModifier) {
		t.Errorf("bracket parts %+v", bracket.Parts)
	}

	if len(p.Thumbnails) != 1 || p.Thumbnails[0].Path != "Metadata/thumbnail.png" ||
		p.Thumbnails[0].MediaType != "image/png" || p.Thumbnails[0].Width != 16 || p.Thumbnails[0].Height != 16 {
		t.Errorf("thumbnails %+v", p.Thumbnails)
	}

	if len(p.Issues) != 0 {
		t.Errorf("issues %+v", p.Issues)
	}
}

func TestReadPrusaSlicer281LegacyRow(t *testing.T) {
	t.Parallel()

	p := read(t, readFile(t, fixture281))

	if p.Producer.Dialect != threemf.DialectPrusaSlicer2 || p.Producer.Version != "2.8.1" {
		t.Errorf("producer %+v", p.Producer)
	}

	// Before 2.9 the beds stood in one row, a fifth of a bed apart: 300 mm
	// for a bed 250 mm wide.
	beds := p.Setups[0].Beds
	if len(beds) != 3 || beds[1].Offset != (threemf.Point{X: 300}) || beds[2].Offset != (threemf.Point{X: 600}) {
		t.Fatalf("beds %+v", beds)
	}

	checkInstances(t, p.Objects[0], []instanceWant{
		{0, true, bed(0, 0), threemf.PlacementInside, 50, 50, 60, 60},
		{1, true, bed(0, 1), threemf.PlacementInside, 350, 50, 360, 60},
	})
	checkInstances(t, p.Objects[1], []instanceWant{
		{2, true, bed(0, 2), threemf.PlacementInside, 650, 60, 670, 70},
	})
}

func TestReadPrusaSlicer2ParkedInstanceKeepsTheGrid(t *testing.T) {
	t.Parallel()

	// A 2.5 project with one cube on the bed and a non-printable plate of
	// parts parked behind the bed, at y 232 to 350. The plate is on no bed
	// of the row, so PrusaSlicer 2.9.6 keeps the grid as it stands, in
	// MultipleBeds::rearrange_after_load. On the grid the plate overlaps bed
	// 2, which stands 210 + 97.9 mm up, and that bed is where 2.9.6 shows it.
	model := fmt.Sprintf(modelHead, "millimeter", "PrusaSlicer-2.5.0+win64") + ` <resources>
  <object id="1" type="model">` + box(10, 10, 10) + `</object>
  <object id="2" type="model">` + box(230, 118, 10) + `</object>
 </resources>
 <build>
  <item objectid="1" transform="1 0 0 0 1 0 0 0 1 50 50 0" printable="1"/>
  <item objectid="2" transform="1 0 0 0 1 0 0 0 1 10 232 0" printable="0"/>
 </build>
</model>`
	config := "; generated by PrusaSlicer 2.5.0+win64 on 2022-11-01 at 10:00:00 UTC\n" +
		"; bed_shape = 0x0,250x0,250x210,0x210\n; max_print_height = 210\n"

	p := read(t, zipOf(t, entry{"3D/3dmodel.model", model}, entry{"Metadata/Slic3r_PE.config", config}))

	beds := p.Setups[0].Beds
	if len(beds) != 3 || beds[2].Offset.Y < 307.9 || beds[2].Offset.Y > 308 || len(beds[1].Instances) != 0 {
		t.Fatalf("beds %+v", beds)
	}

	checkInstances(t, p.Objects[0], []instanceWant{
		{0, true, bed(0, 0), threemf.PlacementInside, 50, 50, 60, 60},
	})
	checkInstances(t, p.Objects[1], []instanceWant{
		{1, false, bed(0, 2), threemf.PlacementPartlyOutside, 10, 232, 240, 350},
	})
}

func TestReadPrusaSlicer3TwoSetups(t *testing.T) {
	t.Parallel()

	p := read(t, readFile(t, fixture3Alpha))

	if p.Producer.Dialect != threemf.DialectPrusaSlicer3 || p.Producer.Version != "3.0.0-alpha12" ||
		p.Producer.FormatVersion != "2" || p.ProjectID == "" {
		t.Errorf("producer %+v, project %q", p.Producer, p.ProjectID)
	}

	if len(p.Setups) != 2 {
		t.Fatalf("%d setups, want 2", len(p.Setups))
	}

	coreOne, mini := p.Setups[0], p.Setups[1]
	if coreOne.PrinterModel != "COREONE" || coreOne.PrinterProfile != "Prusa CORE One & CORE One+" ||
		coreOne.PrintProfile != "0.20mm BALANCED @COREONE 0.4HF" || coreOne.MaxPrintHeight != 270 ||
		coreOne.LabelObjects != "firmware" || coreOne.BinaryGCode == nil || !*coreOne.BinaryGCode ||
		len(coreOne.Nozzles) != 1 || coreOne.Nozzles[0].HighFlow == nil || !*coreOne.Nozzles[0].HighFlow ||
		len(coreOne.Conditions) == 0 {
		t.Errorf("CORE One setup %+v", coreOne)
	}

	if mini.PrinterModel != "MINI" || mini.PrinterProfile != "Original Prusa MINI" ||
		len(mini.BedShape) != 4 || mini.BedShape[2] != (threemf.Point{X: 180, Y: 180}) {
		t.Errorf("MINI setup %+v", mini)
	}

	if len(coreOne.Beds) != 2 || coreOne.Beds[1].Offset != (threemf.Point{X: 270}) ||
		len(
			mini.Beds,
		) != 1 || mini.Beds[0].Offset != (threemf.Point{Y: 240}) || mini.Beds[0].Size != (threemf.Point{X: 180, Y: 180}) {
		t.Fatalf("beds %+v %+v", coreOne.Beds, mini.Beds)
	}

	if len(p.Objects) != 2 || p.Objects[0].Name != "CubeA.stl" || p.Objects[1].Name != "CubeB.stl" {
		t.Fatalf("objects %+v", p.Objects)
	}

	checkInstances(t, p.Objects[0], []instanceWant{
		{0, true, bed(0, 0), threemf.PlacementInside, 50, 50, 60, 60},
		{1, false, bed(0, 0), threemf.PlacementInside, 100, 50, 110, 60},
		{2, true, bed(0, 1), threemf.PlacementInside, 320, 50, 330, 60},
		{3, true, bed(1, 0), threemf.PlacementInside, 50, 290, 60, 300},
	})
	checkInstances(t, p.Objects[1], []instanceWant{
		{4, true, bed(0, 0), threemf.PlacementInside, 150, 100, 170, 110},
		{5, true, bed(0, 1), threemf.PlacementInside, 390, 100, 400, 120},
		{6, true, nil, threemf.PlacementOutside, 1000, 50, 1020, 60},
	})

	groups := map[string]bool{}
	for _, g := range coreOne.Settings {
		groups[g.Name] = true
	}

	if !groups["printer_settings"] || !groups["print_settings"] || !groups["filament_settings"] {
		t.Errorf("settings groups %v", groups)
	}
}

func TestProjectLookups(t *testing.T) {
	t.Parallel()

	p := read(t, readFile(t, fixture296))

	ref, ok := p.Item(5)
	if !ok || ref != (threemf.InstanceRef{Object: 1, Instance: 1}) {
		t.Errorf("Item(5) = %v %v", ref, ok)
	}

	if _, found := p.Item(99); found {
		t.Error("Item(99) found an instance")
	}

	if p.Items() != 7 {
		t.Errorf("Items() = %d", p.Items())
	}

	if p.Bed(threemf.BedRef{Setup: 0, Bed: 3}) != nil || p.Bed(threemf.BedRef{Setup: 1}) != nil {
		t.Error("Bed found a bed that does not exist")
	}

	if p.Instance(threemf.InstanceRef{Object: 3}) != nil {
		t.Error("Instance found an instance that does not exist")
	}
}

func TestThumbnailsReadsOnlyPictures(t *testing.T) {
	t.Parallel()

	data := readFile(t, fixture296)

	thumbs, err := threemf.Thumbnails(t.Context(), bytes.NewReader(data), int64(len(data)), threemf.Limits{})
	if err != nil || len(thumbs) != 1 || thumbs[0].Width != 16 || len(thumbs[0].Data) == 0 {
		t.Fatalf("Thumbnails = %+v, %v", thumbs, err)
	}

	data = readFile(t, fixture3Alpha)

	thumbs, err = threemf.Thumbnails(t.Context(), bytes.NewReader(data), int64(len(data)), threemf.Limits{})
	if err != nil || len(thumbs) != 0 {
		t.Fatalf("Thumbnails of a project without a picture = %+v, %v", thumbs, err)
	}
}

// samePart reports whether a part has the name and type, whatever its
// extruder and painting.
func samePart(p threemf.Part, name string, typ threemf.PartType) bool {
	return p.Name == name && p.Type == typ
}
