package app

import (
	"github.com/prusa3d/prusa-connect/internal/printfile"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// The per-filament needs of a print file, carried from the reader that found them
// to the HTTP surface, and from there onto the module wire. The three shapes
// name every member alike, so each step only moves values.

// fillNeeds copies what the reader found about each filament, the bed and the
// G-code checks onto the stat route's details.
func fillNeeds(out *httpapi.PrintDetails, found printfile.Details) {
	out.MaxLayerZMM = found.MaxLayerZMM

	for _, filament := range found.Filaments {
		out.Filaments = append(out.Filaments, httpapi.PrintFilament{
			Index:                 filament.Index,
			Used:                  filament.Used,
			Material:              filament.Material,
			NozzleMM:              filament.NozzleMM,
			HighFlow:              filament.HighFlow,
			Abrasive:              filament.Abrasive,
			NozzleTempC:           filament.NozzleTempC,
			FirstLayerNozzleTempC: filament.FirstLayerNozzleTempC,
			BedTempC:              filament.BedTempC,
			FirstLayerBedTempC:    filament.FirstLayerBedTempC,
			FilamentG:             filament.FilamentG,
			FilamentMM:            filament.FilamentMM,
			Colour:                filament.Colour,
		})
	}

	if found.Bounds != nil {
		out.Bounds = &httpapi.PrintBounds{
			MinXMM: found.Bounds.MinXMM, MinYMM: found.Bounds.MinYMM,
			MaxXMM: found.Bounds.MaxXMM, MaxYMM: found.Bounds.MaxYMM,
		}
	}

	for _, object := range found.Objects {
		outline := make([]httpapi.PrintPoint, 0, len(object.Polygon))
		for _, point := range object.Polygon {
			outline = append(outline, httpapi.PrintPoint{X: point.X, Y: point.Y})
		}

		out.Objects = append(out.Objects, httpapi.PrintObject{ID: object.ID, Name: object.Name, Outline: outline})
	}

	if found.Bed != (printfile.Bed{}) {
		out.Bed = &httpapi.PrintBed{
			WidthMM: found.Bed.WidthMM, DepthMM: found.Bed.DepthMM, HeightMM: found.Bed.HeightMM,
		}
	}

	if found.Checks != nil {
		checks := &httpapi.PrintChecks{
			PrinterModel: found.Checks.PrinterModel,
			PrinterCode:  found.Checks.PrinterCode,
			MinFirmware:  found.Checks.MinFirmware,
			GCodeLevel:   found.Checks.GCodeLevel,
			Features:     found.Checks.Features,
			Firmware:     found.Checks.Firmware,
		}

		for _, nozzle := range found.Checks.Nozzles {
			checks.Nozzles = append(checks.Nozzles, httpapi.PrintNozzleCheck{
				Index: nozzle.Index, DiameterMM: nozzle.DiameterMM,
				Hardened: nozzle.Hardened, HighFlow: nozzle.HighFlow,
			})
		}

		out.Checks = checks
	}
}

// moduleNeeds copies the per-filament needs of the stat route's details onto the
// module wire.
func moduleNeeds(out *storageapi.PrintInfo, details *httpapi.PrintDetails) {
	out.MaxLayerZMM = details.MaxLayerZMM

	for _, filament := range details.Filaments {
		out.Filaments = append(out.Filaments, storageapi.PrintFilament(filament))
	}

	if details.Bounds != nil {
		bounds := storageapi.PrintBounds(*details.Bounds)
		out.Bounds = &bounds
	}

	if details.Bed != nil {
		bed := storageapi.PrintBed(*details.Bed)
		out.Bed = &bed
	}

	for _, object := range details.Objects {
		outline := make([]storageapi.PrintPoint, 0, len(object.Outline))
		for _, point := range object.Outline {
			outline = append(outline, storageapi.PrintPoint(point))
		}

		out.Objects = append(out.Objects, storageapi.PrintObject{ID: object.ID, Name: object.Name, Outline: outline})
	}

	if details.Checks != nil {
		checks := &storageapi.PrintChecks{
			PrinterModel: details.Checks.PrinterModel,
			PrinterCode:  details.Checks.PrinterCode,
			MinFirmware:  details.Checks.MinFirmware,
			GCodeLevel:   details.Checks.GCodeLevel,
			Features:     details.Checks.Features,
			Firmware:     details.Checks.Firmware,
		}

		for _, nozzle := range details.Checks.Nozzles {
			checks.Nozzles = append(checks.Nozzles, storageapi.PrintNozzleCheck(nozzle))
		}

		out.Checks = checks
	}
}

// detailsFromModule reads what a module said a printer needs out of the
// shape the module wire carries, so a module's answer fills the details and
// the needs as the core's own reader does.
func detailsFromModule(info storageapi.PrintInfo) printfile.Details {
	out := printfile.Details{
		EstimatedSeconds: info.EstimatedSeconds,
		FilamentMM:       info.FilamentMM,
		FilamentG:        info.FilamentG,
		LayerHeightMM:    info.LayerHeightMM,
		NozzleMM:         info.NozzleMM,
		Material:         info.Material,
		PrinterModel:     info.PrinterModel,
		PrintProfile:     info.PrintProfile,
		PrinterProfile:   info.PrinterProfile,
		FilamentProfile:  info.FilamentProfile,
		HighFlow:         info.HighFlow,
		MaxLayerZMM:      info.MaxLayerZMM,
	}

	for _, filament := range info.Filaments {
		out.Filaments = append(out.Filaments, printfile.Filament(filament))
	}

	if info.Bounds != nil {
		bounds := printfile.Bounds(*info.Bounds)
		out.Bounds = &bounds
	}

	if info.Bed != nil {
		out.Bed = printfile.Bed(*info.Bed)
	}

	for _, object := range info.Objects {
		polygon := make([]printfile.Point, 0, len(object.Outline))
		for _, point := range object.Outline {
			polygon = append(polygon, printfile.Point(point))
		}

		out.Objects = append(out.Objects, printfile.LabelledObject{ID: object.ID, Name: object.Name, Polygon: polygon})
	}

	if info.Checks != nil {
		checks := &printfile.Checks{
			PrinterModel: info.Checks.PrinterModel,
			PrinterCode:  info.Checks.PrinterCode,
			MinFirmware:  info.Checks.MinFirmware,
			GCodeLevel:   info.Checks.GCodeLevel,
			Features:     info.Checks.Features,
			Firmware:     info.Checks.Firmware,
		}

		for _, nozzle := range info.Checks.Nozzles {
			checks.Nozzles = append(checks.Nozzles, printfile.NozzleCheck(nozzle))
		}

		out.Checks = checks
	}

	return out
}
