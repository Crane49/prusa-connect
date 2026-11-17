package bridge_test

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/bridge"
	"github.com/prusa3d/fractal-link/interfaces"

	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
)

// The device identifiers of the tests, in the format the server gives a
// printer. The bridge uses the identifier in the Interface Instance ID.
const (
	printerOne = "01K3Q7X8N4B2VJ5R0YH6M9CDEF"
	printerTwo = "01K3Q7XA26D0WM4T1ZJ8P5GHIJ"
)

// mapping loads mapping.json, the table the module ships, and checks it against
// the interface definitions the module ships. The tests use this table and not
// a table of their own, so they test what runs in production.
func mapping(t *testing.T) *bridge.Mapping {
	t.Helper()

	registry := registry(t)

	built, err := bridge.LoadMapping(registry)
	if err != nil {
		t.Fatalf("read the mapping table: %v", err)
	}

	return built
}

// registry loads the interface definitions the module ships.
func registry(t *testing.T) *schema.Registry {
	t.Helper()

	built, err := interfaces.Registry()
	if err != nil {
		t.Fatalf("read the interface definitions: %v", err)
	}

	return built
}

// command is one Command call the fake host received.
type command struct {
	principal string
	device    string
	verb      string
	args      map[string]string
}

// fakeFleet is a fake host. It returns the devices a test gave it and records
// every command it receives. For a device that lists capabilities it refuses
// an argument the list does not declare, as the host does.
type fakeFleet struct {
	mu       sync.Mutex
	devices  map[string]deviceapi.Device
	order    []string
	commands []command
	// refuse is the error Command returns. When it is nil, the command reaches
	// the device.
	refuse error
	// machine is the reason the device gives when it refuses a command. When
	// it is empty, the device accepts the command.
	machine string
	// printed is every Print call, in order.
	printed []deviceapi.PrintParams
	// printRefuse is the error Print returns.
	printRefuse error
	// onPrint runs inside Print, before it returns.
	onPrint func()
}

// newFleet builds a fake host with the given devices.
func newFleet(devices ...deviceapi.Device) *fakeFleet {
	held := &fakeFleet{devices: make(map[string]deviceapi.Device, len(devices))}
	for _, device := range devices {
		held.devices[device.ID] = device
		held.order = append(held.order, device.ID)
	}

	return held
}

func (f *fakeFleet) List(_ context.Context, params deviceapi.ListParams) (deviceapi.Page, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	page := deviceapi.Page{Total: len(f.order), Limit: params.Limit, Offset: params.Offset}

	for i, id := range f.order {
		if i < params.Offset {
			continue
		}

		if params.Limit > 0 && len(page.Items) >= params.Limit {
			break
		}

		page.Items = append(page.Items, f.devices[id])
	}

	return page, nil
}

func (f *fakeFleet) Get(_ context.Context, params deviceapi.GetParams) (deviceapi.Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	device, held := f.devices[params.ID]
	if !held {
		return deviceapi.Device{}, deviceapi.ErrNoDevice
	}

	return device, nil
}

func (f *fakeFleet) Command(
	_ context.Context, params deviceapi.CommandParams,
) (deviceapi.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.commands = append(f.commands, command{
		principal: params.Principal,
		device:    params.ID,
		verb:      params.Verb,
		args:      params.Args,
	})

	if f.refuse != nil {
		return deviceapi.Result{}, f.refuse
	}

	if declared := f.devices[params.ID].Capabilities; declared != nil {
		for arg := range params.Args {
			if !slices.Contains(declared, params.Verb+"#"+arg) {
				return deviceapi.Result{}, deviceapi.ErrArgument
			}
		}
	}

	if f.machine != "" {
		return deviceapi.Result{OK: false, MachineReason: f.machine}, nil
	}

	return deviceapi.Result{OK: true}, nil
}

func (f *fakeFleet) Print(_ context.Context, params deviceapi.PrintParams) (deviceapi.Started, error) {
	f.mu.Lock()
	f.printed = append(f.printed, params)
	refuse, hook := f.printRefuse, f.onPrint
	f.mu.Unlock()

	if hook != nil {
		hook()
	}

	if refuse != nil {
		return deviceapi.Started{}, refuse
	}

	return deviceapi.Started{TransferID: "transfer-1"}, nil
}

// prints returns the Print calls the fake host received, in order.
func (f *fakeFleet) prints() []deviceapi.PrintParams {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]deviceapi.PrintParams(nil), f.printed...)
}

// sent returns the commands the fake host received, in order.
func (f *fakeFleet) sent() []command {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]command(nil), f.commands...)
}

// remove deletes one device from the fake host, as when an operator removes a
// printer from the server.
func (f *fakeFleet) remove(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	delete(f.devices, id)

	kept := f.order[:0]
	for _, held := range f.order {
		if held != id {
			kept = append(kept, held)
		}
	}

	f.order = kept
}

// report is one state report the bridge sent.
type report struct {
	iface    fractal.InterfaceName
	instance fractal.InstanceID
	props    fractal.Props
	at       fractal.Timestamp
}

// fakeNode is a fake Fractal node. It records the interface instances the
// bridge registered and the state reports the bridge sent.
type fakeNode struct {
	mu        sync.Mutex
	instances []fractalnode.Instance
	handles   map[string]*fakeHandle
	owners    map[string]fractalnode.Owner
	reports   []report
	// beforeReport runs once, inside the next Report, before the report is
	// stored.
	beforeReport func()
}

func newNode() *fakeNode {
	return &fakeNode{
		handles: make(map[string]*fakeHandle),
		owners:  make(map[string]fractalnode.Owner),
	}
}

// Register records one interface instance and returns a handle that records
// every report.
func (n *fakeNode) Register(inst fractalnode.Instance) (bridge.Reporter, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.instances = append(n.instances, inst)
	n.owners[key(inst.Name, inst.Instance)] = inst.Owner

	handle := &fakeHandle{node: n, iface: inst.Name, instance: inst.Instance}
	n.handles[key(inst.Name, inst.Instance)] = handle

	return handle, nil
}

// owner returns the Owner the bridge registered for one interface instance, so
// a test can call Snapshot, Desire and Invoke as the Fractal node does.
func (n *fakeNode) owner(t *testing.T, name fractal.InterfaceName, id fractal.InstanceID) fractalnode.Owner {
	t.Helper()

	n.mu.Lock()
	defer n.mu.Unlock()

	held, ok := n.owners[key(name, id)]
	if !ok {
		t.Fatalf("no owner is registered for %s/%s", name, id)
	}

	return held
}

// definition returns the interface definition the bridge registered for one
// interface instance.
func (n *fakeNode) definition(
	t *testing.T, name fractal.InterfaceName, id fractal.InstanceID,
) *schema.Definition {
	t.Helper()

	n.mu.Lock()
	defer n.mu.Unlock()

	for _, inst := range n.instances {
		if inst.Name == name && inst.Instance == id {
			return inst.Definition
		}
	}

	t.Fatalf("no interface instance is registered for %s/%s", name, id)

	return nil
}

// seen returns every report the bridge sent, in order.
func (n *fakeNode) seen() []report {
	n.mu.Lock()
	defer n.mu.Unlock()

	return append([]report(nil), n.reports...)
}

// seenOf returns the reports of one interface of one device, in order.
func (n *fakeNode) seenOf(name fractal.InterfaceName, id fractal.InstanceID) []report {
	n.mu.Lock()
	defer n.mu.Unlock()

	var out []report

	for _, held := range n.reports {
		if held.iface == name && held.instance == id {
			out = append(out, held)
		}
	}

	return out
}

// fakeHandle is the handle of one registered interface instance. Report stores
// a copy of the report in the fakeNode.
type fakeHandle struct {
	node     *fakeNode
	iface    fractal.InterfaceName
	instance fractal.InstanceID
}

func (h *fakeHandle) Report(_ context.Context, props fractal.Props, at fractal.Timestamp) error {
	h.node.mu.Lock()
	hook := h.node.beforeReport
	h.node.beforeReport = nil
	h.node.mu.Unlock()

	if hook != nil {
		hook()
	}

	kept := make(fractal.Props, len(props))
	for name, value := range props {
		kept[name] = append(json.RawMessage(nil), value...)
	}

	h.node.mu.Lock()
	defer h.node.mu.Unlock()

	h.node.reports = append(h.node.reports,
		report{iface: h.iface, instance: h.instance, props: kept, at: at})

	return nil
}

// key returns the map key of one interface instance.
func key(name fractal.InterfaceName, id fractal.InstanceID) string {
	return string(name) + "/" + string(id)
}

// printer returns one device record as the host returns it, with the given
// state tree.
func printer(id string, state map[string]json.RawMessage) deviceapi.Device {
	return deviceapi.Device{
		Kind:      "printer",
		ID:        id,
		Name:      "workshop " + id[:4],
		Vendor:    "Prusa Research",
		Model:     "MK4",
		Firmware:  "6.1.3",
		Serial:    "SN" + id[:6],
		Driver:    "xbuddy",
		Connected: true,
		State:     state,
	}
}

// telemetry returns the state tree of a Prusa printer that is printing.
func telemetry() map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"/state":               json.RawMessage(`"PRINTING"`),
		"/speed":               json.RawMessage(`100`),
		"/flow":                json.RawMessage(`95`),
		"/tools/0/nozzle_temp": json.RawMessage(`214.8`),
		"/tools/0/material_package_instance/package/material/abbreviation": json.RawMessage(`"PETG"`),
		"/tools/0/print_fan_rpm":             json.RawMessage(`5200`),
		"/tools/0/heatbreak_fan_rpm":         json.RawMessage(`7000`),
		"/tools/0/nozzle_target_temp":        json.RawMessage(`230`),
		"/tools/0/features/nozzle_diameter":  json.RawMessage(`0.4`),
		"/tools/0/features/nozzle_high_flow": json.RawMessage(`false`),
		"/job/filament_used_mm":              json.RawMessage(`1520.5`),
		"/temperature/bed":                   json.RawMessage(`59.9`),
		"/temperature/bed_target":            json.RawMessage(`60`),
		"/axis/x":                            json.RawMessage(`10.5`),
		"/axis/y":                            json.RawMessage(`20.25`),
		"/axis/z":                            json.RawMessage(`1.2`),
		"/job/id":                            json.RawMessage(`417`),
		"/job/progress":                      json.RawMessage(`37`),
		"/job/time_printing":                 json.RawMessage(`1800`),
		"/job/time_remaining":                json.RawMessage(`2400`),
	}
}

// at returns a fixed time, so a test can compare the timestamp of a report.
func at() time.Time {
	return time.Date(2026, time.September, 21, 9, 41, 12, 0, time.UTC)
}
