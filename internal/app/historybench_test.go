package app

import (
	"strconv"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

// What the feed costs per frame, which is the half of the price that is not the
// database.
//
// It is not the frame path. The hub marshals an envelope once and every
// subscriber shares the bytes, and this reads them on the feed's own goroutine,
// downstream of the queue that already bounds it. What it answers is how much
// CPU a fleet's window costs the process before a row reaches a disk, so the
// measured flush cost in internal/platform/history is not the whole figure.

// benchDevices and benchMetrics are the fleet earlier research sizes everything
// against: 400 printers reporting six paths.
const (
	benchDevices = 400
	benchMetrics = 6
)

// benchTelemetryWindow is one hub window of a fleet reporting once, as the
// frames a subscriber is handed.
func benchTelemetryWindow(b *testing.B) []stream.Frame {
	b.Helper()

	paths := [benchMetrics]string{
		"/tools/0/nozzle_temp", "/temperature/bed", "/temperature/chamber",
		"/position/z", "/job/progress", "/fan/hotend",
	}

	frames := make([]stream.Frame, 0, benchDevices*benchMetrics)

	for device := range benchDevices {
		id := "mk4s-" + strconv.Itoa(device)

		origin, err := event.NewOrigin(event.KindDevice, id)
		if err != nil {
			b.Fatal(err)
		}

		for metric := range benchMetrics {
			body := `{"specversion":"1.0","id":"01J000000000000000000000",` +
				`"source":"urn:prusa:site:local/printer/` + id + `",` +
				`"type":"` + event.TypeDeviceTelemetrySampled + `",` +
				`"time":"2026-08-28T14:00:00Z","subject":"device:` + id + `",` +
				`"sequence":"00000000000000000042","epoch":"epoch-a","hops":0,` +
				`"evclass":"telemetry","data":{"path":"` + paths[metric] + `","value":214.5}}`

			frames = append(frames, stream.Frame{
				Position: event.Position{Origin: origin, Offset: uint64(metric + 1)},
				Class:    event.ClassTelemetry,
				Type:     event.TypeDeviceTelemetrySampled,
				Bytes:    []byte(body),
			})
		}
	}

	return frames
}

// BenchmarkHistoryCollectsOneFleetWindow is what the feed spends turning one
// window of 400 printers by six metrics into rows.
func BenchmarkHistoryCollectsOneFleetWindow(b *testing.B) {
	feed := &historyFeed{}
	feeding := &historyStream{
		caps:     historyapi.CapSamples | historyapi.CapFacts,
		consumes: []event.Class{event.ClassTelemetry, event.ClassState, event.ClassAudit},
	}
	batch := stream.Batch{Telemetry: benchTelemetryWindow(b)}
	pending := historyBatch{}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		pending.samples = pending.samples[:0]
		pending.facts = pending.facts[:0]

		feed.collect(feeding, batch, &pending)
	}

	b.StopTimer()

	if len(pending.samples) != benchDevices*benchMetrics {
		b.Fatalf("the window produced %d samples and holds %d frames",
			len(pending.samples), benchDevices*benchMetrics)
	}

	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*benchDevices*benchMetrics), "ns/frame")
}
