package bridge

import (
	"slices"

	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
)

// The property every state rule reads, and the enumeration of its values.
const (
	stateInterface   fractal.InterfaceName = "Printer1"
	stateProperty    fractal.PropertyName  = "state"
	printerStateEnum fractal.Identifier    = "PrinterState"
)

// allowed checks the state rules of one method against the stored device.
// The host checks none of them.
func (o *owner) allowed(mapped *MethodMap) error {
	device, held := o.bridge.device(o.device)
	if !held {
		return answerWith(CodeDeviceGone)
	}

	if len(mapped.OnlyIn) > 0 || len(mapped.RefuseIn) > 0 {
		state := o.bridge.mapping.printerState(device)
		if len(mapped.OnlyIn) > 0 && !slices.Contains(mapped.OnlyIn, state) || slices.Contains(mapped.RefuseIn, state) {
			return answerWith(CodeStateRefused)
		}
	}

	if !o.hasOneOf(mapped.NeedsOneOf, device) {
		return answerWith(CodeNoTargetTemperature)
	}

	if mapped.ActiveHeadOnly && !isActiveHead(device.State, o.tool) {
		return answerWith(CodeHeadNotActive)
	}

	return nil
}

// printerState returns the value of Printer1.state for one device, and ""
// when the table maps no state.
func (m *Mapping) printerState(device deviceapi.Device) string {
	iface, ok := m.Interface(stateInterface)
	if !ok {
		return ""
	}

	row, ok := iface.Property(stateProperty)
	if !ok {
		return ""
	}

	value, read := row.read(device, "")
	if !read {
		return ""
	}

	state, err := schema.ReadString(value)
	if err != nil {
		return ""
	}

	return state
}

// hasOneOf reports whether one of the named properties has a value that is
// not null and not excluded. With no names it reports true.
func (o *owner) hasOneOf(needs map[fractal.PropertyName][]string, device deviceapi.Device) bool {
	if len(needs) == 0 {
		return true
	}

	for name, excluded := range needs {
		row, mapped := o.iface.Property(name)
		if !mapped {
			continue
		}

		value, read := row.read(device, o.tool)
		if !read || schema.IsNull(value) {
			continue
		}

		if word, err := schema.ReadString(value); err == nil && slices.Contains(excluded, word) {
			continue
		}

		return true
	}

	return false
}
