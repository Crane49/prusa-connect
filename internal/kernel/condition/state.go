package condition

// State is the narrow view a UI badge, a gauge and a hot-path predicate read.
//
// Its only field is unexported and Derive is the only way to get a populated
// one, so a caller cannot claim a state the conditions do not support. The zero
// State is not serving and matches no derived state.
type State struct {
	name string
}

// StateDisabled is an instance the operator turned off.
func StateDisabled() State { return State{name: "disabled"} }

// StateOrphaned is a ledger row that outlived its config entry.
func StateOrphaned() State { return State{name: "orphaned"} }

// StatePending is an instance constructed and waiting: for a dependency, or for
// its turn.
func StatePending() State { return State{name: "pending"} }

// StateStarting is an instance with Start in flight.
func StateStarting() State { return State{name: "starting"} }

// StateReady is an instance serving normally.
func StateReady() State { return State{name: "ready"} }

// StateDegraded is an instance serving with an abnormal condition holding.
func StateDegraded() State { return State{name: "degraded"} }

// StateRetrying is a recoverable failure with an attempt scheduled.
func StateRetrying() State { return State{name: "retrying"} }

// StateFailed is an instance that stays down until an operator acts.
func StateFailed() State { return State{name: "failed"} }

// StateStopping is an instance with Close in flight, draining inflight work.
func StateStopping() State { return State{name: "stopping"} }

// StateStopped is an instance closed cleanly this run.
func StateStopped() State { return State{name: "stopped"} }

// Lifecycle is what only the host knows and no condition can express: the
// operator's switch, whether a ledger row outlived its config entry, which
// lifecycle call is in flight, and how the host classified a failure.
type Lifecycle uint8

const (
	// LifecycleActive is an instance the host is not moving; the conditions
	// decide its state.
	LifecycleActive Lifecycle = iota
	LifecycleDisabled
	LifecycleOrphaned
	LifecycleStarting
	LifecycleStopping
	LifecycleStopped
	// LifecycleRetrying is a recoverable failure with an attempt scheduled. A
	// failure with no attempt scheduled and none in flight is permanent, so no
	// condition has to say whether a failure can be recovered from.
	LifecycleRetrying
	// LifecycleGaveUp is a failure with no retry attempts left.
	LifecycleGaveUp
)

// Valid reports whether lc is one of the declared positions.
func (lc Lifecycle) Valid() bool {
	switch lc {
	case LifecycleActive, LifecycleDisabled, LifecycleOrphaned, LifecycleStarting,
		LifecycleStopping, LifecycleStopped, LifecycleRetrying, LifecycleGaveUp:
		return true
	}
	return false
}

// Serving reports whether calls may be routed to the instance. A degraded
// instance serves: available and degraded are independent axes.
func (s State) Serving() bool { return s == StateReady() || s == StateDegraded() }

// String returns the state as it is rendered in the API and in metric labels.
// The zero State renders empty.
func (s State) String() string { return s.name }

// Derive computes the state from the host's lifecycle position and the
// conditions, first match wins.
//
// generation is the instance's current construction generation: a condition
// written from an earlier one describes an instance that no longer exists and
// reads as Unknown.
func Derive(generation int64, lc Lifecycle, s *Set) State {
	switch lc {
	case LifecycleDisabled:
		return StateDisabled()
	case LifecycleOrphaned:
		return StateOrphaned()
	case LifecycleStopping:
		return StateStopping()
	case LifecycleStopped:
		return StateStopped()
	case LifecycleActive, LifecycleStarting, LifecycleRetrying, LifecycleGaveUp:
		// the conditions decide
	default:
		// An unrecognised lifecycle is a host this build does not understand.
		// Failing closed keeps traffic off it. Failing open would route live
		// calls at a value nobody can name.
		return StateFailed()
	}

	if failed(generation, lc, s) {
		return StateFailed()
	}
	if lc == LifecycleRetrying {
		return StateRetrying()
	}
	if lc == LifecycleStarting {
		return StateStarting()
	}
	if pending(generation, s) {
		return StatePending()
	}
	if degraded(generation, s) {
		return StateDegraded()
	}
	return StateReady()
}

// failed reads Started=False as permanent only when no attempt is in flight and
// none is scheduled, which is how the host classifies it.
func failed(generation int64, lc Lifecycle, s *Set) bool {
	if lc == LifecycleGaveUp || s.statusAt(generation, TypeConfigured) == StatusFalse {
		return true
	}
	if s.statusAt(generation, TypeStarted) == StatusFalse &&
		lc != LifecycleRetrying && lc != LifecycleStarting {
		return true
	}
	dep, ok := s.at(generation, TypeDependenciesSatisfied)
	if !ok || dep.Status != StatusFalse {
		return false
	}
	return dep.Reason == ReasonDependencyFailed || dep.Reason == ReasonDependencyCycle
}

func pending(generation int64, s *Set) bool {
	return s.statusAt(generation, TypeStarted) == StatusUnknown ||
		s.statusAt(generation, TypeDependenciesSatisfied) == StatusFalse
}

// degraded leaves Reachable=Unknown out. Entering degraded has an anti-flap
// window, and that window belongs to the supervisor, which owns a clock and
// writes Reachable=False once the window has passed.
func degraded(generation int64, s *Set) bool {
	return s.statusAt(generation, TypeBreakerClosed) == StatusFalse ||
		s.statusAt(generation, TypeReachable) == StatusFalse
}
