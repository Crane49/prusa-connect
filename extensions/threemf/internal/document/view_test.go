package document

import (
	"math"
	"testing"

	"github.com/prusa3d/threemf/threemf"
)

// A detailed hull is sent with at most maxOutlinePoints points, each a point
// of the hull, in the bed's coordinates and rounded.
func TestAnOutlineIsSentWithABoundedNumberOfPoints(t *testing.T) {
	t.Parallel()

	const many = 500

	hull := make([]threemf.Point, 0, many)
	for i := range many {
		angle := 2 * math.Pi * float64(i) / many
		hull = append(hull, threemf.Point{X: 300 + 10*math.Cos(angle), Y: 10*math.Sin(angle) + 1.23456})
	}

	sent := outlineOf(hull, threemf.Point{X: 300})
	if len(sent) != maxOutlinePoints {
		t.Fatalf("%d points sent", len(sent))
	}

	for _, point := range sent {
		if math.Abs(math.Hypot(point.X, point.Y-1.23456)-10) > 0.01 ||
			math.Abs(point.X-math.Round(point.X*100)/100) > 1e-12 {
			t.Fatalf("point %+v", point)
		}
	}

	if got := len(outlineOf(hull[:3], threemf.Point{})); got != 3 {
		t.Errorf("a small outline lost points: %d", got)
	}
}

// A project answers the extruders each object and part prints with, the
// filament colours the reader found, the painting, the layer ranges, and the
// layer changes, print order and G-code of each bed.
func TestAProjectAnswersWhatItPrintsWith(t *testing.T) {
	t.Parallel()

	project := &threemf.Project{
		Producer:  threemf.Producer{Name: "PrusaSlicer", Dialect: threemf.DialectPrusaSlicer2},
		Extruders: []int{1, 2, 3},
		Setups: []threemf.Setup{
			{
				Filaments: []threemf.Filament{
					{Extruder: 1, Color: "#FF0000", Type: "PLA"},
					{Extruder: 2, Color: "#00FF00"},
				},
				Beds: []threemf.Bed{{
					Sequential:   true,
					GCode:        "Metadata/plate_1.gcode",
					LayerChanges: []threemf.LayerChange{{Height: 2, Kind: threemf.LayerChangeTool, Extruder: 3}},
				}},
			},
		},
		Objects: []threemf.Object{{
			Name:      "Frog",
			Extruders: []int{1, 2},
			Painting:  threemf.Painting{Multimaterial: true},
			Parts: []threemf.Part{
				{Name: "body", Type: threemf.PartModel, Extruder: 1, PaintedExtruders: []int{2}},
				{Name: "cut", Type: threemf.PartNegative},
			},
			LayerRanges: []threemf.LayerRange{{MinZ: 1, MaxZ: 3, Extruder: 2}},
		}},
		Issues: []threemf.Issue{{Code: threemf.IssueGCodeOnly}},
	}

	out := Of(project, false)

	same(t, []int{1, 2, 3}, out.Extruders, "extruders")
	same(t, "#FF0000", out.Setups[0].Settings.Filaments[0].Colour, "colour")
	same(t, 2, len(out.Setups[0].Settings.Filaments), "filaments")

	bed := out.Setups[0].Beds[0]
	same(t, true, bed.Sequential, "sequential")
	same(t, "Metadata/plate_1.gcode", bed.GCode, "gcode")
	same(t, "tool_change", bed.LayerChanges[0].Kind, "kind")
	same(t, 3, bed.LayerChanges[0].Extruder, "change extruder")

	object := out.Objects[0]
	same(t, []int{1, 2}, object.Extruders, "object extruders")
	same(t, true, object.Painting.Multimaterial, "painting")
	same(t, []int{2}, object.Parts[0].PaintedExtruders, "painted")
	same(t, "negative_volume", object.Parts[1].Type, "type")
	same(t, 0, object.Parts[1].Extruder, "negative extruder")
	same(t, true, object.Parts[1].Painting == nil, "negative painting")
	same(t, 2, object.LayerRanges[0].Extruder, "range extruder")
	same(t, true, object.LayerRanges[0].Settings != nil, "range settings")
	same(t, "gcode_only", out.Issues[0].Code, "issue")
}
