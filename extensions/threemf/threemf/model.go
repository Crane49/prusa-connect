package threemf

// Dialect names the reader that understood the slicer's own files in a
// project. The core 3MF content is read the same way for every dialect.
type Dialect string

// The dialects this package has.
const (
	// DialectGeneric reads a file from a program this package has no dialect
	// for. It reads the 3MF core only and puts every build item on one bed.
	DialectGeneric Dialect = "generic"
	// DialectPrusaSlicer2 reads PrusaSlicer 2.x projects, and the Slic3r PE
	// projects before them. Settings are in Metadata/Slic3r_PE.config, object
	// names in Metadata/Slic3r_PE_model.config, and the beds are not stored:
	// they follow from where each instance stands.
	DialectPrusaSlicer2 Dialect = "prusaslicer2"
	// DialectPrusaSlicer3 reads PrusaSlicer 3.0 projects. Settings, printer
	// setups and bed positions are in Metadata/PrusaSlicer3_project.json.
	DialectPrusaSlicer3 Dialect = "prusaslicer3"
	// DialectOrcaSlicer reads OrcaSlicer projects. Settings are in
	// Metadata/project_settings.config, and the plates, with the instances on
	// each, in Metadata/model_settings.config. Every plate is a bed. A
	// BambuStudio project has the same layout and is read by this dialect too,
	// with IssueUnsupportedProducer.
	DialectOrcaSlicer Dialect = "orcaslicer"
)

// Producer is the program that wrote the project.
type Producer struct {
	// Application is the Application entry of 3D/3dmodel.model as written,
	// such as "PrusaSlicer-2.9.6". It is empty when the file has none.
	Application string `json:"application,omitempty"`
	// Name is the program's name, such as "PrusaSlicer".
	Name string `json:"name,omitempty"`
	// Version is the program's version, such as "2.9.6" or "3.0.0-alpha12".
	Version string `json:"version,omitempty"`
	// FormatVersion is the version of the slicer's own file layout: the
	// slic3rpe:Version3mf entry for PrusaSlicer 2.x, the project version for
	// PrusaSlicer 3.0, and the BambuStudio:3mfVersion entry for OrcaSlicer.
	FormatVersion string `json:"format_version,omitempty"`
	// Dialect is the reader that read the slicer's own files.
	Dialect Dialect `json:"dialect"`
}

// Metadata is one metadata entry of 3D/3dmodel.model, such as Title.
type Metadata struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Point is a position on the bed plane, in millimetres.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Box is an axis-aligned box in millimetres. A Box whose Min is above its
// Max on any axis is empty.
type Box struct {
	Min [3]float64 `json:"min"`
	Max [3]float64 `json:"max"`
}

// Project is everything Read found in one 3MF file.
type Project struct {
	// Producer is the program that wrote the file.
	Producer Producer `json:"producer"`
	// ProjectID is the identifier PrusaSlicer 3.0 gives a project. Other
	// files have none.
	ProjectID string `json:"project_id,omitempty"`
	// Metadata are the metadata entries of 3D/3dmodel.model, in file order.
	Metadata []Metadata `json:"metadata,omitempty"`
	// Setups are the printer setups. Every project has at least one. Only a
	// PrusaSlicer 3.0 project can have more.
	Setups []Setup `json:"setups"`
	// Objects are the objects, in the slicer's order.
	Objects []Object `json:"objects"`
	// Extruders are the extruders the project prints with, counted from 1
	// and in ascending order: those of every object, and those a tool change
	// on any bed switches to. A printer with one extruder, or a file whose
	// printer is not known, has the extruders the file names.
	Extruders []int `json:"extruders,omitempty"`
	// Thumbnails are the pictures the file carries, best first.
	Thumbnails []Thumbnail `json:"thumbnails,omitempty"`
	// Issues are the things Read could not read in full. None of them stopped
	// the read.
	Issues []Issue `json:"issues,omitempty"`
}

// Setup is one printer setup: a printer profile, the print and material
// profiles chosen for it, and the beds that use them. A PrusaSlicer 2.x
// project has one. A PrusaSlicer 3.0 project has one for every printer it was
// laid out for, with the beds of each setup side by side.
type Setup struct {
	// Index is the setup's position in Project.Setups.
	Index int `json:"index"`
	// PrinterModel is the printer the profile is for, such as MK4S or COREONE.
	PrinterModel string `json:"printer_model,omitempty"`
	// PrinterProfile, PrintProfile and MaterialProfiles are the names of the
	// slicer profiles, with one material profile per filament.
	PrinterProfile   string   `json:"printer_profile,omitempty"`
	PrintProfile     string   `json:"print_profile,omitempty"`
	MaterialProfiles []string `json:"material_profiles,omitempty"`
	// Nozzles are the nozzles of the printer profile, one per tool. A printer
	// that feeds several filaments into one hot end, such as one with an MMU,
	// has one.
	Nozzles []Nozzle `json:"nozzles,omitempty"`
	// BedShape is the outline of one bed, in the bed's own coordinates. It is
	// empty when the file does not say.
	BedShape []Point `json:"bed_shape,omitempty"`
	// MaxPrintHeight is the highest a print may reach, in millimetres, and 0
	// when the file does not say.
	MaxPrintHeight float64 `json:"max_print_height,omitempty"`
	// Technology is FFF or SLA.
	Technology string `json:"technology,omitempty"`
	// GCodeFlavor is the G-code dialect the profile emits, such as marlin2.
	GCodeFlavor string `json:"gcode_flavor,omitempty"`
	// LabelObjects is how the slicer marks objects in G-code: firmware,
	// octoprint or disabled. Only the firmware style lets a printer cancel
	// one object.
	LabelObjects string `json:"label_objects,omitempty"`
	// BinaryGCode says whether the profile exports binary G-code. It is nil
	// when the file does not say.
	BinaryGCode *bool `json:"binary_gcode,omitempty"`
	// Filaments are the filaments the setup is loaded with, one per extruder
	// and in the extruders' order. A printer with an MMU has one per slot of
	// the MMU.
	Filaments []Filament `json:"filaments,omitempty"`
	// Conditions are the compatibility conditions the slicer stored with the
	// profiles, as the slicer wrote them.
	Conditions []string `json:"conditions,omitempty"`
	// Beds are the beds of this setup, in the slicer's order.
	Beds []Bed `json:"beds"`
	// Settings are the settings the slicer stored for this setup, as raw text.
	Settings []SettingsGroup `json:"settings,omitempty"`
}

// Nozzle is one nozzle of a printer profile.
type Nozzle struct {
	// DiameterMM is the nozzle diameter in millimetres.
	DiameterMM float64 `json:"diameter_mm"`
	// HighFlow says whether the nozzle is a high-flow one, and is nil when the
	// file does not say.
	HighFlow *bool `json:"high_flow,omitempty"`
}

// Filament is one filament of a setup.
type Filament struct {
	// Extruder is the extruder the filament is loaded in, counted from 1.
	Extruder int `json:"extruder"`
	// Color is the colour the slicer shows the extruder in, as #RRGGBB. It is
	// empty when the file does not say.
	Color string `json:"color,omitempty"`
	// Type is the material, such as PLA or PETG.
	Type string `json:"type,omitempty"`
}

// SettingsGroup is one table of settings, with each value as the slicer wrote
// it. PrusaSlicer 2.x writes one table called "config", with values as text
// such as "0x0,250x0,250x210,0x210". PrusaSlicer 3.0 writes one table per kind
// of profile, such as "printer_settings", with values as JSON text such as
// [[0,0],[250,0],[250,220],[0,220]] or "\"marlin2\"".
type SettingsGroup struct {
	Name   string            `json:"name"`
	Values map[string]string `json:"values"`
}

// BedRef names one bed: the index of its setup in Project.Setups and its
// index in that setup's Beds.
type BedRef struct {
	Setup int `json:"setup"`
	Bed   int `json:"bed"`
}

// Bed is one bed of a setup.
type Bed struct {
	// Index is the bed's position in its setup's Beds. PrusaSlicer and
	// OrcaSlicer show it to a person as Index+1.
	Index int `json:"index"`
	// Name is the name a person gave the bed. Only OrcaSlicer plates have one.
	Name string `json:"name,omitempty"`
	// Locked is true for an OrcaSlicer plate locked against arranging.
	Locked bool `json:"locked,omitempty"`
	// Offset is where the bed's origin lies in the project's coordinates. A
	// point at p on the bed is at p+Offset in the project, and a slicer slices
	// the bed as if it stood at the origin.
	Offset Point `json:"offset"`
	// Size is the width and depth of the bed shape, and zero when the shape is
	// not known.
	Size Point `json:"size"`
	// Instances are the instances whose Bed is this bed, including any that
	// stick out of it.
	Instances []InstanceRef `json:"instances,omitempty"`
	// Sequential is true when the bed prints one object to its top before
	// it starts the next, and false when it prints all objects layer by
	// layer.
	Sequential bool `json:"sequential,omitempty"`
	// LayerChanges are the changes the slicer inserts at a given height of
	// this bed, such as a filament change, ordered by height.
	LayerChanges []LayerChange `json:"layer_changes,omitempty"`
	// GCode is the zip entry that holds G-code the slicer stored for this
	// bed, and empty when there is none.
	GCode string `json:"gcode,omitempty"`
}

// LayerChangeKind is what a LayerChange does.
type LayerChangeKind string

// The kinds of LayerChange. The slicers store them as the numbers 0 to 4, in
// this order.
const (
	// LayerChangeColor stops for a new filament in the same extruder, with
	// the colour change G-code of the printer, such as M600.
	LayerChangeColor LayerChangeKind = "color_change"
	// LayerChangePause pauses the print, with a message for the display.
	LayerChangePause LayerChangeKind = "pause"
	// LayerChangeTool switches to another extruder for the rest of the print.
	LayerChangeTool LayerChangeKind = "tool_change"
	// LayerChangeTemplate inserts the printer's template G-code.
	LayerChangeTemplate LayerChangeKind = "template"
	// LayerChangeCustom inserts G-code a person wrote, held in Extra.
	LayerChangeCustom LayerChangeKind = "custom"
)

// LayerChange is one change a slicer inserts at a given height.
type LayerChange struct {
	// Height is the top of the layer the change comes after, in
	// millimetres above the bed.
	Height float64 `json:"height"`
	// Kind is what the change does.
	Kind LayerChangeKind `json:"kind"`
	// Extruder is the extruder the change is for, counted from 1: the one
	// that takes the new filament, or the one a tool change switches to.
	Extruder int `json:"extruder,omitempty"`
	// Color is the colour of the new filament as #RRGGBB, for a colour
	// change.
	Color string `json:"color,omitempty"`
	// Extra is the G-code of a custom change and the message of a pause.
	Extra string `json:"extra,omitempty"`
}

// InstanceRef names one instance: the index of its object in Project.Objects
// and its index in that object's Instances.
type InstanceRef struct {
	Object   int `json:"object"`
	Instance int `json:"instance"`
}

// PartType is what one part of an object does.
type PartType string

// The part types PrusaSlicer writes. A file from another program has model
// parts only.
const (
	// PartModel is printed.
	PartModel PartType = "ModelPart"
	// PartNegative is cut out of the model parts.
	PartNegative PartType = "NegativeVolume"
	// PartModifier changes the settings inside it.
	PartModifier PartType = "ParameterModifier"
	// PartSupportBlocker stops support inside it.
	PartSupportBlocker PartType = "SupportBlocker"
	// PartSupportEnforcer adds support inside it.
	PartSupportEnforcer PartType = "SupportEnforcer"
)

// Part is one part of an object.
type Part struct {
	Name string   `json:"name,omitempty"`
	Type PartType `json:"type"`
	// Extruder is the extruder that prints the part's outer wall, counted
	// from 1. It is 0 for a part that prints nothing itself, such as a
	// negative volume, and for a modifier that does not change the extruder.
	// A slicer may print the infill with other extruders; the object's
	// Extruders list them.
	Extruder int `json:"extruder,omitempty"`
	// Painting says what was painted on the part's surface.
	Painting Painting `json:"painting,omitzero"`
	// PaintedExtruders are the extruders the multi-material painting of the
	// part prints with, counted from 1 and in ascending order. A slicer
	// ignores this painting on a printer with one extruder, and so the list is
	// empty then.
	PaintedExtruders []int `json:"painted_extruders,omitempty"`
}

// LayerRange is a height range of an object that prints with settings of
// its own, such as another layer height or another extruder.
type LayerRange struct {
	// MinZ and MaxZ are the bottom and top of the range, in millimetres
	// above the bed.
	MinZ float64 `json:"min_z"`
	MaxZ float64 `json:"max_z"`
	// Extruder is the extruder the range prints the outer walls of its
	// model parts with, counted from 1, and 0 when the range does not
	// change the extruder.
	Extruder int `json:"extruder,omitempty"`
	// Settings are the settings of the range as the slicer wrote them.
	Settings map[string]string `json:"settings,omitempty"`
}

// Painting says what a person painted on the surface of a part or an
// object with the painting tools of a slicer.
type Painting struct {
	// Multimaterial is true when areas were painted to print with other
	// extruders.
	Multimaterial bool `json:"multimaterial,omitempty"`
	// Supports is true when areas were painted to have support, or to have
	// none.
	Supports bool `json:"supports,omitempty"`
	// Seams is true when areas were painted to have the seam, or to avoid it.
	Seams bool `json:"seams,omitempty"`
}

// Object is one object of the project. Each copy of it on the plate is an
// Instance.
type Object struct {
	// Index is the object's position in Project.Objects.
	Index int `json:"index"`
	// ResourceID is the id of the object in 3D/3dmodel.model that the build
	// items of this object point at, through any components.
	ResourceID int `json:"resource_id"`
	// Name is the object's name. It may be empty, and two objects may share it.
	Name string `json:"name,omitempty"`
	// Parts are the object's parts, in the slicer's order.
	Parts []Part `json:"parts,omitempty"`
	// Extruders are the extruders the object prints with, counted from 1
	// and in ascending order: those of its model parts, of the modifiers that
	// change the extruder, of its multi-material painting, and of its support
	// when its settings turn support on.
	Extruders []int `json:"extruders,omitempty"`
	// Painting is what was painted on any of the object's parts.
	Painting Painting `json:"painting,omitzero"`
	// LayerRanges are the heights where the object prints with other
	// settings, lowest first.
	LayerRanges []LayerRange `json:"layer_ranges,omitempty"`
	// Instances are the copies of the object, in build order.
	Instances []Instance `json:"instances"`
}

// Placement says where an instance stands against its beds.
type Placement string

// The placements.
const (
	// PlacementInside is an instance whole on its bed. Only these are sliced.
	PlacementInside Placement = "inside"
	// PlacementPartlyOutside is an instance that sticks out of its bed or
	// above the highest print. A slicer does not slice it.
	PlacementPartlyOutside Placement = "partly_outside"
	// PlacementOutside is an instance wholly off the beds. It has no bed,
	// except in a PrusaSlicer 2.x project whose instance has every model part
	// below the bed: PrusaSlicer gives such a part the first bed.
	PlacementOutside Placement = "outside"
	// PlacementUnknown is an instance in a file whose beds are not known. Bed
	// is the one bed of the generic dialect.
	PlacementUnknown Placement = "unknown"
)

// Instance is one copy of an object, standing on the plate: one build item of
// 3D/3dmodel.model.
type Instance struct {
	// Index is the instance's position in its object's Instances. PrusaSlicer
	// shows it to a person as Index+1.
	Index int `json:"index"`
	// Item is the position of the instance's build item in the build, from 0.
	// It is the key Selection and the slicers use.
	Item int `json:"item"`
	// Transform places the object in the project, as the build item wrote it.
	Transform Transform `json:"transform"`
	// Printable is false for an instance the slicer leaves out of a print.
	Printable bool `json:"printable"`
	// Bed is the bed the instance stands on, and nil when it stands on none.
	Bed *BedRef `json:"bed,omitempty"`
	// Placement says whether the instance is whole on its bed.
	Placement Placement `json:"placement"`
	// Bounds is the box around the instance's model parts, in project
	// coordinates.
	Bounds Box `json:"bounds"`
	// Outline is the convex hull of the instance's model parts seen from
	// above, in project coordinates, counter-clockwise.
	Outline []Point `json:"outline,omitempty"`
	// OutlineApprox is true when Outline and Bounds come from the boxes
	// around the meshes, because the meshes were larger than the Limits allow
	// to be kept.
	OutlineApprox bool `json:"outline_approx,omitempty"`
}

// Thumbnail is one picture the project carries.
type Thumbnail struct {
	// Path is the zip entry the picture is in.
	Path string `json:"path"`
	// Bed is the bed the picture shows, and nil for a picture of the whole
	// project.
	Bed *BedRef `json:"bed,omitempty"`
	// MediaType is image/png or image/jpeg.
	MediaType string `json:"media_type"`
	// Width and Height are the picture's size in pixels, read from the
	// picture's own header.
	Width  int `json:"width"`
	Height int `json:"height"`
	// Data is the encoded picture.
	Data []byte `json:"-"`
}

// IssueCode names a kind of Issue.
type IssueCode string

// The issues Read reports.
const (
	// IssueSettingsUnreadable is a settings file that could not be read. The
	// setup carries what the model said, and no settings.
	IssueSettingsUnreadable IssueCode = "settings_unreadable"
	// IssueOutlineApprox is a mesh too large to keep. Instances of it have
	// outlines made from boxes.
	IssueOutlineApprox IssueCode = "outline_approx"
	// IssueThumbnailUnreadable is a picture that could not be read.
	IssueThumbnailUnreadable IssueCode = "thumbnail_unreadable"
	// IssueUnknownReference is a build item or a component that names an
	// object the file does not have. It is left out.
	IssueUnknownReference IssueCode = "unknown_reference"
	// IssueUnsupportedExtension is a 3MF extension the model lists in its
	// requiredextensions and this package does not read. Detail is its
	// namespace. The core specification says a consumer must not process such
	// a model: an extension such as beam lattices or displacement changes the
	// shapes, so outlines, boxes and beds may be wrong. Read goes on and says
	// so, as the slicers read such files too.
	IssueUnsupportedExtension IssueCode = "unsupported_extension"
	// IssueUnsupportedProducer is a file read by a dialect made for another
	// program, such as a BambuStudio project read as an OrcaSlicer one. Detail
	// is the Application entry.
	IssueUnsupportedProducer IssueCode = "unsupported_producer"
	// IssueGCodeOnly is a file that holds G-code for its beds and no object
	// to slice, such as a .gcode.3mf file from OrcaSlicer or BambuStudio.
	// Each bed with G-code names its entry in GCode. Such a file is printed
	// as it is and cannot be sliced again.
	IssueGCodeOnly IssueCode = "gcode_only"
)

// Issue is one thing Read could not read in full.
type Issue struct {
	Code   IssueCode `json:"code"`
	Detail string    `json:"detail,omitempty"`
}

// Bed returns the bed ref names, and nil when the project has no such bed.
func (p *Project) Bed(ref BedRef) *Bed {
	if ref.Setup < 0 || ref.Setup >= len(p.Setups) {
		return nil
	}

	beds := p.Setups[ref.Setup].Beds
	if ref.Bed < 0 || ref.Bed >= len(beds) {
		return nil
	}

	return &beds[ref.Bed]
}

// Instance returns the instance ref names, and nil when the project has no
// such instance.
func (p *Project) Instance(ref InstanceRef) *Instance {
	if ref.Object < 0 || ref.Object >= len(p.Objects) {
		return nil
	}

	instances := p.Objects[ref.Object].Instances
	if ref.Instance < 0 || ref.Instance >= len(instances) {
		return nil
	}

	return &instances[ref.Instance]
}

// Item returns the instance of the build item at position item.
func (p *Project) Item(item int) (InstanceRef, bool) {
	for o, object := range p.Objects {
		for i, instance := range object.Instances {
			if instance.Item == item {
				return InstanceRef{Object: o, Instance: i}, true
			}
		}
	}

	return InstanceRef{}, false
}

// Items is the number of build items the project has.
func (p *Project) Items() int {
	count := 0
	for _, object := range p.Objects {
		count += len(object.Instances)
	}

	return count
}
