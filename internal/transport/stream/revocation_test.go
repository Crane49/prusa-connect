package stream

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/event"
)

// revocableAuthorizer is an authorization graph that moves. Every change bumps
// the epoch, which is the only thing a live subscription reads on its tick.
//
// It records when each sight began, as the projection does: an origin already in
// a principal's set keeps the generation it entered at, and one entering now
// takes the generation this write produces. A set the origin left and re-entered
// begins again, because the grant behind it is a new one.
type revocableAuthorizer struct {
	mu      sync.Mutex
	visible map[string][]event.Origin
	since   map[seen]uint64
	err     error
	epoch   atomic.Uint64
	asked   atomic.Int64
}

// seen is one row of the projection: who, and what they may observe.
type seen struct {
	principal string
	origin    event.Origin
}

func newRevocableAuthorizer(visible map[string][]event.Origin) *revocableAuthorizer {
	a := &revocableAuthorizer{visible: visible, since: map[seen]uint64{}}
	a.epoch.Store(1)

	for principal, origins := range visible {
		for _, origin := range origins {
			a.since[seen{principal: principal, origin: origin}] = 1
		}
	}

	return a
}

func (a *revocableAuthorizer) VisibleOrigins(_ context.Context, principal string) (Visibility, error) {
	a.asked.Add(1)

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.err != nil {
		return Visibility{}, a.err
	}

	origins := make([]Grant, 0, len(a.visible[principal]))
	for _, origin := range a.visible[principal] {
		origins = append(origins, Grant{
			Origin: origin,
			Since:  a.since[seen{principal: principal, origin: origin}],
		})
	}

	return Visibility{Origins: origins, Epoch: a.epoch.Load()}, nil
}

func (a *revocableAuthorizer) Epoch() uint64 { return a.epoch.Load() }

// grant replaces alice's visible set and bumps the graph's generation, which is
// what a policy write does.
func (a *revocableAuthorizer) grant(origins ...event.Origin) {
	a.mu.Lock()

	// The generation this write produces. The store bumps the counter and writes
	// the rows in one transaction, so a sight beginning here begins there.
	next := a.epoch.Load() + 1
	began := make(map[event.Origin]uint64, len(origins))

	for _, origin := range origins {
		if held, kept := a.since[seen{principal: alice, origin: origin}]; kept {
			began[origin] = held

			continue
		}

		began[origin] = next
	}

	for _, origin := range a.visible[alice] {
		delete(a.since, seen{principal: alice, origin: origin})
	}

	for origin, at := range began {
		a.since[seen{principal: alice, origin: origin}] = at
	}

	a.visible[alice] = origins
	a.mu.Unlock()

	a.epoch.Add(1)
}

func (a *revocableAuthorizer) fail(err error) {
	a.mu.Lock()
	a.err = err
	a.mu.Unlock()

	a.epoch.Add(1)
}

// TestARevokedGrantStopsAnOpenStream is an earlier design decision's second half. The channel
// is the decision, and the epoch is what makes a live subscription notice the
// decision changed: alice keeps her stream, loses printerB inside one
// coalescing window, and keeps receiving printerA.
func TestARevokedGrantStopsAnOpenStream(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := newRevocableAuthorizer(map[string][]event.Origin{alice: {printerA, printerB}})
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		require.ElementsMatch(t, []event.Origin{printerA, printerB}, sub.Origins())

		_, err = hub.Publish(stateOf(printerB, 1))
		require.NoError(t, err)

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, sub)
		require.Len(t, batch.Events, 1, "printerB was visible when this was published")

		// The grant on printerB is withdrawn while the stream is open.
		authz.grant(printerA)

		time.Sleep(2 * AuthorizeInterval)
		synctest.Wait()

		require.Equal(t, []event.Origin{printerA}, sub.Origins(),
			"the withdrawn printer is still routed to an open subscription")
		require.Equal(t, authz.Epoch(), sub.ACLEpoch(), "the subscription is at the current generation")

		batch = receive(t, sub)
		require.Equal(t, []event.Origin{printerB}, batch.Withdrawn, "alice was not told printerB left her sight")
		require.Empty(t, batch.Events)

		for _, pub := range []Publication{stateOf(printerB, 2), telemetryOf(printerB, "nozzle", 99)} {
			_, err = hub.Publish(pub)
			require.NoError(t, err)
		}

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		require.Empty(t, sub.Batches(), "a printer alice lost kept reaching her open stream")

		// What she still holds keeps working.
		_, err = hub.Publish(stateOf(printerA, 7))
		require.NoError(t, err)

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch = receive(t, sub)
		require.Len(t, batch.Events, 1)
		require.Equal(t, printerA, batch.Events[0].Position.Origin)
	})
}

// TestARevocationSweepsWhatIsAlreadyQueued is the case Mosquitto is the only
// surveyed broker to handle, and conflation is why it matters more here: the
// telemetry lane holds the newest value per key with no expiry, so a value taken
// a moment before the grant was withdrawn would sit in it until delivered.
func TestARevocationSweepsWhatIsAlreadyQueued(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := newRevocableAuthorizer(map[string][]event.Origin{alice: {printerA, printerB}})
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			// Longer than the test, so nothing drains on its own and the queues
			// still hold everything when the grant is withdrawn.
			Interval: time.Hour,
		})
		require.NoError(t, err)

		defer sub.Close()

		for _, pub := range []Publication{
			telemetryOf(printerA, "nozzle", 215),
			telemetryOf(printerB, "nozzle", 216),
			stateOf(printerA, 1),
			stateOf(printerB, 2),
		} {
			_, err = hub.Publish(pub)
			require.NoError(t, err)
		}

		synctest.Wait()

		stats := sub.Stats()
		require.Equal(t, 2, stats.TelemetryKeys)
		require.Equal(t, 2, stats.EventCount)

		authz.grant(printerA)

		// The pump does the sweep, so drive it the way the pump does.
		require.True(t, sub.reauthorize())

		stats = sub.Stats()
		require.Equal(t, 1, stats.TelemetryKeys, "a conflated value for a withdrawn printer stayed queued")
		require.Equal(t, 1, stats.EventCount, "a queued event for a withdrawn printer stayed queued")

		batch, ready := sub.drain()
		require.True(t, ready)
		require.Len(t, batch.Telemetry, 1)
		require.Equal(t, printerA, batch.Telemetry[0].Position.Origin)
		require.Len(t, batch.Events, 1)
		require.Equal(t, printerA, batch.Events[0].Position.Origin)
		require.Equal(t, []event.Origin{printerB}, batch.Withdrawn, "the swept printer was not named once")

		_, ready = sub.drain()
		require.False(t, ready, "the withdrawn printer was named twice")

		// And the cursor stops naming it: a place in a printer's stream is
		// itself the answer to whether that printer exists.
		generation, held := batch.Cursor.Generation(hub.Epoch())
		require.True(t, held)

		for _, position := range generation.Positions {
			require.NotEqual(t, printerB, position.Origin, "the cursor still names a printer alice lost")
		}
	})
}

// TestAWithdrawnFrameWaitsForAReaderThatAsksForIt keeps the type narrowing
// whole: a reader that named the types it wants is not handed one it did not
// name, including this one.
func TestAWithdrawnFrameWaitsForAReaderThatAsksForIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := newRevocableAuthorizer(map[string][]event.Origin{alice: {printerA, printerB}})
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Types:     []string{"com.prusa.connect.device.state.changed"},
		})
		require.NoError(t, err)

		defer sub.Close()

		authz.grant(printerA)

		time.Sleep(2 * AuthorizeInterval)
		synctest.Wait()

		require.Equal(t, []event.Origin{printerA}, sub.Origins())

		for {
			select {
			case batch := <-sub.Batches():
				require.Empty(t, batch.Withdrawn, "a reader that did not ask for withdrawals was sent one")
			default:
				return
			}
		}
	})
}

// TestANewGrantReachesALiveSubscription is the mirror image: a printer granted
// while the stream is open is routed to it without a reconnect, and from the
// head, because a grant is permission to read from now.
func TestANewGrantReachesALiveSubscription(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := newRevocableAuthorizer(map[string][]event.Origin{alice: {printerA}})
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		// Published before alice may see printerB at all.
		for range 3 {
			_, err := hub.Publish(stateOf(printerB, 1))
			require.NoError(t, err)
		}

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		authz.grant(printerA, printerB)

		time.Sleep(2 * AuthorizeInterval)
		synctest.Wait()

		require.ElementsMatch(t, []event.Origin{printerA, printerB}, sub.Origins())
		require.Empty(t, sub.Batches(), "a new grant replayed events from before it was made")

		_, err = hub.Publish(stateOf(printerB, 4))
		require.NoError(t, err)

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, sub)
		require.Len(t, batch.Events, 1)
		require.Equal(t, uint64(4), batch.Events[0].Position.Offset,
			"printerB's own offsets carry on, and only the new event is delivered")
	})
}

// TestANarrowedSubscriptionIsNotWidenedByANewGrant closes the other direction:
// a reader that asked for one printer does not silently acquire another because
// the graph moved.
func TestANarrowedSubscriptionIsNotWidenedByANewGrant(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := newRevocableAuthorizer(map[string][]event.Origin{alice: {printerA}})
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Origins:   []event.Origin{printerA},
		})
		require.NoError(t, err)

		defer sub.Close()

		authz.grant(printerA, printerB, s3Main)

		time.Sleep(2 * AuthorizeInterval)
		synctest.Wait()

		require.Equal(t, []event.Origin{printerA}, sub.Origins())

		_, err = hub.Publish(stateOf(printerB, 1))
		require.NoError(t, err)

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		require.Empty(t, sub.Batches())
	})
}

// TestReauthorizationDoesNotFailOpen is the rule the whole mechanism turns on.
// An authorization answer the hub could not get is not an answer that says yes,
// so the subscription ends and the reader is told by its channel closing.
//
// An answer that says "nothing" is a different thing: it is an answer, and it
// is held by TestAPrincipalWhoLostEverythingKeepsAnEmptyStream below.
func TestReauthorizationDoesNotFailOpen(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := newRevocableAuthorizer(map[string][]event.Origin{alice: {printerA}})
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		authz.fail(errors.New("policy store unreachable"))

		// Nothing is published, so the idle check is the only thing that can
		// notice.
		time.Sleep(2 * AuthorizeInterval)
		synctest.Wait()

		_, open := <-sub.Batches()
		require.False(t, open, "the reader was left on a stream nobody could authorize")

		_, err = hub.Publish(stateOf(printerA, 1))
		require.NoError(t, err)

		synctest.Wait()
		require.Equal(t, 0, hub.Subscribers(), "the dropped subscription is still registered")
	})
}

// TestAPrincipalWhoLostEverythingKeepsAnEmptyStream is what a withdrawal that
// takes the last grant does, and it is the same answer subscribing with nothing
// gives: an open stream carrying nothing.
//
// Dropping the reader instead would only send them round a reconnect that now
// succeeds and hands back the same empty stream, and it would cost them the
// widening: a grant made a minute later reaches an open stream on its own tick
// and reaches a closed one never.
func TestAPrincipalWhoLostEverythingKeepsAnEmptyStream(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := newRevocableAuthorizer(map[string][]event.Origin{alice: {printerA}})
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		authz.grant()

		time.Sleep(2 * AuthorizeInterval)
		synctest.Wait()

		require.Empty(t, sub.Origins(), "a withdrawn printer is still routed to the stream")
		require.Equal(t, []event.Origin{printerA}, receive(t, sub).Withdrawn)
		require.Equal(t, 1, hub.Subscribers(), "the reader was dropped rather than narrowed")

		_, err = hub.Publish(stateOf(printerA, 1))
		require.NoError(t, err)

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		require.Empty(t, sub.Batches(), "a printer alice no longer holds reached her stream")

		// And the grant coming back widens the stream she never had to reopen.
		authz.grant(printerA)

		time.Sleep(2 * AuthorizeInterval)
		synctest.Wait()

		_, err = hub.Publish(stateOf(printerA, 2))
		require.NoError(t, err)

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, sub)
		require.Len(t, batch.Events, 1, "the regranted printer never reached the stream that stayed open")
	})
}

// TestTheEpochIsReadWithoutAskingThePolicyStore is what makes the tick
// affordable: E10 turns authorization into O(subscribes per hour), and a
// recompute on every window would put it back to O(subscribers times windows).
func TestTheEpochIsReadWithoutAskingThePolicyStore(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := newRevocableAuthorizer(map[string][]event.Origin{alice: {printerA}})
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		asked := authz.asked.Load()
		require.Equal(t, int64(1), asked, "subscribing asks once")

		// A reader that stops collecting parks the pump on its send, and a
		// parked pump reaches no tick at all. This test is about the tick, so
		// the reader keeps up.
		var collected atomic.Int64

		go func() {
			for range sub.Batches() {
				collected.Add(1)
			}
		}()

		for range 20 {
			_, err = hub.Publish(telemetryOf(printerA, "nozzle", 1))
			require.NoError(t, err)

			time.Sleep(DefaultInterval)
		}

		synctest.Wait()
		require.Equal(t, asked, authz.asked.Load(),
			"an unchanged graph was recomputed anyway, once per window per subscriber")

		authz.grant(printerA, printerB)

		time.Sleep(2 * AuthorizeInterval)
		synctest.Wait()

		require.Equal(t, asked+1, authz.asked.Load(), "a moved graph is recomputed exactly once")
		require.Positive(t, collected.Load(), "the reader was collecting, so the pump was never parked")
	})
}

// TestARevocationReclaimsAWindowTheReaderHasNotCollected is the last place a
// withdrawn grant can hide: a batch built and handed to the reader's channel is
// out of both queues, so sweeping them does not reach it.
func TestARevocationReclaimsAWindowTheReaderHasNotCollected(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := newRevocableAuthorizer(map[string][]event.Origin{alice: {printerA, printerB}})
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		for _, pub := range []Publication{stateOf(printerA, 1), stateOf(printerB, 2)} {
			_, err = hub.Publish(pub)
			require.NoError(t, err)
		}

		// The window closes and the batch is parked in the reader's channel,
		// uncollected.
		time.Sleep(2 * DefaultInterval)
		synctest.Wait()
		require.Len(t, sub.Batches(), 1, "the window did not close, so there is nothing parked")

		authz.grant(printerA)

		time.Sleep(2 * AuthorizeInterval)
		synctest.Wait()

		batch := receive(t, sub)
		require.Len(t, batch.Events, 1, "a parked window still carried the printer alice lost")
		require.Equal(t, printerA, batch.Events[0].Position.Origin)
	})
}

// TestAServerScopedEventIsAuthorizedAsItsOwnKind is the failure mode of
// filtering by device id: an event that names no printer passes a predicate
// written as "is this printer visible", and a malformed one becomes visible to
// everybody. Here a node event is an origin like any other, so it is refused
// unless the graph names it, and an event with no origin is refused at publish.
func TestAServerScopedEventIsAuthorizedAsItsOwnKind(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		node := originOf("node", "prague-node-7")

		hub := newTestHub(t, Options{
			Authorizer: staticAuthorizer{visible: map[string][]event.Origin{
				alice: {printerA},
				bob:   {printerA, node},
			}},
		})

		defer hub.Close()

		// An event that names nothing never enters the hub, so no predicate
		// downstream has to decide what to do with it.
		_, err := hub.Publish(Publication{
			Type:  event.TypeResourceStateChanged,
			Class: event.ClassState,
		})
		require.ErrorIs(t, err, ErrInvalidRequest)

		watcher, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer watcher.Close()

		operator, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: bob})
		require.NoError(t, err)

		defer operator.Close()

		_, err = hub.Publish(Publication{
			Origin: node,
			Type:   event.TypeResourceStateChanged,
			Class:  event.ClassState,
			Data:   json.RawMessage(`{"state":"degraded"}`),
		})
		require.NoError(t, err)

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		require.Empty(t, watcher.Batches(),
			"a node event reached a principal the graph never granted the node to")

		batch := receive(t, operator)
		require.Len(t, batch.Events, 1)
		require.Equal(t, node, batch.Events[0].Position.Origin)
	})
}
