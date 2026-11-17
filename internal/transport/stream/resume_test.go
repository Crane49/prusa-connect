package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/event"
)

// TestResumeDeliversWhatWasMissedAndNothingElse is E6: the gap is filled from
// the cursor, and the filling is bounded by the same authorization decision the
// live path uses.
func TestResumeDeliversWhatWasMissedAndNothingElse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		first, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		_, err = hub.Publish(stateOf(printerA, 1))
		require.NoError(t, err)

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, first)
		require.Len(t, batch.Events, 1)
		require.Equal(t, 1, payloadValue(t, batch.Events[0]))

		cursor := batch.Cursor
		first.Close()
		synctest.Wait()

		// Published while nobody was reading: two for alice, and a burst for a
		// printer she may not see.
		for _, pub := range []Publication{
			stateOf(printerA, 2),
			stateOf(printerB, 98),
			stateOf(printerB, 99),
			stateOf(printerA, 3),
		} {
			_, err = hub.Publish(pub)
			require.NoError(t, err)
		}

		second, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice, Resume: cursor})
		require.NoError(t, err)

		defer second.Close()

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		resumed := receive(t, second)
		require.False(t, resumed.Resync, "the gap was inside the window, so continuity holds")
		require.Len(t, resumed.Events, 2)
		require.Equal(t, 2, payloadValue(t, resumed.Events[0]))
		require.Equal(t, 3, payloadValue(t, resumed.Events[1]))
		require.Equal(t, []uint64{2, 3}, offsets(resumed.Events),
			"the replay counted events on a printer she may not see")

		synctest.Wait()
		require.Empty(t, second.Batches(), "the replay stopped at the head")
	})
}

func TestResumeFromAnotherEpochRequiresResync(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		_, err := hub.Publish(stateOf(printerA, 1))
		require.NoError(t, err)

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Resume: event.NewCursor(event.NewGeneration("01STREAMTHATNOLONGEREXIST0",
				[]event.Position{{Origin: printerA, Offset: 4}})),
		})
		require.NoError(t, err)

		defer sub.Close()

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, sub)
		require.True(t, batch.Resync, "offsets from a stream that no longer exists cannot be matched")
		require.Empty(t, batch.Events, "there is no partial recovery")

		_, held := batch.Cursor.Generation(hub.Epoch())
		require.True(t, held, "the reader is handed a place in the generation that does exist")
	})
}

func TestResumeOutsideTheWindowRequiresResync(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{HistoryEvents: 4})
		defer hub.Close()

		var cursor event.Cursor

		for i := range 10 {
			position, err := hub.Publish(stateOf(printerA, i))
			require.NoError(t, err)

			if i == 0 {
				cursor = resumeAt(hub, position)
			}
		}

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice, Resume: cursor})
		require.NoError(t, err)

		defer sub.Close()

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, sub)
		require.True(t, batch.Resync, "the events between the cursor and the head have aged out")
	})
}

func TestResumeAheadOfTheLogRequiresResync(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Resume:    resumeAt(hub, event.Position{Origin: printerA, Offset: 500}),
		})
		require.NoError(t, err)

		defer sub.Close()

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, sub)
		require.True(t, batch.Resync, "a cursor ahead of the log belongs to a stream this one is not")
	})
}

// TestResumeReplaysAPrinterTheCursorDoesNotName covers a printer that produced
// its first event while the reader was away: absent from the cursor means "I
// have seen nothing from it", not "start me live".
func TestResumeReplaysAPrinterTheCursorDoesNotName(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{
			Authorizer: staticAuthorizer{visible: map[string][]event.Origin{alice: {printerA, printerB}}},
		})

		defer hub.Close()

		_, err := hub.Publish(stateOf(printerA, 1))
		require.NoError(t, err)

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Resume:    resumeAt(hub, event.Position{Origin: printerA, Offset: 1}),
		})
		require.NoError(t, err)

		defer sub.Close()

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()
		require.Empty(t, sub.Batches(), "printerB has published nothing, so there is nothing to replay")

		_, err = hub.Publish(stateOf(printerB, 7))
		require.NoError(t, err)

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, sub)
		require.Len(t, batch.Events, 1)
		require.Equal(t, printerB, batch.Events[0].Position.Origin)
		require.Equal(t, uint64(1), batch.Events[0].Position.Offset, "printerB counts its own events from one")
	})
}

// TestTelemetryIsNotReplayed keeps the replay window honest: a resubscribe gets
// a fresh value within one coalescing window, so replaying a measurement that is
// already wrong would cost memory to deliver stale data.
func TestTelemetryIsNotReplayed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		for i := range 50 {
			position, err := hub.Publish(telemetryOf(printerA, "nozzle", i))
			require.NoError(t, err)
			require.Equal(t, uint64(0), position.Offset, "telemetry takes no offset")
		}

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Resume:    event.NewCursor(event.Generation{Epoch: hub.Epoch()}),
		})
		require.NoError(t, err)

		defer sub.Close()

		synctest.Wait()
		require.Empty(t, sub.Batches())
		require.Equal(t, 0, sub.Stats().EventCount)
	})
}

func TestResumeIsRefusedForAPrinterThePrincipalLost(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := staticAuthorizer{visible: map[string][]event.Origin{alice: {printerA, printerB}}}
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		_, err := hub.Publish(stateOf(printerB, 1))
		require.NoError(t, err)

		// The grant on printerB is gone by the time she reconnects.
		authz.visible[alice] = []event.Origin{printerA}

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Resume:    event.NewCursor(event.Generation{Epoch: hub.Epoch()}),
		})
		require.NoError(t, err)

		defer sub.Close()

		synctest.Wait()
		require.Empty(t, sub.Batches(), "the replay applies the decision made at this subscribe, not the last one")
	})
}

// TestAResumeNamingALostOriginIsToldItIsWithdrawn is the reader that was away
// while its grant went: the cursor it comes back with names the printer, so it
// is told the printer left its sight rather than left to wait on it.
func TestAResumeNamingALostOriginIsToldItIsWithdrawn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := staticAuthorizer{visible: map[string][]event.Origin{alice: {printerA, printerB}}}
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		_, err := hub.Publish(stateOf(printerB, 1))
		require.NoError(t, err)

		authz.visible[alice] = []event.Origin{printerA}

		batch := resumeWithdrawn(t, hub, resumeAt(hub, event.Position{Origin: printerB, Offset: 1}))
		require.Equal(t, []event.Origin{printerB}, batch.Withdrawn)
	})
}

// TestAResumeNamingAnOriginThatNeverExistedIsToldTheSame is the other half of
// the previous test: an origin nothing ever published reads exactly like one
// taken away, so the answer is no probe of what exists.
func TestAResumeNamingAnOriginThatNeverExistedIsToldTheSame(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		never := originOf(event.KindDevice, "01JB8XN2K7QW9ZR4MV3T6H0PZZ")

		batch := resumeWithdrawn(t, hub, resumeAt(hub, event.Position{Origin: never, Offset: 1}))
		require.Equal(t, []event.Origin{never}, batch.Withdrawn)
	})
}

func resumeWithdrawn(t *testing.T, hub *Hub, cursor event.Cursor) Batch {
	t.Helper()

	sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice, Resume: cursor})
	require.NoError(t, err)

	t.Cleanup(sub.Close)

	time.Sleep(2 * DefaultInterval)
	synctest.Wait()

	batch := receive(t, sub)
	require.Empty(t, batch.Events)
	require.False(t, batch.Resync)

	return batch
}

// TestReplayIsOrderedAheadOfLiveEvents is the ordering hazard in the attach:
// anything the live path queues while the subscription is being wired up must
// end up behind the replay, or a reader's offsets go backwards.
func TestReplayIsOrderedAheadOfLiveEvents(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		var live []*Message

		for i := range 4 {
			position, publishErr := hub.Publish(stateOf(printerA, i))
			require.NoError(t, publishErr)
			require.Equal(t, uint64(i+1), position.Offset)
		}

		// What the live path queued while the attach was in progress: the two
		// newest events, already in the queue when the replay arrives.
		queued, _ := sub.events.drain(nil)
		live = append(live, queued[2:]...)

		replayed, missing := sub.routing()[0].hist.since(0)
		require.False(t, missing)

		for _, m := range live {
			sub.events.put(m)
		}

		sub.events.adopt(replayed)

		drained, _ := sub.events.drain(nil)
		require.Equal(t, []uint64{1, 2, 3, 4}, positionsOf(drained),
			"the replay was appended behind the live events instead of ahead of them")
	})
}

// TestAnEventInFlightDuringAnAttachIsNotDeliveredTwice is the same hazard on the
// other side of the replay: a publisher that took its offset before the attach
// can enqueue it after the replay that already covered it.
func TestAnEventInFlightDuringAnAttachIsNotDeliveredTwice(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		for i := range 3 {
			_, publishErr := hub.Publish(stateOf(printerA, i))
			require.NoError(t, publishErr)
		}

		inFlight, _ := sub.events.drain(nil)
		require.Len(t, inFlight, 3)

		replayed, missing := sub.routing()[0].hist.since(0)
		require.False(t, missing)

		sub.events.adopt(replayed)

		// The publisher gets to its enqueue only now, after the replay it was
		// racing has already delivered the same offsets.
		for _, m := range inFlight {
			sub.events.put(m)
		}

		drained, _ := sub.events.drain(nil)
		require.Equal(t, []uint64{1, 2, 3}, positionsOf(drained),
			"an event the replay already covered was delivered again")
	})
}

// TestAnEventAResumeCursorNamesIsNotDeliveredAgain is the half of the hazard a
// replay cannot cover.
//
// Publish appends to the replay window before it reads the subscriber list, so
// a publisher already inside that fan-out reaches a subscription that attached
// after the offset was minted. A reader resuming at that offset is replayed
// nothing, because the replay starts above it, and the fan-out then hands it
// the event its cursor says it already holds.
func TestAnEventAResumeCursorNamesIsNotDeliveredAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		position, err := hub.Publish(stateOf(printerA, 1))
		require.NoError(t, err)

		// The place the reader comes back with, which is the Last-Event-ID a
		// browser sends after receiving that event.
		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Resume:    resumeAt(hub, position),
		})
		require.NoError(t, err)

		defer sub.Close()

		inFlight, missing := sub.routing()[0].hist.since(position.Offset - 1)
		require.False(t, missing)
		require.Len(t, inFlight, 1)

		// The publisher reaches the fan-out only now, with the attach behind it.
		sub.deliver(inFlight[0])

		queued, _ := sub.events.drain(nil)
		require.Empty(t, positionsOf(queued),
			"an event the resume cursor already named was delivered a second time")
	})
}

// TestAFreshReaderIsNotHandedAnEventBelowTheHeadItStartsAt is the same rule for
// a reader that carries no cursor. It starts at each origin's head, so an event
// at or below that head is one it is already past, however late the publisher
// carrying it reaches the fan-out.
func TestAFreshReaderIsNotHandedAnEventBelowTheHeadItStartsAt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		position, err := hub.Publish(stateOf(printerA, 1))
		require.NoError(t, err)

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		inFlight, missing := sub.routing()[0].hist.since(position.Offset - 1)
		require.False(t, missing)
		require.Len(t, inFlight, 1)

		sub.deliver(inFlight[0])

		queued, _ := sub.events.drain(nil)
		require.Empty(t, positionsOf(queued),
			"an event below the head this reader started at was delivered to it")
	})
}

func positionsOf(messages []*Message) []uint64 {
	out := make([]uint64, 0, len(messages))
	for _, m := range messages {
		out = append(out, m.position.Offset)
	}

	return out
}

// arrivalOf is what a printer reports about itself when it dials in. It is the
// payload the replay tests below are about: a reader who receives one of these
// learns the model, the firmware and the serial of a machine, which is what a
// refusal exists to withhold.
func arrivalOf(origin event.Origin, serial string) Publication {
	return Publication{
		Origin: origin,
		Type:   event.TypeDeviceArrived,
		Class:  event.ClassState,
		Data: json.RawMessage(fmt.Sprintf(
			`{"model":"Core One","firmware":"6.2.4","serial":%q}`, serial)),
	}
}

// eventsIn renders one window's durable frames as the type, place and payload
// each carries, so a failure names the events that reached the reader rather
// than only how many there were.
func eventsIn(t *testing.T, batch Batch) []string {
	t.Helper()

	out := make([]string, 0, len(batch.Events))

	for _, frame := range batch.Events {
		var env event.Envelope
		require.NoError(t, json.Unmarshal(frame.Bytes, &env))

		out = append(out, fmt.Sprintf("%s at offset %d carrying %s",
			env.Type, frame.Position.Offset, env.Data))
	}

	return out
}

// received collects every durable frame a subscription has ready, across every
// window waiting for it.
func received(t *testing.T, sub *Subscription) []string {
	t.Helper()

	out := make([]string, 0)

	for {
		select {
		case batch, open := <-sub.Batches():
			if !open {
				return out
			}

			out = append(out, eventsIn(t, batch)...)
		default:
			return out
		}
	}
}

// resumeText is the Last-Event-ID a client sends, built the way a client builds
// it rather than through the constructor this server stamps its own cursor with:
// NewGeneration drops an offset of zero and ParseCursor keeps it, and zero is
// the offset that asks for everything.
func resumeText(t *testing.T, hub *Hub, origin event.Origin, offset uint64) event.Cursor {
	t.Helper()

	cursor, err := event.ParseCursor(fmt.Sprintf("%s:%s=%d", hub.Epoch(), origin.Path(), offset))
	require.NoError(t, err)

	return cursor
}

// TestAResumeCursorDoesNotReachBehindTheGrant is the resume path's half of the
// bound the re-authorization path already applies. A grant is permission to read
// from now, and a cursor is client-supplied text: a principal granted an origin
// after it had been publishing may name offset zero in it and must still be
// given nothing that predates the grant.
func TestAResumeCursorDoesNotReachBehindTheGrant(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := newRevocableAuthorizer(map[string][]event.Origin{})
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		// The printer dials in and reports itself, all of it before anybody holds
		// anything on it.
		for value := range 5 {
			_, err := hub.Publish(arrivalOf(printerB, fmt.Sprintf("CZ-2026-%04d", value)))
			require.NoError(t, err)
		}

		authz.grant(printerB)

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Resume:    resumeText(t, hub, printerB, 0),
		})
		require.NoError(t, err)

		defer sub.Close()

		require.Equal(t, []event.Origin{printerB}, sub.Origins())

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, sub)
		require.Empty(t, eventsIn(t, batch),
			"a cursor reached the window this printer published before the grant was made")
		require.True(t, batch.Resync,
			"a place that cannot be honoured was quietly moved forward instead of reported")

		// And the reader carries on: the place it is handed is the head of what it
		// may now read, so the next event reaches it without another reconnect.
		offset, held := heldBy(t, hub, batch.Cursor, printerB)
		require.True(t, held)
		require.Equal(t, uint64(5), offset)

		_, err = hub.Publish(arrivalOf(printerB, "CZ-2026-0005"))
		require.NoError(t, err)

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		require.Equal(t, []uint64{6}, offsets(receive(t, sub).Events),
			"the reader that was told its place was gone was left with no stream")
	})
}

// TestNoHostileCursorReachesBehindTheGrant walks the shapes a client can put in
// Last-Event-ID against an origin it was granted only after that origin had been
// publishing. The offset in a cursor is whatever the sender chose, so a cursor
// narrows what the grant covers and none of these widens it.
//
// Only the first two are refused by the grant floor. The rest are refused before
// it and are here because the answer has to be the same whichever mechanism
// reaches them first: an offset past the head by the log's own bound, a
// generation this hub did not mint by the epoch match, and an origin nobody
// granted by never being in the routing table at all.
func TestNoHostileCursorReachesBehindTheGrant(t *testing.T) {
	cursors := map[string]func(*testing.T, *Hub) event.Cursor{
		"offset zero, which asks for the whole window": func(t *testing.T, hub *Hub) event.Cursor {
			t.Helper()

			return resumeText(t, hub, printerB, 0)
		},
		"an offset inside the window": func(t *testing.T, hub *Hub) event.Cursor {
			t.Helper()

			return resumeText(t, hub, printerB, 2)
		},
		"an offset ahead of the log": func(t *testing.T, hub *Hub) event.Cursor {
			t.Helper()

			return resumeText(t, hub, printerB, 500)
		},
		"a generation this hub never minted": func(t *testing.T, _ *Hub) event.Cursor {
			t.Helper()

			return event.NewCursor(event.NewGeneration("01STREAMTHATNOLONGEREXIST0",
				[]event.Position{{Origin: printerB, Offset: 1}}))
		},
		"an origin the caller was never granted": func(t *testing.T, hub *Hub) event.Cursor {
			t.Helper()

			return resumeText(t, hub, printerA, 0)
		},
	}

	for name, build := range cursors {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				authz := newRevocableAuthorizer(map[string][]event.Origin{})
				hub := newTestHub(t, Options{Authorizer: authz})

				defer hub.Close()

				for value := range 5 {
					_, err := hub.Publish(arrivalOf(printerB, fmt.Sprintf("CZ-2026-%04d", value)))
					require.NoError(t, err)
				}

				for value := range 3 {
					_, err := hub.Publish(arrivalOf(printerA, fmt.Sprintf("CZ-2025-%04d", value)))
					require.NoError(t, err)
				}

				authz.grant(printerB)

				sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
					Principal: alice,
					Resume:    build(t, hub),
				})
				require.NoError(t, err)

				defer sub.Close()

				require.Equal(t, []event.Origin{printerB}, sub.Origins())

				time.Sleep(2 * DefaultInterval)
				synctest.Wait()

				require.Empty(t, received(t, sub), "a cursor reached a window published before the grant")
			})
		})
	}
}

// TestAResumeIsNotBackdatedByAnAnswerStampedOlderThanTheGraph is the resume
// path's half of the stale-stamp case. `authz.Store.Visible` reads the graph's
// generation before the rows it returns, on purpose, so an answer can carry a
// grant and a generation from before the write that made it. A replay bounded by
// the stamp alone would start before the grant.
func TestAResumeIsNotBackdatedByAnAnswerStampedOlderThanTheGraph(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := &staleAuthorizer{}
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		for value := range 5 {
			_, err := hub.Publish(arrivalOf(printerB, fmt.Sprintf("CZ-2026-%04d", value)))
			require.NoError(t, err)
		}

		// The grant lands, and the answer that carries it is stamped with the
		// generation that was read before it.
		authz.grant(printerB)

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Resume:    resumeText(t, hub, printerB, 0),
		})
		require.NoError(t, err)

		defer sub.Close()

		require.Equal(t, []event.Origin{printerB}, sub.Origins())

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		require.Empty(t, received(t, sub),
			"a stale stamp let a cursor reach the window published before the grant")
	})
}

// TestAResumeStillReplaysTheWindowAGrantCovers is the boundary the case above
// must not cross, and it is here so that the bound cannot be satisfied by
// replaying nothing to anybody: a reader granted the origin before it published
// gets every event it missed, from the offset its cursor names.
func TestAResumeStillReplaysTheWindowAGrantCovers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := newRevocableAuthorizer(map[string][]event.Origin{alice: {printerB}})
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		for value := range 5 {
			_, err := hub.Publish(arrivalOf(printerB, fmt.Sprintf("CZ-2026-%04d", value)))
			require.NoError(t, err)
		}

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Resume:    resumeText(t, hub, printerB, 2),
		})
		require.NoError(t, err)

		defer sub.Close()

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, sub)
		require.False(t, batch.Resync, "the gap was inside the window and inside the grant")
		require.Equal(t, []uint64{3, 4, 5}, offsets(batch.Events),
			"a grant made before the window was published replays what the cursor missed")
	})
}

// TestAnUnrelatedWriteCostsAResumeNothing is the other end of the same bound.
// alice held this printer throughout and her grant never changed; a write
// somewhere else in the graph moved the counter, and the events she missed were
// published before it. Her place is honoured, because the floor is the
// generation her own grant began at and that write did not move it.
//
// A floor taken from the graph as it stands costs her the replay instead. That
// is a reload for the state classes and a loss for the other two: no endpoint
// re-serves a command result or an audit record, so what a resync discards
// there is discarded. What keeps her place is the authorizer reporting the
// generation each origin's grant began at, which is the open item this closes
// under the hub in the old backlog.
func TestAnUnrelatedWriteCostsAResumeNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := newRevocableAuthorizer(map[string][]event.Origin{alice: {printerB}})
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		for value := range 5 {
			_, err := hub.Publish(arrivalOf(printerB, fmt.Sprintf("CZ-2026-%04d", value)))
			require.NoError(t, err)
		}

		// The same grant written again: alice's set does not change and the
		// counter moves anyway, which is what any write to the graph does.
		authz.grant(printerB)

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Resume:    resumeText(t, hub, printerB, 2),
		})
		require.NoError(t, err)

		defer sub.Close()

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, sub)
		require.False(t, batch.Resync, "a write that moved nobody's grant cost this reader its place")
		require.Equal(t, []uint64{3, 4, 5}, offsets(batch.Events),
			"the reader was not handed what its cursor missed")

		// A reader that missed nothing is untouched by the same write, which is
		// what keeps the ordinary reconnect out of this.
		atHead, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Resume:    resumeText(t, hub, printerB, 5),
		})
		require.NoError(t, err)

		defer atHead.Close()

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		require.Empty(t, atHead.Batches(), "a reader already at the head was told its place was gone")
	})
}

// TestAForgedPositionForAnInvisiblePrinterIsIgnored is the resume path's half of
// a design requirement: a cursor is client-supplied, so what the hub does with a
// position it did not issue must not answer a question about the printer named
// in it.
func TestAForgedPositionForAnInvisiblePrinterIsIgnored(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		for range 20 {
			_, err := hub.Publish(stateOf(printerB, 1))
			require.NoError(t, err)
		}

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Resume: resumeAt(hub,
				event.Position{Origin: printerB, Offset: 1},
				event.Position{Origin: unknown, Offset: 900},
			),
		})
		require.NoError(t, err)

		defer sub.Close()

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		// Both are named back as withdrawn, the printer that exists and the one
		// that never did alike, so the answer says nothing about either.
		batch := receive(t, sub)
		require.ElementsMatch(t, []event.Origin{printerB, unknown}, batch.Withdrawn)
		require.Empty(t, batch.Events)
		require.False(t, batch.Resync)
		require.Empty(t, sub.Batches(),
			"a position the reader may not hold produced a second answer, resync or otherwise")
		require.False(t, sub.Stats().Resync)
	})
}
