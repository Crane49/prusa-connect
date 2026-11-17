package stream

import (
	"context"
	"encoding/json"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/event"
)

// TestPublishRefusesATypeThatWouldForgeAFrame is the SSE frame-injection path
// the old backlog records, closed. An event type reaches a reader's `event:` field
// verbatim, so a name carrying a newline writes a frame of its own into every
// subscriber's stream.
func TestPublishRefusesATypeThatWouldForgeAFrame(t *testing.T) {
	t.Parallel()

	// Two of these are outside the core namespace on purpose. A forgery inside
	// it is refused by membership even with the character rule switched off, so
	// a table of only those would stay green on a hub that had lost the rule.
	forged := []string{
		event.CoreNamespace + "printer.arrived\nevent: forged",
		event.CoreNamespace + "printer.arrived\rdata: forged",
		event.CoreNamespace + "printer.arrived: forged",
		"com.example.plugin.thing\nevent: forged",
		"com.example.plugin.thing\rid: 1",
		"",
	}

	for _, name := range forged {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		_, err := hub.Publish(Publication{
			Origin: printerA,
			Type:   name,
			Class:  event.ClassState,
			Data:   json.RawMessage(`{}`),
		})

		require.ErrorIsf(t, err, ErrInvalidRequest, "%q was published", name)
	}
}

// TestPublishRefusesAnUnregisteredCoreType is the vocabulary half. Without the
// event-type set, a value nothing declares flows end to end with nothing to
// notice it.
func TestPublishRefusesAnUnregisteredCoreType(t *testing.T) {
	t.Parallel()

	hub := newTestHub(t, Options{})
	defer hub.Close()

	_, err := hub.Publish(Publication{
		Origin: printerA,
		Type:   event.CoreNamespace + "printer.invented",
		Class:  event.ClassState,
		Data:   json.RawMessage(`{}`),
	})

	require.ErrorIs(t, err, ErrInvalidRequest)
	require.ErrorIs(t, err, event.ErrInvalidEnvelope)
}

// TestPublishAcceptsEveryDeclaredCoreType is the other direction. A vocabulary
// the hub refuses is worse than no vocabulary, and a refusal test with no
// acceptance test beside it is half a test.
func TestPublishAcceptsEveryDeclaredCoreType(t *testing.T) {
	t.Parallel()

	types := event.Types()
	require.NotEmpty(t, types, "the vocabulary is empty, so this gate would publish nothing")

	for _, name := range types {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		class := event.ClassState
		if doc, ok := event.LookupType(name); ok {
			class = doc.Class
		}

		_, err := hub.Publish(Publication{
			Origin: printerA,
			Type:   name,
			Class:  class,
			Data:   json.RawMessage(`{}`),
		})

		require.NoErrorf(t, err, "the declared type %q was refused", name)
	}
}

// TestPublishAcceptsAnotherNamespace keeps the closure narrow: a plugin's event
// types stay in its own descriptor, and a peer's stay with the peer.
func TestPublishAcceptsAnotherNamespace(t *testing.T) {
	t.Parallel()

	hub := newTestHub(t, Options{})
	defer hub.Close()

	_, err := hub.Publish(Publication{
		Origin: printerA,
		Type:   "com.example.plugin.thing.happened",
		Class:  event.ClassState,
		Data:   json.RawMessage(`{}`),
	})

	require.NoError(t, err)
}

// TestATypeNarrowedReaderIsSentOnlyThoseTypes is the parameter's whole point.
// A fleet screen that wants state changes is otherwise sent every telemetry
// sample the caller may see, which at four hundred printers is the bulk of the
// stream.
func TestATypeNarrowedReaderIsSentOnlyThoseTypes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Types:     []string{event.TypeDeviceDeparted},
		})
		require.NoError(t, err)

		defer sub.Close()

		for _, pub := range []Publication{
			telemetryOf(printerA, "nozzle", 215),
			stateOf(printerA, 1),
			telemetryOf(printerA, "bed", 60),
		} {
			_, err = hub.Publish(pub)
			require.NoError(t, err)
		}

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, sub)

		require.Empty(t, batch.Telemetry, "a type this reader did not ask for reached it")
		require.Len(t, batch.Events, 1)
		require.Equal(t, event.TypeDeviceDeparted, batch.Events[0].Type)

		// The narrowing happens before the queues, so the filtered events cost
		// this reader no queue budget and are not counted as dropped.
		require.Equal(t, 0, sub.Stats().TelemetryKeys,
			"a filtered event was admitted to the queue and then discarded")
	})
}

// TestAnUnnarrowedReaderStillSeesEveryType holds the default. An empty set is
// every type, and the probe is skipped rather than matching nothing.
func TestAnUnnarrowedReaderStillSeesEveryType(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		for _, pub := range []Publication{
			telemetryOf(printerA, "nozzle", 215),
			stateOf(printerA, 1),
		} {
			_, err = hub.Publish(pub)
			require.NoError(t, err)
		}

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, sub)
		require.Len(t, batch.Telemetry, 1)
		require.Len(t, batch.Events, 1)
	})
}

// TestATypeNobodyPublishesNarrowsToNothing is the forward-compatibility rule.
// The hub takes the name rather than refusing it, so a client written against
// a later release keeps working against this one and simply receives nothing
// under that name.
func TestATypeNobodyPublishesNarrowsToNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Types:     []string{event.CoreNamespace + "printer.invented.by.a.later.release"},
		})
		require.NoError(t, err, "a type this build does not publish refused the whole subscription")

		defer sub.Close()

		_, err = hub.Publish(stateOf(printerA, 1))
		require.NoError(t, err)

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		require.Empty(t, sub.Batches(), "a reader narrowed to an unpublished type was sent something")
	})
}

// TestATypeNarrowingNarrowsAndNeverWidens is the containment half. Types is a
// preference over what Origins already decided, so naming a type on a printer
// this principal may not see reaches nothing.
func TestATypeNarrowingNarrowsAndNeverWidens(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Types:     []string{event.TypeDeviceDeparted},
		})
		require.NoError(t, err)

		defer sub.Close()

		_, err = hub.Publish(stateOf(printerB, 1))
		require.NoError(t, err)

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		require.Empty(t, sub.Batches(), "a printer alice may not see reached a type-narrowed stream")
	})
}
