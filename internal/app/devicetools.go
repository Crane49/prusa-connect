package app

import (
	"encoding/json"
	"strconv"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/printcompat"
)

// toolsOf reads a printer's tool tree out of its state, the same reading a
// module makes with printcompat.PrinterFromState. It answers nil for a printer
// that has said nothing about its tools.
func toolsOf(read printcompat.Printer) *httpapi.DeviceTools {
	tree := read.Tools
	if len(tree.Tools) == 0 && !tree.CountKnown {
		return nil
	}

	out := &httpapi.DeviceTools{
		Tools: make(map[string]*httpapi.DeviceTool, len(tree.Tools)),
	}

	if tree.CountKnown {
		count := tree.Count
		out.ToolCount = &count
	}

	for n, id := range tree.GCodeToolMap {
		if out.GCodeToolMap == nil {
			out.GCodeToolMap = map[string]string{}
		}

		out.GCodeToolMap[strconv.Itoa(n)] = string(id)
	}

	for id, tool := range tree.Tools {
		if tool.Disabled {
			out.Tools[string(id)] = nil

			continue
		}

		out.Tools[string(id)] = deviceTool(tree, tool)
	}

	return out
}

// deviceTool is one enabled tool as the device record carries it.
func deviceTool(tree connectorapi.ToolTree, tool *connectorapi.Tool) *httpapi.DeviceTool {
	out := &httpapi.DeviceTool{
		Type:            tool.Type,
		Active:          tree.IsActive(tool.ID),
		NozzleTemp:      tool.NozzleTemp,
		PrintFanRPM:     tool.PrintFanRPM,
		HeatbreakFanRPM: tool.HeatbreakFanRPM,
		ConsumedWeight:  tool.ConsumedWeight,
		OdometerID:      tool.OdometerID,
		OdometerE:       tool.OdometerE,
		OdometerWeight:  tool.OdometerWeight,
	}

	// A heater that is off reports a target of 0, and the record carries null.
	switch {
	case tool.NozzleTargetTemp == nil:
	case tool.HeaterOff():
		out.NozzleTargetTemp = httpapi.NullOf[float64]()
	default:
		out.NozzleTargetTemp = httpapi.ValueOf(*tool.NozzleTargetTemp)
	}

	// The nulls of the record: a slot count of 0 is a tool known to have no
	// feeder, and NoMaterial a tool known to have nothing loaded. A tool with
	// a feeder carries no material of its own. A feeder whose slot count the
	// printer did not report is unknown.
	switch feeder := tool.Feeder; {
	case feeder == nil || !feeder.SlotCountKnown:
	case feeder.SlotCount == 0:
		out.Feeder = httpapi.NullOf[httpapi.DeviceFeeder]()
	default:
		out.Feeder = httpapi.ValueOf(httpapi.DeviceFeeder{
			SlotCount: feeder.SlotCount, Model: feeder.Model, BaseModel: feeder.BaseModel, Type: feeder.Type,
			MMUProgressCode: feeder.MMUProgressCode, MMUCommandInProgress: feeder.MMUCommandInProgress,
		})
	}

	switch {
	case tool.Material == nil || tool.HasFeeder():
	case *tool.Material == connectorapi.NoMaterial:
		out.MaterialPackageInstance = httpapi.NullOf[httpapi.DeviceMaterialPackageInstance]()
	default:
		out.MaterialPackageInstance = httpapi.ValueOf(httpapi.DeviceMaterialPackageInstance{
			Package: httpapi.DeviceMaterialPackage{Material: httpapi.DeviceMaterial{Abbreviation: *tool.Material}},
		})
	}

	if len(tool.Features) > 0 {
		out.Features = &httpapi.DeviceToolFeatures{
			NozzleDiameterMM: featureNumber(tool, "nozzle_diameter"),
			NozzleHighFlow:   featureBool(tool, "nozzle_high_flow"),
			NozzleHardened:   featureBool(tool, "nozzle_hardened"),
			NozzleHighTemp:   featureBool(tool, "nozzle_high_temp"),
		}
	}

	return out
}

func featureNumber(tool *connectorapi.Tool, name string) *float64 {
	raw, ok := tool.Feature(name)

	var value float64
	if !ok || json.Unmarshal(raw, &value) != nil {
		return nil
	}

	return &value
}

func featureBool(tool *connectorapi.Tool, name string) *bool {
	raw, ok := tool.Feature(name)

	var value bool
	if !ok || json.Unmarshal(raw, &value) != nil {
		return nil
	}

	return &value
}

// buildVolumeOf is the volume the driver reported, and nil when it reported
// none.
func buildVolumeOf(read printcompat.Printer) *httpapi.DeviceBuildVolume {
	if read.BuildVolume == nil {
		return nil
	}

	return &httpapi.DeviceBuildVolume{
		XMM: read.BuildVolume.XMM, YMM: read.BuildVolume.YMM, ZMM: read.BuildVolume.ZMM,
	}
}

// toolFactsOf reads the tools and the build volume out of one state tree.
func toolFactsOf(model string, state map[string]json.RawMessage) (*httpapi.DeviceTools, *httpapi.DeviceBuildVolume) {
	read := printcompat.PrinterFromState(model, "", state)

	return toolsOf(read), buildVolumeOf(read)
}
