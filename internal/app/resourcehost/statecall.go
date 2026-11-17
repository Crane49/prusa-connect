package resourcehost

import (
	"context"
	"fmt"
	"time"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/stateapi"
)

// gatedRecords is the check every state call passes before it reaches the
// store. It refuses a call from a module that is not serving, and it limits
// how many state calls of one instance run at once.
//
// A request into a module goes through moduleHandler, which bounds it with
// supervisor.Call. A state call from a module goes through this. Without
// these checks a module could read and write while its own migration is
// running, and could run as many calls in the host's database at once as it
// can open streams.
//
// # Why this is not supervisor.Call
//
// supervisor.Call would give the readiness gate, the bulkhead, the deadline
// and the panic recovery. It is not used here for two reasons.
//
// It would deadlock. A module usually makes a state call from its own route
// handler, and that handler already runs inside a supervisor.Call that holds
// one of the instance's bulkhead slots. A state call that took a slot from the
// same semaphore would wait for a slot its caller holds. With eight slots,
// eight concurrent requests to a route that reads a record would take all
// eight and then wait for each other until the call budget expired.
//
// It would also count failures against the wrong party. A state call that
// fails has failed in the host's own database. Recording it against the
// module's circuit breaker would open the breaker and refuse the routes of a
// module that did nothing wrong.
//
// The limit here is therefore the instance's Concurrency from the same
// policy, on a separate semaphore.
type gatedRecords struct {
	inner    stateapi.Records
	instance ref.Ref

	// serving answers why this instance is not serving, and empty when it is.
	serving func() string

	// slots is the bulkhead. It is nil when the policy's Concurrency is zero,
	// which means no limit.
	slots chan struct{}
	// width is the limit the refusal names, so a module author can read it.
	width int
	// budget bounds one state call. The session a state call arrives on has no
	// deadline of its own: the wire handler runs under the connection's
	// context, which ends when the process does. Without this a statement
	// that never returns would hold a slot for the life of the module.
	budget time.Duration
}

var _ stateapi.Records = (*gatedRecords)(nil)

// gate runs one state call under the readiness check and the bound.
//
// Not-ready is checked before busy. Not-ready means the host will not run the
// call yet. Busy means the host would run it and has no free slot. A module
// that read busy while it was not ready would retry and be refused every
// time.
func (g *gatedRecords) gate(ctx context.Context, run func(context.Context) error) error {
	if err := g.ready(); err != nil {
		return err
	}

	release, err := g.take()
	if err != nil {
		return err
	}

	defer release()

	if g.budget <= 0 {
		return run(ctx)
	}

	callCtx, cancel := context.WithTimeout(ctx, g.budget)
	defer cancel()

	return run(callCtx)
}

// ready refuses a call from a module the host has not marked ready.
//
// A module runs its own migration before it reports ready, and the host
// routes no request to a module that is not ready. State calls follow the
// same rule: a module that is still migrating must not read and write the
// store that its migration is about to rewrite.
//
// The instance is resolved per call, because the target of a module is
// rebuilt on every start. A handler that kept a pointer would report the
// state of the run it was built for, even after a restart.
func (g *gatedRecords) ready() error {
	if reason := g.serving(); reason != "" {
		return fmt.Errorf("%w: %s", stateapi.ErrNotReady, reason)
	}

	return nil
}

// notServing says why one instance is not serving, and empty when it is.
//
// The state gate and the bus gate both call it, so the check exists once.
// Each gate wraps the reason in the error of its own contract, so this
// returns a reason and not an error.
func notServing(host *Host, instance ref.Ref) string {
	running, ok := host.Instance(instance)
	if !ok {
		return instance.String() + " is not supervised"
	}

	if state := running.State(); !state.Serving() {
		return fmt.Sprintf("%s is %s", instance, state)
	}

	return ""
}

// take claims a slot, and refuses when there is none. It does not queue.
//
// The other limits of the state store also answer with a coded refusal and
// not with a wait: a key outside the grammar, a value over the size, a
// namespace over its bytes and a rate over its limit. Queueing would also not
// protect the host. sdk/wirerpc answers every inbound call on a goroutine of
// its own and bounds neither the goroutines nor the open streams, so waiting
// calls would pile up as goroutines. This limit exists to protect the host
// memory those goroutines use.
func (g *gatedRecords) take() (func(), error) {
	if g.slots == nil {
		return func() {}, nil
	}

	select {
	case g.slots <- struct{}{}:
		return func() { <-g.slots }, nil
	default:
		return nil, fmt.Errorf("%w: %s already has %d running and that is the limit",
			stateapi.ErrBusy, g.instance, g.width)
	}
}

func (g *gatedRecords) Get(ctx context.Context, key stateapi.Key) (stateapi.Value, error) {
	var held stateapi.Value

	err := g.gate(ctx, func(callCtx context.Context) error {
		var inner error
		held, inner = g.inner.Get(callCtx, key)

		return inner
	})

	return held, err
}

func (g *gatedRecords) Put(ctx context.Context, key stateapi.Key, value []byte) (uint64, error) {
	var version uint64

	err := g.gate(ctx, func(callCtx context.Context) error {
		var inner error
		version, inner = g.inner.Put(callCtx, key, value)

		return inner
	})

	return version, err
}

func (g *gatedRecords) CompareAndPut(
	ctx context.Context, key stateapi.Key, expected uint64, value []byte,
) (uint64, error) {
	var version uint64

	err := g.gate(ctx, func(callCtx context.Context) error {
		var inner error
		version, inner = g.inner.CompareAndPut(callCtx, key, expected, value)

		return inner
	})

	return version, err
}

func (g *gatedRecords) Delete(ctx context.Context, key stateapi.Key) error {
	return g.gate(ctx, func(callCtx context.Context) error {
		return g.inner.Delete(callCtx, key)
	})
}

func (g *gatedRecords) List(ctx context.Context, prefix stateapi.Key) ([]stateapi.Key, error) {
	var keys []stateapi.Key

	err := g.gate(ctx, func(callCtx context.Context) error {
		var inner error
		keys, inner = g.inner.List(callCtx, prefix)

		return inner
	})

	return keys, err
}

func (g *gatedRecords) Version(ctx context.Context, key stateapi.Key) (uint64, error) {
	var version uint64

	err := g.gate(ctx, func(callCtx context.Context) error {
		var inner error
		version, inner = g.inner.Version(callCtx, key)

		return inner
	})

	return version, err
}

func (g *gatedRecords) Usage(ctx context.Context) (stateapi.Usage, error) {
	var usage stateapi.Usage

	err := g.gate(ctx, func(callCtx context.Context) error {
		var inner error
		usage, inner = g.inner.Usage(callCtx)

		return inner
	})

	return usage, err
}

// newGate builds the gate for one module's session.
//
// The width and the budget are read from the policy in force at this start, so
// a module rebuilt by a configuration change gets the limits of that policy.
// The supervisor's own semaphore follows the same rule: a width change takes
// effect at the next start.
//
// A read takes a slot like a write. The write path takes the namespace's
// quota lock before it opens its transaction, so one namespace has at most
// one write inside the database at a time, and a waiting writer holds no
// connection. A read takes no lock and holds a pooled connection for as long
// as it runs, so reads are the calls that can use up the pool the host shares
// with its own queries. A wider limit for reads would loosen the only calls
// that are not already serialised.
func newGate(host *Host, instance ref.Ref, policy supervisor.Policy, inner stateapi.Records) *gatedRecords {
	gate := &gatedRecords{
		inner:    inner,
		instance: instance,
		serving:  func() string { return notServing(host, instance) },
		width:    policy.Concurrency,
		budget:   policy.CallTimeout,
	}

	if policy.Concurrency > 0 {
		gate.slots = make(chan struct{}, policy.Concurrency)
	}

	return gate
}
