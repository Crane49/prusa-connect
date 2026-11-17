package devicestore

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
)

// What an upsert costs, on both engines.
//
// Earlier research priced this on SQLite alone and its gaps say so in as many words:
// a number from one engine is not evidence for the other, and no Postgres
// handle existed in the shipping binary to measure against. One does now, so
// these run on whichever engines the build reached and `make test-engines`
// reaches both.
//
// The question they answer is the one that decides the write path. A fleet is
// four hundred devices, a printer reports at 1 to 5 Hz, and a store that wrote
// on every telemetry frame would be a different product from one that writes on
// a transition. So the two cases are the two shapes the flush could take: one
// device in its own transaction, and every changed device in one.
//
//	go test -run '^$' -bench 'BenchmarkDeviceRecord' -benchtime 200x \
//	  -tags "sqlite postgres" ./internal/platform/devicestore/

// benchFrameKeys is how many paths one telemetry frame carries. It is the
// fifteen the xBuddy dialect maps, so the document being written is the size of
// a real one rather than of a fixture.
const benchFrameKeys = 15

// fleetDevices is the fleet this product claims.
const fleetDevices = 400

func fleetDeviceID(i int) string { return fmt.Sprintf("p%03d", i) }

// seedFleet is a store over a directory of fleetDevices devices.
func seedFleet(tb testing.TB, e engine) fixture {
	tb.Helper()

	store := newStore(tb, e)

	devices := make([]authz.DeviceRow, 0, fleetDevices)
	for i := range fleetDevices {
		devices = append(devices, authz.DeviceRow{ID: fleetDeviceID(i), Name: fmt.Sprintf("printer-%03d", i)})
	}

	require.NoError(tb, store.UpsertDevices(tb.Context(), devices...))

	return store
}

// benchFlushDevices is how many devices one flush writes. It is the fleet this
// product claims, which is the worst case: every machine changed inside one
// two-second window.
const benchFlushDevices = fleetDevices

// benchDocument is a state tree the size of one xBuddy frame.
func benchDocument() map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, benchFrameKeys)
	for i := range benchFrameKeys {
		out[fmt.Sprintf("/telemetry/path%02d", i)] = json.RawMessage(fmt.Sprintf("%d.25", 100+i))
	}

	return out
}

// benchRecords is one record per device, all carrying a full frame.
func benchRecords() []DeviceRecord {
	document := benchDocument()
	at := time.Now().UTC()

	out := make([]DeviceRecord, 0, benchFlushDevices)

	for i := range benchFlushDevices {
		out = append(out, DeviceRecord{
			DeviceID: fleetDeviceID(i),
			Driver:   "connector:xbuddy",
			Vendor:   "Prusa",
			Model:    "MK4",
			Serial:   fmt.Sprintf("SN-%03d", i),
			UnitUID:  fmt.Sprintf("uid-%03d", i),
			Firmware: "6.2.4",
			LastSeen: at,
			State:    document,
		})
	}

	return out
}

// BenchmarkDeviceRecordUpsertAlone is one device written in its own
// transaction, which is what the immediate write of a presence change or a
// departure costs. It is also the shape a store that wrote on every frame would
// pay, four hundred devices at a time.
func BenchmarkDeviceRecordUpsertAlone(b *testing.B) {
	for _, e := range benchEngines(b) {
		b.Run(e.Name, func(b *testing.B) {
			store := seedFleet(b, e)
			records := benchRecords()
			ctx := b.Context()

			b.ReportAllocs()
			b.ResetTimer()

			i := 0

			for b.Loop() {
				if err := store.SaveDeviceRecords(ctx, records[i%len(records)]); err != nil {
					require.NoError(b, err)
				}

				i++
			}
		})
	}
}

// BenchmarkDeviceRecordFlush is the flush the device plane runs: every device
// that changed since the last tick, in one transaction. The figure to read is
// the per-device one, which is the total divided by benchFlushDevices, and
// b.ReportMetric puts it in the output rather than leaving it to be worked out.
func BenchmarkDeviceRecordFlush(b *testing.B) {
	for _, e := range benchEngines(b) {
		b.Run(e.Name, func(b *testing.B) {
			store := seedFleet(b, e)
			records := benchRecords()
			ctx := b.Context()

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				if err := store.SaveDeviceRecords(ctx, records...); err != nil {
					require.NoError(b, err)
				}
			}

			b.StopTimer()

			b.ReportMetric(
				float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(benchFlushDevices)/1000,
				"us/device")
		})
	}
}

// BenchmarkUnfinishedDeviceRecords is what every start pays before it serves.
// It is measured because it is on the start-up path of a deployment that just
// crashed, which is the moment an operator is watching the log.
//
// It is a count and not a write. Earlier research holds no presence column, so a
// start reads how the last run ended off departure and writes no page before it
// serves.
func BenchmarkUnfinishedDeviceRecords(b *testing.B) {
	for _, e := range benchEngines(b) {
		b.Run(e.Name, func(b *testing.B) {
			store := seedFleet(b, e)
			ctx := b.Context()

			require.NoError(b, store.SaveDeviceRecords(ctx, benchRecords()...))

			b.ResetTimer()

			for b.Loop() {
				if _, err := store.UnfinishedDeviceRecords(ctx); err != nil {
					require.NoError(b, err)
				}
			}
		})
	}
}

// BenchmarkDeviceRecords is the read a restart makes: every record, once,
// before anything serves.
func BenchmarkDeviceRecords(b *testing.B) {
	for _, e := range benchEngines(b) {
		b.Run(e.Name, func(b *testing.B) {
			store := seedFleet(b, e)
			ctx := b.Context()

			require.NoError(b, store.SaveDeviceRecords(ctx, benchRecords()...))

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				records, err := store.DeviceRecords(ctx)
				if err != nil || len(records) != benchFlushDevices {
					require.NoError(b, err)
					require.Len(b, records, benchFlushDevices)
				}
			}
		})
	}
}
