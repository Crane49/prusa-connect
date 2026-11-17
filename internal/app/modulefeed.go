package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// moduleFeed answers sdk/busapi's subscription half for one module instance.
//
// It is a hub subscriber like any other reader. The hub owns
// fan-out, the queue classes, coalescing and the authorization filter, and a
// second fan-out beside it would be a second copy of all four. A module is
// therefore fed the way the browser stream and the built-in sinks are fed, and
// it observes exactly what the subscription's principal was granted: the
// person it is serving, whom the supervision plane vouched for, or its own
// standing principal for a subscription naming nobody, which the operator's
// ceiling on the instance must list observe for.
type moduleFeed struct {
	hub *stream.Hub
	// standing is who a subscription naming nobody runs as, and what it may do.
	standing moduleStanding
}

var _ busapi.Feed = moduleFeed{}

// moduleFeeds binds an instance to the stream it may observe.
//
// It is a late binding for the reason moduleFleets is: the hub exists before
// the supervision plane, but the binding is kept in one place with the fleet's
// so a module's two inbound doors are wired together.
type moduleFeeds struct {
	hub *stream.Hub
}

func (m *moduleFeeds) forModule(module resourcehost.Module) busapi.Feed {
	if m.hub == nil {
		return nil
	}

	return moduleFeed{hub: m.hub, standing: standingOf(module)}
}

// Subscribe streams windows until the context ends or the module stops reading.
//
// The envelopes are handed over as the hub marshalled them. It marshals once
// per encoding variant and every reader shares that payload, so a module costs
// the hub what a browser costs it.
func (f moduleFeed) Subscribe(
	ctx context.Context, params busapi.SubscribeParams, write func(busapi.Window) error,
) error {
	origins, err := moduleOrigins(params.Origins)
	if err != nil {
		return err
	}

	principal, err := f.standing.as(params.Principal, authz.ActionObserve)
	if err != nil {
		return fmt.Errorf("%w: %s takes %s, and %w",
			busapi.ErrDenied, f.standing.principal, authz.ActionObserve, err)
	}

	request := stream.SubscribeRequest{
		Principal: principal,
		Holder:    f.holder(principal),
		Origins:   origins,
		Types:     params.Types,
		Encoding:  stream.EncodingJSON,
	}

	if params.IntervalMS > 0 {
		request.Interval = time.Duration(params.IntervalMS) * time.Millisecond
	}

	if params.Resume != "" {
		resume, parseErr := event.ParseCursor(params.Resume)
		if parseErr != nil {
			return fmt.Errorf("%w: the resume cursor could not be read: %w",
				busapi.ErrInvalidSubscription, parseErr)
		}

		request.Resume = resume
	}

	subscription, err := f.hub.Subscribe(ctx, request)
	if err != nil {
		return fmt.Errorf("%w: %w", busapi.ErrInvalidSubscription, err)
	}

	defer subscription.Close()

	for {
		select {
		case <-ctx.Done():
			return nil
		case batch, open := <-subscription.Batches():
			if !open {
				return nil
			}

			if err = write(moduleWindow(batch)); err != nil {
				return err
			}
		}
	}
}

// holder is the second name a subscription is counted against: the instance,
// for a subscription it opened as a server that joined this one.
//
// The hub bounds how many subscriptions one principal holds, and every server
// a link relays for is a principal of its own. A link that named a server per
// subscription would hold that bound once per name, and the names come from
// the link's own reading, so the module decides how many there are. Counting
// the instance as well bounds what one module can make this process hold.
//
// A subscription for a person is not counted against the instance. The host
// opened that request and ends it, and one module serving many people is not a
// module holding subscriptions of its own.
func (f moduleFeed) holder(principal string) string {
	if !namesANode(principal) {
		return ""
	}

	return f.standing.principal
}

// moduleWindow renders one batch as the window the contract publishes.
func moduleWindow(batch stream.Batch) busapi.Window {
	window := busapi.Window{
		Resync: batch.Resync,
		Cursor: batch.Cursor.String(),
	}

	for _, frame := range batch.Telemetry {
		window.Telemetry = append(window.Telemetry, json.RawMessage(frame.Bytes))
	}

	for _, frame := range batch.Events {
		window.Events = append(window.Events, json.RawMessage(frame.Bytes))
	}

	return window
}

// moduleOrigins reads the origins a subscription narrowed to.
//
// A malformed origin refuses the whole subscription rather than being dropped
// from it: a module that asked for two printers and was quietly given one
// watches a printer it does not know it is missing.
func moduleOrigins(named []string) ([]event.Origin, error) {
	if len(named) == 0 {
		return nil, nil
	}

	origins := make([]event.Origin, 0, len(named))

	for _, name := range named {
		origin, err := event.ParseOrigin(name)
		if err != nil {
			return nil, fmt.Errorf("%w: the origin %q could not be read: %w",
				busapi.ErrInvalidSubscription, name, err)
		}

		origins = append(origins, origin)
	}

	return origins, nil
}
