package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printer/echodevice"
	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// The contract of the tool tree, held end to end (ADR-20261025-pze): a server with
// one simulated printer of each family, on the numbered wire of the shipping
// firmware and on the tree wire of firmware 8.0, holds its tools in the tree
// and nowhere else, and serves a device record a client parses.

// toolTreeCase is one simulated printer and what its tree must hold.
type toolTreeCase struct {
	name string
	cfg  simulator.Config
	// heads is every head the record names, and off the ones switched off.
	heads []string
	off   []string
	// slots is every feeder slot the record names.
	slots []string
}

// toolTreeCases is each printer family on both wires.
func toolTreeCases() []toolTreeCase {
	families := []toolTreeCase{
		{name: "MINI", cfg: simulator.Config{Model: "MINI"}, heads: []string{"0"}},
		{name: "MK4S", cfg: simulator.Config{Model: "MK4S"}, heads: []string{"0"}},
		{name: "MK3.9", cfg: simulator.Config{Model: "MK3.9"}, heads: []string{"0"}},
		{name: "MK3.5", cfg: simulator.Config{Model: "MK3.5"}, heads: []string{"0"}},
		{name: "CORE One", cfg: simulator.Config{Model: "CoreOne"}, heads: []string{"0"}},
		{
			name: "MK4S with an MMU3", cfg: simulator.Config{Model: "MK4S", MMU: true},
			heads: []string{"0"}, slots: []string{"0.0", "0.1", "0.2", "0.3", "0.4"},
		},
		{
			name: "XL with head 2 switched off", cfg: simulator.Config{Model: "XL", DisabledHeads: []int{2}},
			heads: []string{"0", "1", "2", "3", "4"}, off: []string{"2"},
		},
		{
			name: "CORE One INDX", cfg: simulator.Config{Model: "CoreOne_INDX"},
			heads: []string{"0", "1", "2", "3", "4", "5", "6", "7"},
		},
	}

	out := make([]toolTreeCase, 0, 2*len(families)+1)

	for _, family := range families {
		numbered := family
		numbered.name += " on the numbered wire"
		out = append(out, numbered)

		tree := family
		tree.name += " on the tree wire"
		tree.cfg.ToolTree = true
		out = append(out, tree)
	}

	return append(out, toolTreeCase{
		name:  "two heads with a feeder each on the tree wire",
		cfg:   simulator.Config{Model: "MK4S", ToolTree: true, FeederHeads: 2, FeederSlots: 2},
		heads: []string{"0", "1"}, slots: []string{"0.0", "0.1", "1.0", "1.1"},
	})
}

// outsideTheTree reports whether a state path is one the tree replaced: a
// flat path of the tool printing now, the active tool, or a path of the
// numbered firmware shape.
func outsideTheTree(path string) bool {
	switch path {
	case "/material", "/temperature/nozzle", "/temperature/nozzle_target", "/active_tool":
		return true
	}

	for _, prefix := range []string{"/tool/", "/slot/", "/mmu/"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}

	return false
}

func TestEveryPrinterFamilyHoldsItsToolsInTheTreeAlone(t *testing.T) {
	t.Parallel()

	for i, tc := range toolTreeCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

			cfg := tc.cfg
			cfg.Serial = fmt.Sprintf("SIM-TREE-%02d", i)
			dialInTools(t, running, cfg)

			device, raw := treeSettled(t, running, tc)

			for path := range device.State {
				require.False(t, outsideTheTree(path), "the state holds %s outside the tool tree", path)
			}

			tools := device.Tools
			require.NotNil(t, tools)
			require.Equal(t, len(tc.heads), *tools.ToolCount)
			require.NotNil(t, device.BuildVolume, "the driver reports no build volume")

			for _, id := range append(append([]string{}, tc.heads...), tc.slots...) {
				require.Contains(t, tools.Tools, id, "the record leaves out tool %s", id)
			}

			for _, id := range tc.off {
				require.Nil(t, tools.Tools[id], "a head switched off is null")
			}

			first := tools.Tools["0"]
			require.NotNil(t, first.NozzleTemp, "head 0 has no nozzle reading")
			require.True(t, first.Active, "head 0 is the head printing")
			require.NotNil(t, first.Features)
			require.NotNil(t, first.Features.NozzleDiameterMM, "head 0 names no nozzle")

			// The record parses back into the published shape, field for
			// field, and names no active tool of the whole printer.
			require.NotContains(t, string(raw), `"active_tool"`)

			var back httpapi.Device

			decoder := json.NewDecoder(strings.NewReader(string(raw)))
			decoder.DisallowUnknownFields()
			require.NoError(t, decoder.Decode(&back), "the record does not parse as a Device")
		})
	}
}

// treeSettled waits until the one printer listed has reported its tools and a
// telemetry frame, and answers its record and the record's own bytes.
func treeSettled(t *testing.T, running *process, tc toolTreeCase) (httpapi.Device, []byte) {
	t.Helper()

	var (
		found httpapi.Device
		raw   []byte
	)

	require.Eventually(t, func() bool {
		listed := printers(t, running)
		if len(listed) != 1 || listed[0].Tools == nil || listed[0].Tools.ToolCount == nil {
			return false
		}

		for _, id := range append(append([]string{}, tc.heads...), tc.slots...) {
			if _, held := listed[0].Tools.Tools[id]; !held {
				return false
			}
		}

		head := listed[0].Tools.Tools["0"]
		if head == nil || head.NozzleTemp == nil || !head.Active {
			return false
		}

		read := running.get(t, "/api/v1/devices/"+listed[0].ID)
		found, raw = listed[0], []byte(read.body)

		return true
	}, 2*settle, 25*time.Millisecond, "the printer's tools never settled in the listing")

	return found, raw
}

// TestTheEchoMachineHoldsItsOneHeadInTheTree holds the machine of the echo
// dialect, which the demo runs beside the simulated Prusa printers, to the
// shape every driver with one head writes: one head of the print-head type,
// enabled and active, and the build volume its configuration names.
func TestTheEchoMachineHoldsItsOneHeadInTheTree(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), lines("core:", "  log:", "    level: error",
		"  connectors:", "    "+echoInstance+":", "      type: echo"))

	machine, err := echodevice.New(echodevice.Config{
		BaseURL: "http://" + running.addr, Connector: echoInstance,
		Vendor: "Echo", Model: "echo-1", Serial: "ECHO-TREE-01", Firmware: "1.0.0",
		Report:      50 * time.Millisecond,
		BuildVolume: echodevice.BuildVolume{X: 350, Y: 350, Z: 340},
	})
	require.NoError(t, err)

	ctx, stop := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan error, 1)

	go func() { done <- machine.Run(ctx) }()

	t.Cleanup(func() {
		stop()
		<-done
	})

	device, raw := treeSettled(t, running, toolTreeCase{heads: []string{"0"}})

	for path := range device.State {
		require.False(t, outsideTheTree(path), "the state holds %s outside the tool tree", path)
	}

	require.Equal(t, 1, *device.Tools.ToolCount)
	require.Len(t, device.Tools.Tools, 1, "the record names a tool the machine does not have")

	head := device.Tools.Tools["0"]
	require.NotNil(t, head, "the one head is switched off")
	require.Equal(t, "FFF_PRINT_HEAD", head.Type)
	require.True(t, head.Active, "the one head is the head printing")
	require.NotNil(t, head.NozzleTemp, "the head has no nozzle reading")

	require.Eventually(t, func() bool {
		listed := printers(t, running)

		return len(listed) == 1 && listed[0].BuildVolume != nil
	}, 2*settle, 25*time.Millisecond, "the machine reports no build volume")

	listed := printers(t, running)
	require.Equal(t, httpapi.DeviceBuildVolume{XMM: 350, YMM: 350, ZMM: 340}, *listed[0].BuildVolume)

	require.NotContains(t, string(raw), `"active_tool"`)
}
