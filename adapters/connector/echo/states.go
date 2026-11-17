package echo

import (
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// The states an echo device reports. They are the five words every dialect in
// this tree already maps its own onto, so a client that reads this set reads
// what a printer answers with (ADR-20260831-c7b rule 5).
const (
	// StateIdle is a device that is on, reachable and printing nothing.
	StateIdle connectorapi.StateName = "IDLE"
	// StatePrinting is a device working through a file.
	StatePrinting connectorapi.StateName = "PRINTING"
	// StateFinished is a device that reached the end of a file.
	StateFinished connectorapi.StateName = "FINISHED"
	// StateAttention is a device waiting for a person.
	StateAttention connectorapi.StateName = "ATTENTION"
	// StateError is a device that stopped on a fault.
	StateError connectorapi.StateName = "ERROR"
)

// states is the declaration this driver's package holds. It is a static
// composite literal and never a call into a session, because the host reads a
// declaration from what it installed and never from a running module
// (ADR-20260831-c7b rule 3).
//
//nolint:gochecknoglobals // the declaration table for this adapter, read-only after init.
var states = connectorapi.StateSet{
	States: []connectorapi.DeviceState{
		{Name: StateIdle, DisplayName: "Idle", Accepts: []connectorapi.Capability{
			VerbStatus, VerbNozzleTarget, VerbStartPrint,
		}},
		{Name: StatePrinting, DisplayName: "Printing", Accepts: []connectorapi.Capability{
			VerbStatus, VerbNozzleTarget,
		}},
		{Name: StateFinished, DisplayName: "Finished", Accepts: []connectorapi.Capability{
			VerbStatus, VerbNozzleTarget,
		}},
		{Name: StateAttention, DisplayName: "Needs attention", Accepts: []connectorapi.Capability{
			VerbStatus, VerbNozzleTarget,
		}},
		{Name: StateError, DisplayName: "Error", Accepts: []connectorapi.Capability{VerbStatus}},
	},
	Transitions: []connectorapi.Transition{
		{From: StateIdle, To: StatePrinting, Verb: VerbStartPrint},
		{From: StatePrinting, To: StateFinished},
		{From: StatePrinting, To: StateAttention},
		{From: StatePrinting, To: StateError},
		{From: StateAttention, To: StatePrinting},
		{From: StateAttention, To: StateError},
		{From: StateFinished, To: StateIdle},
		{From: StateError, To: StateIdle},
	},
}

// States is what a device this driver drives reports it can be in, and the
// moves between two of those states.
//
// A print starts only from IDLE, so start_print is accepted there and nowhere
// else. A status read is accepted in every state, and a nozzle target in every
// state but ERROR.
//
// One transition names a verb and seven do not, which is what
// connectorapi.Transition.Verb describes: a caller starts a print and the
// machine reaches every other state by itself.
//
// It returns a copy, so a host placing the set cannot edit what this driver
// declared.
func States() connectorapi.StateSet {
	return states.Clone()
}
