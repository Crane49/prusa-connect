package app

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
)

// The instance every case here runs as, spelled as the graph keys it.
const (
	ceilingModule    = "planner"
	ceilingPrincipal = "sink:" + ceilingModule
)

// moduleUnder is a module's fleet view and feed over one plane, with the
// ceiling the case writes and the plane's graph deciding the rest.
func moduleUnder(t *testing.T, graph *authz.Store, plane *devices, ceiling ...authz.Action) (moduleFleet, moduleFeed) {
	t.Helper()

	instance, err := ref.New(ref.KindSink, ceilingModule)
	require.NoError(t, err)

	module := resourcehost.Module{Instance: instance, Permissions: ceiling}

	fleets := &moduleFleets{fleet: plane, grants: graph}
	feeds := &moduleFeeds{hub: plane.hub}

	fleet, ok := fleets.forModule(module).(moduleFleet)
	require.True(t, ok)

	feed, ok := feeds.forModule(module).(moduleFeed)
	require.True(t, ok)

	return fleet, feed
}

// grantOrg writes one relation on the org for one subject, which is what an
// operator does to let an instance at the fleet.
func grantOrg(t *testing.T, graph *authz.Store, relation authz.Relation, subject string) {
	t.Helper()

	require.NoError(t, graph.Write(t.Context(), tuple(t, authz.TypeOrg, bootstrapOrg, relation, subject)))
}

// A module listing the fleet as itself reads what the operator granted the
// instance, once the operator has also listed observe for its own work.
func TestAModulesOwnListingIsInsideItsCeilingAndItsGrants(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arrive(t, plane, "SIM-CEILING-LIST")

	grantOrg(t, graph, authz.RelViewer, ceilingPrincipal)

	fleet, _ := moduleUnder(t, graph, plane, authz.ActionObserve)

	page, err := fleet.List(t.Context(), deviceapi.ListParams{})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, id, page.Items[0].ID)

	device, err := fleet.Get(t.Context(), deviceapi.GetParams{ID: id})
	require.NoError(t, err)
	require.Equal(t, id, device.ID)
}

// A module whose operator listed nothing for its own work reads nothing as
// itself, whatever the instance was granted. The ceiling is not a grant, and a
// grant is not a ceiling: the two halves each refuse on their own.
func TestAModulesOwnListingOutsideItsCeilingIsRefused(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	arrive(t, plane, "SIM-CEILING-NONE")

	grantOrg(t, graph, authz.RelViewer, ceilingPrincipal)

	t.Run("nothing declared", func(t *testing.T) {
		t.Parallel()

		fleet, _ := moduleUnder(t, graph, plane)

		_, err := fleet.List(t.Context(), deviceapi.ListParams{})
		require.ErrorIs(t, err, deviceapi.ErrDenied)

		_, err = fleet.Get(t.Context(), deviceapi.GetParams{ID: "p1"})
		require.ErrorIs(t, err, deviceapi.ErrDenied)
	})

	t.Run("operate declared and not observe", func(t *testing.T) {
		t.Parallel()

		fleet, _ := moduleUnder(t, graph, plane, authz.ActionOperate)

		_, err := fleet.List(t.Context(), deviceapi.ListParams{})
		require.ErrorIs(t, err, deviceapi.ErrDenied)
	})
}

// The other half. A module that declared observe and was granted nothing lists
// nothing, because the graph answers for the instance and it holds nothing.
func TestAModulesOwnListingInsideItsCeilingAndGrantedNothingIsEmpty(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	arrive(t, plane, "SIM-CEILING-UNGRANTED")

	fleet, _ := moduleUnder(t, graph, plane, authz.ActionObserve)

	page, err := fleet.List(t.Context(), deviceapi.ListParams{})
	require.NoError(t, err)
	require.Empty(t, page.Items)
	require.Zero(t, page.Total)
}

// A command the module sends as itself is held to the class the verb carries.
// An operator who listed operate and granted operator on the device lets the
// verb reach the machine; one who listed observe alone does not, whatever the
// grant says.
func TestAModulesOwnCommandIsHeldToTheVerbsClass(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arriveRefusing(t, plane, "SIM-CEILING-COMMAND", "NOT_NOW")

	// On the device and not the org: the vocabulary lets a sink watch the
	// whole fleet and drive only what it was named on.
	require.NoError(t, graph.Write(t.Context(), tuple(t, authz.TypeDevice, id, authz.RelOperator, ceilingPrincipal)))

	command := deviceapi.CommandParams{ID: id, Verb: nozzleVerb}

	t.Run("operate declared", func(t *testing.T) {
		t.Parallel()

		fleet, _ := moduleUnder(t, graph, plane, authz.ActionObserve, authz.ActionOperate)

		result, err := fleet.Command(t.Context(), command)
		require.NoError(t, err, "the command did not reach the machine")
		require.False(t, result.OK)
		require.Equal(t, "NOT_NOW", result.MachineReason, "the machine's own answer did not come back")
	})

	t.Run("observe alone", func(t *testing.T) {
		t.Parallel()

		fleet, _ := moduleUnder(t, graph, plane, authz.ActionObserve)

		_, err := fleet.Command(t.Context(), command)
		require.ErrorIs(t, err, deviceapi.ErrDenied)
	})
}

// A verb nothing in this build declares carries no class, and no ceiling holds
// the empty class, so a command the module sends as itself is refused here and
// the fleet never sees it. The module holds every action an operator can list
// and the operator relation on the device, so what refuses it is the verb.
func TestAModulesOwnCommandOfAVerbNothingDeclaresIsRefused(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arriveRefusing(t, plane, "SIM-CEILING-UNKNOWN", "NOT_NOW")

	require.NoError(t, graph.Write(t.Context(), tuple(t, authz.TypeDevice, id, authz.RelOperator, ceilingPrincipal)))

	fleet, _ := moduleUnder(t, graph, plane, authz.ActionObserve, authz.ActionOperate)

	_, err := fleet.Command(t.Context(), deviceapi.CommandParams{
		ID:   id,
		Verb: "core.command.nothing_declares_this",
	})
	require.ErrorIs(t, err, deviceapi.ErrDenied)
}

// A subscription the module opens as itself takes observe in its ceiling, and
// then observes what the instance was granted.
func TestAModulesOwnSubscriptionIsInsideItsCeiling(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	grantOrg(t, graph, authz.RelViewer, ceilingPrincipal)

	t.Run("observe declared", func(t *testing.T) {
		t.Parallel()

		_, feed := moduleUnder(t, graph, plane, authz.ActionObserve)

		ctx, cancel := context.WithCancel(t.Context())
		opened := make(chan error, 1)

		go func() {
			opened <- feed.Subscribe(ctx, busapi.SubscribeParams{}, func(busapi.Window) error { return nil })
		}()

		// The subscription is open: it is a hub subscriber now, and the case
		// ends it rather than the host refusing it.
		require.Eventually(t, func() bool { return plane.hub.Subscribers() > 0 }, settle, 5*time.Millisecond,
			"the subscription never reached the hub")

		cancel()
		require.NoError(t, <-opened)
	})

	t.Run("nothing declared", func(t *testing.T) {
		t.Parallel()

		_, feed := moduleUnder(t, graph, plane)

		err := feed.Subscribe(t.Context(), busapi.SubscribeParams{}, func(busapi.Window) error {
			t.Error("a window arrived on a subscription outside the ceiling")

			return nil
		})
		require.ErrorIs(t, err, busapi.ErrDenied)
	})
}

// A call for a person the module is serving is that person's and is not held
// to the ceiling. The instance's own list may be empty and the person still
// reads what they hold.
func TestACallForAPersonIsNotHeldToTheModulesCeiling(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arrive(t, plane, "SIM-CEILING-PERSON")

	grantOrg(t, graph, authz.RelViewer, "user:alice")

	fleet, _ := moduleUnder(t, graph, plane)

	page, err := fleet.List(t.Context(), deviceapi.ListParams{Principal: "user:alice"})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, id, page.Items[0].ID)
}
