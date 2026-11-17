package resourcehost

import (
	"context"
	"encoding/json"
	"slices"
	"sync"

	"github.com/prusa3d/prusa-connect/internal/extension/bus"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/uiapi"
)

// uiHidden is the list each running module hides of its own interface, by
// instance. It is kept in memory and never written to the overlay. The module
// derives it from its own records and sends it again after every start, so a
// stored list could be left from an older release of the module.
//
// It has a lock of its own and does not use h.mu, because the listing reads
// it for every module on every GET of the resources, and a start holds h.mu
// while it claims a namespace.
type uiHidden struct {
	mu   sync.Mutex
	held map[ref.Ref][]string
}

// set replaces one instance's list and reports whether it changed. An empty
// list forgets the instance.
func (u *uiHidden) set(instance ref.Ref, hidden []string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()

	if slices.Equal(u.held[instance], hidden) {
		return false
	}

	if len(hidden) == 0 {
		delete(u.held, instance)

		return true
	}

	if u.held == nil {
		u.held = map[ref.Ref][]string{}
	}

	u.held[instance] = slices.Clone(hidden)

	return true
}

// of is a copy of one instance's list, and nil when it hides nothing.
func (u *uiHidden) of(instance ref.Ref) []string {
	u.mu.Lock()
	defer u.mu.Unlock()

	return slices.Clone(u.held[instance])
}

// forget drops one instance's list. A start calls it, because a process that
// just started has sent no list, and a stop calls it too.
func (u *uiHidden) forget(instance ref.Ref) {
	u.set(instance, nil)
}

// uiHider answers ui.hide for one instance. It can change only the list of
// the instance it was built for: the wire carries no instance, so a module
// cannot hide another module's entries.
type uiHider struct {
	host     *Host
	instance ref.Ref
	emitter  bus.Emitter

	// publish keeps the events of this instance in the order the lists were
	// stored. sdk/wirerpc runs every inbound call on its own goroutine, so two
	// calls of one module can arrive at the same time.
	publish sync.Mutex
}

var _ uiapi.Hider = (*uiHider)(nil)

// uiChange is the data of event.TypeResourceUIChanged.
type uiChange struct {
	Resource string   `json:"resource"`
	Hidden   []string `json:"hidden"`
}

// Hide stores the module's whole list, and publishes it when it differs from
// the one held. A list that could not be published is still stored. The
// listing returns it, so a web app that missed the event gets it on its next
// read of the listing.
func (u *uiHider) Hide(ctx context.Context, hidden []string) error {
	u.publish.Lock()
	defer u.publish.Unlock()

	if !u.host.hidden.set(u.instance, hidden) || u.emitter == nil {
		return nil
	}

	if hidden == nil {
		hidden = []string{}
	}

	body, err := json.Marshal(uiChange{Resource: u.instance.String(), Hidden: hidden})
	if err == nil {
		err = u.emitter.Emit(bus.Emission{
			Type: event.TypeResourceUIChanged,
			Key:  u.instance.String(),
			Body: string(body),
		})
	}

	if err != nil {
		u.host.cfg.Logger.WarnContext(ctx, "the list a module hides could not be published",
			"resource", u.instance.String(), "error", err.Error())
	}

	return nil
}

// hiderFor is the ui.hide method of one instance. The instance's list is
// forgotten here, because this runs at every start of the process.
func (h *Host) hiderFor(instance ref.Ref) uiapi.Hider {
	h.hidden.forget(instance)

	return &uiHider{host: h, instance: instance, emitter: h.emitterFor(instance)}
}
