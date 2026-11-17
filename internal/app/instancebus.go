package app

import (
	"encoding/json"

	"github.com/prusa3d/prusa-connect/internal/extension/bus"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// instanceEmitter binds each supervised instance to this server's own event
// stream.
//
// It lives in the composition root because internal/extension may not import a
// transport, and because the origin is the instance's and not the extension's.
// What a script or a module publishes is an event of the instance that
// published it. The origin is fixed here before either one runs.
//
// One emitter serves both tiers, so the origin is decided in one place. A
// script reaches it through the endpoint table in its settings. A module
// reaches it over sdk/busapi, through the endpoints its declaration names.
//
// An instance whose name cannot be rendered as an origin is given no bus, so a
// destination it declares is refused at start and does not fail on every
// event.
func instanceEmitter(hub *stream.Hub, meters fleetMeters) func(ref.Ref) bus.Emitter {
	return func(instance ref.Ref) bus.Emitter {
		origin, err := event.NewOrigin(event.Kind(instance.Kind()), instance.ID())
		if err != nil {
			return nil
		}

		return instanceBus{hub: hub, origin: origin, meters: meters}
	}
}

// instanceBus is one instance's half of the event stream.
//
// The operator chooses the type: a script's comes from its endpoint table, and
// a module's is the module's own namespace joined to a name the operator
// wrote. The origin is the instance's. A reader routes on the type and the
// origin, and an extension chooses neither.
type instanceBus struct {
	hub    *stream.Hub
	origin event.Origin
	meters fleetMeters
}

var _ bus.Emitter = instanceBus{}

// Emit publishes one emission.
//
// The class is state: what an extension emits is a transition to keep, and not
// a measurement the next one supersedes. History is kept per origin and is
// bounded, so an instance emitting quickly fills its own window and no
// printer's.
//
// Hub.Publish answers from bounded queues and never blocks. So the goroutine
// draining a script's queue and the handler answering a module's call each
// call it directly.
func (b instanceBus) Emit(emission bus.Emission) error {
	_, err := b.hub.Publish(stream.Publication{
		Origin: b.origin,
		Type:   emission.Type,
		Class:  event.ClassState,
		Key:    emission.Key,
		Data:   json.RawMessage(emission.Body),
	})
	if err != nil {
		return err
	}

	b.meters.published(emission.Type)

	return nil
}
