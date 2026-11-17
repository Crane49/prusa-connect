package stream

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/event"
)

// TestOnePrincipalHoldsOnlyAsManySubscriptionsAsTheHubAllows is the bound on
// what one caller can make this process hold. Every subscription is a goroutine,
// two queues and, for an SSE reader, a write buffer, and nothing else in the
// path counts them.
func TestOnePrincipalHoldsOnlyAsManySubscriptionsAsTheHubAllows(t *testing.T) {
	t.Parallel()

	hub := newTestHub(t, Options{MaxPerPrincipal: 2})
	defer hub.Close()

	held := make([]*Subscription, 0, 2)

	for range 2 {
		sub, err := hub.Subscribe(t.Context(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		held = append(held, sub)
	}

	_, err := hub.Subscribe(t.Context(), SubscribeRequest{Principal: alice})
	require.ErrorIs(t, err, ErrTooManySubscriptions, "a principal opened one more than the hub allows")

	var reached LimitError

	require.ErrorAs(t, err, &reached, "the refusal does not carry the bound it met")
	require.Equal(t, 2, reached.Limit)

	// The bound is per principal, so one caller at it cannot refuse anybody else.
	other, err := hub.Subscribe(t.Context(), SubscribeRequest{Principal: bob})
	require.NoError(t, err, "one principal at the bound refused a different principal")

	defer other.Close()

	// And a place comes back when the reader that held it goes away.
	held[0].Close()

	next, err := hub.Subscribe(t.Context(), SubscribeRequest{Principal: alice})
	require.NoError(t, err, "closing a subscription did not give its place back")

	defer next.Close()
	defer held[1].Close()
}

// TestARefusedSubscribeKeepsNoPlace is the leak the reservation opens if it is
// taken before the authorization query and not given back after one. A caller
// the graph grants nothing would otherwise spend the whole bound on refusals.
func TestARefusedSubscribeKeepsNoPlace(t *testing.T) {
	t.Parallel()

	hub := newTestHub(t, Options{MaxPerPrincipal: 1})
	defer hub.Close()

	// Narrowed to a printer nemo may not see, which is the refusal an unnarrowed
	// request no longer is: a principal permitted nothing who asked for nothing
	// in particular gets an empty stream.
	refused := SubscribeRequest{Principal: nemo, Origins: []event.Origin{printerA}}

	for range 3 {
		_, err := hub.Subscribe(t.Context(), refused)
		require.ErrorIs(t, err, ErrNotPermitted)
	}

	// nemo is permitted nothing throughout, so what is asserted is alice: a
	// principal whose refusals had counted would have spent her place too if the
	// counter were shared, and nemo's own place is proved by his refusals
	// staying the same refusal rather than becoming the limit one.
	sub, err := hub.Subscribe(t.Context(), SubscribeRequest{Principal: alice})
	require.NoError(t, err)

	defer sub.Close()

	_, err = hub.Subscribe(t.Context(), refused)
	require.ErrorIs(t, err, ErrNotPermitted,
		"a refusal was reported as a limit, so the refused attempts kept their places")
}

// TestTheCoalescingWindowIsHeldInsideItsBand is an earlier design decision's floor and the
// ceiling above it. A subscriber asking for either side of the band gets the
// band, because everything downstream sizes its queues against it.
func TestTheCoalescingWindowIsHeldInsideItsBand(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		asked time.Duration
		want  time.Duration
	}{
		"under the floor":  {asked: time.Millisecond, want: MinInterval},
		"at the floor":     {asked: MinInterval, want: MinInterval},
		"inside the band":  {asked: 500 * time.Millisecond, want: 500 * time.Millisecond},
		"at the ceiling":   {asked: MaxInterval, want: MaxInterval},
		"over the ceiling": {asked: time.Hour, want: MaxInterval},
		"none at all":      {asked: 0, want: DefaultInterval},
	}

	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			hub := newTestHub(t, Options{})
			defer hub.Close()

			sub, err := hub.Subscribe(t.Context(), SubscribeRequest{
				Principal: alice,
				Interval:  want.asked,
			})
			require.NoError(t, err)

			defer sub.Close()

			require.Equal(t, want.want, sub.interval)
		})
	}
}

// TestAResumeStartsAnUnnamedOriginWhereAFreshReaderStarts is the fleet-wide
// reconnect at the hub. A cursor renders only the origins its holder has seen an
// event from, so an origin it does not name is one this reader has no place in,
// and handing it the oldest event still retained replays a window the reader was
// never going to be given.
func TestAResumeStartsAnUnnamedOriginWhereAFreshReaderStarts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{
			Authorizer: staticAuthorizer{visible: map[string][]event.Origin{alice: {printerA, printerB}}},
		})
		defer hub.Close()

		// Both origins have a window, and the reader below holds a place in one.
		for value := range 3 {
			_, err := hub.Publish(stateOf(printerA, value+1))
			require.NoError(t, err)

			_, err = hub.Publish(stateOf(printerB, value+1))
			require.NoError(t, err)
		}

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Resume:    resumeAt(hub, event.Position{Origin: printerA, Offset: 2}),
		})
		require.NoError(t, err)

		defer sub.Close()

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, sub)
		require.False(t, batch.Resync, "a reader holding its own place was told its continuity was gone")

		replayed := make([]event.Position, 0, len(batch.Events))
		for _, frame := range batch.Events {
			replayed = append(replayed, frame.Position)
		}

		require.Equal(t, []event.Position{{Origin: printerA, Offset: 3}}, replayed,
			"the replay did not fill exactly the gap the cursor names")

		// The unnamed origin is live from its head, where a fresh reader starts.
		offset, named := heldBy(t, hub, sub.Cursor(), printerB)
		require.True(t, named, "the unnamed origin was not attached at its head")
		require.Equal(t, uint64(3), offset)
	})
}

// TestAResumeIsNotDerailedByAnOriginThatOutranItsWindow is the same reconnect
// against a fleet where somebody else is busy. An origin this reader holds no
// place in cannot make the whole subscription resync, because there is no gap
// under it to be unfillable.
func TestAResumeIsNotDerailedByAnOriginThatOutranItsWindow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{
			HistoryEvents: 2,
			Authorizer:    staticAuthorizer{visible: map[string][]event.Origin{alice: {printerA, printerB}}},
		})
		defer hub.Close()

		_, err := hub.Publish(stateOf(printerA, 1))
		require.NoError(t, err)

		// Past the two-event window, so this origin's oldest retained event is
		// no longer its first and a replay from zero could not be honoured.
		for value := range 4 {
			_, err = hub.Publish(stateOf(printerB, value+1))
			require.NoError(t, err)
		}

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Resume:    resumeAt(hub, event.Position{Origin: printerA, Offset: 1}),
		})
		require.NoError(t, err)

		defer sub.Close()

		// The reader holds printerA's head, so nothing is replayed and the only
		// thing a window could carry is the resync this must not produce.
		_, err = hub.Publish(stateOf(printerA, 2))
		require.NoError(t, err)

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, sub)
		require.False(t, batch.Resync,
			"an origin the reader holds no place in declared the whole subscription's continuity gone")
		require.Len(t, batch.Events, 1, "the reader was given something other than the one event it missed")
		require.Equal(t, printerA, batch.Events[0].Position.Origin)
	})
}

// TestOneHolderIsBoundedAcrossThePrincipalsItOpensFor is the bound on a caller
// that subscribes as principals other than itself. Every subscription is a
// goroutine and two queues whoever it is opened for, and a caller that names a
// new principal per subscription would meet no bound at all.
func TestOneHolderIsBoundedAcrossThePrincipalsItOpensFor(t *testing.T) {
	t.Parallel()

	hub := newTestHub(t, Options{MaxPerPrincipal: 2})
	defer hub.Close()

	held := make([]*Subscription, 0, 2)

	for _, principal := range []string{alice, bob} {
		sub, err := hub.Subscribe(t.Context(), SubscribeRequest{Principal: principal, Holder: "sink:relay"})
		require.NoError(t, err)

		held = append(held, sub)
	}

	// A third principal, and the holder is at the bound.
	_, err := hub.Subscribe(t.Context(), SubscribeRequest{Principal: nemo, Holder: "sink:relay"})
	require.ErrorIs(t, err, ErrTooManySubscriptions, "one holder opened one more than the hub allows")

	// The refusal names the holder and not the principal, which holds no place
	// yet: whoever reads it would otherwise go and look at the wrong name.
	var reached LimitError

	require.ErrorAs(t, err, &reached)
	require.Equal(t, "sink:relay", reached.Name)
	require.Contains(t, err.Error(), "sink:relay holds the limit of 2")

	// The principals themselves are not at it: each holds one place of two, so
	// a reader of its own is unaffected by what the holder opened.
	own, err := hub.Subscribe(t.Context(), SubscribeRequest{Principal: alice})
	require.NoError(t, err, "a holder at the bound refused the principal's own reader")

	defer own.Close()

	// And a place comes back for both names when a subscription ends.
	held[0].Close()

	next, err := hub.Subscribe(t.Context(), SubscribeRequest{Principal: nemo, Holder: "sink:relay"})
	require.NoError(t, err, "closing a subscription did not give the holder's place back")

	defer next.Close()
	defer held[1].Close()
}

// TestAHolderThatIsThePrincipalTakesOnePlace, because two names would halve
// what a reader holding its own subscriptions may open.
func TestAHolderThatIsThePrincipalTakesOnePlace(t *testing.T) {
	t.Parallel()

	hub := newTestHub(t, Options{MaxPerPrincipal: 2})
	defer hub.Close()

	for range 2 {
		sub, err := hub.Subscribe(t.Context(), SubscribeRequest{Principal: alice, Holder: alice})
		require.NoError(t, err)

		defer sub.Close()
	}

	_, err := hub.Subscribe(t.Context(), SubscribeRequest{Principal: alice, Holder: alice})
	require.ErrorIs(t, err, ErrTooManySubscriptions)
}

// TestAHolderRefusedAtTheBoundKeepsNoPlaceForThePrincipal is the leak a second
// reservation opens: the principal's place is taken before the holder's is
// asked for, so a refusal has to give the first one back.
func TestAHolderRefusedAtTheBoundKeepsNoPlaceForThePrincipal(t *testing.T) {
	t.Parallel()

	hub := newTestHub(t, Options{MaxPerPrincipal: 1})
	defer hub.Close()

	first, err := hub.Subscribe(t.Context(), SubscribeRequest{Principal: bob, Holder: "sink:relay"})
	require.NoError(t, err)

	defer first.Close()

	_, err = hub.Subscribe(t.Context(), SubscribeRequest{Principal: alice, Holder: "sink:relay"})
	require.ErrorIs(t, err, ErrTooManySubscriptions)

	own, err := hub.Subscribe(t.Context(), SubscribeRequest{Principal: alice})
	require.NoError(t, err, "a refused subscription kept the principal's place")

	defer own.Close()
}
