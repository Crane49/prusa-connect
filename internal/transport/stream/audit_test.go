package stream

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/event"
)

// auditAuthorizer is a graph that answers audit sight per origin and can have
// it withdrawn while a subscription is open. Every change bumps the epoch,
// which is the only thing a live subscription reads on its tick.
type auditAuthorizer struct {
	mu      sync.Mutex
	visible map[string][]event.Origin
	audited map[seen]bool
	epoch   atomic.Uint64
}

func newAuditAuthorizer(visible map[string][]event.Origin, audited ...seen) *auditAuthorizer {
	a := &auditAuthorizer{visible: visible, audited: map[seen]bool{}}
	a.epoch.Store(1)

	for _, one := range audited {
		a.audited[one] = true
	}

	return a
}

func (a *auditAuthorizer) VisibleOrigins(_ context.Context, principal string) (Visibility, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	origins := make([]Grant, 0, len(a.visible[principal]))
	for _, origin := range a.visible[principal] {
		origins = append(origins, Grant{
			Origin: origin,
			Audit:  a.audited[seen{principal: principal, origin: origin}],
		})
	}

	return Visibility{Origins: origins, Epoch: a.epoch.Load()}, nil
}

func (a *auditAuthorizer) Epoch() uint64 { return a.epoch.Load() }

// revokeAudit withdraws one principal's audit sight of one origin, the way a
// grant withdrawal does: the rows change and the generation moves.
func (a *auditAuthorizer) revokeAudit(principal string, origin event.Origin) {
	a.mu.Lock()
	delete(a.audited, seen{principal: principal, origin: origin})
	a.mu.Unlock()

	a.epoch.Add(1)
}

// auditOf is one authorized frame on an origin.
func auditOf(origin event.Origin, commandID string) Publication {
	return Publication{
		Origin:      origin,
		Type:        event.TypeDeviceCommandAuthorized,
		Class:       event.ClassAudit,
		CausationID: commandID,
		AuthType:    "user",
		AuthID:      "operator",
		Data: json.RawMessage(
			`{"command_id":"` + commandID + `","verb":"core.command.send_info",` +
				`"auth_type":"user","auth_id":"operator"}`),
	}
}

// typesIn renders one batch as the event types it carried.
func typesIn(batch Batch) []string {
	out := make([]string, 0, len(batch.Events))
	for _, frame := range batch.Events {
		out = append(out, frame.Type)
	}

	return out
}

// TestAnAuditFrameReachesOnlyASubscriberWithAuditSight is the stream half of
// the equivalence gate for ADR-20260827-wgc rule 11. Both readers may see the printer
// and both are delivered its state frame; only the one holding audit sight is
// delivered the frame that says which operator commanded it.
func TestAnAuditFrameReachesOnlyASubscriberWithAuditSight(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authorizer := newAuditAuthorizer(
			map[string][]event.Origin{alice: {printerA}, bob: {printerA}},
			seen{principal: alice, origin: printerA},
		)

		hub := newTestHub(t, Options{Authorizer: authorizer})
		defer hub.Close()

		audited, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer audited.Close()

		plain, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: bob})
		require.NoError(t, err)

		defer plain.Close()

		_, err = hub.Publish(auditOf(printerA, "c-1"))
		require.NoError(t, err)

		_, err = hub.Publish(stateOf(printerA, 1))
		require.NoError(t, err)

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		require.Equal(t,
			[]string{event.TypeDeviceCommandAuthorized, event.TypeDeviceDeparted},
			typesIn(receive(t, audited)))

		require.Equal(t, []string{event.TypeDeviceDeparted}, typesIn(receive(t, plain)),
			"a subscriber holding no audit sight was told which operator commanded the printer")

		t.Log("observed: the audit frame reached the audited subscription and not the plain one")
	})
}

// TestRevokingAuditSightStopsDeliveryAtTheNextEpochMove is ADR-20260901-atx rule 12's
// second half. The boolean is not fixed at subscribe time: it is recomputed in
// the re-authorization pass that already rebuilds the allowed origins, so a
// long-lived stream cannot outlive its grant and no reconnect is needed.
func TestRevokingAuditSightStopsDeliveryAtTheNextEpochMove(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authorizer := newAuditAuthorizer(
			map[string][]event.Origin{alice: {printerA}},
			seen{principal: alice, origin: printerA},
		)

		hub := newTestHub(t, Options{Authorizer: authorizer})
		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		_, err = hub.Publish(auditOf(printerA, "c-1"))
		require.NoError(t, err)

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		require.Equal(t, []string{event.TypeDeviceCommandAuthorized}, typesIn(receive(t, sub)))

		authorizer.revokeAudit(alice, printerA)

		// The idle tick, which is what an unread stream re-authorizes on. The
		// check is on the publish path, so a frame published before the pump
		// has noticed the graph moved is delivered under the decision that was
		// in force when it was published: what rule 12 promises is that the
		// grant is rechecked as often as an origin grant is, not that a frame
		// already queued is retracted.
		time.Sleep(2 * AuthorizeInterval)
		synctest.Wait()

		_, err = hub.Publish(auditOf(printerA, "c-2"))
		require.NoError(t, err)

		_, err = hub.Publish(stateOf(printerA, 2))
		require.NoError(t, err)

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		require.Equal(t, []string{event.TypeDeviceDeparted}, typesIn(receive(t, sub)),
			"a revoked audit grant still delivered an audit frame on the same stream")

		t.Log("observed: audit delivery stopped at the epoch move, without a reconnect")
	})
}

// TestAReplayedAuditFrameIsFilteredToo is the path deliver never runs. A
// reconnecting reader is handed a window out of the channel's history, so a
// narrowing enforced only on the live path is a narrowing that leaks to
// everybody who reconnects.
func TestAReplayedAuditFrameIsFilteredToo(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authorizer := newAuditAuthorizer(map[string][]event.Origin{bob: {printerA}})

		hub := newTestHub(t, Options{Authorizer: authorizer})
		defer hub.Close()

		first, err := hub.Publish(stateOf(printerA, 1))
		require.NoError(t, err)

		_, err = hub.Publish(auditOf(printerA, "c-1"))
		require.NoError(t, err)

		_, err = hub.Publish(stateOf(printerA, 2))
		require.NoError(t, err)

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: bob,
			Resume:    resumeAt(hub, first),
		})
		require.NoError(t, err)

		defer sub.Close()

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		require.Equal(t, []string{event.TypeDeviceDeparted}, typesIn(receive(t, sub)),
			"a replay handed an audit frame to a reader with no audit sight")

		t.Log("observed: the replayed window carried the state frame and not the audit one")
	})
}

// TestAnOriginNarrowedSubscriptionKeepsItsAuditSight is the case every other
// case in this file missed, because every other case subscribes to everything.
//
// A reader that names the origins it wants goes through selectOrigins, which
// rebuilt each grant from the origin and its generation. Audit was left false,
// so a principal holding view_audit was silently denied the audit class on the
// live path and on the replay path both, while the history read served it the
// same frames. That is the one-transport narrowing ADR-20260827-wgc rule 11 and
// ADR-20260901-atx rules 12 and 13 exist to refuse, in the missing-frame direction.
//
// It was found against the running binary, not here: `curl` on
// `/api/v1/events?origins=device:<id>` received requested and settled and
// never authorized, and `GET /api/v1/history/facts` for the same principal and
// the same printer answered all three.
func TestAnOriginNarrowedSubscriptionKeepsItsAuditSight(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authorizer := newAuditAuthorizer(
			map[string][]event.Origin{alice: {printerA}},
			seen{principal: alice, origin: printerA},
		)

		hub := newTestHub(t, Options{Authorizer: authorizer})
		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Origins:   []event.Origin{printerA},
		})
		require.NoError(t, err)

		defer sub.Close()

		_, err = hub.Publish(auditOf(printerA, "c-1"))
		require.NoError(t, err)

		_, err = hub.Publish(stateOf(printerA, 1))
		require.NoError(t, err)

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		require.Equal(t,
			[]string{event.TypeDeviceCommandAuthorized, event.TypeDeviceDeparted},
			typesIn(receive(t, sub)),
			"naming the origin cost the subscriber the audit class it holds view_audit for")

		t.Log("observed: the origin-narrowed subscription was delivered the audit frame")
	})
}
