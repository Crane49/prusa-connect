package condition

import (
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestConditionValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cond Condition
		ok   bool
	}{
		{
			name: "true with a reason",
			cond: Condition{Type: TypeStarted, Status: StatusTrue, Reason: "Started"},
			ok:   true,
		},
		{
			name: "reason with the punctuation Kubernetes allows",
			cond: Condition{Type: TypeReachable, Status: StatusFalse, Reason: "Dial_Timeout,Retry:3"},
			ok:   true,
		},
		{
			name: "message may be empty",
			cond: Condition{Type: TypeConfigured, Status: StatusUnknown, Reason: "NotChecked"},
			ok:   true,
		},
		{
			name: "reason at the length limit",
			cond: Condition{Type: TypeStarted, Status: StatusTrue, Reason: strings.Repeat("A", maxReasonLen)},
			ok:   true,
		},
		{
			name: "type at the length limit",
			cond: Condition{Type: Type(strings.Repeat("A", maxTypeLen)), Status: StatusTrue, Reason: "Ok"},
			ok:   true,
		},
		{
			name: "an extension type outside the core set",
			cond: Condition{Type: "IndexRebuilt", Status: StatusTrue, Reason: "Ok"},
			ok:   true,
		},
		{
			name: "empty type",
			cond: Condition{Status: StatusTrue, Reason: "Started"},
		},
		{
			name: "type with a newline",
			cond: Condition{Type: "Started\nInjected", Status: StatusTrue, Reason: "Started"},
		},
		{
			name: "type carrying a json fragment",
			cond: Condition{Type: `{"type":"Started"}`, Status: StatusTrue, Reason: "Started"},
		},
		{
			name: "type with a space",
			cond: Condition{Type: "Has Started", Status: StatusTrue, Reason: "Started"},
		},
		{
			name: "type starting with a digit",
			cond: Condition{Type: "3Started", Status: StatusTrue, Reason: "Started"},
		},
		{
			name: "type one past the length limit",
			cond: Condition{Type: Type(strings.Repeat("A", maxTypeLen+1)), Status: StatusTrue, Reason: "Ok"},
		},
		{
			name: "empty status",
			cond: Condition{Type: TypeStarted, Reason: "Started"},
		},
		{
			name: "lowercase status",
			cond: Condition{Type: TypeStarted, Status: "true", Reason: "Started"},
		},
		{
			name: "empty reason",
			cond: Condition{Type: TypeStarted, Status: StatusTrue},
		},
		{
			name: "reason starting with a digit",
			cond: Condition{Type: TypeStarted, Status: StatusTrue, Reason: "3Started"},
		},
		{
			name: "reason with a space",
			cond: Condition{Type: TypeStarted, Status: StatusTrue, Reason: "Start Failed"},
		},
		{
			name: "reason with a newline",
			cond: Condition{Type: TypeStarted, Status: StatusTrue, Reason: "Started\nInjected"},
		},
		{
			name: "reason ending in a comma",
			cond: Condition{Type: TypeStarted, Status: StatusTrue, Reason: "Started,"},
		},
		{
			name: "reason one past the length limit",
			cond: Condition{Type: TypeStarted, Status: StatusTrue, Reason: strings.Repeat("A", maxReasonLen+1)},
		},
		{
			name: "message one past the length limit",
			cond: Condition{
				Type:    TypeStarted,
				Status:  StatusTrue,
				Reason:  "Started",
				Message: strings.Repeat("x", maxMessageLen+1),
			},
		},
		{
			name: "negative generation",
			cond: Condition{Type: TypeStarted, Status: StatusTrue, Reason: "Started", ObservedGeneration: -1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.cond.Validate()
			if tc.ok && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
			if !tc.ok && !errors.Is(err, ErrInvalidCondition) {
				t.Fatalf("Validate() = %v, want ErrInvalidCondition", err)
			}
		})
	}
}

func TestConditionValidateBoundsTheEchoedType(t *testing.T) {
	t.Parallel()

	oversized := Condition{
		Type:   Type(strings.Repeat("A", 1<<20)),
		Status: StatusTrue,
		Reason: "Ok",
	}
	err := oversized.Validate()
	if !errors.Is(err, ErrInvalidCondition) {
		t.Fatalf("Validate() = %v, want ErrInvalidCondition", err)
	}
	if len(err.Error()) > 128 {
		t.Fatalf("the error is %d bytes; it quotes the rejected type", len(err.Error()))
	}
}

func TestSetRejectsAnInvalidCondition(t *testing.T) {
	t.Parallel()

	var s Set
	changed, err := s.Set(time.Now(), Condition{Type: TypeStarted, Status: StatusTrue})
	if !errors.Is(err, ErrInvalidCondition) {
		t.Fatalf("Set() = %v, want ErrInvalidCondition", err)
	}
	if changed {
		t.Fatal("Set() reported a change for a rejected condition")
	}
	if got := s.All(); len(got) != 0 {
		t.Fatalf("the rejected condition was stored: %v", got)
	}
}

func TestSetRejectsAGenerationRegression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		written int64
		wantErr error
	}{
		{name: "same generation", written: 7},
		{name: "newer generation", written: 8},
		{name: "one generation behind", written: 6, wantErr: ErrStaleGeneration},
		{name: "generation zero after seven", written: 0, wantErr: ErrStaleGeneration},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			now := time.Now()
			var s Set
			if _, err := s.Set(now, Condition{
				Type: TypeStarted, Status: StatusTrue, Reason: "Started", ObservedGeneration: 7,
			}); err != nil {
				t.Fatalf("Set: %v", err)
			}

			_, err := s.Set(now, Condition{
				Type: TypeStarted, Status: StatusFalse, Reason: "DialTimeout", ObservedGeneration: tc.written,
			})
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("Set() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Set() = %v, want %v", err, tc.wantErr)
			}

			got, _ := s.Get(TypeStarted)
			if got.ObservedGeneration != 7 || got.Status != StatusTrue || got.Reason != "Started" {
				t.Fatalf("the refused write landed anyway: %+v", got)
			}
		})
	}
}

func TestNilSetReadsEmptyAndRefusesWrites(t *testing.T) {
	t.Parallel()

	var s *Set

	if got := s.StatusOf(TypeBreakerClosed); got != StatusUnknown {
		t.Fatalf("StatusOf on a nil set = %q, want %q", got, StatusUnknown)
	}
	if _, ok := s.Get(TypeBreakerClosed); ok {
		t.Fatal("Get on a nil set found a condition")
	}
	if got := s.All(); got != nil {
		t.Fatalf("All() on a nil set = %v, want nil", got)
	}

	changed, err := s.Set(time.Now(), Condition{Type: TypeStarted, Status: StatusTrue, Reason: "Started"})
	if !errors.Is(err, ErrNilSet) {
		t.Fatalf("Set() on a nil set = %v, want ErrNilSet", err)
	}
	if changed {
		t.Fatal("Set() on a nil set reported a change")
	}
}

func TestSetLastTransitionTime(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var s Set

		first := time.Now()
		changed, err := s.Set(first, Condition{Type: TypeReachable, Status: StatusTrue, Reason: "Probed"})
		if err != nil {
			t.Fatalf("Set: %v", err)
		}
		if !changed {
			t.Fatal("the first write reported no change")
		}
		if got, _ := s.Get(TypeReachable); !got.LastTransitionTime.Equal(first) {
			t.Fatalf("LastTransitionTime = %v, want %v", got.LastTransitionTime, first)
		}

		time.Sleep(time.Hour)

		// Same status, different reason, message and generation: everything but
		// LastTransitionTime moves.
		changed, err = s.Set(time.Now(), Condition{
			Type:               TypeReachable,
			Status:             StatusTrue,
			Reason:             "Reprobed",
			Message:            "still answering",
			ObservedGeneration: 7,
		})
		if err != nil {
			t.Fatalf("Set: %v", err)
		}
		if changed {
			t.Fatal("a write with an unchanged status reported a change")
		}
		got, _ := s.Get(TypeReachable)
		if !got.LastTransitionTime.Equal(first) {
			t.Fatalf("LastTransitionTime moved to %v on an unchanged status, want %v", got.LastTransitionTime, first)
		}
		if got.Reason != "Reprobed" || got.Message != "still answering" || got.ObservedGeneration != 7 {
			t.Fatalf("the unchanged-status write did not update the other fields: %+v", got)
		}

		time.Sleep(time.Hour)

		flipped := time.Now()
		changed, err = s.Set(flipped, Condition{
			Type: TypeReachable, Status: StatusFalse, Reason: "DialTimeout", ObservedGeneration: 7,
		})
		if err != nil {
			t.Fatalf("Set: %v", err)
		}
		if !changed {
			t.Fatal("a status change reported no change")
		}
		got, _ = s.Get(TypeReachable)
		if !got.LastTransitionTime.Equal(flipped) {
			t.Fatalf("LastTransitionTime = %v, want %v", got.LastTransitionTime, flipped)
		}
	})
}

func TestSetKeepsFirstWriteOrderAndCopiesOnRead(t *testing.T) {
	t.Parallel()

	now := time.Now()
	var s Set
	order := []Type{TypeConfigured, TypeDependenciesSatisfied, TypeStarted}
	for _, typ := range order {
		if _, err := s.Set(now, Condition{Type: typ, Status: StatusTrue, Reason: "Ok"}); err != nil {
			t.Fatalf("Set(%q): %v", typ, err)
		}
	}
	// Rewriting the first condition must not move it to the end.
	if _, err := s.Set(now, Condition{Type: TypeConfigured, Status: StatusTrue, Reason: "Revalidated"}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	all := s.All()
	if len(all) != len(order) {
		t.Fatalf("All() has %d conditions, want %d", len(all), len(order))
	}
	for i, typ := range order {
		if all[i].Type != typ {
			t.Fatalf("All()[%d] is %q, want %q", i, all[i].Type, typ)
		}
	}

	all[0].Reason = "Tampered"
	if got, _ := s.Get(TypeConfigured); got.Reason != "Revalidated" {
		t.Fatalf("mutating All() changed the set: reason is %q", got.Reason)
	}
}

func TestStatusOfTreatsAbsenceAsUnknown(t *testing.T) {
	t.Parallel()

	var s Set
	if got := s.StatusOf(TypeBreakerClosed); got != StatusUnknown {
		t.Fatalf("StatusOf on an empty set = %q, want %q", got, StatusUnknown)
	}
	if _, ok := s.Get(TypeBreakerClosed); ok {
		t.Fatal("Get found a condition that was never written")
	}
}

func TestConditionStale(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		observed   int64
		generation int64
		want       bool
	}{
		{name: "written by the current generation", observed: 7, generation: 7},
		{name: "written by a newer generation", observed: 8, generation: 7},
		{name: "written by an older generation", observed: 6, generation: 7, want: true},
		{name: "never observed", observed: 0, generation: 1, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := Condition{ObservedGeneration: tc.observed}
			if got := c.Stale(tc.generation); got != tc.want {
				t.Fatalf("Stale(%d) = %v, want %v", tc.generation, got, tc.want)
			}
		})
	}
}
