package supervised_test

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/extension/supervised"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
)

const sinkQueueDepth = 4

// sinkFixture builds a supervised sink over one fake processor.
func sinkFixture(t *testing.T, processor *fakeProcessor) (*supervised.Sink, func()) {
	t.Helper()

	return sinkFixtureOf(t, processor)
}

// sinkFixtureOf runs one sink instance over any processor and attaches the
// wrapper to it, the way the sink feed does. The Stop closes the processor,
// as the resource host's does. The returned func stops supervision.
func sinkFixtureOf(t *testing.T, processor eventsapi.Processor) (*supervised.Sink, func()) {
	t.Helper()

	instance, err := ref.New(ref.KindSink, "webhook")
	require.NoError(t, err)

	policy := supervised.DefaultSinkPolicy()
	policy.Backoff.Spread = 0
	policy.Queue = supervisor.QueuePolicy{Depth: sinkQueueDepth, Drop: supervisor.DropOldest}

	running, err := supervisor.New(supervisor.Config[any]{
		Ref:    instance,
		Type:   "fake",
		Policy: policy,
		Start:  func(context.Context, supervisor.Handle) (any, error) { return processor, nil },
		Stop: func(_ context.Context, target any) error {
			closer, ok := target.(interface{ Close() error })
			if !ok {
				return nil
			}

			return closer.Close()
		},
	})
	require.NoError(t, err)

	// A context from outside the synctest bubble is not bubbled, so a goroutine
	// blocked on its Done channel never counts as durably blocked.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)

		_ = running.Run(ctx)
	}()

	synctest.Wait()

	stop := func() {
		cancel()
		<-done
	}

	sink, err := supervised.AttachSink(running)
	if err != nil {
		stop()
		require.NoError(t, err)
	}

	return sink, stop
}

func TestSupervisedSinkDeliversThroughItsQueue(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		processor := newFakeProcessor(false)
		sink, stop := sinkFixture(t, processor)

		require.Equal(t, "ready", sink.Instance().State().String())

		sink.PrinterStateChanged(eventsapi.PrinterState{ID: "mk4-01", State: "PRINTING"})
		synctest.Wait()

		assert.Equal(t, 1, processor.delivered())
		assert.Zero(t, sink.Instance().Dropped())

		stop()

		assert.True(t, processor.isClosed(), "the supervisor owns the lifecycle, so it closes the processor")
	})
}

// samplingFake is a fake processor that also takes samples.
type samplingFake struct {
	*fakeProcessor

	samples atomic.Int32
}

func (p *samplingFake) SamplesObserved(eventsapi.Samples) { p.samples.Add(1) }

// TestASampleIsQueuedOnlyForAProcessorThatTakesIt holds the check the wrapper
// makes before it queues a sample (ADR-20261025-frz): a processor that takes samples
// is handed one, and a processor that does not costs no queue slot, so a
// telemetry burst never pushes a durable fact out of its queue.
func TestASampleIsQueuedOnlyForAProcessorThatTakesIt(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		sampling := &samplingFake{fakeProcessor: newFakeProcessor(false)}
		takes, stopTaking := sinkFixtureOf(t, sampling)

		plain := newFakeProcessor(false)
		refuses, stopRefusing := sinkFixture(t, plain)

		assert.True(t, takes.TakesSamples())
		assert.False(t, refuses.TakesSamples())

		for range 3 * sinkQueueDepth {
			samples := eventsapi.Samples{DeviceID: "mk4-01", Nodes: []eventsapi.Sample{{Path: "/tools/0/nozzle_temp"}}}
			takes.SamplesObserved(samples)
			refuses.SamplesObserved(samples)
		}

		refuses.PrinterStateChanged(eventsapi.PrinterState{ID: "mk4-01", State: "PRINTING"})
		synctest.Wait()

		assert.Positive(t, sampling.samples.Load(), "the processor that takes samples was handed none")
		assert.Zero(t, refuses.Instance().Dropped(), "a sample took a queue slot from a sink that refuses them")
		assert.Equal(t, 1, plain.delivered())

		stopTaking()
		stopRefusing()
	})
}

func TestSlowSinkDropsInsteadOfBlockingTheHub(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		processor := newFakeProcessor(true)
		sink, stop := sinkFixture(t, processor)

		// The first fact is picked up by the drain goroutine, which is now stuck
		// inside the third-party code.
		sink.PrinterStateChanged(eventsapi.PrinterState{ID: "mk4-01", State: "PRINTING"})
		<-processor.entered

		const offered = 20

		started := time.Now()

		for range offered {
			sink.PrinterStateChanged(eventsapi.PrinterState{ID: "mk4-01", State: "PRINTING"})
		}

		// The contract's comment tells an implementation it "MUST NOT block".
		// The host is what keeps that promise.
		assert.Zero(t, time.Since(started), "the hub's goroutine never waits on a sink")
		assert.Equal(t, int64(offered-sinkQueueDepth), sink.Instance().Dropped())
		assert.Zero(t, processor.delivered(), "nothing was delivered while the sink was stuck")

		close(processor.release)
		stop()
	})
}

// closeOnlyChild is what a `runtime: service` instance builds: a handle that
// stops the process and makes one control call, and nothing from any contract.
// It is written out here because internal/app/resourcehost's serviceChild is
// unexported, and this package may not import the composition root. What
// matters is that it satisfies nothing in sdk/eventsapi.
type closeOnlyChild struct{}

func (closeOnlyChild) Close() error { return nil }

// TestATargetThatIsNotAProcessorIsRefusedInsteadOfBeingFedIntoNothing covers a
// type under the sink kind that builds something other than an event
// processor: the instance launches, reports ready, and nothing ever gives the
// target it built an event.
//
// Attaching on the kind alone would subscribe the instance to the hub, and
// every window would enter its queue as a call that fails on the type
// assertion, one per event, for the life of the process, with nothing logged
// and readiness green. So the test asserts on the attach and not on a delivery
// counter. An operator is told once, where the sink would have gone on the bus.
func TestATargetThatIsNotAProcessorIsRefusedInsteadOfBeingFedIntoNothing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		instance := supervisedTarget(t, ref.KindSink, "example", closeOnlyChild{})

		_, err := supervised.AttachSink(instance)

		require.ErrorIs(t, err, supervised.ErrNotASink)
		require.ErrorContains(t, err, "sink:example built a supervised_test.closeOnlyChild",
			"the refusal does not name the instance and what it built")
		require.ErrorContains(t, err, "not an event processor",
			"the refusal does not say why the sink cannot receive events")
	})
}

// TestASinkThatDoesServeTheContractIsAttached is the other side of the same
// check, so the refusal above cannot pass by refusing everything.
func TestASinkThatDoesServeTheContractIsAttached(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		instance := supervisedTarget(t, ref.KindSink, "webhook", newFakeProcessor(false))

		attached, err := supervised.AttachSink(instance)
		require.NoError(t, err)
		require.Equal(t, instance, attached.Instance())
	})
}

// TestASinkThatIsNotServingIsNotAMismatch keeps the refusal from sticking to an
// instance that has not started yet. The feed reconciles on a timer and on
// every status change, so "not yet" has to read differently from "never".
func TestASinkThatIsNotServingIsNotAMismatch(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		instance, err := ref.New(ref.KindSink, "starting")
		require.NoError(t, err)

		// Never run, so there is no construction to read a target out of.
		unstarted, err := supervisor.New(supervisor.Config[any]{
			Ref:    instance,
			Type:   "webhook",
			Policy: supervised.DefaultSinkPolicy(),
			Start:  func(context.Context, supervisor.Handle) (any, error) { return closeOnlyChild{}, nil },
		})
		require.NoError(t, err)

		_, err = supervised.AttachSink(unstarted)
		require.ErrorIs(t, err, supervised.ErrNotASink)
		require.ErrorContains(t, err, "sink:starting is")
		require.NotContains(t, err.Error(), "not an event processor",
			"an instance that has not started was answered as a contract mismatch")
	})
}

// supervisedTarget runs one instance whose Start returns target, the way the
// resource host builds every runtime. The supervision plane is type-erased and
// the contract is asserted afterwards.
func supervisedTarget(t *testing.T, kind ref.Kind, name string, target any) *supervisor.Instance[any] {
	t.Helper()

	instance, err := ref.New(kind, name)
	require.NoError(t, err)

	policy := supervised.DefaultSinkPolicy()
	policy.Backoff.Spread = 0

	running, err := supervisor.New(supervisor.Config[any]{
		Ref:    instance,
		Type:   "example",
		Policy: policy,
		Start:  func(context.Context, supervisor.Handle) (any, error) { return target, nil },
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)

		_ = running.Run(ctx)
	}()

	synctest.Wait()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	require.Equal(t, "ready", running.State().String())

	return running
}

func TestSinkCloseIsTheSupervisorsToCall(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		processor := newFakeProcessor(false)
		sink, stop := sinkFixture(t, processor)
		defer stop()

		require.NoError(t, sink.Close())
		assert.False(t, processor.isClosed(), "a caller must not close a resource the host is supervising")
		assert.Equal(t, "ready", sink.Instance().State().String())
	})
}
