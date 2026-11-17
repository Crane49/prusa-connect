package threemf_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/prusa3d/threemf/threemf"
)

// These tests read files at the edges of the 3MF core specification 1.4 and
// its production extension 1.2, and of the Open Packaging Conventions the
// core builds on.

const rels = `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
 <Relationship Target="%s" Id="rel0" Type="http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"/>
</Relationships>`

// TestSpecUnitInch reads a model in inches. The core specification says the
// unit applies to every vertex, location and measurement of the model, so the
// move of a build item is in inches too.
func TestSpecUnitInch(t *testing.T) {
	t.Parallel()

	model := fmt.Sprintf(modelHead, "inch", "Modeller-1.4") + ` <resources>
  <object id="1" type="model">` + box(1, 1, 1) + `</object>
 </resources>
 <build><item objectid="1" transform="1 0 0 0 1 0 0 0 1 2 0 0"/></build>
</model>`

	p := read(t, zipOf(t, entry{"3D/3dmodel.model", model}))

	if b := p.Objects[0].Instances[0].Bounds; !near(b.Min[0], 50.8) || !near(b.Max[0], 76.2) || !near(b.Max[2], 25.4) {
		t.Errorf("bounds %+v, want 50.8..76.2 mm wide and 25.4 mm high", b)
	}
}

// TestSpecPartNames finds the root model through a relationship whose target
// is a URI, with a %20 escape and other letter case than the zip entry. Part
// names are compared without regard to case.
func TestSpecPartNames(t *testing.T) {
	t.Parallel()

	model := fmt.Sprintf(modelHead, "millimeter", "Modeller-1.4") + ` <resources>
  <object id="1" type="model">` + box(10, 10, 10) + `</object>
 </resources>
 <build><item objectid="1"/></build>
</model>`

	for _, target := range []string{"/3D/my%20model.model", "/3d/MY MODEL.model"} {
		p := read(t, zipOf(t,
			entry{"_rels/.rels", fmt.Sprintf(rels, target)},
			entry{"3D/my model.model", model},
		))

		if len(p.Objects) != 1 {
			t.Errorf("%s: objects %+v", target, p.Objects)
		}
	}
}

// TestSpecRequiredExtensions reads a model that requires an extension this
// package does not read. The core specification says a consumer must not
// process it; Read reads it and says which extension it left out. The
// production and materials extensions are read or do not change a shape.
func TestSpecRequiredExtensions(t *testing.T) {
	t.Parallel()

	model := `<?xml version="1.0" encoding="UTF-8"?>
<model unit="millimeter" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02"
 xmlns:p="http://schemas.microsoft.com/3dmanufacturing/production/2015/06"
 xmlns:m="http://schemas.microsoft.com/3dmanufacturing/material/2015/02"
 xmlns:b="http://schemas.microsoft.com/3dmanufacturing/beamlattice/2017/02"
 requiredextensions="p m b q">
 <resources><object id="1" type="model">` + box(10, 10, 10) + `</object></resources>
 <build><item objectid="1"/></build>
</model>`

	p := read(t, zipOf(t, entry{"3D/3dmodel.model", model}))

	want := []threemf.Issue{
		{
			Code:   threemf.IssueUnsupportedExtension,
			Detail: "http://schemas.microsoft.com/3dmanufacturing/beamlattice/2017/02",
		},
		{Code: threemf.IssueUnsupportedExtension, Detail: "q"},
	}
	if fmt.Sprint(p.Issues) != fmt.Sprint(want) {
		t.Errorf("issues %+v, want %+v", p.Issues, want)
	}
}

// TestSpecPrintableDefault reads build items with and without the printable
// attribute. It is not in any 3MF specification; PrusaSlicer writes it and
// reads a missing one as printable, get_attribute_value_bool in
// src/libslic3r/Format/3mf.cpp of 2.9.6.
func TestSpecPrintableDefault(t *testing.T) {
	t.Parallel()

	model := fmt.Sprintf(modelHead, "millimeter", "Modeller-1.4") + ` <resources>
  <object id="1" type="model">` + box(10, 10, 10) + `</object>
 </resources>
 <build>
  <item objectid="1"/>
  <item objectid="1" printable="0"/>
  <item objectid="1" printable="1"/>
 </build>
</model>`

	p := read(t, zipOf(t, entry{"3D/3dmodel.model", model}))

	got := []bool{}
	for _, inst := range p.Objects[0].Instances {
		got = append(got, inst.Printable)
	}

	if fmt.Sprint(got) != "[true false true]" {
		t.Errorf("printable %v, want [true false true]", got)
	}
}

// TestSpecPathOnlyInRootModel refuses a component with a p:path in a model
// file that is not the root. The production extension: "Any consumer of a 3MF
// package that contains path attributes in components in a non-root model
// file MUST generate an error for that package."
func TestSpecPathOnlyInRootModel(t *testing.T) {
	t.Parallel()

	root := fmt.Sprintf(modelHead, "millimeter", "Modeller-1.4") + ` <resources>
  <object id="2" type="model"><components><component objectid="7" p:path="/3D/a.model"/></components></object>
 </resources>
 <build><item objectid="2"/></build>
</model>`
	child := fmt.Sprintf(modelHead, "millimeter", "Modeller-1.4") + ` <resources>
  <object id="7" type="model"><components><component objectid="8" p:path="/3D/b.model"/></components></object>
 </resources>
 <build/>
</model>`
	leaf := fmt.Sprintf(modelHead, "millimeter", "Modeller-1.4") + ` <resources>
  <object id="8" type="model">` + box(1, 1, 1) + `</object>
 </resources>
 <build/>
</model>`

	_, err := readBytes(t, zipOf(t,
		entry{"3D/3dmodel.model", root},
		entry{"3D/a.model", child},
		entry{"3D/b.model", leaf},
	), threemf.Limits{})
	if !errors.Is(err, threemf.ErrFormat) {
		t.Errorf("Read: %v, want ErrFormat", err)
	}
}
