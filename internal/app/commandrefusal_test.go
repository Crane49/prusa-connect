package app

import (
	"encoding/json"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The record of a command that was refused, which is ADR-20260901-atx rule 4 and the
// correction the live run of 2026-09-01 asked for.
//
// The rule says the id is minted before the authorization check so that a
// refusal is addressable. It was minted after, so an operator reaching for a
// verb they may not send answered 403 with no id, published no frame and stored
// no row: the reach existed in the access log and nowhere else.

// TestARefusedCommandIsRecordedAndAddressable is the gate. A caller who reaches
// the device and not the verb gets an id back on the refusal, and the device's
// origin carries the two frames that say what was asked and that it was not
// allowed.
func TestARefusedCommandIsRecordedAndAddressable(t *testing.T) {
	t.Parallel()

	// In a bubble: a reader attached before the printer arrives is fed on the
	// hub's authorize tick, a second the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		plane, bus, graph := planeBusAndGraph(t)
		id := arriveTalking(t, plane, "SIM-REFUSED", connectorapi.Result{OK: true})

		// The operator holds the class and not the dangerous tier, which is the one
		// refusal that happens behind the reach check.
		require.NoError(t, graph.Write(t.Context(),
			tuple(t, authz.TypeDevice, id, authz.RelOperator, operator)))

		_, err := plane.Command(t.Context(), operator, id, httpapi.PrinterCommand{Verb: gcodeVerb})
		require.Error(t, err)
		require.True(t, fault.HasCode(err, authz.CodeAccessDenied()),
			"the dangerous verb was refused with %v", err)

		var carried *fault.Fault
		require.ErrorAs(t, err, &carried)

		commandID := paramOf(t, carried, "command_id")
		require.NotEmpty(t, commandID,
			"a refused command carried no id, so the record it wrote is unaddressable")

		frames := bus.awaitCommands(t, 2)
		indexed := byType(frames)

		require.Len(t, indexed, 2,
			"a refused command published %d kinds of frame and not two", len(indexed))

		requested, published := indexed[event.TypeDeviceCommandRequested]
		require.True(t, published, "a refused command published no instruction")
		require.Equal(t, commandID, requested.CausationID)

		settled, ended := indexed[event.TypeDeviceCommandSettled]
		require.True(t, ended, "a refused command published no outcome")
		require.Equal(t, commandID, settled.CausationID)

		// There is no audit frame, because nothing authorized it. An audit record
		// of an authorization that did not happen would be the one lie in the trail.
		_, authorized := indexed[event.TypeDeviceCommandAuthorized]
		require.False(t, authorized,
			"a command nothing authorized published an authorization")

		var outcome event.CommandSettled
		require.NoError(t, json.Unmarshal(settled.Data, &outcome))

		require.Equal(t, event.CommandUnauthorized, outcome.Status)
		require.Equal(t, gcodeVerb, outcome.Verb)
		require.Equal(t, authz.CodeAccessDenied().String(), outcome.FaultCode)
		require.False(t, outcome.OK)

		t.Logf("observed: a refused %s was recorded under %s as %s",
			gcodeVerb, commandID, outcome.Status)
	})
}

// TestACallerWhoReachesNothingWritesNoFrame is the other half, and it is the
// reason the mint moving does not move the publication with it.
//
// A frame put on a device's origin is a frame in that device's replay window,
// which the hub bounds at 32 events and 32 KiB. A caller who holds nothing on
// the machine being able to write into it would be a way to evict a printer's
// presence frames while holding no grant at all, so the refusal that hides the
// device also publishes nothing.
func TestACallerWhoReachesNothingWritesNoFrame(t *testing.T) {
	t.Parallel()

	plane, bus, _ := planeBusAndGraph(t)
	id := arriveTalking(t, plane, "SIM-STRANGER", connectorapi.Result{OK: true})

	_, err := plane.Command(t.Context(), stranger, id, httpapi.PrinterCommand{Verb: nozzleVerb})
	require.Error(t, err)
	require.True(t, fault.HasCode(err, fault.PrinterNotFound.Code()),
		"a caller holding nothing was told something other than not-found: %v", err)

	require.Empty(t, bus.commands(),
		"a caller who reaches nothing on the device wrote into its replay window")

	t.Logf("observed: a stranger's command published %d frames", len(bus.commands()))
}

// paramOf reads one parameter off a problem document.
func paramOf(t *testing.T, carried *fault.Fault, name string) string {
	t.Helper()

	for _, param := range carried.Params() {
		if param.Key == name {
			return param.Value.String()
		}
	}

	return ""
}
