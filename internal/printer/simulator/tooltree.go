package simulator

import (
	"strconv"
)

// What a simulated printer says about its tools in the tree shape of the
// public Prusa architecture
// (https://specs.prusa3d.tech/architecture/#/printer_configuration, printer
// configuration and printer telemetry). The shipping firmware sends the
// numbered shape in tools.go; this is the shape a firmware that speaks the
// architecture sends, so a driver's reading of it can be driven end to end.

// treeHead is one head and the slots of the feeder on it, if any.
type treeHead struct {
	tool  simTool
	slots []string
}

// treeHeads is the printer's heads. Each XL head has no feeder, an MMU is a
// feeder on the one head, and FeederHeads asks for a feeder on every head.
func (p *Printer) treeHeads() []treeHead {
	if p.cfg.FeederHeads > 0 {
		slots := max(p.cfg.FeederSlots, 1)
		out := make([]treeHead, 0, p.cfg.FeederHeads)

		for h := range p.cfg.FeederHeads {
			head := treeHead{tool: p.fitted(h, simTool{nozzle: standardNozzle, hardened: h%2 == 1})}
			for s := range slots {
				head.slots = append(head.slots, mmuSlots[(h+s)%len(mmuSlots)])
			}

			out = append(out, head)
		}

		return out
	}

	if p.hasMMU() {
		return []treeHead{{tool: p.fitted(0, simTool{nozzle: standardNozzle}), slots: mmuSlots[:]}}
	}

	tools := p.tools()
	out := make([]treeHead, 0, len(tools))

	for _, tool := range tools {
		out = append(out, treeHead{tool: tool})
	}

	return out
}

// treeMaterial is a material package instance naming one material, or nil
// for a tool with nothing loaded.
func treeMaterial(material string) any {
	if material == "" {
		return nil
	}

	return map[string]any{"package": map[string]any{"material": map[string]any{"abbreviation": material}}}
}

// treeInfo writes tool_count and the tools object INFO carries.
func (p *Printer) treeInfo(into map[string]any) {
	heads := p.treeHeads()
	tools := map[string]any{}

	for h, head := range heads {
		id := strconv.Itoa(h)
		if p.headDisabled(h) {
			tools[id] = nil

			continue
		}

		entry := map[string]any{
			"type": "FFF_PRINT_HEAD",
			"features": map[string]any{
				"nozzle_diameter":  head.tool.nozzle,
				"nozzle_high_flow": head.tool.highFlow,
				"nozzle_hardened":  head.tool.hardened,
			},
			"feeder": nil,
		}

		if len(head.slots) == 0 {
			if !p.cfg.NoMaterial {
				entry["material_package_instance"] = treeMaterial(head.tool.material)
			}
		} else {
			entry["feeder"] = map[string]any{
				"type": "MMU", "model": "MMU3", "base_model": "MMU3", "slot_count": len(head.slots),
			}

			for s, material := range head.slots {
				slot := map[string]any{"type": "FFF_FEEDER_SLOT"}
				if !p.cfg.NoMaterial {
					slot["material_package_instance"] = treeMaterial(material)
				}

				tools[id+"."+strconv.Itoa(s)] = slot
			}
		}

		tools[id] = entry
	}

	into["tool_count"] = len(heads)
	into["tools"] = tools
}

// treeTelemetry is the tools object of one telemetry frame. The first head is
// picked. Each head with a feeder has the slot the print has reached loaded
// into it, so a head that is not picked still has an active slot, as the
// architecture marks it. The caller holds the lock.
func (p *Printer) treeTelemetry(printing bool) map[string]any {
	heads := p.treeHeads()
	tools := map[string]any{}

	temp, target := ambientTemp, 0.0
	if printing {
		temp, target = printingNozzleTemp, printingNozzleTemp
	}

	for h, head := range heads {
		id := strconv.Itoa(h)
		if p.headDisabled(h) {
			tools[id] = nil

			continue
		}

		entry := map[string]any{
			"active":             h == 0,
			"nozzle_temp":        temp + float64(h),
			"nozzle_target_temp": nil,
			"print_fan_rpm":      0,
			"heatbreak_fan_rpm":  xlFanHotend,
		}

		if target > 0 && h == 0 {
			entry["nozzle_target_temp"] = target
		}

		tools[id] = entry

		if len(head.slots) == 0 {
			p.treeOdometer(entry, h == 0 && printing)

			continue
		}

		code, command := p.mmuWork(printing && h == 0)
		unit := map[string]any{"mmu_progress_code": code}

		if command != "" {
			unit["mmu_command_in_progress"] = command
		}

		entry["feeder"] = unit

		loaded := 0
		if printing {
			loaded = min(p.progress()*len(head.slots)/fullProgress, len(head.slots)-1)
		}

		for s := range head.slots {
			slot := map[string]any{"active": s == loaded}
			p.treeOdometer(slot, h == 0 && printing && s == loaded)
			tools[id+"."+strconv.Itoa(s)] = slot
		}
	}

	return tools
}

// treeOdometer writes the odometers of a tool that can take a filament: a
// head with no feeder, or a slot. The tool printing now has extruded a length
// that grows with the print, and every other tool nothing since its reset.
// The id is the same for the whole session: the simulated odometers are
// never reset.
func (p *Printer) treeOdometer(entry map[string]any, printing bool) {
	extruded := 0.0
	if printing {
		extruded = float64(p.progress()) * odometerMMPerPercent
	}

	entry["odometer_id"] = 1
	entry["odometer_e"] = extruded
}

// odometerMMPerPercent is how much filament the simulated print extrudes for
// each percent of progress, in millimetres.
const odometerMMPerPercent = 120.0
