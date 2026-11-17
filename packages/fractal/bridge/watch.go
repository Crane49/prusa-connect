package bridge

import (
	"context"
	"encoding/json"

	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// TelemetrySampled is the type of the event the host publishes for one
// telemetry reading: the value at one path of the state tree of one device.
const TelemetrySampled = "com.prusa.connect.device.telemetry.sampled"

// sampleData is the data of one TelemetrySampled event: the path in the state
// tree, and the value as the device encoded it.
type sampleData struct {
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
}

// Window handles one batch of the host's event stream. It stores each
// telemetry reading for the next Flush. For any other event about a device it
// reads that device again. It always returns nil.
func (b *Bridge) Window(ctx context.Context, window busapi.Window) error {
	if window.Resync {
		// The subscription missed events, and any device may have changed.
		if err := b.Sync(ctx); err != nil {
			b.log.WarnContext(ctx, "the device listing could not be read after a resync",
				"error", err.Error())
		}
	}

	for _, encoded := range window.Telemetry {
		b.telemetry(ctx, encoded)
	}

	b.durable(ctx, window.Events)

	return nil
}

// telemetry stores the reading of one event envelope. It drops an envelope
// it cannot decode, one of another type, and one with no device or no path.
func (b *Bridge) telemetry(ctx context.Context, encoded json.RawMessage) {
	var envelope event.Envelope
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		return
	}

	device, named := deviceOf(envelope.Subject)
	if envelope.Type != TelemetrySampled || !named {
		return
	}

	var reading sampleData
	if err := json.Unmarshal(envelope.Data, &reading); err != nil || reading.Path == "" {
		return
	}

	b.Sample(ctx, device, reading.Path, reading.Value, envelope.Time)
}

// deviceOf reads the device id out of the subject the host gives an event
// about a device, device:<id>. It reports false for any other subject.
func deviceOf(subject string) (string, bool) {
	origin, err := event.ParseOrigin(subject)
	if err != nil || origin.Kind() != event.KindDevice {
		return "", false
	}

	return origin.ID(), true
}

// durable reads again, once, every device an event names as its subject. It
// also releases the copy of a print whose transfer settled.
func (b *Bridge) durable(ctx context.Context, events []json.RawMessage) {
	var named map[string]struct{}

	for _, encoded := range events {
		var envelope event.Envelope
		if err := json.Unmarshal(encoded, &envelope); err != nil {
			continue
		}

		if envelope.Type == event.TypeDeviceTransferSettled {
			b.settle(ctx, envelope.Data)
		}

		device, isDevice := deviceOf(envelope.Subject)
		if !isDevice {
			continue
		}

		if named == nil {
			named = make(map[string]struct{}, len(events))
		}

		named[device] = struct{}{}
	}

	for id := range named {
		b.Touch(ctx, id)
	}
}
