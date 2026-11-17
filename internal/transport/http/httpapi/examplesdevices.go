package httpapi

import (
	"encoding/json"
	"time"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/deviceissue"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// The device fixtures every example is built from: the printers, their jobs,
// their issues, their capability set, and the resource and preset beside them.
//
// They live apart from examples.go for the reason the other examples files do,
// which is that the operation table there is the set the gate reads and a
// reader looking for what a printer example actually contains is not looking
// through it.

// The values every printer example is built from. They are one real CoreOne:
// the identity is what the xbuddy dialect reports, the id is what the device
// plane minted for it, and the capability set is what the shipped profile and
// that driver's baseline resolve to for this model on this firmware.
const (
	// examplePrinterID is a version 4 UUID, which is what the device plane
	// mints when a printer's registration is approved. There is
	// nothing to derive it from and nothing to check it against: an example id
	// is one draw of a random id, and the machine behind it is named by
	// unit_uid and serial beside it.
	examplePrinterID = "9f2a4c18-6d3b-4e07-b5a1-8c0f2d7e4b93"
	examplePrinterFW = "6.4.0-0"
	exampleSerial    = "29876543"
	exampleModel     = "CoreOne"
	exampleDriver    = "xbuddy"
	exampleVendor    = "Prusa"

	// The second printer on the listing example, the one that has stopped. Its
	// id is a second draw and bears no relation to the first, which is the
	// whole of what a minted id promises.
	exampleStoppedPrinterID = "3c81d64a-0f52-4b9e-8a17-5d2e6f0b1c74"
	exampleStoppedSerial    = "29876544"

	// exampleAuthorizationEpoch is the generation of the authorization graph
	// every example's action set was read at. One number across the examples,
	// so a reader comparing two of them sees the same graph in both.
	exampleAuthorizationEpoch = 7
)

// exampleDeviceName is what an operator called the printer in the examples. It
// says where the machine is and what it is for, which is what a person types
// when a fleet has more than one of a model.
const exampleDeviceName = "Workshop CoreOne"

// exampleTime is the instant every timestamp in an example carries. One instant
// rather than several, because an example that shows five unrelated times reads
// as five facts and is one.
var exampleTime = time.Date(2026, time.August, 26, 9, 14, 5, 0, time.UTC)

// The values the example tool points at.
//
//nolint:gochecknoglobals // example values, read-only.
var (
	exampleFalse        = false
	exampleNozzle       = 214.8
	exampleNozzleTarget = 215.0
	exampleNozzleMM     = 0.4
	exampleToolCount    = 1
)

// examplePrinter is one connected printer mid-print. The state paths are the
// ones the xbuddy driver publishes and the values are passed through as the
// printer encoded them, so a number stays a number and the state stays a string.
func examplePrinter() Device {
	return Device{
		Kind:            "printer",
		Tags:            []string{"workshop", "PETG only"},
		Labels:          map[string]string{"workshop": "", "PETG only": ""},
		ID:              examplePrinterID,
		Name:            exampleModel + " " + exampleSerial,
		Vendor:          exampleVendor,
		Model:           exampleModel,
		Firmware:        examplePrinterFW,
		Serial:          exampleSerial,
		Driver:          exampleDriver,
		Connected:       true,
		Capabilities:    exampleCapabilities(),
		CapabilityEpoch: 1,

		Actions: []string{"observe", "operate"},
		// What observe and operate reach, read from the table the handler
		// reads: an operator who does not own the printer, so no flash.
		Verbs: authz.VerbsReached(authz.DeviceKindPrinter,
			authz.MaskOf([]authz.Action{authz.ActionObserve, authz.ActionOperate}), nil),
		AuthorizationEpoch: exampleAuthorizationEpoch,

		ProfileMatched: true,
		State: DeviceState{
			"/state":                             json.RawMessage(`"PRINTING"`),
			"/temperature/bed":                   json.RawMessage(`59.9`),
			"/temperature/bed_target":            json.RawMessage(`60`),
			"/axis/x":                            json.RawMessage(`132.4`),
			"/axis/y":                            json.RawMessage(`98.1`),
			"/axis/z":                            json.RawMessage(`4.2`),
			"/speed":                             json.RawMessage(`100`),
			"/flow":                              json.RawMessage(`100`),
			"/job/id":                            json.RawMessage(`41`),
			"/job/state":                         json.RawMessage(`"PRINTING"`),
			"/job/path":                          json.RawMessage(`"` + examplePrintPath + `"`),
			"/job/progress":                      json.RawMessage(`37`),
			"/job/time_printing":                 json.RawMessage(`1284`),
			"/job/time_remaining":                json.RawMessage(`2160`),
			"/tool_count":                        json.RawMessage(`1`),
			"/tools/0/enabled":                   json.RawMessage(`true`),
			"/tools/0/type":                      json.RawMessage(`"FFF_PRINT_HEAD"`),
			"/tools/0/active":                    json.RawMessage(`true`),
			"/tools/0/nozzle_temp":               json.RawMessage(`214.8`),
			"/tools/0/nozzle_target_temp":        json.RawMessage(`215`),
			"/tools/0/features/nozzle_diameter":  json.RawMessage(`0.40`),
			"/tools/0/features/nozzle_high_flow": json.RawMessage(`false`),
			"/tools/0/features/nozzle_hardened":  json.RawMessage(`false`),
			"/tools/0/feeder/slot_count":         json.RawMessage(`0`),
			"/tools/0/material_package_instance/package/material/abbreviation": json.RawMessage(`"PLA"`),
			"/build_volume/x": json.RawMessage(`250`),
			"/build_volume/y": json.RawMessage(`220`),
			"/build_volume/z": json.RawMessage(`270`),
		},
		BuildVolume: &DeviceBuildVolume{XMM: 250, YMM: 220, ZMM: 270},
		Tools: &DeviceTools{
			ToolCount: &exampleToolCount,
			Tools: map[string]*DeviceTool{"0": {
				Type: "FFF_PRINT_HEAD", Active: true,
				Features: &DeviceToolFeatures{
					NozzleDiameterMM: &exampleNozzleMM, NozzleHighFlow: &exampleFalse, NozzleHardened: &exampleFalse,
				},
				Feeder: NullOf[DeviceFeeder](),
				MaterialPackageInstance: ValueOf(DeviceMaterialPackageInstance{
					Package: DeviceMaterialPackage{Material: DeviceMaterial{Abbreviation: "PLA"}},
				}),
				NozzleTemp: &exampleNozzle, NozzleTargetTemp: ValueOf(exampleNozzleTarget),
			}},
		},
		Job: &DeviceJobSummary{
			ID:              exampleJobID,
			DeviceJobID:     "41",
			Path:            examplePrintPath,
			DisplayName:     "Benchy",
			State:           "PRINTING",
			Progress:        37,
			TimeRemainingMS: 2_160_000,
			Running:         true,
		},
		Link: DeviceLink{
			Counted:             true,
			MeasuredOverSeconds: 8.4,
		},
		Issues: []DeviceIssue{},
	}
}

// examplePrintPath is the file the example printer is printing, in the
// machine's own absolute spelling. It is the machine's path and not the one a
// caller sends to start_print, which is relative to the storage.
const examplePrintPath = "/usb/benchy.bgcode"

// exampleDeviceJobs is what the jobs route answers for a machine printing its
// second job: the one on the bed, and the one before it.
// Every instant is derived from exampleTime, which is the one instant every
// example carries. A job is the one shape here that needs more than one: a
// print that started and ended at the same moment is not a print, and the row
// before it has to be older than the row after it or "last" reads as a guess.
func exampleDeviceJobs() DeviceJobs {
	previousStarted := exampleTime.Add(-time.Hour)
	previousEnded := exampleTime.Add(-10 * time.Minute)

	return DeviceJobs{
		Current: &DeviceJob{
			ID:              exampleJobID,
			DeviceJobID:     "41",
			DeviceID:        examplePrinterID,
			Path:            examplePrintPath,
			DisplayName:     "Benchy",
			State:           "PRINTING",
			Progress:        37,
			TimeRemainingMS: 2_160_000,
			Started:         exampleTime,
		},
		Last: &DeviceJob{
			ID:              "0199e2c1-0405-7e48-b0af-e812537ad808",
			DeviceJobID:     "40",
			DeviceID:        examplePrinterID,
			Path:            "/usb/examples/bracket.bgcode",
			DisplayName:     "Bracket",
			State:           "FINISHED",
			Progress:        100,
			TimeRemainingMS: 0,
			Started:         previousStarted,
			Ended:           &previousEnded,
			ObjectCount:     &exampleObjectCount,
			Objects:         exampleJobObjects(),
		},
	}
}

// exampleObjectCount is how many objects the finished example print had.
//
//nolint:gochecknoglobals // a sample value, read-only; a pointer field needs an address.
var exampleObjectCount = 3

// exampleJobObjects is the finished example print's objects: three brackets,
// one of which the operator skipped.
func exampleJobObjects() []JobObject {
	return []JobObject{
		{ID: 0, Cancelled: false},
		{ID: 1, Cancelled: true},
		{ID: 2, Cancelled: false},
	}
}

// exampleDeviceJobHistory is a page of the prints that have ended on the
// example printer, newest first. It carries a cursor, because a page that
// showed none would show a reader the empty case rather than how paging works.
//
// The current print is deliberately absent. It has no end, so it is the jobs
// read's current and never a row here.
func exampleDeviceJobHistory() DeviceJobPage {
	firstStarted := exampleTime.Add(-time.Hour)
	firstEnded := exampleTime.Add(-10 * time.Minute)
	secondStarted := exampleTime.Add(-5 * time.Hour)
	secondEnded := exampleTime.Add(-4 * time.Hour)

	return DeviceJobPage{
		Limit: 50,
		Items: []DeviceJob{
			{
				ID:          "0199e2c1-0405-7e48-b0af-e812537ad808",
				DeviceJobID: "40",
				DeviceID:    examplePrinterID,
				Path:        "/usb/examples/bracket.bgcode",
				DisplayName: "Bracket",
				State:       "FINISHED",
				Progress:    100,
				Started:     firstStarted,
				Ended:       &firstEnded,
			},
			{
				ID:              "0199e270-7e21-72ab-bdc2-64acbf01f91b",
				DeviceJobID:     "39",
				DeviceID:        examplePrinterID,
				Path:            "/usb/examples/clip.bgcode",
				DisplayName:     "Clip",
				State:           "STOPPED",
				Progress:        17,
				TimeRemainingMS: 1_980_000,
				Started:         secondStarted,
				Ended:           &secondEnded,
			},
		},
		Cursor: exampleJobCursor,
	}
}

// exampleJobPage is a page of the fleet's prints: the one running on the
// example printer, then the ones it finished. The running print leads because
// it started last, and it carries no end.
func exampleJobPage() JobPage {
	current := exampleDeviceJobs().Current
	ended := exampleDeviceJobHistory().Items

	return JobPage{
		Limit:  50,
		Items:  append([]DeviceJob{*current}, ended...),
		Cursor: exampleJobCursor,
	}
}

// exampleJobCursor is what this server minted for the page above. It is opaque
// on the wire, and it is a constant here so the sample a reader copies is the
// one the round trip sends.
const exampleJobCursor = "MTc1NjE4MDQ0NTAwMAA5ZjJhNGMxOC02ZDNiLTRlMDctYjVhMS04YzBmMmQ3ZTRiOTM6Mzk"

// exampleStoppedPrinter is the second printer on the listing example: one that
// has stopped and is waiting for somebody. A listing example whose every
// printer is fine shows a reader the members but not the case they are reading
// the listing to find.
func exampleStoppedPrinter() Device {
	stopped := examplePrinter()

	stopped.ID = exampleStoppedPrinterID
	stopped.Name = exampleModel + " " + exampleStoppedSerial
	stopped.Serial = exampleStoppedSerial
	stopped.State = DeviceState{
		"/state":         json.RawMessage(`"ATTENTION"`),
		"/dialog/id":     json.RawMessage(`"42"`),
		"/dialog/code":   json.RawMessage(`"13501"`),
		"/dialog/title":  json.RawMessage(`"Filament runout"`),
		"/dialog/text":   json.RawMessage(`"Insert filament, then press Retry."`),
		"/dialog/button": json.RawMessage(`["Retry","Cancel"]`),
	}
	stopped.Issues = []DeviceIssue{
		{Reason: "awaiting_operator", Severity: "error", Detail: "Filament runout"},
	}

	return stopped
}

// exampleIssueSummary is the rollup over the twelve printers the listing
// example says the caller may see. One of them is stopped, and the reasons at
// zero are published too, which is what the member promises.
func exampleIssueSummary() DeviceIssueSummary {
	counts := map[string]int{}
	for _, doc := range deviceissue.Docs() {
		counts[doc.Reason.String()] = 0
	}

	counts[deviceissue.ReasonAwaitingOperator.String()] = 1

	return DeviceIssueSummary{
		Affected:   1,
		Counts:     counts,
		BySeverity: map[string]int{"error": 1, "warning": 0, "info": 0},
	}
}

// exampleCapabilities is the resolved set for a CoreOne on 6.4.0-0, taken from
// the shipped profile and the xbuddy baseline. Eighty-five keys, of which
// forty-five are verbs and the rest are the arguments each verb accepts.
//
// It is here in full rather than as a sample, because the count is the point: a
// reader who sees six keys writes a client that assumes six.
//
//nolint:funlen // one line per capability key, and a shorter form would hide the set.
func exampleCapabilities() []string {
	return []string{
		"core.command.beep",
		"core.command.cancel_object",
		"core.command.cancel_object#id",
		"core.command.cancel_printer_ready",
		"core.command.create_folder",
		"core.command.create_folder#path",
		"core.command.delete_file",
		"core.command.delete_file#path",
		"core.command.delete_folder",
		"core.command.delete_folder#path",
		"core.command.dialog_action",
		"core.command.dialog_action#button",
		"core.command.dialog_action#dialog_id",
		"core.command.disable_steppers",
		"core.command.flash",
		"core.command.flash#path",
		"core.command.force_gcode",
		"core.command.force_gcode#gcode",
		"core.command.home",
		"core.command.home#axis",
		"core.command.load_filament",
		"core.command.load_filament#filament",
		"core.command.mesh_bed_leveling",
		"core.command.move",
		"core.command.move#feedrate",
		"core.command.move#x",
		"core.command.move#y",
		"core.command.move_e",
		"core.command.move_e#feedrate",
		"core.command.move_e#length",
		"core.command.move_z",
		"core.command.move_z#distance",
		"core.command.move_z#feedrate",
		"core.command.pause_print",
		"core.command.reset",
		"core.command.reset_printer",
		"core.command.resume_print",
		"core.command.send_file_info",
		"core.command.send_file_info#path",
		"core.command.send_gcode",
		"core.command.send_gcode#gcode",
		"core.command.send_info",
		"core.command.send_job_info",
		"core.command.send_job_info#job_id",
		"core.command.send_state_info",
		"core.command.set_addon_power",
		"core.command.set_addon_power#addon_power",
		"core.command.set_chamber_fan_target",
		"core.command.set_chamber_fan_target#chamber.fan_pwm_target",
		"core.command.set_chamber_led_intensity",
		"core.command.set_chamber_led_intensity#chamber.led_intensity",
		"core.command.set_chamber_target_temp",
		"core.command.set_chamber_target_temp#chamber.target_temp",
		"core.command.set_flow",
		"core.command.set_flow#flow",
		"core.command.set_bed_temperature",
		"core.command.set_bed_temperature#bed_temperature",
		"core.command.set_hostname",
		"core.command.set_hostname#hostname",
		"core.command.set_idle",
		"core.command.set_nozzle_temperature",
		"core.command.set_nozzle_temperature#nozzle_temperature",
		"core.command.set_printer_ready",
		"core.command.set_speed",
		"core.command.set_speed#speed",
		"core.command.set_token",
		"core.command.set_token#token",
		"core.command.set_tool_hardened",
		"core.command.set_tool_hardened#hardened",
		"core.command.set_tool_hardened#tool",
		"core.command.set_tool_high_flow",
		"core.command.set_tool_high_flow#high_flow",
		"core.command.set_tool_high_flow#tool",
		"core.command.set_tool_nozzle_diameter",
		"core.command.set_tool_nozzle_diameter#nozzle_diameter",
		"core.command.set_tool_nozzle_diameter#tool",
		"core.command.start_print",
		"core.command.start_print#path",
		"core.command.start_print#tool_mapping",
		"core.command.stop_print",
		"core.command.uncancel_object",
		"core.command.uncancel_object#id",
		"core.command.unload_filament",
		"core.command.upgrade",
		"core.command.upgrade#path",
	}
}

// exampleResource is the storage instance a fresh install declares.
func exampleResource() ResourceInstance {
	return ResourceInstance{
		ResourceSpec: ResourceSpec{
			Type:     "local",
			Enabled:  true,
			Required: true,
			Config:   ResourceConfig(`{"root":"/var/lib/prusa-connect/files"}`),
		},
		Kind:       exampleResourceKind,
		Name:       exampleResourceName,
		UID:        "01K3Q7X8N4B2VJ5R0YH6M9CDEF",
		Runtime:    "builtin",
		Source:     "overlay",
		Version:    "v7",
		Generation: 3,
		Status: ResourceStatus{
			State:              "ready",
			Serving:            true,
			ObservedGeneration: 3,
			Conditions: []ResourceCondition{{
				Type:               "Started",
				Status:             "True",
				Reason:             "Started",
				LastTransitionTime: exampleTime,
				ObservedGeneration: 3,
			}},
		},
	}
}

// exampleAssetResource is a module that runs no process and draws in the
// browser: the second row of the listing, so the reference shows the ui object
// as well as the instance without one (ADR-20260911-4p6 rule 32).
func exampleAssetResource() ResourceInstance {
	return ResourceInstance{
		ResourceSpec: ResourceSpec{
			Type:                "com.github.aux.acme.planner/spools",
			Enabled:             true,
			Config:              ResourceConfig(`{}`),
			HiddenContributions: []string{"device_card/spools"},
		},
		Kind:       "asset",
		Name:       "spools",
		UID:        "01K3Q7X8N4B2VJ5R0YH6M9CDEG",
		Runtime:    "asset",
		Namespace:  exampleNamespace,
		Source:     "overlay",
		Version:    "v7",
		Generation: 1,
		Module: &ResourceModule{
			Version: "1.4.0",
			Summary: "Counts what each print used and warns before a spool runs out.",
			Author:  "acme",
		},
		UI: &ResourceUI{
			EntryURL: ModuleUIPath(exampleNamespace) + "ui/index.js",
			Library:  ">=0.3.0 <1.0.0",
			Contributes: ResourceContributes{
				Pages:      []ResourcePage{{Path: "spools", Title: "Spools", Icon: "layers"}},
				DeviceCard: []ResourceSection{{ID: "spool-status", Title: "Loaded spool"}},
				Settings:   []ResourceSection{{ID: "spools", Title: "Spools"}},
			},
		},
		Status: ResourceStatus{
			State:              "ready",
			Serving:            true,
			ObservedGeneration: 1,
			Conditions: []ResourceCondition{{
				Type:               "Started",
				Status:             "True",
				Reason:             "Started",
				LastTransitionTime: exampleTime,
				ObservedGeneration: 1,
			}},
		},
	}
}

// exampleGCodePreset is a maintenance macro an operator stored.
func exampleGCodePreset() GCodePreset {
	return GCodePreset{
		GCodePresetSpec: GCodePresetSpec{
			Summary: "Warm the nozzle and the bed for PLA, then home.",
			Models:  []string{"CoreOne", "MK4S"},
			Steps:   []string{"M104 S215", "M140 S60", "G28"},
		},
		Name:    examplePresetName,
		Version: "v7",
	}
}

// exampleFilters is what the listing example's caller is offered: the values
// present in their fleet, each counted under every filter but its own.
func exampleFilters() []DeviceFilter {
	return []DeviceFilter{
		{Param: "search", Kind: "text", MaxLength: 96},
		{Param: "state", Kind: "multi", Values: []DeviceFilterValue{
			{Value: "IDLE", Count: 10}, {Value: "STOPPED", Count: 2},
		}},
		{Param: "model", Kind: "multi", Values: []DeviceFilterValue{{Value: "XL", Count: 12}}},
		{Param: "severity", Kind: "multi", Values: []DeviceFilterValue{
			{Value: "error", Count: 1}, {Value: "warning", Count: 0}, {Value: "info", Count: 0},
		}},
		{Param: "tag", Kind: "multi", Values: []DeviceFilterValue{{Value: "shop-a", Count: 7}}},
	}
}

// exampleSorts is every field the listing may be ordered by.
func exampleSorts() []DeviceSort {
	return []DeviceSort{
		{Field: "urgency", DefaultOrder: "asc"},
		{Field: "name", DefaultOrder: "asc"},
		{Field: "model", DefaultOrder: "asc"},
		{Field: "state", DefaultOrder: "asc"},
		{Field: "last_seen", DefaultOrder: "desc"},
		{Field: "id", DefaultOrder: "asc"},
	}
}

// exampleDeviceCapabilities is four verbs of the example printer, written out
// by the projection the endpoint runs: a temperature with its unit and an
// optional head, a percentage, a closed set and a verb that takes nothing. The
// bounds are the xbuddy driver's.
func exampleDeviceCapabilities() DeviceCapabilities {
	return deviceCapabilitiesOf(examplePrinterID, exampleDeviceCapabilitySet())
}

// exampleDeviceCapabilitySet is the descriptor the example above projects.
func exampleDeviceCapabilitySet() DeviceCapabilitySet {
	return DeviceCapabilitySet{
		Epoch:          1,
		ProfileMatched: true,
		Set: connectorapi.Set{
			"core.command.beep": {Flag: true},
			"core.command.home": {Flag: true},
			"core.command.home#axis": {
				Flag:      true,
				Kind:      connectorapi.KindString,
				Supported: []string{"X", "XY", "XYZ"},
			},
			"core.command.set_nozzle_temperature": {Flag: true},
			"core.command.set_nozzle_temperature#nozzle_temperature": {
				Flag: true, Kind: connectorapi.KindInteger, Required: true,
				Min: 0, HasMin: true, Limit: 300, HasLimit: true, Unit: connectorapi.UnitCelsius,
			},
			"core.command.set_nozzle_temperature#tool": {
				Flag: true, Kind: connectorapi.KindInteger, Min: 0, HasMin: true, Limit: 7, HasLimit: true,
			},
			"core.command.set_speed": {Flag: true},
			"core.command.set_speed#speed": {
				Flag: true, Kind: connectorapi.KindInteger, Required: true,
				Min: 20, HasMin: true, Limit: 999, HasLimit: true, Unit: connectorapi.UnitPercent,
			},
		},
	}
}
