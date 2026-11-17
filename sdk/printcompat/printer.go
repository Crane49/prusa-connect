// Package printcompat says whether a print file fits a Prusa printer, the way
// the printer's firmware would judge it before it starts. It does no I/O.
//
// PrinterFromState reads what a printer has from its state tree.
// storageapi.PrintInfo is what a print file needs. Evaluate compares the two
// and returns a Verdict.
//
// The rules are the checks the public Prusa firmware makes before a print
// (gcode_compatibility.cpp in Prusa-Firmware-Buddy), plus one of this
// package: whether the print fits the build volume. A check whose facts are
// unknown on either side is reported as unverified, never as passed.
package printcompat

import (
	"encoding/json"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// Printer is what one printer has, as far as it said. A zero or a nil means
// the printer did not say.
type Printer struct {
	// Model is the printer's model as its device record names it, such as
	// MK4S or CoreOne.
	Model string
	// Firmware is the firmware version the printer reported.
	Firmware string
	// Tools is the printer's tool tree.
	Tools connectorapi.ToolTree
	// BuildVolume is the space a print may fill, and nil when the driver has
	// no figures for this machine.
	BuildVolume *Volume
}

// Volume is a build volume, in millimetres.
type Volume struct {
	XMM, YMM, ZMM float64
}

// PrinterFromState reads a printer's tools and build volume out of its state
// tree. A machine with one nozzle is head "0" there.
func PrinterFromState(model, firmware string, state map[string]json.RawMessage) Printer {
	return Printer{
		Model:       model,
		Firmware:    firmware,
		Tools:       connectorapi.ReadTools(state),
		BuildVolume: volumeAt(state),
	}
}

// volumeAt reads the build volume, and nil unless all three axes are known.
func volumeAt(state map[string]json.RawMessage) *Volume {
	x := positiveAt(state, connectorapi.BuildVolumeXPath)
	y := positiveAt(state, connectorapi.BuildVolumeYPath)
	z := positiveAt(state, connectorapi.BuildVolumeZPath)

	if x == 0 || y == 0 || z == 0 {
		return nil
	}

	return &Volume{XMM: x, YMM: y, ZMM: z}
}

func numberAt(state map[string]json.RawMessage, path string) *float64 {
	var value float64
	if raw, held := state[path]; !held || json.Unmarshal(raw, &value) != nil {
		return nil
	}

	return &value
}

func positiveAt(state map[string]json.RawMessage, path string) float64 {
	if value := numberAt(state, path); value != nil && *value > 0 {
		return *value
	}

	return 0
}
