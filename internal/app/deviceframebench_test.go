package app

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// What one printer frame costs the process before any window closes.
//
// This is the path an earlier design decision and the hot-path rules are about: a driver hands
// the plane a patch, the plane records it and publishes it, and the hub queues
// it for every subscriber. Nothing here waits on a database and nothing here
// waits on a reader.
//
// The fleet arithmetic is what makes the per-patch figure worth having. One
// xBuddy frame carries fifteen paths and the dialect runs at 1 to 5 Hz, so four
// hundred printers put between 6,000 and 30,000 patches a second on this path.
//
//	go test -run '^$' -bench BenchmarkPrinterFrame -benchtime 2000x ./internal/app/

// benchFrameReaders is the reader count one frame is priced at: nobody
// watching, one browser tab, and a small operations room.
var benchFrameReaders = []int{0, 1, 20}

// BenchmarkPrinterFrame prices one whole frame, which is what
// devices.pump does for every patch a driver reports.
//
// It reports the per-patch cost as well, because that is the number the fleet
// rate multiplies.
func BenchmarkPrinterFrame(b *testing.B) {
	for _, readers := range benchFrameReaders {
		b.Run(fmt.Sprintf("readers=%d", readers), func(b *testing.B) {
			plane, hub := benchFramePlane(b, readers)
			defer hub.Close()

			entry := &device{id: benchDeviceID(0), state: map[string]json.RawMessage{}}
			frame := benchTelemetryFrame()
			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				for _, patch := range frame {
					entry.record(patch)
					plane.publish(ctx, entry.id, event.TypeDeviceTelemetrySampled,
						event.ClassTelemetry, patch.Path, sampleOf(patch))
				}
			}

			b.StopTimer()
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*benchStatePaths), "ns/patch")
		})
	}
}

// benchTelemetryFrame is one xBuddy frame as the driver reports it, fifteen paths each
// carrying the value the printer encoded.
func benchTelemetryFrame() []connectorapi.StatePatch {
	out := make([]connectorapi.StatePatch, 0, benchStatePaths)

	for path := range benchStatePaths {
		out = append(out, connectorapi.StatePatch{
			Path:  fmt.Sprintf("/telemetry/path%02d", path),
			Value: json.RawMessage(fmt.Sprintf("%d.25", 100+path)),
		})
	}

	return out
}

// benchFramePlane is a device plane over a real hub with a chosen number of
// readers draining. Zero readers is the floor: the publish still mints an id,
// builds an envelope and marshals it, and only the fan-out is absent.
//
// It holds no store and runs no flush goroutine, so nothing here can reach a
// database by accident.
func benchFramePlane(b *testing.B, readers int) (*devices, *stream.Hub) {
	b.Helper()

	origins := make([]event.Origin, 0, benchFleet)

	for i := range benchFleet {
		origin, err := event.NewOrigin(event.KindDevice, benchDeviceID(i))
		require.NoError(b, err)

		origins = append(origins, origin)
	}

	hub, err := stream.New(stream.Options{
		Site:       "local",
		Authorizer: everyOrigin{origins: origins},
		Logger:     discard(),
		Interval:   stream.MinInterval,
	})
	require.NoError(b, err)

	for reader := range readers {
		sub, subErr := hub.Subscribe(b.Context(), stream.SubscribeRequest{
			Principal: fmt.Sprintf("user:reader%d", reader),
		})
		require.NoError(b, subErr)

		go func() {
			for range sub.Batches() { // the reader exists to drain
			}
		}()

		b.Cleanup(sub.Close)
	}

	return &devices{hub: hub, log: discard()}, hub
}
