package printfile

import "unsafe"

// Filament is what a print needs of one of its filaments. A file names each
// filament by the G-code tool number that selects it: T0 is the first. Which
// tool of a printer that lands on is the printer's to say, through its tool
// tree. Every field is optional, and a zero or a nil means the file did not
// say.
type Filament struct {
	// Index is n of the Tn that selects the filament, counted from 0.
	Index int
	// Used says whether the print extrudes this filament, by the firmware's own
	// rule: more than FilamentUsedMinG grams. It is nil when the file does not
	// say how much of each filament it uses.
	Used *bool
	// Material is the filament type, such as PLA.
	Material string
	// NozzleMM is the nozzle diameter the filament was sliced for.
	NozzleMM float64
	// HighFlow says whether the filament was sliced for a high-flow nozzle.
	HighFlow *bool
	// Abrasive says whether the filament wears a nozzle that is not hardened.
	Abrasive *bool
	// NozzleTempC and FirstLayerNozzleTempC are the nozzle temperatures the
	// filament profile prints at, in degrees Celsius.
	NozzleTempC           float64
	FirstLayerNozzleTempC float64
	// BedTempC and FirstLayerBedTempC are the bed temperatures the filament
	// profile asks for, in degrees Celsius.
	BedTempC           float64
	FirstLayerBedTempC float64
	// FilamentG and FilamentMM are how much of this filament the print uses.
	FilamentG  float64
	FilamentMM float64
	// Colour is the colour the slicer shows the filament in, such as #FF8000.
	Colour string
}

// FilamentUsedMinG is how many grams of a filament a print has to use for it
// to count as used. It is the firmware's threshold (FILAMENT_USED_MIN_G in
// src/common/gcode/gcode_info.hpp): a purge alone is about 0.06 g.
const FilamentUsedMinG = 0.0003

// Bounds is a rectangle on the bed, in millimetres from the bed's origin.
type Bounds struct {
	MinXMM, MinYMM, MaxXMM, MaxYMM float64
}

// Bed is the printable volume of the printer profile, in millimetres. A zero
// means the file did not say.
type Bed struct {
	WidthMM, DepthMM, HeightMM float64
}

// Checks is what the start of the G-code asks the printer to verify before it
// prints: the M862 lines, and the newest firmware the M115 line names.
type Checks struct {
	// PrinterModel is the model an M862.3 line names, such as MK4S or
	// MK4SMMU3.
	PrinterModel string
	// PrinterCode is the numeric model code an M862.2 line names.
	PrinterCode int
	// Nozzles is what each M862.1 line says, in the order the file writes
	// them.
	Nozzles []NozzleCheck
	// MinFirmware is the lowest firmware an M862.4 line accepts.
	MinFirmware string
	// GCodeLevel is the G-code level an M862.5 line names.
	GCodeLevel int
	// Features are the firmware features M862.6 lines name, such as
	// "Input shaper" and "MMU3", in the order the file writes them.
	Features []string
	// Firmware is the version an M115 U line names: the newest firmware the
	// slicer's profile knows.
	Firmware string
}

// NozzleCheck is one M862.1 line.
type NozzleCheck struct {
	// Index is n of the Tn the line names, counted from 0. A line that names
	// no T is about T0.
	Index int
	// DiameterMM is the nozzle diameter the line asks for, and zero when it
	// names none.
	DiameterMM float64
	// Hardened and HighFlow say whether the line asks for a hardened or a
	// high-flow nozzle, and are nil when the line does not say.
	Hardened *bool
	HighFlow *bool
}

// Empty reports whether the file said nothing this package could read.
func (d Details) Empty() bool {
	return d.EstimatedSeconds == 0 && d.FilamentMM == 0 && d.FilamentG == 0 &&
		d.LayerHeightMM == 0 && d.NozzleMM == 0 && d.Material == "" &&
		d.PrinterModel == "" && len(d.Filaments) == 0 && d.MaxLayerZMM == 0 &&
		d.Bounds == nil && d.Objects == nil && d.Bed == (Bed{}) && d.Checks == nil &&
		d.PrintProfile == "" && d.PrinterProfile == "" && d.FilamentProfile == "" && !d.HighFlow
}

// Bytes is about how much memory the details hold beyond the struct itself, so
// a cache that keeps them can count what it keeps.
func (d Details) Bytes() int {
	held := len(d.Material) + len(d.PrinterModel) + len(d.PrintProfile) +
		len(d.PrinterProfile) + len(d.FilamentProfile)

	for _, filament := range d.Filaments {
		held += int(unsafe.Sizeof(filament)) + len(filament.Material) + len(filament.Colour)
	}

	if d.Bounds != nil {
		held += int(unsafe.Sizeof(*d.Bounds))
	}

	for _, object := range d.Objects {
		held += int(unsafe.Sizeof(object)) + len(object.Name) +
			len(object.Polygon)*int(unsafe.Sizeof(Point{}))
	}

	if d.Checks != nil {
		held += int(unsafe.Sizeof(*d.Checks)) + len(d.Checks.PrinterModel) +
			len(d.Checks.MinFirmware) + len(d.Checks.Firmware) +
			len(d.Checks.Nozzles)*int(unsafe.Sizeof(NozzleCheck{}))

		for _, feature := range d.Checks.Features {
			held += int(unsafe.Sizeof(feature)) + len(feature)
		}
	}

	return held
}
