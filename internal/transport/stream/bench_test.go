package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/event"
)

// benchmarkSubscribers is the fan-out width the proposal's load budget spans:
// one reader, a small team, a large site, and past anything expected.
var benchmarkSubscribers = []int{1, 10, 100, 1000}

// perSubscriberAllocs is what one subscriber may cost per event: its own frame
// slice and the frames in it. A marshal, an encode or a copy per subscriber
// costs several times this and fails the gate.
const perSubscriberAllocs = 2.0

// allocationTolerance absorbs the few allocations the measure cannot exclude.
// A per-subscriber regression is a whole allocation for each subscriber, so it
// cannot hide in this.
const allocationTolerance = 10.0

// allocationRounds is how many times each width is measured. The test keeps the
// lowest count, because testing.AllocsPerRun counts every allocation in the
// process and noise from other goroutines only ever adds to it.
const allocationRounds = 5

// fanOutHub attaches n subscribers to one printer. The coalescing window is far
// beyond any benchmark's runtime on purpose: the pump is rate-limited to at most
// ten batches a second by design, so the benchmark drives the drain itself and
// prices the work rather than the waiting.
func fanOutHub(tb testing.TB, subscribers int) (*Hub, []*Subscription) {
	tb.Helper()

	visible := make(map[string][]event.Origin, subscribers)
	for i := range subscribers {
		visible[strconv.Itoa(i)] = []event.Origin{printerA}
	}

	hub, err := New(Options{
		Site:           "01K2A0SPRAGUEFARM07",
		Authorizer:     staticAuthorizer{visible: visible},
		EventBytes:     1 << 30,
		HistoryEvents:  1 << 16,
		HistoryBytes:   1 << 30,
		TelemetryBytes: 1 << 20,
	})
	require.NoError(tb, err)

	subs := make([]*Subscription, 0, subscribers)

	for i := range subscribers {
		sub, subErr := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: strconv.Itoa(i),
			Interval:  time.Hour,
		})
		require.NoError(tb, subErr)

		subs = append(subs, sub)
	}

	return hub, subs
}

// drainAll runs the work a pump does when its window closes: encode, build
// frames, move the cursor. Without it a benchmark measures the queue and calls
// it the fan-out.
func drainAll(tb testing.TB, subs []*Subscription) {
	tb.Helper()

	for _, sub := range subs {
		if _, ready := sub.drain(); !ready {
			tb.Fatal("a subscriber had nothing to drain")
		}
	}
}

// BenchmarkFanOutTelemetry prices one printer frame all the way to every
// subscriber's frame: publish, conflate, encode once, build each reader's
// frames.
func BenchmarkFanOutTelemetry(b *testing.B) {
	for _, subscribers := range benchmarkSubscribers {
		b.Run(fmt.Sprintf("subscribers=%d", subscribers), func(b *testing.B) {
			hub, subs := fanOutHub(b, subscribers)
			defer hub.Close()

			pub := telemetryOf(printerA, "nozzle", 215)

			b.ReportAllocs()

			for b.Loop() {
				if _, err := hub.Publish(pub); err != nil {
					b.Fatal(err)
				}

				drainAll(b, subs)
			}
		})
	}
}

// BenchmarkFanOutEvent is the same for the durable lane, which also appends to
// the printer's replay window and moves every reader's cursor.
func BenchmarkFanOutEvent(b *testing.B) {
	for _, subscribers := range benchmarkSubscribers {
		b.Run(fmt.Sprintf("subscribers=%d", subscribers), func(b *testing.B) {
			hub, subs := fanOutHub(b, subscribers)
			defer hub.Close()

			pub := stateOf(printerA, 1)

			b.ReportAllocs()

			for b.Loop() {
				if _, err := hub.Publish(pub); err != nil {
					b.Fatal(err)
				}

				drainAll(b, subs)
			}
		})
	}
}

// BenchmarkIngest is the publish path alone: what a printer frame costs before
// any window closes, and the part that must not grow with subscriber count.
func BenchmarkIngest(b *testing.B) {
	for _, subscribers := range benchmarkSubscribers {
		b.Run(fmt.Sprintf("subscribers=%d", subscribers), func(b *testing.B) {
			hub, _ := fanOutHub(b, subscribers)
			defer hub.Close()

			pub := telemetryOf(printerA, "nozzle", 215)

			b.ReportAllocs()

			for b.Loop() {
				if _, err := hub.Publish(pub); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// fleetOrigins is the fleet size the product claims, and fleetMetrics is how
// many telemetry keys one printer reports per frame.
const (
	fleetOrigins = 400
	fleetMetrics = 6
)

// fleetSubscribers is the reader count one fleet-scale window is priced at.
var fleetSubscribers = []int{1, 20}

// fleetHub attaches subscribers to every origin of a four hundred printer site,
// which is the routing table an operator's browser tab is given. A
// telemetryBytes of zero takes the shipped default, which is what a gate on
// delivery has to measure.
func fleetHub(tb testing.TB, subscribers, telemetryBytes int) (*Hub, []*Subscription, []event.Origin) {
	tb.Helper()

	origins := make([]event.Origin, 0, fleetOrigins)
	for i := range fleetOrigins {
		origins = append(origins, originOf(event.KindDevice, fmt.Sprintf("01JB8XN2K7QW9ZR4MV3T6H%05d", i)))
	}

	visible := make(map[string][]event.Origin, subscribers)
	for i := range subscribers {
		visible[strconv.Itoa(i)] = origins
	}

	hub, err := New(Options{
		Site:           "01K2A0SPRAGUEFARM07",
		Authorizer:     staticAuthorizer{visible: visible},
		EventBytes:     1 << 30,
		HistoryEvents:  1 << 16,
		HistoryBytes:   1 << 30,
		TelemetryBytes: telemetryBytes,
	})
	require.NoError(tb, err)

	subs := make([]*Subscription, 0, subscribers)

	for i := range subscribers {
		sub, subErr := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: strconv.Itoa(i),
			Interval:  time.Hour,
		})
		require.NoError(tb, subErr)

		subs = append(subs, sub)
	}

	return hub, subs, origins
}

// BenchmarkFleetWindow prices one coalescing window of a four hundred printer
// site: every printer reports six metrics, then every subscriber's window
// closes. One iteration is one window, so at the five windows a second decision
// E9 fixes an operator's whole second is five of these.
//
// It is the fan-out benchmark with the origin count turned up. The narrow one
// above holds the encoding claim; this one holds what a routing table of four
// hundred costs the reader that follows all of it.
func BenchmarkFleetWindow(b *testing.B) {
	for _, subscribers := range fleetSubscribers {
		b.Run(fmt.Sprintf("subscribers=%d", subscribers), func(b *testing.B) {
			hub, subs, origins := fleetHub(b, subscribers, 1<<30)
			defer hub.Close()

			frame := make([]Publication, 0, fleetOrigins*fleetMetrics)

			for _, origin := range origins {
				for metric := range fleetMetrics {
					frame = append(frame, telemetryOf(origin, strconv.Itoa(metric), 215))
				}
			}

			b.ReportAllocs()

			for b.Loop() {
				for _, pub := range frame {
					if _, err := hub.Publish(pub); err != nil {
						b.Fatal(err)
					}
				}

				drainAll(b, subs)
			}
		})
	}
}

// BenchmarkFleetCursor prices the place a reader is handed after a window that
// carried a durable event: one position per origin it follows, rendered into
// the id field of an SSE frame. A subscriber following four hundred printers
// gets all four hundred every time the cursor moves.
func BenchmarkFleetCursor(b *testing.B) {
	hub, subs, origins := fleetHub(b, 1, 1<<30)
	defer hub.Close()

	for _, origin := range origins {
		if _, err := hub.Publish(stateOf(origin, 1)); err != nil {
			b.Fatal(err)
		}
	}

	batch, ready := subs[0].drain()
	require.True(b, ready)
	require.Equal(b, fleetOrigins, batch.Cursor.Places())

	b.ReportAllocs()

	var scratch []byte

	for b.Loop() {
		scratch = AppendSSECursor(scratch[:0], batch.Cursor)
	}

	b.ReportMetric(float64(len(scratch)), "bytes/frame")
}

// BenchmarkEncoding prices E12 directly: the same event delivered to a hundred
// subscribers, marshalled once against marshalled per subscriber.
func BenchmarkEncoding(b *testing.B) {
	const subscribers = 100

	env := event.Envelope{
		SpecVersion: event.SpecVersion,
		ID:          "01K2A0S3P4000000000000000A",
		Source:      event.SourceURN("01K2A0SPRAGUEFARM07", printerA),
		Type:        event.TypeDeviceTelemetrySampled,
		Time:        time.Now().UTC(),
		Subject:     printerA.String(),
		Sequence:    event.FormatSequence(914233),
		Epoch:       "01K2A0S3P4",
		Class:       event.ClassTelemetry,
		Data:        json.RawMessage(`{"nozzle":215.3,"bed":60.1,"progress":42}`),
	}

	b.Run("shared", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			m, err := newMessage(env, printerA, "telemetry")
			if err != nil {
				b.Fatal(err)
			}

			for range subscribers {
				if len(m.Encode(EncodingJSON)) == 0 {
					b.Fatal("empty encoding")
				}
			}
		}
	})

	b.Run("per_subscriber", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			for range subscribers {
				encoded, err := json.Marshal(env)
				if err != nil {
					b.Fatal(err)
				}

				if len(encoded) == 0 {
					b.Fatal("empty encoding")
				}
			}
		}
	})
}

// BenchmarkSubscriberSlice prices the registry read the fan-out does on every
// event: a copy-on-write slice behind an atomic pointer against the same slice
// behind an RWMutex.
func BenchmarkSubscriberSlice(b *testing.B) {
	const subscribers = 100

	subs := make([]*Subscription, subscribers)
	for i := range subs {
		subs[i] = &Subscription{}
	}

	b.Run("atomic_pointer", func(b *testing.B) {
		ch := newChannel(printerA, 1, 1)
		ch.readers.Store(&subs)

		b.ReportAllocs()
		b.RunParallel(func(p *testing.PB) {
			for p.Next() {
				if len(ch.subscribers()) != subscribers {
					b.Fatal("wrong length")
				}
			}
		})
	})

	b.Run("rwmutex", func(b *testing.B) {
		guarded := struct {
			mu   sync.RWMutex
			subs []*Subscription
		}{subs: subs}

		b.ReportAllocs()
		b.RunParallel(func(p *testing.PB) {
			for p.Next() {
				guarded.mu.RLock()
				n := len(guarded.subs)
				guarded.mu.RUnlock()

				if n != subscribers {
					b.Fatal("wrong length")
				}
			}
		})
	})
}

// TestFanOutSharesOneMarshal is the exact gate on E12, and the one an
// allocation count cannot be: a thousand readers hold the same backing array,
// or somebody copied the payload per subscriber.
func TestFanOutSharesOneMarshal(t *testing.T) {
	hub, subs := fanOutHub(t, 1000)
	defer hub.Close()

	_, err := hub.Publish(stateOf(printerA, 1))
	require.NoError(t, err)

	var first []byte

	for i, sub := range subs {
		batch, ready := sub.drain()
		require.True(t, ready)
		require.Len(t, batch.Events, 1)

		if i == 0 {
			first = batch.Events[0].Bytes

			continue
		}

		require.Samef(t, &first[0], &batch.Events[0].Bytes[0],
			"subscriber %d was handed its own copy of the event", i)
	}
}

// TestFanOutAllocationsDoNotScaleWithSubscribers gates on allocations per
// published event, which is deterministic where wall time is not. The path
// measured is publish plus every subscriber's drain, so an encode moved inside
// the per-subscriber loop shows up here.
func TestFanOutAllocationsDoNotScaleWithSubscribers(t *testing.T) {
	measure := func(subscribers int) float64 {
		hub, subs := fanOutHub(t, subscribers)
		defer hub.Close()

		pub := telemetryOf(printerA, "nozzle", 215)

		lowest := math.Inf(1)

		for range allocationRounds {
			lowest = min(lowest, testing.AllocsPerRun(200, func() {
				if _, err := hub.Publish(pub); err != nil {
					t.Fatal(err)
				}

				drainAll(t, subs)
			}))
		}

		return lowest
	}

	const wide = 1000

	one := measure(1)
	many := measure(wide)

	perSubscriber := (many - one) / float64(wide-1)

	t.Logf("allocs/op: 1 subscriber %.0f, %d subscribers %.0f, per subscriber %.3f",
		one, wide, many, perSubscriber)

	require.LessOrEqual(t, perSubscriber, perSubscriberAllocs+allocationTolerance/float64(wide-1),
		"the fan-out allocates per subscriber beyond that subscriber's own frames")
}

// fleetTelemetryPaths is what one printer can report in a window: the fifteen
// keys internal/connector/xbuddy maps into the state tree. An idle MK4 frame
// carries eleven of them and a printing one carries the four job keys as well,
// so a bound that covers a fleet covers fifteen. They are data here because a
// transport does not import a driver.
var fleetTelemetryPaths = []string{
	"/state",
	"/tools/0/nozzle_temp",
	"/tools/0/nozzle_target_temp",
	"/temperature/bed",
	"/temperature/bed_target",
	"/axis/x",
	"/axis/y",
	"/axis/z",
	"/speed",
	"/flow",
	"/tools/0/material_package_instance/package/material/abbreviation",
	"/job/id",
	"/job/progress",
	"/job/time_printing",
	"/job/time_remaining",
}

// wireTelemetryBytes is the smallest telemetry envelope this server was
// measured putting on its own event stream: 398 bytes, against a mean of 406
// over 24,511 frames of a 400-printer run. A delivery gate is
// only as strong as the frame it publishes, so it refuses to run on one
// smaller than the wire carries.
const wireTelemetryBytes = 398

// sampleOf is a telemetry publication in the shape internal/app publishes: one
// state-tree path and its value, conflated under the path.
func sampleOf(origin event.Origin, path string) Publication {
	return Publication{
		Origin: origin,
		Type:   event.TypeDeviceTelemetrySampled,
		Class:  event.ClassTelemetry,
		Key:    path,
		Data:   json.RawMessage(fmt.Sprintf(`{"path":%q,"value":215.75}`, path)),
	}
}

// TestOneWindowOfTheClaimedFleetReachesEveryReader gates on delivery, which is
// what TestFanOutAllocationsDoNotScaleWithSubscribers cannot see: allocations
// per event stayed flat while a reader watching four hundred printers was
// given nine per cent of what they said.
//
// The whole window is published before anything is drained, so nothing here is
// a slow reader. What decides is the per-subscriber byte bound, and the hub is
// built with the shipped default so this measures what a deployment gets.
func TestOneWindowOfTheClaimedFleetReachesEveryReader(t *testing.T) {
	const readers = 8

	hub, subs, origins := fleetHub(t, readers, 0)
	defer hub.Close()

	published := 0

	for _, origin := range origins {
		for _, path := range fleetTelemetryPaths {
			if _, err := hub.Publish(sampleOf(origin, path)); err != nil {
				t.Fatal(err)
			}

			published++
		}
	}

	for i, sub := range subs {
		batch, ready := sub.drain()
		require.Truef(t, ready, "reader %d was given nothing at all", i)

		delivered, bytes := len(batch.Telemetry), 0
		for _, frame := range batch.Telemetry {
			bytes += len(frame.Bytes)
		}

		coverage := float64(delivered) / float64(published) * percentage

		if i == 0 {
			t.Logf("published %d frames, delivered %d (%.1f%%), %d bytes at %d a frame",
				published, delivered, coverage, bytes, bytes/max(delivered, 1))
		}

		require.GreaterOrEqualf(t, bytes/max(delivered, 1), wireTelemetryBytes,
			"reader %d was given a frame smaller than the wire carries, so this gate proves less "+
				"than it says", i)

		require.Equalf(t, published, delivered,
			"reader %d was given %.1f%% of one window of the fleet this product claims", i, coverage)

		require.Zerof(t, sub.Stats().TelemetryDropped, "reader %d had frames dropped", i)
	}
}

// percentage turns a fraction into the number a coverage message prints.
const percentage = 100
