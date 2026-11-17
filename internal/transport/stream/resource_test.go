package stream

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/event"
)

// degradedOf is an instance state transition in the shape R2 asks for: a
// derived state, and the generation of the instance spec it was observed at.
func degradedOf(origin event.Origin, generation int) Publication {
	return Publication{
		Origin: origin,
		Type:   event.TypeResourceStateChanged,
		Class:  event.ClassState,
		Data: json.RawMessage(
			`{"state":"degraded","reason":"bucket unreachable","observedGeneration":` +
				string(rune('0'+generation)) + `}`),
	}
}

// TestInstanceStateTransitionRidesTheSameBus is an earlier design decision. A storage adapter
// has no printer, and the assertion is that none appears anywhere: not in the
// publication, not in the source, not in the subject, and not in the cursor the
// reader resumes from.
func TestInstanceStateTransitionRidesTheSameBus(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{
			Authorizer: staticAuthorizer{visible: map[string][]event.Origin{alice: {s3Main}}},
		})

		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Encoding:  EncodingSSE,
		})
		require.NoError(t, err)

		defer sub.Close()

		position, err := hub.Publish(degradedOf(s3Main, 4))
		require.NoError(t, err)
		require.Equal(t, s3Main, position.Origin)
		require.Equal(t, uint64(1), position.Offset, "a storage instance counts its own events from one")

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, sub)
		require.Len(t, batch.Events, 1)

		frame := batch.Events[0]
		require.Equal(t, s3Main, frame.Position.Origin)

		var env event.Envelope
		require.NoError(t, json.Unmarshal(sseData(t, frame.Bytes), &env))

		require.Equal(t, "urn:prusa:site:01K2A0SPRAGUEFARM07/storage/s3-main", env.Source,
			"the transition is addressed as a storage instance, not as a printer")
		require.Equal(t, "storage:s3-main", env.Subject, "the subject is the authorization key")
		require.Equal(t, event.ClassState, env.Class, "a transition is durable, so it is replayable")
		require.Equal(t, hub.Epoch(), env.Epoch)
		require.NotContains(t, string(frame.Bytes), "printer",
			"a printer appeared somewhere in a storage instance's event")

		// The reader resumes from it like any other event.
		id := frame.Position.ID(env.Epoch)
		require.Equal(t, hub.Epoch()+":storage/s3-main:1", id)

		resume, err := event.ParseID(id)
		require.NoError(t, err)

		_, err = hub.Publish(degradedOf(s3Main, 5))
		require.NoError(t, err)

		resumed, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Resume:    resume,
		})
		require.NoError(t, err)

		defer resumed.Close()

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		second := receive(t, resumed)
		require.False(t, second.Resync, "the resume was honoured")
		require.Len(t, second.Events, 1, "the resume delivered the one transition it had missed")
		require.Equal(t, uint64(2), second.Events[0].Position.Offset)
	})
}

// TestACameraPublishesWithoutAPrinter covers the other day-one resource that
// owns no printer.
func TestACameraPublishesWithoutAPrinter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{
			Authorizer: staticAuthorizer{visible: map[string][]event.Origin{alice: {camFront}}},
		})

		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		_, err = hub.Publish(Publication{
			Origin: camFront,
			Type:   event.TypeResourceStateChanged,
			Class:  event.ClassState,
			Data:   json.RawMessage(`{"bytes":184320}`),
		})
		require.NoError(t, err)

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, sub)
		require.Len(t, batch.Events, 1)

		var env event.Envelope
		require.NoError(t, json.Unmarshal(batch.Events[0].Bytes, &env))
		require.Equal(t, "urn:prusa:site:01K2A0SPRAGUEFARM07/camera/front", env.Source)
	})
}

// TestVisibilityIsNotWidenedByKind is the refusal half of the new addressing.
// Carrying more kinds on the bus must not turn a principal's printer grant into
// a licence to read a storage instance or a camera, and a principal granted
// nothing must stay a principal granted nothing.
func TestVisibilityIsNotWidenedByKind(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{
			Authorizer: staticAuthorizer{visible: map[string][]event.Origin{alice: {printerA}}},
		})

		defer hub.Close()

		// nemo is permitted nothing, and a bus carrying node and camera events
		// does not change that: his stream carries none of them.
		empty, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: nemo})
		require.NoError(t, err)

		defer empty.Close()

		require.Empty(t, empty.Origins())

		// alice is permitted one printer, and asking for a storage instance is
		// refused in the same words as asking for a printer she may not see.
		_, err = hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Origins:   []event.Origin{s3Main},
		})
		require.ErrorIs(t, err, ErrNotPermitted)

		_, printerErr := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Origins:   []event.Origin{printerB},
		})
		require.Equal(t, err.Error(), printerErr.Error(),
			"a kind she may not see is refused in the same words as a printer she may not see")

		// And her unnarrowed subscription carries printers alone, however much
		// else the hub is publishing.
		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		require.Equal(t, []event.Origin{printerA}, sub.Origins())

		for _, pub := range []Publication{degradedOf(s3Main, 1), stateOf(printerB, 1)} {
			_, err = hub.Publish(pub)
			require.NoError(t, err)
		}

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		require.Empty(t, sub.Batches(), "a resource alice may not see reached her stream")
		require.Empty(t, empty.Batches(), "a principal permitted nothing was given something")
	})
}

// TestAForeignGenerationDoesNotInvalidateThisOne is the scoping rule seen from
// the hub: a reader following several nodes hands back one cursor, and this hub
// reads the generation it minted and ignores the rest. A sibling node
// recreating its log costs this reader nothing here.
func TestAForeignGenerationDoesNotInvalidateThisOne(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		for i := range 3 {
			_, err := hub.Publish(stateOf(printerA, i))
			require.NoError(t, err)
		}

		// What a parent holds: a place in this hub, and a place in a sibling
		// node whose log was recreated a moment ago.
		spanning := event.NewCursor(
			event.NewGeneration(hub.Epoch(), []event.Position{{Origin: printerA, Offset: 1}}),
			event.NewGeneration("01SIBLINGNODEGENERATION000", []event.Position{{Origin: printerB, Offset: 4200}}),
		)

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Resume:    spanning,
		})
		require.NoError(t, err)

		defer sub.Close()

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, sub)
		require.False(t, batch.Resync,
			"a sibling node's generation in the cursor cost this reader its own places")
		require.Equal(t, []uint64{2, 3}, offsets(batch.Events),
			"the replay resumed from this hub's own generation")

		// What the hub hands back names its own generation and nothing else: a
		// hub does not get to speak for a log it did not write.
		require.Len(t, batch.Cursor.Generations, 1)
		require.Equal(t, hub.Epoch(), batch.Cursor.Generations[0].Epoch)
	})
}

// sseData pulls the JSON out of an SSE frame, which is where a browser reads it
// from.
func sseData(t *testing.T, frame []byte) []byte {
	t.Helper()

	for line := range strings.SplitSeq(string(frame), sseNewline) {
		if data, found := strings.CutPrefix(line, sseDataField); found {
			return []byte(data)
		}
	}

	t.Fatalf("no %q field in frame %q", sseDataField, frame)

	return nil
}
