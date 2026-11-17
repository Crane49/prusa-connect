package stream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/event"
)

const (
	alice = "alice"
	bob   = "bob"
	carol = "carol"
	nemo  = "nemo"

	// kindStorage stands in for internal/kernel/ref's closed kind set, which
	// sdk/event cannot import and does not duplicate.
	kindStorage event.Kind = "storage"
	kindCamera  event.Kind = "camera"
)

// The origins these tests publish from. A printer and a storage instance are
// the same sort of thing to the hub, which is the point of R5.
var (
	printerA = originOf(event.KindDevice, "01JB8XN2K7QW9ZR4MV3T6H0PDA")
	printerB = originOf(event.KindDevice, "01JB8XN2K7QW9ZR4MV3T6H0PDB")
	unknown  = originOf(event.KindDevice, "01PRINTERTHATDOESNOTEXIST0")
	s3Main   = originOf(kindStorage, "s3-main")
	camFront = originOf(kindCamera, "front")
)

// originOf builds an origin for a test table. A construction that fails is a
// broken test rather than a runtime condition, so it panics.
func originOf(kind event.Kind, id string) event.Origin {
	origin, err := event.NewOrigin(kind, id)
	if err != nil {
		panic(err)
	}

	return origin
}

// staticAuthorizer stands in for internal/authz. The hub asks one question and
// this answers it from a table.
type staticAuthorizer struct {
	visible map[string][]event.Origin
	epoch   uint64
	err     error
}

func (a staticAuthorizer) VisibleOrigins(_ context.Context, principal string) (Visibility, error) {
	if a.err != nil {
		return Visibility{}, a.err
	}

	return Visibility{Origins: grantsOf(a.visible[principal]...), Epoch: a.epoch}, nil
}

// grantsOf renders a permitted set as the answer an authorizer gives, with no
// generation on any of it. Zero is a grant with no recorded generation, so a
// fake that reports none exercises the fallback the hub keeps for it.
func grantsOf(origins ...event.Origin) []Grant {
	out := make([]Grant, 0, len(origins))
	for _, origin := range origins {
		out = append(out, Grant{Origin: origin})
	}

	return out
}

func (a staticAuthorizer) Epoch() uint64 { return a.epoch }

func newTestHub(t *testing.T, opts Options) *Hub {
	t.Helper()

	if opts.Site == "" {
		opts.Site = "01K2A0SPRAGUEFARM07"
	}

	if opts.Authorizer == nil {
		opts.Authorizer = staticAuthorizer{visible: map[string][]event.Origin{alice: {printerA}, bob: {printerB}}}
	}

	hub, err := New(opts)
	require.NoError(t, err)

	return hub
}

func telemetryOf(origin event.Origin, key string, value int) Publication {
	return Publication{
		Origin: origin,
		Type:   event.TypeDeviceTelemetrySampled,
		Class:  event.ClassTelemetry,
		Key:    key,
		Data:   json.RawMessage(fmt.Sprintf(`{"key":%q,"value":%d}`, key, value)),
	}
}

func stateOf(origin event.Origin, value int) Publication {
	return Publication{
		Origin: origin,
		Type:   event.TypeDeviceDeparted,
		Class:  event.ClassState,
		Data:   json.RawMessage(fmt.Sprintf(`{"value":%d}`, value)),
	}
}

// resumeAt is the cursor a reader holds after seeing one origin's offset, in
// the generation this hub minted.
func resumeAt(hub *Hub, positions ...event.Position) event.Cursor {
	return event.NewCursor(event.NewGeneration(hub.Epoch(), positions))
}

// heldBy returns the reader's place in the generation this hub minted.
func heldBy(t *testing.T, hub *Hub, cursor event.Cursor, origin event.Origin) (uint64, bool) {
	t.Helper()

	generation, held := cursor.Generation(hub.Epoch())
	if !held {
		return 0, false
	}

	return generation.Offset(origin)
}

func receive(t *testing.T, sub *Subscription) Batch {
	t.Helper()

	select {
	case batch, open := <-sub.Batches():
		require.True(t, open, "the subscription closed instead of delivering a batch")

		return batch
	default:
		t.Fatal("no batch waiting")

		return Batch{}
	}
}

func payloadValue(t *testing.T, frame Frame) int {
	t.Helper()

	var env event.Envelope
	require.NoError(t, json.Unmarshal(frame.Bytes, &env))

	var data struct {
		Value int `json:"value"`
	}

	require.NoError(t, json.Unmarshal(env.Data, &data))

	return data.Value
}

func channelNames(hub *Hub) []string {
	names := make([]string, 0)
	for origin := range *hub.channels.Load() {
		names = append(names, origin.String())
	}

	slices.Sort(names)

	return names
}

func offsets(frames []Frame) []uint64 {
	out := make([]uint64, 0, len(frames))
	for _, frame := range frames {
		out = append(out, frame.Position.Offset)
	}

	return out
}

// TestSubscriberNeverSeesAnUnauthorizedPrinter is a design requirement, asserted as an
// absence rather than as a timeout: after synctest.Wait every goroutine in the
// bubble is durably blocked, so an empty channel is proof and not a guess.
func TestSubscriberNeverSeesAnUnauthorizedPrinter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		for _, pub := range []Publication{
			telemetryOf(printerB, "nozzle", 215),
			stateOf(printerB, 1),
		} {
			_, err = hub.Publish(pub)
			require.NoError(t, err)
		}

		synctest.Wait()
		require.Empty(t, sub.Batches(), "a printer alice may not see reached her stream")
		require.Equal(t, 0, sub.Stats().TelemetryKeys, "an unauthorized printer did not even reach the queue")

		_, err = hub.Publish(telemetryOf(printerA, "nozzle", 215))
		require.NoError(t, err)

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, sub)
		require.Len(t, batch.Telemetry, 1)
		require.Equal(t, printerA, batch.Telemetry[0].Position.Origin)
	})
}

// TestOffsetsCountOnlyVisiblePrinters is the other half of a design requirement, and
// the one a global counter fails: an offset a reader can see must count that
// printer's events and nothing else, or the distance between two of them is a
// tally of traffic the reader was refused.
func TestOffsetsCountOnlyVisiblePrinters(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		// printerB publishes far more than printerA, and invisibly.
		for i := range 3 {
			for range 5 {
				_, err = hub.Publish(stateOf(printerB, i))
				require.NoError(t, err)
			}

			_, err = hub.Publish(stateOf(printerA, i))
			require.NoError(t, err)
		}

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, sub)
		require.Equal(t, []uint64{1, 2, 3}, offsets(batch.Events),
			"alice's offsets counted events on a printer she may not see")
		require.Equal(t, resumeAt(hub, event.Position{Origin: printerA, Offset: 3}),
			batch.Cursor, "the cursor names only what she follows")
	})
}

// TestTelemetryCarriesNoPosition closes the same leak on the lane that has no
// offsets to give: a telemetry-only reader must not be handed a number that
// moves when other printers publish durable events.
func TestTelemetryCarriesNoPosition(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Encoding:  EncodingSSE,
		})
		require.NoError(t, err)

		defer sub.Close()

		for range 20 {
			_, err = hub.Publish(stateOf(printerB, 1))
			require.NoError(t, err)
		}

		_, err = hub.Publish(telemetryOf(printerA, "nozzle", 215))
		require.NoError(t, err)

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, sub)
		require.Len(t, batch.Telemetry, 1)
		require.Equal(t, uint64(0), batch.Telemetry[0].Position.Offset)
		require.NotContains(t, string(batch.Telemetry[0].Bytes), sseIDField,
			"a telemetry frame that carries an id hands out a place it cannot resume from")
		generation, held := batch.Cursor.Generation(hub.Epoch())
		require.True(t, held)
		require.Empty(t, generation.Positions, "no durable event was delivered, so there is no place to resume")
	})
}

func TestSubscribeRefusesAPrinterThePrincipalMayNotSee(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		unauthorized, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Origins:   []event.Origin{printerB},
		})
		require.ErrorIs(t, err, ErrNotPermitted)
		require.Nil(t, unauthorized)
		require.NotContains(t, err.Error(), printerB.ID(), "the refusal named the printer it exists to hide")

		_, unknownErr := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Origins:   []event.Origin{unknown},
		})
		require.Equal(t, err.Error(), unknownErr.Error(),
			"a printer that does not exist must be refused in the same words as one that does")

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Origins:   []event.Origin{printerA},
		})
		require.NoError(t, err)

		defer sub.Close()

		require.Equal(t, []event.Origin{printerA}, sub.Origins())
	})
}

// TestAPrincipalWithNothingVisibleGetsAnEmptyStream is the other half of the
// refusal rule. A caller permitted nothing is told nothing, which an open
// stream carrying no event does as completely as a 403 and without putting the
// reader who most needs the stream, a page opened before the first printer has
// dialled in, on its own reconnect ladder.
//
// What it must not do is answer a question. A forged cursor naming an origin
// this caller may not see replays nothing and reports no gap under it, so the
// holder learns neither that the origin exists nor where its log is. The origin
// is named back as withdrawn, which an origin that never existed is as well.
func TestAPrincipalWithNothingVisibleGetsAnEmptyStream(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		_, err := hub.Publish(stateOf(printerA, 1))
		require.NoError(t, err)

		here := event.NewCursor(event.NewGeneration(hub.Epoch(),
			[]event.Position{{Origin: printerA, Offset: 1}}))

		for name, tc := range map[string]struct {
			req             SubscribeRequest
			expectWithdrawn []event.Origin
		}{
			"live":            {req: SubscribeRequest{Principal: nemo}},
			"forged position": {req: SubscribeRequest{Principal: nemo, Resume: here}, expectWithdrawn: []event.Origin{printerA}},
		} {
			sub, opened := hub.Subscribe(context.Background(), tc.req)
			require.NoErrorf(t, opened, "request %q", name)
			require.NotNilf(t, sub, "request %q", name)
			require.Emptyf(t, sub.Origins(), "request %q attached an origin nobody granted", name)

			_, published := hub.Publish(stateOf(printerA, 2))
			require.NoError(t, published)

			time.Sleep(2 * DefaultInterval)
			synctest.Wait()

			if tc.expectWithdrawn != nil {
				batch := receive(t, sub)
				require.Equal(t, tc.expectWithdrawn, batch.Withdrawn)
				require.Empty(t, batch.Events)
				require.False(t, batch.Resync)
			}

			require.Emptyf(t, sub.Batches(), "request %q was given an event", name)

			sub.Close()
		}
	})
}

// TestANarrowingToAnOriginAPrincipalMayNotSeeIsStillRefused is the line the
// empty stream above does not cross. A caller who named an origin is telling
// the hub what they mean to watch, and handing them a stream that quietly omits
// it is a client watching a printer it does not know it is missing.
func TestANarrowingToAnOriginAPrincipalMayNotSeeIsStillRefused(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: nemo,
			Origins:   []event.Origin{printerA},
		})
		require.ErrorIs(t, err, ErrNotPermitted)
		require.Nil(t, sub)
	})
}

func TestSubscribeReportsAnAuthorizerFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		failure := errors.New("policy store unreachable")
		hub := newTestHub(t, Options{Authorizer: staticAuthorizer{err: failure}})

		defer hub.Close()

		_, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.ErrorIs(t, err, ErrAuthorize)
		require.ErrorIs(t, err, failure)
	})
}

func TestConflationKeepsTheLatestValuePerKey(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		const rounds = 100

		for i := range rounds {
			_, err = hub.Publish(telemetryOf(printerA, "nozzle", i))
			require.NoError(t, err)
		}

		_, err = hub.Publish(telemetryOf(printerA, "bed", 60))
		require.NoError(t, err)

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, sub)
		require.Len(t, batch.Telemetry, 2, "one frame per key, not one per publication")
		require.Equal(t, rounds-1, payloadValue(t, batch.Telemetry[0]), "the superseded values are gone")
		require.Equal(t, 60, payloadValue(t, batch.Telemetry[1]))

		stats := sub.Stats()
		require.Equal(t, uint64(rounds-1), stats.TelemetrySuperseded)
		require.Equal(t, uint64(0), stats.TelemetryDropped, "conflation is not a drop policy")
	})
}

// TestStalledSubscriberTelemetryStaysBounded is the structural bound: the queue
// is capped by the number of distinct keys, so a reader that stopped reading
// costs the same as one that is keeping up.
func TestStalledSubscriberTelemetryStaysBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		keys := []string{"nozzle", "bed", "position", "fan", "progress", "flow"}

		const burst = 20_000

		for i := range burst {
			_, err = hub.Publish(telemetryOf(printerA, keys[i%len(keys)], i))
			require.NoError(t, err)
		}

		synctest.Wait()

		stats := sub.Stats()
		require.LessOrEqual(t, stats.TelemetryKeys, len(keys))
		require.Less(t, stats.TelemetryBytes, len(keys)*512)
		require.Equal(t, uint64(burst-len(keys)), stats.TelemetrySuperseded)
		require.Equal(t, uint64(0), stats.TelemetryDropped, "a stalled reader is never disconnected over telemetry")
	})
}

func TestTelemetryByteCapEvictsRatherThanGrowing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{TelemetryBytes: 1024})
		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		for i := range 200 {
			_, err = hub.Publish(telemetryOf(printerA, fmt.Sprintf("metric-%d", i), i))
			require.NoError(t, err)
		}

		synctest.Wait()

		stats := sub.Stats()
		require.LessOrEqual(t, stats.TelemetryBytes, 1024+512)
		require.Positive(t, stats.TelemetryDropped, "distinct keys past the cap have to go somewhere")
	})
}

// locked is a log sink two goroutines write to and read. A pump logs while the
// test reads, and a strings.Builder shared that way is a race.
type locked struct {
	mu      sync.Mutex
	written strings.Builder
}

func (l *locked) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.written.Write(p)
}

func (l *locked) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.written.String()
}

// TestATelemetryDropReachesTheLog is the observability half of the byte bound.
// The count on Stats is read by nothing outside a test, so a fleet whose reader
// is being given part of its telemetry looks exactly like one that is not.
// The line is what an operator has to go on.
func TestATelemetryDropReachesTheLog(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		said := &locked{}

		hub := newTestHub(t, Options{
			TelemetryBytes: 1024,
			Logger:         slog.New(slog.NewTextHandler(said, &slog.HandlerOptions{Level: slog.LevelWarn})),
		})
		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		for i := range 200 {
			_, err = hub.Publish(telemetryOf(printerA, fmt.Sprintf("metric-%d", i), i))
			require.NoError(t, err)
		}

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		line := said.String()
		require.Contains(t, line, "not being given all the telemetry")
		require.Contains(t, line, "principal="+alice)
		require.Contains(t, line, "bytes_limit=1024")
		require.Regexp(t, `dropped=[1-9]`, line)
	})
}

// TestTheTelemetryLogIsWrittenOncePerInterval holds the other half of the
// choice: a queue over its bound is over it on every window, and a line each
// time would bury the first one.
func TestTheTelemetryLogIsWrittenOncePerInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		said := &locked{}

		hub := newTestHub(t, Options{
			TelemetryBytes: 1024,
			Logger:         slog.New(slog.NewTextHandler(said, &slog.HandlerOptions{Level: slog.LevelWarn})),
		})
		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		// Ten windows, each of them over the bound, inside one report interval.
		for window := range 10 {
			for i := range 200 {
				_, err = hub.Publish(telemetryOf(printerA, fmt.Sprintf("metric-%d-%d", window, i), i))
				require.NoError(t, err)
			}

			time.Sleep(2 * DefaultInterval)
			synctest.Wait()
		}

		require.Equal(t, 1, strings.Count(said.String(), "not being given all the telemetry"))
	})
}

func TestStalledSubscriberDoesNotSlowAHealthyOne(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{
			Authorizer: staticAuthorizer{visible: map[string][]event.Origin{alice: {printerA}, bob: {printerA}}},
		})

		defer hub.Close()

		stalled, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer stalled.Close()

		healthy, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: bob})
		require.NoError(t, err)

		defer healthy.Close()

		var received atomic.Int64

		go func() {
			for range healthy.Batches() {
				received.Add(1)
			}
		}()

		const rounds = 10

		for i := range rounds {
			_, err = hub.Publish(telemetryOf(printerA, "nozzle", i))
			require.NoError(t, err)

			time.Sleep(DefaultInterval + DefaultInterval/4)
		}

		synctest.Wait()

		require.Equal(t, int64(rounds), received.Load(), "the healthy reader got every window")
		require.LessOrEqual(t, stalled.Stats().TelemetryKeys, 1)
	})
}

// TestEventOverflowSignalsResync is the other half of the queue split: the
// durable lane may not drop quietly, because a client holding part of a stream
// is wrong without knowing it.
func TestEventOverflowSignalsResync(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{EventBytes: 2048})
		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		const burst = 200

		for i := range burst {
			_, err = hub.Publish(stateOf(printerA, i))
			require.NoError(t, err)
		}

		synctest.Wait()

		stats := sub.Stats()
		require.True(t, stats.Resync)
		require.Positive(t, stats.EventDropped)
		require.LessOrEqual(t, stats.EventBytes, 2048)

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, sub)
		require.True(t, batch.Resync, "the reader is told, not left to notice")
		require.Less(t, len(batch.Events), burst)
		require.False(t, sub.Stats().Resync, "the flag is cleared once it has been delivered")
	})
}

// TestResyncCursorIsTheHead follows from declaring continuity gone: the reader
// reloads state from the API, so a cursor behind the events that were discarded
// would replay what the reload already reflects and walk its state backwards.
func TestResyncCursorIsTheHead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{EventBytes: 2048})
		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		var head event.Position

		for i := range 200 {
			head, err = hub.Publish(stateOf(printerA, i))
			require.NoError(t, err)
		}

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, sub)
		require.True(t, batch.Resync)

		offset, named := heldBy(t, hub, batch.Cursor, printerA)
		require.True(t, named)
		require.Equal(t, head.Offset, offset, "a resync hands back the head, not a place behind the discard")

		resumed, err := hub.Subscribe(context.Background(), SubscribeRequest{
			Principal: alice,
			Resume:    batch.Cursor,
		})
		require.NoError(t, err)

		defer resumed.Close()

		synctest.Wait()
		require.Empty(t, resumed.Batches(), "the reader that resynced does not get the discarded events back")
	})
}

// TestOneMarshalIsSharedByEverySubscriber is E12: the bytes a thousand readers
// get are the same bytes, or the fan-out allocates per subscriber.
func TestOneMarshalIsSharedByEverySubscriber(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{
			Authorizer: staticAuthorizer{
				visible: map[string][]event.Origin{alice: {printerA}, bob: {printerA}, carol: {printerA}},
			},
		})

		defer hub.Close()

		first, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer first.Close()

		same, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: carol})
		require.NoError(t, err)

		defer same.Close()

		second, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: bob, Encoding: EncodingSSE})
		require.NoError(t, err)

		defer second.Close()

		_, err = hub.Publish(stateOf(printerA, 1))
		require.NoError(t, err)

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		firstBatch := receive(t, first)
		sameBatch := receive(t, same)
		secondBatch := receive(t, second)

		require.Len(t, firstBatch.Events, 1)
		require.Len(t, sameBatch.Events, 1)
		require.Len(t, secondBatch.Events, 1)

		require.Same(t, &firstBatch.Events[0].Bytes[0], &sameBatch.Events[0].Bytes[0],
			"two subscribers on one encoding share one marshal")
		require.NotSame(t, &firstBatch.Events[0].Bytes[0], &secondBatch.Events[0].Bytes[0],
			"two encodings are two byte layouts")
		require.Contains(t, string(secondBatch.Events[0].Bytes),
			sseIDField+event.Position{Origin: printerA, Offset: 1}.ID(hub.Epoch()))
	})
}

func TestCoalescingHoldsTheOutboundRate(t *testing.T) {
	tests := map[string]struct {
		interval  time.Duration
		maxFrames int64
	}{
		"ten a second at the floor":           {interval: MinInterval, maxFrames: 11},
		"five a second by default":            {interval: DefaultInterval, maxFrames: 6},
		"a request below the floor is raised": {interval: time.Millisecond, maxFrames: 11},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				hub := newTestHub(t, Options{})
				defer hub.Close()

				sub, err := hub.Subscribe(context.Background(), SubscribeRequest{
					Principal: alice,
					Interval:  tc.interval,
				})
				require.NoError(t, err)

				defer sub.Close()

				require.GreaterOrEqual(t, sub.interval, MinInterval)

				var received atomic.Int64

				go func() {
					for range sub.Batches() {
						received.Add(1)
					}
				}()

				// One printer frame per millisecond for a second: a thousand
				// ingress events against an outbound rate a browser can render.
				for i := range 1000 {
					_, err = hub.Publish(telemetryOf(printerA, "nozzle", i))
					require.NoError(t, err)

					time.Sleep(time.Millisecond)
				}

				synctest.Wait()

				require.LessOrEqual(t, received.Load(), tc.maxFrames)
				require.Positive(t, received.Load())
			})
		})
	}
}

func TestPublishRejectsAMalformedPublication(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		for name, pub := range map[string]Publication{
			"no origin": {Type: event.TypeDeviceDeparted, Class: event.ClassState},
			"no type":   {Origin: printerA, Class: event.ClassState},
			"no class":  {Origin: printerA, Type: event.TypeDeviceDeparted},
		} {
			_, err := hub.Publish(pub)
			require.ErrorIsf(t, err, ErrInvalidRequest, "publication %q", name)
		}

		ttlOnAnEvent := stateOf(printerA, 1)
		ttlOnAnEvent.TTL = new(uint32(30))

		_, err := hub.Publish(ttlOnAnEvent)
		require.ErrorIs(t, err, ErrInvalidRequest, "an event that expires is a contradiction")

		broken := stateOf(printerA, 1)
		broken.Data = json.RawMessage(`{"value":`)

		_, err = hub.Publish(broken)
		require.ErrorIs(t, err, ErrInvalidRequest)

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		_, err = hub.Publish(stateOf(printerA, 1))
		require.NoError(t, err)

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, sub)

		var env event.Envelope
		require.NoError(t, json.Unmarshal(batch.Events[0].Bytes, &env))
		require.Equal(t, event.FormatSequence(1), env.Sequence,
			"a rejected publication does not consume a sequence number")
	})
}

func TestPublishAssignsIdentityAndSequencePerSource(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		defer sub.Close()

		for i := range 3 {
			_, err = hub.Publish(stateOf(printerA, i))
			require.NoError(t, err)
		}

		time.Sleep(2 * DefaultInterval)
		synctest.Wait()

		batch := receive(t, sub)
		require.Len(t, batch.Events, 3)

		seen := make(map[string]struct{}, len(batch.Events))

		for i, frame := range batch.Events {
			var env event.Envelope
			require.NoError(t, json.Unmarshal(frame.Bytes, &env))

			require.Equal(t, event.SpecVersion, env.SpecVersion)
			require.Equal(t, event.SourceURN("01K2A0SPRAGUEFARM07", printerA), env.Source)
			require.Equal(t, "urn:prusa:site:01K2A0SPRAGUEFARM07/device/01JB8XN2K7QW9ZR4MV3T6H0PDA", env.Source,
				"a printer's source is what an earlier design decision fixed on the wire, byte for byte")
			require.Equal(t, printerA.String(), env.Subject)
			require.Equal(t, hub.Epoch(), env.Epoch)
			require.Equal(t, uint8(0), env.Hops)
			require.Equal(t, event.FormatSequence(uint64(i+1)), env.Sequence)

			_, duplicate := seen[env.ID]
			require.False(t, duplicate, "an id is unique per source")
			seen[env.ID] = struct{}{}
		}
	})
}

func TestCloseEndsEveryStream(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)
		require.Equal(t, 1, hub.Subscribers())

		hub.Close()
		synctest.Wait()

		_, open := <-sub.Batches()
		require.False(t, open, "a closed hub closes the reader's channel")

		_, err = hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.ErrorIs(t, err, ErrHubClosed)

		_, err = hub.Publish(stateOf(printerA, 1))
		require.ErrorIs(t, err, ErrHubClosed)

		sub.Close()
		hub.Close()
	})
}

// gateAuthorizer holds a subscribe inside the authorization call, which is
// where the hub does I/O and where a Close can land.
type gateAuthorizer struct {
	entered chan struct{}
	release chan struct{}
	visible []event.Origin
}

func (g gateAuthorizer) VisibleOrigins(_ context.Context, _ string) (Visibility, error) {
	close(g.entered)
	<-g.release

	return Visibility{Origins: grantsOf(g.visible...)}, nil
}

func (g gateAuthorizer) Epoch() uint64 { return 0 }

// TestSubscribeRacingCloseStartsNoPump is F4 as a test: a Close landing while
// the authorizer is still answering must not leave a pump running past the wait,
// nor a reader holding a channel that never closes. synctest fails the test if a
// goroutine outlives the bubble, so the leak has nowhere to hide.
func TestSubscribeRacingCloseStartsNoPump(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := gateAuthorizer{
			entered: make(chan struct{}),
			release: make(chan struct{}),
			visible: []event.Origin{printerA},
		}
		hub := newTestHub(t, Options{Authorizer: gate})

		var (
			sub *Subscription
			err error
		)

		done := make(chan struct{})

		go func() {
			defer close(done)

			sub, err = hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		}()

		<-gate.entered
		hub.Close()
		close(gate.release)
		<-done

		require.ErrorIs(t, err, ErrHubClosed)
		require.Nil(t, sub)
		require.Equal(t, 0, hub.Subscribers())
	})
}

func TestSubscriptionCloseDetaches(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := newTestHub(t, Options{})
		defer hub.Close()

		sub, err := hub.Subscribe(context.Background(), SubscribeRequest{Principal: alice})
		require.NoError(t, err)

		sub.Close()
		sub.Close()
		synctest.Wait()

		require.Equal(t, 0, hub.Subscribers())
		require.Equal(t, []string{printerA.String()}, channelNames(hub),
			"a detach touched a channel it had no business creating")

		_, err = hub.Publish(telemetryOf(printerA, "nozzle", 1))
		require.NoError(t, err)
	})
}
