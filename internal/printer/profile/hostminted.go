package profile

import (
	"slices"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// The raw gcode pair and its argument. They are spelled here and not imported
// from internal/connector/xbuddy, because that package imports this one.
// TestTheHostMintedKeysAreInTheDriverBaseline holds the two copies together.
const (
	sendGCodeVerb  connectorapi.Capability = "core.command.send_gcode"
	forceGCodeVerb connectorapi.Capability = "core.command.force_gcode"
	gcodeArgument                          = "gcode"
)

// The nozzle setpoint's tool argument. The shipping matrix declares the verb
// with one argument on every model, so a tool changer could heat only its
// active head. The numbering is the set_tool_* commands' own: a head id from
// 0, as /tools/<id> publishes it.
const nozzleTemperatureVerb connectorapi.Capability = "core.command.set_nozzle_temperature"
const toolArgument = "tool"

// fileUploadFeature is the generic capability a host asks about when it wants to
// know whether a printer can be sent a file. The shipping matrix has no row for
// it: it names each dialect's own transfer commands instead, and this is the key
// that stands for all of them.
const fileUploadFeature = connectorapi.VerbFileUpload

// HostMinted is the descriptor keys the generated profile of model carries that
// the shipping firmware matrix has no row for. Most are on every model; the
// nozzle's tool argument is only on a model whose slots are toolheads.
//
// The matrix describes named firmware commands and knows nothing about raw
// gcode. Without this list the profile tier would subtract a verb this
// repository added on every model, and nothing could reach it. The keys are
// written into the generated file from this one list, so profiles.json is a
// function of the matrix and this list, and a drift in either shows as a diff
// in the divergence test.
//
// A key here is still bounded by the driver's baseline, because the merge is
// an intersection. This list grants nothing. It stops the profile tier from
// removing a key no profile mentions.
func HostMinted(model string) connectorapi.Set {
	set := connectorapi.Set{}

	for _, verb := range []connectorapi.Capability{sendGCodeVerb, forceGCodeVerb} {
		set[verb] = connectorapi.Value{Flag: true}
		set[connectorapi.ArgumentKey(verb, gcodeArgument)] = connectorapi.Value{
			Flag:     true,
			Kind:     connectorapi.KindString,
			Required: true,
		}
	}

	// A flag and no arguments: it is not a command, so there is nothing to
	// declare about how it is called.
	set[fileUploadFeature] = connectorapi.Value{Flag: true}

	if slices.ContainsFunc(modelDocs, func(doc ModelDoc) bool { return doc.Model == model && doc.ToolChanger }) {
		set[connectorapi.ArgumentKey(nozzleTemperatureVerb, toolArgument)] = connectorapi.Value{
			Flag: true,
			Kind: connectorapi.KindInteger,
		}
	}

	return set
}
