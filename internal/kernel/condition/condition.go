// Package condition is the status shape a supervised instance reports:
// conditions in the Kubernetes shape, with a State derived from them and never
// assigned.
package condition

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

const (
	maxTypeLen    = 316
	maxReasonLen  = 1024
	maxMessageLen = 32768
)

var (
	// ErrInvalidCondition reports a condition that does not satisfy the shape.
	ErrInvalidCondition = errors.New("invalid condition")
	// ErrStaleGeneration reports a write carrying an older generation than the
	// condition it would overwrite.
	ErrStaleGeneration = errors.New("condition generation went backwards")
	// ErrNilSet reports a write against a nil *Set.
	ErrNilSet = errors.New("write to a nil condition set")
)

// tokenPattern is Kubernetes' own reason pattern: a CamelCase token a client can
// switch on. Condition types are held to it too, so a type carrying a newline or
// a JSON fragment cannot reach a metric label or a log line.
var tokenPattern = regexp.MustCompile(`^[A-Za-z]([A-Za-z0-9_,:]*[A-Za-z0-9_])?$`)

// Status is the tri-state of a condition.
type Status string

const (
	StatusTrue    Status = "True"
	StatusFalse   Status = "False"
	StatusUnknown Status = "Unknown"
)

// Type names one independent axis of an instance's status. The set is open:
// adding a type is additive, and a client that does not know one ignores it.
type Type string

// The condition types core writes. Every one of them is normal-true, so Derive
// reads them from a table and needs no per-type polarity.
const (
	TypeConfigured            Type = "Configured"
	TypeDependenciesSatisfied Type = "DependenciesSatisfied"
	TypeStarted               Type = "Started"
	TypeReachable             Type = "Reachable"
	TypeBreakerClosed         Type = "BreakerClosed"
)

// The reasons Derive reads. A writer chooses any other reason freely, because
// no derivation branches on it.
const (
	ReasonDependencyFailed = "DependencyFailed"
	ReasonDependencyCycle  = "DependencyCycle"
)

// Valid reports whether s is one of the declared statuses.
func (s Status) Valid() bool {
	switch s {
	case StatusTrue, StatusFalse, StatusUnknown:
		return true
	}
	return false
}

// String returns the status as it is rendered in the API.
func (s Status) String() string { return string(s) }

// Valid reports whether t is a CamelCase token within the length limit.
func (t Type) Valid() bool {
	return t != "" && len(t) <= maxTypeLen && tokenPattern.MatchString(string(t))
}

// String returns the type as it is rendered in the API.
func (t Type) String() string { return string(t) }

// Condition is one axis of an instance's status, in the field shape of
// Kubernetes' metav1.Condition.
type Condition struct {
	Type   Type
	Status Status
	// Reason is a CamelCase token a program can branch on. It is required, even
	// when Status is True.
	Reason string
	// Message is the human sentence. It may be empty.
	Message string
	// LastTransitionTime is owned by Set. It moves only when Status changes, so
	// a reader can see how long the current status has held.
	LastTransitionTime time.Time
	// ObservedGeneration is the instance generation the condition was written
	// from. It answers "is this status stale" without comparing clocks.
	ObservedGeneration int64
}

// Validate reports whether c satisfies the shape. Set calls it, so a Set never
// holds a condition that fails it.
func (c Condition) Validate() error {
	switch {
	case c.Type == "":
		return fmt.Errorf("%w: empty type", ErrInvalidCondition)
	case len(c.Type) > maxTypeLen:
		return fmt.Errorf("%w: type is %d bytes, the limit is %d", ErrInvalidCondition, len(c.Type), maxTypeLen)
	case !c.Type.Valid():
		return fmt.Errorf("%w: type %q is not a CamelCase token", ErrInvalidCondition, c.Type)
	case !c.Status.Valid():
		return fmt.Errorf("%w: %q status %q is not True, False or Unknown", ErrInvalidCondition, c.Type, c.Status)
	case c.Reason == "":
		return fmt.Errorf("%w: %q has no reason", ErrInvalidCondition, c.Type)
	case len(c.Reason) > maxReasonLen:
		return fmt.Errorf("%w: %q reason is %d bytes, the limit is %d",
			ErrInvalidCondition, c.Type, len(c.Reason), maxReasonLen)
	case !tokenPattern.MatchString(c.Reason):
		return fmt.Errorf("%w: %q reason %q is not a CamelCase token", ErrInvalidCondition, c.Type, c.Reason)
	case len(c.Message) > maxMessageLen:
		return fmt.Errorf("%w: %q message is %d bytes, the limit is %d",
			ErrInvalidCondition, c.Type, len(c.Message), maxMessageLen)
	case c.ObservedGeneration < 0:
		return fmt.Errorf("%w: %q generation is %d", ErrInvalidCondition, c.Type, c.ObservedGeneration)
	}
	return nil
}

// Stale reports whether the condition was written from an older generation than
// the one asked about. Derive uses it to ignore a condition that describes an
// earlier generation of the instance.
func (c Condition) Stale(generation int64) bool { return c.ObservedGeneration < generation }

// Set is the conditions held for one instance, keyed by Type and kept in
// first-write order. The zero value is an empty set.
//
// A nil *Set reads as an empty set and refuses writes with ErrNilSet, so a
// caller that has not constructed one gets an error and not a panic.
//
// It is not safe for concurrent use: the supervisor that owns an instance owns
// its Set.
type Set struct {
	conditions []Condition
}

// Set records c and reports whether its Status changed. Reason, Message and
// ObservedGeneration are written on every call; LastTransitionTime is written
// from now only on a change, and a first write counts as a change.
//
// A write carrying an older generation than the condition it would overwrite is
// refused: accepting it would pin ObservedGeneration in the past and leave every
// later read reporting a fresh condition as stale.
func (s *Set) Set(now time.Time, c Condition) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("%w: %q", ErrNilSet, c.Type)
	}
	if err := c.Validate(); err != nil {
		return false, err
	}
	for i := range s.conditions {
		if s.conditions[i].Type != c.Type {
			continue
		}
		if c.ObservedGeneration < s.conditions[i].ObservedGeneration {
			return false, fmt.Errorf("%w: %q holds generation %d, the write carries %d",
				ErrStaleGeneration, c.Type, s.conditions[i].ObservedGeneration, c.ObservedGeneration)
		}
		changed := s.conditions[i].Status != c.Status
		c.LastTransitionTime = s.conditions[i].LastTransitionTime
		if changed {
			c.LastTransitionTime = now
		}
		s.conditions[i] = c
		return changed, nil
	}
	c.LastTransitionTime = now
	s.conditions = append(s.conditions, c)
	return true, nil
}

// Get returns the condition of type t.
func (s *Set) Get(t Type) (Condition, bool) {
	if s == nil {
		return Condition{}, false
	}
	for _, c := range s.conditions {
		if c.Type == t {
			return c, true
		}
	}
	return Condition{}, false
}

// StatusOf returns the status of type t, treating an absent condition as
// Unknown so a producer that skips one is tolerated.
func (s *Set) StatusOf(t Type) Status {
	c, ok := s.Get(t)
	if !ok {
		return StatusUnknown
	}
	return c.Status
}

// All returns a copy of the conditions in first-write order.
func (s *Set) All() []Condition {
	if s == nil {
		return nil
	}
	out := make([]Condition, len(s.conditions))
	copy(out, s.conditions)
	return out
}

// at returns the condition of type t only if it was written from generation or
// later. A condition left over from an earlier generation describes an instance
// that no longer exists, so Derive must not read it.
func (s *Set) at(generation int64, t Type) (Condition, bool) {
	c, ok := s.Get(t)
	if !ok || c.Stale(generation) {
		return Condition{}, false
	}
	return c, true
}

func (s *Set) statusAt(generation int64, t Type) Status {
	c, ok := s.at(generation, t)
	if !ok {
		return StatusUnknown
	}
	return c.Status
}
