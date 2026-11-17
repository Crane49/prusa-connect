package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The id a refused command is recorded under, on the refusal a caller holds.
//
// withCommandID says the id is on every refusal, and it was on two codes:
// fault.With drops a param the code does not declare, so for every other
// refusal the id went nowhere and the log line carried "[undeclared params:
// command_id]" where the promise was supposed to be. A caller holding such a
// refusal held a record it could not address, which is the whole reason the id
// is minted before the authorization check.

// refusalPath is every code a refused or failed command can carry: the two the
// host decides before any machine is reached, the descriptor's, the dialect's,
// and the printer's own. The printer's refusals reach a caller on the result
// rather than as a fault (connectorapi.Result), and the registry declares
// command_id on them either way, so they are held to the same promise.
func refusalPath() map[string]*fault.Fault {
	return map[string]*fault.Fault{
		"session ended": fault.PrinterSessionEnded.New("the printer's session has ended"),
		"access denied": fault.New(authz.CodeAccessDenied(), "that verb is conferred by no role"),
		"capability":    connectorapi.CapabilityNotSupported.New("the printer does not have that capability"),
		"queue full": connectorapi.CommandQueueFull.New(
			"the printer is already holding as many commands as it takes",
		),
		"path not allowed": connectorapi.PathNotAllowed.New("the path leaves the storage it names"),
		"printer changed":  connectorapi.PrinterChanged.New("the printer changed while the command was waiting"),
		"unanswered":       connectorapi.CommandUnanswered.New("the printer lost its connection"),
		"argument unknown": connectorapi.ArgumentUnknown.New("the command does not accept that argument"),
		"argument missing": connectorapi.ArgumentMissing.New("the command needs an argument that was not supplied"),
		"argument value":   connectorapi.ArgumentNotSupported.New("the printer does not support that value"),
		"argument type":    connectorapi.ArgumentMalformed.New("the argument is not readable as the type it declares"),
		"argument range": connectorapi.ArgumentOutOfRange.New(
			"the argument is outside the range the printer accepts",
		),
		"gcode malformed":   connectorapi.GCodeMalformed.New("the payload is not one gcode command"),
		"gcode not allowed": connectorapi.GCodeNotAllowed.New("this server never sends that gcode word"),
		"refused":           connectorapi.CommandRefused.New("the printer refused the command"),
		"refused in silence": connectorapi.CommandRefusedWithoutReason.
			New("the printer refused the command and gave no reason for it"),
		"failed": connectorapi.CommandFailed.New("the printer took the command and could not carry it out"),
		"failed in silence": connectorapi.CommandFailedWithoutReason.
			New("the printer took the command, could not carry it out, and gave no reason for it"),
	}
}

// TestEveryRefusalCarriesTheCommandID is the gate on the promise. It is a table
// over the codes and not one call, because the defect was one code declaring
// the param and the next one not.
func TestEveryRefusalCarriesTheCommandID(t *testing.T) {
	t.Parallel()

	const commandID = "01JQZK9V2X4M8N6P0R3T5W7Y9B"

	for name, refusal := range refusalPath() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var carried *fault.Fault
			require.ErrorAs(t, withCommandID(refusal, commandID), &carried)

			require.Equal(t, commandID, paramOf(t, carried, "command_id"),
				"the refusal reaches the caller with no command id, so the row it wrote is unaddressable")

			// The log line is the other half. A dropped param is named there,
			// so a code that stopped declaring command_id would read as a
			// refusal promising an id it did not carry.
			require.NotContains(t, carried.Error(), "undeclared params",
				"the code does not declare command_id: %v", carried)
		})
	}
}

// TestARefusalWithNoIDIsHandedBackUnchanged is the case there is nothing to
// carry. A mint that could not draw entropy skips the record and lets the
// command run, so the refusal has no id to put anywhere.
func TestARefusalWithNoIDIsHandedBackUnchanged(t *testing.T) {
	t.Parallel()

	refusal := connectorapi.CommandQueueFull.New("the printer is already holding as many commands as it takes")

	require.Equal(t, refusal, withCommandID(refusal, ""))
	require.NotContains(t, refusal.Error(), "command_id")
}
