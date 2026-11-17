package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// dialInTools runs one simulated printer against a core until the case ends.
func dialInTools(t *testing.T, running *process, cfg simulator.Config) {
	t.Helper()

	dialInSimulator(t, running, cfg)
}

// dialInSimulator is dialInTools, and hands back the printer so a test can
// change it while it runs.
func dialInSimulator(t *testing.T, running *process, cfg simulator.Config) *simulator.Printer {
	t.Helper()

	cfg.BaseURL = "http://" + running.addr

	printer, err := simulator.New(cfg)
	require.NoError(t, err)

	ctx, stop := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan error, 1)

	go func() { done <- printer.RegisterAndRun(ctx) }()

	t.Cleanup(func() {
		stop()
		<-done
	})

	return printer
}

// toolsOfListing waits for the one printer listed to report the tools named.
func toolsOfListing(t *testing.T, running *process, ids ...string) httpapi.Device {
	t.Helper()

	return listingWhere(t, running, func(c *assert.CollectT, device httpapi.Device) {
		for _, id := range ids {
			assert.NotNil(c, device.Tools.Tools[id], "tool %s is not listed", id)
		}
	})
}

// listingWhere waits for the one printer listed to pass check, and hands it
// back.
//
// A tool is listed once any path under it is in the tree, and INFO is what
// says what the tool is. Telemetry can name the tools before INFO arrives,
// and the patches of one INFO land one at a time. A case that reads what
// INFO says therefore checks it here, and not once the tools are listed.
func listingWhere(t *testing.T, running *process, check func(*assert.CollectT, httpapi.Device)) httpapi.Device {
	t.Helper()

	var found httpapi.Device

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		listed := printers(t, running)
		if !assert.Len(c, listed, 1) || !assert.NotNil(c, listed[0].Tools, "no tools are listed") {
			return
		}

		check(c, listed[0])

		found = listed[0]
	}, settle, 25*time.Millisecond, "the printer's tools never reached the listing")

	return found
}

// TestTheToolsOfAPrinterReachTheAPIAndSurviveARestart is the whole path of a
// tool fact: an XL's INFO over the frozen wire, the state tree, the listing,
// the store, and the listing of a core started again while the printer is
// away.
func TestTheToolsOfAPrinterReachTheAPIAndSurviveARestart(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	first := start(t, dir, "core:\n  log:\n    level: error\n")

	dialInTools(t, first, simulator.Config{Model: "XL", Serial: "SIM-XL-TOOLS"})

	listingWhere(t, first, func(c *assert.CollectT, before httpapi.Device) {
		second := before.Tools.Tools["1"]
		if !assert.NotNil(c, second, "tool 1 is not listed") ||
			!assert.NotNil(c, second.Features, "tool 1 has no nozzle") {
			return
		}

		assert.Equal(c, new(5), before.Tools.ToolCount)
		assert.Equal(c, "FFF_PRINT_HEAD", second.Type)

		if assert.NotNil(c, second.Features.NozzleDiameterMM) {
			assert.InDelta(c, 0.4, *second.Features.NozzleDiameterMM, 1e-9)
		}

		assert.Equal(c, new(true), second.Features.NozzleHighFlow)
		assert.Equal(c, new(true), second.Features.NozzleHardened)
		assert.Equal(c, "PETG", abbreviationIn(second))
		assert.True(c, second.Feeder.Known, "an XL head says it has no feeder")
		assert.Nil(c, second.Feeder.Value, "an XL head has no feeder")
		assert.Equal(c, &httpapi.DeviceBuildVolume{XMM: 360, YMM: 360, ZMM: 360}, before.BuildVolume)
	})

	// The flush is on a timer, and a clean stop writes once more on its way
	// out, so the tree the listing showed is the tree the store keeps.
	first.stopAndWait(t)

	again := start(t, dir, "core:\n  log:\n    level: error\n")

	after := printers(t, again)
	require.Len(t, after, 1)
	require.False(t, after[0].Connected, "nothing has reached the printer since the restart")
	require.NotNil(t, after[0].Tools, "the tools were forgotten by a restart")
	require.Equal(t, 5, *after[0].Tools.ToolCount)
	require.Len(t, after[0].Tools.Tools, 5)
	require.Equal(t, "PETG", abbreviationIn(after[0].Tools.Tools["1"]))
	require.NotNil(t, after[0].BuildVolume, "the volume was forgotten by a restart")
}

// abbreviationIn is the abbreviation of the material loaded in a tool, or
// nothing when no material is.
func abbreviationIn(tool *httpapi.DeviceTool) string {
	if tool == nil || tool.MaterialPackageInstance.Value == nil {
		return ""
	}

	return tool.MaterialPackageInstance.Value.Package.Material.Abbreviation
}

// TestAnMMUShowsAsOneHeadWithFiveSlots reads the listing of an MK4S with an
// MMU3.
func TestAnMMUShowsAsOneHeadWithFiveSlots(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	dialInTools(t, running, simulator.Config{Model: "MK4S", Serial: "SIM-MMU", MMU: true})

	listingWhere(t, running, func(c *assert.CollectT, found httpapi.Device) {
		head, empty := found.Tools.Tools["0"], found.Tools.Tools["0.4"]
		if !assert.NotNil(c, head, "the head is not listed") || !assert.NotNil(c, empty, "slot 4 is not listed") {
			return
		}

		assert.Equal(c, new(1), found.Tools.ToolCount)

		if assert.NotNil(c, head.Feeder.Value, "the head has no feeder") {
			assert.Equal(c, 5, head.Feeder.Value.SlotCount)
		}

		assert.False(c, head.MaterialPackageInstance.Known, "a head with a feeder carries no material")
		assert.Equal(c, "PETG", abbreviationIn(found.Tools.Tools["0.1"]))
		assert.True(c, empty.MaterialPackageInstance.Known, "an empty slot is a known empty tool")
		assert.Nil(c, empty.MaterialPackageInstance.Value, "an empty slot has no material package instance")
	})
}

// TestToolsOfReadsTheTree covers the rendering on its own: a printer that has
// said nothing about its tools has no tools member at all, a disabled tool is
// null, and the feeder and the material keep the architecture's three
// readings: left out, null and a value.
func TestToolsOfReadsTheTree(t *testing.T) {
	t.Parallel()

	none, noVolume := toolFactsOf("MK4", map[string]json.RawMessage{"/state": json.RawMessage(`"IDLE"`)})
	require.Nil(t, none)
	require.Nil(t, noVolume)

	got, _ := toolFactsOf("XL", map[string]json.RawMessage{
		"/tool_count":                       json.RawMessage(`2`),
		"/tools/0/features/nozzle_diameter": json.RawMessage(`0.6`),
		"/tools/1/enabled":                  json.RawMessage(`false`),
		"/tools/0/nozzle_temp":              json.RawMessage(`215`),
		"/tools/0/feeder/slot_count":        json.RawMessage(`0`),
		"/tools/0/material_package_instance/package/material/abbreviation": json.RawMessage(`"PLA"`),
		"/tools/2/enabled": json.RawMessage(`true`),
		"/tools/2/material_package_instance/package/material/abbreviation": json.RawMessage(`""`),
		"/tools/3/enabled": json.RawMessage(`true`),
	})
	require.NotNil(t, got)
	require.Equal(t, 2, *got.ToolCount)
	require.Len(t, got.Tools, 4)
	require.InDelta(t, 0.6, *got.Tools["0"].Features.NozzleDiameterMM, 1e-9)
	require.InDelta(t, 215, *got.Tools["0"].NozzleTemp, 1e-9)

	raw, err := json.Marshal(got)
	require.NoError(t, err)

	var tools map[string]map[string]json.RawMessage

	require.NoError(t, json.Unmarshal(raw, &struct {
		Tools *map[string]map[string]json.RawMessage `json:"tools"`
	}{&tools}))
	require.Contains(t, tools, "1")
	require.Nil(t, tools["1"], "a disabled tool is null")
	require.JSONEq(t, `null`, string(tools["0"]["feeder"]), "a slot count of 0 is the null feeder")
	require.JSONEq(t, `{"package":{"material":{"abbreviation":"PLA"}}}`,
		string(tools["0"]["material_package_instance"]), "the material is nested as the architecture nests it")
	require.JSONEq(t, `null`, string(tools["2"]["material_package_instance"]), "nothing loaded is null")

	for _, unsaid := range []string{"feeder", "material_package_instance"} {
		require.NotContains(t, tools["3"], unsaid, "a member the printer did not report is left out")
	}

	var back httpapi.DeviceTools

	require.NoError(t, json.Unmarshal(raw, &back))
	require.Equal(t, got, &back, "a client reads the three readings back")
}

// TestToolsOfMarksEachActiveToolAndAnOffHeater holds the architecture's
// telemetry on the device record: each tool's own active member, a slot
// loaded into a head that is not picked, and no active tool of the whole printer,
// a heater that is off as a null target, and a G-code tool map the driver
// reported.
func TestToolsOfMarksEachActiveToolAndAnOffHeater(t *testing.T) {
	t.Parallel()

	got, _ := toolFactsOf("XL", map[string]json.RawMessage{
		"/tool_count":                 json.RawMessage(`2`),
		"/tools/0/active":             json.RawMessage(`true`),
		"/tools/0/nozzle_target_temp": json.RawMessage(`215`),
		"/tools/0/feeder/slot_count":  json.RawMessage(`2`),
		"/tools/0.1/active":           json.RawMessage(`true`),
		"/tools/1/active":             json.RawMessage(`false`),
		"/tools/1/nozzle_target_temp": json.RawMessage(`0`),
		"/tools/1/feeder/slot_count":  json.RawMessage(`2`),
		"/tools/1.0/active":           json.RawMessage(`true`),
		"/tools/1.1/active":           json.RawMessage(`false`),
		"/tools/1.1/nozzle_temp":      json.RawMessage(`20`),
		"/gcode_tool_map/3":           json.RawMessage(`"1.0"`),
	})
	require.NotNil(t, got)
	require.Equal(t, map[string]string{"3": "1.0"}, got.GCodeToolMap)
	require.True(t, got.Tools["0"].Active)
	require.False(t, got.Tools["1"].Active)
	require.True(t, got.Tools["1.0"].Active, "a slot loaded into a head that is not picked is active")
	require.False(t, got.Tools["1.1"].Active)

	raw, err := json.Marshal(got)
	require.NoError(t, err)

	var tools map[string]map[string]json.RawMessage

	require.NoError(t, json.Unmarshal(raw, &struct {
		Tools *map[string]map[string]json.RawMessage `json:"tools"`
	}{&tools}))
	require.NotContains(t, string(raw), "active_tool", "the printer as a whole names no active tool")
	require.JSONEq(t, `215`, string(tools["0"]["nozzle_target_temp"]))
	require.JSONEq(t, `null`, string(tools["1"]["nozzle_target_temp"]), "a heater that is off is a null target")
	require.NotContains(t, tools["1.1"], "nozzle_target_temp", "a target the printer did not report is left out")
}

// TestToolsOfCarriesTheFeederAndTheOdometers holds the feeder's names, what
// its unit is doing and each tool's odometers on the device record, and a
// feeder whose slot count the printer did not report left out.
func TestToolsOfCarriesTheFeederAndTheOdometers(t *testing.T) {
	t.Parallel()

	got, _ := toolFactsOf("MK4S", map[string]json.RawMessage{
		"/tools/0/feeder/slot_count":              json.RawMessage(`5`),
		"/tools/0/feeder/model":                   json.RawMessage(`"MMU3"`),
		"/tools/0/feeder/base_model":              json.RawMessage(`"MMU3"`),
		"/tools/0/feeder/type":                    json.RawMessage(`"MMU"`),
		"/tools/0/feeder/mmu_progress_code":       json.RawMessage(`17`),
		"/tools/0/feeder/mmu_command_in_progress": json.RawMessage(`"L"`),
		"/tools/0.1/odometer_id":                  json.RawMessage(`9`),
		"/tools/0.1/odometer_e":                   json.RawMessage(`310.5`),
		"/tools/1/feeder/mmu_progress_code":       json.RawMessage(`3`),
	})
	require.NotNil(t, got)

	feeder := got.Tools["0"].Feeder
	require.True(t, feeder.Known)
	require.NotNil(t, feeder.Value)
	require.Equal(t, "MMU", feeder.Value.Type)
	require.Equal(t, "MMU3", feeder.Value.BaseModel)
	require.Equal(t, int64(17), *feeder.Value.MMUProgressCode)
	require.Equal(t, "L", feeder.Value.MMUCommandInProgress)
	require.Equal(t, int64(9), *got.Tools["0.1"].OdometerID)
	require.InDelta(t, 310.5, *got.Tools["0.1"].OdometerE, 1e-9)
	require.Nil(t, got.Tools["0.1"].OdometerWeight)
	require.False(t, got.Tools["1"].Feeder.Known, "a feeder with no slot count is unknown")
}
