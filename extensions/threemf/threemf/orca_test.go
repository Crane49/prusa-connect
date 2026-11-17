package threemf_test

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"io"
	"strings"
	"testing"

	"github.com/prusa3d/threemf/threemf"
)

// orcaProject builds a project in the layout OrcaSlicer writes: objects whose
// parts are components in a model file of their own, the settings as JSON,
// and the plates with their instances in model_settings.config. Plate 2
// stands a fifth of the 200 mm bed to the right of plate 1.
func orcaProject(t *testing.T, settings string) []byte {
	t.Helper()

	return orcaProjectWith(t, settings, orcaPlates)
}

// orcaPlates lists Cube on plate 1 and Pin on plate 2, where they stand.
const orcaPlates = `  <plate>
    <metadata key="plater_id" value="1"/>
    <metadata key="plater_name" value="First"/>
    <metadata key="locked" value="true"/>
    <model_instance><metadata key="object_id" value="2"/><metadata key="instance_id" value="0"/></model_instance>
    <model_instance><metadata key="object_id" value="2"/><metadata key="instance_id" value="1"/></model_instance>
  </plate>
  <plate>
    <metadata key="plater_id" value="2"/>
    <metadata key="plater_name" value="Second"/>
    <metadata key="locked" value="false"/>
    <model_instance><metadata key="object_id" value="4"/><metadata key="instance_id" value="0"/></model_instance>
  </plate>
`

// orcaProjectWith builds the project of orcaProject with other plates.
func orcaProjectWith(t *testing.T, settings, plates string) []byte {
	t.Helper()

	return orcaProjectFull(t, settings, orcaObjects, orcaParts(box(10, 10, 10)), plates)
}

// orcaObjects are the objects of model_settings.config: Cube, a body with a
// hole cut in it, and Pin.
const orcaObjects = `  <object id="2">
    <metadata key="name" value="Cube"/>
    <part id="1" subtype="normal_part"><metadata key="name" value="body"/></part>
    <part id="5" subtype="negative_part"><metadata key="name" value="hole"/></part>
  </object>
  <object id="4">
    <metadata key="name" value="Pin"/>
    <part id="3" subtype="normal_part"><metadata key="name" value="pin"/></part>
  </object>
`

// orcaParts is the model file of the parts, with the mesh of Cube's body.
func orcaParts(body string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<model unit="millimeter" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02">
 <resources>
  <object id="1" type="model">` + body + `</object>
  <object id="3" type="model">` + box(20, 20, 5) + `</object>
  <object id="5" type="model">` + box(2, 2, 10) + `</object>
 </resources>
 <build/>
</model>`
}

// orcaProjectFull builds the project of orcaProject with other objects in
// model_settings.config, another parts file, other plates and more entries.
func orcaProjectFull(t *testing.T, settings, objects, parts, plates string, extra ...entry) []byte {
	t.Helper()

	root := `<?xml version="1.0" encoding="UTF-8"?>
<model unit="millimeter" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02"` +
		` xmlns:p="http://schemas.microsoft.com/3dmanufacturing/production/2015/06" requiredextensions="p">
 <metadata name="Application">BambuStudio-2.2.0</metadata>
 <metadata name="BambuStudio:3mfVersion">1</metadata>
 <resources>
  <object id="2" type="model"><components>
   <component p:path="/3D/Objects/parts.model" objectid="1" transform="1 0 0 0 1 0 0 0 1 0 0 0"/>
   <component p:path="/3D/Objects/parts.model" objectid="5" transform="1 0 0 0 1 0 0 0 1 2 2 0"/>
  </components></object>
  <object id="4" type="model"><components>
   <component p:path="/3D/Objects/parts.model" objectid="3" transform="1 0 0 0 1 0 0 0 1 0 0 0"/>
  </components></object>
 </resources>
 <build>
  <item objectid="2" transform="1 0 0 0 1 0 0 0 1 50 50 0" printable="1"/>
  <item objectid="2" transform="1 0 0 0 1 0 0 0 1 100 50 0" printable="0"/>
  <item objectid="4" transform="1 0 0 0 1 0 0 0 1 290 50 0" printable="1"/>
 </build>
</model>`
	modelSettings := `<?xml version="1.0" encoding="UTF-8"?>
<config>
` + objects + plates + `</config>`

	entries := []entry{
		{"3D/3dmodel.model", root},
		{"3D/Objects/parts.model", parts},
		{"Metadata/model_settings.config", modelSettings},
		{"Metadata/project_settings.config", settings},
		{"Metadata/plate_1.png", pngOf(t, 8, 4)},
		{"Metadata/plate_2.png", pngOf(t, 6, 6)},
	}

	return zipOf(t, append(entries, extra...)...)
}

func pngOf(t *testing.T, w, h int) string {
	t.Helper()

	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}

	return buf.String()
}

const orcaSettings = `{
  "printable_area": ["0x0", "200x0", "200x200", "0x200"],
  "printable_height": "250",
  "printer_model": "Generic 200",
  "printer_settings_id": "Generic 200 0.4 nozzle",
  "print_settings_id": "0.20mm Standard",
  "filament_settings_id": ["Generic PLA"],
  "nozzle_diameter": ["0.4"],
  "gcode_flavor": "klipper",
  "printer_technology": "FFF",
  "staggered_inner_seams": "0",
  "wipe_on_loops": "0"
}`

func TestReadOrcaSlicerPlates(t *testing.T) {
	t.Parallel()

	p := read(t, orcaProject(t, orcaSettings))

	want := threemf.Producer{
		Application: "BambuStudio-2.2.0", Name: "OrcaSlicer", Version: "2.2.0", FormatVersion: "1",
		Dialect: threemf.DialectOrcaSlicer,
	}
	if p.Producer != want {
		t.Errorf("producer %+v, want %+v", p.Producer, want)
	}

	if len(p.Issues) != 0 {
		t.Errorf("issues %+v", p.Issues)
	}

	if len(p.Setups) != 1 {
		t.Fatalf("setups %+v", p.Setups)
	}

	s := p.Setups[0]
	if s.PrinterModel != "Generic 200" || s.PrinterProfile != "Generic 200 0.4 nozzle" ||
		s.PrintProfile != "0.20mm Standard" || len(s.MaterialProfiles) != 1 || len(s.Nozzles) != 1 ||
		s.Nozzles[0].DiameterMM != 0.4 || s.MaxPrintHeight != 250 || s.GCodeFlavor != "klipper" || len(s.BedShape) != 4 {
		t.Errorf("setup %+v", s)
	}

	if len(s.Beds) != 2 ||
		s.Beds[0].Name != "First" || !s.Beds[0].Locked || s.Beds[0].Offset != (threemf.Point{}) ||
		s.Beds[1].Name != "Second" || s.Beds[1].Locked || s.Beds[1].Offset != (threemf.Point{X: 240}) ||
		s.Beds[1].Size != (threemf.Point{X: 200, Y: 200}) {
		t.Fatalf("beds %+v", s.Beds)
	}

	if len(p.Objects) != 2 || p.Objects[0].Name != "Cube" || p.Objects[1].Name != "Pin" ||
		len(
			p.Objects[0].Parts,
		) != 2 || !samePart(p.Objects[0].Parts[1], "hole", threemf.PartNegative) {
		t.Fatalf("objects %+v", p.Objects)
	}

	checkInstances(t, p.Objects[0], []instanceWant{
		{
			item:      0,
			printable: true,
			bed:       bed(0, 0),
			placement: threemf.PlacementInside,
			minX:      50,
			minY:      50,
			maxX:      60,
			maxY:      60,
		},
		{item: 1, bed: bed(0, 0), placement: threemf.PlacementInside, minX: 100, minY: 50, maxX: 110, maxY: 60},
	})
	checkInstances(t, p.Objects[1], []instanceWant{
		{
			item:      2,
			printable: true,
			bed:       bed(0, 1),
			placement: threemf.PlacementInside,
			minX:      290,
			minY:      50,
			maxX:      310,
			maxY:      70,
		},
	})

	if len(p.Thumbnails) != 2 || p.Thumbnails[0].Bed == nil || *p.Thumbnails[0].Bed != (threemf.BedRef{}) ||
		p.Thumbnails[1].Bed == nil || *p.Thumbnails[1].Bed != (threemf.BedRef{Bed: 1}) || p.Thumbnails[1].Width != 6 {
		t.Errorf("thumbnails %+v", p.Thumbnails)
	}
}

func TestReadBambuStudioAsUnsupported(t *testing.T) {
	t.Parallel()

	// The same layout without the settings only OrcaSlicer writes.
	p := read(t, orcaProject(t, `{"printable_area": ["0x0", "200x0", "200x200", "0x200"]}`))

	if p.Producer.Name != "BambuStudio" || p.Producer.Dialect != threemf.DialectOrcaSlicer {
		t.Errorf("producer %+v", p.Producer)
	}

	if len(p.Issues) != 1 ||
		p.Issues[0] != (threemf.Issue{Code: threemf.IssueUnsupportedProducer, Detail: "BambuStudio-2.2.0"}) {
		t.Errorf("issues %+v", p.Issues)
	}

	if len(p.Setups[0].Beds) != 2 || len(p.Setups[0].Beds[1].Instances) != 1 {
		t.Errorf("beds %+v", p.Setups[0].Beds)
	}
}

func TestRewriteRefusesOrcaSlicer(t *testing.T) {
	t.Parallel()

	data := orcaProject(t, orcaSettings)

	err := threemf.Rewrite(t.Context(), io.Discard, bytes.NewReader(data), int64(len(data)), read(t, data),
		threemf.Selection{}, threemf.Limits{})
	if !errors.Is(err, threemf.ErrUnsupported) {
		t.Errorf("Rewrite: %v, want ErrUnsupported", err)
	}
}

// TestOrcaPlatesFromGeometry pins PartPlateList::reload_all_objects of
// OrcaSlicer 1.9 to 2.3: on opening a project the instances lists of
// model_settings.config are not used. Every instance goes to the first plate
// whose box its box overlaps. The plates stand ceil(sqrt(n)) to a row, rows
// going towards -Y, a fifth of the plate's whole-millimetre size apart.
func TestOrcaPlatesFromGeometry(t *testing.T) {
	t.Parallel()

	// Every instance listed on plate 1, five plates, a bed 200.6 mm wide.
	plates := `
  <plate><metadata key="plater_id" value="1"/>
    <model_instance><metadata key="object_id" value="2"/><metadata key="instance_id" value="0"/></model_instance>
    <model_instance><metadata key="object_id" value="2"/><metadata key="instance_id" value="1"/></model_instance>
    <model_instance><metadata key="object_id" value="4"/><metadata key="instance_id" value="0"/></model_instance>
  </plate>
  <plate><metadata key="plater_id" value="2"/></plate>
  <plate><metadata key="plater_id" value="3"/></plate>
  <plate><metadata key="plater_id" value="4"/></plate>
  <plate><metadata key="plater_id" value="5"/></plate>
`
	settings := strings.Replace(orcaSettings, `"200x0", "200x200"`, `"200.6x0", "200.6x200"`, 1)
	p := read(t, orcaProjectWith(t, settings, plates))

	beds := p.Setups[0].Beds
	if len(beds) != 5 {
		t.Fatalf("%d beds, want 5", len(beds))
	}

	for i, want := range []threemf.Point{{}, {X: 240}, {X: 480}, {Y: -240}, {X: 240, Y: -240}} {
		if beds[i].Offset != want {
			t.Errorf("plate %d offset %v, want %v", i+1, beds[i].Offset, want)
		}
	}

	if beds[0].Size != (threemf.Point{X: 200.6, Y: 200}) {
		t.Errorf("plate size %v", beds[0].Size)
	}

	pin := p.Objects[1].Instances[0]
	if pin.Bed == nil || *pin.Bed != (threemf.BedRef{Bed: 1}) {
		t.Errorf("Pin, listed on plate 1 and standing on plate 2, is on %v", pin.Bed)
	}
}
