package app

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/kernel/machinetext"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// The host bounds what one machine may put in its state tree. The event
// carrying the same value went out as the driver sent it, so a machine
// reporting a value larger than the tree keeps sent that value to every
// subscriber of the stream, and to every sink behind it.
func TestATelemetryEventCarriesWhatTheHostRetainedAndNoMore(t *testing.T) {
	t.Parallel()

	plane, hub := planeAndHub(t)

	host := plane.Arrivals("test", talkingBaseline(),
		capability.NewResolver(profile.Registry{}, discard()), "")

	session := &countedSession{
		identity: connectorapi.Identity{Vendor: "Prusa", Model: "MK4", Serial: "SIM-WIDE-VALUE"},
		patches:  make(chan connectorapi.StatePatch),
	}

	_, err := host.Admit(context.WithoutCancel(t.Context()), session.identity)
	require.NoError(t, err)
	require.NoError(t, host.Arrived(context.WithoutCancel(t.Context()), session))

	// After the arrival: a subscription takes the origins it may see when it is
	// opened, and the telemetry lane is delivered on that table alone.
	samples := readTelemetry(t, hub)

	wide, err := json.Marshal(strings.Repeat("x", machinetext.MaxBytes*2))
	require.NoError(t, err)

	session.patches <- connectorapi.StatePatch{Path: "/telemetry/note", Value: wide}

	// A refused path: not a string, and over the bound, so the host keeps
	// nothing of it and has nothing to publish either.
	refused := json.RawMessage("[" + strings.TrimSuffix(strings.Repeat("1,", machinetext.MaxBytes), ",") + "]")
	session.patches <- connectorapi.StatePatch{Path: "/telemetry/refused", Value: refused}

	// A patch after both, so the assertions run on a stream that has carried
	// everything the session sent.
	session.patches <- connectorapi.StatePatch{Path: "/telemetry/nozzle", Value: json.RawMessage("215.5")}

	close(session.patches)

	published := samples.await(t, 2)

	paths := map[string]bool{}

	for _, one := range published {
		require.LessOrEqual(t, len(one.Value), machinetext.MaxBytes+len(machinetext.Marker)+2,
			"the stream carried "+strconv.Itoa(len(one.Value))+" bytes for "+one.Path+
				", which is more than the host retained")

		paths[one.Path] = true
	}

	require.True(t, paths["/telemetry/nozzle"], "the reading after the wide ones never reached the stream")
	require.False(t, paths["/telemetry/refused"],
		"a value the host refused to store was published to every subscriber anyway")
}

// planeAndHub is planeAndBus for a test that reads the telemetry lane itself.
// A telemetry frame arrives in Batch.Telemetry, which busReader does not read.
func planeAndHub(t *testing.T) (*devices, *stream.Hub) {
	t.Helper()

	graph := newGraph(t)

	hub, err := stream.New(stream.Options{
		Site: "local", Authorizer: visibility{graph: graph}, Logger: discard(),
		Interval: 5 * time.Millisecond,
	})
	require.NoError(t, err)

	t.Cleanup(hub.Close)

	return newPlaneOn(t, graph, hub), hub
}

// telemetryReader is every telemetry sample one subscription has carried.
type telemetryReader struct {
	mu   sync.Mutex
	seen []sample
}

// readTelemetry subscribes as the deployment's own principal and collects the
// telemetry lane, which is the lane an oversized value would reach a sink on.
func readTelemetry(t *testing.T, hub *stream.Hub) *telemetryReader {
	t.Helper()

	sub, err := hub.Subscribe(context.WithoutCancel(t.Context()), stream.SubscribeRequest{
		Principal: "user:" + config.DefaultPrincipal,
		Encoding:  stream.EncodingJSON,
		Lossless:  true,
	})
	require.NoError(t, err)

	t.Cleanup(sub.Close)

	reader := &telemetryReader{}

	go func() {
		for batch := range sub.Batches() {
			reader.mu.Lock()

			for _, frame := range batch.Telemetry {
				var envelope event.Envelope
				if json.Unmarshal(frame.Bytes, &envelope) != nil {
					continue
				}

				var one sample
				if json.Unmarshal(envelope.Data, &one) == nil {
					reader.seen = append(reader.seen, one)
				}
			}

			reader.mu.Unlock()
		}
	}()

	return reader
}

// await waits for at least want samples and returns everything seen.
func (r *telemetryReader) await(t *testing.T, want int) []sample {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	for {
		r.mu.Lock()
		out := slices.Clone(r.seen)
		r.mu.Unlock()

		if len(out) >= want {
			return out
		}

		if time.Now().After(deadline) {
			t.Fatalf("the stream carried %d telemetry samples, want at least %d", len(out), want)
		}

		time.Sleep(10 * time.Millisecond)
	}
}
