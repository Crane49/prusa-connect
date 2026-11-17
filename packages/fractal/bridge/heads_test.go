package bridge_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/bridge"

	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// toolchanger returns the state tree of a ready printer with room for five
// heads, of which heads 0 and 1 are fitted and head 1 is picked.
func toolchanger() map[string]json.RawMessage {
	state := map[string]json.RawMessage{
		"/state":                      json.RawMessage(`"READY"`),
		"/tool_count":                 json.RawMessage(`5`),
		"/tools/0/enabled":            json.RawMessage(`true`),
		"/tools/0/type":               json.RawMessage(`"FFF_PRINT_HEAD"`),
		"/tools/0/active":             json.RawMessage(`false`),
		"/tools/0/nozzle_temp":        json.RawMessage(`24.5`),
		"/tools/1/enabled":            json.RawMessage(`true`),
		"/tools/1/type":               json.RawMessage(`"FFF_PRINT_HEAD"`),
		"/tools/1/active":             json.RawMessage(`true`),
		"/tools/1/nozzle_temp":        json.RawMessage(`214.5`),
		"/tools/1/nozzle_target_temp": json.RawMessage(`215`),
		"/tools/2/enabled":            json.RawMessage(`false`),
	}

	return state
}

// setpoint is the verb that sets a nozzle target.
const setpoint = "core.command.set_nozzle_temperature"

// declaring returns a printer that lists the nozzle setpoint among its
// capabilities. A tool changer also declares the tool argument.
func declaring(state map[string]json.RawMessage, toolChanger bool) deviceapi.Device {
	device := printer(printerOne, state)
	device.Capabilities = []string{setpoint, setpoint + "#nozzle_temperature"}

	if toolChanger {
		device.Capabilities = append(device.Capabilities, setpoint+"#tool")
	}

	return device
}

// registered returns the instance ids of one interface the bridge
// registered, in the order it registered them.
func registered(node *fakeNode, name fractal.InterfaceName) []fractal.InstanceID {
	node.mu.Lock()
	defer node.mu.Unlock()

	var out []fractal.InstanceID

	for _, inst := range node.instances {
		if inst.Name == name {
			out = append(out, inst.Instance)
		}
	}

	return out
}

// TestEveryHeadIsAnInstance: a printer with more than one head has one
// FDMPrinterHead1 per enabled print head, numbered from 1, each with the
// readings of its own head. A disabled print head and an enabled tool of
// another type get none. A head that is enabled later gets its instance then.
func TestEveryHeadIsAnInstance(t *testing.T) {
	t.Parallel()

	state := toolchanger()
	state["/tools/3/enabled"] = json.RawMessage(`true`)
	state["/tools/3/type"] = json.RawMessage(`"FFF_FEEDER_SLOT"`)
	state["/tools/4/enabled"] = json.RawMessage(`false`)
	state["/tools/4/type"] = json.RawMessage(`"FFF_PRINT_HEAD"`)

	node := newNode()
	made := build(t, newFleet(printer(printerOne, state)), node, fixed(at()))

	if err := made.Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	first, second, third := fractal.InstanceID(printerOne+"_1"), fractal.InstanceID(printerOne+"_2"),
		fractal.InstanceID(printerOne+"_3")

	if got := registered(node, "FDMPrinterHead1"); !reflect.DeepEqual(got, []fractal.InstanceID{first, second}) {
		t.Fatalf("the heads are %v, want %v", got, []fractal.InstanceID{first, second})
	}

	props, _, err := node.owner(t, "FDMPrinterHead1", second).Snapshot(t.Context())
	if err != nil {
		t.Fatalf("take a full report of head 2: %v", err)
	}

	want := fractal.Props{
		"tempNozzle": json.RawMessage(`214.5`), "targetNozzle": json.RawMessage(`215`),
		"fanExtruder": json.RawMessage(`null`), "fanPrint": json.RawMessage(`null`),
		"nozzleDiameter": json.RawMessage(`null`), "nozzleHighFlow": json.RawMessage(`null`),
		"nozzleType": json.RawMessage(`null`), "material": json.RawMessage(`null`),
	}
	if !reflect.DeepEqual(props, want) {
		t.Errorf("head 2 reads %s, want %s", props, want)
	}

	made.Sample(t.Context(), printerOne, "/tools/2/enabled", json.RawMessage(`true`), at().Add(time.Second))
	made.Sample(t.Context(), printerOne, "/tools/2/type", json.RawMessage(`"FFF_PRINT_HEAD"`), at().Add(time.Second))

	if got := registered(node, "FDMPrinterHead1"); !reflect.DeepEqual(got, []fractal.InstanceID{first, second, third}) {
		t.Fatalf("after head 3 was enabled the heads are %v, want %v", got,
			[]fractal.InstanceID{first, second, third})
	}
}

// TestAHeadIsAddressedByItsTool: a nozzle target goes to the head of its
// instance through the tool argument.
func TestAHeadIsAddressedByItsTool(t *testing.T) {
	t.Parallel()

	fleet := newFleet(declaring(toolchanger(), true))
	node := newNode()

	if err := build(t, fleet, node, fixed(at())).Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	for _, head := range []fractal.InstanceID{printerOne + "_1", printerOne + "_2"} {
		if _, err := node.owner(t, "FDMPrinterHead1", head).Desire(t.Context(),
			fractal.Props{"targetNozzle": json.RawMessage(`215`)}); err != nil {
			t.Fatalf("set the target of %s: %v", head, err)
		}
	}

	want := []command{
		{
			principal: "node:hq", device: printerOne, verb: setpoint,
			args: map[string]string{"nozzle_temperature": "215", "tool": "0"},
		},
		{
			principal: "node:hq", device: printerOne, verb: setpoint,
			args: map[string]string{"nozzle_temperature": "215", "tool": "1"},
		},
	}
	if got := fleet.sent(); !reflect.DeepEqual(got, want) {
		t.Errorf("the host received %+v, want %+v", got, want)
	}
}

// TestATargetForADisabledHeadIsRefused: a printer that declares the tool
// argument gets no nozzle target for a head it no longer has enabled, as the
// filament methods get no call for it.
func TestATargetForADisabledHeadIsRefused(t *testing.T) {
	t.Parallel()

	fleet := newFleet(declaring(toolchanger(), true))
	node := newNode()
	made := build(t, fleet, node, fixed(at()))

	if err := made.Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	made.Sample(t.Context(), printerOne, "/tools/1/enabled", json.RawMessage(`false`), at().Add(time.Second))

	_, err := node.owner(t, "FDMPrinterHead1", printerOne+"_2").Desire(t.Context(),
		fractal.Props{"targetNozzle": json.RawMessage(`215`)})
	if raised, ok := errors.AsType[*fractal.Error](err); !ok || raised.Code != bridge.CodeHeadNotActive {
		t.Errorf("a target for a disabled head returned %v, want CodeHeadNotActive", err)
	}

	if sent := fleet.sent(); len(sent) != 0 {
		t.Errorf("the host received %+v, want nothing", sent)
	}
}

// TestAnUndeclaredToolHeatsOnlyThePickedHead: a printer with several heads
// that declares no tool argument sets the target of the head it has picked.
// Any other head refuses the write, so it never heats the wrong head.
func TestAnUndeclaredToolHeatsOnlyThePickedHead(t *testing.T) {
	t.Parallel()

	fleet := newFleet(declaring(toolchanger(), false))
	node := newNode()

	if err := build(t, fleet, node, fixed(at())).Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	target := fractal.Props{"targetNozzle": json.RawMessage(`215`)}

	_, err := node.owner(t, "FDMPrinterHead1", printerOne+"_1").Desire(t.Context(), target)
	if raised, ok := errors.AsType[*fractal.Error](err); !ok || raised.Code != bridge.CodeHeadNotActive {
		t.Errorf("a target for a head that is not picked returned %v, want CodeHeadNotActive", err)
	}

	if _, err = node.owner(t, "FDMPrinterHead1", printerOne+"_2").Desire(t.Context(), target); err != nil {
		t.Errorf("a target for the picked head returned %v", err)
	}

	want := []command{{
		principal: "node:hq", device: printerOne, verb: setpoint,
		args: map[string]string{"nozzle_temperature": "215"},
	}}
	if got := fleet.sent(); !reflect.DeepEqual(got, want) {
		t.Errorf("the host received %+v, want %+v", got, want)
	}
}

// TestAFilamentMethodGoesToThePickedHead: a method the core can send only to
// the active head is refused on any other head with CodeHeadNotActive.
func TestAFilamentMethodGoesToThePickedHead(t *testing.T) {
	t.Parallel()

	fleet := newFleet(printer(printerOne, toolchanger()))
	node := newNode()

	if err := build(t, fleet, node, fixed(at())).Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	_, err := node.owner(t, "FDMPrinterHead1", printerOne+"_1").Invoke(t.Context(), fractalnode.Call{
		Method: "UnloadFilament", Caller: "hq",
	})
	if raised, ok := errors.AsType[*fractal.Error](err); !ok || raised.Code != bridge.CodeHeadNotActive {
		t.Errorf("an unload on a head that is not picked returned %v, want CodeHeadNotActive", err)
	}

	if _, err = node.owner(t, "FDMPrinterHead1", printerOne+"_2").Invoke(t.Context(), fractalnode.Call{
		Method: "UnloadFilament", Caller: "hq",
	}); err != nil {
		t.Errorf("an unload on the picked head returned %v", err)
	}

	want := []command{{principal: "node:hq", device: printerOne, verb: "core.command.unload_filament"}}
	if got := fleet.sent(); !reflect.DeepEqual(got, want) {
		t.Errorf("the host received %+v, want %+v", got, want)
	}
}

// TestAFilamentMethodNeedsAPickedHead: head 0 stands for the active head only
// on a printer with one head. A tool changer that has picked no head, and a
// disabled head, refuse a method the core sends to the active head.
func TestAFilamentMethodNeedsAPickedHead(t *testing.T) {
	t.Parallel()

	for name, change := range map[string]map[string]string{
		"no head picked": {"/tools/1/active": `false`},
		"head 0 disabled": {
			"/tools/0/enabled": `false`, "/tools/1/enabled": `false`, "/tools/1/active": `false`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			state := toolchanger()
			for path, value := range change {
				state[path] = json.RawMessage(value)
			}

			fleet, node := newFleet(printer(printerOne, state)), newNode()
			if err := build(t, fleet, node, fixed(at())).Sync(t.Context()); err != nil {
				t.Fatalf("read the devices: %v", err)
			}

			_, err := node.owner(t, "FDMPrinterHead1", printerOne+"_1").Invoke(t.Context(), fractalnode.Call{
				Method: "UnloadFilament", Caller: "hq",
			})
			if raised, ok := errors.AsType[*fractal.Error](err); !ok || raised.Code != bridge.CodeHeadNotActive {
				t.Errorf("an unload on head 1 returned %v, want CodeHeadNotActive", err)
			}

			if sent := fleet.sent(); len(sent) != 0 {
				t.Errorf("the host received %+v, want nothing", sent)
			}
		})
	}
}

// TestASingleHeadNamesNoTool: a printer that is not a tool changer declares
// no tool argument, and the host refuses an argument nobody declared. So a
// nozzle target there goes without one.
func TestASingleHeadNamesNoTool(t *testing.T) {
	t.Parallel()

	fleet := newFleet(declaring(map[string]json.RawMessage{"/state": json.RawMessage(`"READY"`)}, false))
	node := newNode()

	if err := build(t, fleet, node, fixed(at())).Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	if _, err := node.owner(t, "FDMPrinterHead1", printerOne+"_1").Desire(t.Context(),
		fractal.Props{"targetNozzle": json.RawMessage(`215`)}); err != nil {
		t.Fatalf("set the target of the head: %v", err)
	}

	want := []command{{
		principal: "node:hq", device: printerOne, verb: setpoint,
		args: map[string]string{"nozzle_temperature": "215"},
	}}
	if got := fleet.sent(); !reflect.DeepEqual(got, want) {
		t.Errorf("the host received %+v, want %+v", got, want)
	}
}

// TestEverySlotIsAnInstance: a printer with a multi-material unit has one
// FilamentSlot1 per slot, numbered from 1, each with the filament of its own
// slot. A unit fitted later gets its instances then.
func TestEverySlotIsAnInstance(t *testing.T) {
	t.Parallel()

	const material = "/material_package_instance/package/material/abbreviation"

	node := newNode()
	made := build(t, newFleet(printer(printerOne, map[string]json.RawMessage{
		"/state": json.RawMessage(`"READY"`),
	})), node, fixed(at()))

	if err := made.Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	// The slot count comes last, so only that reading can register the slots.
	later := at().Add(time.Second)
	for _, reading := range [][2]string{
		{"/tools/0.0/enabled", `true`},
		{"/tools/0.0/type", `"FFF_FEEDER_SLOT"`},
		{"/tools/0.0" + material, `"PLA"`},
		{"/tools/0.1/enabled", `true`},
		{"/tools/0.1/type", `"FFF_FEEDER_SLOT"`},
		{"/tools/0.1" + material, `""`},
		{"/tools/0/feeder/slot_count", `2`},
	} {
		made.Sample(t.Context(), printerOne, reading[0], json.RawMessage(reading[1]), later)
	}

	got := map[fractal.InstanceID]fractal.Props{}

	for _, id := range []fractal.InstanceID{printerOne + "_1", printerOne + "_2"} {
		props, _, err := node.owner(t, "FilamentSlot1", id).Snapshot(t.Context())
		if err != nil {
			t.Fatalf("take a full report of %s: %v", id, err)
		}

		got[id] = props
	}

	slot := func(material string) fractal.Props {
		return fractal.Props{
			"material": json.RawMessage(material), "tempPreheater": json.RawMessage(`null`),
			"fanHotend": json.RawMessage(`null`), "fanPrint": json.RawMessage(`null`),
		}
	}

	want := map[fractal.InstanceID]fractal.Props{
		printerOne + "_1": slot(`"PLA"`), printerOne + "_2": slot(`"NOTHING"`),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the slots read %s, want %s", got, want)
	}
}

// TestSlotsRegisterInOrder: the slots of a unit register by their number, so
// the instances of a node come in the same order on every run.
func TestSlotsRegisterInOrder(t *testing.T) {
	t.Parallel()

	state := map[string]json.RawMessage{
		"/state": json.RawMessage(`"READY"`), "/tools/0/feeder/slot_count": json.RawMessage(`5`),
	}
	for k := range 5 {
		state["/tools/0."+strconv.Itoa(k)+"/enabled"] = json.RawMessage(`true`)
	}

	node := newNode()
	if err := build(t, newFleet(printer(printerOne, state)), node, fixed(at())).Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	want := []fractal.InstanceID{
		printerOne + "_1", printerOne + "_2", printerOne + "_3", printerOne + "_4", printerOne + "_5",
	}
	if got := registered(node, "FilamentSlot1"); !reflect.DeepEqual(got, want) {
		t.Errorf("the slots registered as %v, want %v", got, want)
	}
}
