package stream

import (
	"context"
	"strconv"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/event"
)

// TestTelemetryOfADeviceThatArrivedAfterTheSubscribeReachesTheReader is the
// order a printer really arrives in, read from the telemetry side.
//
// The grant that makes the machine visible is written first and the machine
// starts reporting second, and a reader open before either is attached to the
// new origin only on its next authorization check. Telemetry has no replay
// window, so everything the machine reported in between reached nobody and the
// reader showed a printer with no readings until it reconnected.
func TestTelemetryOfADeviceThatArrivedAfterTheSubscribeReachesTheReader(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := newRevocableAuthorizer(map[string][]event.Origin{alice: {printerA}})
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		authz.grant(printerA, printerB)

		_, err = hub.Publish(telemetryOf(printerB, "/temp/nozzle", 210))
		require.NoError(t, err)

		time.Sleep(2 * AuthorizeInterval)
		synctest.Wait()

		require.ElementsMatch(t, []event.Origin{printerA, printerB}, sub.Origins())

		batch := receive(t, sub)
		require.Len(t, batch.Telemetry, 1, "the readings of a printer that dialled in reached nobody")
		require.Equal(t, printerB, batch.Telemetry[0].Position.Origin)
		require.Equal(t, 210, payloadValue(t, batch.Telemetry[0]))
	})
}

// TestAGrantWrittenAfterTheSubscribeCarriesTheTelemetryWithIt is the same hole
// reached the other way round. The machine is already reporting and the reader
// is granted it afterwards, which is an operator giving somebody sight of a
// printer that is already running.
func TestAGrantWrittenAfterTheSubscribeCarriesTheTelemetryWithIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := newRevocableAuthorizer(map[string][]event.Origin{alice: {}})
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		require.Empty(t, sub.Origins())

		authz.grant(printerB)

		_, err = hub.Publish(telemetryOf(printerB, "/temp/nozzle", 215))
		require.NoError(t, err)

		time.Sleep(2 * AuthorizeInterval)
		synctest.Wait()

		batch := receive(t, sub)
		require.Len(t, batch.Telemetry, 1, "a grant on a printer that is already reporting carried no reading")
		require.Equal(t, 215, payloadValue(t, batch.Telemetry[0]))
	})
}

// TestOnlyTheNewestReadingOfAKeyReachesANewlyAttachedReader keeps the retained
// set what it says it is. The reader is shown the machine as it stands, not the
// run of readings it was not there for.
func TestOnlyTheNewestReadingOfAKeyReachesANewlyAttachedReader(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := newRevocableAuthorizer(map[string][]event.Origin{alice: {}})
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		authz.grant(printerB)

		for value := range 5 {
			_, err = hub.Publish(telemetryOf(printerB, "/temp/nozzle", value+1))
			require.NoError(t, err)
		}

		time.Sleep(2 * AuthorizeInterval)
		synctest.Wait()

		batch := receive(t, sub)
		require.Len(t, batch.Telemetry, 1)
		require.Equal(t, 5, payloadValue(t, batch.Telemetry[0]))
	})
}

// TestTelemetryPublishedBeforeAGrantIsNotHandedToTheReaderThatGotIt is the
// security half, and it is the rule the durable replay already holds to: a
// grant is permission to read from now. A reading taken before the grant was
// written is not handed over because the machine happened to still be holding
// it.
func TestTelemetryPublishedBeforeAGrantIsNotHandedToTheReaderThatGotIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := newRevocableAuthorizer(map[string][]event.Origin{alice: {}})
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		for value := range 5 {
			_, err = hub.Publish(telemetryOf(printerB, "/temp/nozzle", value+1))
			require.NoError(t, err)
		}

		authz.grant(printerB)

		time.Sleep(2 * AuthorizeInterval)
		synctest.Wait()

		require.Equal(t, []event.Origin{printerB}, sub.Origins())
		require.Empty(t, sub.Batches(), "a reading taken before the grant was handed to the reader")
	})
}

// TestAReaderWhoLostAGrantIsNotGivenTheReadingsItLeftBehind checks the other
// direction of the same rule. A withdrawn origin's retained values must not
// come back on the next authorization pass.
func TestAReaderWhoLostAGrantIsNotGivenTheReadingsItLeftBehind(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := newRevocableAuthorizer(map[string][]event.Origin{alice: {printerA, printerB}})
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		_, err = hub.Publish(telemetryOf(printerB, "/temp/nozzle", 220))
		require.NoError(t, err)

		authz.grant(printerA)

		time.Sleep(2 * AuthorizeInterval)
		synctest.Wait()

		require.Equal(t, []event.Origin{printerA}, sub.Origins())

		for {
			select {
			case batch, open := <-sub.Batches():
				require.True(t, open)

				for _, frame := range batch.Telemetry {
					require.NotEqual(t, printerB, frame.Position.Origin,
						"a withdrawn printer's readings were handed over")
				}
			default:
				return
			}
		}
	})
}

// TestALosslessReaderIsGivenNoRetainedReading keeps the series lane a series. A
// history store is asked later what the readings were, and one value handed to
// it because a grant moved would be a reading it records twice.
func TestALosslessReaderIsGivenNoRetainedReading(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authz := newRevocableAuthorizer(map[string][]event.Origin{alice: {}})
		hub := newTestHub(t, Options{Authorizer: authz})

		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Lossless:  true,
		})
		require.NoError(t, err)

		defer sub.Close()

		authz.grant(printerB)

		_, err = hub.Publish(telemetryOf(printerB, "/temp/nozzle", 205))
		require.NoError(t, err)

		time.Sleep(2 * AuthorizeInterval)
		synctest.Wait()

		require.Equal(t, []event.Origin{printerB}, sub.Origins())
		require.Empty(t, sub.Batches(), "a lossless reader was handed a retained reading")
	})
}

// TestOneOriginKeepsAtMostTheBoundOfTelemetryKeys is what stops the retained
// set growing with whatever a driver reports. Past the bound a new key is not
// kept and the ones already held keep moving.
func TestOneOriginKeepsAtMostTheBoundOfTelemetryKeys(t *testing.T) {
	hub := newTestHub(t, Options{})

	defer hub.Close()

	for key := range maxRetainedKeys + 10 {
		_, err := hub.Publish(telemetryOf(printerA, "/k/"+strconv.Itoa(key), key))
		require.NoError(t, err)
	}

	ch := hub.channel(printerA)
	require.Equal(t, maxRetainedKeys, ch.last.count())
	require.Len(t, ch.last.since(0), maxRetainedKeys)
}
