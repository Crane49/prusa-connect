package xbuddy

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The tool setters: set_tool_nozzle_diameter, set_tool_hardened and
// set_tool_high_flow. Each travels as SET_VALUE with one kwargs key,
// "tools.<k>.<name>", because that is the key the firmware reads (6.2 and
// later). k counts from 1 on firmware that sends the numbered INFO and from 0
// on firmware that sends the tree-shaped one, as the INFO tools block itself
// does. The tool argument is a head id counted from 0 on every firmware
// (ADR-20261022-6df rule 8 as amended), so the driver adds one for the numbered shape.

// ErrToolBaseUnknown reports a tool setter built before the session read an
// INFO. Until an INFO says which shape the firmware speaks, any key would be a
// guess, and a wrong guess changes the nozzle of another tool.
var ErrToolBaseUnknown = errors.New("xbuddy: the printer has not said how it numbers its tools")

// toolBase is the number the firmware gives its first tool in a setter key.
type toolBase int32

const (
	// toolBaseUnknown is a session that has read no INFO yet.
	toolBaseUnknown toolBase = iota
	// toolBaseOne is firmware that sends the numbered INFO.
	toolBaseOne
	// toolBaseZero is firmware that sends the tree-shaped INFO.
	toolBaseZero
)

// stateBeforeInfo is the device state a tool setter refused before the first
// INFO names. It is UNKNOWN in the state names this driver reports, because
// what is not known yet is what the printer's tools are.
const stateBeforeInfo = "UNKNOWN"

// toolBaseFor is the tool base a command is built with: the one the last INFO
// said for a tool setter, and toolBaseUnknown for any other verb, which reads
// none. A tool setter before any INFO is refused.
func (s *Session) toolBaseFor(capability connectorapi.Capability, form verb) (toolBase, error) {
	if form.toolKey == "" {
		return toolBaseUnknown, nil
	}

	base := toolBase(s.toolBase.Load())
	if base == toolBaseUnknown {
		return base, refuseToolSetter(capability)
	}

	return base, nil
}

// encodeFor renders a command as its frame, and returns the tool numbering a
// tool setter's key was built with. A tool setter before any INFO is refused.
func (s *Session) encodeFor(
	id uint32, capability connectorapi.Capability, form verb, args arguments,
) ([]byte, toolBase, error) {
	base, err := s.toolBaseFor(capability, form)
	if err != nil {
		return nil, base, err
	}

	frame, err := encodeCommand(id, form, args, base)

	return frame, base, err
}

// toolBaseMoved settles a tool setter whose key was built for a numbering the
// printer no longer reports, and reports whether it did. A setter queued
// before a reconnection would otherwise reach firmware that reads its key as
// another tool, as after an update from firmware 7 to 8. Nothing is sent.
func (s *Session) toolBaseMoved(pending *call) bool {
	if pending.base == toolBaseUnknown || toolBase(s.toolBase.Load()) == pending.base {
		return false
	}

	pending.settle(outcome{err: refuseToolSetter(pending.verb)})

	return true
}

// setterLink waits for the connection a command goes out on, as waitLink does.
// A tool setter whose numbering moved meanwhile is settled with its refusal
// and gets no link. The caller's own settle that follows changes nothing,
// because a call settles once.
func (s *Session) setterLink(pending *call) (*link, bool) {
	current, ok := s.waitLink()
	if !ok || s.toolBaseMoved(pending) {
		return nil, false
	}

	return current, true
}

// refuseToolSetter is the answer to a tool setter sent before the printer's
// first INFO on its connection. Nothing is sent, and the caller may send it
// again once the printer has described itself, which it does within seconds of
// connecting.
func refuseToolSetter(verb connectorapi.Capability) error {
	return fault.PrinterVerbNotAllowedInState.
		New("the printer has not sent its INFO yet, so the key its tool setters use is not known").
		With(
			fault.StringParam("verb", verb.String()),
			fault.StringParam("state", stateBeforeInfo),
		).
		Wrap(ErrToolBaseUnknown)
}

// wireKwargs renders the kwargs of a J frame: the one key of a tool setter,
// and every argument under its own name for any other verb.
func (a arguments) wireKwargs(form verb, base toolBase) (map[string]json.RawMessage, error) {
	if form.toolKey != "" {
		return a.toolKwargs(form.toolKey, base)
	}

	return a.kwargs()
}

// toolKwargs renders a tool setter as the one key the firmware reads,
// "tools.<k>.<name>". The tool argument counts from 0, and base says where the
// firmware starts.
func (a arguments) toolKwargs(name string, base toolBase) (map[string]json.RawMessage, error) {
	// The descriptor bounds the tool to a whole number, and the host checks
	// it before a command is built. toolBaseFor refuses an unknown base.
	tool, _ := a.number("tool")
	if base == toolBaseOne {
		tool++
	}

	value, ok := a.values[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q is missing", ErrArgumentUnusable, name)
	}

	encoded, err := encodeArgument(a.declared[name].Kind, value)
	if err != nil {
		return nil, fmt.Errorf("%s argument %q: %w", a.verb, name, err)
	}

	return map[string]json.RawMessage{"tools." + strconv.Itoa(int(tool)) + "." + name: encoded}, nil
}
