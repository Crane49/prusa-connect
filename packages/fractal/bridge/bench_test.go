package bridge_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/prusa3d/fractal-link/bridge"
	"github.com/prusa3d/fractal-link/interfaces"

	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
)

// The bridge has to run on a small computer such as a Raspberry Pi with a farm
// of printers. These benchmarks measure the cost of storing one telemetry
// reading and the cost of one flush over the whole farm.

// farmSize is the number of printers in the benchmarks.
const farmSize = 400

// farm builds one bridge with farmSize printers and returns their identifiers.
func farm(b *testing.B) (*bridge.Bridge, []string) {
	b.Helper()

	devices := make([]deviceapi.Device, 0, farmSize)
	ids := make([]string, 0, farmSize)

	for i := range farmSize {
		id := fmt.Sprintf("01K3Q7X8N4B2VJ5R0YH6M%05d", i)
		ids = append(ids, id)
		devices = append(devices, printer(id, telemetry()))
	}

	fleet := newFleet(devices...)

	made, err := bridge.New(bridge.Config{
		Fleet:   fleet,
		Node:    newNode(),
		Mapping: benchMapping(b),
		Now:     at,
	})
	if err != nil {
		b.Fatalf("build the bridge: %v", err)
	}

	if err = made.Sync(context.Background()); err != nil {
		b.Fatalf("read the devices: %v", err)
	}

	return made, ids
}

// benchMapping loads mapping.json for a benchmark.
func benchMapping(b *testing.B) *bridge.Mapping {
	b.Helper()

	registry, err := interfaces.Registry()
	if err != nil {
		b.Fatalf("read the interface definitions: %v", err)
	}

	built, err := bridge.LoadMapping(registry)
	if err != nil {
		b.Fatalf("read the mapping table: %v", err)
	}

	return built
}

// BenchmarkSample measures Sample for a path the mapping table reads. A
// printer sends several readings a second, so a farm calls Sample very often.
func BenchmarkSample(b *testing.B) {
	made, ids := farm(b)
	value := json.RawMessage(`214.8`)
	when := at()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; b.Loop(); i++ {
		made.Sample(b.Context(), ids[i%farmSize], "/tools/0/nozzle_temp", value, when)
	}
}

// BenchmarkSampleOfAPathNothingReads measures Sample for a path that no
// property reads. Most readings a printer sends are of this kind, and Sample
// drops them.
func BenchmarkSampleOfAPathNothingReads(b *testing.B) {
	made, ids := farm(b)
	value := json.RawMessage(`"Insert filament"`)
	when := at()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; b.Loop(); i++ {
		made.Sample(b.Context(), ids[i%farmSize], "/dialog/title", value, when)
	}
}

// BenchmarkFlushOneFarm measures one Flush after every printer of the farm
// reported a new nozzle temperature.
func BenchmarkFlushOneFarm(b *testing.B) {
	made, ids := farm(b)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; b.Loop(); i++ {
		for n, id := range ids {
			made.Sample(ctx, id, "/tools/0/nozzle_temp",
				json.RawMessage(fmt.Sprintf("%d.5", 200+(i+n)%50)), at())
		}

		made.Flush(ctx)
	}
}

// BenchmarkSnapshot measures Snapshot, the full state of one interface. The
// node above asks for it once per interface of every printer.
func BenchmarkSnapshot(b *testing.B) {
	made, ids := farm(b)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; b.Loop(); i++ {
		if _, _, err := made.Snapshot(ids[i%farmSize], "FDMPrinter1"); err != nil {
			b.Fatalf("Snapshot of FDMPrinter1: %v", err)
		}
	}
}
