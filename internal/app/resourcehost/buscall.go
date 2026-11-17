package resourcehost

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/prusa3d/prusa-connect/internal/extension/bus"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/busapi"
)

// gatedBus is the check every emission passes before it reaches the event
// stream. It refuses a module that is not serving, looks the endpoint up in
// the table the operator declared, publishes for one origin from one
// goroutine at a time, and limits how many goroutines wait to publish.
//
// A request into a module is bounded by supervisor.Call, a state call from
// one by gatedRecords, and an event from one by this. Without a limit a child
// process would decide how many of the host's goroutines are running.
//
// # Why one publisher per origin
//
// The package doc of internal/transport/stream says that events for one
// origin must be published from one goroutine. In the hub, a sequence number
// comes from an atomic counter and an offset comes from the origin's history
// under its own mutex. Two goroutines publishing for one origin can take
// sequence 5 and 6 and then append in the other order. A reader would then
// see offsets and sequence numbers in different orders.
//
// A script instance publishes from one goroutine: it drains one queue and
// calls the emitter directly. A module does not. sdk/wirerpc answers every
// inbound call on a goroutine of its own, so a module that emits from two
// handlers at once publishes for one origin from two goroutines. The publish
// lock here serialises them.
//
// # Why the bound is not supervisor.Call, and not gatedRecords either
//
// supervisor.Call would deadlock, for the reason gatedRecords gives: a module
// usually emits from its own route handler, and that handler already holds
// one of the instance's route slots.
//
// The semaphore is separate from the one for state calls because the two
// protect different resources. A state call reaches the host's database and
// holds a pooled connection while it runs. An emission takes this instance's
// publish lock. With one shared semaphore, publishes could use all the slots
// that state calls need, and the refusal a module author reads would name the
// wrong limit.
//
// There is no call budget here, because a budget would bound nothing.
// Hub.Publish answers from bounded queues and never blocks, so the publish
// lock is held only for the time one marshal takes.
type gatedBus struct {
	emitter  bus.Emitter
	instance ref.Ref

	// serving answers why this instance is not serving, and empty when it is.
	// It is asked per call, because the target of a module is rebuilt on
	// every start. A gate that kept the state of the run it was built for
	// would report that state after a restart.
	serving func() string

	// endpoints is the operator's table. It maps the name a module selects by
	// to the event type the host publishes for it. It is built at every start
	// from the declaration in force, so when an operator adds an endpoint the
	// instance restarts and the module reaches the new one.
	endpoints map[string]string

	// publish serialises this instance's emissions, because the hub requires
	// one publisher per origin.
	publish sync.Mutex
	// slots bounds how many goroutines may be waiting for that lock. It is nil
	// when the policy's Concurrency is zero, which means no limit.
	slots chan struct{}
	// width is the limit the refusal names, so a module author can read it.
	width int
}

var _ busapi.Publisher = (*gatedBus)(nil)

// Emit publishes one event of the instance, on the endpoint the operator
// declared for it.
//
// A consumer routes on the event type and the origin, and the caller sets
// neither. The type is looked up here, in a table the module never sees. The
// origin is bound to the emitter, which was built from the instance's ref.
// The module sends only an endpoint name, a conflation key and a payload.
//
// The three refusals are checked in this order. Not-ready means the module
// must wait. An undeclared endpoint means an operator must edit the
// declaration, and a retry does not help, so it is answered before a slot is
// taken. Busy means the module should retry. A module that read busy while it
// named an undeclared endpoint would retry and be refused every time.
func (g *gatedBus) Emit(_ context.Context, endpoint, key string, data json.RawMessage) error {
	if reason := g.serving(); reason != "" {
		return fmt.Errorf("%w: %s", busapi.ErrNotReady, reason)
	}

	eventType, declared := g.endpoints[endpoint]
	if !declared {
		return fmt.Errorf("%w: %s declares %s, and this emission names %q",
			busapi.ErrNoEndpoint, g.instance, g.declaredNames(), endpoint)
	}

	release, err := g.take()
	if err != nil {
		return err
	}

	defer release()

	// The body must be a JSON document, because the bus carries a CloudEvents
	// data member. An emission with no data is published as an empty object,
	// which a reader can decode.
	body := string(data)
	if body == "" {
		body = "{}"
	}

	g.publish.Lock()
	defer g.publish.Unlock()

	if err = g.emitter.Emit(bus.Emission{Type: eventType, Key: key, Body: body}); err != nil {
		return fmt.Errorf("publish %s for %s: %w", eventType, g.instance, err)
	}

	return nil
}

// take claims an admission slot, and refuses when there is none. It does not
// queue.
//
// sdk/wirerpc answers every inbound call on a goroutine of its own and bounds
// neither the goroutines nor the open streams. Without this limit a child
// process would decide how many of the host's goroutines wait for the publish
// lock. This limit exists to protect the host memory those goroutines use.
func (g *gatedBus) take() (func(), error) {
	if g.slots == nil {
		return func() {}, nil
	}

	select {
	case g.slots <- struct{}{}:
		return func() { <-g.slots }, nil
	default:
		return nil, fmt.Errorf("%w: %s already has %d publishing and that is the limit",
			busapi.ErrBusy, g.instance, g.width)
	}
}

// declaredNames renders the endpoints this instance has, so a refusal can
// list them.
func (g *gatedBus) declaredNames() string {
	if len(g.endpoints) == 0 {
		return "no endpoints"
	}

	names := make([]string, 0, len(g.endpoints))
	for name := range g.endpoints {
		names = append(names, name)
	}

	slices.Sort(names)

	return "the endpoints " + strings.Join(names, ", ")
}

// newBus builds the bus one module's session publishes through, and reports a
// declaration this host cannot serve.
//
// The width is read from the policy in force at this start, so a module rebuilt
// by a configuration change gets the limit of that policy. The supervisor's
// own semaphore and the state gate follow the same rule.
func newBus(
	host *Host, instance ref.Ref, policy supervisor.Policy,
	endpoints map[string]string, emitter bus.Emitter,
) (*gatedBus, error) {
	// An operator who declared endpoints on a deployment with no event stream
	// is told at start. Otherwise the module would fail on every event it
	// emits. A script's event: endpoint is refused in the same way.
	if emitter == nil {
		return nil, fmt.Errorf(
			"%s declares %d event endpoints and this host offers no event stream to publish them on",
			instance, len(endpoints))
	}

	built := &gatedBus{
		emitter:   emitter,
		instance:  instance,
		serving:   func() string { return notServing(host, instance) },
		endpoints: endpoints,
		width:     policy.Concurrency,
	}

	if policy.Concurrency > 0 {
		built.slots = make(chan struct{}, policy.Concurrency)
	}

	return built, nil
}
