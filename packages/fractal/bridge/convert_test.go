package bridge_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// TestAWordBecomesAValueOfTheEnumeration: the bridge converts the word a
// printer reports at /state and for a head through the "enums" tables of
// mapping.json. A word the table does not list becomes the default of the
// table.
func TestAWordBecomesAValueOfTheEnumeration(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		path  string
		says  string
		iface fractal.InterfaceName
		prop  fractal.PropertyName
		want  string
	}{
		"printing":                 {path: "/state", says: `"PRINTING"`, iface: "Printer1", prop: "state", want: `"PRINTING"`},
		"idle becomes blocked":     {path: "/state", says: `"IDLE"`, iface: "Printer1", prop: "state", want: `"BLOCKED"`},
		"finished becomes blocked": {path: "/state", says: `"FINISHED"`, iface: "Printer1", prop: "state", want: `"BLOCKED"`},
		"a state not in the table": {path: "/state", says: `"SELF_TEST"`, iface: "Printer1", prop: "state", want: `"BLOCKED"`},
		"a value that is not a word": {
			path: "/state", says: `7`, iface: "Printer1", prop: "state", want: `"BLOCKED"`,
		},
		"petg with a hyphen": {
			path: headMaterial, says: `"PET-G"`, iface: "FDMPrinterHead1", prop: "material", want: `"PETG"`,
		},
		"a filament not in the table": {
			path: headMaterial, says: `"WOOD"`, iface: "FDMPrinterHead1", prop: "material", want: `"OTHER"`,
		},
		"no filament": {
			path: headMaterial, says: `""`, iface: "FDMPrinterHead1", prop: "material", want: `"NOTHING"`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if held := readOne(t, tc.path, tc.says, tc.iface, tc.prop); held != tc.want {
				t.Fatalf("%s is %s, want %s", tc.prop, held, tc.want)
			}
		})
	}
}

// TestADialogOverAPrintIsAPause: a printer with a dialog on its screen
// reports ATTENTION. Over a running print the print is held, so it reads
// PAUSED; with no print it reads BLOCKED. The readings come as live
// telemetry after the first listing, as on a running server.
func TestADialogOverAPrintIsAPause(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		job  string
		want string
	}{
		"over a running print": {job: `"PRINTING"`, want: `"PAUSED"`},
		"over a paused print":  {job: `"PAUSED"`, want: `"PAUSED"`},
		"after a print":        {job: `"FINISHED"`, want: `"BLOCKED"`},
		"with no print":        {want: `"BLOCKED"`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			made := build(t, newFleet(printer(printerOne, map[string]json.RawMessage{
				"/state": json.RawMessage(`"IDLE"`),
			})), newNode(), fixed(at()))

			if err := made.Sync(t.Context()); err != nil {
				t.Fatalf("read the devices: %v", err)
			}

			later := at().Add(time.Second)
			if tc.job != "" {
				made.Sample(t.Context(), printerOne, "/job/state", json.RawMessage(tc.job), later)
			}

			made.Sample(t.Context(), printerOne, "/state", json.RawMessage(`"ATTENTION"`), later)

			props, _, err := made.Snapshot(printerOne, "Printer1")
			if err != nil {
				t.Fatalf("take a full report: %v", err)
			}

			if want := (fractal.Props{"state": json.RawMessage(tc.want)}); !reflect.DeepEqual(props, want) {
				t.Errorf("the printer reads %s, want %s", props, want)
			}
		})
	}
}

// headMaterial is where a printer reports the filament loaded in head 0.
const headMaterial = "/tools/0/material_package_instance/package/material/abbreviation"

// TestTheHeadMaterialComesFromTheToolTree: a head with a multi-material unit
// carries no material, and the slot loaded into it does. The bridge reports
// the head's own material, or the material of that slot.
func TestTheHeadMaterialComesFromTheToolTree(t *testing.T) {
	t.Parallel()

	const slotMaterial = "/material_package_instance/package/material/abbreviation"

	mmu := func(extra map[string]string) map[string]json.RawMessage {
		state := map[string]json.RawMessage{
			"/tools/0/feeder/slot_count": json.RawMessage(`3`),
			"/tools/0.0/active":          json.RawMessage(`false`),
			"/tools/0.1/active":          json.RawMessage(`false`),
			"/tools/0.2/active":          json.RawMessage(`false`),
			"/tools/0.0" + slotMaterial:  json.RawMessage(`"PLA"`),
			"/tools/0.1" + slotMaterial:  json.RawMessage(`"PETG"`),
		}
		for path, value := range extra {
			state[path] = json.RawMessage(value)
		}

		return state
	}

	for name, tc := range map[string]struct {
		state map[string]json.RawMessage
		want  string
	}{
		"the head's own":       {state: map[string]json.RawMessage{headMaterial: json.RawMessage(`"ASA"`)}, want: `"ASA"`},
		"the loaded slot's":    {state: mmu(map[string]string{"/tools/0.1/active": `true`}), want: `"PETG"`},
		"no slot loaded":       {state: mmu(nil), want: `"NOTHING"`},
		"a slot with no label": {state: mmu(map[string]string{"/tools/0.2/active": `true`}), want: `null`},
		"nothing reported":     {state: map[string]json.RawMessage{}, want: `null`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			want := fractal.Props{
				"tempNozzle": json.RawMessage(`null`), "targetNozzle": json.RawMessage(`null`),
				"fanExtruder": json.RawMessage(`null`), "fanPrint": json.RawMessage(`null`),
				"nozzleType": json.RawMessage(`null`), "nozzleDiameter": json.RawMessage(`null`),
				"nozzleHighFlow": json.RawMessage(`null`), "material": json.RawMessage(tc.want),
			}
			if got := snapshotFrom(t, tc.state, "FDMPrinterHead1"); !reflect.DeepEqual(got, want) {
				t.Errorf("the head reads %s, want %s", got, want)
			}
		})
	}
}

// TestTheActiveSlotComesFromTheToolTree: FDMPrinter1.activeSlot is the slot of
// the multi-material unit loaded into its head, counted from 1, 0 with no
// slot loaded, and null with no unit fitted.
func TestTheActiveSlotComesFromTheToolTree(t *testing.T) {
	t.Parallel()

	mmu := func(active string) map[string]json.RawMessage {
		state := map[string]json.RawMessage{
			"/tools/0/feeder/slot_count": json.RawMessage(`3`),
			"/tools/0.0/active":          json.RawMessage(`false`),
			"/tools/0.1/active":          json.RawMessage(`false`),
			"/tools/0.2/active":          json.RawMessage(`false`),
		}
		if active != "" {
			state["/tools/"+active+"/active"] = json.RawMessage(`true`)
		}

		return state
	}

	for name, tc := range map[string]struct {
		state map[string]json.RawMessage
		want  string
	}{
		"slot 3 loaded":    {state: mmu("0.2"), want: `3`},
		"no slot loaded":   {state: mmu(""), want: `0`},
		"no unit fitted":   {state: map[string]json.RawMessage{"/tools/0/feeder/slot_count": json.RawMessage(`0`)}, want: `null`},
		"nothing reported": {state: map[string]json.RawMessage{}, want: `null`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			want := fractal.Props{
				"axisX": json.RawMessage(`null`), "axisY": json.RawMessage(`null`), "axisZ": json.RawMessage(`null`),
				"tempBed": json.RawMessage(`null`), "targetBed": json.RawMessage(`null`),
				"activeSlot": json.RawMessage(tc.want),
			}
			if got := snapshotFrom(t, tc.state, "FDMPrinter1"); !reflect.DeepEqual(got, want) {
				t.Errorf("the printer reads %s, want %s", got, want)
			}
		})
	}
}

// TestAReadingBecomesTheDeclaredType: a printer may send a number as 100, as
// 99.6 or as 3.7e1. The bridge reports it in the type the interface declares,
// an int or a float, and reports null for a reading that is not a number.
func TestAReadingBecomesTheDeclaredType(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		path  string
		says  string
		iface fractal.InterfaceName
		prop  fractal.PropertyName
		want  string
	}{
		"a whole speed":            {path: "/speed", says: `100`, iface: "Printer1", prop: "speed", want: `100`},
		"a speed with a fraction":  {path: "/speed", says: `99.6`, iface: "Printer1", prop: "speed", want: `100`},
		"a temperature as a whole": {path: "/temperature/bed", says: `60`, iface: "FDMPrinter1", prop: "tempBed", want: `60`},
		"a temperature with a fraction": {
			path: "/temperature/bed", says: `59.9`, iface: "FDMPrinter1", prop: "tempBed", want: `59.9`,
		},
		"a flow in exponent form": {
			path: "/flow", says: `9.5e1`, iface: "FDMPrinter1", prop: "flow", want: `95`,
		},
		"a reading that is not a number": {
			path: "/axis/x", says: `"home"`, iface: "FDMPrinter1", prop: "axisX", want: `null`,
		},
		"a null reading": {
			path: "/axis/x", says: `null`, iface: "FDMPrinter1", prop: "axisX", want: `null`,
		},
		"a bed heater that is off": {
			path: "/temperature/bed_target", says: `0.0`, iface: "FDMPrinter1", prop: "targetBed", want: `null`,
		},
		"a nozzle heater that is off": {
			path: "/tools/0/nozzle_target_temp", says: `0`, iface: "FDMPrinterHead1", prop: "targetNozzle",
			want: `null`,
		},
		"a bed heating": {
			path: "/temperature/bed_target", says: `60`, iface: "FDMPrinter1", prop: "targetBed", want: `60`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if held := readOne(t, tc.path, tc.says, tc.iface, tc.prop); held != tc.want {
				t.Fatalf("%s is %s, want %s", tc.prop, held, tc.want)
			}
		})
	}
}

// TestAPropertyThatIsNeverNullIsLeftOutRatherThanInvented: the property speed
// may not be null. When the printer reports a speed that is not a number, the
// bridge leaves speed out of the report, and the node above keeps its last
// value.
func TestAPropertyThatIsNeverNullIsLeftOutRatherThanInvented(t *testing.T) {
	t.Parallel()

	fleet := newFleet(printer(printerOne, map[string]json.RawMessage{
		"/speed": json.RawMessage(`"fast"`),
	}))
	node := newNode()
	made := build(t, fleet, node, fixed(at()))

	if err := made.Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	props, _, err := made.Snapshot(printerOne, "Printer1")
	if err != nil {
		t.Fatalf("take a full report: %v", err)
	}

	if _, sent := props["speed"]; sent {
		t.Fatalf("the report has speed %s, and the printer reported a speed that is not a number", props["speed"])
	}
}

// TestADeviceFieldBecomesAProperty: the firmware version is a field of the
// device record and is not in the state tree. A property with a "field" row in
// mapping.json reports that field.
func TestADeviceFieldBecomesAProperty(t *testing.T) {
	t.Parallel()

	fleet := newFleet(printer(printerOne, telemetry()))
	node := newNode()
	made := build(t, fleet, node, fixed(at()))

	if err := made.Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	props, _, err := made.Snapshot(printerOne, "UpgradableDevice1")
	if err != nil {
		t.Fatalf("take a full report: %v", err)
	}

	if held := props["firmwareVersion"]; string(held) != `"6.1.3"` {
		t.Fatalf("firmwareVersion is %s, want \"6.1.3\"", held)
	}
}

// readOne returns the value the bridge reports for one property, for a printer
// whose state tree has only the value says at path.
func readOne(
	t *testing.T, path, says string, iface fractal.InterfaceName, prop fractal.PropertyName,
) string {
	t.Helper()

	return readFrom(t, map[string]json.RawMessage{path: json.RawMessage(says)}, iface, prop)
}

// readFrom returns the value the bridge reports for one property, for a
// printer whose state tree is state.
func readFrom(
	t *testing.T, state map[string]json.RawMessage, iface fractal.InterfaceName, prop fractal.PropertyName,
) string {
	t.Helper()

	held, sent := snapshotFrom(t, state, iface)[prop]
	if !sent {
		t.Fatalf("the report has no property %s", prop)
	}

	return string(held)
}

// snapshotFrom returns the full report of one interface, for a printer whose
// state tree is state.
func snapshotFrom(t *testing.T, state map[string]json.RawMessage, iface fractal.InterfaceName) fractal.Props {
	t.Helper()

	made := build(t, newFleet(printer(printerOne, state)), newNode(), fixed(at()))
	if err := made.Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	props, _, err := made.Snapshot(printerOne, iface)
	if err != nil {
		t.Fatalf("take a full report: %v", err)
	}

	return props
}

// TestAJobReadsNullWhileNoPrintRuns: a printer keeps the last job's values
// after the print ends. PrintJob1 and FDMPrintJob1 read null for each while
// Printer1.state is neither PRINTING nor PAUSED.
func TestAJobReadsNullWhileNoPrintRuns(t *testing.T) {
	t.Parallel()

	running := fractal.Props{
		"currentJobID":  json.RawMessage(`417`),
		"progress":      json.RawMessage(`37`),
		"timePrinting":  json.RawMessage(`1800`),
		"timeRemaining": json.RawMessage(`2400`),
	}
	none := fractal.Props{
		"currentJobID":  json.RawMessage(`null`),
		"progress":      json.RawMessage(`null`),
		"timePrinting":  json.RawMessage(`null`),
		"timeRemaining": json.RawMessage(`null`),
	}
	filament := map[bool]fractal.Props{
		true:  {"filamentUsed": json.RawMessage(`1520.5`)},
		false: {"filamentUsed": json.RawMessage(`null`)},
	}

	for name, tc := range map[string]struct {
		change map[string]string
		want   fractal.Props
	}{
		"printing":              {change: map[string]string{"/state": `"PRINTING"`}, want: running},
		"a dialog over a print": {change: map[string]string{"/state": `"ATTENTION"`, "/job/state": `"PRINTING"`}, want: running},
		"finished":              {change: map[string]string{"/state": `"FINISHED"`}, want: none},
		"ready":                 {change: map[string]string{"/state": `"READY"`}, want: none},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			state := telemetry()
			for path, value := range tc.change {
				state[path] = json.RawMessage(value)
			}

			made := build(t, newFleet(printer(printerOne, state)), newNode(), fixed(at()))
			if err := made.Sync(t.Context()); err != nil {
				t.Fatalf("read the devices: %v", err)
			}

			got := map[fractal.InterfaceName]fractal.Props{}

			for _, iface := range []fractal.InterfaceName{"PrintJob1", "FDMPrintJob1"} {
				props, _, err := made.Snapshot(printerOne, iface)
				if err != nil {
					t.Fatalf("take a full report of %s: %v", iface, err)
				}

				got[iface] = props
			}

			want := map[fractal.InterfaceName]fractal.Props{
				"PrintJob1": tc.want, "FDMPrintJob1": filament[reflect.DeepEqual(tc.want, running)],
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("the job is %s, want %s", got, want)
			}
		})
	}
}
