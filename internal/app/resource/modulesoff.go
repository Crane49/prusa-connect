package resource

import (
	"slices"
	"time"

	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

const (
	// ReasonModulesDisabled is the reason the listing gives an installed module
	// that does not start because the operator switched modules off.
	ReasonModulesDisabled = "ModulesDisabled"
	// ReasonSafeMode is the reason the listing gives a module that does not
	// start because the server runs in safe mode.
	ReasonSafeMode = "SafeMode"
)

// modulesKey is the operator's core.modules_enabled: false. since is when the
// store was built, and the listing's condition reports that time. safe
// extends the key to every module, installed or declared in the operator's
// file.
type modulesKey struct {
	off   bool
	safe  bool
	since time.Time
}

// Held reports whether the key keeps this declaration from running: modules
// are off, and the server installed it, or safe mode is on and it is a
// module. All code that says why an instance is not running calls it, so the
// answers agree.
func (s *Store) Held(declared Declaration) bool {
	return s.modules.off && (declared.Installed() || s.modules.safe && declared.Module())
}

// SafeMode reports whether the server runs in safe mode. Safe mode extends
// modules-off, so it is false while modules are on.
func (s *Store) SafeMode() bool { return s.modules.off && s.modules.safe }

// effective is the document the plane is asked to converge on. Every
// instance the host holds off is switched off in it. While modules are off,
// every module Held names is switched off too, whatever its enabled flag
// says.
//
// It changes both sides of the diff and never the document on disk. An
// instance that moves from the operator's file to the overlay while the
// server runs therefore changes from on to off, and the plane stops it. The
// enabled flag in the overlay is kept, so the module starts again when the
// key is set back on.
func (s *Store) effective(doc Document) Document {
	doc = s.withoutHeld(doc)
	if !s.modules.off {
		return doc
	}

	masked := doc
	masked.Instances = slices.Clone(doc.Instances)

	for i := range masked.Instances {
		if s.Held(masked.Instances[i]) {
			masked.Instances[i].Spec.Enabled = false
		}
	}

	return masked
}

// declaredStatus is the status the listing shows for an instance the plane
// has not reported on. A module the key keeps from running is disabled, with
// a condition that says why, so a reader does not wait for a pending instance
// that never starts.
func (s *Store) declaredStatus(declared Declaration) httpapi.ResourceStatus {
	switch {
	case s.Held(declared):
		return s.heldStatus(declared)
	case !declared.Spec.Enabled, s.heldByHost(declared.Ref):
		return httpapi.ResourceStatus{State: condition.StateDisabled().String()}
	}

	return httpapi.ResourceStatus{State: condition.StatePending().String()}
}

// heldByHost reports whether the host holds an instance of the operator's
// file off until the server restarts.
func (s *Store) heldByHost(instance ref.Ref) bool {
	_, held := s.hostHeld[instance]

	return held
}

// heldStatus is the status of a module the key keeps off.
func (s *Store) heldStatus(declared Declaration) httpapi.ResourceStatus {
	reason, message := ReasonModulesDisabled, "core.modules_enabled is false, so this server starts no module it installed"
	if s.modules.safe {
		reason, message = ReasonSafeMode, "the server runs in safe mode, so it starts no module"
	}

	return httpapi.ResourceStatus{
		State:              condition.StateDisabled().String(),
		ObservedGeneration: declared.Generation,
		Conditions: []httpapi.ResourceCondition{httpapi.NewResourceCondition(condition.Condition{
			Type:               condition.TypeStarted,
			Status:             condition.StatusFalse,
			Reason:             reason,
			Message:            message,
			LastTransitionTime: s.modules.since,
			ObservedGeneration: declared.Generation,
		})},
	}
}

// refuseSwitchingOn refuses a write that turns a held module from off to on
// while modules are off. Only that one change is refused. The web app sends
// enabled with every save, so a write that keeps an enabled module enabled is
// a settings change, and it is accepted.
func (s *Store) refuseSwitchingOn(current Declaration, exists bool, next Spec) error {
	if exists && s.Held(current) && next.Enabled && !current.Spec.Enabled {
		return httpfault.ModulesHeld(s.modules.safe)
	}

	return nil
}

// restart asks the plane to rebuild one declared instance. A held module is
// refused with the code for held modules. The plane would answer that the
// module is not running and advise a write to start it, and that write would
// be refused as well.
func (s *Store) restart(declared Declaration) error {
	if s.Held(declared) {
		return httpfault.ModulesHeld(s.modules.safe)
	}

	if s.plane == nil || !s.plane.Restart(declared.Ref) {
		return httpfault.ResourceNotRunning(declared.Ref.String())
	}

	return nil
}
