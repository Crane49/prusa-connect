package app

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/devicestore"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// What the record frame adds to a flush.
//
// Earlier research priced the write itself: four hundred devices, eighty per
// statement, one transaction, 7.1 to 8.9 ms on SQLite and 26.1 to 29.3 ms on
// Postgres per two-second cycle. Publishing sits on top of that, so what is
// measured here is only what was added, in the two states a fleet is in.
//
// The quiet case is the steady state and it is the one that matters: four
// hundred machines reporting telemetry mark every entry for the store and move
// no field a frame reports, so the flush compares four hundred records and
// publishes nothing. The moved case is the worst one a real fleet reaches: a
// switch reboots and four hundred devices change presence inside one window.
//
//	go test -run '^$' -bench 'BenchmarkRecord' -benchtime 2000x ./internal/app/

// benchFleet is the fleet this product claims, and the number earlier research's
// flush figures are quoted at.
const benchFleet = 400

// benchStatePaths is how many paths one xBuddy frame carries.
const benchStatePaths = 15

// BenchmarkRecordFieldsMoved is the comparison one device costs on every flush,
// whether or not anything moved. It is the whole cost a flush that publishes
// nothing pays for publishing.
func BenchmarkRecordFieldsMoved(b *testing.B) {
	record := benchRecord(0)
	stated := factsOf(record, true)

	var moved recordFields

	for b.Loop() {
		moved |= factsOf(record, true).movedFrom(stated)
	}

	require.Zero(b, moved, "the fixture moved, so this measured the wrong branch")
}

// BenchmarkRecordFlushAnnounce is the publish half, over a real hub with a real
// subscriber attached, for one whole fleet.
func BenchmarkRecordFlushAnnounce(b *testing.B) {
	cases := []struct {
		name  string
		moved recordFields
	}{
		// Every entry was written and no field a frame reports moved, which is
		// what a fleet reporting telemetry does every two seconds for ever.
		{name: "quiet", moved: 0},
		// A switch rebooted: every device's presence moved inside one window.
		{name: "presence-moved", moved: fieldConnected},
	}

	for _, one := range cases {
		b.Run(one.name, func(b *testing.B) {
			plane, hub := benchPlane(b)
			defer hub.Close()

			written := make([]changedRecord, 0, benchFleet)
			for i := range benchFleet {
				written = append(written, changedRecord{record: benchRecord(i), moved: one.moved})
			}

			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				plane.announce(ctx, written)
			}
		})
	}
}

// benchPlane is a device plane with nothing but a hub under it, because what is
// being measured is the publish and not the write. It runs no flush goroutine
// and holds no store, so nothing here can reach a database by accident.
func benchPlane(b *testing.B) (*devices, *stream.Hub) {
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

	// One reader attached, so the fan-out the publish pays for is real. A hub
	// with no subscriber drops every frame at the first branch and would price
	// the wrong thing.
	sub, err := hub.Subscribe(b.Context(), stream.SubscribeRequest{Principal: "user:reader"})
	require.NoError(b, err)

	go func() {
		for range sub.Batches() { // the reader exists to drain
		}
	}()

	b.Cleanup(sub.Close)

	return &devices{hub: hub, log: discard()}, hub
}

// everyOrigin is a stream.Authorizer that permits the whole benchmark fleet.
type everyOrigin struct {
	origins []event.Origin
}

func (e everyOrigin) VisibleOrigins(context.Context, string) (stream.Visibility, error) {
	origins := make([]stream.Grant, 0, len(e.origins))
	for _, origin := range e.origins {
		origins = append(origins, stream.Grant{Origin: origin})
	}

	return stream.Visibility{Origins: origins, Epoch: 1}, nil
}

func (e everyOrigin) Epoch() uint64 { return 1 }

// benchRecord is one device's stored row, carrying a full xBuddy frame.
func benchRecord(index int) devicestore.DeviceRecord {
	state := make(map[string]json.RawMessage, benchStatePaths)
	for path := range benchStatePaths {
		state[fmt.Sprintf("/telemetry/path%02d", path)] = json.RawMessage(fmt.Sprintf("%d.25", 100+path))
	}

	return devicestore.DeviceRecord{
		DeviceID: benchDeviceID(index),
		Driver:   "connector:xbuddy",
		Vendor:   "Prusa",
		Model:    "MK4",
		Serial:   fmt.Sprintf("SN%06d", index),
		UnitUID:  fmt.Sprintf("UID%06d", index),
		Firmware: "6.4.0-0",
		LastSeen: time.Now().UTC(),
		State:    state,
	}
}

// benchDeviceID is a device id of the shape the plane mints: the canonical
// lowercase form of a UUID. The index goes in the last field so
// that a benchmark builds a distinct id per device and reads the same one twice.
func benchDeviceID(index int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012x", index)
}
