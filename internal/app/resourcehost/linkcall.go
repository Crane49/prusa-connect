package resourcehost

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/prusa3d/prusa-connect/internal/extension/bus"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/linkapi"
)

// linkChanges receives the link.changed call of a link module. A link module
// sends it when its status changes: the servers it connects and their state.
//
// The module sends the status and the host publishes the event
// event.TypeLinkStateChanged. A module cannot publish that type. Every event
// type a module emits through bus.emit starts with the namespace of the
// module, and no subscriber of this server listens for such a type.
//
// Each call has the whole status and not a difference. A subscriber that
// missed one event has the full status again after the next event.
type linkChanges struct {
	emitter  bus.Emitter
	instance ref.Ref

	// serving returns the reason this instance is not serving, or an empty
	// string when it is serving. Changed calls it on every call, because the
	// host builds a new target each time the module starts.
	serving func() string

	// claim records the servers that the status lists as connected through
	// this link. It returns an error for a status that lists a server another
	// link already connects. The instance may later act as node:<id> only for
	// the servers recorded here.
	claim func(context.Context, linkapi.Status) error

	// publish makes the events of this instance go out one at a time. The
	// event hub requires that for one origin. sdk/wirerpc runs every inbound
	// call on its own goroutine, so two calls of one module can arrive at the
	// same time.
	publish sync.Mutex

	// last is the reading of the call before this one, and seen says whether
	// there was one. Changed compares the two to find the servers that joined
	// and the connections that closed or opened again. Both are read and
	// written under publish.
	last linkapi.Status
	seen bool
}

var _ linkapi.Changes = (*linkChanges)(nil)

// Changed records the servers the status lists and then publishes the status
// as an event.
//
// It records first. When the status lists a server that another link already
// connects, Changed returns the error of claim and publishes no event. So a
// module cannot get the node id of another link by reporting it.
//
// After the whole reading it publishes what changed since the reading before
// it: a server that joined, a connection that closed or opened again, and the
// server above refusing this site's request to join. The whole reading gives
// the state and not what changed. An operator who sends events to a webhook
// or to a history store needs an event for each change.
func (c *linkChanges) Changed(ctx context.Context, params linkapi.ChangedParams) error {
	if reason := c.serving(); reason != "" {
		return fmt.Errorf("%w: %s", linkapi.ErrNotReady, reason)
	}

	if err := c.claim(ctx, params.Status); err != nil {
		return err
	}

	body, err := json.Marshal(params.Status)
	if err != nil {
		return fmt.Errorf("encode the link's reading for %s: %w", c.instance, err)
	}

	c.publish.Lock()
	defer c.publish.Unlock()

	changes := nodeChanges(c.instance.String(), c.last, params.Status, !c.seen)

	c.last, c.seen = params.Status, true

	if err = c.emitter.Emit(bus.Emission{
		Type: event.TypeLinkStateChanged,
		Key:  c.instance.String(),
		Body: string(body),
	}); err != nil {
		return fmt.Errorf("publish the link's reading for %s: %w", c.instance, err)
	}

	if err = publishNodeChanges(c.emitter, changes); err != nil {
		return fmt.Errorf("publish what changed under %s: %w", c.instance, err)
	}

	return nil
}

// newLinkChanges returns the handler for link.changed, or nil when the server
// has no event stream. With nil the host registers no link.changed method. A
// module that calls it gets CodeMethodNotFound and keeps running, and the
// host still reads the status of the link on every API request.
func newLinkChanges(host *Host, instance ref.Ref, emitter bus.Emitter) linkapi.Changes {
	if emitter == nil {
		return nil
	}

	return &linkChanges{
		emitter:  emitter,
		instance: instance,
		serving:  func() string { return notServing(host, instance) },
		claim: func(ctx context.Context, status linkapi.Status) error {
			return host.claimNodes(ctx, instance, status)
		},
	}
}
