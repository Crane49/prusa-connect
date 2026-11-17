package stream

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

// TestAStoreIsGivenEveryReadingAndABrowserOnlyTheNewest is the finding this
// lane closes: a store asked what a series was could only answer with one
// reading per coalescing window, because it was fed on the queue built for a
// screen. Both readers are on the same hub and the same window here, so the
// difference between them is the lane and nothing else.
func TestAStoreIsGivenEveryReadingAndABrowserOnlyTheNewest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		store, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice, Lossless: true})
		require.NoError(t, err)

		defer store.Close()

		browser, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer browser.Close()

		const readings = 20

		for value := range readings {
			_, err = hub.Publish(telemetryOf(printerA, "nozzle", value))
			require.NoError(t, err)
		}

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		kept := receive(t, store)
		require.False(t, kept.Resync, "nothing was dropped, so nothing should ask the store to resync")
		require.Len(t, kept.Telemetry, readings, "the store was given a sample of the series instead of the series")

		values := make([]int, 0, readings)
		for _, frame := range kept.Telemetry {
			values = append(values, payloadValue(t, frame))
		}

		require.Equal(t, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19}, values,
			"the readings did not arrive in the order they were reported")

		drawn := receive(t, browser)
		require.Len(t, drawn.Telemetry, 1, "a screen was given more than the value it would draw")
		require.Equal(t, readings-1, payloadValue(t, drawn.Telemetry[0]),
			"the value a screen draws is not the newest one")
	})
}

// TestAStoreTooFarBehindIsToldItsSeriesHasAGap is what the lane costs. The
// conflating queue is bounded by the distinct keys a subscriber can see and so
// no reader is slow enough to overflow it; this one is bounded by the lag. Past
// the bound the reader is told, because a run of readings quietly missing its
// middle is one nothing downstream can mark incomplete.
func TestAStoreTooFarBehindIsToldItsSeriesHasAGap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const limit = 1024

		hub := newTestHub(t, Options{TelemetryBytes: limit})
		defer hub.Close()

		store, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice, Lossless: true})
		require.NoError(t, err)

		defer store.Close()

		for value := range 200 {
			_, err = hub.Publish(telemetryOf(printerA, "nozzle", value))
			require.NoError(t, err)

			require.LessOrEqual(t, store.Stats().TelemetryBytes, limit,
				"the lane grew past the bound instead of discarding at it")
		}

		require.True(t, store.Stats().Resync, "the lane discarded readings without saying so")

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, store)
		require.True(t, batch.Resync, "the store was handed a shortened series with nothing to mark it incomplete")
		require.Less(t, len(batch.Telemetry), 200, "the bound did not discard anything, so this proves nothing")

		// The flag is per window, not sticky: the next window is whole again.
		_, err = hub.Publish(telemetryOf(printerA, "nozzle", 200))
		require.NoError(t, err)

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		next := receive(t, store)
		require.False(t, next.Resync, "a window that lost nothing still asked the store to resync")
		require.Len(t, next.Telemetry, 1)
	})
}
