package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
)

// A refusal the fleet raises for the person a module is serving crosses as the
// sentinel the module branches on: an argument the verb does not take is
// ErrArgument, and a class the person does not hold on a device they can read
// is ErrDenied, the same answer the ceiling gives the module's own work.
func TestTheFleetsRefusalsReachTheModuleAsItsSentinels(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arriveRefusing(t, plane, "SIM-REFUSAL", "NOT_NOW")

	grantOrg(t, graph, authz.RelViewer, "user:watcher")
	grantOrg(t, graph, authz.RelOperator, "user:driver")

	fleet, _ := moduleUnder(t, graph, plane)

	t.Run("an argument the verb does not take", func(t *testing.T) {
		t.Parallel()

		_, err := fleet.Command(t.Context(), deviceapi.CommandParams{
			Principal: "user:driver", ID: id, Verb: nozzleVerb, Args: map[string]string{"nozzle_temperature": "215"},
		})
		require.ErrorIs(t, err, deviceapi.ErrArgument)
	})

	t.Run("a class the person does not hold", func(t *testing.T) {
		t.Parallel()

		_, err := fleet.Command(t.Context(), deviceapi.CommandParams{
			Principal: "user:watcher", ID: id, Verb: nozzleVerb,
		})
		require.ErrorIs(t, err, deviceapi.ErrDenied)
	})

	t.Run("a device the person cannot read", func(t *testing.T) {
		t.Parallel()

		_, err := fleet.Command(t.Context(), deviceapi.CommandParams{
			Principal: "user:stranger", ID: id, Verb: nozzleVerb,
		})
		require.ErrorIs(t, err, deviceapi.ErrNoDevice)
	})

	t.Run("a verb this build does not carry", func(t *testing.T) {
		t.Parallel()

		_, err := fleet.Command(t.Context(), deviceapi.CommandParams{
			Principal: "user:driver", ID: id, Verb: "core.command.make_coffee",
		})
		require.ErrorIs(t, err, deviceapi.ErrVerbUnknown)
	})
}
