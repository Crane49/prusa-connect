package app

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
)

// The link instance and the server above it, as the graph keys them.
const (
	linkModule    = "fractal"
	linkPrincipal = "sink:" + linkModule
	nodeAbove     = "node:hq"
)

// linkUnder is a link's fleet view over one plane. It is moduleUnder for an
// instance of kind link, which is the one kind that may name a node.
func linkUnder(t *testing.T, graph *authz.Store, plane *devices, ceiling ...authz.Action) moduleFleet {
	t.Helper()

	instance, err := ref.New(ref.KindLink, linkModule)
	require.NoError(t, err)

	fleets := &moduleFleets{fleet: plane, grants: graph}

	fleet, ok := fleets.forModule(
		resourcehost.Module{Instance: instance, Permissions: ceiling},
	).(moduleFleet)
	require.True(t, ok)

	return fleet
}

// A method call from the server above becomes a command made as node:hq, and
// an administrator who has written that node no grant gets the refusal any
// caller without one gets. This is the whole of what enrolment in the explicit
// mode buys: the server above joins and reaches nothing.
func TestAServerAboveThisOneReachesNothingUntilItIsGranted(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arriveRefusing(t, plane, "SIM-NODE-UNGRANTED", "NOT_NOW")

	// The instance itself is granted on the device, so what refuses the call is
	// the node's own standing and never the link's.
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOperator, linkPrincipal)))

	fleet := linkUnder(t, graph, plane, authz.ActionObserve, authz.ActionOperate)

	// A node that cannot see the printer is told what it is told for an id this
	// server does not have: the class of the verb cannot be resolved without
	// reading the device, so neither answer carries a fact about it.
	_, err := fleet.Command(t.Context(), deviceapi.CommandParams{
		Principal: nodeAbove, ID: id, Verb: nozzleVerb,
	})
	require.ErrorIs(t, err, deviceapi.ErrDenied)

	_, err = fleet.Command(t.Context(), deviceapi.CommandParams{
		Principal: nodeAbove, ID: "00000000-0000-4000-8000-000000000000", Verb: nozzleVerb,
	})
	require.ErrorIs(t, err, deviceapi.ErrDenied)

	_, err = fleet.Get(t.Context(), deviceapi.GetParams{Principal: nodeAbove, ID: id})
	require.ErrorIs(t, err, deviceapi.ErrNoDevice)

	page, err := fleet.List(t.Context(), deviceapi.ListParams{Principal: nodeAbove})
	require.NoError(t, err)
	require.Empty(t, page.Items)
}

// Granted operator on one printer, the server above drives that printer and no
// other. The grant is written on the device, so the second printer is the
// refusal direction of the same rule.
func TestAGrantedServerAboveDrivesOnlyThePrinterItWasGranted(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	granted := arriveRefusing(t, plane, "SIM-NODE-GRANTED", "NOT_NOW")
	withheld := arriveRefusing(t, plane, "SIM-NODE-WITHHELD", "NOT_NOW")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, granted, authz.RelOperator, nodeAbove)))

	fleet := linkUnder(t, graph, plane, authz.ActionObserve, authz.ActionOperate)

	result, err := fleet.Command(t.Context(), deviceapi.CommandParams{
		Principal: nodeAbove, ID: granted, Verb: nozzleVerb,
	})
	require.NoError(t, err, "the command did not reach the machine")
	require.False(t, result.OK)
	require.Equal(t, "NOT_NOW", result.MachineReason)

	_, err = fleet.Command(t.Context(), deviceapi.CommandParams{
		Principal: nodeAbove, ID: withheld, Verb: nozzleVerb,
	})
	require.ErrorIs(t, err, deviceapi.ErrDenied)

	page, err := fleet.List(t.Context(), deviceapi.ListParams{Principal: nodeAbove})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, granted, page.Items[0].ID)
}

// A server above this one that was given sight of a printer and nothing more
// is refused the verb, which is the other refusal shape: it may see the
// machine, so the answer says it holds no permission rather than hiding the
// printer.
func TestAServerAboveThisOneWithSightAloneMayNotDriveThePrinter(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arriveRefusing(t, plane, "SIM-NODE-VIEWER", "NOT_NOW")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelViewer, nodeAbove)))

	fleet := linkUnder(t, graph, plane, authz.ActionObserve, authz.ActionOperate)

	device, err := fleet.Get(t.Context(), deviceapi.GetParams{Principal: nodeAbove, ID: id})
	require.NoError(t, err)
	require.Equal(t, id, device.ID)

	_, err = fleet.Command(t.Context(), deviceapi.CommandParams{
		Principal: nodeAbove, ID: id, Verb: nozzleVerb,
	})
	require.ErrorIs(t, err, deviceapi.ErrDenied)
}

// The ceiling an operator writes at install bounds what a link relays for the
// server above it, and not only what the link does for itself. A link
// installed with observe alone reads printers for that server and drives none,
// whatever the graph grants the server.
func TestALinkRelaysOnlyWhatItsCeilingLists(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arriveRefusing(t, plane, "SIM-NODE-CEILING", "NOT_NOW")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOperator, nodeAbove)))

	t.Run("observe alone", func(t *testing.T) {
		t.Parallel()

		fleet := linkUnder(t, graph, plane, authz.ActionObserve)

		device, err := fleet.Get(t.Context(), deviceapi.GetParams{Principal: nodeAbove, ID: id})
		require.NoError(t, err, "the ceiling lists observe, so the reading goes up")
		require.Equal(t, id, device.ID)

		_, err = fleet.Command(t.Context(), deviceapi.CommandParams{
			Principal: nodeAbove, ID: id, Verb: nozzleVerb,
		})
		require.ErrorIs(t, err, deviceapi.ErrDenied)
	})

	t.Run("nothing at all", func(t *testing.T) {
		t.Parallel()

		fleet := linkUnder(t, graph, plane)

		_, err := fleet.Get(t.Context(), deviceapi.GetParams{Principal: nodeAbove, ID: id})
		require.ErrorIs(t, err, deviceapi.ErrDenied)
	})
}

// linkFeedOn is a link's subscription door over one hub.
func linkFeedOn(t *testing.T, hub *stream.Hub) busapi.Feed {
	t.Helper()

	instance, err := ref.New(ref.KindLink, linkModule)
	require.NoError(t, err)

	return (&moduleFeeds{hub: hub}).forModule(resourcehost.Module{
		Instance:    instance,
		Permissions: []authz.Action{authz.ActionObserve},
	})
}

// openSubscriptions starts one subscription per principal and waits for the hub
// to hold them all. Each one runs until ctx ends, which is what a subscription
// the hub accepted does.
func openSubscriptions(
	ctx context.Context, t *testing.T, hub *stream.Hub, feed busapi.Feed, principals ...string,
) {
	t.Helper()

	for _, principal := range principals {
		go func() {
			_ = feed.Subscribe(ctx, busapi.SubscribeParams{Principal: principal},
				func(busapi.Window) error { return nil })
		}()
	}

	require.Eventually(t, func() bool { return hub.Subscribers() == len(principals) },
		time.Second, time.Millisecond, "the hub did not take every subscription")
}

// A link's subscriptions are counted against the link as well as against the
// server each one names. The hub bounds what one principal holds, and the
// servers a link names come from the link's own reading, so counting only the
// name in the request would let one module hold that bound once per server it
// invents.
func TestALinkHoldsNoMoreSubscriptionsThanOnePrincipalMay(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)

	hub, err := stream.New(stream.Options{
		Site: "local", Authorizer: visibility{graph: graph}, MaxPerPrincipal: 2,
	})
	require.NoError(t, err)

	defer hub.Close()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	feed := linkFeedOn(t, hub)

	openSubscriptions(ctx, t, hub, feed, "node:one", "node:two")

	// Bounded, because a subscription the hub took runs until its context ends
	// and this one is expected to be refused before it starts.
	third, stop := context.WithTimeout(ctx, time.Second)
	defer stop()

	err = feed.Subscribe(third, busapi.SubscribeParams{Principal: "node:three"},
		func(busapi.Window) error { return nil })

	require.ErrorIs(t, err, busapi.ErrInvalidSubscription)
	require.ErrorIs(t, err, stream.ErrTooManySubscriptions)
}

// A subscription a module opens for a person is counted against that person
// alone. The host opened the request and ends it, so a module serving many
// people is not a module holding subscriptions of its own.
func TestAModuleServingPeopleIsNotCountedAgainstItself(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)

	hub, err := stream.New(stream.Options{
		Site: "local", Authorizer: visibility{graph: graph}, MaxPerPrincipal: 2,
	})
	require.NoError(t, err)

	defer hub.Close()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	feed := linkFeedOn(t, hub)

	openSubscriptions(ctx, t, hub, feed, "user:anna", "user:bob", "user:cleo")
}

// Two supervised kinds under one name are one principal in the authorization
// graph, because the principal is built from the name alone. That is the
// reason the supervision plane refuses to run both: Host.claimName in
// internal/app/resourcehost holds one name across the kinds that read the
// graph, so the pair below never runs on one server.
func TestTwoKindsUnderOneNameWouldBeOnePrincipal(t *testing.T) {
	t.Parallel()

	sink, err := ref.New(ref.KindSink, "audit")
	require.NoError(t, err)

	link, err := ref.New(ref.KindLink, "audit")
	require.NoError(t, err)

	require.Equal(t, sinkPrincipal(sink), sinkPrincipal(link))
}
