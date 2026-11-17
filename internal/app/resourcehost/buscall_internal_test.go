package resourcehost

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/extension/bus"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/busapi"
)

// This file is in the package because the two properties below are properties
// of the gate and not of the deployment. Neither is reachable from outside: a
// module writes its frames onto one wire, one at a time, so a burst of
// emissions arrives at the host almost serially and a case driven through a
// child process cannot make the host hold more than one or two at once. The end
// to end case in internal/app proves the path; this proves the bound.
//
// The emitter is a stand-in and the hub is not, which is the right way round.
// What is under test is admission and ordering, and the emitter is the input
// they are applied to. Nothing here asserts anything about a frame.

// emitterDwell is how long the emitter below stays inside itself. It is far
// longer than a publish takes and far shorter than a test run, so a case about
// how many callers overlap is deciding on the serialisation rather than on the
// scheduler.
const emitterDwell = 20 * time.Millisecond

// heldEmitter is an emitter that stays inside itself long enough for a second
// caller to arrive, and records the largest number of callers that were in at
// once.
//
// dwell is what makes the count mean something. Hub.Publish returns in
// microseconds, so eight goroutines calling an emitter that returns at once
// hardly ever overlap, and a case over that emitter would pass against a host
// with no serialisation at all. dwell is long enough that every goroutine has
// started before the first one leaves.
//
// release, when it is set, holds every caller until it is closed. It is what a
// case about the admission bound needs and what a case about serialisation must
// not have.
type heldEmitter struct {
	dwell   time.Duration
	release chan struct{}
	entered chan struct{}

	inside  atomic.Int64
	highest atomic.Int64

	mu   sync.Mutex
	seen []bus.Emission
}

var _ bus.Emitter = (*heldEmitter)(nil)

func (h *heldEmitter) Emit(emission bus.Emission) error {
	now := h.inside.Add(1)
	for {
		high := h.highest.Load()
		if now <= high || h.highest.CompareAndSwap(high, now) {
			break
		}
	}

	select {
	case h.entered <- struct{}{}:
	default:
	}

	if h.release != nil {
		<-h.release
	}

	if h.dwell > 0 {
		time.Sleep(h.dwell)
	}

	h.mu.Lock()
	h.seen = append(h.seen, emission)
	h.mu.Unlock()

	h.inside.Add(-1)

	return nil
}

func (h *heldEmitter) emissions() []bus.Emission {
	h.mu.Lock()
	defer h.mu.Unlock()

	return append([]bus.Emission(nil), h.seen...)
}

// testBus is a gate over a module that is serving, so a case reaches the two
// properties below without a supervision plane and a child process.
func testBus(t *testing.T, emitter bus.Emitter, width int) *gatedBus {
	t.Helper()

	instance, err := ref.New(ref.KindSink, "planner")
	require.NoError(t, err)

	built, err := newBus(nil, instance, supervisor.Policy{Concurrency: width},
		map[string]string{"changed": "com.github.aux.acme.planner.spool.changed"}, emitter)
	require.NoError(t, err)

	// The readiness answer is an input to this gate, not the mechanism it is
	// being held to. newBus builds one that asks the plane; a case that has no
	// plane states the answer instead.
	built.serving = func() string { return "" }

	return built
}

// TestOneInstancePublishesFromOneGoroutineAtATime is the hub's own requirement,
// held at the door a module reaches it through.
//
// internal/transport/stream says events for one origin must be published from
// one goroutine. It keeps the promise in two places that are not one critical
// section: the sequence is an atomic counter and the offset is taken under the
// origin's history lock, so two goroutines publishing for one origin can take
// their sequence numbers in one order and their offsets in the other. A reader
// resuming from a cursor is then handed an offset that runs backwards against
// the sequence beside it.
//
// The script tier satisfies this by construction, with one goroutine per
// instance. A module does not: sdk/wirerpc answers every inbound call on a
// goroutine of its own.
func TestOneInstancePublishesFromOneGoroutineAtATime(t *testing.T) {
	t.Parallel()

	emitter := &heldEmitter{dwell: emitterDwell, entered: make(chan struct{}, 1)}
	gate := testBus(t, emitter, supervisor.DefaultConcurrency)

	const emissions = supervisor.DefaultConcurrency

	var (
		ready sync.WaitGroup
		done  sync.WaitGroup
		start = make(chan struct{})
	)

	ready.Add(emissions)
	done.Add(emissions)

	for i := range emissions {
		go func() {
			defer done.Done()

			ready.Done()
			<-start

			_ = gate.Emit(t.Context(), "changed", strconv.Itoa(i), json.RawMessage(`{}`))
		}()
	}

	ready.Wait()
	close(start)
	done.Wait()

	require.Len(t, emitter.emissions(), emissions,
		"an emission was lost, so the count below is over the wrong set")
	require.Equal(t, int64(1), emitter.highest.Load(),
		"two goroutines were inside the hub for one origin at once, which is what makes an "+
			"offset run backwards against the sequence beside it")
}

// TestAnInstanceAtItsEmitLimitIsRefusedAndNotQueued is the admission bound in
// front of that lock.
//
// Without it the lock is a place a child process parks as many of the host's
// goroutines as it can open streams, which is the failure the state door was
// given a semaphore for. The refusal names the width and the instance, because
// ADR-20260827-wgc rule 12 asks that an author read the limit rather than guess it.
func TestAnInstanceAtItsEmitLimitIsRefusedAndNotQueued(t *testing.T) {
	t.Parallel()

	const width = 2

	emitter := &heldEmitter{release: make(chan struct{}), entered: make(chan struct{}, 1)}
	gate := testBus(t, emitter, width)

	// Two emissions that take the slots and stay in them, so the third meets a
	// full semaphore rather than a race.
	var held sync.WaitGroup

	held.Add(width)

	for i := range width {
		go func() {
			defer held.Done()

			_ = gate.Emit(t.Context(), "changed", strconv.Itoa(i), json.RawMessage(`{}`))
		}()
	}

	// The first is inside the emitter, and the second is either inside the
	// semaphore or waiting for the lock. Both hold a slot either way.
	select {
	case <-emitter.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("no emission reached the emitter")
	}

	require.Eventually(t, func() bool {
		return len(gate.slots) == width
	}, 5*time.Second, time.Millisecond, "the slots were never all taken")

	refused := gate.Emit(t.Context(), "changed", "over", json.RawMessage(`{}`))
	require.ErrorIs(t, refused, busapi.ErrBusy,
		"an emission over the width was not refused, so nothing bounds this door")
	require.Contains(t, refused.Error(), strconv.Itoa(width),
		"the refusal does not name the width")
	require.Contains(t, refused.Error(), "sink:planner",
		"the refusal does not say whose limit was reached")

	close(emitter.release)
	held.Wait()

	// And the door opens again once the slots are given back, so this is a
	// limit and not a closed door.
	require.NoError(t, gate.Emit(t.Context(), "changed", "after", json.RawMessage(`{}`)))
}

// TestAnEndpointNothingDeclaredHasNoTypeToBePublishedUnder is the containment
// stated at the gate: the type comes out of a table the operator wrote, so a
// name that is not in it resolves to nothing at all.
func TestAnEndpointNothingDeclaredHasNoTypeToBePublishedUnder(t *testing.T) {
	t.Parallel()

	emitter := &heldEmitter{entered: make(chan struct{}, 1)}
	gate := testBus(t, emitter, supervisor.DefaultConcurrency)

	for _, endpoint := range []string{
		"forged",
		"com.prusa.connect.device.arrived",
		"spool.changed",
	} {
		err := gate.Emit(t.Context(), endpoint, "", json.RawMessage(`{}`))
		require.ErrorIs(t, err, busapi.ErrNoEndpoint, "%q reached the event stream", endpoint)
		require.Contains(t, err.Error(), "the endpoints changed",
			"the refusal does not say which endpoints the declaration carries")
	}

	require.Empty(t, emitter.emissions(), "a refused endpoint reached the emitter")

	// The declared one publishes, and it publishes the operator's type and not
	// the module's endpoint name.
	require.NoError(t, gate.Emit(t.Context(), "changed", "k", json.RawMessage(`{"a":1}`)))

	sent := emitter.emissions()
	require.Len(t, sent, 1)
	require.Equal(t, "com.github.aux.acme.planner.spool.changed", sent[0].Type)
	require.Equal(t, "k", sent[0].Key)
	require.Equal(t, `{"a":1}`, sent[0].Body)
	require.False(t, strings.HasPrefix(sent[0].Type, "com.prusa.connect."),
		"a module's emission carries a type in this project's own closed namespace")
}

// TestAnEmissionWithNoPayloadIsAnEmptyDocument holds the one rewrite this gate
// makes. The bus carries a CloudEvents data member, and a module that emitted
// nothing would otherwise publish a frame whose data member is absent where
// every other producer's is an object.
func TestAnEmissionWithNoPayloadIsAnEmptyDocument(t *testing.T) {
	t.Parallel()

	emitter := &heldEmitter{entered: make(chan struct{}, 1)}
	gate := testBus(t, emitter, supervisor.DefaultConcurrency)

	require.NoError(t, gate.Emit(t.Context(), "changed", "", nil))

	sent := emitter.emissions()
	require.Len(t, sent, 1)
	require.JSONEq(t, `{}`, sent[0].Body)
}

// TestAnInstanceWithNoEventStreamIsRefusedAtStart is the operator's answer for a
// deployment that offers no bus: the instance fails to start and says why,
// rather than running and logging on every event.
func TestAnInstanceWithNoEventStreamIsRefusedAtStart(t *testing.T) {
	t.Parallel()

	instance, err := ref.New(ref.KindSink, "planner")
	require.NoError(t, err)

	_, err = newBus(nil, instance, supervisor.Policy{Concurrency: 1},
		map[string]string{"changed": "com.github.aux.acme.planner.spool.changed"}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no event stream")
	require.Contains(t, err.Error(), "sink:planner")
}
