package resource

import (
	"context"
	"time"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// Outcome is what a change to one instance requires. In place, restart and
// replace are separate outcomes, because an operator loses data when they are
// confused: a config edit restarts the instance and keeps its data, and a type
// edit replaces the instance with a different one.
//
// Start and Stop are for an instance that enters or leaves the merged
// document, or whose enabled switch an operator changed.
type Outcome uint8

const (
	// OutcomeNone is a declaration that did not change.
	OutcomeNone Outcome = iota
	// OutcomeInPlace is a change to host policy: applied without restarting the
	// instance, so the resource is never reconstructed for it.
	OutcomeInPlace
	// OutcomeRestart is a change to the settings the implementation reads: stop,
	// reconstruct, start. Same UID, same name, same data rows.
	OutcomeRestart
	// OutcomeReplace is a change to type or uid: a different instance, not an
	// update. The API refuses both, so this arises only from a hand edit.
	OutcomeReplace
	// OutcomeStart is an instance that is declared and enabled and was not
	// before.
	OutcomeStart
	// OutcomeStop is an instance that was removed from the document, or was
	// switched off.
	OutcomeStop
)

// String returns the outcome as it is rendered in logs.
func (o Outcome) String() string {
	switch o {
	case OutcomeNone:
		return "none"
	case OutcomeInPlace:
		return "in-place"
	case OutcomeRestart:
		return "restart"
	case OutcomeReplace:
		return "replace"
	case OutcomeStart:
		return "start"
	case OutcomeStop:
		return "stop"
	}

	return "unknown"
}

// Step is one instance's share of a plan.
type Step struct {
	// Outcome is what has to happen to it.
	Outcome Outcome
	// Ref is the instance.
	Ref ref.Ref
	// Desired is the declaration to converge on. It is zero for a stop.
	Desired Declaration
	// Previous is the declaration that was running. It is zero for a start.
	Previous Declaration
}

// Plan is what one reconcile has to carry out, ordered by kind then name so
// that two runs against the same pair of documents issue the same calls in the
// same order.
type Plan struct {
	// Steps are the instances that need a change. An instance that did not
	// change has no step, so an unchanged configuration is a plan with no
	// steps.
	Steps []Step
}

// Empty reports a plan with nothing to do.
func (p Plan) Empty() bool { return len(p.Steps) == 0 }

// Plane is the supervision plane as this package sees it: it carries out a
// plan and reports what it observes. It is one interface because the store
// needs both for every instance it returns, and one type implements both.
type Plane interface {
	// Reconcile carries out the plan. It drives the supervisor and implements
	// no lifecycle of its own.
	Reconcile(ctx context.Context, plan Plan) error
	// Status returns the supervision plane's view of one instance, and false
	// when it is not supervising it.
	Status(instance ref.Ref) (httpapi.ResourceStatus, bool)
	// Documented reports whether the instance is a running module that shipped
	// an OpenAPI document. The listing then shows the document's address. This
	// package builds the address from the namespace, so the plane returns no
	// path.
	Documented(instance ref.Ref) bool
	// UI is the browser part of a running module, for the listing to publish
	// beside the instance, and false for an instance that has none. The plane
	// renders it, because the entry address is built from the namespace the
	// bundle is mounted under and the entry the manifest declared, and only
	// the plane has read that manifest.
	UI(instance ref.Ref) (httpapi.ResourceUI, bool)
	// Database is the schema a running module's own database holds, as the
	// module reported it when it started, and false for an instance that is
	// not running or reported none.
	Database(instance ref.Ref) (wirerpc.DatabaseState, bool)
	// Restart rebuilds one instance from the declaration in force, and reports
	// false when the plane is supervising nothing under that name. It does not
	// change the declaration, so it needs operate_resource and not
	// configure_resource.
	Restart(instance ref.Ref) bool
	// MemoryHistory is what the instance's process used since the instant,
	// and the stops inside that window. It reports false for an instance that
	// is not a service module the plane has watched.
	MemoryHistory(instance ref.Ref, since time.Time) (httpapi.ResourceMemoryHistory, bool)
	// Log is the standard error the plane kept of the instance, and false
	// when it keeps none.
	Log(instance ref.Ref) (httpapi.ResourceLog, bool)
}

// Diff computes what has to happen to move from previous to next. Only Diff
// decides the outcomes, so a write and a startup get the same plan for the
// same change.
func Diff(previous, next Document) Plan {
	var plan Plan

	for _, desired := range next.Instances {
		was, existed := previous.Find(desired.Ref)
		if !existed {
			if desired.Spec.Enabled {
				plan.Steps = append(plan.Steps, Step{Outcome: OutcomeStart, Ref: desired.Ref, Desired: desired})
			}

			continue
		}

		if outcome := outcomeFor(was, desired); outcome != OutcomeNone {
			plan.Steps = append(plan.Steps, Step{
				Outcome: outcome, Ref: desired.Ref, Desired: desired, Previous: was,
			})
		}
	}

	// A removed instance is a stop even when it was switched off: nothing runs
	// to stop, but the plane drops what it kept for the instance across stops.
	for _, was := range previous.Instances {
		if _, still := next.Find(was.Ref); !still {
			plan.Steps = append(plan.Steps, Step{Outcome: OutcomeStop, Ref: was.Ref, Previous: was})
		}
	}

	return plan
}

// outcomeFor decides what one instance's change requires. The most severe
// change is checked first: a change of identity, then a change of the enabled
// switch, then a change of settings or policy.
//
// A replace is a stop and a start, so an identity change to an instance that
// is switched off replaces nothing: the old one is stopped if it ran, and the
// new one is not started.
func outcomeFor(was, desired Declaration) Outcome {
	switch {
	case (was.Spec.Type != desired.Spec.Type || was.UID != desired.UID) && desired.Spec.Enabled:
		return OutcomeReplace
	case was.Spec.Enabled && !desired.Spec.Enabled:
		return OutcomeStop
	case !was.Spec.Enabled && desired.Spec.Enabled:
		return OutcomeStart
	case !desired.Spec.Enabled:
		// The instance did not run and will not run, so there is nothing to do.
		return OutcomeNone
	case was.Spec.Equal(desired.Spec):
		return OutcomeNone
	}

	return restartOrPolicy(was, desired)
}

// restartOrPolicy separates a change that is only host policy from one that
// changes the implementation's own settings. Required and a service module's
// on_over_memory and switch_off are host policy, and the supervisor applies
// them without a restart. Any other change, the memory budget included, means
// the resource has to be rebuilt: a module learns its budget only at start,
// from PCL_MEMORY_BUDGET_BYTES and GOMEMLIMIT.
func restartOrPolicy(was, desired Declaration) Outcome {
	sameOtherwise := was.Spec
	sameOtherwise.Required = desired.Spec.Required

	if was.Spec.Service != nil && desired.Spec.Service != nil {
		service := *was.Spec.Service
		service.OnOverMemory = desired.Spec.Service.OnOverMemory
		service.SwitchOff = desired.Spec.Service.SwitchOff
		sameOtherwise.Service = &service
	}

	if sameOtherwise.Equal(desired.Spec) {
		return OutcomeInPlace
	}

	return OutcomeRestart
}
