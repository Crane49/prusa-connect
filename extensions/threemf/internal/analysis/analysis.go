// Package analysis answers the core's question about one .3mf file
// (ADR-20261027-wvj rules 1 to 12).
//
// The core asks for the picture in a listing, the details on a file's page
// and what a printer needs. The host opens the file through a read handle
// for this one call, so the module needs no read_storage for it and reads no
// other file. The answer is held to the bounds sdk/fileapi publishes, and the
// host keeps it per file version and per release of this module.
package analysis

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/prusa3d/prusa-connect/sdk/fileapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"

	"github.com/prusa3d/threemf/internal/document"
	"github.com/prusa3d/threemf/internal/projects"
	"github.com/prusa3d/threemf/threemf"
)

// Types is the file types this module reads for the core. module.yaml names
// the same list under file_analysis, and a test holds the two together.
//
//nolint:gochecknoglobals // a fixed list.
var Types = []string{".3mf"}

// New answers file.analyze with the reader's slots.
func New(reader *projects.Reader) fileapi.Analyze {
	return func(ctx context.Context, params fileapi.Params, file *storageapi.File) (fileapi.Analysis, error) {
		project, err := reader.Analyze(ctx, file)
		if err != nil {
			return fileapi.Analysis{}, fmt.Errorf("read %s: %w", params.Name, err)
		}

		return Of(project), nil
	}
}

// Of describes one project in the shape the core reads.
func Of(project *threemf.Project) fileapi.Analysis {
	view := document.Of(project, false)

	return fileapi.Analysis{
		Title:      clip(view.Title),
		Thumbnails: thumbnails(project.Thumbnails),
		Print:      printOf(project, view),
		Facts:      facts(view),
		Objects:    objects(view.Objects),
	}
}

// thumbnails picks the pictures the core may serve: the pictures of the
// whole project first, then those of the beds, as many as the bounds hold.
func thumbnails(pictures []threemf.Thumbnail) []fileapi.Thumbnail {
	ordered := slices.Clone(pictures)
	slices.SortStableFunc(ordered, func(a, b threemf.Thumbnail) int {
		switch {
		case a.Bed == nil && b.Bed != nil:
			return -1
		case a.Bed != nil && b.Bed == nil:
			return 1
		}

		return 0
	})

	var (
		out   []fileapi.Thumbnail
		bytes int
	)

	for _, picture := range ordered {
		if len(out) == fileapi.MaxThumbnails {
			break
		}

		if picture.MediaType != fileapi.MediaPNG && picture.MediaType != fileapi.MediaJPEG {
			continue
		}

		if len(picture.Data) == 0 || picture.Width <= 0 || picture.Height <= 0 ||
			bytes+len(picture.Data) > fileapi.MaxThumbnailBytes {
			continue
		}

		bytes += len(picture.Data)
		out = append(out, fileapi.Thumbnail{
			MediaType: picture.MediaType, Width: picture.Width, Height: picture.Height, Data: picture.Data,
		})
	}

	return out
}

// printOf is what a printer needs, read from the first setup. A project is
// not sliced, so it says nothing about time or filament used. Texts and
// filaments are cut to the bounds sdk/fileapi publishes.
func printOf(project *threemf.Project, view document.Project) *storageapi.PrintInfo {
	if len(project.Setups) == 0 || len(view.Setups) == 0 {
		return nil
	}

	setup := project.Setups[0]
	settings := view.Setups[0].Settings

	out := &storageapi.PrintInfo{
		PrinterModel:   clip(setup.PrinterModel),
		PrinterProfile: clip(setup.PrinterProfile),
		PrintProfile:   clip(setup.PrintProfile),
		LayerHeightMM:  settings.LayerHeightMM,
	}

	if len(setup.MaterialProfiles) > 0 {
		out.FilamentProfile = clip(setup.MaterialProfiles[0])
	}

	if len(setup.Nozzles) > 0 {
		out.NozzleMM = setup.Nozzles[0].DiameterMM
		out.HighFlow = setup.Nozzles[0].HighFlow != nil && *setup.Nozzles[0].HighFlow
	}

	for i, filament := range settings.Filaments[:min(len(settings.Filaments), fileapi.MaxFilaments)] {
		row := storageapi.PrintFilament{Index: i, Material: clip(filament.Type), Colour: clip(filament.Colour)}
		if i < len(setup.Nozzles) {
			row.NozzleMM = setup.Nozzles[i].DiameterMM
			row.HighFlow = setup.Nozzles[i].HighFlow
		}

		out.Filaments = append(out.Filaments, row)
	}

	if len(out.Filaments) > 0 {
		out.Material = out.Filaments[0].Material
	}

	if size := view.Setups[0]; len(size.Beds) > 0 && size.Beds[0].Size.X > 0 {
		out.Bed = &storageapi.PrintBed{
			WidthMM: size.Beds[0].Size.X, DepthMM: size.Beds[0].Size.Y, HeightMM: setup.MaxPrintHeight,
		}
	}

	for _, picture := range project.Thumbnails {
		if picture.Bed == nil {
			out.ThumbnailWidth, out.ThumbnailHeight = picture.Width, picture.Height

			break
		}
	}

	return out
}

// facts are the lines the file page lists, in the order a person reads them.
func facts(view document.Project) []fileapi.Fact {
	var out []fileapi.Fact

	add := func(key, label, value string) {
		if value == "" || len(out) == fileapi.MaxFacts {
			return
		}

		out = append(out, fileapi.Fact{
			Key: key, Label: label, Value: clip(value),
		})
	}

	producer := strings.TrimSpace(view.Producer.Name + " " + view.Producer.Version)
	add("producer", "Saved by", producer)

	if len(view.Setups) > 0 {
		add("printer", "Printer", view.Setups[0].PrinterModel)
	}

	if view.Counts.Setups > 1 {
		add("setups", "Printer setups", strconv.Itoa(view.Counts.Setups))
	}

	add("beds", "Beds", strconv.Itoa(view.Counts.Beds))
	add("objects", "Objects", strconv.Itoa(view.Counts.Objects))
	add("instances", "Copies", strconv.Itoa(view.Counts.Instances))

	if view.Counts.NonPrintable > 0 {
		add("non_printable", "Copies left out of the print", strconv.Itoa(view.Counts.NonPrintable))
	}

	if view.Counts.PartlyOutside+view.Counts.Outside > 0 {
		add("outside", "Copies not whole on a bed", strconv.Itoa(view.Counts.PartlyOutside+view.Counts.Outside))
	}

	if len(view.Extruders) > 0 {
		add("extruders", "Extruders", strconv.Itoa(len(view.Extruders)))
	}

	if len(view.Issues) > 0 {
		add("issues", "Things not read", strconv.Itoa(len(view.Issues)))
	}

	return out
}

// objects are the objects of the project, as many as the bounds hold. The id
// is the object's index in the project.
func objects(rows []document.ProjectObject) []fileapi.Object {
	out := make([]fileapi.Object, 0, min(len(rows), fileapi.MaxObjects))

	for _, row := range rows {
		if len(out) == fileapi.MaxObjects {
			break
		}

		name := row.Name
		if name == "" {
			name = "Object " + strconv.Itoa(row.Index+1)
		}

		out = append(out, fileapi.Object{ID: strconv.Itoa(row.Index), Name: clip(name)})
	}

	return out
}

// clip cuts text to at most fileapi.MaxTextBytes, on a rune boundary.
func clip(text string) string {
	if len(text) <= fileapi.MaxTextBytes {
		return text
	}

	cut := fileapi.MaxTextBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}

	return text[:cut]
}
