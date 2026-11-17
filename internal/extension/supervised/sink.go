package supervised

import (
	"context"
	"errors"
	"fmt"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
)

// ErrNotASink reports an instance that cannot carry domain events: one the host
// is not supervising, one of another kind, or one whose target does not satisfy
// eventsapi.Processor.
var ErrNotASink = errors.New("supervised: the instance is not an event sink")

// ErrNotServing reports an instance with no target to read yet: one still
// starting, or one on the restart ladder. It means not available yet, not that
// it never will be. A caller reconciling on a timer can retry without logging,
// and report only a mismatch that will not change.
var ErrNotServing = errors.New("supervised: the instance is not serving")

// Sink is a supervised event processor. It satisfies eventsapi.Processor, so it
// is a drop-in for the unsupervised value.
//
// The contract's comment says an implementation "MUST NOT block (buffer
// internally) and MUST NOT panic". The host keeps the first promise: every
// method puts the work on the instance's bounded queue and returns. It keeps
// the second only as far as a recover reaches, which is the goroutine the drain
// runs on. See the supervisor package doc.
//
// The instance is type-erased, because the host that reconciles configuration
// supervises every kind through one loop, and two supervision loops over one
// name would each close a target the other built. So the assertion back to the
// contract happens per call, inside the supervised span, and a type declared
// under the sink kind that builds something else fails as a counted call and
// not as a panic on the fan-out goroutine. That per-call check is a backstop.
// AttachSink makes the same check once, before the sink is on the bus, because
// a counted failure per event tells nobody anything.
type Sink struct {
	instance *supervisor.Instance[any]
}

var (
	_ eventsapi.Processor      = (*Sink)(nil)
	_ eventsapi.SampleObserver = (*Sink)(nil)
)

// AttachSink wraps an instance the host is already supervising, which is how a
// sink declared under resources: is fed without a second supervision loop over
// one name.
//
// The kind alone is not enough, so the target is read too. See receivesEvents.
func AttachSink(instance *supervisor.Instance[any]) (*Sink, error) {
	if instance == nil {
		return nil, fmt.Errorf("%w: no instance", ErrNotASink)
	}

	if kind := instance.Ref().Kind(); kind != ref.KindSink {
		return nil, fmt.Errorf("%w: %s is a %s", ErrNotASink, instance.Ref(), kind)
	}

	if err := receivesEvents(instance); err != nil {
		return nil, err
	}

	return &Sink{instance: instance}, nil
}

// receivesEvents checks the instance against the contract before anything
// subscribes to feed it.
//
// The kind alone does not answer this. A type declared under the sink kind can
// build a target that satisfies nothing in sdk/eventsapi. A `runtime: service`
// instance is one: it launches a module reached over a session whose method set
// is `plugin.info` and `plugin.shutdown`, and the feed in internal/app leaves
// such an instance alone before it gets here. A builtin or script type that
// builds something other than a processor is the other.
//
// Attaching on the kind alone would subscribe such an instance to the hub,
// forward every window into its queue and count one failed call per event for
// the life of the process, while the instance reported ready. The refusal here
// says it once instead.
//
// A target that is not there yet is not a mismatch. An instance still starting,
// or one climbing the restart ladder, has nothing to contradict, so it is
// refused as not serving and the feed's next pass attaches it.
func receivesEvents(instance *supervisor.Instance[any]) error {
	mismatch, serving := supervisor.Peek(instance, func(target any) error {
		if _, ok := target.(eventsapi.Processor); ok {
			return nil
		}

		return fmt.Errorf("%w: %s built a %T, which is not an event processor, so its type "+
			"cannot receive events as a sink", ErrNotASink, instance.Ref(), target)
	})

	if !serving {
		return fmt.Errorf("%w: %w: %s is %s",
			ErrNotASink, ErrNotServing, instance.Ref(), instance.State())
	}

	return mismatch
}

// DefaultSinkPolicy is the host policy for an event sink: a bounded queue that
// drops the oldest item, and a breaker, because a dead webhook must not stall
// the bus and the newest fact is the one worth keeping.
func DefaultSinkPolicy() supervisor.Policy {
	policy := supervisor.DefaultPolicy()
	policy.Queue = supervisor.QueuePolicy{Depth: supervisor.DefaultQueueDepth, Drop: supervisor.DropOldest}

	return policy
}

// Instance returns the supervised instance, for the host that runs it and the
// API that reports on it.
func (s *Sink) Instance() *supervisor.Instance[any] { return s.instance }

// PrinterStateChanged queues the fact and returns.
func (s *Sink) PrinterStateChanged(state eventsapi.PrinterState) {
	s.offer("printer_state_changed", func(p eventsapi.Processor) {
		p.PrinterStateChanged(state)
	})
}

// PrinterLabelsChanged queues the fact and returns.
func (s *Sink) PrinterLabelsChanged(labels eventsapi.PrinterLabels) {
	s.offer("printer_labels_changed", func(p eventsapi.Processor) {
		p.PrinterLabelsChanged(labels)
	})
}

// PrinterDeleted queues the fact and returns.
func (s *Sink) PrinterDeleted(id string) {
	s.offer("printer_deleted", func(p eventsapi.Processor) {
		p.PrinterDeleted(id)
	})
}

// JobUpdated queues the fact and returns.
func (s *Sink) JobUpdated(job eventsapi.JobInfo) {
	s.offer("job_updated", func(p eventsapi.Processor) {
		p.JobUpdated(job)
	})
}

// LabelDeleted queues the fact and returns.
func (s *Sink) LabelDeleted(id string) {
	s.offer("label_deleted", func(p eventsapi.Processor) {
		p.LabelDeleted(id)
	})
}

// FactObserved queues the fact and returns.
func (s *Sink) FactObserved(fact eventsapi.Fact) {
	s.offer("fact_observed", func(p eventsapi.Processor) {
		p.FactObserved(fact)
	})
}

// SamplesObserved queues the samples and returns, for a processor that takes
// samples. For any other processor it does nothing. The check reads the
// target before the samples are queued, because a sink that does not take
// samples must not lose a durable fact from its queue to them.
func (s *Sink) SamplesObserved(samples eventsapi.Samples) {
	if !s.TakesSamples() {
		return
	}

	s.offer("samples_observed", func(p eventsapi.Processor) {
		if observer, ok := p.(eventsapi.SampleObserver); ok {
			observer.SamplesObserved(samples)
		}
	})
}

// TakesSamples reports whether the processor running now implements
// eventsapi.SampleObserver. A feed asks before it decodes a telemetry frame,
// so a sink that takes no samples costs the feed no work per frame.
func (s *Sink) TakesSamples() bool {
	takes, serving := supervisor.Peek(s.instance, func(target any) bool {
		_, ok := target.(eventsapi.SampleObserver)

		return ok
	})

	return serving && takes
}

// Close is a no-op. The supervisor owns the lifecycle: it calls the processor's
// Close when the instance stops, so a caller closing the wrapper would close a
// resource the host is still supervising.
func (s *Sink) Close() error { return nil }

// offer puts one fact on the queue. A contract method that returns nothing has
// nowhere to report a drop, so a drop is counted on the instance instead.
func (s *Sink) offer(op string, deliver func(eventsapi.Processor)) {
	_ = supervisor.Offer(s.instance, op, func(_ context.Context, target any) error {
		processor, mismatch := processorOf(target)
		if mismatch != nil {
			return mismatch
		}

		deliver(processor)

		return nil
	})
}

// processorOf reads the supervised target back as the contract. A sink type
// whose constructor builds something else is a build-time mistake that only a
// type-erased instance carries this far, so the error names it.
func processorOf(target any) (eventsapi.Processor, error) {
	processor, ok := target.(eventsapi.Processor)
	if !ok {
		return nil, fmt.Errorf("%w: it built a %T", ErrNotASink, target)
	}

	return processor, nil
}
