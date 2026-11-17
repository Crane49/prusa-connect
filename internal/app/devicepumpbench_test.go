package app

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// What one telemetry patch costs the pump: the bound, the write into the state
// tree and the publish, over a real hub with a real subscriber attached.
//
// The two cases are the two ways the event can be built. "as-sent" is what the
// pump did before the bound reached the stream: it publishes the driver's own
// bytes, so the bound is paid once and the publish reads a value nothing
// checked. "as-retained" is what it does now: the value the tree kept is what
// the event carries, so the bound is still paid once and no second marshal and
// no second copy is added.
//
//	go test -run '^$' -bench 'BenchmarkPumpPatch' ./internal/app/
func BenchmarkPumpPatch(b *testing.B) {
	cases := []struct {
		name  string
		build func(entry *device, patch connectorapi.StatePatch) (connectorapi.StatePatch, bool)
	}{
		{
			name: "as-sent",
			build: func(entry *device, patch connectorapi.StatePatch) (connectorapi.StatePatch, bool) {
				entry.record(patch)

				return patch, true
			},
		},
		{
			name:  "as-retained",
			build: (*device).record,
		},
	}

	for _, one := range cases {
		b.Run(one.name, func(b *testing.B) {
			plane, hub := benchPlane(b)
			defer hub.Close()

			entry := &device{
				id:     benchDeviceID(0),
				state:  map[string]json.RawMessage{},
				budget: defaultTreeBudget,
			}

			patches := make([]connectorapi.StatePatch, 0, benchStatePaths)
			for path := range benchStatePaths {
				patches = append(patches, connectorapi.StatePatch{
					Path:  fmt.Sprintf("/telemetry/path%02d", path),
					Value: json.RawMessage(fmt.Sprintf("%d.25", 100+path)),
				})
			}

			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				for _, patch := range patches {
					held, kept := one.build(entry, patch)
					if !kept {
						continue
					}

					plane.publish(ctx, entry.id, event.TypeDeviceTelemetrySampled,
						event.ClassTelemetry, held.Path, sampleOf(held))
				}
			}
		})
	}
}
