package xbuddy_test

import (
	"strings"
	"testing"
	"testing/synctest"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
)

// TestTheSimulatorsToolsReachTheStateTree drives simulated printers through
// the whole session. The first three speak the shipping firmware's numbered
// shape, so this is the boundary the driver converts at. The last three speak
// the tree shape, including a machine with a feeder on each of two heads.
func TestTheSimulatorsToolsReachTheStateTree(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		model  string
		config func(*simulator.Config)
		wanted map[string]string
	}{
		{name: "XL", model: "XL", wanted: map[string]string{
			"/tool_count": "5", "/tools/1/features/nozzle_hardened": "true",
			"/tools/1/features/nozzle_high_flow": "true", "/tools/0/features/nozzle_diameter": "0.4",
			"/tools/4/" + materialMember: `"PVA"`, "/tools/4/feeder/slot_count": "0",
			"/tools/0/active": "true", "/tools/1/active": "false",
			"/tools/2/nozzle_temp": "27", "/build_volume/x": "360", "/build_volume/z": "360",
		}},
		{
			name:   "MK4S with an MMU3",
			model:  "MK4S",
			config: func(cfg *simulator.Config) { cfg.MMU = true },
			wanted: map[string]string{
				"/tool_count":                       "1",
				"/tools/0/feeder/slot_count":        "5",
				"/tools/0.1/" + materialMember:      `"PETG"`,
				"/tools/0.4/" + materialMember:      `""`,
				"/tools/0/features/nozzle_diameter": "0.4",
				"/tools/0/active":                   "true",
				"/tools/0.0/active":                 "true",
				"/tools/0.1/active":                 "false",
				"/tools/0/feeder/type":              `"MMU"`,
				"/tools/0/feeder/mmu_progress_code": "0",
			},
		},
		{name: "MK4S", model: "MK4S", wanted: map[string]string{
			"/tool_count": "1", "/tools/0/feeder/slot_count": "0", "/tools/0/" + materialMember: `"PLA"`,
			"/tools/0/active": "true"}},
		{
			name:   "XL in the tree shape",
			model:  "XL",
			config: func(cfg *simulator.Config) { cfg.ToolTree = true },
			wanted: map[string]string{
				"/tool_count": "5", "/tools/1/features/nozzle_hardened": "true", "/tools/4/" + materialMember: `"PVA"`,
				"/tools/3/feeder/slot_count": "0", "/tools/2/nozzle_temp": "27",
				"/tools/1/odometer_id": "1", "/tools/1/odometer_e": "0",
			},
		},
		{name: "MK4S with an MMU3 in the tree shape", model: "MK4S", config: func(cfg *simulator.Config) {
			cfg.ToolTree, cfg.MMU = true, true
		}, wanted: map[string]string{
			"/tool_count":                       "1",
			"/tools/0/feeder/slot_count":        "5",
			"/tools/0/feeder/model":             `"MMU3"`,
			"/tools/0.3/" + materialMember:      `"ASA"`,
			"/tools/0.4/" + materialMember:      `""`,
			"/tools/0/feeder/base_model":        `"MMU3"`,
			"/tools/0/feeder/type":              `"MMU"`,
			"/tools/0/feeder/mmu_progress_code": "0",
			"/tools/0.2/odometer_id":            "1",
		}},
		{name: "two heads each with a feeder", model: "MK4S", config: func(cfg *simulator.Config) {
			cfg.ToolTree, cfg.FeederHeads, cfg.FeederSlots = true, 2, 4
		}, wanted: map[string]string{
			"/tool_count":                       "2",
			"/tools/0/feeder/slot_count":        "4",
			"/tools/1/feeder/slot_count":        "4",
			"/tools/1/features/nozzle_hardened": "true",
			"/tools/1.2/" + materialMember:      `"ASA"`,
			"/tools/1.3/" + materialMember:      `""`,
			"/tools/1/nozzle_temp":              "26",
			"/tools/0/active":                   "true",
			"/tools/1/active":                   "false",
			"/tools/1.0/active":                 "true",
			"/tools/1.1/active":                 "false",
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				rig := start(t, tc.model, withAutoRegistration,
					withPrinter(func(cfg *simulator.Config) {
						if tc.config != nil {
							tc.config(cfg)
						}
					}))
				defer rig.stop(t)

				session := rig.connect(t)
				seen := map[string]string{}

				for !holdsValues(seen, tc.wanted) {
					select {
					case patch, open := <-session.State():
						if !open {
							t.Fatal("the state channel closed before the tools were reported")
						}

						seen[patch.Path] = string(patch.Value)
					case <-rig.ctx.Done():
						t.Fatalf("the tools did not arrive: %v", seen)
					}
				}

				// The 1-based paths and the flat paths of the tool printing
				// now are only ever removed, for a tree an earlier release
				// stored. The tree is the only place a tool lives.
				for path, value := range seen {
					if value != "null" && (strings.HasPrefix(path, "/slot/") || flatPath(path)) {
						t.Errorf("the driver wrote %s outside the tool tree", path)
					}
				}
			})
		})
	}
}

// materialMember is where a tool's material sits under its id.
const materialMember = "material_package_instance/package/material/abbreviation"

func holdsValues(seen, wanted map[string]string) bool {
	for path, value := range wanted {
		if seen[path] != value {
			return false
		}
	}

	return true
}

// flatPath reports whether a path is one an earlier release wrote beside the
// tool tree.
func flatPath(path string) bool {
	switch path {
	case "/active_tool", "/material", "/temperature/nozzle", "/temperature/nozzle_target":
		return true
	}

	return false
}
