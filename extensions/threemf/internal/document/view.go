package document

import (
	"maps"
	"math"

	"github.com/prusa3d/threemf/threemf"
)

// partTypes is the word the API gives each part type PrusaSlicer writes.
//
//nolint:gochecknoglobals // a constant table.
var partTypes = map[threemf.PartType]string{
	threemf.PartModel:           "model",
	threemf.PartNegative:        "negative_volume",
	threemf.PartModifier:        "modifier",
	threemf.PartSupportBlocker:  "support_blocker",
	threemf.PartSupportEnforcer: "support_enforcer",
}

// Of renders a project for the project route.
//
// An instance's outline and box are moved into the coordinates of its bed, so
// a page that draws one bed draws it at the origin. An instance on no bed
// keeps the project's coordinates, which a bed's offset is measured in.
func Of(project *threemf.Project, rawSettings bool) Project {
	out := Project{
		Producer:   producerOf(project),
		Title:      titleOf(project.Metadata),
		Setups:     make([]ProjectSetup, 0, len(project.Setups)),
		Objects:    make([]ProjectObject, 0, len(project.Objects)),
		Thumbnails: make([]ProjectThumbnail, 0, len(project.Thumbnails)),
		Issues:     make([]ProjectIssue, 0, len(project.Issues)),
		Extruders:  project.Extruders,
	}

	pictures := map[threemf.BedRef]int{}

	for i, picture := range project.Thumbnails {
		row := ProjectThumbnail{
			Index: i, MediaType: picture.MediaType, Width: picture.Width, Height: picture.Height,
		}

		if picture.Bed != nil {
			row.Bed = bedRefOf(picture.Bed)

			if _, seen := pictures[*picture.Bed]; !seen {
				pictures[*picture.Bed] = i
			}
		}

		out.Thumbnails = append(out.Thumbnails, row)
	}

	for _, setup := range project.Setups {
		out.Setups = append(out.Setups, setupOf(setup, pictures, rawSettings))
		out.Counts.Beds += len(setup.Beds)
	}

	for _, object := range project.Objects {
		out.Objects = append(out.Objects, objectOf(project, object, &out.Counts))
	}

	for _, issue := range project.Issues {
		out.Issues = append(out.Issues, ProjectIssue{Code: string(issue.Code), Detail: issue.Detail})
	}

	out.Counts.Setups = len(out.Setups)
	out.Counts.Objects = len(out.Objects)

	return out
}

func producerOf(project *threemf.Project) ProjectProducer {
	supported := true

	for _, issue := range project.Issues {
		if issue.Code == threemf.IssueUnsupportedProducer {
			supported = false
		}
	}

	return ProjectProducer{
		Application:   project.Producer.Application,
		Name:          project.Producer.Name,
		Version:       project.Producer.Version,
		FormatVersion: project.Producer.FormatVersion,
		Dialect:       string(project.Producer.Dialect),
		Supported:     supported,
	}
}

func titleOf(metadata []threemf.Metadata) string {
	for _, entry := range metadata {
		if entry.Name == "Title" {
			return entry.Value
		}
	}

	return ""
}

func setupOf(setup threemf.Setup, pictures map[threemf.BedRef]int, rawSettings bool) ProjectSetup {
	out := ProjectSetup{
		Index:            setup.Index,
		PrinterModel:     setup.PrinterModel,
		PrinterProfile:   setup.PrinterProfile,
		PrintProfile:     setup.PrintProfile,
		MaterialProfiles: append([]string{}, setup.MaterialProfiles...),
		Nozzles:          make([]ProjectNozzle, 0, len(setup.Nozzles)),
		BedShape:         pointsOf(setup.BedShape, threemf.Point{}),
		MaxPrintHeightMM: setup.MaxPrintHeight,
		Technology:       setup.Technology,
		LabelObjects:     setup.LabelObjects,
		BinaryGCode:      setup.BinaryGCode,
		Settings:         projectSettingsOf(summariseSettings(setup), setup.Filaments),
		Beds:             make([]ProjectBed, 0, len(setup.Beds)),
	}

	for _, nozzle := range setup.Nozzles {
		out.Nozzles = append(
			out.Nozzles,
			ProjectNozzle{DiameterMM: nozzle.DiameterMM, HighFlow: nozzle.HighFlow},
		)
	}

	if rawSettings {
		out.RawSettings = make([]ProjectSettingsGroup, 0, len(setup.Settings))
		for _, group := range setup.Settings {
			out.RawSettings = append(out.RawSettings, ProjectSettingsGroup{
				Name: group.Name, Values: maps.Clone(group.Values),
			})
		}
	}

	for _, bed := range setup.Beds {
		row := ProjectBed{
			Index:      bed.Index,
			Name:       bed.Name,
			Locked:     bed.Locked,
			Offset:     ProjectPoint{X: bed.Offset.X, Y: bed.Offset.Y},
			Size:       ProjectPoint{X: bed.Size.X, Y: bed.Size.Y},
			Instances:  make([]ProjectInstanceRef, 0, len(bed.Instances)),
			Sequential: bed.Sequential,

			GCode: bed.GCode,
		}

		for _, change := range bed.LayerChanges {
			row.LayerChanges = append(row.LayerChanges, ProjectLayerChange{
				HeightMM: change.Height, Kind: string(change.Kind), Extruder: change.Extruder,
				Color: change.Color, Extra: change.Extra,
			})
		}

		if picture, ok := pictures[threemf.BedRef{Setup: setup.Index, Bed: bed.Index}]; ok {
			row.Thumbnail = &picture
		}

		for _, ref := range bed.Instances {
			row.Instances = append(
				row.Instances,
				ProjectInstanceRef{Object: ref.Object, Instance: ref.Instance},
			)
		}

		out.Beds = append(out.Beds, row)
	}

	return out
}

// projectSettingsOf renders the settings summary. The filaments come from
// the reader, which takes the colour each slicer shows an extruder in, and
// from the summary for a file whose reader found none.
func projectSettingsOf(summary settingsSummary, filaments []threemf.Filament) ProjectSettings {
	out := ProjectSettings{
		LayerHeightMM:      summary.LayerHeightMM,
		FirstLayerHeightMM: summary.FirstLayerHeightMM,
		InfillPercent:      summary.InfillPercent,
		InfillPattern:      summary.InfillPattern,
		Perimeters:         summary.Perimeters,
		Supports:           summary.Supports,
		BrimWidthMM:        summary.BrimWidthMM,
		WipeTower:          summary.WipeTower,
		Filaments:          make([]ProjectFilament, 0, len(summary.Filaments)),
	}

	if len(filaments) > 0 {
		for _, filament := range filaments {
			out.Filaments = append(out.Filaments, ProjectFilament{Type: filament.Type, Colour: filament.Color})
		}

		return out
	}

	for _, filament := range summary.Filaments {
		out.Filaments = append(out.Filaments, ProjectFilament(filament))
	}

	return out
}

func objectOf(project *threemf.Project, object threemf.Object, counts *ProjectCounts) ProjectObject {
	out := ProjectObject{
		Index:       object.Index,
		Name:        object.Name,
		Parts:       make([]ProjectPart, 0, len(object.Parts)),
		Instances:   make([]ProjectInstance, 0, len(object.Instances)),
		Extruders:   append([]int{}, object.Extruders...),
		Painting:    paintingOf(object.Painting),
		LayerRanges: make([]ProjectLayerRange, 0, len(object.LayerRanges)),
	}

	for _, r := range object.LayerRanges {
		out.LayerRanges = append(out.LayerRanges, ProjectLayerRange{
			MinZMM: r.MinZ, MaxZMM: r.MaxZ, Extruder: r.Extruder, Settings: maps.Clone(r.Settings),
		})

		if out.LayerRanges[len(out.LayerRanges)-1].Settings == nil {
			out.LayerRanges[len(out.LayerRanges)-1].Settings = map[string]string{}
		}
	}

	for _, part := range object.Parts {
		kind, ok := partTypes[part.Type]
		if !ok {
			kind = partTypes[threemf.PartModel]
		}

		out.Parts = append(out.Parts, ProjectPart{
			Name: part.Name, Type: kind, Extruder: part.Extruder,
			PaintedExtruders: part.PaintedExtruders,
			Painting:         paintingOf(part.Painting),
		})
	}

	for _, instance := range object.Instances {
		out.Instances = append(out.Instances, instanceOf(project, instance))

		counts.Instances++

		if !instance.Printable {
			counts.NonPrintable++
		}

		switch instance.Placement {
		case threemf.PlacementPartlyOutside:
			counts.PartlyOutside++
		case threemf.PlacementOutside:
			counts.Outside++
		case threemf.PlacementInside, threemf.PlacementUnknown:
		}
	}

	return out
}

func instanceOf(project *threemf.Project, instance threemf.Instance) ProjectInstance {
	var origin threemf.Point

	if instance.Bed != nil {
		if bed := project.Bed(*instance.Bed); bed != nil {
			origin = bed.Offset
		}
	}

	out := ProjectInstance{
		Index:         instance.Index,
		Item:          instance.Item,
		Printable:     instance.Printable,
		Bed:           bedRefOf(instance.Bed),
		Placement:     string(instance.Placement),
		Outline:       outlineOf(instance.Outline, origin),
		OutlineApprox: instance.OutlineApprox,
		Transform:     instance.Transform,
	}

	box := instance.Bounds
	if box.Min[0] <= box.Max[0] && box.Min[1] <= box.Max[1] && box.Min[2] <= box.Max[2] {
		out.Size = ProjectSize{
			X: box.Max[0] - box.Min[0],
			Y: box.Max[1] - box.Min[1],
			Z: box.Max[2] - box.Min[2],
		}
		out.Min = ProjectPoint{X: box.Min[0] - origin.X, Y: box.Min[1] - origin.Y}
	}

	return out
}

func bedRefOf(ref *threemf.BedRef) *ProjectBedRef {
	if ref == nil {
		return nil
	}

	return &ProjectBedRef{Setup: ref.Setup, Bed: ref.Bed}
}

// maxOutlinePoints is the most points an instance's outline is sent with. The
// hull of a detailed mesh has hundreds, and a plan drawn on a screen cannot
// show more than a few dozen, while a project of a thousand copies would send
// every one of them.
const maxOutlinePoints = 64

// outlineResolution is the step an outline's coordinates are rounded to, in
// millimetres. It is finer than a nozzle draws.
const outlineResolution = 100

// outlineOf moves an outline by -origin, keeps at most maxOutlinePoints of its
// points spread evenly around it, and rounds them to a hundredth of a
// millimetre. Every point kept is a point of the convex hull, so the shape
// drawn is still convex and stays inside the true one.
func outlineOf(points []threemf.Point, origin threemf.Point) []ProjectPoint {
	kept := points

	if len(points) > maxOutlinePoints {
		kept = make([]threemf.Point, 0, maxOutlinePoints)
		for i := range maxOutlinePoints {
			kept = append(kept, points[i*len(points)/maxOutlinePoints])
		}
	}

	out := pointsOf(kept, origin)
	for i := range out {
		out[i].X = math.Round(out[i].X*outlineResolution) / outlineResolution
		out[i].Y = math.Round(out[i].Y*outlineResolution) / outlineResolution
	}

	return out
}

// pointsOf moves points by -origin.
func pointsOf(points []threemf.Point, origin threemf.Point) []ProjectPoint {
	out := make([]ProjectPoint, 0, len(points))
	for _, point := range points {
		out = append(out, ProjectPoint{X: point.X - origin.X, Y: point.Y - origin.Y})
	}

	return out
}

// paintingOf is nil for a surface nothing was painted on.
func paintingOf(p threemf.Painting) *ProjectPainting {
	if p == (threemf.Painting{}) {
		return nil
	}

	return &ProjectPainting{Multimaterial: p.Multimaterial, Supports: p.Supports, Seams: p.Seams}
}
