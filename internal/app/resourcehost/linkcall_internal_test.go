package resourcehost

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/extension/bus"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/linkapi"
)

// This file is in the package because what it holds is a property of the door
// and not of a deployment: the module states the fact and the host states the
// event type. Nothing a module sends reaches the type.

// testLinkChanges is the door over a link that is serving, so a case reaches
// the property without a supervision plane and a child process.
func testLinkChanges(t *testing.T, emitter bus.Emitter) *linkChanges {
	t.Helper()

	instanceRef, err := ref.New(ref.KindLink, "fractal")
	require.NoError(t, err)

	host := &Host{}

	return &linkChanges{
		emitter:  emitter,
		instance: instanceRef,
		serving:  func() string { return "" },
		claim: func(ctx context.Context, status linkapi.Status) error {
			return host.claimNodes(ctx, instanceRef, status)
		},
	}
}

// TestTheHostNamesTheEventALinkModuleAnnounces: the module sends a reading and
// the host publishes it under this project's own type, keyed by the instance.
func TestTheHostNamesTheEventALinkModuleAnnounces(t *testing.T) {
	t.Parallel()

	hub := &heldEmitter{entered: make(chan struct{}, 1)}
	door := testLinkChanges(t, hub)

	reading := linkapi.Status{
		NodeID:    "b856eab039d11de1",
		Accepting: true,
		Uplink:    &linkapi.Uplink{State: linkapi.UplinkLost, Reason: "the connection closed"},
	}

	require.NoError(t, door.Changed(t.Context(), linkapi.ChangedParams{Status: reading}))

	emissions := hub.emissions()
	require.Len(t, emissions, 1)
	require.Equal(t, event.TypeLinkStateChanged, emissions[0].Type)
	require.Equal(t, "link:fractal", emissions[0].Key)

	var body linkapi.Status
	require.NoError(t, json.Unmarshal([]byte(emissions[0].Body), &body))
	require.Equal(t, linkapi.NodeID("b856eab039d11de1"), body.NodeID)
	require.NotNil(t, body.Uplink)
	require.Equal(t, linkapi.UplinkLost, body.Uplink.State)
}

// TestALinkThatIsNotServingAnnouncesNothing: the readiness check is asked per
// call, so a module reporting through the previous life's state is refused.
func TestALinkThatIsNotServingAnnouncesNothing(t *testing.T) {
	t.Parallel()

	hub := &heldEmitter{entered: make(chan struct{}, 1)}
	door := testLinkChanges(t, hub)
	door.serving = func() string { return "link:fractal is starting" }

	err := door.Changed(t.Context(), linkapi.ChangedParams{Status: linkapi.Status{}})
	require.ErrorIs(t, err, linkapi.ErrNotReady)
	require.Empty(t, hub.emissions())
}

// TestAReadingRecordsTheServersTheLinkAnswersFor: the reading is what the host
// later holds a node principal against, so it is recorded before the event is
// published.
func TestAReadingRecordsTheServersTheLinkAnswersFor(t *testing.T) {
	t.Parallel()

	host := &Host{}
	linkInstance, err := ref.New(ref.KindLink, "fractal")
	require.NoError(t, err)

	hub := &heldEmitter{entered: make(chan struct{}, 1)}
	door := &linkChanges{
		emitter:  hub,
		instance: linkInstance,
		serving:  func() string { return "" },
		claim: func(ctx context.Context, status linkapi.Status) error {
			return host.claimNodes(ctx, linkInstance, status)
		},
	}

	reading := linkapi.Status{
		NodeID:   "floor",
		NodePath: "hq/floor",
		Nodes:    []linkapi.Node{{ID: "cell"}},
	}

	require.NoError(t, door.Changed(t.Context(), linkapi.ChangedParams{Status: reading}))
	require.True(t, host.flights.nodes.holds(linkInstance, "hq"))
	require.True(t, host.flights.nodes.holds(linkInstance, "cell"))
	require.False(t, host.flights.nodes.holds(linkInstance, "floor"))
	require.Len(t, hub.emissions(), 1)
}

// TestAReadingNamingAServerAnotherLinkAnswersForIsRefused, whole and with
// nothing published, so a second link module cannot take the calls written for
// the first by reporting its id.
func TestAReadingNamingAServerAnotherLinkAnswersForIsRefused(t *testing.T) {
	t.Parallel()

	first, err := ref.New(ref.KindLink, "fractal")
	require.NoError(t, err)

	second, err := ref.New(ref.KindLink, "other")
	require.NoError(t, err)

	// The first link is supervised, which is what makes the id it reported one
	// the host is still holding for it.
	host := &Host{running: map[ref.Ref]*instance{first: {}}}
	require.Empty(t, host.flights.nodes.report(first, []string{"hq"}, host.supervised()))

	hub := &heldEmitter{entered: make(chan struct{}, 1)}
	door := &linkChanges{
		emitter:  hub,
		instance: second,
		serving:  func() string { return "" },
		claim: func(ctx context.Context, status linkapi.Status) error {
			return host.claimNodes(ctx, second, status)
		},
	}

	reading := linkapi.Status{NodeID: "floor", NodePath: "hq/floor"}

	err = door.Changed(t.Context(), linkapi.ChangedParams{Status: reading})
	require.ErrorIs(t, err, linkapi.ErrInvalidReading)
	require.Empty(t, hub.emissions(), "a refused reading was published")
	require.False(t, host.flights.nodes.holds(second, "hq"))
	require.True(t, host.flights.nodes.holds(first, "hq"))
}

// TestARefusedReadingFailsTheLinkThatSentIt: an operator reads the conflict on
// the instance's own status, because a module the host cannot believe about
// which servers it carries must not go on relaying calls for them.
func TestARefusedReadingFailsTheLinkThatSentIt(t *testing.T) {
	t.Parallel()

	first, err := ref.New(ref.KindLink, "fractal")
	require.NoError(t, err)

	second, err := ref.New(ref.KindLink, "other")
	require.NoError(t, err)

	runner, err := supervisor.New(supervisor.Config[any]{
		Ref:    second,
		Policy: supervisor.DefaultPolicy(),
		Start:  func(context.Context, supervisor.Handle) (any, error) { return struct{}{}, nil },
	})
	require.NoError(t, err)

	host := &Host{running: map[ref.Ref]*instance{
		first:  {},
		second: {runner: runner},
	}}

	require.Empty(t, host.flights.nodes.report(first, []string{"hq"}, host.supervised()))

	err = host.claimNodes(t.Context(), second, linkapi.Status{NodePath: "hq/floor"})
	require.ErrorIs(t, err, linkapi.ErrInvalidReading)

	var started condition.Condition

	for _, held := range runner.Status().Conditions {
		if held.Type == condition.TypeStarted {
			started = held
		}
	}

	require.Equal(t, condition.StatusFalse, started.Status)
	require.Contains(t, started.Message, "another link already answers for")
}

// TestADeploymentWithNoEventStreamInstallsNoLinkDoor, so a module reporting a
// change reads CodeMethodNotFound and carries on.
func TestADeploymentWithNoEventStreamInstallsNoLinkDoor(t *testing.T) {
	t.Parallel()

	instance, err := ref.New(ref.KindLink, "fractal")
	require.NoError(t, err)

	require.Nil(t, newLinkChanges(nil, instance, nil))
	require.Empty(t, linkapi.ChangedHandlers(newLinkChanges(nil, instance, nil)))
}
