package bridge

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
)

// The readings a property row derives from the tool tree, by the name its
// "derive" member gives them.
const (
	// deriveHeadMaterial is the material of a head: its own, or the material
	// of the slot of its feeder that is loaded into it.
	deriveHeadMaterial = "headMaterial"
	// deriveActiveSlot is the slot of the multi-material unit loaded into its
	// head, counted from 1, 0 with no slot loaded, and unknown with no unit.
	deriveActiveSlot = "activeSlot"
)

// derivations is every name a "derive" member may take.
//
//nolint:gochecknoglobals // a fixed set, read-only after init.
var derivations = map[string]struct{}{deriveHeadMaterial: {}, deriveActiveSlot: {}}

// firstHead is the head every printer has. A machine with one nozzle reports
// it as head 0.
const firstHead connectorapi.ToolID = "0"

// derive returns one reading the tool tree of the device gives for the tool of
// an instance, as JSON, and false when the tree does not say.
func derive(name string, device deviceapi.Device, tool connectorapi.ToolID) (json.RawMessage, bool) {
	tree := connectorapi.ReadTools(device.State)

	switch name {
	case deriveHeadMaterial:
		return headMaterial(tree, tool)
	case deriveActiveSlot:
		return activeSlot(tree)
	}

	return nil, false
}

// activeSlot returns the slot loaded into the head of the multi-material
// unit, counted from 1, and 0 when none is loaded. With no unit it is false.
func activeSlot(tree connectorapi.ToolTree) (json.RawMessage, bool) {
	head, fitted := unitHead(tree)
	if !fitted {
		return nil, false
	}

	for _, slot := range slotNumbers(tree, head) {
		if tree.IsActive(slot.tool) {
			return schema.RawInt(int64(slot.number)), true
		}
	}

	return schema.RawInt(0), true
}

// slotNumbers returns the slots of the feeder on one head with their number
// in the vocabulary, in order: slot k of the feeder is number k+1.
func slotNumbers(tree connectorapi.ToolTree, head connectorapi.ToolID) []numbered {
	var out []numbered

	for _, id := range tree.Slots(head) {
		k, err := strconv.Atoi(strings.TrimPrefix(string(id), string(head)+"."))
		if err == nil {
			out = append(out, numbered{tool: id, number: k + 1})
		}
	}

	return out
}

// headMaterial returns the material of one head. For a head with a feeder it
// is the material of the loaded slot, or NoMaterial when no slot is loaded.
func headMaterial(tree connectorapi.ToolTree, head connectorapi.ToolID) (json.RawMessage, bool) {
	tool := tree.Tool(head)
	if tool == nil {
		return nil, false
	}

	if !tool.HasFeeder() {
		if tool.Material == nil {
			return nil, false
		}

		return jsonString(*tool.Material), true
	}

	slots := tree.Slots(head)
	everySlotSaid := len(slots) == tool.Feeder.SlotCount

	for _, id := range slots {
		slot := tree.Tool(id)
		if slot.Active == nil {
			everySlotSaid = false

			continue
		}

		if *slot.Active {
			if slot.Material == nil {
				return nil, false
			}

			return jsonString(*slot.Material), true
		}
	}

	if !everySlotSaid {
		return nil, false
	}

	return jsonString(connectorapi.NoMaterial), true
}

// isToolPath reports whether a path is one member of one tool. A derived
// reading may read any of them.
func isToolPath(path string) bool {
	_, _, ok := connectorapi.SplitToolPath(path)

	return ok
}

// numbered is one tool of an interface with one instance per tool: its id and
// its number in the instance id, counted from 1.
type numbered struct {
	tool   connectorapi.ToolID
	number int
}

// toolsOf returns the tools of one device that get an instance each: the
// heads for "head", the slots of the multi-material unit for "slot".
func toolsOf(per string, state map[string]json.RawMessage) []numbered {
	tree := connectorapi.ReadTools(state)

	switch per {
	case perHead:
		return heads(tree)
	case perSlot:
		return slots(tree)
	}

	return nil
}

// heads returns head 0 and every other enabled print head, numbered from 1.
// A disabled tool has no type, so the type check drops it too.
func heads(tree connectorapi.ToolTree) []numbered {
	out := []numbered{{tool: firstHead, number: 1}}

	for _, id := range tree.Heads() {
		if id == firstHead || tree.Tool(id).Type != connectorapi.ToolTypeHead {
			continue
		}

		if n, err := strconv.Atoi(string(id)); err == nil {
			out = append(out, numbered{tool: id, number: n + 1})
		}
	}

	return out
}

// slots returns the slots of the multi-material unit, numbered from 1.
func slots(tree connectorapi.ToolTree) []numbered {
	head, fitted := unitHead(tree)
	if !fitted {
		return nil
	}

	return slotNumbers(tree, head)
}

// unitHead returns the head the multi-material unit sits on: the first head
// with a feeder. It reports false with none.
func unitHead(tree connectorapi.ToolTree) (connectorapi.ToolID, bool) {
	for _, head := range tree.Heads() {
		if tree.Tool(head).HasFeeder() {
			return head, true
		}
	}

	return "", false
}

// isActiveHead reports whether the printer acts on this head: the head it has
// picked, or head 0 when it names none and has one head.
func isActiveHead(state map[string]json.RawMessage, head connectorapi.ToolID) bool {
	tree := connectorapi.ReadTools(state)
	if tool := tree.Tool(head); tool != nil && tool.Disabled {
		return false
	}

	if tree.Active == "" {
		return head == firstHead && len(heads(tree)) == 1
	}

	return tree.Active.Head() == head
}

// namesTools reports whether a path can add a tool: the enabled or the type
// member of a tool, or the slot count of a feeder.
func namesTools(path string) bool {
	_, member, ok := connectorapi.SplitToolPath(path)

	return ok && (member == connectorapi.ToolEnabled || member == connectorapi.ToolType ||
		member == connectorapi.FeederSlotCount)
}
