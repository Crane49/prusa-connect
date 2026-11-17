package histstore_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

// What a fleet-sized flush costs, on each engine that is reachable.
//
// The comparison these numbers are read against is the device record's own
// flush, which is 7.1 to 8.9 ms on SQLite and 26 to 29 ms on PostgreSQL per two
// second cycle for 400 devices. A history flush is on the same cadence, so the
// question an operator has is what the two cost together.
//
// A design requirement: the figure quoted anywhere about this store has a program in
// the repository behind it.

// fleetDevices and fleetMetrics are the fleet earlier research sizes everything
// against: 400 printers reporting six paths.
const (
	fleetDevices = 400
	fleetMetrics = 6
)

// forEachEngineBench runs body as a sub-benchmark per reachable engine.
func forEachEngineBench(b *testing.B, body func(b *testing.B, e engine)) {
	b.Helper()

	for _, e := range engines(b) {
		b.Run(e.Name, func(b *testing.B) { body(b, e) })
	}
}

// fleetSamples is one flush of a fleet reporting once: 2,400 rows over 2,400
// series, which is the widest a single cycle gets at this size.
func fleetSamples(at time.Time) []historyapi.Sample {
	samples := make([]historyapi.Sample, 0, fleetDevices*fleetMetrics)

	paths := [fleetMetrics]string{
		"/tools/0/nozzle_temp", "/temperature/bed", "/temperature/chamber",
		"/position/z", "/job/progress", "/fan/hotend",
	}

	for device := range fleetDevices {
		origin, err := event.NewOrigin(event.KindDevice, "mk4s-"+padded(device))
		if err != nil {
			panic(err)
		}

		for metric := range fleetMetrics {
			samples = append(samples, historyapi.Sample{
				At: at, Ref: origin, Metric: paths[metric], Value: float64(device*fleetMetrics + metric),
			})
		}
	}

	return samples
}

// padded renders a device number as a DNS-1123 label segment.
func padded(n int) string {
	digits := []byte{byte('0' + n/100%10), byte('0' + n/10%10), byte('0' + n%10)}

	return string(digits)
}

// BenchmarkAppendOneFleetFlush is one cycle of 400 printers by six metrics. The
// first iteration interns 2,400 series and every one after it reads the cache,
// which is the steady state a running deployment is in.
func BenchmarkAppendOneFleetFlush(b *testing.B) {
	forEachEngineBench(b, func(b *testing.B, e engine) {
		store := newStore(b, e)
		at := time.Date(2026, time.August, 28, 14, 0, 0, 0, time.UTC)

		// Outside the timer: the dictionary write is once per series for the
		// life of the process, and measuring it inside would report a cost a
		// deployment pays on its first cycle and never again.
		if err := store.AppendSamples(b.Context(), fleetSamples(at)); err != nil {
			b.Fatal(err)
		}

		b.ResetTimer()

		for i := 0; b.Loop(); i++ {
			if err := store.AppendSamples(b.Context(),
				fleetSamples(at.Add(time.Duration(i+1)*time.Second))); err != nil {
				b.Fatal(err)
			}
		}

		b.ReportMetric(float64(fleetDevices*fleetMetrics), "rows/flush")
		b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*fleetDevices*fleetMetrics), "ns/row")
	})
}

// BenchmarkAppendFactsOfOneFlush is the other half. A fleet produces far fewer
// facts than samples, so this is sized at one record change per device, which is
// the worst a flush reaches when every machine moved at once.
func BenchmarkAppendFactsOfOneFlush(b *testing.B) {
	payload := json.RawMessage(`{"device_id":"mk4s-000","connected":true,"changed":["firmware"]}`)

	forEachEngineBench(b, func(b *testing.B, e engine) {
		store := newStore(b, e)
		at := time.Date(2026, time.August, 28, 14, 0, 0, 0, time.UTC)

		facts := make([]historyapi.Fact, 0, fleetDevices)

		for device := range fleetDevices {
			origin, err := event.NewOrigin(event.KindDevice, "mk4s-"+padded(device))
			if err != nil {
				b.Fatal(err)
			}

			facts = append(facts, historyapi.Fact{
				At: at, Ref: origin, Type: event.TypeDeviceRecordChanged,
				Epoch: "epoch-a", Payload: payload,
			})
		}

		b.ResetTimer()

		for i := 0; b.Loop(); i++ {
			for j := range facts {
				facts[j].Sequence = uint64(i + 1)
			}

			if err := store.AppendFacts(b.Context(), facts); err != nil {
				b.Fatal(err)
			}
		}

		b.ReportMetric(float64(fleetDevices), "rows/flush")
		b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*fleetDevices), "ns/row")
	})
}
