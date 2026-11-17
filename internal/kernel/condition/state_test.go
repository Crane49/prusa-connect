package condition

import (
	"testing"
	"time"
)

// generation is the instance generation every case in this file writes from,
// except the staleness cases which say so.
const generation = 3

func TestDerive(t *testing.T) {
	t.Parallel()

	// A condition set that would otherwise derive as ready, so each case shows
	// only what it changes.
	healthy := []Condition{
		{Type: TypeConfigured, Status: StatusTrue, Reason: "Validated"},
		{Type: TypeDependenciesSatisfied, Status: StatusTrue, Reason: "AllServing"},
		{Type: TypeStarted, Status: StatusTrue, Reason: "Started"},
		{Type: TypeReachable, Status: StatusTrue, Reason: "Probed"},
		{Type: TypeBreakerClosed, Status: StatusTrue, Reason: "Closed"},
	}

	tests := []struct {
		name       string
		generation int64
		lifecycle  Lifecycle
		conds      []Condition
		want       State
	}{
		{
			name:      "the operator turned it off",
			lifecycle: LifecycleDisabled,
			conds:     healthy,
			want:      StateDisabled(),
		},
		{
			name:      "disabled outranks a broken config",
			lifecycle: LifecycleDisabled,
			conds:     []Condition{{Type: TypeConfigured, Status: StatusFalse, Reason: "ConfigInvalid"}},
			want:      StateDisabled(),
		},
		{
			name:      "the ledger row outlived its entry",
			lifecycle: LifecycleOrphaned,
			conds:     healthy,
			want:      StateOrphaned(),
		},
		{
			name:      "close in flight",
			lifecycle: LifecycleStopping,
			conds:     healthy,
			want:      StateStopping(),
		},
		{
			name:      "closed this run",
			lifecycle: LifecycleStopped,
			conds:     healthy,
			want:      StateStopped(),
		},
		{
			name:      "config rejected",
			lifecycle: LifecycleActive,
			conds:     []Condition{{Type: TypeConfigured, Status: StatusFalse, Reason: "ConfigInvalid"}},
			want:      StateFailed(),
		},
		{
			name:      "config rejected while starting",
			lifecycle: LifecycleStarting,
			conds:     []Condition{{Type: TypeConfigured, Status: StatusFalse, Reason: "SchemaViolation"}},
			want:      StateFailed(),
		},
		{
			name:      "a dependency failed permanently",
			lifecycle: LifecycleActive,
			conds: replace(healthy, Condition{
				Type: TypeDependenciesSatisfied, Status: StatusFalse, Reason: ReasonDependencyFailed,
			}),
			want: StateFailed(),
		},
		{
			name:      "a dependency cycle",
			lifecycle: LifecycleActive,
			conds: replace(healthy, Condition{
				Type: TypeDependenciesSatisfied, Status: StatusFalse, Reason: ReasonDependencyCycle,
			}),
			want: StateFailed(),
		},
		{
			name:      "start failed with no attempt scheduled or in flight",
			lifecycle: LifecycleActive,
			conds: replace(healthy, Condition{
				Type: TypeStarted, Status: StatusFalse, Reason: "AuthRejected",
			}),
			want: StateFailed(),
		},
		{
			name:      "the retry ladder ran out",
			lifecycle: LifecycleGaveUp,
			conds:     healthy,
			want:      StateFailed(),
		},
		{
			name:      "start failed with an attempt scheduled",
			lifecycle: LifecycleRetrying,
			conds: replace(healthy, Condition{
				Type: TypeStarted, Status: StatusFalse, Reason: "DialTimeout",
			}),
			want: StateRetrying(),
		},
		{
			// The restart path: the previous attempt's Started=False is still
			// written when the next attempt begins. Reading it as failed makes a
			// badge and a gauge flap to failed on every restart.
			name:      "the previous attempt failed and the next one is in flight",
			lifecycle: LifecycleStarting,
			conds: replace(healthy, Condition{
				Type: TypeStarted, Status: StatusFalse, Reason: "DialTimeout",
			}),
			want: StateStarting(),
		},
		{
			name:      "start in flight",
			lifecycle: LifecycleStarting,
			conds:     without(healthy, TypeStarted),
			want:      StateStarting(),
		},
		{
			name:      "start in flight outranks an open breaker",
			lifecycle: LifecycleStarting,
			conds: replace(healthy, Condition{
				Type: TypeBreakerClosed, Status: StatusFalse, Reason: "ConsecutiveFailures",
			}),
			want: StateStarting(),
		},
		{
			name:      "never started",
			lifecycle: LifecycleActive,
			conds:     without(healthy, TypeStarted),
			want:      StatePending(),
		},
		{
			name:      "waiting on a recoverable dependency",
			lifecycle: LifecycleActive,
			conds: replace(healthy, Condition{
				Type: TypeDependenciesSatisfied, Status: StatusFalse, Reason: "DependencyStarting",
			}),
			want: StatePending(),
		},
		{
			name:      "an empty set has not been visited",
			lifecycle: LifecycleActive,
			want:      StatePending(),
		},
		{
			name:      "the breaker is open",
			lifecycle: LifecycleActive,
			conds: replace(healthy, Condition{
				Type: TypeBreakerClosed, Status: StatusFalse, Reason: "FailureRatio",
			}),
			want: StateDegraded(),
		},
		{
			name:      "the remote stopped answering",
			lifecycle: LifecycleActive,
			conds: replace(healthy, Condition{
				Type: TypeReachable, Status: StatusFalse, Reason: "DialTimeout",
			}),
			want: StateDegraded(),
		},
		{
			name:      "reachability not yet probed is not degraded",
			lifecycle: LifecycleActive,
			conds:     without(healthy, TypeReachable),
			want:      StateReady(),
		},
		{
			name:      "a kind with no breaker omits the condition",
			lifecycle: LifecycleActive,
			conds:     without(healthy, TypeBreakerClosed),
			want:      StateReady(),
		},
		{
			name:      "everything true",
			lifecycle: LifecycleActive,
			conds:     healthy,
			want:      StateReady(),
		},
		{
			// A reload constructs a new instance and bumps the generation. Every
			// condition still describes the previous one, so nothing is known yet.
			name:       "a reload leaves every condition a generation behind",
			generation: generation + 1,
			lifecycle:  LifecycleActive,
			conds:      healthy,
			want:       StatePending(),
		},
		{
			name:       "a stale degrading condition does not degrade the new instance",
			generation: generation + 1,
			lifecycle:  LifecycleActive,
			conds: append(atGeneration(without(healthy, TypeBreakerClosed), generation+1), Condition{
				Type:               TypeBreakerClosed,
				Status:             StatusFalse,
				Reason:             "ConsecutiveFailures",
				ObservedGeneration: generation,
			}),
			want: StateReady(),
		},
		{
			name:       "a stale failing config does not fail the new instance",
			generation: generation + 1,
			lifecycle:  LifecycleActive,
			conds: append(atGeneration(without(healthy, TypeConfigured), generation+1), Condition{
				Type:               TypeConfigured,
				Status:             StatusFalse,
				Reason:             "ConfigInvalid",
				ObservedGeneration: generation,
			}),
			want: StateReady(),
		},
		{
			name:       "a lifecycle this build does not understand fails closed",
			lifecycle:  Lifecycle(200),
			conds:      healthy,
			want:       StateFailed(),
			generation: generation,
		},
		{
			name:       "the value one past the last declared lifecycle fails closed",
			lifecycle:  LifecycleGaveUp + 1,
			conds:      healthy,
			want:       StateFailed(),
			generation: generation,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gen := tc.generation
			if gen == 0 {
				gen = generation
			}
			set := build(t, atGeneration(tc.conds, generation))
			if got := Derive(gen, tc.lifecycle, set); got != tc.want {
				t.Fatalf("Derive(%d, %d) = %q, want %q", gen, tc.lifecycle, got, tc.want)
			}
		})
	}
}

func TestDeriveOnANilSet(t *testing.T) {
	t.Parallel()

	if got := Derive(generation, LifecycleActive, nil); got != StatePending() {
		t.Fatalf("Derive on a nil set = %q, want %q", got, StatePending())
	}
}

func TestLifecycleValid(t *testing.T) {
	t.Parallel()

	declared := []Lifecycle{
		LifecycleActive, LifecycleDisabled, LifecycleOrphaned, LifecycleStarting,
		LifecycleStopping, LifecycleStopped, LifecycleRetrying, LifecycleGaveUp,
	}
	for _, lc := range declared {
		if !lc.Valid() {
			t.Errorf("Lifecycle(%d).Valid() = false, want true", lc)
		}
	}
	for _, lc := range []Lifecycle{LifecycleGaveUp + 1, 42, 200, 255} {
		if lc.Valid() {
			t.Errorf("Lifecycle(%d).Valid() = true, want false", lc)
		}
	}
}

// A State can only come out of Derive: the zero value is inert, and no exported
// conversion produces a populated one. condition.State("ready") does not compile.
func TestZeroStateIsInert(t *testing.T) {
	t.Parallel()

	var zero State

	if zero.Serving() {
		t.Fatal("the zero State is serving")
	}
	if got := zero.String(); got != "" {
		t.Fatalf("the zero State renders as %q, want empty", got)
	}
	for _, s := range allStates() {
		if zero == s {
			t.Fatalf("the zero State equals %q", s)
		}
	}
}

func TestStateServing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state State
		want  bool
	}{
		{state: StateReady(), want: true},
		{state: StateDegraded(), want: true},
		{state: StateDisabled()},
		{state: StateOrphaned()},
		{state: StatePending()},
		{state: StateStarting()},
		{state: StateRetrying()},
		{state: StateFailed()},
		{state: StateStopping()},
		{state: StateStopped()},
	}

	for _, tc := range tests {
		t.Run(tc.state.String(), func(t *testing.T) {
			t.Parallel()

			if got := tc.state.Serving(); got != tc.want {
				t.Fatalf("%q.Serving() = %v, want %v", tc.state, got, tc.want)
			}
		})
	}
}

func allStates() []State {
	return []State{
		StateDisabled(), StateOrphaned(), StatePending(), StateStarting(), StateReady(),
		StateDegraded(), StateRetrying(), StateFailed(), StateStopping(), StateStopped(),
	}
}

func build(t *testing.T, conds []Condition) *Set {
	t.Helper()

	now := time.Now()
	set := &Set{}
	for _, c := range conds {
		if _, err := set.Set(now, c); err != nil {
			t.Fatalf("Set(%q): %v", c.Type, err)
		}
	}
	return set
}

// atGeneration stamps every condition that has not already claimed one.
func atGeneration(conds []Condition, gen int64) []Condition {
	out := make([]Condition, 0, len(conds))
	for _, c := range conds {
		if c.ObservedGeneration == 0 {
			c.ObservedGeneration = gen
		}
		out = append(out, c)
	}
	return out
}

func without(conds []Condition, typ Type) []Condition {
	out := make([]Condition, 0, len(conds))
	for _, c := range conds {
		if c.Type != typ {
			out = append(out, c)
		}
	}
	return out
}

// replace swaps the condition of the same type, keeping first-write order.
func replace(conds []Condition, c Condition) []Condition {
	out := make([]Condition, 0, len(conds))
	for _, existing := range conds {
		if existing.Type == c.Type {
			out = append(out, c)
			continue
		}
		out = append(out, existing)
	}
	return out
}
