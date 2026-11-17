package threemf_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/prusa3d/threemf/threemf"
)

// entry is one zip entry a test builds.
type entry struct {
	name string
	data string
}

// zipOf builds a zip of the entries, in order. archive/zip writes two entries
// of one name when asked, which is what one test needs.
func zipOf(t testing.TB, entries ...entry) []byte {
	t.Helper()

	var buf bytes.Buffer

	w := zip.NewWriter(&buf)

	for _, e := range entries {
		f, err := w.Create(e.name)
		if err != nil {
			t.Fatalf("create %s: %v", e.name, err)
		}

		if _, err = f.Write([]byte(e.data)); err != nil {
			t.Fatalf("write %s: %v", e.name, err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	return buf.Bytes()
}

const modelHead = `<?xml version="1.0" encoding="UTF-8"?>
<model unit="%s" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02"` +
	` xmlns:p="http://schemas.microsoft.com/3dmanufacturing/production/2015/06">
 <metadata name="Application">%s</metadata>
`

// box is the mesh of a box from the origin to (x, y, z).
func box(x, y, z float64) string {
	var b strings.Builder

	b.WriteString("<mesh><vertices>")

	for i := range 8 {
		px, py, pz := 0.0, 0.0, 0.0
		if i&1 != 0 {
			px = x
		}

		if i&2 != 0 {
			py = y
		}

		if i&4 != 0 {
			pz = z
		}

		fmt.Fprintf(&b, `<vertex x="%g" y="%g" z="%g"/>`, px, py, pz)
	}

	b.WriteString(`</vertices><triangles>`)

	for _, tri := range [][3]int{
		{0, 2, 1}, {1, 2, 3}, {4, 5, 6}, {5, 7, 6}, {0, 1, 4}, {1, 5, 4},
		{2, 6, 3}, {3, 6, 7}, {0, 4, 2}, {2, 4, 6}, {1, 3, 5}, {3, 7, 5},
	} {
		fmt.Fprintf(&b, `<triangle v1="%d" v2="%d" v3="%d"/>`, tri[0], tri[1], tri[2])
	}

	b.WriteString("</triangles></mesh>")

	return b.String()
}

func readBytes(t testing.TB, data []byte, limits threemf.Limits) (*threemf.Project, error) {
	t.Helper()

	return threemf.Read(t.Context(), bytes.NewReader(data), int64(len(data)), limits)
}

func TestReadGenericFile(t *testing.T) {
	t.Parallel()

	// A file from a modelling program, in centimetres, with an object built
	// of two components: one in the root model, one in another model file
	// through the production extension.
	model := fmt.Sprintf(modelHead, "centimeter", "Modeller-1.4") + ` <resources>
  <object id="1" name="Block" type="model">` + box(1, 1, 1) + `</object>
  <object id="2" name="Assembly" type="model"><components>
   <component objectid="1"/>
   <component objectid="7" p:path="/3D/Objects/side.model" transform="1 0 0 0 1 0 0 0 1 2 0 0"/>
  </components></object>
 </resources>
 <build>
  <item objectid="2" transform="1 0 0 0 1 0 0 0 1 5 5 0"/>
  <item objectid="2" transform="0 1 0 -1 0 0 0 0 1 20 5 0"/>
 </build>
</model>`
	side := fmt.Sprintf(modelHead, "centimeter", "Modeller-1.4") + ` <resources>
  <object id="7" name="Side" type="model">` + box(1, 2, 1) + `</object>
 </resources>
 <build/>
</model>`

	p, err := readBytes(t, zipOf(t,
		entry{"3D/3dmodel.model", model},
		entry{"3D/Objects/side.model", side},
	), threemf.Limits{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if p.Producer != (threemf.Producer{Application: "Modeller-1.4", Name: "Modeller", Version: "1.4", Dialect: threemf.DialectGeneric}) {
		t.Errorf("producer %+v", p.Producer)
	}

	if len(p.Setups) != 1 || len(p.Setups[0].Beds) != 1 || len(p.Setups[0].Beds[0].Instances) != 2 {
		t.Fatalf("setups %+v", p.Setups)
	}

	if len(p.Objects) != 1 || p.Objects[0].Name != "Assembly" || len(p.Objects[0].Parts) != 2 ||
		p.Objects[0].Parts[1].Name != "Side" || p.Objects[0].Parts[1].Type != threemf.PartModel ||
		p.Objects[0].Parts[1].Extruder != 1 {
		t.Fatalf("objects %+v", p.Objects)
	}

	first := p.Objects[0].Instances[0]
	if first.Placement != threemf.PlacementUnknown || first.Bed == nil || *first.Bed != (threemf.BedRef{}) {
		t.Errorf("first instance %+v", first)
	}

	// Block spans 0..1 cm and Side 2..3 cm by 0..2 cm, moved by 5 cm.
	if b := first.Bounds; !near(b.Min[0], 50) || !near(b.Max[0], 80) || !near(b.Max[1], 70) || !near(b.Max[2], 10) {
		t.Errorf("bounds %+v", b)
	}

	// The second copy is turned a quarter turn: x becomes y, y becomes -x.
	if b := p.Objects[0].Instances[1].Bounds; !near(b.Min[0], 180) || !near(b.Max[0], 200) || !near(b.Min[1], 50) ||
		!near(b.Max[1], 80) {
		t.Errorf("turned bounds %+v", b)
	}
}

func TestReadApplicationAfterBuild(t *testing.T) {
	t.Parallel()

	// Cura writes the metadata of the model after the build.
	model := `<?xml version="1.0"?>
<model unit="millimeter" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02" xml:lang="en-US">
 <resources><object id="1" type="model">` + box(10, 10, 10) + `</object></resources>
 <build><item objectid="1"/></build>
 <metadata name="Application">Ultimaker Cura 5.2.1</metadata>
</model>`

	p, err := readBytes(t, zipOf(t, entry{"3D/3dmodel.model", model}), threemf.Limits{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	want := threemf.Producer{
		Application: "Ultimaker Cura 5.2.1", Name: "Ultimaker Cura", Version: "5.2.1", Dialect: threemf.DialectGeneric,
	}
	if p.Producer != want {
		t.Errorf("producer %+v, want %+v", p.Producer, want)
	}
}

func TestReadProducerNames(t *testing.T) {
	t.Parallel()

	cases := []struct{ app, name, version string }{
		{"PrusaSlicer-2.9.6+MacOS-arm64", "PrusaSlicer", "2.9.6"},
		{"PrusaSlicer-3.0.0-alpha12+UNKNOWN", "PrusaSlicer", "3.0.0-alpha12"},
		{"Ultimaker Cura 5.2.1", "Ultimaker Cura", "5.2.1"},
		{"Creality_Print V5.1.4.10249 Alpha", "Creality_Print", "5.1.4.10249"},
		{"Autodesk Fusion 360", "Autodesk Fusion 360", ""},
		{"Ultimaker Cura", "Ultimaker Cura", ""},
	}

	for _, tc := range cases {
		model := fmt.Sprintf(modelHead, "millimeter", tc.app) +
			`<resources><object id="1">` + box(1, 1, 1) + `</object></resources><build><item objectid="1"/></build></model>`

		p, err := readBytes(t, zipOf(t, entry{"3D/3dmodel.model", model}), threemf.Limits{})
		if err != nil {
			t.Fatalf("%s: %v", tc.app, err)
		}

		if p.Producer.Name != tc.name || p.Producer.Version != tc.version {
			t.Errorf(
				"%s: name %q version %q, want %q %q",
				tc.app,
				p.Producer.Name,
				p.Producer.Version,
				tc.name,
				tc.version,
			)
		}
	}
}

func TestReadRefusesHostileFiles(t *testing.T) {
	t.Parallel()

	validModel := fmt.Sprintf(modelHead, "millimeter", "X-1") + ` <resources><object id="1">` + box(1, 1, 1) +
		`</object></resources><build><item objectid="1"/></build></model>`

	nested := fmt.Sprintf(
		modelHead,
		"millimeter",
		"X-1",
	) + strings.Repeat(
		"<a>",
		200,
	) + strings.Repeat(
		"</a>",
		200,
	) + "</model>"

	cycle := fmt.Sprintf(modelHead, "millimeter", "X-1") + ` <resources>
  <object id="1"><components><component objectid="2"/></components></object>
  <object id="2"><components><component objectid="1"/></components></object>
 </resources><build><item objectid="1"/></build></model>`

	climbing := fmt.Sprintf(modelHead, "millimeter", "X-1") + ` <resources>
  <object id="1"><components><component objectid="1" p:path="../../../etc/passwd"/></components></object>
 </resources><build><item objectid="1"/></build></model>`

	manyItems := fmt.Sprintf(modelHead, "millimeter", "X-1") + ` <resources><object id="1">` + box(1, 1, 1) +
		`</object></resources><build>` + strings.Repeat(`<item objectid="1"/>`, 5) + `</build></model>`

	cases := []struct {
		name  string
		data  []byte
		lim   threemf.Limits
		want  error
		limit string
	}{
		{name: "not a zip", data: []byte("solid cube\nendsolid\n"), want: threemf.ErrFormat},
		{name: "empty", data: nil, want: threemf.ErrFormat},
		{name: "no model", data: zipOf(t, entry{"readme.txt", "hello"}), want: threemf.ErrFormat},
		{
			name: "too many entries",
			data: zipOf(t, entry{"a", ""}, entry{"b", ""}, entry{"c", ""}, entry{"3D/3dmodel.model", validModel}),
			lim:  threemf.Limits{MaxEntries: 3}, want: threemf.ErrLimit, limit: "MaxEntries",
		},
		{
			name: "two entries of one name",
			data: zipOf(t, entry{"3D/3dmodel.model", validModel}, entry{"3D/3dmodel.model", validModel}),
			want: threemf.ErrFormat,
		},
		{
			name: "model past its limit",
			data: zipOf(t, entry{"3D/3dmodel.model", validModel + strings.Repeat(" ", 4096)}),
			lim:  threemf.Limits{MaxModelBytes: 1024}, want: threemf.ErrLimit, limit: "MaxModelBytes",
		},
		{name: "nested a thousand deep", data: zipOf(t, entry{"3D/3dmodel.model", nested}), want: threemf.ErrFormat},
		{
			name:  "component cycle",
			data:  zipOf(t, entry{"3D/3dmodel.model", cycle}),
			want:  threemf.ErrLimit,
			limit: "MaxComponentDepth",
		},
		{name: "path out of the archive", data: zipOf(t, entry{"3D/3dmodel.model", climbing}), want: threemf.ErrFormat},
		{
			name: "too many items", data: zipOf(t, entry{"3D/3dmodel.model", manyItems}),
			lim: threemf.Limits{MaxItems: 4}, want: threemf.ErrLimit, limit: "MaxItems",
		},
		{
			name: "not XML",
			data: zipOf(t, entry{"3D/3dmodel.model", "<model><resources></model>"}),
			want: threemf.ErrFormat,
		},
		{
			name: "unknown unit",
			data: zipOf(t, entry{"3D/3dmodel.model", strings.Replace(validModel, "millimeter", "furlong", 1)}),
			want: threemf.ErrFormat,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := readBytes(t, tc.data, tc.lim)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Read = %v, want %v", err, tc.want)
			}

			if tc.limit == "" {
				return
			}

			var limit *threemf.LimitError
			if !errors.As(err, &limit) || limit.Limit != tc.limit {
				t.Errorf("limit %v, want %s", err, tc.limit)
			}
		})
	}
}

func TestReadKeepsGoingPastSoftLimits(t *testing.T) {
	t.Parallel()

	data := readFile(t, fixture296)

	// Four vertices are fewer than one box has, so every mesh is drawn from
	// its box, and the picture is over its limit.
	p, err := readBytes(t, data, threemf.Limits{MaxVertices: 4, MaxThumbnailBytes: 10})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	inst := p.Objects[0].Instances[0]
	if !inst.OutlineApprox || inst.Bed == nil || inst.Placement != threemf.PlacementInside {
		t.Errorf("instance %+v", inst)
	}

	codes := map[threemf.IssueCode]bool{}
	for _, issue := range p.Issues {
		codes[issue.Code] = true
	}

	if !codes[threemf.IssueOutlineApprox] || !codes[threemf.IssueThumbnailUnreadable] || len(p.Thumbnails) != 0 {
		t.Errorf("issues %+v, thumbnails %d", p.Issues, len(p.Thumbnails))
	}
}

func TestReadDamagedSettings(t *testing.T) {
	t.Parallel()

	model := `<?xml version="1.0"?><model unit="millimeter" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02">` +
		`<metadata name="Application">PrusaSlicer-3.0.0-alpha12</metadata><resources><object id="1">` + box(
		10,
		10,
		10,
	) +
		`</object></resources><build><item objectid="1"/></build></model>`

	p, err := readBytes(t, zipOf(t,
		entry{"3D/3dmodel.model", model},
		entry{"Metadata/PrusaSlicer3_project.json", `{"objects": [`},
	), threemf.Limits{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if p.Producer.Dialect != threemf.DialectPrusaSlicer3 || len(p.Issues) != 1 ||
		p.Issues[0].Code != threemf.IssueSettingsUnreadable || len(p.Setups) != 1 {
		t.Errorf("project %+v", p)
	}
}
