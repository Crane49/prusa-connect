package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/bridge"
	"github.com/prusa3d/fractal-link/internal/testport"

	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// This file runs two nodes of this module in one process. The node "workshop"
// joins the node "hq" over the real transport, each with its own embedded NATS
// server. The test checks that hq reads the interfaces of a printer of
// workshop, and that a method hq calls becomes a command for that printer.

// machine is the identifier of the printer in these tests.
const machine = "01K3Q7X8N4B2VJ5R0YH6M9CDEF"

// crossWait is the longest a test waits for the join or for a message, and
// crossPoll is how often it checks.
const (
	crossWait = 60 * time.Second
	crossPoll = 25 * time.Millisecond
)

// oneMachine is a host with one printer. It records every command it receives.
type oneMachine struct {
	mu       sync.Mutex
	device   deviceapi.Device
	commands []deviceapi.CommandParams
}

func newMachine() *oneMachine {
	return &oneMachine{device: deviceapi.Device{
		Kind: "printer", ID: machine, Vendor: "Prusa Research", Model: "MK4",
		Firmware: "6.1.3", Serial: "SN0001", Driver: "xbuddy", Connected: true,
		State: map[string]json.RawMessage{
			"/state":               json.RawMessage(`"PRINTING"`),
			"/speed":               json.RawMessage(`100`),
			"/tools/0/nozzle_temp": json.RawMessage(`214.8`),
		},
	}}
}

func (m *oneMachine) List(context.Context, deviceapi.ListParams) (deviceapi.Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return deviceapi.Page{Items: []deviceapi.Device{m.device}, Total: 1, Limit: 1}, nil
}

func (m *oneMachine) Get(_ context.Context, params deviceapi.GetParams) (deviceapi.Device, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if params.ID != m.device.ID {
		return deviceapi.Device{}, deviceapi.ErrNoDevice
	}

	return m.device, nil
}

func (m *oneMachine) Command(
	_ context.Context, params deviceapi.CommandParams,
) (deviceapi.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.commands = append(m.commands, params)

	return deviceapi.Result{OK: true}, nil
}

func (m *oneMachine) Print(context.Context, deviceapi.PrintParams) (deviceapi.Started, error) {
	return deviceapi.Started{}, deviceapi.ErrNoDevice
}

func (m *oneMachine) sent() []deviceapi.CommandParams {
	m.mu.Lock()
	defer m.mu.Unlock()

	return append([]deviceapi.CommandParams(nil), m.commands...)
}

// noMachines is a host with no printer, for the node at the top of the tree.
type noMachines struct{}

func (noMachines) List(context.Context, deviceapi.ListParams) (deviceapi.Page, error) {
	return deviceapi.Page{}, nil
}

func (noMachines) Get(context.Context, deviceapi.GetParams) (deviceapi.Device, error) {
	return deviceapi.Device{}, deviceapi.ErrNoDevice
}

func (noMachines) Command(context.Context, deviceapi.CommandParams) (deviceapi.Result, error) {
	return deviceapi.Result{}, deviceapi.ErrNoDevice
}

func (noMachines) Print(context.Context, deviceapi.PrintParams) (deviceapi.Started, error) {
	return deviceapi.Started{}, deviceapi.ErrNoDevice
}

// started builds one node from the settings in record and runs its goroutines
// until ctx ends.
func started(
	ctx context.Context, t *testing.T, record string, fleet bridge.Fleet,
) *parts {
	t.Helper()

	cfg, err := ParseConfig([]byte(record))
	if err != nil {
		t.Fatalf("read the settings: %v", err)
	}

	state := newLink()
	host := session{fleet: fleet}

	built, err := build(ctx, cfg, state, &tree{}, host, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("build the node: %v", err)
	}

	var group sync.WaitGroup

	failed := make(chan error, runners)

	built.start(ctx, &group, failed)

	t.Cleanup(func() {
		group.Wait()
		_ = built.node.Close()
		_ = built.binding.Close()
	})

	return built
}

// settingsAbove returns the settings of the node at the top, "hq". It joins no
// server, accepts servers under it, and keeps a shadow of every interface they
// report.
func settingsAbove(t *testing.T, dir string, client, leaf int) string {
	t.Helper()

	return fmt.Sprintf(`{
		"node_id": "hq",
		"data_dir": %q,
		"dedup_window": "1m",
		"heartbeat_interval": "100ms",
		"max_heartbeat_interval": "200ms",
		"poll_interval": "50ms",
		"accept": {
			"advertise": "nats://127.0.0.1:%d",
			"client_listen": "127.0.0.1:%d",
			"leaf_listen": "127.0.0.1:%d"
		},
		"shadow": {"patterns": ["*"]},
		"flush_interval": "50ms"
	}`, dir, leaf, client, leaf)
}

// settingsBelow returns the settings of the node "workshop", which joins hq and
// has the printer.
func settingsBelow(t *testing.T, dir string, client, leaf, above int) string {
	t.Helper()

	return fmt.Sprintf(`{
		"node_id": "workshop",
		"data_dir": %q,
		"command_principal": "node",
		"dedup_window": "1m",
		"heartbeat_interval": "100ms",
		"max_heartbeat_interval": "200ms",
		"poll_interval": "50ms",
		"join": {"address": "127.0.0.1:%d"},
		"accept": {
			"advertise": "nats://127.0.0.1:%d",
			"client_listen": "127.0.0.1:%d",
			"leaf_listen": "127.0.0.1:%d"
		},
		"flush_interval": "50ms"
	}`, dir, above, leaf, client, leaf)
}

// TestTwoServersOneUnderTheOther: over the real transport, the shadow of hq
// receives the state of the printer of workshop. A PauseJob call from hq
// becomes one pause_print command for that printer, with the principal
// "node:hq".
func TestTwoServersOneUnderTheOther(t *testing.T) {
	if testing.Short() {
		t.Skip("this test starts two NATS servers and joins one to the other")
	}

	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	aboveClient, aboveLeaf := testport.Free(t), testport.Free(t)
	belowClient, belowLeaf := testport.Free(t), testport.Free(t)

	above := started(ctx, t, settingsAbove(t, t.TempDir(), aboveClient, aboveLeaf), noMachines{})

	machines := newMachine()
	below := started(ctx, t,
		settingsBelow(t, t.TempDir(), belowClient, belowLeaf, aboveClient), machines)

	joined(t, below)

	key := shadow.Key{
		Node:      "hq/workshop",
		Interface: "Printer1",
		Instance:  fractal.InstanceID(machine),
	}

	printing := awaitShadow(t, above, key)
	if string(printing.Props["state"]) != `"PRINTING"` {
		t.Fatalf("the state in the shadow of hq is %s, want \"PRINTING\"", printing.Props["state"])
	}

	answer, err := above.node.Call(ctx, fractalnode.CallRequest{
		Target:    "hq/workshop",
		Interface: "Printer1",
		Instance:  fractal.InstanceID(machine),
		Method:    "PauseJob",
		Expires:   fractal.TimestampOf(time.Now().Add(crossWait)),
	})
	if err != nil {
		t.Fatalf("the PauseJob call got no result: %v", err)
	}

	if answer.Error != nil {
		t.Fatalf("the PauseJob call returned the error %s", answer.Error)
	}

	sent := machines.sent()
	if len(sent) != 1 || sent[0].Verb != "core.command.pause_print" {
		t.Fatalf("the printer received %v, want one core.command.pause_print", sent)
	}

	if sent[0].Principal != "node:hq" {
		t.Fatalf("the principal of the command is %q, want node:hq", sent[0].Principal)
	}
}

// joined waits until the node below has the Node Path "hq/workshop".
func joined(t *testing.T, below *parts) {
	t.Helper()

	deadline := time.Now().Add(crossWait)
	for time.Now().Before(deadline) {
		if below.binding.Path() == "hq/workshop" {
			return
		}

		time.Sleep(crossPoll)
	}

	t.Fatalf("the node below never joined, and its status is %+v", below.binding.Status())
}

// awaitShadow waits until the shadow of the node above has the interface key
// with the status OK, and returns it.
func awaitShadow(t *testing.T, above *parts, key shadow.Key) bridge.ShadowInterface {
	t.Helper()

	deadline := time.Now().Add(crossWait)
	for time.Now().Before(deadline) {
		for _, held := range above.shadow.Subtree("hq") {
			if held.Node == key.Node && held.Interface == key.Interface &&
				held.Instance == key.Instance && held.Status == shadow.StatusOK.String() {
				return held
			}
		}

		time.Sleep(crossPoll)
	}

	t.Fatalf("the shadow of the node above never had %s with the status OK", key)

	return bridge.ShadowInterface{}
}
