package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// staticGraph answers with a visibility somebody wrote out, so the rendering
// can be tested against a known answer rather than against a database.
//
// current is deliberately separate from the epoch inside the answer: Visible
// stamps its answer with the generation it read before the rows, and the
// adapter must carry that stamp rather than take a fresh reading.
type staticGraph struct {
	answer  authz.Visibility
	current uint64
	err     error
	// audited is the device set the audit action answers, empty for a principal
	// who may audit none of them.
	audited []authz.VisibleDevice
	// admits is whether this principal may decide an admission, which is what
	// decides whether the connectors are offered as origins.
	admits bool
}

func (g staticGraph) Visible(context.Context, authz.Subject) (authz.Visibility, error) {
	if g.err != nil {
		return authz.Visibility{}, g.err
	}

	return g.answer, nil
}

func (g staticGraph) Can(
	_ context.Context, _ authz.Subject, action authz.Action, _ authz.Object,
) (bool, error) {
	if g.err != nil {
		return false, g.err
	}

	// Only the admission question is answered yes: the dashboard origins ask
	// the same method, and this graph grants no sight of a dashboard.
	return g.admits && action == authz.ActionAdmitDevice, nil
}

func (g staticGraph) VisibleDevicesFor(
	context.Context, authz.Subject, authz.Action,
) ([]authz.VisibleDevice, error) {
	if g.err != nil {
		return nil, g.err
	}

	return g.audited, nil
}

func (g staticGraph) Epoch() uint64 { return g.current }

func instanceRef(t *testing.T, kind ref.Kind, id string) ref.Ref {
	t.Helper()

	instance, err := ref.New(kind, id)
	require.NoError(t, err)

	return instance
}

// TestVisibilityRendersTheGraphAnswer is the whole contract of the adapter: a
// printer becomes a printer origin, an instance becomes an origin of its own
// kind, each carries the generation the graph recorded for it, and nothing else
// happens.
func TestVisibilityRendersTheGraphAnswer(t *testing.T) {
	t.Parallel()

	answer := authz.Visibility{
		Epoch: 41,
		Devices: []authz.VisibleDevice{
			{ID: "mk4-017", Since: 12},
			{ID: "core-one-2", Since: 40},
		},
		Resources: []authz.VisibleResource{
			{Ref: instanceRef(t, ref.KindStorage, "local"), Since: 3},
			{Ref: instanceRef(t, ref.KindCamera, "door"), Since: 41},
		},
	}

	rendered, err := visibility{graph: staticGraph{answer: answer, current: 41}}.
		VisibleOrigins(t.Context(), "user:operator")
	require.NoError(t, err)

	require.Equal(t, []string{"device:mk4-017", "device:core-one-2", "storage:local", "camera:door"},
		originNames(rendered.Origins))
	require.Equal(t, uint64(41), rendered.Epoch)

	// The generation belongs to the origin and not to the answer. Flattening the
	// four onto the answer's own 41 is the loss this carries them for: every
	// origin granted before the graph last moved would be replayed from nothing.
	require.Equal(t, []uint64{12, 40, 3, 41}, sinceOf(rendered.Origins))
}

// sinceOf is the generation each rendered origin carries.
func sinceOf(origins []stream.Grant) []uint64 {
	out := make([]uint64, 0, len(origins))
	for _, grant := range origins {
		out = append(out, grant.Since)
	}

	return out
}

// TestTheAdapterDoesNotComputeTheEpoch is the correctness of E10's revocation
// half. Visible reads the generation before the rows on purpose: an answer
// stamped older than the graph costs one re-authorization, and one stamped
// newer than the rows it carries describes a graph that has already moved, so
// the revocation that moved it is never noticed.
func TestTheAdapterDoesNotComputeTheEpoch(t *testing.T) {
	t.Parallel()

	// A grant changed between the stamp Visible took and the moment the adapter
	// was asked, which is exactly the race the ordering exists for.
	graph := staticGraph{answer: authz.Visibility{Epoch: 41}, current: 99}

	rendered, err := visibility{graph: graph}.VisibleOrigins(t.Context(), "user:operator")
	require.NoError(t, err)

	require.Equal(t, uint64(41), rendered.Epoch,
		"the stamp is the graph's, taken before the rows; re-reading it here loses the revocation")
	require.Equal(t, uint64(99), visibility{graph: graph}.Epoch(),
		"the per-tick reading is the current generation, which is what makes the two comparable")
}

// TestAPrincipalTheGraphCannotParseIsNotASubscription keeps a malformed
// principal from reaching the graph as something it resembles.
func TestAPrincipalTheGraphCannotParseIsNotASubscription(t *testing.T) {
	t.Parallel()

	_, err := visibility{graph: staticGraph{}}.VisibleOrigins(t.Context(), "operator")
	require.ErrorContains(t, err, "read the subscriber's principal")
}

// TestAGraphThatCannotAnswerIsNotAGrant is fail-closed at the subscription: a
// store that will not answer refuses the reader rather than attaching them to
// nothing and letting a later grant widen it.
func TestAGraphThatCannotAnswerIsNotAGrant(t *testing.T) {
	t.Parallel()

	_, err := visibility{graph: staticGraph{err: errors.New("the graph is unreachable")}}.
		VisibleOrigins(t.Context(), "user:operator")
	require.ErrorContains(t, err, "the graph is unreachable")
}

// TestAPrincipalWhoMayObserveNothingIsAttachedToNothing is the other half of
// fail-closed, and the half that is easy to get backwards. A graph that grants
// this principal nothing is not a graph that attaches them to everything: the
// stream opens and carries no origin at all, and widens only when the graph
// says something else.
func TestAPrincipalWhoMayObserveNothingIsAttachedToNothing(t *testing.T) {
	t.Parallel()

	hub, err := stream.New(stream.Options{Site: "local", Authorizer: visibility{graph: staticGraph{}}})
	require.NoError(t, err)

	defer hub.Close()

	sub, err := hub.Subscribe(t.Context(), stream.SubscribeRequest{Principal: "user:operator"})
	require.NoError(t, err)

	defer sub.Close()

	require.Empty(t, sub.Origins(), "a principal the graph grants nothing was attached to an origin")
}

// TestAnInstanceTransitionReachesASubscriber is an earlier design decision end to end over the
// real composition: the real supervision plane, the real status hook, the real
// hub and a real subscription. It is the only surface an instance's lifecycle
// has, so a plane that publishes nothing to it leaves the decision unmet with
// every part in place.
func TestAnInstanceTransitionReachesASubscriber(t *testing.T) {
	t.Parallel()

	// In a bubble: a reader attached before the printer arrives is fed on the
	// hub's authorize tick, a second the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		local := instanceRef(t, ref.KindStorage, "local")

		origin, err := event.NewOrigin(event.Kind(local.Kind()), local.ID())
		require.NoError(t, err)

		hub, err := stream.New(stream.Options{
			Site:     "local",
			Interval: stream.MinInterval,
			Authorizer: visibility{graph: staticGraph{
				answer:  authz.Visibility{Epoch: 1, Resources: []authz.VisibleResource{{Ref: local, Since: 1}}},
				current: 1,
			}},
		})
		require.NoError(t, err)

		defer hub.Close()

		// Subscribing first is what makes this a live read rather than a replay: a
		// fresh reader starts at each origin's head.
		sub, err := hub.Subscribe(t.Context(), stream.SubscribeRequest{Principal: "user:operator"})
		require.NoError(t, err)

		defer sub.Close()

		require.Equal(t, []event.Origin{origin}, sub.Origins())

		supervision := startTestPlane(t, hub)

		write(t, filepath.Join(dir, "config.yaml"), lines("resources:", "  storage:", "    local:",
			"      type: filesystem",
			"      enabled: true",
			"      config:",
			"        root: "+slashed(filepath.Join(dir, "storage"))))

		store, err := resource.New(resource.Config{Paths: resource.Paths{
			File:    filepath.Join(dir, "config.yaml"),
			Overlay: filepath.Join(dir, "config.d", "resources.json"),
		}, Plane: supervision.host, Graph: newGraph(t)})
		require.NoError(t, err)

		require.NoError(t, store.Reload(t.Context()))

		states := readTransitions(t, sub, origin)

		require.Contains(t, states, "ready",
			"the instance reached ready and no subscriber was told: %v", states)
	})
}

// readTransitions drains batches until the instance reports ready or the wait
// runs out, and returns every state it saw on the way.
func readTransitions(t *testing.T, sub *stream.Subscription, origin event.Origin) []string {
	t.Helper()

	deadline := time.After(settle)

	var states []string

	for {
		select {
		case batch, open := <-sub.Batches():
			require.True(t, open, "the subscription closed before the instance reached ready")

			for _, frame := range batch.Events {
				require.Equal(t, event.TypeResourceStateChanged, frame.Type)
				require.Equal(t, origin, frame.Position.Origin,
					"an event arrived on an origin this subscription did not ask for")

				states = append(states, stateOfFrame(t, frame))
			}

			if len(states) > 0 && states[len(states)-1] == "ready" {
				return states
			}
		case <-deadline:
			t.Fatalf("no ready transition arrived; saw %v", states)

			return states
		}
	}
}

// stateOfFrame reads the transition out of one encoded envelope, the way a
// browser reading the stream would.
func stateOfFrame(t *testing.T, frame stream.Frame) string {
	t.Helper()

	var envelope struct {
		Type string     `json:"type"`
		Data transition `json:"data"`
	}

	require.NoError(t, json.Unmarshal(frame.Bytes, &envelope))
	require.Equal(t, event.TypeResourceStateChanged, envelope.Type)

	return envelope.Data.State
}

// startTestPlane runs the real supervision plane over the real type registry,
// with the real status hook attached to hub.
func startTestPlane(t *testing.T, hub *stream.Hub) *plane {
	t.Helper()

	supervision, err := startPlane(
		context.WithoutCancel(t.Context()),
		discard(),
		hub,
		nil,
		t.TempDir(),
		false,
		false,
		install.SwitchOff{},
		nil,
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, supervision.shutdown(context.WithoutCancel(t.Context()), discard(), settle))
	})

	return supervision
}

func originNames(origins []stream.Grant) []string {
	names := make([]string, 0, len(origins))
	for _, grant := range origins {
		names = append(names, grant.Origin.String())
	}

	return names
}
