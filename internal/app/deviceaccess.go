package app

import (
	"context"
	"fmt"
	"slices"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// reachHolding is reach with the caller's whole action set on the printer. One
// statement produces both: the set is the row the point check tests, so the
// permission the operation needs is decided by the same read that fills the
// answer's actions member.
//
// The refusal is the one every path on this surface gives, with one exception.
// A printer the caller reaches nothing on, a printer this process has never
// been dialled by, and an id no printer could carry are the same answer, in
// that order, so nothing about what exists is learned from which refusal came
// back. The exception is a caller who can already read this printer: see
// classRefusal.
func (d *devices) reachHolding(
	ctx context.Context, principal string, id string, action authz.Action,
) (*device, []authz.Action, error) {
	who, err := authz.ParseSubject(principal)
	if err != nil {
		return nil, nil, fmt.Errorf("read the caller's principal: %w", err)
	}

	object, err := authz.NewObject(authz.TypeDevice, id)
	if err != nil {
		return nil, nil, notFound(id)
	}

	held, err := d.graph.ObjectActions(ctx, who, object)
	if err != nil {
		return nil, nil, err
	}

	entry, seen := d.known(id)
	if !slices.Contains(held, action) || !seen {
		return nil, nil, classRefusal(id, action, held, seen)
	}

	return entry, held, nil
}

// classRefusal is the refusal for a caller who does not hold the class this
// operation takes.
//
// It is 404 for almost everybody. A printer the caller holds nothing on reads
// exactly like one this server has never met, so a refusal never tells anybody
// a printer exists.
//
// A caller who holds observe on this machine is answered 403. They read it at
// GET /api/v1/devices/{id} and see it in their listing, so a 404 would hide
// nothing from them. It would mislead them: an operator who can see the
// printer and is told it does not exist goes looking for a lost machine, when
// the answer is that they hold observe and this takes operate.
//
// reachesVerb below answers 403 for the same reason, one step further in.
func classRefusal(id string, action authz.Action, held []authz.Action, seen bool) error {
	if !seen || !slices.Contains(held, authz.ActionObserve) {
		return notFound(id)
	}

	return fault.New(authz.CodeAccessDenied(),
		"the caller reads this printer and does not hold the class this operation takes").
		With(fault.StringParam("object", "device:"+id), fault.StringParam("action", action.String()))
}

// kindOf is the device kind the directory registered an id under. The verb
// table is keyed by that kind.
//
// It reads the directory and not the live entry, because the graph compiles
// against the directory and the two must not disagree about what a verb means
// on one machine. An id the directory does not hold reads as printer:
// the kind only classifies a verb, and a device nothing registered is
// refused by the reach check that follows whatever this answers.
func (d *devices) kindOf(ctx context.Context, id string) (authz.DeviceKind, error) {
	kind, err := d.graph.DeviceKind(ctx, id)
	if err != nil {
		return "", err
	}

	return kind, nil
}

// reachesVerb refuses a dangerous verb the caller does not reach.
//
// It is a second check and not a replacement for the class check. A verb no
// hand marked is conferred by its class and this answers yes; a marked one is
// conferred by no class, role or level, and reaching it takes a row naming the
// verb or ownership of the machine.
//
// The refusal is 403 and not the 404 the reach check gives, because the caller
// has already passed the reach check: they hold the class, so the device is in
// their listing and hiding it here would withhold nothing.
func (d *devices) reachesVerb(
	ctx context.Context, principal string, id string, kind authz.DeviceKind, verb string,
) error {
	if !authz.Dangerous(kind, verb) {
		return nil
	}

	who, err := authz.ParseSubject(principal)
	if err != nil {
		return fmt.Errorf("read the caller's principal: %w", err)
	}

	object, err := authz.NewObject(authz.TypeDevice, id)
	if err != nil {
		return notFound(id)
	}

	allowed, err := d.graph.CanVerb(ctx, who, object, verb)
	if err != nil {
		return err
	}

	if !allowed {
		return fault.New(authz.CodeAccessDenied(),
			"that verb is conferred by no role: it takes a grant naming the verb, or ownership of "+
				"the machine").
			With(fault.StringParam("object", "device:"+id), fault.StringParam("verb", verb))
	}

	return nil
}
