package supervisor

import (
	"errors"

	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
)

// reasonDependenciesMet is what the DependenciesSatisfied condition carries once
// a start that was refused for a missing dependency has succeeded.
const reasonDependenciesMet = "DependenciesMet"

// DependencyRefusal is a start error saying the instance was not built because
// something it declared it needs is not there.
//
// The host that reads a declaration decides what a dependency is and which
// reason token a refusal carries. This package only writes the condition, so
// the supervisor does not have to know what a capability or a module type is.
type DependencyRefusal interface {
	error
	// DependencyReason is the CamelCase token the DependenciesSatisfied
	// condition carries. condition.ReasonDependencyFailed and
	// condition.ReasonDependencyCycle both read as failed, and any other token
	// leaves the instance on the retry ladder.
	DependencyReason() string
}

// waitingOnDependency reports whether a start error is a refusal that a retry
// can meet: the dependency is not serving yet. Whether a retry can meet it is
// the classification of the fault the refusal carries.
func waitingOnDependency(err error) bool {
	return dependencyReasonOf(err) != "" && Classify(err) == ClassRecoverable
}

// dependencyReasonOf is the reason a start error asks for, and the empty string
// for an error that is not about a dependency.
func dependencyReasonOf(err error) string {
	refusal, ok := errors.AsType[DependencyRefusal](err)
	if !ok {
		return ""
	}

	return refusal.DependencyReason()
}

// setDependencyLocked writes DependenciesSatisfied for a start that was refused
// for a missing dependency, and writes nothing for any other failure. The caller
// holds i.mu.
func (i *Instance[T]) setDependencyLocked(err error) bool {
	reason := dependencyReasonOf(err)
	if reason == "" {
		return false
	}

	return i.setConditionLocked(
		condition.TypeDependenciesSatisfied, condition.StatusFalse, reason, message(err))
}

// clearDependencyLocked records that a start succeeded, for an instance whose
// earlier start was refused for a missing dependency. The caller holds i.mu.
//
// An instance that never carried the condition does not gain it here. Every
// instance would otherwise publish a dependency condition, including the ones
// that declare no dependency at all.
func (i *Instance[T]) clearDependencyLocked() bool {
	if _, held := i.conditions.Get(condition.TypeDependenciesSatisfied); !held {
		return false
	}

	return i.setConditionLocked(
		condition.TypeDependenciesSatisfied, condition.StatusTrue, reasonDependenciesMet, "")
}
