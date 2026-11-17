package document

// Project is what one 3MF project file holds: who wrote it, the printer
// setups with their beds, the objects with every copy of them on the plate,
// the pictures, and what the module could not read.
//
// Every position is in millimetres. A bed's offset is where its origin lies in
// the project, so a plan that draws each bed at its offset lays the beds out
// the way the slicer did.
type Project struct {
	// The program that wrote the file.
	Producer ProjectProducer `json:"producer"`
	// The title the file carries in its metadata. Absent when it has none.
	Title string `json:"title,omitempty"`
	// How many of each thing the project holds, counted by the module so a page
	// does not walk the lists to say so.
	Counts ProjectCounts `json:"counts"`
	// The printer setups, each with its beds. Every project has at least one.
	// Only a PrusaSlicer 3 project has more than one.
	Setups []ProjectSetup `json:"setups"`
	// The objects, in the slicer's order, each with its instances.
	Objects []ProjectObject `json:"objects"`
	// The pictures the file carries, best first. get-project-thumbnail serves
	// each one by its index.
	Thumbnails []ProjectThumbnail `json:"thumbnails"`
	// What the module could not read in full. None of them stopped the read.
	// Empty when the file was read whole.
	Issues []ProjectIssue `json:"issues"`
	// The extruders the project prints with, counted from 1: those of every
	// object and those a tool change switches to. Absent for a project with no
	// object.
	Extruders []int `json:"extruders,omitempty"`
}

// ProjectProducer is the program that wrote a project.
type ProjectProducer struct {
	// The Application entry of the model as the program wrote it, such as
	// PrusaSlicer-2.9.6.
	Application string `json:"application,omitempty"`
	// The program's name, such as PrusaSlicer or OrcaSlicer.
	Name string `json:"name,omitempty"`
	// The program's version, such as 2.9.6 or 3.0.0-alpha12.
	Version string `json:"version,omitempty"`
	// The version of the program's own file layout inside the 3MF.
	FormatVersion string `json:"format_version,omitempty"`
	// The reader that understood the program's own files. generic reads the 3MF
	// core alone: one setup, one bed and no settings.
	Dialect string `json:"dialect"`
	// Whether the dialect was made for this program. A BambuStudio project is
	// read by the OrcaSlicer dialect and is not supported, so what it says may
	// be incomplete.
	Supported bool `json:"supported"`
}

// ProjectCounts says how many of each thing a project holds.
type ProjectCounts struct {
	// The number of printer setups.
	Setups int `json:"setups"`
	// The number of beds, across every setup.
	Beds int `json:"beds"`
	// The number of objects.
	Objects int `json:"objects"`
	// The number of instances, which is the number of copies of objects on the
	// plate.
	Instances int `json:"instances"`
	// The instances the slicer leaves out of a print.
	NonPrintable int `json:"non_printable"`
	// The instances that stick out of their bed or above the highest print.
	PartlyOutside int `json:"partly_outside"`
	// The instances that stand on no bed.
	Outside int `json:"outside"`
}

// ProjectSetup is one printer setup and the beds that use it.
type ProjectSetup struct {
	// The setup's position in setups, from 0.
	Index int `json:"index"`
	// The printer the profile is for, such as MK4S or COREONE.
	PrinterModel string `json:"printer_model,omitempty"`
	// The name of the slicer's printer profile.
	PrinterProfile string `json:"printer_profile,omitempty"`
	// The name of the slicer's print profile.
	PrintProfile string `json:"print_profile,omitempty"`
	// The names of the slicer's material profiles, one per filament.
	MaterialProfiles []string `json:"material_profiles"`
	// The nozzles of the printer profile, one per tool.
	Nozzles []ProjectNozzle `json:"nozzles"`
	// The outline of one bed in the bed's own coordinates. Empty when the file
	// does not say.
	BedShape []ProjectPoint `json:"bed_shape"`
	// The highest a print may reach. Absent when the file does not say.
	MaxPrintHeightMM float64 `json:"max_print_height_mm,omitempty"`
	// FFF or SLA.
	Technology string `json:"technology,omitempty"`
	// How the slicer marks objects in G-code: firmware, octoprint or disabled.
	// Only firmware lets a printer cancel one object.
	LabelObjects string `json:"label_objects,omitempty"`
	// Whether the profile exports binary G-code. Absent when the file does not
	// say.
	BinaryGCode *bool `json:"binary_gcode,omitempty"`
	// A few settings read out of the slicer's tables, for a summary. A setting
	// the file does not hold is absent.
	Settings ProjectSettings `json:"settings"`
	// Every setting the slicer stored, one table per kind of profile, with each
	// value as the slicer wrote it. Present only when raw_settings=true was
	// asked for.
	RawSettings []ProjectSettingsGroup `json:"raw_settings,omitempty"`
	// The beds of this setup, in the slicer's order.
	Beds []ProjectBed `json:"beds"`
}

// ProjectNozzle is one nozzle of a printer profile.
type ProjectNozzle struct {
	// The nozzle diameter.
	DiameterMM float64 `json:"diameter_mm"`
	// Whether it is a high-flow nozzle. Absent when the file does not say.
	HighFlow *bool `json:"high_flow,omitempty"`
}

// ProjectPoint is a position on the bed plane, in millimetres.
type ProjectPoint struct {
	// Millimetres to the right of the origin.
	X float64 `json:"x"`
	// Millimetres behind the origin.
	Y float64 `json:"y"`
}

// ProjectSize is the extent of a thing, in millimetres.
type ProjectSize struct {
	// The width.
	X float64 `json:"x"`
	// The depth.
	Y float64 `json:"y"`
	// The height.
	Z float64 `json:"z"`
}

// ProjectSettings is the summary of one setup's settings. The values are
// normalised from the slicer's own tables, which spell them differently.
type ProjectSettings struct {
	// The layer height.
	LayerHeightMM float64 `json:"layer_height_mm,omitempty"`
	// The height of the first layer.
	FirstLayerHeightMM float64 `json:"first_layer_height_mm,omitempty"`
	// The sparse infill density, from 0 to 100.
	InfillPercent *float64 `json:"infill_percent,omitempty"`
	// The slicer's name for the sparse infill pattern, such as gyroid.
	InfillPattern string `json:"infill_pattern,omitempty"`
	// The number of perimeters, which OrcaSlicer calls wall loops.
	Perimeters int `json:"perimeters,omitempty"`
	// Where the slicer generates support. Absent when the file does not say.
	Supports string `json:"supports,omitempty"`
	// The brim width. 0 is no brim.
	BrimWidthMM *float64 `json:"brim_width_mm,omitempty"`
	// Whether the slicer adds a wipe tower.
	WipeTower *bool `json:"wipe_tower,omitempty"`
	// The filaments the setup is sliced for, in tool order.
	Filaments []ProjectFilament `json:"filaments"`
}

// ProjectFilament is one filament of a setup.
type ProjectFilament struct {
	// The filament type, such as PLA.
	Type string `json:"type,omitempty"`
	// The colour the slicer shows the filament in, as #RRGGBB.
	Colour string `json:"colour,omitempty"`
}

// ProjectSettingsGroup is one of the slicer's tables of settings.
type ProjectSettingsGroup struct {
	// The table's name: config for PrusaSlicer 2, a kind of profile such as
	// print_settings for PrusaSlicer 3, and project_settings for OrcaSlicer.
	Name string `json:"name"`
	// Each setting by its key, with the value as the slicer wrote it.
	Values map[string]string `json:"values"`
}

// ProjectBedRef names one bed.
type ProjectBedRef struct {
	// The index of the setup in setups.
	Setup int `json:"setup"`
	// The index of the bed in that setup's beds.
	Bed int `json:"bed"`
}

// ProjectInstanceRef names one instance.
type ProjectInstanceRef struct {
	// The index of the object in objects.
	Object int `json:"object"`
	// The index of the instance in that object's instances.
	Instance int `json:"instance"`
}

// ProjectBed is one bed of a setup.
type ProjectBed struct {
	// The bed's position in its setup, from 0. A slicer shows it as index+1.
	Index int `json:"index"`
	// The name a person gave the bed. Only an OrcaSlicer plate has one.
	Name string `json:"name,omitempty"`
	// The OrcaSlicer plate is locked against arranging.
	Locked bool `json:"locked,omitempty"`
	// Where the bed's origin lies in the project.
	Offset ProjectPoint `json:"offset"`
	// The width and depth of the bed. Zero when the bed shape is not known.
	Size ProjectPoint `json:"size"`
	// The index in thumbnails of the picture of this bed. Absent when the file
	// carries none.
	Thumbnail *int `json:"thumbnail,omitempty"`
	// The instances that stand on this bed, including any that stick out of it.
	Instances []ProjectInstanceRef `json:"instances"`
	// The bed prints one object to its top before it starts the next.
	Sequential bool `json:"sequential,omitempty"`
	// The changes the slicer inserts at a given height of this bed, lowest
	// first. Absent when there are none.
	LayerChanges []ProjectLayerChange `json:"layer_changes,omitempty"`
	// The entry inside the file that holds G-code the slicer stored for this
	// bed. Absent when there is none.
	GCode string `json:"gcode,omitempty"`
}

// ProjectLayerChange is one change a slicer inserts at a given height.
type ProjectLayerChange struct {
	// The top of the layer the change comes after.
	HeightMM float64 `json:"height_mm"`
	// What the change does. color_change stops for a new filament, pause pauses
	// the print, tool_change switches to another extruder, template inserts the
	// printer's template G-code and custom inserts G-code a person wrote.
	Kind string `json:"kind"`
	// The extruder the change is for, counted from 1.
	Extruder int `json:"extruder,omitempty"`
	// The colour of the new filament as #RRGGBB, for a colour change or a tool
	// change.
	Color string `json:"color,omitempty"`
	// The G-code of a custom change, or the message of a pause.
	Extra string `json:"extra,omitempty"`
}

// ProjectObject is one object and its copies.
type ProjectObject struct {
	// The object's position in objects.
	Index int `json:"index"`
	// The object's name. Two objects may share one, and it may be absent.
	Name string `json:"name,omitempty"`
	// The parts the object is built from, in the slicer's order.
	Parts []ProjectPart `json:"parts"`
	// The copies of the object, in build order.
	Instances []ProjectInstance `json:"instances"`
	// The extruders the object prints with, counted from 1: those of its parts,
	// its painting, its layer ranges and its support.
	Extruders []int `json:"extruders,omitempty"`
	// What was painted on any of the object's parts. Absent when nothing was.
	Painting *ProjectPainting `json:"painting,omitempty"`
	// The heights where the object prints with settings of their own, lowest
	// first. Absent when there are none.
	LayerRanges []ProjectLayerRange `json:"layer_ranges,omitempty"`
}

// ProjectPainting says what a person painted on a surface with the painting
// tools of a slicer.
type ProjectPainting struct {
	// Areas were painted to print with other extruders.
	Multimaterial bool `json:"multimaterial,omitempty"`
	// Areas were painted to have support, or to have none.
	Supports bool `json:"supports,omitempty"`
	// Areas were painted to have the seam, or to avoid it.
	Seams bool `json:"seams,omitempty"`
}

// ProjectLayerRange is a height range of an object with settings of its own.
type ProjectLayerRange struct {
	// The bottom of the range.
	MinZMM float64 `json:"min_z_mm"`
	// The top of the range.
	MaxZMM float64 `json:"max_z_mm"`
	// The extruder the range prints the outer walls with, counted from 1. Absent
	// when the range does not change the extruder.
	Extruder int `json:"extruder,omitempty"`
	// The settings of the range, by key, with each value as the slicer wrote it.
	Settings map[string]string `json:"settings"`
}

// ProjectPart is one part of an object.
type ProjectPart struct {
	// The part's name.
	Name string `json:"name,omitempty"`
	// What the part does. model is printed, negative_volume is cut out of the
	// model, modifier changes the settings inside it, and support_blocker and
	// support_enforcer stop or add support.
	Type string `json:"type"`
	// The extruder that prints the part's outer wall, counted from 1. Absent for
	// a part that prints nothing itself, and for a modifier that does not change
	// the extruder.
	Extruder int `json:"extruder,omitempty"`
	// The extruders the multi-material painting of the part prints with, counted
	// from 1. Absent on a printer with one extruder and for a part with no such
	// painting.
	PaintedExtruders []int `json:"painted_extruders,omitempty"`
	// What was painted on the part. Absent when nothing was.
	Painting *ProjectPainting `json:"painting,omitempty"`
}

// ProjectInstance is one copy of an object on the plate.
type ProjectInstance struct {
	// The instance's position in its object, from 0. A slicer shows it as
	// index+1.
	Index int `json:"index"`
	// The position of the instance's build item in the file, from 0.
	Item int `json:"item"`
	// False for an instance the slicer leaves out of a print.
	Printable bool `json:"printable"`
	// The bed the instance stands on. Absent when it stands on none.
	Bed *ProjectBedRef `json:"bed,omitempty"`
	// Whether the instance is whole on its bed. Only an instance inside is
	// sliced. unknown is the one bed of a file whose beds the module does not
	// know.
	Placement string `json:"placement"`
	// The size of the box around the instance's model parts.
	Size ProjectSize `json:"size"`
	// The front left corner of that box, in the coordinates of the instance's
	// bed, or of the project for an instance on no bed.
	Min ProjectPoint `json:"min"`
	// The outline of the instance's model parts seen from above, counter-
	// clockwise, in the same coordinates as min. Add the bed's offset for the
	// project's coordinates. It has at most 64 points, rounded to a hundredth of
	// a millimetre.
	Outline []ProjectPoint `json:"outline"`
	// The outline and the box come from the boxes around the meshes, because the
	// meshes were too large to keep.
	OutlineApprox bool `json:"outline_approx,omitempty"`
	// The build item's transform as the 3MF file writes it: m00 m01 m02 m10 m11
	// m12 m20 m21 m22 m30 m31 m32, in project coordinates.
	Transform [12]float64 `json:"transform"`
}

// ProjectThumbnail is one picture a project carries.
type ProjectThumbnail struct {
	// The picture's position in thumbnails. get-project-thumbnail takes it.
	Index int `json:"index"`
	// The bed the picture shows. Absent for a picture of the whole project.
	Bed *ProjectBedRef `json:"bed,omitempty"`
	// image/png or image/jpeg.
	MediaType string `json:"media_type"`
	// The width in pixels.
	Width int `json:"width"`
	// The height in pixels.
	Height int `json:"height"`
}

// ProjectIssue is one thing the module could not read in full.
type ProjectIssue struct {
	// What kind of thing. settings_unreadable is a settings file that could not
	// be read, outline_approx a mesh too large to keep, thumbnail_unreadable a
	// picture that could not be read, unknown_reference a build item or a
	// component naming an object the file does not have, unsupported_extension a
	// 3MF extension the module does not read, unsupported_producer a file read
	// by a dialect made for another program, and gcode_only a file that holds
	// G-code for its beds and no object to slice.
	Code string `json:"code"`
	// What the issue is about, such as the name of the file inside the project.
	Detail string `json:"detail,omitempty"`
}

// ProjectMesh is one object of a 3MF project as triangles, made light enough
// for a page to draw.
type ProjectMesh struct {
	// The object's index in the project's objects.
	Object int `json:"object"`
	// x, y and z of each vertex in millimetres, three numbers a vertex, rounded
	// to a hundredth. The object stands as its first copy stands, turned and
	// scaled but not moved, centred on x 0 and y 0 with its lowest point at z 0.
	Vertices []float32 `json:"vertices"`
	// Indices into the vertices, three a triangle, part after part in the order
	// of parts.
	Triangles []uint32 `json:"triangles"`
	// The object's model parts in the order their triangles come. Modifiers,
	// negative volumes and support blockers or enforcers are left out.
	Parts []ProjectMeshPart `json:"parts"`
	// How many triangles the model parts have in the file. More than the
	// triangles sent means the mesh was made coarser.
	SourceTriangles int `json:"source_triangles"`
	// A mesh was too large for the module to keep, and its part is drawn as the
	// box around it.
	Box bool `json:"box,omitempty"`
}

// ProjectMeshPart is one model part of a ProjectMesh.
type ProjectMeshPart struct {
	// The part's name. Absent when the file gives none.
	Name string `json:"name,omitempty"`
	// The extruder of the part's outer wall, counted from 1, as the part in get-
	// project says.
	Extruder int `json:"extruder,omitempty"`
	// How many triangles of the mesh belong to the part.
	Triangles int `json:"triangles"`
}
