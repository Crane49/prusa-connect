package stream

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/event"
)

// TestAnOriginThatFirstAppearsAfterASubscriptionReachesIt is the order a printer
// really arrives in: the grant that makes it visible is written first, and the
// event announcing it is published second, on a channel that did not exist until
// that publication created it.
//
// The subscription is open before either happens, which is what makes this a
// live read. A subscription whose origin set was fixed at subscribe time has
// nothing attached to the new channel, so the arrival lands in that channel's
// replay window and reaches nobody.
func TestAnOriginThatFirstAppearsAfterASubscriptionReachesIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := newRevocableAuthorizer(map[string][]event.Origin{alice: {printerA}})
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		require.Equal(t, []event.Origin{printerA}, sub.Origins())

		// The device plane registers the printer, which moves the epoch, and
		// then publishes its arrival. Nothing between the two lets a subscriber
		// attach itself.
		authz.grant(printerA, printerB)

		_, err = hub.Publish(stateOf(printerB, 1))
		require.NoError(t, err)

		time.Sleep(2 * AuthorizeInterval)
		synctest.Wait()

		require.ElementsMatch(t, []event.Origin{printerA, printerB}, sub.Origins())

		batch := receive(t, sub)
		require.Len(t, batch.Events, 1, "the arrival of a printer that dialled in reached nobody")
		require.Equal(t, printerB, batch.Events[0].Position.Origin)
		require.Equal(t, uint64(1), batch.Events[0].Position.Offset)
		require.Equal(t, 1, payloadValue(t, batch.Events[0]))

		// The cursor names the place the reader has reached, so a reconnect
		// resumes after the arrival rather than replaying it.
		offset, held := heldBy(t, hub, batch.Cursor, printerB)
		require.True(t, held)
		require.Equal(t, uint64(1), offset)
	})
}

// TestAnOriginThatAppearedBeforeTheSubscriptionIsStillReadFromTheHead is the
// boundary the case above must not cross. A channel this hub already had when
// the subscription started carries events from before it, and a grant made later
// is permission to read from now.
func TestAnOriginThatAppearedBeforeTheSubscriptionIsStillReadFromTheHead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := newRevocableAuthorizer(map[string][]event.Origin{alice: {printerA}})
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		// printerB is on the bus and alice may not see it.
		for value := range 3 {
			_, err := hub.Publish(stateOf(printerB, value))
			require.NoError(t, err)
		}

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		authz.grant(printerA, printerB)

		time.Sleep(2 * AuthorizeInterval)
		synctest.Wait()

		require.ElementsMatch(t, []event.Origin{printerA, printerB}, sub.Origins())
		require.Empty(t, sub.Batches(), "a grant replayed a channel that predates the subscription")
	})
}

// TestAGrantMadeAfterAnArrivalReplaysNothingFromBeforeIt is the door the case
// above opens if the replay is bounded by the subscription rather than by the
// grant. A page opened before the first printer dialled in holds a stream that
// carries nothing, the printer arrives and publishes, and the grant is written
// afterwards. What that grant is permission to read starts where it was made.
func TestAGrantMadeAfterAnArrivalReplaysNothingFromBeforeIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := newRevocableAuthorizer(map[string][]event.Origin{})
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		require.Empty(t, sub.Origins(), "a principal the graph grants nothing was attached to something")

		// The printer dials in and publishes, on a channel this hub creates after
		// the subscription started and that alice may not see.
		for value := range 5 {
			_, err = hub.Publish(stateOf(printerB, value+1))
			require.NoError(t, err)
		}

		authz.grant(printerB)

		time.Sleep(2 * AuthorizeInterval)
		synctest.Wait()

		require.Equal(t, []event.Origin{printerB}, sub.Origins())
		require.Empty(t, sub.Batches(), "the grant replayed the window published before it was made")
	})
}

// TestAGrantIsNotBackdatedByAGenerationTheReaderWasNeverCheckedAt is the same
// leak reached through a pump that did not run. Two writes land between the
// reader's last check and the grant, so "later than the last check" alone lets
// through everything published during them.
func TestAGrantIsNotBackdatedByAGenerationTheReaderWasNeverCheckedAt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := newRevocableAuthorizer(map[string][]event.Origin{})
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		// A write that grants alice nothing, then the printer's traffic, then the
		// grant. Nothing sleeps in between, so the pump is checked at neither of
		// the first two generations.
		authz.grant()

		for value := range 5 {
			_, err = hub.Publish(stateOf(printerB, value+1))
			require.NoError(t, err)
		}

		authz.grant(printerB)

		time.Sleep(2 * AuthorizeInterval)
		synctest.Wait()

		require.Equal(t, []event.Origin{printerB}, sub.Origins())
		require.Empty(t, sub.Batches(), "the grant replayed a generation the reader was never checked at")
	})
}

// TestAnAnswerStampedOlderThanTheGraphDoesNotBackdateAGrant is the other half.
// The graph reads its generation before the rows it returns, so an answer can
// name a generation older than the grant it already carries, and a replay bound
// by that generation alone would start before the grant.
func TestAnAnswerStampedOlderThanTheGraphDoesNotBackdateAGrant(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := &staleAuthorizer{}
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		require.Empty(t, sub.Origins())

		for value := range 5 {
			_, err = hub.Publish(stateOf(printerB, value+1))
			require.NoError(t, err)
		}

		// The grant lands, and the answer that carries it is stamped with the
		// generation that was read before it.
		authz.grant(printerB)

		time.Sleep(2 * AuthorizeInterval)
		synctest.Wait()

		require.Equal(t, []event.Origin{printerB}, sub.Origins())
		require.Empty(t, sub.Batches(), "a stale stamp replayed the window published before the grant")
	})
}

// staleAuthorizer answers with the rows it has now and the generation it had
// before them, which is what a graph reading its counter ahead of its rows does
// when a write lands in between.
type staleAuthorizer struct {
	mu      sync.Mutex
	visible []event.Origin
	stamp   uint64
	epoch   atomic.Uint64
}

func (a *staleAuthorizer) VisibleOrigins(context.Context, string) (Visibility, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	return Visibility{Origins: grantsOf(a.visible...), Epoch: a.stamp}, nil
}

func (a *staleAuthorizer) Epoch() uint64 { return a.epoch.Load() }

// grant records the generation as it stood before the rows it then writes,
// which is the order internal/app's visibility reads them in.
func (a *staleAuthorizer) grant(origins ...event.Origin) {
	a.mu.Lock()
	a.stamp = a.epoch.Load()
	a.visible = origins
	a.mu.Unlock()

	a.epoch.Add(1)
}

// TestEveryArrivalOfAFleetReachesAReaderWhoWasAlreadyWatching is the case above
// with more than one printer in it, which is what a site has.
//
// Each machine is registered by a write of its own, so each one moves the graph
// a generation, and every one of them lands before the reader's pump is next
// checked. A floor taken from the generation the answer was computed at is then
// six generations past the first arrival, and only the machine registered last
// is replayed. The other five announcements reach nobody.
func TestEveryArrivalOfAFleetReachesAReaderWhoWasAlreadyWatching(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := newRevocableAuthorizer(map[string][]event.Origin{alice: {}})
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		require.Empty(t, sub.Origins())

		// The fleet dials in. One registration, one generation, one arrival, and
		// nothing in between lets the reader attach itself.
		var granted []event.Origin

		for _, printer := range arrivingFleet {
			granted = append(granted, printer)
			authz.grant(granted...)

			_, err = hub.Publish(stateOf(printer, 1))
			require.NoError(t, err)
		}

		time.Sleep(2 * AuthorizeInterval)
		synctest.Wait()

		require.ElementsMatch(t, arrivingFleet, sub.Origins())

		batch := receive(t, sub)

		arrived := make([]event.Origin, 0, len(batch.Events))
		for _, frame := range batch.Events {
			arrived = append(arrived, frame.Position.Origin)
		}

		require.ElementsMatch(t, arrivingFleet, arrived,
			"the arrival of a printer that dialled in reached nobody")
	})
}

// arrivingFleet is a site rather than a machine: six printers, each admitted by a
// write of its own.
var arrivingFleet = []event.Origin{
	originOf(event.KindDevice, "01JB8XN2K7QW9ZR4MV3T6H0P01"),
	originOf(event.KindDevice, "01JB8XN2K7QW9ZR4MV3T6H0P02"),
	originOf(event.KindDevice, "01JB8XN2K7QW9ZR4MV3T6H0P03"),
	originOf(event.KindDevice, "01JB8XN2K7QW9ZR4MV3T6H0P04"),
	originOf(event.KindDevice, "01JB8XN2K7QW9ZR4MV3T6H0P05"),
	originOf(event.KindDevice, "01JB8XN2K7QW9ZR4MV3T6H0P06"),
}

// TestANewOriginDoesNotWidenANarrowedSubscription keeps the re-attach inside the
// narrowing the reader asked for. A printer dialling in is not a reason to start
// sending it to somebody who asked for one other printer.
func TestANewOriginDoesNotWidenANarrowedSubscription(t *testing.T) {
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

		authz.grant(printerA, printerB)

		_, err = hub.Publish(stateOf(printerB, 1))
		require.NoError(t, err)

		time.Sleep(2 * AuthorizeInterval)
		synctest.Wait()

		require.Equal(t, []event.Origin{printerA}, sub.Origins())
		require.Empty(t, sub.Batches(), "a printer that dialled in widened a subscription that named one")
	})
}

// TestAnOriginThatAppearsWhileTheGrantIsAbsentStaysUnread is the security half:
// a channel created after the subscription started is still only read once the
// graph says so, and a principal the grant never reached is told nothing.
func TestAnOriginThatAppearsWhileTheGrantIsAbsentStaysUnread(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := newRevocableAuthorizer(map[string][]event.Origin{alice: {printerA}, bob: {printerA}})
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		watcher, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: bob})
		require.NoError(t, err)

		defer watcher.Close()

		// alice is granted the new printer. bob is not.
		authz.grant(printerA, printerB)

		_, err = hub.Publish(stateOf(printerB, 1))
		require.NoError(t, err)

		time.Sleep(2 * AuthorizeInterval)
		synctest.Wait()

		require.Equal(t, []event.Origin{printerA}, watcher.Origins())
		require.Empty(t, watcher.Batches(), "a printer bob holds nothing on reached his stream")
	})
}
