package simulator

import (
	"slices"
	"strconv"
	"strings"
)

// What a simulated printer says about its tools, in the shape the firmware's
// INFO event and telemetry slot block take (src/connect/render.cpp).

// mmuModels are the models whose firmware is built with MMU support, so their
// INFO carries an mmu block whether or not a unit is fitted. They are the
// models with an MMU3 variant in include/common/printer_model_data.hpp.
//
//nolint:gochecknoglobals // a fixed list, read-only.
var mmuModels = []string{"MK3.5", "MK3.5S", "MK3.9", "MK3.9S", "MK4", "MK4S", "COREONE", "COREONEL"}

// mmuVersion is the unit firmware a fitted MMU3 reports.
const mmuVersion = "3.0.3"

// noMMUVersion is what the firmware writes when no unit answers.
const noMMUVersion = "0.0.0"

// standardNozzle is the nozzle every simulated tool carries, in millimetres.
const standardNozzle = 0.4

// mmuSlots is the filament in each of an MMU3's five slots. The empty one is
// a slot with nothing loaded, which the firmware reports as "---".
//
//nolint:gochecknoglobals // a fixed table, read-only.
var mmuSlots = [...]string{"PLA", "PETG", "PLA", "ASA", ""}

// canTakeMMU reports whether the model's firmware is built with MMU support.
func canTakeMMU(model string) bool {
	return slices.Contains(mmuModels, strings.ToUpper(model))
}

// hasMMU reports whether this printer has an MMU fitted and enabled.
func (p *Printer) hasMMU() bool { return p.cfg.MMU && canTakeMMU(p.model.Name) }

// simTool is one tool as this simulator describes it.
type simTool struct {
	material string
	nozzle   float64
	highFlow bool
	hardened bool
}

// nozzle is the nozzle the tool setters put on one tool.
type nozzle struct {
	diameter float64
	highFlow bool
	hardened bool
}

// fitted is tool i with the nozzle a tool setter put on it, if one did. The
// caller holds the lock.
func (p *Printer) fitted(i int, tool simTool) simTool {
	if n, set := p.nozzles[i]; set {
		tool.nozzle, tool.highFlow, tool.hardened = n.diameter, n.highFlow, n.hardened
	}

	return tool
}

// tools is every tool the printer has enabled, in order, with the nozzles the
// tool setters put on them. The caller holds the lock.
func (p *Printer) tools() []simTool {
	out := p.builtTools()
	for i := range out {
		out[i] = p.fitted(i, out[i])
	}

	return out
}

// reportedHeads is the tool of every head the printer reports, in the shape it
// speaks, with the nozzles the tool setters put on them. The caller holds the
// lock.
func (p *Printer) reportedHeads() []simTool {
	if !p.cfg.ToolTree {
		return p.tools()
	}

	heads := p.treeHeads()
	out := make([]simTool, len(heads))

	for i, head := range heads {
		out[i] = head.tool
	}

	return out
}

// setNozzle runs one tool setter, as a person who swapped the nozzle tells the
// printer: it changes what the printer reports for that head from now on. It
// refuses a head the printer does not have. The profile has checked the
// value's type already.
func (p *Printer) setNozzle(command jsonCommand) (reason string, refused bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	head, _ := command.Kwargs["tool"].(float64)
	i := int(head)

	heads := p.reportedHeads()
	if i < 0 || i >= len(heads) || float64(i) != head {
		return ReasonInvalidArgument, true
	}

	n := nozzle{diameter: heads[i].nozzle, highFlow: heads[i].highFlow, hardened: heads[i].hardened}

	switch command.Command {
	case "SET_TOOL_NOZZLE_DIAMETER":
		n.diameter, _ = command.Kwargs["nozzle_diameter"].(float64)
	case "SET_TOOL_HIGH_FLOW":
		n.highFlow, _ = command.Kwargs["high_flow"].(bool)
	case "SET_TOOL_HARDENED":
		n.hardened, _ = command.Kwargs["hardened"].(bool)
	}

	if p.nozzles == nil {
		p.nozzles = map[int]nozzle{}
	}

	p.nozzles[i] = n

	return "", false
}

// builtTools is every tool the printer has enabled, in order, as it was built.
func (p *Printer) builtTools() []simTool {
	switch {
	case p.multiHead():
		out := make([]simTool, 0, len(p.headTable()))
		for i, tool := range p.headTable() {
			// The second head carries a hardened high-flow nozzle, so a
			// reader can tell the heads apart by more than the material.
			special := i == 1
			out = append(
				out,
				simTool{material: tool.material, nozzle: standardNozzle, highFlow: special, hardened: special},
			)
		}

		return out
	case p.hasMMU():
		out := make([]simTool, 0, len(mmuSlots))
		for _, material := range mmuSlots {
			out = append(out, simTool{material: material, nozzle: standardNozzle})
		}

		return out
	}

	return []simTool{{material: "PLA", nozzle: standardNozzle}}
}

// headDisabled reports whether the head counted i from 0 is switched off.
func (p *Printer) headDisabled(i int) bool { return slices.Contains(p.cfg.DisabledHeads, i) }

// infoTools is the tools block, the mmu block and the slot count INFO
// carries.
func (p *Printer) infoTools(into map[string]any) {
	tools := p.tools()
	block := make(map[string]any, len(tools))

	for i, tool := range tools {
		if p.headDisabled(i) {
			continue
		}

		material := tool.material
		if material == "" {
			material = "---"
		}

		entry := map[string]any{
			"nozzle_diameter": tool.nozzle,
			"high_flow":       tool.highFlow,
			"hardened":        tool.hardened,
			"material":        material,
		}

		// A printer set to report no material says nothing of it here either,
		// so the host keeps what a person said is loaded.
		if p.cfg.NoMaterial {
			delete(entry, "material")
		}

		block[strconv.Itoa(i+1)] = entry
	}

	into["tools"] = block
	into["nozzle_diameter"] = standardNozzle

	if len(tools) > 0 {
		into["nozzle_diameter"] = tools[0].nozzle
	}
	into["slots"] = len(tools)

	if canTakeMMU(p.model.Name) {
		version := noMMUVersion
		if p.hasMMU() {
			version = mmuVersion
		}

		into["mmu"] = map[string]any{"enabled": p.hasMMU(), "version": version}
	}
}

// mmuSlotFields builds the slot block of a printer with an MMU. One nozzle
// serves every slot, so every slot reports its temperature. The caller holds
// the lock.
func (p *Printer) mmuSlotFields(printing bool, nozzle float64) map[string]any {
	active := 1
	if printing {
		active = min(p.progress()*2/fullProgress, 1) + 1
	}

	code, command := p.mmuWork(printing)
	slot := map[string]any{"active": active, "state": code, "command": command}

	for i, material := range mmuSlots {
		entry := map[string]any{
			"material":   material,
			"temp":       nozzle,
			"fan_print":  0,
			"fan_hotend": xlFanHotend,
		}

		if p.cfg.NoMaterial {
			delete(entry, "material")
		}

		slot[strconv.Itoa(i+1)] = entry
	}

	return slot
}

// The MMU progress codes and the command the simulator reports (the
// ProgressCode enum of the public MMU firmware, Prusa-Firmware-Buddy
// lib/Prusa-Firmware-MMU/src/logic/progress_codes.h).
const (
	mmuProgressOK            = 0
	mmuProgressSelectingSlot = 18
	mmuToolChange            = "T"
	// mmuSwitchWindow is how many percent of progress after each switch of
	// slot the simulated unit reports a toolchange in progress.
	mmuSwitchWindow = 3
	// mmuSwitchEvery is the progress, in percent, between two switches of
	// slot: the simulated print uses two slots, one for each half.
	mmuSwitchEvery = fullProgress / 2
)

// mmuWork is the MMU's progress code and the command it runs: a toolchange
// for a moment after the print switches slot, and idle otherwise. An idle
// unit's command is empty, as the firmware formats its command 0. The caller
// holds the lock.
func (p *Printer) mmuWork(printing bool) (int, string) {
	if printing && p.progress()%mmuSwitchEvery < mmuSwitchWindow {
		return mmuProgressSelectingSlot, mmuToolChange
	}

	return mmuProgressOK, ""
}

// SetMMU fits or removes the MMU while the printer runs, as a person does when
// they switch the unit on or off in the menu. The printer then volunteers an
// INFO, so the host learns of the slots that came or went without asking. It
// reports whether that INFO reached the wire: a printer with no session sends
// it on the next SEND_INFO instead.
func (p *Printer) SetMMU(fitted bool) bool {
	p.mu.Lock()
	p.cfg.MMU = fitted
	p.mu.Unlock()

	return p.volunteerInfo()
}

// SetDisabledHeads switches off exactly these heads, counted from 0, while the
// printer runs, and volunteers an INFO as SetMMU does.
func (p *Printer) SetDisabledHeads(heads ...int) bool {
	p.mu.Lock()
	p.cfg.DisabledHeads = slices.Clone(heads)
	p.mu.Unlock()

	return p.volunteerInfo()
}

// volunteerInfo sends an INFO that answers no command.
func (p *Printer) volunteerInfo() bool {
	p.mu.Lock()
	conn := p.conn
	p.mu.Unlock()

	if conn == nil {
		return false
	}

	return p.send(conn, event{Event: eventInfo, State: p.State(), Data: p.infoData()}) == nil
}
