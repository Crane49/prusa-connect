package supervisor

import (
	"fmt"
	"time"

	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// Result says why an instance last stopped or failed. It is separate from the
// state. The state says where the instance is, the result says how it got
// there, and the result stays until the next attempt clears it.
type Result string

// The results this plane can produce. Dependency ordering and config decoding
// happen before an instance exists, so their results are not here.
const (
	// ResultSuccess is stopped cleanly, or never failed.
	ResultSuccess Result = "success"
	// ResultStartError is Start returning an error.
	ResultStartError Result = "start-error"
	// ResultRuntimeError is a failure the resource reported after Start
	// returned.
	ResultRuntimeError Result = "runtime-error"
	// ResultTimeout is Start or Close exceeding its budget.
	ResultTimeout Result = "timeout"
	// ResultPanic is a recovered panic.
	ResultPanic Result = "panic"
	// ResultGiveUp is a retry ladder that ran out.
	ResultGiveUp Result = "give-up"
	// ResultStoppedByOperator tells a stop somebody asked for from a crash.
	// Without it, a supervisor restarts what an operator just turned off.
	ResultStoppedByOperator Result = "stopped-by-operator"
	// ResultOverMemory is a process stopped for going over its memory budget.
	ResultOverMemory Result = "over-memory"
	// ResultNotResponding is a process stopped because it stopped answering.
	ResultNotResponding Result = "not-responding"
	// ResultExited is a process that ended on its own with an exit code.
	ResultExited Result = "exited"
	// ResultSignal is a process ended by a signal it did not ask for.
	ResultSignal Result = "signal"
	// ResultSwitchedOffByHost is an instance that failed too often inside the
	// failure window. It stays off until an operator acts.
	ResultSwitchedOffByHost Result = "switched-off-by-host"
)

// Stop says why a process went down. A *Stop passed to Reporter.Recoverable
// sets the result the instance publishes, and is kept in Status.RecentStops.
type Stop struct { //nolint:errname // a record kept in Status first, an error second
	Result Result
	// At is when it stopped. A zero value is filled in when it is recorded.
	At time.Time
	// Used and Budget are bytes, set for ResultOverMemory only.
	Used   int64
	Budget int64
	// Code is set for ResultExited only.
	Code int
	// Signal is set for ResultSignal only, for example "SIGSEGV".
	Signal string
}

func (s *Stop) Error() string {
	switch s.Result { //nolint:exhaustive // every other result prints its own name
	case ResultOverMemory:
		return fmt.Sprintf("used %d bytes of a %d byte memory budget", s.Used, s.Budget)
	case ResultExited:
		return fmt.Sprintf("exited with code %d", s.Code)
	case ResultSignal:
		return "ended by " + s.Signal
	default:
		return string(s.Result)
	}
}

// String returns the result as it is rendered in the API and in logs.
func (r Result) String() string { return string(r) }

// Status is everything published about one instance at one moment. It carries
// the whole status and not a change set, so a consumer that missed an event
// does not have to reconstruct the state.
type Status struct {
	// Ref is the operator-chosen name.
	Ref ref.Ref
	// UID is the host-minted identity.
	UID ref.UID
	// Type is the registered type name the instance was built from.
	Type string
	// State is derived from Conditions and the host's lifecycle position. It is
	// never assigned directly.
	State condition.State
	// Result says why it last stopped or failed.
	Result Result
	// Conditions are the independent axes, in first-write order.
	Conditions []condition.Condition
	// Generation counts constructions. A condition carrying an older
	// generation describes a target that no longer exists.
	Generation int64
	// Attempt is how many starts this ladder has made.
	Attempt int
	// NextRetryAt is when the next attempt is due, zero when none is scheduled.
	NextRetryAt time.Time
	// Dropped is how many offered items the queue has lost.
	Dropped int64
	// RecentStops are the failures inside the failure window, oldest first.
	// It is empty when the policy has no failure window.
	RecentStops []Stop
	// HangEvidenceAt is when a call last ran into the host's deadline or found
	// every bulkhead slot taken, zero when none has. Failures answered promptly
	// and breaker refusals are not evidence.
	HangEvidenceAt time.Time
}

// Serving reports whether calls may be routed to the instance.
func (s Status) Serving() bool { return s.State.Serving() }
