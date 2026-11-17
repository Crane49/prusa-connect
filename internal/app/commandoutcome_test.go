package app

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The word the command series is divided by.
//
// A printer that refused a command is not a printer this server could not
// reach, and folding the two hides a firmware saying no behind a network fault:
// a refusal counted as an error moves pcl_printer_errors_total. The cases below
// are the fork, and what divides them is the driver contract on
// connectorapi.Result.

// TestACodedRefusalIsMeasuredAsARefusal is the direction the defect was in.
func TestACodedRefusalIsMeasuredAsARefusal(t *testing.T) {
	t.Parallel()

	refusal := connectorapi.Result{
		OK:        false,
		FaultCode: connectorapi.CommandRefusedWithoutReason.Code().String(),
	}

	require.Equal(t, outcomeRefusal, commandOutcome(refusal, nil),
		"a refusal the driver gave a code is counted as an error")

	explained := connectorapi.Result{
		OK:            false,
		FaultCode:     connectorapi.CommandRefused.Code().String(),
		MachineReason: "FILE_EXISTS",
	}

	require.Equal(t, outcomeRefusal, commandOutcome(explained, nil))
}

// TestAFailureIsStillMeasuredAsAnError is the other direction, and it is what
// keeps the split from swallowing the case the label exists for.
func TestAFailureIsStillMeasuredAsAnError(t *testing.T) {
	t.Parallel()

	require.Equal(t, outcomeError,
		commandOutcome(connectorapi.Result{}, errors.New("the link went away")),
		"a command that failed on the way to the printer is not a refusal")

	require.Equal(t, outcomeError,
		commandOutcome(connectorapi.Result{}, fault.PrinterSessionEnded.New("the session has ended")),
		"a coded failure that is not a refusal is not a refusal")

	// A printer that took the command and could not carry it out is an error
	// too, and its result is not OK either. The error is what says which, so a
	// failure whose result reached this far is still not counted as a refusal.
	require.Equal(t, outcomeError,
		commandOutcome(connectorapi.Result{OK: false},
			connectorapi.CommandFailedWithoutReason.New("the printer could not carry it out")),
		"a command the printer took and failed is counted as a refusal")

	require.Equal(t, outcomeOK, commandOutcome(connectorapi.Result{OK: true}, nil))
}
