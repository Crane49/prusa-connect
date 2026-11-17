// Package hostfake holds the host's device door in memory, for tests. It
// answers the way the host does: a device the caller may not see is
// deviceapi.ErrNoDevice, and a command is recorded rather than sent.
package hostfake

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
)

// Devices is a fleet in memory.
type Devices struct {
	mu       sync.Mutex
	items    []deviceapi.Device
	commands []deviceapi.CommandParams
	lists    int
	// sees is the devices each principal may see. A principal it does not
	// name sees every device, and so does the module itself.
	sees map[string][]string

	// Refuse, when set, is what Command answers instead of running.
	Refuse error
	// refuseList, when set, is what List answers for a principal.
	refuseList error
}

// Add puts one connected printer with its network on the fleet. An empty
// value leaves that node out, as the connector does for an interface that is
// down.
func (d *Devices) Add(id, name, lanMAC, lanIP, wifiIP string) {
	d.add(id, name, lanMAC, lanIP, wifiIP, true)
}

// AddAway puts one printer on the fleet that has been seen and is away, with
// the network it reported last.
func (d *Devices) AddAway(id, name, lanMAC, lanIP, wifiIP string) {
	d.add(id, name, lanMAC, lanIP, wifiIP, false)
}

// Sees limits what principal may see to the devices named.
func (d *Devices) Sees(principal string, ids ...string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.sees == nil {
		d.sees = map[string][]string{}
	}

	d.sees[principal] = ids
}

func (d *Devices) add(id, name, lanMAC, lanIP, wifiIP string, connected bool) {
	state := map[string]json.RawMessage{}

	for path, value := range map[string]string{
		"/network/lan_mac": lanMAC, "/network/lan_ipv4": lanIP, "/network/wifi_ipv4": wifiIP,
	} {
		if value != "" {
			encoded, _ := json.Marshal(value) //nolint:errchkjson // a string always encodes.
			state[path] = encoded
		}
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	d.items = append(d.items, deviceapi.Device{
		Kind: "printer", ID: id, Name: name, State: state, Connected: connected,
	})
}

// RefuseList makes List answer err for a principal. It takes the lock,
// because the module reads the fleet in the background while a test runs.
func (d *Devices) RefuseList(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.refuseList = err
}

// List answers one page of the devices the principal may see.
func (d *Devices) List(_ context.Context, params deviceapi.ListParams) (deviceapi.Page, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.lists++

	if d.refuseList != nil && params.Principal != "" {
		return deviceapi.Page{}, d.refuseList
	}

	limit := params.Limit
	if limit == 0 {
		limit = deviceapi.DefaultPageLimit
	}

	items := d.items
	if ids, limited := d.sees[params.Principal]; limited {
		items = slices.DeleteFunc(slices.Clone(d.items), func(device deviceapi.Device) bool {
			return !slices.Contains(ids, device.ID)
		})
	}

	start := min(params.Offset, len(items))
	end := min(start+limit, len(items))

	return deviceapi.Page{
		Items: slices.Clone(items[start:end]), Total: len(items), Limit: limit, Offset: params.Offset,
	}, nil
}

// Get answers one device.
func (d *Devices) Get(_ context.Context, params deviceapi.GetParams) (deviceapi.Device, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, device := range d.items {
		if device.ID == params.ID {
			return device, nil
		}
	}

	return deviceapi.Device{}, fmt.Errorf("%w: %s", deviceapi.ErrNoDevice, params.ID)
}

// Command records one command.
func (d *Devices) Command(_ context.Context, params deviceapi.CommandParams) (deviceapi.Result, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.Refuse != nil {
		return deviceapi.Result{}, d.Refuse
	}

	if !slices.ContainsFunc(d.items, func(device deviceapi.Device) bool { return device.ID == params.ID }) {
		return deviceapi.Result{}, fmt.Errorf("%w: %s", deviceapi.ErrNoDevice, params.ID)
	}

	d.commands = append(d.commands, params)

	return deviceapi.Result{OK: true}, nil
}

// Commands is every command sent so far.
func (d *Devices) Commands() []deviceapi.CommandParams {
	d.mu.Lock()
	defer d.mu.Unlock()

	return slices.Clone(d.commands)
}

// Lists is how many pages were read.
func (d *Devices) Lists() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.lists
}
